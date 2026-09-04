package main

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDockerContext(t *testing.T) {
	if got := dockerContext("default"); got != "colima" {
		t.Fatalf("default context = %q", got)
	}
	if got := dockerContext("dev"); got != "colima-dev" {
		t.Fatalf("profile context = %q", got)
	}
}

func TestHumanBytes(t *testing.T) {
	for _, test := range []struct {
		input int64
		want  string
	}{
		{0, "0b"},
		{1024, "1.0k"},
		{2 * 1024 * 1024, "2.0m"},
	} {
		if got := humanBytes(test.input); got != test.want {
			t.Errorf("humanBytes(%d) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestSystemProfileAndContainers(t *testing.T) {
	if _, err := exec.LookPath("colima"); err != nil {
		t.Skip("colima is not installed")
	}
	profiles, err := listProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) == 0 {
		t.Fatal("expected at least one colima profile")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not installed")
	}
	containers, err := listContainers(profiles[0].Name)
	if err != nil && !strings.Contains(err.Error(), "Cannot connect") {
		t.Fatal(err)
	}
	_ = containers
}

func TestMouseWheelScrollsLogs(t *testing.T) {
	m := model{width: 120, height: 24, logs: []string{"one", "two", "three", "four"}}
	updated, _ := m.Update(tea.MouseMsg(tea.MouseEvent{
		X: 80, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp,
	}))
	got := updated.(model)
	if got.logScroll != 3 || got.focus != 1 {
		t.Fatalf("wheel up = scroll %d, focus %d", got.logScroll, got.focus)
	}
	updated, _ = got.Update(tea.MouseMsg(tea.MouseEvent{
		X: 80, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown,
	}))
	if updated.(model).logScroll != 0 {
		t.Fatalf("wheel down = scroll %d", updated.(model).logScroll)
	}
}
