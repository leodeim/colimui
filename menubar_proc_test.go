package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestMenubarPid(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if pid, running := menubarPid(); running {
		t.Fatalf("no pidfile should mean not running, got pid %d", pid)
	}

	if err := writeMenubarPidfile(); err != nil {
		t.Fatal(err)
	}
	pid, running := menubarPid()
	if !running || pid != os.Getpid() {
		t.Fatalf("menubarPid() = %d, %t, want %d, true", pid, running, os.Getpid())
	}

	removeMenubarPidfile()
	if _, running := menubarPid(); running {
		t.Fatal("removed pidfile should mean not running")
	}
}

func TestMenubarPidStale(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := menubarPidPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"garbage":     "not-a-pid\n",
		"negative":    "-4\n",
		"nonexistent": strconv.Itoa(1<<30-1) + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if pid, running := menubarPid(); running {
				t.Errorf("pidfile %q should mean not running, got pid %d", content, pid)
			}
		})
	}
}
