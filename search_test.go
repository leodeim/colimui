package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func searchModel() model {
	m := newModel(&fakeBackend{}, func() tea.Cmd { return nil })
	m.containers = []container{
		{ID: "a", Name: "Api-One", Image: "nginx:alpine", ComposeProject: "Shop", ComposeService: "web", State: "running"},
		{ID: "b", Name: "database", Image: "postgres", ComposeProject: "Shop", State: "exited"},
		{ID: "c", Name: "worker", Image: "redis", State: "running"},
	}
	m.containerIndex = m.firstContainerItem()
	return m
}

func TestSearchFieldsAndRunningIntersection(t *testing.T) {
	for _, tc := range []struct {
		query   string
		running bool
		count   int
	}{
		{"API-ONE", false, 1}, {"alpine", false, 1}, {"shop", false, 2}, {"exited", false, 1}, {"web", false, 1},
		{"shop", true, 1}, {"exited", true, 0}, {"", true, 2}, {"   ", false, 3}, {"absent", false, 0},
	} {
		m := searchModel()
		m.searchQuery, m.runningOnly = tc.query, tc.running
		if got := m.matchingCount(); got != tc.count {
			t.Errorf("%q running=%v: got %d want %d", tc.query, tc.running, got, tc.count)
		}
	}
}

func TestSearchEditingDoesNotDispatchShortcuts(t *testing.T) {
	m := searchModel()
	u, _ := m.key(shortcutKey("/"))
	m = u.(model)
	u, cmd := m.key(shortcutKey("d"))
	m = u.(model)
	if cmd != nil || m.confirmDelete || m.searchQuery != "d" {
		t.Fatal("search dispatched shortcut")
	}
	u, _ = m.key(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.searchQuery != "" || m.searchEditing {
		t.Fatal("cancel did not restore query")
	}
	u, _ = m.key(shortcutKey("/"))
	m = u.(model)
	u, _ = m.key(shortcutKey("数据库"))
	m = u.(model)
	u, _ = m.key(tea.KeyMsg{Type: tea.KeyBackspace})
	m = u.(model)
	if m.searchQuery != "数据" {
		t.Fatalf("unicode backspace: %q", m.searchQuery)
	}
	u, _ = m.key(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if m.searchEditing || m.searchQuery != "数据" {
		t.Fatal("enter did not apply")
	}
}

func TestFilterSelectionAndRefresh(t *testing.T) {
	m := searchModel()
	m.containerIndex = m.findContainerItem("b")
	u, _ := m.key(shortcutKey("R"))
	m = u.(model)
	if m.selectedID() != "a" {
		t.Fatalf("selected %s", m.selectedID())
	}
	m.searchQuery = "redis"
	m.filterSelection("a")
	if m.selectedID() != "c" {
		t.Fatal("wrong filtered identity")
	}
	u, _ = m.Update(refreshMsg{containers: m.containers})
	m = u.(model)
	if m.selectedID() != "c" || !m.runningOnly || m.searchQuery != "redis" {
		t.Fatal("refresh lost filters/selection")
	}
	m.searchQuery = "absent"
	m.filterSelection("c")
	if m.selectedContainer() != nil || len(m.listItems()) != 0 {
		t.Fatal("empty results still selectable")
	}
	if !strings.Contains(m.renderContainers(20, 38), "no matches") {
		t.Fatal("missing empty state")
	}
	u, cmd := m.key(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(model)
	if cmd != nil {
		t.Fatal("empty list dispatched action")
	}
	u, _ = m.key(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.matchingCount() != 3 || m.selectedID() == "" {
		t.Fatal("clear did not restore results")
	}
}

func TestSearchRevealsCollapsedGroups(t *testing.T) {
	m := searchModel()
	m.expanded["Shop"] = false
	m.searchQuery = "nginx"
	if m.findContainerItem("a") < 0 {
		t.Fatal("matching service hidden")
	}
	if m.expanded["Shop"] {
		t.Fatal("search changed saved expansion")
	}
}
