package main

import (
	"errors"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"testing"
	"time"
)

type fakeStatsBackend struct {
	fakeBackend
	calls       int
	profile, id string
}

func (b *fakeStatsBackend) Stats(profile, id string) (containerStats, error) {
	b.calls++
	b.profile, b.id = profile, id
	return containerStats{CPU: "1.2%", Memory: "12MiB / 2GiB", Network: "1MB / 2MB"}, nil
}

func TestStatsParse(t *testing.T) {
	good := `{"CPUPerc":"1.2%","MemUsage":"12MiB / 2GiB","NetIO":"1MB / 2MB"}`
	s, err := parseStats([]byte(good))
	if err != nil || s.CPU != "1.2%" {
		t.Fatal(s, err)
	}
	for _, s := range []string{"", "null", "{}", "garbage", good + good} {
		if _, err := parseStats([]byte(s)); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}

func TestStatsPollingAndStaleSelection(t *testing.T) {
	b := &fakeStatsBackend{}
	m := newModel(b, nil)
	if m.pollStats() != nil {
		t.Fatal("polled empty selection")
	}
	m.containers = []container{{ID: "a", State: "exited"}}
	if m.pollStats() != nil {
		t.Fatal("polled stopped container")
	}
	m.containers[0].State = "running"
	cmd := m.pollStats()
	if cmd == nil || !m.statsBusy || m.pollStats() != nil {
		t.Fatal("poll overlap")
	}
	msg := cmd().(statsMsg)
	if b.calls != 1 || b.id != "a" || b.profile != "default" {
		t.Fatal("wrong request")
	}
	m.containers = []container{{ID: "b", State: "running"}}
	u, _ := m.Update(msg)
	m = u.(model)
	if m.statsBusy || m.stats.id != "" {
		t.Fatal("stale response applied")
	}
	cmd = m.pollStats()
	msg = cmd().(statsMsg)
	u, _ = m.Update(msg)
	m = u.(model)
	if m.stats.id != "b" {
		t.Fatal("current response missing")
	}
	lines := strings.Join(m.resourceLines(80), "\n")
	for _, want := range []string{"1.2%", "12MiB / 2GiB", "1MB / 2MB"} {
		if !strings.Contains(lines, want) {
			t.Fatal(lines)
		}
	}
	m.stats.err = errors.New("offline")
	if !strings.Contains(strings.Join(m.resourceLines(80), ""), "offline") {
		t.Fatal("missing error")
	}
	m.stats.err = nil
	m.stats.at = time.Now().Add(-time.Minute)
	if !strings.Contains(strings.Join(m.resourceLines(80), ""), "stale") {
		t.Fatal("old sample looks live")
	}
	m.stats.profile = "other"
	if !strings.Contains(strings.Join(m.resourceLines(80), ""), "loading") {
		t.Fatal("cross-profile sample displayed")
	}
}

func TestStatsDetailsFit(t *testing.T) {
	m := model{containers: []container{{ID: "a", Name: "test", State: "running", Ports: strings.Repeat("port ", 40)}}, stats: statsMsg{id: "a", profile: "default", at: time.Now(), value: containerStats{CPU: "1%", Memory: "12MiB / 2GiB", Network: "1MB / 2MB"}}}
	v := m.renderDetails(10, 53)
	if lipgloss.Height(v) != 12 || lipgloss.Width(v) > 53 {
		t.Fatalf("pane is %dx%d", lipgloss.Width(v), lipgloss.Height(v))
	}
	for _, label := range []string{"cpu", "memory", "net I/O"} {
		if !strings.Contains(v, label) {
			t.Fatal("missing " + label)
		}
	}
}
