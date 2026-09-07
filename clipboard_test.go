package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func stubClipboard(t *testing.T) *string {
	t.Helper()
	var copied string
	old := clipboardCopy
	clipboardCopy = func(text string) error {
		copied = text
		return nil
	}
	t.Cleanup(func() { clipboardCopy = old })
	return &copied
}

func TestCopyContainerDetailsKey(t *testing.T) {
	copied := stubClipboard(t)
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "abc123", Name: "web", Image: "nginx", State: "running", Status: "Up 2 hours", Ports: "80/tcp"}}
	updated, _ := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	got := updated.(model)
	for _, want := range []string{"name: web", "id: abc123", "image: nginx", "ports: 80/tcp"} {
		if !strings.Contains(*copied, want) {
			t.Fatalf("details copy missing %q in %q", want, *copied)
		}
	}
	if strings.Contains(*copied, "compose") || got.status != "copied details for web" {
		t.Fatalf("details copy = %q status %q", *copied, got.status)
	}
}

func TestCopyAllLogsKeyStripsTimestamps(t *testing.T) {
	copied := stubClipboard(t)
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.logs = []string{"2024-01-01T00:00:00Z alpha", "2024-01-01T00:00:01Z beta"}
	updated, _ := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
	got := updated.(model)
	if *copied != "alpha\nbeta" || got.status != "copied 2 log lines" {
		t.Fatalf("log copy = %q status %q", *copied, got.status)
	}
	m.logs = nil
	if updated, _ = m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}}); updated.(model).status != "no logs to copy" {
		t.Fatalf("empty log copy status = %q", updated.(model).status)
	}
}
