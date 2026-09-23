package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestMenubarLock(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if pid, running := menubarPid(); running {
		t.Fatalf("no pidfile should mean not running, got pid %d", pid)
	}

	lock, err := lockMenubar()
	if err != nil {
		t.Fatal(err)
	}
	pid, running := menubarPid()
	if !running || pid != os.Getpid() {
		t.Fatalf("menubarPid() = %d, %t, want %d, true", pid, running, os.Getpid())
	}
	if second, err := lockMenubar(); !errors.Is(err, errMenubarRunning) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second lockMenubar() error = %v, want errMenubarRunning", err)
	}

	lock.Close()
	if pid, running := menubarPid(); running {
		t.Fatalf("released lock should mean not running, got pid %d", pid)
	}
	relock, err := lockMenubar()
	if err != nil {
		t.Fatalf("relock after release: %v", err)
	}
	relock.Close()
}

// An unlocked pidfile naming a live process (here: this test) is exactly the
// pid-reuse case after a crash; it must not count as a running menu bar.
func TestMenubarPidIgnoresUnlockedFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := menubarPidPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"live pid": strconv.Itoa(os.Getpid()) + "\n",
		"garbage":  "not-a-pid\n",
		"negative": "-4\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if pid, running := menubarPid(); running {
				t.Errorf("unlocked pidfile %q should mean not running, got pid %d", content, pid)
			}
			if err := stopMenubar(); err != nil {
				t.Errorf("stopMenubar() on unlocked pidfile = %v, want no-op", err)
			}
		})
	}
}
