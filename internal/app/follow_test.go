package app

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFollowReconnectsAfterStreamEnds(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "running"}}
	m.follow = true
	reader := &logReader{}
	m.reader = reader
	m.logs = []string{"2024-01-02T03:04:05.000000001Z hello"}
	updated, cmd := m.Update(logsMsg{reader: reader, done: true})
	got := updated.(model)
	if !got.follow || got.reader != nil || cmd == nil {
		t.Fatalf("stream end = follow %t reader %v retry %t", got.follow, got.reader, cmd != nil)
	}
	updated, _ = got.Update(logRetryMsg{})
	got = updated.(model)
	if !got.follow || !backend.logFollow || backend.logSince != "2024-01-02T03:04:05.000000002Z" {
		t.Fatalf("reconnect = follow %t backend follow %t since %q", got.follow, backend.logFollow, backend.logSince)
	}
	if len(got.logs) != 1 {
		t.Fatal("reconnect cleared retained logs")
	}
}

func TestFollowSurvivesNavigatingThroughGroupHeader(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{
		{ID: "a", Name: "app-a", ComposeProject: "app", ComposeService: "a", State: "running"},
		{ID: "b", Name: "app-b", ComposeProject: "app", ComposeService: "b", State: "running"},
	}
	m.follow = true
	m.containerIndex = 1
	updated, _ := m.key(tea.KeyMsg{Type: tea.KeyUp})
	got := updated.(model)
	if !got.follow || got.selectedID() != "" {
		t.Fatalf("group header = follow %t selected %q", got.follow, got.selectedID())
	}
	updated, _ = got.key(tea.KeyMsg{Type: tea.KeyDown})
	got = updated.(model)
	if !got.follow || backend.logID != "a" || !backend.logFollow {
		t.Fatalf("back on container = follow %t opened %q backend follow %t", got.follow, backend.logID, backend.logFollow)
	}
}

func TestFollowSurvivesRefreshSelectionChange(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.width = 100
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "old", Name: "web", State: "running"}}
	m.follow = true
	m.appliedRefreshID, m.refreshID = 1, 1
	updated, _ := m.Update(refreshMsg{
		profileName: "default",
		requestID:   2,
		profiles:    m.profiles,
		containers:  []container{{ID: "new", Name: "web", State: "running"}},
	})
	got := updated.(model)
	if !got.follow || backend.logID != "new" || !backend.logFollow {
		t.Fatalf("recreated container = follow %t opened %q backend follow %t", got.follow, backend.logID, backend.logFollow)
	}
}

func TestFollowRetryWaitsForStoppedContainer(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "exited"}}
	m.follow = true
	updated, cmd := m.Update(logRetryMsg{})
	got := updated.(model)
	if !got.follow || cmd == nil || backend.logID != "" {
		t.Fatalf("stopped container retry = follow %t rescheduled %t opened %q", got.follow, cmd != nil, backend.logID)
	}
}

func TestFollowStopsOnStreamErrorOrPause(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "running"}}
	m.follow = true
	reader := &logReader{}
	m.reader = reader
	updated, cmd := m.Update(logsMsg{reader: reader, done: true, err: errors.New("boom")})
	got := updated.(model)
	if got.follow || cmd != nil {
		t.Fatalf("stream error = follow %t retry %t", got.follow, cmd != nil)
	}

	got.follow = false
	if _, cmd = got.Update(logRetryMsg{}); cmd != nil {
		t.Fatal("paused follow still scheduled a retry")
	}
}

func TestNonFollowStreamEndDoesNotRetry(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "running"}}
	reader := &logReader{}
	m.reader = reader
	if _, cmd := m.Update(logsMsg{reader: reader, done: true}); cmd != nil {
		t.Fatal("tail load end scheduled a follow retry")
	}
}

func TestClearLogsKeepsStreamAndSkipsClearedTailOnReconnect(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "running"}}
	m.follow = true
	reader := &logReader{}
	m.reader = reader
	m.appendLogs("2024-01-02T03:04:05.000000001Z old\n2024-01-02T03:04:06Z half")
	m.logScroll, m.logSelActive, m.logsTruncated = 1, true, true

	updated, _ := m.key(shortcutKey("z"))
	got := updated.(model)
	if len(got.logs) != 0 || got.logScroll != 0 || got.logSelActive || got.logsTruncated {
		t.Fatalf("clear left logs %q scroll %d selection %t truncated %t", got.logs, got.logScroll, got.logSelActive, got.logsTruncated)
	}
	if got.reader != reader || !got.follow {
		t.Fatalf("clear stopped the stream: reader kept %t follow %t", got.reader == reader, got.follow)
	}
	updated, _ = got.Update(logsMsg{reader: reader, data: []byte(" line\n")})
	got = updated.(model)
	if len(got.logs) != 1 || got.logs[0] != "2024-01-02T03:04:06Z half line" {
		t.Fatalf("partial line split by clear: %q", got.logs)
	}

	got.logs, got.logBytes = nil, 0
	updated, _ = got.key(shortcutKey("z"))
	got = updated.(model)
	updated, _ = got.Update(logsMsg{reader: reader, done: true})
	got = updated.(model)
	updated, _ = got.Update(logRetryMsg{})
	got = updated.(model)
	if backend.logSince != "2024-01-02T03:04:05.000000002Z" {
		t.Fatalf("reconnect after empty re-clear lost the cutoff: since %q", backend.logSince)
	}

	updated, _ = got.key(shortcutKey("l"))
	got = updated.(model)
	if got.logClearedSince != "" || backend.logSince != "" {
		t.Fatalf("reload kept the clear cutoff: model %q backend %q", got.logClearedSince, backend.logSince)
	}
}

func TestClearLogsNeedsSelectedContainer(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.containers = []container{
		{ID: "a", Name: "app-a", ComposeProject: "app", ComposeService: "a", State: "running"},
		{ID: "b", Name: "app-b", ComposeProject: "app", ComposeService: "b", State: "running"},
	}
	m.logs = []string{"kept"}
	updated, _ := m.key(shortcutKey("z"))
	if got := updated.(model); len(got.logs) != 1 || got.status == "logs cleared" {
		t.Fatalf("clear on group header = logs %q status %q", got.logs, got.status)
	}
	for _, item := range m.actionMenuItems() {
		if item.shortcut == "z" && item.enabled {
			t.Fatal("clear enabled without a selected container")
		}
	}
}
