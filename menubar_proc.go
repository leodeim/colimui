package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// menubarPidPath is the pidfile beside the config file, empty when no config
// directory can be resolved; the pidfile keeps the menu bar item a singleton
// and lets the TUI stop it.
func menubarPidPath() string {
	config := settingsPath()
	if config == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(config), "menubar.pid")
}

// menubarPid reports the pid recorded in the pidfile when that process is
// still alive; a stale or unreadable pidfile counts as not running.
func menubarPid() (int, bool) {
	path := menubarPidPath()
	if path == "" {
		return 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	process, err := os.FindProcess(pid)
	if err != nil || process.Signal(syscall.Signal(0)) != nil {
		return 0, false
	}
	return pid, true
}

func writeMenubarPidfile() error {
	path := menubarPidPath()
	if path == "" {
		return errors.New("cannot resolve a config directory for the pidfile")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

func removeMenubarPidfile() {
	if path := menubarPidPath(); path != "" {
		os.Remove(path)
	}
}

// spawnMenubar starts `colimui menubar` in its own session so it survives the
// TUI exiting; it is a no-op when one is already running.
func spawnMenubar() error {
	if _, running := menubarPid(); running {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "menubar")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// menubarAlive reports whether a menu bar process is running; while it is,
// it owns idle auto-stop and the TUI must not dispatch stops of its own.
func menubarAlive() bool {
	_, running := menubarPid()
	return running
}

func stopMenubar() error {
	pid, running := menubarPid()
	if !running {
		return nil
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(syscall.SIGTERM)
}
