package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	accent = lipgloss.Color("#86EFAC")
	muted  = lipgloss.Color("#94A3B8")
	red    = lipgloss.Color("#FCA5A5")
	yellow = lipgloss.Color("#FDE68A")
	panel  = lipgloss.Color("#334155")

	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(accent)
	mutedStyle       = lipgloss.NewStyle().Foreground(muted)
	selectedStyle    = lipgloss.NewStyle().Foreground(accent).Bold(true)
	selectedRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Background(lipgloss.Color("#14532D")).Bold(true)
	runningStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399"))
	stoppedStyle     = lipgloss.NewStyle().Foreground(muted)
	logHeadingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7DD3FC")).Bold(true)
	statusStyle      = lipgloss.NewStyle().Foreground(yellow)
	errorStyle       = lipgloss.NewStyle().Foreground(red)
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < 60 || m.height < 16 {
		return truncate("terminal too small — resize to 60×16", m.width)
	}
	dashboard := m.renderDashboard()
	if m.actionMenu {
		return overlay(m.width, m.height, dashboard, m.renderActionMenu())
	}
	return dashboard
}

func (m model) renderDashboard() string {
	p := m.currentProfile()
	header := titleStyle.Render("colimui") + "  " + mutedStyle.Render("profile "+sanitizeText(m.currentProfileName()))
	if p != nil {
		indicator := "○"
		if isRunning(p.Status) {
			indicator = "●"
		}
		profileStatus := statusStyle
		if isRunning(p.Status) {
			profileStatus = runningStyle
		}
		header += "  " + profileStatus.Render(indicator+" "+strings.ToLower(sanitizeText(p.Status)))
		header += "  " + mutedStyle.Render(fmt.Sprintf("%d cpu · %s ram · %s", p.CPUs, humanBytes(p.Memory), humanBytes(p.Disk)))
	}

	bodyHeight := max(5, m.height-4)
	leftWidth := min(38, max(26, m.width/3))
	rightWidth := max(20, m.width-leftWidth-1)
	left := m.renderContainers(bodyHeight, leftWidth)
	detailsHeight := min(10, max(2, bodyHeight/2))
	logsHeight := max(1, bodyHeight-detailsHeight-2)
	details := m.renderDetails(detailsHeight, rightWidth)
	logs := m.renderLogs(logsHeight, rightWidth)
	right := lipgloss.JoinVertical(lipgloss.Left, details, logs)
	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	focusName := "containers"
	if m.focus == 1 {
		focusName = "logs"
	}
	footer := mutedStyle.Render("focus: " + focusName + "  ↑↓/jk move  enter start/stop  f follow logs  ? actions  q quit")
	if m.confirmDelete {
		footer = statusStyle.Render(sanitizeText(m.status))
	} else if m.err != nil {
		footer = errorStyle.Render(sanitizeText(m.status) + ": " + sanitizeText(m.err.Error()))
	} else if m.status != "ready" {
		footer = statusStyle.Render(sanitizeText(m.status))
	} else if m.updateVersion != "" {
		footer = statusStyle.Render("update available: " + sanitizeText(m.updateVersion) + " — run colimui update")
	}
	return ansi.Truncate(header, m.width, "") + "\n" + panes + "\n" + ansi.Truncate(footer, m.width, "")
}

func (m model) renderActionMenu() string {
	items := m.actionMenuItems()
	lines := []string{titleStyle.Render("actions"), mutedStyle.Render("select an action and press enter"), ""}
	for index, item := range items {
		label := item.label
		shortcut := "[" + item.shortcut + "]"
		if !item.enabled {
			lines = append(lines, mutedStyle.Render("  "+label+"  "+shortcut))
			continue
		}
		if index == m.actionIndex {
			lines = append(lines, selectedRowStyle.Render("> "+label+"  "+shortcut))
		} else {
			lines = append(lines, "  "+label+"  "+mutedStyle.Render(shortcut))
		}
	}
	lines = append(lines, "", logHeadingStyle.Render("keyboard shortcuts"), mutedStyle.Render("↑↓/j k select  enter run  esc/? close"), mutedStyle.Render("[] profile  tab focus  end latest logs  q quit"))
	popupWidth := min(64, max(38, m.width-4))
	return lipgloss.NewStyle().Width(popupWidth).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Background(lipgloss.Color("#1E293B")).Render(strings.Join(lines, "\n"))
}

func overlay(width, height int, background, foreground string) string {
	backgroundLines := strings.Split(background, "\n")
	foregroundLines := strings.Split(foreground, "\n")
	foregroundWidth := lipgloss.Width(foreground)
	x := max(0, (width-foregroundWidth)/2)
	y := max(0, (height-len(foregroundLines))/2)
	lines := make([]string, height)

	for row := 0; row < height; row++ {
		line := ""
		if row < len(backgroundLines) {
			line = ansi.Truncate(backgroundLines[row], width, "")
		}
		line += strings.Repeat(" ", max(0, width-lipgloss.Width(line)))
		if row >= y && row < y+len(foregroundLines) {
			popupLine := ansi.Truncate(foregroundLines[row-y], foregroundWidth, "")
			popupLine += strings.Repeat(" ", max(0, foregroundWidth-lipgloss.Width(popupLine)))
			line = ansi.Cut(line, 0, x) + popupLine + ansi.Cut(line, x+foregroundWidth, width)
		}
		lines[row] = line
	}
	return strings.Join(lines, "\n")
}

func (m model) renderContainers(height, width int) string {
	heading := "containers"
	if m.focus == 0 {
		heading = selectedStyle.Render("▸ " + heading)
	}
	lines := []string{heading + "  " + mutedStyle.Render(strconv.Itoa(len(m.containers)))}
	items := m.listItems()
	if len(items) == 0 {
		lines = append(lines, "")
		if p := m.currentProfile(); p != nil && !isRunning(p.Status) {
			lines = append(lines, mutedStyle.Render("colima is stopped"), statusStyle.Render("press s to start"))
		} else {
			lines = append(lines, mutedStyle.Render("no containers"))
		}
	} else {
		rowCount := max(1, height-1)
		start := max(0, m.containerIndex-rowCount+1)
		start = min(start, max(0, len(items)-rowCount))
		end := min(len(items), start+rowCount)
		rowWidth := max(8, width-4)
		for i := start; i < end; i++ {
			item := items[i]
			if item.groupHeader {
				group := m.selectedGroupByName(item.group)
				marker := "▶"
				if m.isExpanded(item.group) {
					marker = "▼"
				}
				summary := groupSummary(group, m.containers)
				summaryWidth := min(len(summary), max(4, rowWidth-9))
				nameWidth := min(18, max(4, rowWidth-summaryWidth-5))
				summary = truncate(summary, summaryWidth)
				plain := fmt.Sprintf("%s %-*s %s", marker, nameWidth, middleTruncate(group.Name, nameWidth), summary)
				if i == m.containerIndex {
					lines = append(lines, selectedRowStyle.Render("> "+plain))
				} else {
					lines = append(lines, "  "+titleStyle.Render(marker)+" "+middleTruncate(group.Name, nameWidth)+" "+mutedStyle.Render(summary))
				}
				continue
			}
			c := m.containers[item.containerIndex]
			indent := ""
			if item.group != "standalone" {
				indent = "  "
			}
			nameWidth := min(18, max(10, rowWidth-11-len(indent)))
			statusWidth := max(4, rowWidth-nameWidth-5-len(indent))
			marker := "○"
			if c.State == "running" {
				marker = "●"
			}
			containerStatus := statusLabel(c.Status, statusWidth)
			action, isActing := m.activeContainerAction(c.ID)
			if isActing {
				marker = spinnerFrames[m.spinnerFrame]
				containerStatus = actionProgressLabel(action.label)
			}
			name := middleTruncate(c.listName(), nameWidth)
			plain := fmt.Sprintf("%s%s %-*s %s", indent, marker, nameWidth, name, containerStatus)
			if i == m.containerIndex {
				lines = append(lines, selectedRowStyle.Render("> "+plain))
			} else {
				markerStyle := stoppedStyle
				if c.State == "running" {
					markerStyle = runningStyle
				}
				if isActing {
					markerStyle = statusStyle
				}
				body := fmt.Sprintf("%s%s %-*s %s", indent, markerStyle.Render(marker), nameWidth, name, markerStyle.Render(containerStatus))
				lines = append(lines, "  "+body)
			}
		}
	}
	return m.renderPane(lines, width, height, m.focus == 0)
}

func actionProgressLabel(action string) string {
	if action == "stop" {
		return "stopping…"
	}
	if action == "start" {
		return "starting…"
	}
	if action == "restart" {
		return "restarting…"
	}
	if action == "delete" {
		return "deleting…"
	}
	return action + "…"
}

func (m model) renderDetails(height, width int) string {
	c := m.selectedContainer()
	lines := []string{"details"}
	if c == nil {
		if group := m.selectedGroup(); group != nil {
			lines = append(lines, "", titleStyle.Render(sanitizeText(group.Name)), "", "services "+strconv.Itoa(len(group.Indices)), "status   "+groupSummary(*group, m.containers))
		} else {
			lines = append(lines, "", mutedStyle.Render("select a container"))
		}
	} else {
		valueWidth := max(10, width-10)
		lines = append(lines, "", titleStyle.Render(truncate(c.Name, max(10, width-2))), "", "state   "+truncate(c.State, valueWidth), "status  "+truncate(c.Status, valueWidth), "image   "+truncate(c.Image, valueWidth), "id      "+truncate(c.ID, valueWidth), "command "+truncate(c.Command, valueWidth), "ports   "+truncate(c.Ports, valueWidth))
	}
	return m.renderPane(lines, width, height, false)
}

func (m model) renderLogs(height, width int) string {
	heading := "logs"
	if m.focus == 1 {
		heading = selectedStyle.Render("▸ " + heading)
	}
	lines := []string{heading + "  " + map[bool]string{true: "following", false: "paused"}[m.follow]}
	if m.selectedContainer() == nil {
		lines = append(lines, "", mutedStyle.Render("select a service"))
	} else if len(m.logs) == 0 {
		lines = append(lines, mutedStyle.Render("no logs"))
	} else {
		if m.logsTruncated {
			lines = append(lines, mutedStyle.Render("older logs truncated"))
		}
		for _, line := range m.visibleLogs(max(0, height-len(lines)-1)) {
			lines = append(lines, truncate(line, max(10, width-2)))
		}
	}
	return m.renderPane(lines, width, height, m.focus == 1)
}

func (m model) renderPane(lines []string, width, height int, focused bool) string {
	border := panel
	if focused {
		border = accent
	}
	return lipgloss.NewStyle().Width(max(1, width-2)).Height(height).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(border).Render(strings.Join(lines, "\n"))
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
	value = sanitizeText(value)
	if width < 4 || ansi.StringWidth(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "...")
}

func middleTruncate(value string, width int) string {
	value = sanitizeText(value)
	if width < 4 || ansi.StringWidth(value) <= width {
		return value
	}
	left := (width - 3 + 1) / 2
	right := width - 3 - left
	valueWidth := ansi.StringWidth(value)
	return ansi.Truncate(value, left, "") + "..." + ansi.Cut(value, max(0, valueWidth-right), valueWidth)
}

func statusLabel(value string, width int) string {
	value = sanitizeText(value)
	fields := strings.Fields(value)
	if width >= 4 && len(fields) > 0 && len(fields[0]) <= width {
		return fields[0]
	}
	return truncate(value, width)
}

func sanitizeText(value string) string {
	var sanitized strings.Builder
	sanitized.Grow(len(value))
	for i := 0; i < len(value); {
		switch value[i] {
		case 0x1b:
			i = skipEscapeSequence(value, i)
		case 0x90, 0x98, 0x9d, 0x9e, 0x9f:
			i = skipStringControl(value, i+1)
		case 0x9b:
			i = skipCSI(value, i+1)
		case 0x9c:
			i++
		default:
			r, size := utf8.DecodeRuneInString(value[i:])
			if unicode.IsControl(r) {
				sanitized.WriteByte(' ')
			} else {
				sanitized.WriteRune(r)
			}
			i += size
		}
	}
	return sanitized.String()
}

func skipEscapeSequence(value string, start int) int {
	i := start + 1
	if i >= len(value) {
		return i
	}
	switch value[i] {
	case '[':
		return skipCSI(value, i+1)
	case ']', 'P', '^', '_', 'X':
		return skipStringControl(value, i+1)
	}
	for i < len(value) && value[i] >= 0x20 && value[i] <= 0x2f {
		i++
	}
	if i < len(value) && value[i] >= 0x30 && value[i] <= 0x7e {
		return i + 1
	}
	return i
}

func skipCSI(value string, start int) int {
	for i := start; i < len(value); i++ {
		if value[i] >= 0x40 && value[i] <= 0x7e {
			return i + 1
		}
	}
	return len(value)
}

func skipStringControl(value string, start int) int {
	for i := start; i < len(value); i++ {
		switch value[i] {
		case 0x07, 0x9c:
			return i + 1
		case 0x1b:
			if i+1 < len(value) && value[i+1] == '\\' {
				return i + 2
			}
		}
	}
	return len(value)
}
