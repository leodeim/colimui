package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// settings is the schema of the colimui config file; values are written on
// toggle and loaded at startup.
type settings struct {
	AutoStop      string `json:"auto_stop,omitempty"`
	LogTimestamps bool   `json:"log_timestamps,omitempty"`
	LogWrap       bool   `json:"log_wrap,omitempty"`
}

// settingsPath follows XDG ($XDG_CONFIG_HOME, else ~/.config) on every
// platform: terminal tools live in ~/.config on macOS too, where Go's
// os.UserConfigDir would pick ~/Library/Application Support. Empty when no
// home directory can be resolved; persistence is then disabled and toggles
// apply to the current run only.
func settingsPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "colimui", "config.json")
}

func loadSettings(path string) (settings, error) {
	var s settings
	if path == "" {
		return s, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

// updateSettings applies one change on top of the saved file so values
// written by other toggles are preserved.
func updateSettings(path string, apply func(*settings)) error {
	if path == "" {
		return nil
	}
	s, err := loadSettings(path)
	if err != nil {
		return err
	}
	apply(&s)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// persistSetting saves one settings change; on failure the toggle still
// applies to the current run and the footer reports it was not saved.
func (m *model) persistSetting(apply func(*settings)) {
	if err := updateSettings(m.settingsFile, apply); err != nil {
		m.err, m.status = err, m.status+" (not saved)"
	}
}
