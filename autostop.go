package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const autoStopDefault = 30 * time.Minute
const autoStopEnv = "COLIMUI_AUTO_STOP"

// resolveAutoStop picks the idle window: COLIMUI_AUTO_STOP wins for this run,
// then the saved config, then the default. path only names the config file in
// errors.
func resolveAutoStop(envValue string, saved settings, path string) (time.Duration, bool, error) {
	if value := strings.TrimSpace(envValue); value != "" {
		after, enabled, err := parseAutoStop(value)
		if err != nil {
			return 0, false, fmt.Errorf("invalid %s %q: %w", autoStopEnv, value, err)
		}
		return after, enabled, nil
	}
	if saved.AutoStop == "" {
		return autoStopDefault, true, nil
	}
	after, enabled, err := parseAutoStop(saved.AutoStop)
	if err != nil {
		return 0, false, fmt.Errorf("invalid auto_stop %q in %s: %w", saved.AutoStop, path, err)
	}
	return after, enabled, nil
}

// parseAutoStop accepts "off"/"0"/"false" or a duration of at least a minute;
// a disabled setting keeps the default window so the toggle stays usable.
func parseAutoStop(value string) (time.Duration, bool, error) {
	switch strings.ToLower(value) {
	case "off", "0", "false":
		return autoStopDefault, false, nil
	}
	after, err := time.ParseDuration(value)
	if err != nil || after < time.Minute {
		return 0, false, errors.New("use a duration of 1m or more (e.g. 45m, 2h) or \"off\"")
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
