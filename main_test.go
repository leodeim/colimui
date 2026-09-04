package main

import (
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestDockerContext(t *testing.T) {
	if got := dockerContext("default"); got != "colima" {
		t.Fatalf("default context = %q", got)
	}
	if got := dockerContext("dev"); got != "colima-dev" {
		t.Fatalf("profile context = %q", got)
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
