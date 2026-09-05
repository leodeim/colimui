package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type fakeOverviewBackend struct {
	fakeBackend
	samples  int
	disks    int
	cleanups int
	cleanup  error
}

func (b *fakeOverviewBackend) AllStats(string) ([]containerStats, error) {
	b.samples++
	return []containerStats{{ID: "a", CPU: "50%", Memory: "1MiB / 2GiB", Network: "0B / 0B"}}, nil
}
func (b *fakeOverviewBackend) Storage(string) ([]storageRow, error) {
	b.disks++
	return []storageRow{{Type: "Images", Size: "1GB", Reclaimable: "0B"}}, nil
}
func (b *fakeOverviewBackend) Cleanup(string) error {
	b.cleanups++
	return b.cleanup
}

func TestOverviewTotals(t *testing.T) {
	cpu, mem, err := statsTotals([]containerStats{{CPU: "125.5%", Memory: "1MiB / 2GiB"}, {CPU: "2.5%", Memory: "1MB / 2GiB"}})
	if err != nil || cpu != 128 || mem != 2048576 {
		t.Fatal(cpu, mem, err)
	}
	cpu, mem, err = statsTotals(nil)
	if err != nil || cpu != 0 || mem != 0 {
		t.Fatal("empty engine should total zero")
	}
	for _, bad := range []containerStats{{CPU: "NaN", Memory: "1MB / 2GB"}, {CPU: "2%", Memory: "bad"}, {CPU: "2%", Memory: "1PB / 2PB"}} {
		if _, _, err := statsTotals([]containerStats{bad}); err == nil {
			t.Fatal("invalid sample accepted")
		}
	}
}
func TestOverviewParsing(t *testing.T) {
	values, err := parseAllStats([]byte(""))
	if err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
	if _, err := parseAllStats([]byte(`{"CPUPerc":"1%","MemUsage":"1MB / 2GB","NetIO":"0B / 0B"}`)); err == nil {
		t.Fatal("missing ID accepted")
	}
	rows, err := parseStorage([]byte("{\"Type\":\"Images\",\"Size\":\"1GB\",\"Reclaimable\":\"0B\"}\n{\"Type\":\"Containers\",\"Size\":\"0B\"}\n"))
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	for _, s := range []string{"", "{}", "null", "bad"} {
		if _, err := parseStorage([]byte(s)); err == nil {
			t.Fatal("bad storage accepted")
		}
	}
}
func TestOverviewPollingAndFiltering(t *testing.T) {
	b := &fakeOverviewBackend{}
	m := newModel(b, nil)
	m.profiles = []profile{{Name: "dev", Status: "Running"}}
	m.searchQuery = "does-not-match" // Summary must ignore list filters and selection.
	cmd := m.pollStats()
	if cmd == nil || m.pollStats() != nil {
		t.Fatal("stats poll missing or overlap")
	}
	u, _ := m.Update(cmd())
	m = u.(model)
	if len(m.overall.all) != 1 {
		t.Fatal("summary missing")
	}
	if m.pollStorage() != nil {
		t.Fatal("storage polled while closed")
	}
	m.usageOverview = true
	cmd = m.pollStorage()
	if cmd == nil || m.pollStorage() != nil {
		t.Fatal("storage poll missing or overlap")
	}
	u, _ = m.Update(cmd())
	m = u.(model)
	if m.pollStorage() != nil {
		t.Fatal("storage not throttled")
	}
	u, _ = m.Update(statsMsg{profile: "other", aggregate: true})
	m = u.(model)
	if m.overall.profile != "dev" {
		t.Fatal("wrong profile sample accepted")
	}
	m.containers = []container{{ID: "a", State: "running"}}
	m.searchQuery = ""
	if !strings.Contains(strings.Join(m.resourceLines(80), ""), "50%") {
		t.Fatal("selected stats not shared")
	}
	for _, size := range [][2]int{{80, 24}, {60, 16}} {
		m.width, m.height = size[0], size[1]
		v := m.renderUsageOverview()
		if lipgloss.Width(v) > m.width || lipgloss.Height(v) > m.height {
			t.Fatal("overview overflow")
		}
		if !strings.Contains(v, "close") {
			t.Fatal("close hint missing")
		}
	}
	m.storageRequested = time.Now().Add(-31 * time.Second)
	if m.pollStorage() == nil {
		t.Fatal("storage never refreshed")
	}
}
func TestOverviewMenuEntry(t *testing.T) {
	m := newModel(&fakeBackend{}, nil)
	found := false
	for _, item := range m.actionMenuItems() {
		if item.shortcut == "u" {
			found = true
		}
	}
	if !found {
		t.Fatal("overview absent from Actions")
	}
	u, _ := m.key(shortcutKey("u"))
	m = u.(model)
	if !m.usageOverview {
		t.Fatal("shortcut did not open")
	}
	u, _ = m.key(shortcutKey("q"))
	if u.(model).usageOverview {
		t.Fatal("q did not close")
	}
}

func TestOverviewUsesActionsMenuHeaderAndFooterStyle(t *testing.T) {
	m := model{
		width:    100,
		height:   30,
		profiles: []profile{{Name: "default", Status: "Running", CPUs: 2, Memory: 2 << 30, Disk: 100 << 30}},
		storage: storageMsg{profile: "default", at: time.Now(), rows: []storageRow{
			{Type: "Images", Size: "1GB", Reclaimable: "1GB (100%)"},
		}},
		overall: statsMsg{profile: "default", at: time.Now()},
	}
	view := ansi.Strip(m.renderUsageOverview())
	lines := strings.Split(view, "\n")
	var titleLine, footerLine string
	footerIndex := -1
	for index, line := range lines {
		if strings.Contains(line, "docker usage overview") {
			titleLine = line
		}
		if strings.Contains(line, "clean up reclaimable storage") {
			footerLine, footerIndex = line, index
		}
	}
	if strings.Index(titleLine, "docker usage overview") > 6 {
		t.Fatalf("title is not aligned with the Actions menu: %q", titleLine)
	}
	if footerIndex < 1 || !strings.Contains(lines[footerIndex-1], "keyboard shortcuts") || !strings.Contains(footerLine, "esc / q / u") {
		t.Fatalf("footer has no shortcut section: %q", footerLine)
	}
}

func TestOverviewAndActionsMenuUseTheSamePopupWidth(t *testing.T) {
	m := model{width: 100, height: 30}
	if got, want := lipgloss.Width(m.renderActionMenu()), lipgloss.Width(m.renderUsageOverview()); got != want {
		t.Fatalf("actions width = %d, usage width = %d", got, want)
	}
}

func TestOverviewLowercasesDockerDisplayText(t *testing.T) {
	m := model{
		width:    100,
		height:   30,
		profiles: []profile{{Name: "default", Status: "Running"}},
		storage: storageMsg{profile: "default", at: time.Now(), rows: []storageRow{
			{Type: "Local Volumes", Size: "48.65MB", Reclaimable: "48.65MB (100%)"},
		}},
		overall: statsMsg{profile: "default", at: time.Now()},
	}
	view := ansi.Strip(m.renderUsageOverview())
	for _, unexpected := range []string{"Docker usage", "VM allocated", "CPU", "RAM", "Local Volumes", "48.65MB"} {
		if strings.Contains(view, unexpected) {
			t.Fatalf("overview contains title-case display text %q: %q", unexpected, view)
		}
	}
	for _, expected := range []string{"docker usage overview", "vm allocated", "cpu", "ram", "local volumes", "48.65mb"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("overview is missing lowercase display text %q: %q", expected, view)
		}
	}
}

func TestCleanupRequiresConfirmationAndRefreshesStorage(t *testing.T) {
	backend := &fakeOverviewBackend{}
	m := newModel(backend, nil)
	m.width, m.height = 100, 30
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.storage = storageMsg{profile: "default", rows: []storageRow{
		{Type: "Images", Reclaimable: "429.1MB (99%)"},
		{Type: "Local Volumes", Reclaimable: "48.65MB (100%)"},
	}}

	updated, command := m.key(shortcutKey("c"))
	m = updated.(model)
	if command == nil || !m.usageOverview || !m.confirmCleanup || backend.cleanups != 0 {
		t.Fatal("cleanup should open a confirmation before running")
	}
	if view := m.renderCleanupConfirmation(); !strings.Contains(view, "Images: 429.1MB") || !strings.Contains(view, "Local Volumes: 48.65MB") || !strings.Contains(view, "cannot be recovered") {
		t.Fatalf("confirmation lacks cleanup scope: %q", view)
	}

	updated, command = m.key(shortcutKey("y"))
	m = updated.(model)
	if command == nil || m.confirmCleanup || !m.cleanupRunning || backend.cleanups != 0 {
		t.Fatal("confirmation did not start cleanup")
	}
	message := command().(cleanupMsg)
	if backend.cleanups != 1 || message.err != nil {
		t.Fatalf("cleanup call = %d, error = %v", backend.cleanups, message.err)
	}
	updated, command = m.Update(message)
	m = updated.(model)
	if command == nil || m.cleanupRunning || m.status != "cleanup complete" || m.storage.profile != "" {
		t.Fatalf("completion state = %#v", m)
	}
}

func TestCleanupCancellationAndFailure(t *testing.T) {
	backend := &fakeOverviewBackend{cleanup: errors.New("daemon failed")}
	m := newModel(backend, nil)
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.usageOverview, m.confirmCleanup = true, true
	updated, command := m.key(shortcutKey("n"))
	m = updated.(model)
	if command != nil || m.confirmCleanup || backend.cleanups != 0 {
		t.Fatal("cancel ran cleanup")
	}
	m.confirmCleanup = true
	updated, command = m.key(shortcutKey("y"))
	m = updated.(model)
	updated, _ = m.Update(command())
	m = updated.(model)
	if m.status != "cleanup failed" || m.err == nil || m.cleanupRunning {
		t.Fatalf("cleanup failure = %#v", m)
	}
}
