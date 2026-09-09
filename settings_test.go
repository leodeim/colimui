package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSettingsPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	if got := settingsPath(); got != "/custom/config/colimui/config.json" {
		t.Fatalf("settingsPath() = %q", got)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := settingsPath(); got != filepath.Join(home, ".config", "colimui", "config.json") {
		t.Fatalf("settingsPath() = %q", got)
	}
}

func TestLoadSettings(t *testing.T) {
	if s, err := loadSettings(filepath.Join(t.TempDir(), "missing.json")); err != nil || s != (settings{}) {
		t.Fatalf("missing file = %#v, %v", s, err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSettings(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("malformed file error = %v, want it to name %s", err, path)
	}
}

func TestUpdateSettingsPreservesOtherFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "colimui", "config.json")
	if err := updateSettings(path, func(s *settings) { s.AutoStop = "2h" }); err != nil {
		t.Fatal(err)
	}
	if err := updateSettings(path, func(s *settings) { s.LogWrap = true }); err != nil {
		t.Fatal(err)
	}
	s, err := loadSettings(path)
	if err != nil || s.AutoStop != "2h" || !s.LogWrap {
		t.Fatalf("settings after two updates = %#v, %v", s, err)
	}
}

func TestLogToggleKeysPersist(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.settingsFile = filepath.Join(t.TempDir(), "colimui", "config.json")
	updated, _ := m.key(shortcutKey("T"))
	m = updated.(model)
	if !m.logTimestamps || m.status != "log timestamps on" {
		t.Fatalf("T toggle: enabled %t status %q", m.logTimestamps, m.status)
	}
	updated, _ = m.key(shortcutKey("w"))
	m = updated.(model)
	if !m.logWrap || m.status != "log wrap on" {
		t.Fatalf("w toggle: enabled %t status %q", m.logWrap, m.status)
	}
	s, err := loadSettings(m.settingsFile)
	if err != nil || !s.LogTimestamps || !s.LogWrap {
		t.Fatalf("saved settings = %#v, %v", s, err)
	}
	updated, _ = m.key(shortcutKey("T"))
	m = updated.(model)
	s, err = loadSettings(m.settingsFile)
	if err != nil || s.LogTimestamps || !s.LogWrap || m.status != "log timestamps off" {
		t.Fatalf("settings after toggle back = %#v, %v, status %q", s, err, m.status)
	}
}
