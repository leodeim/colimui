package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
)

// clipboardCopy is a variable so tests can capture copied text.
var clipboardCopy = copyToClipboard

func copyToClipboard(text string) error {
	if path, err := exec.LookPath("pbcopy"); err == nil {
		cmd := exec.Command(path)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	// OSC52 fallback for hosts without pbcopy; stderr shares the tty but
	// bypasses the renderer's stdout buffering.
	_, err := osc52.New(text).WriteTo(os.Stderr)
	return err
}

func (m model) copyText(text, label string) (tea.Model, tea.Cmd) {
	if err := clipboardCopy(text); err != nil {
		m.err, m.status = err, "copy failed"
		return m, nil
	}
	m.err, m.status = nil, "copied "+label
	return m, nil
}

func (m model) copySelectedDetails() (tea.Model, tea.Cmd) {
	c := m.selectedContainer()
	if c == nil {
		m.status = "select a container to copy its details"
		return m, nil
	}
	return m.copyText(containerDetailsText(*c), "details for "+c.listName())
}

func (m model) copyFilteredLogs() (tea.Model, tea.Cmd) {
	lines := m.logTextLines(m.filteredLogs())
	if len(lines) == 0 {
		m.status = "no logs to copy"
		return m, nil
	}
	return m.copyText(strings.Join(lines, "\n"), countLabel(len(lines), "log line"))
}

func (m model) logTextLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = m.logText(line)
	}
	return out
}

func containerDetailsText(c container) string {
	pairs := [][2]string{
		{"name", c.Name},
		{"state", c.State},
		{"status", c.Status},
		{"image", c.Image},
		{"id", c.ID},
		{"command", c.Command},
		{"ports", c.Ports},
		{"compose project", c.ComposeProject},
		{"compose service", c.ComposeService},
	}
	var b strings.Builder
	for _, pair := range pairs {
		if pair[1] == "" {
			continue
		}
		b.WriteString(pair[0] + ": " + pair[1] + "\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func countLabel(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}
