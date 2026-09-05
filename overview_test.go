package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type fakeOverviewBackend struct {
	fakeBackend
	samples int
	disks   int
}

func (b *fakeOverviewBackend) AllStats(string) ([]containerStats, error) {
	b.samples++
	return []containerStats{{ID: "a", CPU: "50%", Memory: "1MiB / 2GiB", Network: "0B / 0B"}}, nil
}
func (b *fakeOverviewBackend) Storage(string) ([]storageRow, error) {
	b.disks++
	return []storageRow{{Type: "Images", Size: "1GB", Reclaimable: "0B"}}, nil
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
