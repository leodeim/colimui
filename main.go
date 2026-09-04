package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type profile struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Arch    string `json:"arch"`
	CPUs    int    `json:"cpus"`
	Memory  int64  `json:"memory"`
	Disk    int64  `json:"disk"`
	Runtime string `json:"runtime"`
}

type container struct {
	ID      string
	Name    string
	Image   string
	State   string
	Status  string
	Command string
	Ports   string
}

type refreshMsg struct {
	profiles   []profile
	containers []container
	err        error
}

type actionMsg struct {
	label string
	err   error
}

type tickMsg time.Time

type logsMsg struct {
	reader *logReader
	data   []byte
	done   bool
	err    error
}

type logReader struct {
	ctx    context.Context
	cancel context.CancelFunc
	out    io.ReadCloser
	cmd    *exec.Cmd
	stderr bytes.Buffer
}

var (
	accent = lipgloss.Color("86EFAC")
	muted  = lipgloss.Color("94A3B8")
	red    = lipgloss.Color("FCA5A5")
	yellow = lipgloss.Color("FDE68A")
	panel  = lipgloss.Color("334155")

	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	mutedStyle    = lipgloss.NewStyle().Foreground(muted)
	selectedStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	statusStyle   = lipgloss.NewStyle().Foreground(yellow)
	errorStyle    = lipgloss.NewStyle().Foreground(red)
)

type model struct {
	profiles       []profile
	profileIndex   int
	containers     []container
	containerIndex int
	focus          int
	width          int
	height         int
	status         string
	err            error
	confirmDelete  bool
	logs           []string
	logPartial     string
	logScroll      int
	follow         bool
	reader         *logReader
}

func initialModel() model {
	return model{focus: 0, status: "loading"}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(refreshCmd(), tickCmd())
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
		oldID := m.selectedID()
		m.profiles, m.containers, m.err = msg.profiles, msg.containers, msg.err
		if m.profileIndex >= len(m.profiles) {
			m.profileIndex = max(0, len(m.profiles)-1)
		}
		if len(m.containers) == 0 {
			m.containerIndex = 0
		} else if m.containerIndex >= len(m.containers) {
			m.containerIndex = len(m.containers) - 1
		}
		if m.err != nil {
			m.status = "connection error"
		} else {
			m.status = "ready"
		}
		if oldID != m.selectedID() {
			m.stopLogs()
			m.logs, m.logPartial, m.logScroll = nil, "", 0
			if m.selectedID() != "" {
				m.follow = false
				var err error
				m.reader, err = openLogs(m.currentProfileName(), m.selectedID(), m.follow)
				if err != nil {
					m.err, m.status = err, "logs failed"
					return m, nil
				}
				return m, m.readLogsCmd()
			}
		}
	case actionMsg:
		m.confirmDelete = false
		if msg.err != nil {
			m.err, m.status = msg.err, msg.label+" failed"
		} else {
			m.err, m.status = nil, msg.label+" complete"
		}
		return m, tea.Batch(refreshCmd(m.currentProfileName()), tickCmd())
	case tickMsg:
		return m, tea.Batch(refreshCmd(m.currentProfileName()), tickCmd())
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
			if m.follow && msg.err == nil {
				return m, m.readLogsCmd()
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
		if event.Button == tea.MouseButtonWheelUp && m.containerIndex > 0 {
			m.containerIndex--
			return m, m.reloadSelectedLogs()
		}
		if event.Button == tea.MouseButtonWheelDown && m.containerIndex < len(m.containers)-1 {
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
	if m.confirmDelete {
		switch key {
		case "y", "enter":
			if c := m.selectedContainer(); c != nil {
				m.status = "deleting " + c.Name
				return m, actionCmd(m.currentProfileName(), "delete", "docker", "rm", c.ID)
			}
		case "n", "esc", "q":
			m.confirmDelete = false
			m.status = "ready"
		}
		return m, nil
	}

	switch key {
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
			m.containers, m.logs, m.logPartial, m.logScroll = nil, nil, "", 0
			m.status = "switching to " + m.currentProfileName()
			return m, refreshCmd(m.currentProfileName())
		}
	case "up", "k":
		if m.focus == 0 && m.containerIndex > 0 {
			m.containerIndex--
			return m, m.reloadSelectedLogs()
		}
	case "down", "j":
		if m.focus == 0 && m.containerIndex < len(m.containers)-1 {
			m.containerIndex++
			return m, m.reloadSelectedLogs()
		}
	case "r":
		m.status = "refreshing"
		return m, refreshCmd(m.currentProfileName())
	case "s":
		if p := m.currentProfile(); p != nil && !isRunning(p.Status) {
			m.status = "starting " + p.Name
			return m, actionCmd(p.Name, "start", "colima", "start", "--profile", p.Name)
		}
	case "x":
		if p := m.currentProfile(); p != nil && isRunning(p.Status) {
			m.status = "stopping " + p.Name
			return m, actionCmd(p.Name, "stop", "colima", "stop", "--profile", p.Name)
		}
	case "t":
		if c := m.selectedContainer(); c != nil {
			m.status = "restarting " + c.Name
			return m, actionCmd(m.currentProfileName(), "restart", "docker", "restart", c.ID)
		}
	case "enter":
		if c := m.selectedContainer(); c != nil {
			verb := "stop"
			if c.State != "running" {
				verb = "start"
			}
			m.status = map[string]string{"start": "starting ", "stop": "stopping "}[verb] + c.Name
			return m, actionCmd(m.currentProfileName(), verb, "docker", verb, c.ID)
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
	case "pgup", "pgdown", "home", "end":
		m.scrollLogs(key)
	}
	return m, nil
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	p := m.currentProfile()
	header := titleStyle.Render("colimui") + "  " + mutedStyle.Render("profile "+m.currentProfileName())
	if p != nil {
		indicator := "○"
		if isRunning(p.Status) {
			indicator = "●"
		}
		header += "  " + statusStyle.Render(indicator+" "+strings.ToLower(p.Status))
		header += "  " + mutedStyle.Render(fmt.Sprintf("%d cpu · %s ram · %s", p.CPUs, humanBytes(p.Memory), humanBytes(p.Disk)))
	}

	bodyHeight := max(5, m.height-4)
	leftWidth := min(38, max(26, m.width/3))
	rightWidth := max(20, m.width-leftWidth-1)
	left := m.renderContainers(bodyHeight, leftWidth)
	right := m.renderDetails(bodyHeight, rightWidth)
	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	focusName := "containers"
	if m.focus == 1 {
		focusName = "details"
	}
	footer := mutedStyle.Render("focus: " + focusName + "  ↑↓/jk move  [] profile  tab focus  enter start/stop  t restart  d delete  l logs  f follow  r refresh  q quit")
	if m.confirmDelete {
		footer = statusStyle.Render(m.status)
	} else if m.err != nil {
		footer = errorStyle.Render(m.status + ": " + m.err.Error())
	} else if m.status != "ready" {
		footer = statusStyle.Render(m.status)
	}
	return header + "\n" + panes + "\n" + footer
}

func (m model) renderContainers(height, width int) string {
	heading := "containers"
	if m.focus == 0 {
		heading = selectedStyle.Render("▸ " + heading)
	}
	lines := []string{heading + "  " + mutedStyle.Render(strconv.Itoa(len(m.containers)))}
	if len(m.containers) == 0 {
		lines = append(lines, "", mutedStyle.Render("no containers"))
	} else {
		rowCount := max(1, height-1)
		start := max(0, m.containerIndex-rowCount+1)
		start = min(start, max(0, len(m.containers)-rowCount))
		end := min(len(m.containers), start+rowCount)
		rowWidth := max(8, width-4)
		nameWidth := min(18, max(8, rowWidth-15))
		statusWidth := max(4, rowWidth-nameWidth-5)
		for i := start; i < end; i++ {
			c := m.containers[i]
			marker := "○"
			if c.State == "running" {
				marker = "●"
			}
			line := fmt.Sprintf("%s %-*s %s", marker, nameWidth, truncate(c.Name, nameWidth), truncate(c.Status, statusWidth))
			if i == m.containerIndex {
				line = selectedStyle.Render("> " + line)
			} else {
				line = "  " + line
			}
			lines = append(lines, line)
		}
	}
	return m.renderPane(lines, width, height, m.focus == 0)
}

func (m model) renderDetails(height, width int) string {
	c := m.selectedContainer()
	heading := "details"
	if m.focus == 1 {
		heading = selectedStyle.Render("▸ " + heading)
	}
	lines := []string{heading}
	if c == nil {
		lines = append(lines, "", mutedStyle.Render("select a container"))
	} else {
		valueWidth := max(10, width-10)
		lines = append(lines, "", titleStyle.Render(truncate(c.Name, max(10, width-2))), "", "state   "+truncate(c.State, valueWidth), "status  "+truncate(c.Status, valueWidth), "image   "+truncate(c.Image, valueWidth), "id      "+truncate(c.ID, valueWidth), "command "+truncate(c.Command, valueWidth), "ports   "+truncate(c.Ports, valueWidth), "")
		lines = append(lines, "logs  "+map[bool]string{true: "following", false: "paused"}[m.follow])
		if len(m.logs) == 0 {
			lines = append(lines, mutedStyle.Render("no logs"))
		} else {
			for _, line := range m.visibleLogs(height - len(lines) - 1) {
				lines = append(lines, truncate(line, max(10, width-2)))
			}
		}
	}
	return m.renderPane(lines, width, height, m.focus == 1)
}

func (m model) renderPane(lines []string, width, height int, focused bool) string {
	border := panel
	if focused {
		border = accent
	}
	return lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(strings.Join(lines, "\n"))
}

func (m model) visibleLogs(count int) []string {
	if count <= 0 || len(m.logs) == 0 {
		return nil
	}
	end := len(m.logs) - m.logScroll
	if end < 0 {
		end = 0
	}
	start := max(0, end-count)
	return m.logs[start:end]
}

func (m *model) appendLogs(data string) {
	data = m.logPartial + data
	parts := strings.Split(data, "\n")
	m.logPartial = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		m.logs = append(m.logs, strings.TrimSuffix(line, "\r"))
	}
	if len(m.logs) > 1000 {
		m.logs = m.logs[len(m.logs)-1000:]
	}
	if m.logScroll == 0 {
		return
	}
	m.logScroll = min(m.logScroll, max(0, len(m.logs)-1))
}

func (m *model) scrollLogs(key string) {
	switch key {
	case "pgup":
		m.logScroll = min(len(m.logs), m.logScroll+10)
	case "pgdown":
		m.logScroll = max(0, m.logScroll-10)
	case "home":
		m.logScroll = len(m.logs)
	case "end":
		m.logScroll = 0
	}
}

func (m *model) reloadSelectedLogs() tea.Cmd {
	m.stopLogs()
	m.logs, m.logPartial, m.logScroll = nil, "", 0
	if m.selectedID() == "" {
		return nil
	}
	reader, err := openLogs(m.currentProfileName(), m.selectedID(), m.follow)
	if err != nil {
		m.err = err
		return nil
	}
	m.reader = reader
	return m.readLogsCmd()
}

func (m *model) stopLogs() {
	if m.reader != nil {
		m.reader.cancel()
		_ = m.reader.out.Close()
		m.reader = nil
	}
}

func (m model) readLogsCmd() tea.Cmd {
	r := m.reader
	if r == nil {
		return nil
	}
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := r.out.Read(buf)
		if n > 0 {
			return logsMsg{reader: r, data: buf[:n], err: err}
		}
		return logsMsg{reader: r, done: true, err: err}
	}
}

func (m model) selectedContainer() *container {
	if m.containerIndex < 0 || m.containerIndex >= len(m.containers) {
		return nil
	}
	return &m.containers[m.containerIndex]
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

func refreshCmd(profileName ...string) tea.Cmd {
	return func() tea.Msg {
		profiles, err := listProfiles()
		if err != nil {
			return refreshMsg{err: err}
		}
		name := "default"
		if len(profileName) > 0 && profileName[0] != "" {
			name = profileName[0]
		} else if len(profiles) > 0 {
			name = profiles[0].Name
		}
		containers, dockerErr := listContainers(name)
		for _, p := range profiles {
			if p.Name == name && !isRunning(p.Status) {
				dockerErr = nil
				break
			}
		}
		return refreshMsg{profiles: profiles, containers: containers, err: dockerErr}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func actionCmd(profileName, label, command string, args ...string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command(command, args...)
		if command == "docker" {
			cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profileName))
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			if len(output) > 0 {
				err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
			}
		}
		return actionMsg{label: label, err: err}
	}
}

func listProfiles() ([]profile, error) {
	output, err := exec.Command("colima", "list", "--json").Output()
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, err
	}
	var profiles []profile
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &profiles); err != nil {
			return nil, err
		}
	} else {
		var p profile
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		profiles = []profile{p}
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles, nil
}

func listContainers(profileName string) ([]container, error) {
	cmd := exec.Command("docker", "ps", "--all", "--format", "{{json .}}")
	cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profileName))
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var containers []container
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		var item struct {
			ID      string `json:"ID"`
			Names   string `json:"Names"`
			Image   string `json:"Image"`
			Command string `json:"Command"`
			State   string `json:"State"`
			Status  string `json:"Status"`
			Ports   string `json:"Ports"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			return nil, err
		}
		containers = append(containers, container{ID: item.ID, Name: item.Names, Image: item.Image, Command: item.Command, State: item.State, Status: item.Status, Ports: item.Ports})
	}
	sort.SliceStable(containers, func(i, j int) bool {
		if containers[i].State == containers[j].State {
			return containers[i].Name < containers[j].Name
		}
		return containers[i].State == "running"
	})
	return containers, scanner.Err()
}

func openLogs(profileName, id string, follow bool) (*logReader, error) {
	ctx, cancel := context.WithCancel(context.Background())
	args := []string{"logs", "--tail", "200"}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, id)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profileName))
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	reader := &logReader{ctx: ctx, cancel: cancel, out: out, cmd: cmd}
	cmd.Stderr = &reader.stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	return reader, nil
}

func dockerContext(profileName string) string {
	if profileName == "" || profileName == "default" {
		return "colima"
	}
	return "colima-" + profileName
}

func isRunning(status string) bool {
	return strings.EqualFold(status, "running")
}

func humanBytes(value int64) string {
	if value <= 0 {
		return "0b"
	}
	units := []string{"b", "k", "m", "g", "t"}
	amount := float64(value)
	i := 0
	for amount >= 1024 && i < len(units)-1 {
		amount /= 1024
		i++
	}
	if amount >= 10 || i == 0 {
		return fmt.Sprintf("%.0f%s", amount, units[i])
	}
	return fmt.Sprintf("%.1f%s", amount, units[i])
}

func truncate(value string, width int) string {
	value = strings.ReplaceAll(value, "\n", " ")
	if width < 4 || len(value) <= width {
		return value
	}
	return value[:width-3] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	if _, err := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
