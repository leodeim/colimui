package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
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

// errMenubarRunning means another process holds the menu bar lock.
var errMenubarRunning = errors.New("the menu bar item is already running")

// lockMenubar takes the menu bar's exclusive lock and records the pid in the
// locked file.
func lockMenubar() (*os.File, error) {
	path := menubarPidPath()
	if path == "" {
		return nil, errors.New("cannot resolve a config directory for the pidfile")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	// A concurrent menubarPid probe holds a shared lock for a few syscalls;
	// retry briefly so it cannot make a fresh start fail.
	for attempt := 0; ; attempt++ {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(err, syscall.EWOULDBLOCK) || attempt == 10 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errMenubarRunning
		}
		return nil, err
	}
	if err := file.Truncate(0); err != nil {
		file.Close()
		return nil, err
	}
	if _, err := file.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

// menubarPid reports the pid of the process holding the menu bar lock; an
// unlocked, missing or unreadable pidfile counts as not running.
func menubarPid() (int, bool) {
	path := menubarPidPath()
	if path == "" {
		return 0, false
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return 0, false
	} else if !errors.Is(err, syscall.EWOULDBLOCK) {
		return 0, false
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
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
	// The menu bar outlives this run, so it follows the saved setting rather
	// than this run's COLIMUI_AUTO_STOP override.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, autoStopEnv+"=") })
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
