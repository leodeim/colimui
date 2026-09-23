//go:build darwin && cgo

package main

import (
	_ "embed"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
)

const menubarPollInterval = 5 * time.Second

// menubarSupported gates the menu bar toggle to builds that can run it.
const menubarSupported = true

//go:embed assets/menubar-template.png
var menubarIcon []byte

// runMenubar blocks until the menu bar item quits. A locked pidfile keeps it
// a singleton and lets the TUI's toggle find and stop it.
func runMenubar(backend Backend) error {
	lock, err := lockMenubar()
	if err != nil {
		return err
	}
	// systray never invokes onExit with its internal macOS event loop, but Run
	// does return once Quit is called.
	defer lock.Close()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-signals
		systray.Quit()
	}()
	m := &menubar{backend: backend, refresh: make(chan struct{}, 1), busy: map[string]bool{}}
	systray.Run(m.onReady, nil)
	return nil
}

type menubar struct {
	backend Backend
	refresh chan struct{}

	// done closes on every rebuild so click handlers of removed items exit.
	done chan struct{}
	sig  string
	dim  *bool

	// idle is only touched from the sync loop goroutine.
	idle idleTracker

	mu      sync.Mutex
	busy    map[string]bool
	lastErr string
}

func (m *menubar) onReady() {
	systray.SetTooltip("colima")
	go m.loop()
}

func (m *menubar) loop() {
	ticker := time.NewTicker(menubarPollInterval)
	defer ticker.Stop()
	for {
		m.sync()
		select {
		case <-ticker.C:
		case <-m.refresh:
		}
	}
}

// kick requests an immediate sync without blocking the caller.
func (m *menubar) kick() {
	select {
	case m.refresh <- struct{}{}:
	default:
	}
}

func (m *menubar) sync() {
	profiles, err := m.backend.Profiles()
	systray.SetTitle(menubarTitle(profiles))
	m.setIcon(menubarRunning(profiles) == 0)

	auto := resolveMenubarAutoStop()
	m.autoStopCheck(profiles, auto)
	countdowns := map[string]string{}
	for _, p := range profiles {
		if remaining, tracked := m.idle.remaining(p.Name, time.Now(), auto.after); tracked && auto.enabled {
			countdowns[p.Name] = "auto-stop in " + formatCountdown(remaining)
		}
	}

	sig := menubarSignature(profiles) + "auto:" + auto.label()
	for _, p := range profiles {
		sig += p.Name + "=" + countdowns[p.Name] + ";"
	}
	if err != nil {
		sig += "err:" + err.Error()
	}
	m.mu.Lock()
	busy := make([]string, 0, len(m.busy))
	for name := range m.busy {
		busy = append(busy, name)
	}
	sort.Strings(busy)
	sig += "busy:" + strings.Join(busy, ",") + "lastErr:" + m.lastErr
	m.mu.Unlock()

	if sig == m.sig {
		return
	}
	m.sig = sig
	m.rebuild(profiles, err, auto, countdowns)
}

// autoStopCheck advances the idle timers and stops profiles whose window
// elapsed. The menu bar enforces auto-stop for every profile while it runs;
// the TUI defers to it (see trackIdle).
func (m *menubar) autoStopCheck(profiles []profile, auto autoStopState) {
	if !auto.enabled {
		m.idle = idleTracker{}
		return
	}
	for _, p := range profiles {
		if !isRunning(p.Status) || m.isBusy(p.Name) {
			m.idle.clear(p.Name)
			continue
		}
		containers, err := m.backend.Containers(p.Name)
		if err != nil {
			m.idle.clear(p.Name)
			continue
		}
		active := slices.ContainsFunc(containers, func(c container) bool { return isActive(c.State) })
		if m.idle.observe(p.Name, active, time.Now(), auto.after) {
			m.action(p.Name, "stop")()
		}
	}
}

// setIcon swaps between the full and dimmed template icon, only on change so
// the native image is not rebuilt on every poll.
func (m *menubar) setIcon(dim bool) {
	if m.dim != nil && *m.dim == dim {
		return
	}
	m.dim = &dim
	icon := menubarIcon
	if dim {
		icon = dimmedIcon(menubarIcon)
	}
	systray.SetTemplateIcon(icon, icon)
}

func (m *menubar) rebuild(profiles []profile, listErr error, auto autoStopState, countdowns map[string]string) {
	if m.done != nil {
		close(m.done)
	}
	m.done = make(chan struct{})
	done := m.done
	systray.ResetMenu()

	if listErr != nil {
		systray.AddMenuItem(truncate("colima unavailable: "+listErr.Error(), 70), listErr.Error()).Disable()
		systray.AddSeparator()
	}
	m.mu.Lock()
	lastErr := m.lastErr
	m.mu.Unlock()
	if lastErr != "" {
		systray.AddMenuItem(truncate("error: "+lastErr, 70), lastErr).Disable()
		systray.AddSeparator()
	}

	for _, p := range profiles {
		item := systray.AddMenuItem(menubarProfileLine(p), "")
		item.AddSubMenuItem(menubarProfileDetails(p), "").Disable()
		if countdown := countdowns[p.Name]; countdown != "" {
			item.AddSubMenuItem(countdown, "").Disable()
		}
		if m.isBusy(p.Name) {
			item.AddSubMenuItem("Working…", "").Disable()
			continue
		}
		if isRunning(p.Status) {
			m.handle(done, item.AddSubMenuItem("Stop", ""), m.action(p.Name, "stop"))
			m.handle(done, item.AddSubMenuItem("Restart", ""), m.action(p.Name, "restart"))
		} else {
			m.handle(done, item.AddSubMenuItem("Start", ""), m.action(p.Name, "start"))
		}
	}
	if len(profiles) > 0 {
		systray.AddSeparator()
	}

	autoItem := systray.AddMenuItem(auto.label(), "Stop idle profiles automatically")
	if auto.env || auto.err != nil {
		autoItem.Disable()
	} else {
		m.handle(done, autoItem, m.toggleAutoStop(auto))
	}
	systray.AddSeparator()

	m.handle(done, systray.AddMenuItem("Open colimui", "Open the colimui TUI in Terminal"), openTUI)
	m.handle(done, systray.AddMenuItem("Quit", ""), systray.Quit)
}

// toggleAutoStop persists the flipped setting; the next sync re-reads it, so
// the TUI and the menu bar stay on the one saved value.
func (m *menubar) toggleAutoStop(auto autoStopState) func() {
	return func() {
		err := updateSettings(settingsPath(), func(s *settings) {
			s.AutoStop = auto.after.String()
			if auto.enabled {
				s.AutoStop = "off"
			}
		})
		m.setLastErr(err)
		m.kick()
	}
}

// handle runs fn on every click until the menu is rebuilt.
func (m *menubar) handle(done chan struct{}, item *systray.MenuItem, fn func()) {
	go func() {
		for {
			select {
			case <-item.ClickedCh:
				fn()
			case <-done:
				return
			}
		}
	}()
}

func (m *menubar) action(profileName, command string) func() {
	return func() {
		go func() {
			m.setBusy(profileName, true)
			m.kick()
			err := m.backend.Action(profileName, "colima", command, "--profile", profileName)
			m.setBusy(profileName, false)
			m.setLastErr(err)
			m.kick()
		}()
	}
}

func (m *menubar) isBusy(profileName string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.busy[profileName]
}

func (m *menubar) setBusy(profileName string, busy bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if busy {
		m.busy[profileName] = true
	} else {
		delete(m.busy, profileName)
	}
}

func (m *menubar) setLastErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastErr = ""
	if err != nil {
		m.lastErr = err.Error()
	}
}

func openTUI() {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	shellCommand := "'" + strings.ReplaceAll(executable, "'", `'\''`) + "'"
	exec.Command("osascript",
		"-e", "tell application \"Terminal\" to activate",
		"-e", "tell application \"Terminal\" to do script "+appleScriptQuote(shellCommand),
	).Run()
}
