package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"time"
)

// idleTracker records how long each running profile has had no active
// containers, for the menu bar's idle auto-stop.
type idleTracker struct {
	idleSince map[string]time.Time
}

// observe updates one profile's idle state and reports whether the idle
// window elapsed; a fire clears the entry so the stop dispatches only once.
func (t *idleTracker) observe(name string, active bool, now time.Time, after time.Duration) bool {
	if active {
		delete(t.idleSince, name)
		return false
	}
	if t.idleSince == nil {
		t.idleSince = map[string]time.Time{}
	}
	since, tracked := t.idleSince[name]
	if !tracked {
		t.idleSince[name] = now
		return false
	}
	if now.Sub(since) < after {
		return false
	}
	delete(t.idleSince, name)
	return true
}

func (t *idleTracker) clear(name string) {
	delete(t.idleSince, name)
}

// remaining reports the countdown for a profile currently tracked as idle.
func (t *idleTracker) remaining(name string, now time.Time, after time.Duration) (time.Duration, bool) {
	since, tracked := t.idleSince[name]
	if !tracked {
		return 0, false
	}
	remaining := after - now.Sub(since)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

// autoStopState is the menu bar's view of the auto-stop setting, re-resolved
// every poll so toggles from the TUI apply without a restart.
type autoStopState struct {
	after   time.Duration
	enabled bool
	env     bool
	err     error
}

func resolveMenubarAutoStop() autoStopState {
	path := settingsPath()
	saved, err := loadSettings(path)
	if err != nil {
		return autoStopState{err: err}
	}
	after, enabled, err := resolveAutoStop(os.Getenv(autoStopEnv), saved, path)
	return autoStopState{after: after, enabled: enabled, env: strings.TrimSpace(os.Getenv(autoStopEnv)) != "", err: err}
}

// label is the auto-stop menu item text; env-pinned and broken settings
// render as a non-clickable state description instead of a toggle.
func (a autoStopState) label() string {
	switch {
	case a.err != nil:
		return "idle auto-stop: invalid setting"
	case a.env && a.enabled:
		return fmt.Sprintf("idle auto-stop: %s (%s)", formatCountdown(a.after), autoStopEnv)
	case a.env:
		return fmt.Sprintf("idle auto-stop: off (%s)", autoStopEnv)
	case a.enabled:
		return "Disable auto-stop"
	default:
		return "Enable auto-stop"
	}
}

// menubarRunning counts profiles in the running state.
func menubarRunning(profiles []profile) int {
	running := 0
	for _, p := range profiles {
		if isRunning(p.Status) {
			running++
		}
	}
	return running
}

// menubarTitle is the text next to the menu bar icon: the running-profile
// count when several run, otherwise nothing (the icon alone shows the state).
func menubarTitle(profiles []profile) string {
	if running := menubarRunning(profiles); running > 1 {
		return fmt.Sprintf("%d", running)
	}
	return ""
}

// dimmedIcon scales the icon's alpha down to render the muted menu bar state
// while nothing runs; color is dropped because template icons only use alpha.
// On any decode or encode failure the original bytes come back unchanged so
// the icon always renders.
func dimmedIcon(iconPNG []byte) []byte {
	src, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return iconPNG
	}
	bounds := src.Bounds()
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA()
			out.SetNRGBA(x, y, color.NRGBA{A: uint8((a >> 8) * 2 / 5)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return iconPNG
	}
	return buf.Bytes()
}

func menubarProfileLine(p profile) string {
	status := p.Status
	if status == "" {
		status = "Unknown"
	}
	return fmt.Sprintf("%s — %s", p.Name, status)
}

func menubarProfileDetails(p profile) string {
	details := fmt.Sprintf("%d cpu · %s ram · %s disk", p.CPUs, humanBytes(p.Memory), humanBytes(p.Disk))
	if p.Arch != "" {
		details += " · " + p.Arch
	}
	return details
}

// menubarSignature identifies the menu-relevant state; the menu is rebuilt
// only when it changes, so an open menu is not torn down on every poll.
func menubarSignature(profiles []profile) string {
	var b strings.Builder
	for _, p := range profiles {
		fmt.Fprintf(&b, "%s|%s|%d|%d|%d;", p.Name, p.Status, p.CPUs, p.Memory, p.Disk)
	}
	return b.String()
}

// appleScriptQuote escapes a value for use inside an AppleScript string literal.
func appleScriptQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
