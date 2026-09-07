package main

import (
	"errors"
	"os/exec"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestShellKeyOpensExecForRunningContainer(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "dev", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "web", State: "running", Status: "Up"}}
	updated, cmd := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := updated.(model)
	if cmd == nil || got.status != "shell: web" || backend.shellProfileName != "dev" || backend.shellID != "one" {
		t.Fatalf("shell open = command %t status %q backend %q/%q", cmd != nil, got.status, backend.shellProfileName, backend.shellID)
	}
}

func TestShellKeyRefusesStoppedContainer(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "dev", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "web", State: "exited", Status: "Exited"}}
	updated, cmd := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	got := updated.(model)
	if cmd != nil || backend.shellID != "" || got.status != "start the container before opening a shell" {
		t.Fatalf("stopped shell = command %t backend %q status %q", cmd != nil, backend.shellID, got.status)
	}
}

func TestShellFailedSeparatesUserExitFromExecFailure(t *testing.T) {
	if shellFailed(exec.Command("sh", "-c", "exit 1").Run()) {
		t.Fatal("user shell exit reported as failure")
	}
	if !shellFailed(exec.Command("sh", "-c", "exit 127").Run()) {
		t.Fatal("missing shell exit code not reported as failure")
	}
	if !shellFailed(errors.New("no terminal")) {
		t.Fatal("non-exit error not reported as failure")
	}
}

func TestExecDoneRefreshesAndReportsFailure(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	updated, cmd := m.Update(execDoneMsg{})
	got := updated.(model)
	if got.status != "ready" || got.err != nil || cmd == nil {
		t.Fatalf("clean shell exit = status %q err %v refresh %t", got.status, got.err, cmd != nil)
	}
	updated, _ = got.Update(execDoneMsg{err: errors.New("no terminal")})
	got = updated.(model)
	if got.status != "shell failed" || got.err == nil {
		t.Fatalf("failed shell exit = status %q err %v", got.status, got.err)
	}
}
