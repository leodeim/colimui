package main

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fakeBackend struct {
	profiles          []profile
	containers        []container
	profileName       string
	actionProfileName string
	actionCommand     string
	actionArgs        []string
	actionCalls       int
	logProfileName    string
	logID             string
	logFollow         bool
	logFromStart      bool
	logsErr           error
}

func (b *fakeBackend) Profiles() ([]profile, error) {
	return b.profiles, nil
}

func (b *fakeBackend) Containers(profileName string) ([]container, error) {
	b.profileName = profileName
	return b.containers, nil
}

func (b *fakeBackend) Action(profileName, command string, args ...string) error {
	b.actionCalls++
	b.actionProfileName = profileName
	b.actionCommand = command
	b.actionArgs = args
	return nil
}

func (b *fakeBackend) OpenLogs(profileName, id string, follow, fromStart bool) (*logReader, error) {
	b.logProfileName, b.logID = profileName, id
	b.logFollow, b.logFromStart = follow, fromStart
	return nil, b.logsErr
}

func TestDockerContext(t *testing.T) {
	if got := dockerContext("default"); got != "colima" {
		t.Fatalf("default context = %q", got)
	}
	if got := dockerContext("dev"); got != "colima-dev" {
		t.Fatalf("profile context = %q", got)
	}
}

func TestModelInjectsBackendTimerAndLogFactory(t *testing.T) {
	backend := &fakeBackend{
		profiles:   []profile{{Name: "dev", Status: "Running"}},
		containers: []container{{ID: "id", Name: "test", State: "running", Status: "Up"}},
	}
	ticks := 0
	m := newModel(backend, func() tea.Cmd {
		ticks++
		return func() tea.Msg { return tickMsg(time.Now()) }
	})
	msg := m.refreshCmd(1, "dev")().(refreshMsg)
	if backend.profileName != "dev" || msg.profileName != "dev" || len(msg.containers) != 1 {
		t.Fatalf("refresh backend call = profile %q message %#v", backend.profileName, msg)
	}
	if tick := m.nextTick(); tick == nil || ticks != 1 {
		t.Fatalf("timer factory calls = %d", ticks)
	}
	if action := m.actionCmd("dev", "restart", "docker", "restart", "id")().(actionMsg); action.err != nil || backend.actionProfileName != "dev" || backend.actionCommand != "docker" || strings.Join(backend.actionArgs, " ") != "restart id" {
		t.Fatalf("action backend call = profile %q command %q args %#v", backend.actionProfileName, backend.actionCommand, backend.actionArgs)
	}
	m.profiles, m.containers = backend.profiles, backend.containers
	if cmd := m.reloadSelectedLogs(); cmd != nil || backend.logProfileName != "dev" || backend.logID != "id" {
		t.Fatalf("log factory call = profile %q id %q", backend.logProfileName, backend.logID)
	}
}

func TestDeleteConfirmationDispatchesOneAction(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "id", Name: "test", State: "exited", Status: "Exited"}}
	m.confirmDelete = true
	m.deleteProfile, m.deleteID = "default", "id"
	updated, cmd := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got := updated.(model)
	if cmd == nil || got.confirmDelete || len(got.activeActions) != 1 {
		t.Fatalf("delete state = confirm %t active %d command %v", got.confirmDelete, len(got.activeActions), cmd != nil)
	}
	var requestID uint64
	for requestID = range got.activeActions {
	}
	updated, duplicate := got.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if duplicate != nil || len(updated.(model).activeActions) != 1 {
		t.Fatal("duplicate delete confirmation started another action")
	}
	if msg := cmd().(actionMsg); msg.requestID != requestID || backend.actionCalls != 1 {
		t.Fatalf("delete message = %#v calls %d", msg, backend.actionCalls)
	}
}

func TestDeleteConfirmationStaysWithOriginalContainer(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.width = 80
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "exited"}, {ID: "two", Name: "two", State: "exited"}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated, _ = updated.(model).Update(tea.MouseMsg(tea.MouseEvent{X: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown}))
	updated, command := updated.(model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if command == nil {
		t.Fatal("delete confirmation did not dispatch")
	}
	_ = command()
	if len(backend.actionArgs) == 0 || backend.actionArgs[len(backend.actionArgs)-1] != "one" {
		t.Fatalf("deleted %v, want container one", backend.actionArgs)
	}
}

func TestDeleteConfirmationModalSelectsDelete(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.width, m.height = 100, 24
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "exited"}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	updated, _ = updated.(model).Update(tea.KeyMsg{Type: tea.KeyDown})
	got := updated.(model)
	if got.deleteChoice != 1 || !strings.Contains(got.View(), "delete container?") {
		t.Fatalf("delete modal choice = %d", got.deleteChoice)
	}
	updated, command := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || updated.(model).confirmDelete {
		t.Fatal("delete choice did not dispatch")
	}
}

func TestCtrlCQuitsFromModals(t *testing.T) {
	for _, modal := range []model{{actionMenu: true}, {confirmDelete: true}} {
		_, command := modal.key(tea.KeyMsg{Type: tea.KeyCtrlC})
		if command == nil {
			t.Fatal("Ctrl+C returned no quit command")
		}
		if _, ok := command().(tea.QuitMsg); !ok {
			t.Fatal("Ctrl+C did not quit")
		}
	}
}

func TestRefreshAcceptsCompletedRequestWhenNewerOneIsQueued(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.refreshID = 10
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	updated, _ := m.Update(tickMsg{})
	updated, _ = updated.(model).Update(refreshMsg{requestID: 10, profileName: "default", profiles: m.profiles, containers: []container{{ID: "new", Name: "new"}}})
	if got := updated.(model).containers[0].ID; got != "new" {
		t.Fatalf("completed refresh was discarded: %q", got)
	}
}

func TestLogBuffersAreBounded(t *testing.T) {
	m := model{}
	chunk := strings.Repeat("x", 4096)
	for i := 0; i < 512; i++ {
		m.appendLogs(chunk)
	}
	if len(m.logPartial) > maxLogPartialBytes {
		t.Fatalf("partial log retained %d bytes", len(m.logPartial))
	}
}

func TestViewFitsSmallTerminals(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {40, 12}, {20, 5}} {
		m := model{width: size[0], height: size[1], status: "ready"}
		if view := m.View(); lipgloss.Width(view) > m.width || lipgloss.Height(view) > m.height {
			t.Fatalf("terminal %dx%d, view %dx%d", m.width, m.height, lipgloss.Width(view), lipgloss.Height(view))
		}
	}
}

func TestActionIgnoresStaleCompletion(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.activeActions = map[uint64]activeAction{2: {label: "start"}}
	m.status = "starting default"
	updated, cmd := m.Update(actionMsg{requestID: 1, label: "stop"})
	got := updated.(model)
	if cmd != nil || len(got.activeActions) != 1 || got.status != "starting default" {
		t.Fatalf("stale action changed state: %#v", got)
	}
	updated, cmd = got.Update(actionMsg{requestID: 2, label: "start"})
	got = updated.(model)
	if cmd == nil || len(got.activeActions) != 0 || got.status != "start complete" {
		t.Fatalf("active action completion = %#v", got)
	}
}

func TestActiveActionAllowsNavigationAndKeepsStatus(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.activeActions = map[uint64]activeAction{1: {containerID: "one", label: "stop"}}
	m.nextActionID = 1
	m.status = "stopping api"
	m.containers = []container{{ID: "one", Name: "api", State: "running"}, {ID: "two", Name: "worker", State: "running"}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := updated.(model)
	if got.containerIndex != 1 || len(got.activeActions) != 1 {
		t.Fatalf("navigation during action = index %d active %d", got.containerIndex, len(got.activeActions))
	}
	updated, command := got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil || len(updated.(model).activeActions) != 2 {
		t.Fatalf("second container action = active %d command %t", len(updated.(model).activeActions), command != nil)
	}
	updated, _ = got.Update(refreshMsg{profiles: []profile{{Name: "default", Status: "Running"}}, containers: got.containers})
	if got := updated.(model); got.status != "stopping api" {
		t.Fatalf("refresh replaced active action status with %q", got.status)
	}
}

func TestActiveContainerShowsProgress(t *testing.T) {
	m := model{
		width:         100,
		height:        24,
		status:        "stopping api",
		containers:    []container{{ID: "api-id", Name: "api", State: "running", Status: "Up"}},
		activeActions: map[uint64]activeAction{1: {containerID: "api-id", label: "stop"}},
	}
	if view := m.View(); !strings.Contains(view, "stopping…") || !strings.Contains(view, spinnerFrames[0]) {
		t.Fatalf("active container progress missing: %q", view)
	}
	updated, command := m.Update(spinnerTickMsg(time.Now()))
	if command == nil || updated.(model).spinnerFrame != 1 {
		t.Fatalf("spinner tick = frame %d command %t", updated.(model).spinnerFrame, command != nil)
	}
}

func TestHumanBytes(t *testing.T) {
	for _, test := range []struct {
		input int64
		want  string
	}{
		{0, "0b"},
		{1024, "1.0k"},
		{2 * 1024 * 1024, "2.0m"},
	} {
		if got := humanBytes(test.input); got != test.want {
			t.Errorf("humanBytes(%d) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestSystemProfileAndContainers(t *testing.T) {
	if _, err := exec.LookPath("colima"); err != nil {
		t.Skip("colima is not installed")
	}
	profiles, err := listProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) == 0 {
		t.Fatal("expected at least one colima profile")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	containers, err := listContainers(profiles[0].Name)
	if err != nil && !strings.Contains(err.Error(), "Cannot connect") {
		t.Fatal(err)
	}
	_ = containers
}

func TestMouseWheelScrollsLogs(t *testing.T) {
	m := model{width: 120, height: 24, logs: []string{"one", "two", "three", "four"}}
	updated, _ := m.Update(tea.MouseMsg(tea.MouseEvent{
		X: 80, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
	}))
	got := updated.(model)
	if got.logScroll != 3 || got.focus != 1 {
		t.Fatalf("wheel up = scroll %d, focus %d", got.logScroll, got.focus)
	}
	updated, _ = got.Update(tea.MouseMsg(tea.MouseEvent{
		X: 80, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	}))
	if updated.(model).logScroll != 0 {
		t.Fatalf("wheel down = scroll %d", updated.(model).logScroll)
	}
}

func TestTabChangesFocus(t *testing.T) {
	m := model{focus: 0}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := updated.(model).focus; got != 1 {
		t.Fatalf("focus after tab = %d, want 1", got)
	}
}

func TestContainerPaneFitsHeight(t *testing.T) {
	containers := make([]container, 51)
	for i := range containers {
		containers[i] = container{Name: "colimui-test", State: "running", Status: "Up"}
	}
	m := model{containers: containers, containerIndex: 50}
	if got := lipgloss.Height(m.renderContainers(20, 38)); got != 22 {
		t.Fatalf("container pane height = %d, want 22", got)
	}
}

func TestStoppedProfileShowsStartHint(t *testing.T) {
	m := model{profiles: []profile{{Name: "default", Status: "Stopped"}}}
	view := m.renderContainers(10, 38)
	if !strings.Contains(view, "colima is stopped") || !strings.Contains(view, "press s to start") {
		t.Fatalf("stopped profile view = %q", view)
	}
}

func TestStartKeyWorksWithoutProfileRecord(t *testing.T) {
	m := model{status: "ready"}
	updated, cmd := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("start key returned no command")
	}
	if got := updated.(model).status; got != "starting default" {
		t.Fatalf("start status = %q", got)
	}
}

func TestActionMenuShowsShortcuts(t *testing.T) {
	m := model{width: 100, height: 28, status: "ready", actionMenu: true, containers: []container{{Name: "api", State: "running"}}}
	view := m.View()
	for _, text := range []string{"colimui", "containers", "actions", "stop api", "restart api", "delete api", "keyboard shortcuts", "enter run", "[] profile"} {
		if !strings.Contains(view, text) {
			t.Fatalf("action menu is missing %q: %q", text, view)
		}
	}
}

func TestActionMenuRunsSelectedAction(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Stopped"}}
	m.actionMenu = true
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if got.actionMenu || got.status != "starting default" || command == nil {
		t.Fatalf("menu action = open %t status %q command %t", got.actionMenu, got.status, command != nil)
	}
}

func TestDetailsPanePreservesBorder(t *testing.T) {
	m := model{
		containers: []container{{ID: "abc", Name: "test", State: "running", Status: "Up", Image: "alpine"}},
		logs:       make([]string, 200),
	}
	view := m.renderDetails(20, 80)
	if got := lipgloss.Height(view); got != 22 {
		t.Fatalf("details pane height = %d, want 22", got)
	}
	if !strings.Contains(view, "╰") {
		t.Fatal("details pane is missing its bottom border")
	}
}

func TestDetailsPaneIsNotSelectable(t *testing.T) {
	m := model{
		focus:      1,
		containers: []container{{ID: "abc", Name: "test", State: "running", Status: "Up"}},
	}
	if view := m.renderDetails(10, 80); strings.Contains(view, "▸ details") {
		t.Fatal("details pane shows a focus marker")
	}
}

func TestLogsPaneShowsFocus(t *testing.T) {
	m := model{
		focus:      1,
		containers: []container{{ID: "abc", Name: "test", State: "running", Status: "Up"}},
		logs:       []string{"hello"},
	}
	if view := m.renderLogs(8, 80); !strings.Contains(view, "▸ logs") {
		t.Fatal("logs pane is missing its focus marker")
	}
}

func TestVisibleLogsAtStart(t *testing.T) {
	m := model{logs: []string{"first", "second", "third", "last"}, logScroll: 4}
	got := m.visibleLogs(2)
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("logs at start = %#v", got)
	}
}

func TestReadLogsMergesOutputAndReportsFailure(t *testing.T) {
	cmd := exec.Command("sh", "-c", "printf stdout; printf stderr >&2; exit 7")
	reader, err := startLogReader(cmd, func() {})
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	m := model{reader: reader}
	for {
		msg := m.readLogsCmd()().(logsMsg)
		output.Write(msg.data)
		if msg.done {
			if msg.err == nil {
				t.Fatal("expected command failure")
			}
			break
		}
	}
	if got := output.String(); !strings.Contains(got, "stdout") || !strings.Contains(got, "stderr") {
		t.Fatalf("merged output = %q", got)
	}
}

func TestFinishLogsKeepsPartialLine(t *testing.T) {
	m := model{}
	m.appendLogs("complete\npartial")
	m.finishLogs()
	if len(m.logs) != 2 || m.logs[1] != "partial" || m.logPartial != "" {
		t.Fatalf("finished logs = %#v partial %q", m.logs, m.logPartial)
	}
}

func TestMiddleTruncatePreservesSuffix(t *testing.T) {
	got := middleTruncate("colimui-test-01", 10)
	if len(got) != 10 || !strings.HasSuffix(got, "01") {
		t.Fatalf("middle truncate = %q", got)
	}
	if got := statusLabel("Up 31 minutes", 6); got != "Up" {
		t.Fatalf("status label = %q", got)
	}
}

func TestSanitizeTextStripsTerminalControls(t *testing.T) {
	input := "safe\x1b[31mred\x1b[2J \x1b]0;title\a\x1bPdata\x1b\\\x9b?25l next\x1b#8\nlast"
	if got := sanitizeText(input); got != "safered  next last" {
		t.Fatalf("sanitized text = %q", got)
	}
}

func TestRenderSanitizesContainerTextAndLogs(t *testing.T) {
	m := model{
		containers: []container{{
			ID:      "id",
			Name:    "container\x1b]0;title\a",
			Image:   "image\x1b[2J",
			State:   "running",
			Status:  "Up\x1b[?25l",
			Command: "command\x1b#8",
			Ports:   "ports\x9b?25l",
		}},
		logs: []string{"log\x1b]52;c;clipboard\a"},
	}
	if view := m.renderDetails(12, 80); strings.Contains(view, "\x1b]0;title") || strings.Contains(view, "\x1b[2J") {
		t.Fatalf("unsanitized metadata in details: %q", view)
	}
	if view := m.renderLogs(8, 80); strings.Contains(view, "\x1b]52;c;clipboard") {
		t.Fatalf("unsanitized log in logs pane: %q", view)
	}
}

func TestReadAllLogReader(t *testing.T) {
	cmd := exec.Command("sh", "-c", "printf partial")
	reader, err := startLogReader(cmd, func() {})
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader.out)
	if err != nil {
		t.Fatal(err)
	}
	if exitErr := reader.exitError(); exitErr != nil {
		t.Fatalf("command error = %v", exitErr)
	}
	if string(output) != "partial" {
		t.Fatalf("output = %q", output)
	}
}

func TestFinishedLogErrorIncludesOutput(t *testing.T) {
	reader := &logReader{}
	m := model{reader: reader}
	updated, _ := m.Update(logsMsg{reader: reader, data: []byte("docker error\n"), done: true, err: errors.New("exit status 1")})
	got := updated.(model)
	if got.err == nil || !strings.Contains(got.err.Error(), "docker error") {
		t.Fatalf("error = %v", got.err)
	}
}

func TestRefreshPreservesLogError(t *testing.T) {
	containers := []container{{ID: "id", Name: "test", State: "running", Status: "Up"}}
	m := model{
		profiles:   []profile{{Name: "default", Status: "Running"}},
		containers: containers,
		err:        errors.New("docker error"),
		status:     "logs failed",
	}
	updated, _ := m.Update(refreshMsg{profiles: m.profiles, containers: containers})
	got := updated.(model)
	if got.err == nil || got.status != "logs failed" {
		t.Fatalf("refresh error = %v status %q", got.err, got.status)
	}
}

func TestRefreshDropsStaleResponse(t *testing.T) {
	oldContainers := []container{{ID: "old", Name: "old", State: "running", Status: "Up"}}
	m := model{
		profiles:       []profile{{Name: "default", Status: "Running"}, {Name: "dev", Status: "Running"}},
		profileIndex:   1,
		containers:     oldContainers,
		refreshID:      2,
		containerIndex: 0,
	}
	updated, cmd := m.Update(refreshMsg{
		profileName: "default",
		requestID:   1,
		profiles:    []profile{{Name: "default", Status: "Running"}},
		containers:  []container{{ID: "stale", Name: "stale", State: "running", Status: "Up"}},
	})
	got := updated.(model)
	if cmd != nil {
		t.Fatal("stale refresh returned a command")
	}
	if got.currentProfileName() != "dev" || got.containers[0].ID != "old" {
		t.Fatalf("stale refresh changed profile %q or containers %#v", got.currentProfileName(), got.containers)
	}
}

func TestRefreshPreservesProfileName(t *testing.T) {
	m := model{
		profiles:     []profile{{Name: "default", Status: "Running"}, {Name: "dev", Status: "Running"}},
		profileIndex: 1,
		refreshID:    1,
	}
	updated, _ := m.Update(refreshMsg{
		profileName: "dev",
		requestID:   1,
		profiles:    []profile{{Name: "dev", Status: "Running"}, {Name: "default", Status: "Running"}},
	})
	got := updated.(model)
	if got.currentProfileName() != "dev" || got.profileIndex != 0 {
		t.Fatalf("profile after refresh = %q at index %d", got.currentProfileName(), got.profileIndex)
	}
}

func TestComposeLabels(t *testing.T) {
	project, service := composeLabels("com.docker.compose.project=ides,com.docker.compose.service=postgres,other=value")
	if project != "ides" || service != "postgres" {
		t.Fatalf("compose labels = %q, %q", project, service)
	}
}

func TestComposeGroups(t *testing.T) {
	m := model{containers: []container{
		{ID: "1", Name: "ides-postgres-1", ComposeProject: "ides", ComposeService: "postgres", State: "running", Status: "Up"},
		{ID: "2", Name: "ides-api-1", ComposeProject: "ides", ComposeService: "api", State: "exited", Status: "Exited"},
		{ID: "3", Name: "other", State: "running", Status: "Up"},
	}}
	m.syncExpanded()
	items := m.listItems()
	if len(items) != 4 || !items[0].groupHeader || items[0].group != "ides" || items[1].containerIndex != 0 || items[2].containerIndex != 1 || items[3].group != "standalone" {
		t.Fatalf("group items = %#v", items)
	}
	if got := groupSummary(m.selectedGroupByName("ides"), m.containers); got != "1/2 running" {
		t.Fatalf("group summary = %q", got)
	}
}

func TestEnterTogglesComposeGroup(t *testing.T) {
	m := model{containers: []container{{ID: "1", Name: "ides-postgres-1", ComposeProject: "ides", ComposeService: "postgres"}}, expanded: map[string]bool{"ides": true}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(model)
	if got.expanded["ides"] || len(got.listItems()) != 1 || !got.listItems()[0].groupHeader {
		t.Fatalf("collapsed group = %#v expanded=%v", got.listItems(), got.expanded)
	}
}

func TestComposeGroupFitsNarrowPane(t *testing.T) {
	m := model{containers: []container{{ID: "1", Name: "ides-postgres-1", ComposeProject: "ides", ComposeService: "postgres", State: "running", Status: "Up"}}}
	if got := lipgloss.Height(m.renderContainers(20, 26)); got != 22 {
		t.Fatalf("group pane height = %d, want 22", got)
	}
}
