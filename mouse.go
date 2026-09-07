package main

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) mouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.usageOverview || m.confirmCleanup || m.confirmDelete || m.actionMenu || m.width == 0 {
		return m, nil
	}
	event := tea.MouseEvent(msg)
	switch {
	case event.IsWheel():
		if event.Action != tea.MouseActionPress {
			return m, nil
		}
		return m.wheel(event)
	case event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress:
		return m.leftPress(event)
	case event.Action == tea.MouseActionMotion && m.logSelecting:
		m.extendLogSelection(event)
		return m, nil
	case event.Action == tea.MouseActionRelease && m.logSelecting:
		return m.finishLogSelection()
	}
	return m, nil
}

func (m model) wheel(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	if event.X < m.paneLayout().leftWidth {
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

func (m model) leftPress(event tea.MouseEvent) (tea.Model, tea.Cmd) {
	m.clearLogSelection()
	layout := m.paneLayout()
	if event.X < layout.leftWidth {
		m.focus = 0
		headerLines, start := m.containerListWindow(layout.bodyHeight)
		items := m.listItems()
		index := start + event.Y - paneContentTop - headerLines
		if event.Y < paneContentTop+headerLines || index < 0 || index >= len(items) {
			return m, nil
		}
		if index == m.containerIndex {
			if items[index].groupHeader {
				m.toggleSelectedGroup()
			}
			return m, nil
		}
		m.containerIndex = index
		return m, m.reloadSelectedLogs()
	}
	if event.Y < m.logContentTop(layout) {
		return m, nil
	}
	m.focus = 1
	rows, indices := m.logRowsIndexed(m.logRowCapacity(layout), max(1, layout.rightWidth-4))
	row := event.Y - m.logContentTop(layout) - m.logHeaderLines()
	if row < 0 || row >= len(rows) {
		return m, nil
	}
	m.pauseLogs()
	m.logSelecting, m.logSelActive, m.logSelDragged = true, true, false
	m.logSelStart, m.logSelEnd = indices[row], indices[row]
	return m, nil
}

func (m *model) extendLogSelection(event tea.MouseEvent) {
	layout := m.paneLayout()
	_, indices := m.logRowsIndexed(m.logRowCapacity(layout), max(1, layout.rightWidth-4))
	if len(indices) == 0 {
		return
	}
	row := event.Y - m.logContentTop(layout) - m.logHeaderLines()
	row = max(0, min(row, len(indices)-1))
	m.logSelEnd = indices[row]
	m.logSelDragged = true
}

// A plain click only focuses the pane; copying requires a drag so that
// clicking around never clobbers the clipboard.
func (m model) finishLogSelection() (tea.Model, tea.Cmd) {
	m.logSelecting = false
	if !m.logSelDragged {
		m.logSelActive = false
		return m, nil
	}
	lo, hi := m.logSelStart, m.logSelEnd
	if lo > hi {
		lo, hi = hi, lo
	}
	filtered := m.filteredLogs()
	if len(filtered) == 0 || lo >= len(filtered) {
		m.logSelActive = false
		return m, nil
	}
	hi = min(hi, len(filtered)-1)
	lines := m.logTextLines(filtered[lo : hi+1])
	return m.copyText(strings.Join(lines, "\n"), countLabel(len(lines), "log line"))
}

// logContentTop is the screen row of the log pane's first content line
// (heading), below the details pane and the log pane's own border.
func (m model) logContentTop(layout paneLayout) int {
	return layout.detailsHeight + 4
}

func (m model) logRowCapacity(layout paneLayout) int {
	return max(0, layout.logsHeight-m.logHeaderLines()-1)
}
