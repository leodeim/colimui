package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg(tea.MouseEvent{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

func TestClickSelectsContainerAndReloadsLogs(t *testing.T) {
	backend := &fakeBackend{}
	m := newModel(backend, func() tea.Cmd { return nil })
	m.width, m.height = 120, 24
	m.focus = 1
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{
		{ID: "one", Name: "one", State: "running"},
		{ID: "two", Name: "two", State: "running"},
	}
	updated, _ := m.Update(leftClick(2, 4))
	got := updated.(model)
	if got.containerIndex != 1 || got.focus != 0 || backend.logID != "two" {
		t.Fatalf("click selection = index %d focus %d logs for %q", got.containerIndex, got.focus, backend.logID)
	}
	if updated, _ = got.Update(leftClick(2, 12)); updated.(model).containerIndex != 1 {
		t.Fatal("click below the list moved the selection")
	}
}

func TestClickOnSelectedGroupHeaderToggles(t *testing.T) {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.width, m.height = 120, 24
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", ComposeProject: "app", ComposeService: "web", State: "running"}}
	updated, _ := m.Update(leftClick(2, 3))
	got := updated.(model)
	if got.isExpanded("app") {
		t.Fatal("click on the selected group header did not collapse it")
	}
	updated, _ = got.Update(leftClick(2, 3))
	if !updated.(model).isExpanded("app") {
		t.Fatal("second click did not expand the group again")
	}
}

func TestLogDragSelectionCopiesLines(t *testing.T) {
	copied := stubClipboard(t)
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.width, m.height = 120, 24
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "running"}}
	m.follow = true
	m.logs = []string{
		"2024-01-01T00:00:00Z alpha",
		"2024-01-01T00:00:01Z beta",
		"2024-01-01T00:00:02Z gamma",
	}
	updated, _ := m.Update(leftClick(60, 15))
	got := updated.(model)
	if !got.logSelecting || got.follow || got.focus != 1 {
		t.Fatalf("press state = selecting %t follow %t focus %d", got.logSelecting, got.follow, got.focus)
	}
	updated, _ = got.Update(tea.MouseMsg(tea.MouseEvent{X: 60, Y: 16, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}))
	updated, _ = updated.(model).Update(tea.MouseMsg(tea.MouseEvent{X: 60, Y: 16, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}))
	got = updated.(model)
	if *copied != "alpha\nbeta" || got.status != "copied 2 log lines" || got.logSelecting {
		t.Fatalf("drag copy = %q status %q", *copied, got.status)
	}
	if !got.logSelActive {
		t.Fatal("copied selection lost its highlight")
	}
}

func TestPlainLogClickDoesNotCopy(t *testing.T) {
	copied := stubClipboard(t)
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.width, m.height = 120, 24
	m.profiles = []profile{{Name: "default", Status: "Running"}}
	m.containers = []container{{ID: "one", Name: "one", State: "running"}}
	m.logs = []string{"2024-01-01T00:00:00Z alpha"}
	updated, _ := m.Update(leftClick(60, 15))
	updated, _ = updated.(model).Update(tea.MouseMsg(tea.MouseEvent{X: 60, Y: 15, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}))
	got := updated.(model)
	if *copied != "" || got.logSelActive || got.focus != 1 {
		t.Fatalf("plain click = copied %q selection %t focus %d", *copied, got.logSelActive, got.focus)
	}
}
