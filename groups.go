package main

import (
	"fmt"
	"sort"
)

type containerGroup struct {
	Name    string
	Indices []int
}

type listItem struct {
	group          string
	groupHeader    bool
	containerIndex int
}

func (m model) selectedContainer() *container {
	item := m.selectedItem()
	if item == nil || item.groupHeader {
		return nil
	}
	return &m.containers[item.containerIndex]
}

func (m model) selectedItem() *listItem {
	items := m.listItems()
	if m.containerIndex < 0 || m.containerIndex >= len(items) {
		return nil
	}
	return &items[m.containerIndex]
}

func (m model) selectedGroupName() string {
	item := m.selectedItem()
	if item == nil || !item.groupHeader {
		return ""
	}
	return item.group
}

func (m model) containerGroups() []containerGroup {
	byName := make(map[string][]int)
	for i, c := range m.containers {
		name := c.ComposeProject
		if name == "" {
			name = "standalone"
		}
		byName[name] = append(byName[name], i)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if names[i] == "standalone" {
			return false
		}
		if names[j] == "standalone" {
			return true
		}
		return names[i] < names[j]
	})
	groups := make([]containerGroup, 0, len(names))
	for _, name := range names {
		groups = append(groups, containerGroup{Name: name, Indices: byName[name]})
	}
	return groups
}

func (m model) listItems() []listItem {
	groups := m.containerGroups()
	items := make([]listItem, 0, len(m.containers)+len(groups))
	for _, group := range groups {
		if group.Name == "standalone" {
			for _, index := range group.Indices {
				items = append(items, listItem{group: group.Name, containerIndex: index})
			}
			continue
		}
		items = append(items, listItem{group: group.Name, groupHeader: true})
		if m.isExpanded(group.Name) {
			for _, index := range group.Indices {
				items = append(items, listItem{group: group.Name, containerIndex: index})
			}
		}
	}
	return items
}

func (m model) isExpanded(name string) bool {
	if m.expanded == nil {
		return true
	}
	expanded, ok := m.expanded[name]
	return !ok || expanded
}

func (m *model) syncExpanded() {
	if m.expanded == nil {
		m.expanded = make(map[string]bool)
	}
	for _, group := range m.containerGroups() {
		if _, ok := m.expanded[group.Name]; !ok {
			m.expanded[group.Name] = true
		}
	}
}

func (m model) findContainerItem(id string) int {
	if id == "" {
		return -1
	}
	for i, item := range m.listItems() {
		if !item.groupHeader && m.containers[item.containerIndex].ID == id {
			return i
		}
	}
	return -1
}

func (m model) findGroupItem(name string) int {
	for i, item := range m.listItems() {
		if item.groupHeader && item.group == name {
			return i
		}
	}
	return -1
}

func (m model) firstContainerItem() int {
	for i, item := range m.listItems() {
		if !item.groupHeader {
			return i
		}
	}
	return 0
}

func (m model) selectedGroup() *containerGroup {
	name := m.selectedGroupName()
	if name == "" {
		return nil
	}
	for _, group := range m.containerGroups() {
		if group.Name == name {
			return &group
		}
	}
	return nil
}

func (m model) selectedGroupByName(name string) containerGroup {
	for _, group := range m.containerGroups() {
		if group.Name == name {
			return group
		}
	}
	return containerGroup{Name: name}
}

func groupSummary(group containerGroup, containers []container) string {
	running := 0
	for _, index := range group.Indices {
		if isRunning(containers[index].State) {
			running++
		}
	}
	return fmt.Sprintf("%d/%d running", running, len(group.Indices))
}

func (m *model) toggleSelectedGroup() {
	name := m.selectedGroupName()
	if name == "" {
		return
	}
	m.syncExpanded()
	m.expanded[name] = !m.expanded[name]
	m.err = nil
	m.status = "ready"
}

func (m model) selectedID() string {
	if c := m.selectedContainer(); c != nil {
		return c.ID
	}
	return ""
}

func (m model) currentProfile() *profile {
	if m.profileIndex < 0 || m.profileIndex >= len(m.profiles) {
		return nil
	}
	return &m.profiles[m.profileIndex]
}

func (m model) currentProfileName() string {
	if p := m.currentProfile(); p != nil {
		return p.Name
	}
	return "default"
}
