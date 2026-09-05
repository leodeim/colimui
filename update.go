package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const refreshInterval = 3 * time.Second

func (m model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(m.refreshID, ""), m.nextTick(), checkForUpdateCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case refreshMsg:
		if msg.requestID != 0 && msg.requestID < m.refreshID {
			return m, nil
		}
		if msg.requestID > m.refreshID {
			m.refreshID = msg.requestID
		}
		oldID := m.selectedID()
		oldGroup := m.selectedGroupName()
		activeProfileName := m.currentProfileName()
		previousErr, previousStatus := m.err, m.status
		m.profiles, m.containers, m.err = msg.profiles, msg.containers, msg.err
		m.syncExpanded()
		if index := findProfile(m.profiles, activeProfileName); index >= 0 {
			m.profileIndex = index
		} else if index := findProfile(m.profiles, msg.profileName); index >= 0 {
			m.profileIndex = index
		} else if m.profileIndex >= len(m.profiles) {
			m.profileIndex = max(0, len(m.profiles)-1)
		}
		if len(m.containers) == 0 {
			m.containerIndex = 0
		} else if index := m.findContainerItem(oldID); index >= 0 {
			m.containerIndex = index
		} else if index := m.findGroupItem(oldGroup); index >= 0 {
			m.containerIndex = index
		} else if oldID == "" && oldGroup == "" {
			m.containerIndex = m.firstContainerItem()
		} else if items := m.listItems(); m.containerIndex >= len(items) {
			m.containerIndex = len(items) - 1
		}
		if m.err != nil {
			m.status = "connection error"
		} else if oldID == m.selectedID() && previousStatus == "logs failed" && previousErr != nil {
			m.err, m.status = previousErr, previousStatus
		} else {
			m.status = "ready"
		}
		if oldID != m.selectedID() {
			m.stopLogs()
			m.logs, m.logPartial, m.logScroll, m.logFromStart = nil, "", 0, false
			if m.selectedID() != "" {
				m.follow = false
				var err error
				m.reader, err = m.currentBackend().OpenLogs(m.currentProfileName(), m.selectedID(), m.follow, false)
				if err != nil {
					m.err, m.status = err, "logs failed"
					return m, nil
				}
				return m, m.readLogsCmd()
			}
		}
	case actionMsg:
		if msg.requestID == 0 || msg.requestID != m.activeActionID {
			return m, nil
		}
		m.activeActionID = 0
		if msg.err != nil {
			m.err, m.status = msg.err, msg.label+" failed"
		} else {
			m.err, m.status = nil, msg.label+" complete"
		}
		return m, m.queueRefresh(m.currentProfileName())
	case tickMsg:
		return m, tea.Batch(m.queueRefresh(m.currentProfileName()), m.nextTick())
	case updateCheckMsg:
		m.updateVersion = msg.version
	case logsMsg:
		if msg.reader != m.reader {
			return m, nil
		}
		if len(msg.data) > 0 {
			m.appendLogs(string(msg.data))
		}
		if msg.err != nil && !errors.Is(msg.err, io.EOF) {
			m.err, m.status = msg.err, "logs failed"
		}
		if msg.done {
			partial := strings.TrimSpace(m.logPartial)
			m.finishLogs()
			if msg.err != nil && partial == "" && len(m.logs) > 0 {
				partial = strings.TrimSpace(m.logs[len(m.logs)-1])
			}
			if msg.err != nil && partial != "" {
				msg.err = fmt.Errorf("%w: %s", msg.err, partial)
			}
			if msg.err != nil && !errors.Is(msg.err, io.EOF) {
				m.err, m.status = msg.err, "logs failed"
			}
			if m.logFromStart {
				m.logScroll = len(m.logs)
				m.logFromStart = false
			}
			return m, nil
		}
		return m, m.readLogsCmd()
	}
	return m, nil
}

func (m model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	event := tea.MouseEvent(msg)
	if event.Action != tea.MouseActionPress || !event.IsWheel() {
		return m, nil
	}
	leftWidth := min(38, max(26, m.width/3))
	if event.X < leftWidth {
		itemCount := len(m.listItems())
		if event.Button == tea.MouseButtonWheelUp && m.containerIndex > 0 {
			m.containerIndex--
			return m, m.reloadSelectedLogs()
		}
		if event.Button == tea.MouseButtonWheelDown && m.containerIndex < itemCount-1 {
			m.containerIndex++
			return m, m.reloadSelectedLogs()
		}
		return m, nil
	}
	m.focus = 1
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.logScroll = min(len(m.logs), m.logScroll+3)
	case tea.MouseButtonWheelDown:
		m.logScroll = max(0, m.logScroll-3)
	}
	return m, nil
}

func (m model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.activeActionID != 0 {
		if key == "q" || key == "ctrl+c" {
			m.stopLogs()
			return m, tea.Quit
		}
		return m, nil
	}
	if m.actionMenu {
		return m.actionMenuKey(msg)
	}
	if m.confirmDelete {
		switch key {
		case "y", "enter":
			if c := m.selectedContainer(); c != nil {
				m.confirmDelete = false
				m.status = "deleting " + c.Name
				return m, m.actionCmd(m.currentProfileName(), "delete", "docker", "rm", c.ID)
			}
		case "n", "esc", "q":
			m.confirmDelete = false
			m.status = "ready"
		}
		return m, nil
	}

	switch key {
	case "?":
		m.actionMenu = true
		m.actionIndex = 0
	case "q", "ctrl+c":
		m.stopLogs()
		return m, tea.Quit
	case "tab", "left", "right":
		m.focus = 1 - m.focus
	case "[", "]":
		if len(m.profiles) > 1 {
			if key == "[" {
				m.profileIndex = (m.profileIndex + len(m.profiles) - 1) % len(m.profiles)
			} else {
				m.profileIndex = (m.profileIndex + 1) % len(m.profiles)
			}
			m.stopLogs()
			m.containers, m.logs, m.logPartial, m.logScroll, m.logFromStart = nil, nil, "", 0, false
			m.status = "switching to " + m.currentProfileName()
			return m, m.queueRefresh(m.currentProfileName())
		}
	case "up", "k":
		if m.focus == 0 && m.containerIndex > 0 {
			m.containerIndex--
			return m, m.reloadSelectedLogs()
		}
	case "down", "j":
		if m.focus == 0 && m.containerIndex < len(m.listItems())-1 {
			m.containerIndex++
			return m, m.reloadSelectedLogs()
		}
	case "r":
		m.status = "refreshing"
		return m, m.queueRefresh(m.currentProfileName())
	case "s":
		if p := m.currentProfile(); p == nil || !isRunning(p.Status) {
			name := m.currentProfileName()
			m.status = "starting " + name
			return m, m.actionCmd(name, "start", "colima", "start", "--profile", name)
		}
	case "x":
		if p := m.currentProfile(); p != nil && isRunning(p.Status) {
			m.status = "stopping " + p.Name
			return m, m.actionCmd(p.Name, "stop", "colima", "stop", "--profile", p.Name)
		}
	case "t":
		if c := m.selectedContainer(); c != nil {
			m.status = "restarting " + c.Name
			return m, m.actionCmd(m.currentProfileName(), "restart", "docker", "restart", c.ID)
		}
	case "enter":
		if item := m.selectedItem(); item != nil && item.groupHeader {
			m.toggleSelectedGroup()
		} else if c := m.selectedContainer(); c != nil {
			verb := "stop"
			if c.State != "running" {
				verb = "start"
			}
			m.status = map[string]string{"start": "starting ", "stop": "stopping "}[verb] + c.Name
			return m, m.actionCmd(m.currentProfileName(), verb, "docker", verb, c.ID)
		}
	case "d":
		if c := m.selectedContainer(); c != nil {
			if c.State == "running" {
				m.status = "stop the container before deleting it"
			} else {
				m.confirmDelete = true
				m.status = "delete " + c.Name + "? y/n"
			}
		}
	case "l":
		return m, m.reloadSelectedLogs()
	case "f":
		m.follow = !m.follow
		return m, m.reloadSelectedLogs()
	case "home":
		m.follow = false
		m.status = "loading all logs"
		return m, m.reloadSelectedLogs(true)
	case "pgup", "pgdown", "end":
		m.scrollLogs(key)
	}
	return m, nil
}

func (m model) actionMenuItems() []actionMenuItem {
	profile := m.currentProfile()
	profileLabel, profileShortcut := "start colima", "s"
	if profile != nil && isRunning(profile.Status) {
		profileLabel, profileShortcut = "stop colima", "x"
	}

	container := m.selectedContainer()
	containerLabel := "start/stop selected container"
	if container != nil {
		if container.State == "running" {
			containerLabel = "stop " + container.listName()
		} else {
			containerLabel = "start " + container.listName()
		}
	}

	return []actionMenuItem{
		{label: profileLabel, shortcut: profileShortcut, enabled: true},
		{label: containerLabel, shortcut: "enter", enabled: container != nil},
		{label: "restart selected container", shortcut: "t", enabled: container != nil},
		{label: "delete selected container", shortcut: "d", enabled: container != nil && container.State != "running"},
		{label: "reload selected logs", shortcut: "l", enabled: container != nil},
		{label: "toggle log following", shortcut: "f", enabled: container != nil},
		{label: "load all selected logs", shortcut: "home", enabled: container != nil},
		{label: "refresh", shortcut: "r", enabled: true},
	}
}

func (m model) actionMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	items := m.actionMenuItems()
	if len(items) == 0 {
		m.actionMenu = false
		return m, nil
	}
	if m.actionIndex >= len(items) {
		m.actionIndex = len(items) - 1
	}
	switch key {
	case "esc", "?", "q":
		m.actionMenu = false
		return m, nil
	case "up", "k":
		m.actionIndex = (m.actionIndex + len(items) - 1) % len(items)
		return m, nil
	case "down", "j":
		m.actionIndex = (m.actionIndex + 1) % len(items)
		return m, nil
	case "enter":
		item := items[m.actionIndex]
		if !item.enabled {
			return m, nil
		}
		m.actionMenu = false
		return m.key(shortcutKey(item.shortcut))
	}
	return m, nil
}

func shortcutKey(shortcut string) tea.KeyMsg {
	switch shortcut {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(shortcut)}
}

func (m *model) queueRefresh(profileName string) tea.Cmd {
	m.refreshID++
	return m.refreshCmd(m.refreshID, profileName)
}

func findProfile(profiles []profile, name string) int {
	for index, p := range profiles {
		if p.Name == name {
			return index
		}
	}
	return -1
}

func (m model) refreshCmd(requestID uint64, profileName string) tea.Cmd {
	backend := m.currentBackend()
	return func() tea.Msg {
		profiles, err := backend.Profiles()
		if err != nil {
			name := profileName
			if name == "" {
				name = "default"
			}
			return refreshMsg{profileName: name, requestID: requestID, err: err}
		}
		name := profileName
		if name == "" {
			name = "default"
			if len(profiles) > 0 {
				name = profiles[0].Name
			}
		}
		containers, dockerErr := backend.Containers(name)
		for _, p := range profiles {
			if p.Name == name && !isRunning(p.Status) {
				dockerErr = nil
				break
			}
		}
		return refreshMsg{profileName: name, requestID: requestID, profiles: profiles, containers: containers, err: dockerErr}
	}
}

func defaultTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) actionCmd(profileName, label, command string, args ...string) tea.Cmd {
	m.nextActionID++
	m.activeActionID = m.nextActionID
	requestID := m.activeActionID
	backend := m.currentBackend()
	return func() tea.Msg {
		return actionMsg{requestID: requestID, label: label, err: backend.Action(profileName, command, args...)}
	}
}
