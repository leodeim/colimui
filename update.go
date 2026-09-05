package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	refreshInterval = 3 * time.Second
	spinnerInterval = 120 * time.Millisecond
)

func (m model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(m.refreshID, ""), m.nextTick(), checkForUpdateCmd(), statsTick())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case cleanupMsg:
		m.cleanupRunning = false
		if msg.profile != m.currentProfileName() {
			return m, nil
		}
		m.confirmCleanup = false
		if msg.err != nil {
			m.err, m.status = msg.err, "cleanup failed"
			return m, nil
		}
		m.storage = storageMsg{}
		m.storageRequested = time.Time{}
		m.status = "cleanup complete"
		return m, tea.Batch(m.queueRefresh(m.currentProfileName()), m.pollStorage())
	case statsTickMsg:
		return m, tea.Batch(m.pollStats(), m.pollStorage(), statsTick())
	case storageMsg:
		m.storageBusy = false
		if msg.profile == m.currentProfileName() {
			m.storage = msg
		}
		return m, nil
	case statsMsg:
		m.statsBusy = false
		if msg.aggregate {
			if msg.profile == m.currentProfileName() {
				m.overall = msg
			}
			return m, nil
		}
		c := m.selectedContainer()
		if c != nil && isRunning(c.State) && msg.id == c.ID && msg.profile == m.currentProfileName() {
			m.stats = msg
		}
		return m, nil

	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case refreshMsg:
		if msg.requestID != 0 {
			if msg.requestID < m.appliedRefreshID {
				return m, nil
			}
			if len(m.profiles) > 0 && msg.profileName != "" && msg.profileName != m.currentProfileName() {
				return m, nil
			}
			m.appliedRefreshID = msg.requestID
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
		if m.hasActiveActions() {
			// Keep the in-progress action visible while background refreshes run.
		} else if m.err != nil {
			m.status = "connection error"
		} else if oldID == m.selectedID() && previousStatus == "logs failed" && previousErr != nil {
			m.err, m.status = previousErr, previousStatus
		} else {
			m.status = "ready"
		}
		m.validateDeleteConfirmation()
		if oldID != m.selectedID() {
			m.stopLogs()
			m.logs, m.logPartial, m.logScroll, m.logFromStart, m.logBytes, m.logsTruncated, m.partialTrimmed = nil, "", 0, false, 0, false, false
			m.follow = false
			if m.selectedID() != "" {
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
		action, ok := m.activeActions[msg.requestID]
		if !ok {
			return m, nil
		}
		delete(m.activeActions, msg.requestID)
		if msg.err != nil {
			m.err, m.status = msg.err, action.label+" failed"
		} else {
			m.err, m.status = nil, action.label+" complete"
		}
		return m, m.queueRefresh(m.currentProfileName())
	case tickMsg:
		return m, tea.Batch(m.queueRefresh(m.currentProfileName()), m.nextTick())
	case spinnerTickMsg:
		if !m.hasActiveActions() {
			return m, nil
		}
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, spinnerTick()
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
			m.follow = false
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
	if m.usageOverview {
		return m, nil
	}
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
	m.pauseLogs()
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.logScroll = min(len(m.filteredLogs()), m.logScroll+3)
	case tea.MouseButtonWheelDown:
		m.logScroll = max(0, m.logScroll-3)
	}
	return m, nil
}

func (m model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		m.stopLogs()
		return m, tea.Quit
	}
	if m.confirmCleanup {
		switch key {
		case "y":
			m.confirmCleanup = false
			return m, m.cleanupCmd()
		case "enter":
			if m.cleanupChoice == 1 {
				m.confirmCleanup = false
				return m, m.cleanupCmd()
			}
			m.confirmCleanup = false
		case "up", "k":
			m.cleanupChoice = 0
		case "down", "j":
			m.cleanupChoice = 1
		case "n", "esc", "q", "c":
			m.confirmCleanup = false
		}
		return m, nil
	}
	if m.usageOverview {
		if key == "c" && !m.cleanupRunning {
			m.confirmCleanup, m.cleanupChoice = true, 0
			return m, nil
		}
		if key == "esc" || key == "q" || key == "u" || key == "?" {
			m.usageOverview = false
		}
		return m, nil
	}
	if m.logSearchEditing {
		return m.logSearchKey(msg)
	}
	if m.searchEditing {
		return m.searchKey(msg)
	}
	if m.actionMenu {
		return m.actionMenuKey(msg)
	}
	if m.confirmDelete {
		switch key {
		case "y":
			return m.dispatchDelete()
		case "enter":
			if m.deleteChoice == 1 {
				return m.dispatchDelete()
			}
			m.cancelDeleteConfirmation("ready")
		case "up", "k":
			m.deleteChoice = 0
		case "down", "j":
			m.deleteChoice = 1
		case "n", "esc", "q":
			m.cancelDeleteConfirmation("ready")
		}
		return m, nil
	}

	switch key {
	case "c":
		m.usageOverview, m.confirmCleanup, m.cleanupChoice = true, true, 0
		return m, tea.Batch(m.pollStats(), m.pollStorage())
	case "u":
		m.usageOverview = true
		return m, tea.Batch(m.pollStats(), m.pollStorage())
	case "L":
		m.logSearchEditing, m.logSearchBefore, m.focus = true, m.logQuery, 1
		m.pauseLogs()
	case "T":
		m.logTimestamps = !m.logTimestamps
	case "/":
		m.searchEditing, m.searchBefore = true, m.searchQuery
		m.focus = 0
	case "R":
		oldID := m.selectedID()
		m.runningOnly = !m.runningOnly
		return m, m.filterSelection(oldID)
	case "esc":
		oldID := m.selectedID()
		m.searchQuery, m.runningOnly = "", false
		return m, m.filterSelection(oldID)
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
			m.cancelDeleteConfirmation("ready")
			m.stopLogs()
			m.containers, m.logs, m.logPartial, m.logScroll, m.logFromStart, m.logBytes, m.logsTruncated, m.partialTrimmed = nil, nil, "", 0, false, 0, false, false
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
		if p := m.currentProfile(); !m.hasActiveProfileAction() && (p == nil || !isRunning(p.Status)) {
			name := m.currentProfileName()
			m.status = "starting " + name
			command := m.actionCmd(name, "start", "colima", "start", "--profile", name)
			return m, tea.Batch(command, spinnerTick())
		}
	case "x":
		if p := m.currentProfile(); !m.hasActiveProfileAction() && p != nil && isRunning(p.Status) {
			m.status = "stopping " + p.Name
			command := m.actionCmd(p.Name, "stop", "colima", "stop", "--profile", p.Name)
			return m, tea.Batch(command, spinnerTick())
		}
	case "t":
		if c := m.selectedContainer(); c != nil {
			if action, active := m.activeContainerAction(c.ID); active {
				m.status = actionProgressLabel(action.label) + " " + c.Name
				return m, nil
			}
			m.status = "restarting " + c.Name
			command := m.actionCmd(m.currentProfileName(), "restart", "docker", "restart", c.ID)
			return m, tea.Batch(command, spinnerTick())
		}
	case "enter":
		if item := m.selectedItem(); item != nil && item.groupHeader {
			m.toggleSelectedGroup()
		} else if c := m.selectedContainer(); c != nil {
			if action, active := m.activeContainerAction(c.ID); active {
				m.status = actionProgressLabel(action.label) + " " + c.Name
				return m, nil
			}
			verb := "stop"
			if c.State != "running" {
				verb = "start"
			}
			m.status = map[string]string{"start": "starting ", "stop": "stopping "}[verb] + c.Name
			command := m.actionCmd(m.currentProfileName(), verb, "docker", verb, c.ID)
			return m, tea.Batch(command, spinnerTick())
		}
	case "d":
		if c := m.selectedContainer(); c != nil {
			if action, active := m.activeContainerAction(c.ID); active {
				m.status = actionProgressLabel(action.label) + " " + c.Name
				return m, nil
			}
			if c.State == "running" {
				m.status = "stop the container before deleting it"
			} else {
				m.confirmDelete = true
				m.deleteProfile, m.deleteID = m.currentProfileName(), c.ID
				m.deleteChoice = 0
				m.status = "delete " + c.Name + "? y/n"
			}
		}
	case "l":
		return m, m.reloadSelectedLogs()
	case "f":
		if m.follow {
			m.pauseLogs()
			return m, nil
		}
		m.follow = true
		return m, m.reloadSelectedLogs()
	case "home":
		m.follow = false
		m.status = "loading all logs"
		return m, m.reloadSelectedLogs(true)
	case "pgup", "pgdown", "end":
		m.pauseLogs()
		m.scrollLogs(key)
	}
	return m, nil
}

func (m model) dispatchDelete() (tea.Model, tea.Cmd) {
	if c := m.deleteTarget(); c != nil && c.State != "running" {
		m.confirmDelete = false
		m.deleteProfile, m.deleteID, m.deleteChoice = "", "", 0
		m.status = "deleting " + c.Name
		return m, m.actionCmd(m.currentProfileName(), "delete", "docker", "rm", c.ID)
	}
	m.cancelDeleteConfirmation("delete canceled: container changed")
	return m, nil
}

func (m model) actionMenuItems() []actionMenuItem {
	profile := m.currentProfile()
	profileLabel, profileShortcut := "start colima", "s"
	if profile != nil && isRunning(profile.Status) {
		profileLabel, profileShortcut = "stop colima", "x"
	}

	container := m.selectedContainer()
	containerBusy := false
	if container != nil {
		_, containerBusy = m.activeContainerAction(container.ID)
	}
	containerName := "selected container"
	if container != nil {
		containerName = container.listName()
	}
	containerLabel := "start/stop selected container"
	if container != nil {
		if container.State == "running" {
			containerLabel = "stop " + containerName
		} else {
			containerLabel = "start " + containerName
		}
	}
	followLabel := "follow logs for " + containerName
	if m.follow {
		followLabel = "pause logs for " + containerName
	}

	return []actionMenuItem{
		{label: profileLabel, shortcut: profileShortcut, enabled: !m.hasActiveProfileAction()},
		{label: containerLabel, shortcut: "enter", enabled: container != nil && !containerBusy},
		{label: "restart " + containerName, shortcut: "t", enabled: container != nil && !containerBusy},
		{label: "delete " + containerName, shortcut: "d", enabled: container != nil && !containerBusy && container.State != "running"},
		{label: "reload logs for " + containerName, shortcut: "l", enabled: container != nil},
		{label: followLabel, shortcut: "f", enabled: container != nil},
		{label: "load all logs for " + containerName, shortcut: "home", enabled: container != nil},
		{label: "search log text", shortcut: "L", enabled: container != nil},
		{label: "toggle log timestamps", shortcut: "T", enabled: true},
		{label: "clean up reclaimable docker storage", shortcut: "c", enabled: !m.cleanupRunning},
		{label: "docker usage overview", shortcut: "u", enabled: true},
		{label: "refresh", shortcut: "r", enabled: true},
		{label: "search containers", shortcut: "/", enabled: true},
		{label: map[bool]string{false: "show running only", true: "show all states"}[m.runningOnly], shortcut: "R", enabled: true},
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

func (m *model) validateDeleteConfirmation() {
	if !m.confirmDelete || m.deleteTarget() != nil {
		return
	}
	m.cancelDeleteConfirmation("delete canceled: container changed")
}

func (m model) deleteTarget() *container {
	if !m.confirmDelete || m.deleteProfile != m.currentProfileName() {
		return nil
	}
	for index := range m.containers {
		if m.containers[index].ID == m.deleteID {
			return &m.containers[index]
		}
	}
	return nil
}

func (m *model) cancelDeleteConfirmation(status string) {
	m.confirmDelete = false
	m.deleteProfile, m.deleteID, m.deleteChoice = "", "", 0
	m.status = status
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

func spinnerTick() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg { return spinnerTickMsg(t) })
}

func (m model) hasActiveActions() bool {
	return len(m.activeActions) > 0
}

func (m model) activeContainerAction(id string) (activeAction, bool) {
	for _, action := range m.activeActions {
		if action.containerID == id {
			return action, true
		}
	}
	return activeAction{}, false
}

func (m model) hasActiveProfileAction() bool {
	for _, action := range m.activeActions {
		if action.containerID == "" {
			return true
		}
	}
	return false
}

func (m *model) actionCmd(profileName, label, command string, args ...string) tea.Cmd {
	m.nextActionID++
	requestID := m.nextActionID
	action := activeAction{label: label}
	if command == "docker" && len(args) > 1 {
		action.containerID = args[len(args)-1]
	}
	if m.activeActions == nil {
		m.activeActions = make(map[uint64]activeAction)
	}
	m.activeActions[requestID] = action
	backend := m.currentBackend()
	return func() tea.Msg {
		return actionMsg{requestID: requestID, label: label, err: backend.Action(profileName, command, args...)}
	}
}

// Preserve the selected identity when possible; never reuse a filtered row index.
func (m *model) filterSelection(oldID string) tea.Cmd {
	if index := m.findContainerItem(oldID); index >= 0 {
		m.containerIndex = index
	} else {
		m.containerIndex = m.firstContainerItem()
	}
	if oldID != m.selectedID() {
		return m.reloadSelectedLogs()
	}
	return nil
}

func (m model) searchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	oldID := m.selectedID()
	switch msg.Type {
	case tea.KeyEnter:
		m.searchEditing = false
	case tea.KeyEsc:
		m.searchQuery, m.searchEditing = m.searchBefore, false
	case tea.KeyBackspace, tea.KeyDelete:
		runes := []rune(m.searchQuery)
		if len(runes) > 0 {
			m.searchQuery = string(runes[:len(runes)-1])
		}
	case tea.KeyCtrlU:
		m.searchQuery = ""
	case tea.KeySpace:
		m.searchQuery += " "
	case tea.KeyRunes:
		if len([]rune(m.searchQuery))+len(msg.Runes) <= 256 {
			m.searchQuery += string(msg.Runes)
		}
	}
	return m, m.filterSelection(oldID)
}
