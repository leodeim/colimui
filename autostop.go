package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const autoStopDefault = 30 * time.Minute
const autoStopEnv = "COLIMUI_AUTO_STOP"

// autoStopFromEnv reads COLIMUI_AUTO_STOP: unset enables the default window,
// "off"/"0"/"false" disables, otherwise a duration of at least one minute.
func autoStopFromEnv() (time.Duration, bool, error) {
	value := strings.TrimSpace(os.Getenv(autoStopEnv))
	switch strings.ToLower(value) {
	case "":
		return autoStopDefault, true, nil
	case "off", "0", "false":
		return autoStopDefault, false, nil
	}
	after, err := time.ParseDuration(value)
	if err != nil || after < time.Minute {
		return 0, false, fmt.Errorf("invalid %s %q: use a duration of 1m or more (e.g. 45m, 2h) or \"off\"", autoStopEnv, value)
	}
	return after, true, nil
}

// isActive reports states that must hold off the idle auto-stop; paused
// containers count because they would not survive a VM stop.
func isActive(state string) bool {
	switch strings.ToLower(state) {
	case "running", "restarting", "paused":
		return true
	}
	return false
}

// trackIdle runs after each applied refresh: once the current profile has run
// with no active containers for autoStopAfter, stop it like a manual x press.
// Errors and in-flight actions reset the timer rather than risk a bad stop.
func (m *model) trackIdle() tea.Cmd {
	p := m.currentProfile()
	if !m.autoStop || p == nil || !isRunning(p.Status) || m.err != nil || m.hasActiveActions() {
		m.clearIdle()
		return nil
	}
	if m.idleProfile != p.Name {
		m.clearIdle()
		m.idleProfile = p.Name
	}
	for _, c := range m.containers {
		if isActive(c.State) {
			m.idleSince = time.Time{}
			return nil
		}
	}
	now := m.clock()
	if m.idleSince.IsZero() {
		m.idleSince = now
		return nil
	}
	if now.Sub(m.idleSince) < m.autoStopAfter {
		return nil
	}
	m.clearIdle()
	m.status = "auto-stopping " + p.Name + " (idle " + formatCountdown(m.autoStopAfter) + ")"
	return tea.Batch(m.actionCmd(p.Name, "auto-stop", "colima", "stop", "--profile", p.Name), spinnerTick())
}

func (m *model) clearIdle() {
	m.idleProfile, m.idleSince = "", time.Time{}
}

func (m model) idleRemaining() (time.Duration, bool) {
	if !m.autoStop || m.idleSince.IsZero() {
		return 0, false
	}
	remaining := m.autoStopAfter - m.clock().Sub(m.idleSince)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

func formatCountdown(d time.Duration) string {
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Round(time.Minute)/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}
