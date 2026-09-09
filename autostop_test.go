package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAutoStopFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		after   time.Duration
		enabled bool
		wantErr bool
	}{
		{"unset", "", autoStopDefault, true, false},
		{"off", "off", autoStopDefault, false, false},
		{"zero", "0", autoStopDefault, false, false},
		{"custom", "45m", 45 * time.Minute, true, false},
		{"below minimum", "30s", 0, false, true},
		{"garbage", "soon", 0, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(autoStopEnv, tt.value)
			after, enabled, err := autoStopFromEnv()
			if (err != nil) != tt.wantErr {
				t.Fatalf("autoStopFromEnv() error = %v, wantErr %t", err, tt.wantErr)
			}
			if err == nil && (after != tt.after || enabled != tt.enabled) {
				t.Fatalf("autoStopFromEnv() = %v %t, want %v %t", after, enabled, tt.after, tt.enabled)
			}
		})
	}
}

func idleModel(backend Backend, clock *time.Time) model {
	m := newModel(backend, func() tea.Cmd { return nil })
	m.width, m.height = 100, 24
	m.autoStop = true
	m.now = func() time.Time { return *clock }
	return m
}

func refreshAt(m model, msg refreshMsg) (model, tea.Cmd) {
	updated, cmd := m.Update(msg)
	return updated.(model), cmd
}

func TestIdleAutoStopDispatchesAfterThreshold(t *testing.T) {
	backend := &fakeBackend{}
	clock := time.Now()
	m := idleModel(backend, &clock)
	running := refreshMsg{profileName: "default", profiles: []profile{{Name: "default", Status: "Running"}}}

	m, cmd := refreshAt(m, running)
	if cmd != nil || !strings.Contains(m.View(), "auto-stop in 30m") {
		t.Fatalf("first idle refresh: cmd %v view %q", cmd != nil, m.View())
	}

	clock = clock.Add(29 * time.Minute)
	m, cmd = refreshAt(m, running)
	if cmd != nil || !strings.Contains(m.View(), "auto-stop in 1m") {
		t.Fatalf("near-threshold refresh: cmd %v view %q", cmd != nil, m.View())
	}

	clock = clock.Add(2 * time.Minute)
	m, cmd = refreshAt(m, running)
	if cmd == nil || !m.hasActiveProfileAction() || !strings.Contains(m.status, "auto-stopping default") {
		t.Fatalf("threshold refresh: cmd %v active %t status %q", cmd != nil, m.hasActiveProfileAction(), m.status)
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("threshold command = %T, want batch", cmd())
	}
	for _, c := range batch {
		c()
	}
	if backend.actionCommand != "colima" || strings.Join(backend.actionArgs, " ") != "stop --profile default" {
		t.Fatalf("stop dispatch = %q %v", backend.actionCommand, backend.actionArgs)
	}

	// The in-flight stop must not be dispatched a second time.
	clock = clock.Add(time.Hour)
	m, cmd = refreshAt(m, running)
	if cmd != nil || backend.actionCalls != 1 {
		t.Fatalf("refresh during stop: cmd %v calls %d", cmd != nil, backend.actionCalls)
	}
}

func TestIdleTimerResets(t *testing.T) {
	profiles := []profile{{Name: "default", Status: "Running"}, {Name: "dev", Status: "Running"}}
	idle := refreshMsg{profileName: "default", profiles: profiles}
	tests := []struct {
		name string
		msg  refreshMsg
	}{
		{"running container", refreshMsg{profileName: "default", profiles: profiles, containers: []container{{ID: "id", Name: "web", State: "running"}}}},
		{"paused container", refreshMsg{profileName: "default", profiles: profiles, containers: []container{{ID: "id", Name: "web", State: "paused"}}}},
		{"refresh error", refreshMsg{profileName: "default", profiles: profiles, err: errors.New("daemon offline")}},
		{"profile stopped", refreshMsg{profileName: "default", profiles: []profile{{Name: "default", Status: "Stopped"}}}},
		{"profile switched", refreshMsg{profileName: "dev", profiles: profiles}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := &fakeBackend{}
			clock := time.Now()
			m := idleModel(backend, &clock)
			m, _ = refreshAt(m, idle)
			if tt.name == "profile switched" {
				m.profileIndex = 1
			}
			clock = clock.Add(29 * time.Minute)
			m, _ = refreshAt(m, tt.msg)
			clock = clock.Add(2 * time.Minute)
			m, cmd := refreshAt(m, tt.msg)
			if m.hasActiveProfileAction() || backend.actionCalls != 0 {
				t.Fatalf("auto-stop fired: cmd %v calls %d", cmd != nil, backend.actionCalls)
			}
		})
	}
}

func TestAutoStopToggleKey(t *testing.T) {
	backend := &fakeBackend{}
	clock := time.Now()
	m := idleModel(backend, &clock)
	m, _ = refreshAt(m, refreshMsg{profileName: "default", profiles: []profile{{Name: "default", Status: "Running"}}})
	if _, idle := m.idleRemaining(); !idle {
		t.Fatal("idle timer not armed")
	}
	updated, _ := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(model)
	if _, idle := m.idleRemaining(); m.autoStop || idle || m.status != "idle auto-stop off" {
		t.Fatalf("toggle off: enabled %t idle %t status %q", m.autoStop, idle, m.status)
	}
	updated, _ = m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(model)
	if !m.autoStop || m.status != "idle auto-stop on (30m)" {
		t.Fatalf("toggle on: enabled %t status %q", m.autoStop, m.status)
	}
}
