package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const statsInterval = 3 * time.Second
const statsTimeout = 4 * time.Second

type containerStats struct {
	ID      string `json:"ID"`
	CPU     string `json:"CPUPerc"`
	Memory  string `json:"MemUsage"`
	Network string `json:"NetIO"`
}

type statsBackend interface {
	Stats(string, string) (containerStats, error)
}
type statsTickMsg struct{}
type statsMsg struct {
	profile, id string
	all         []containerStats
	aggregate   bool
	value       containerStats
	err         error
	at          time.Time
}

func statsTick() tea.Cmd {
	return tea.Tick(statsInterval, func(time.Time) tea.Msg { return statsTickMsg{} })
}

func (execBackend) Stats(profile, id string) (containerStats, error) {
	ctx, cancel := context.WithTimeout(context.Background(), statsTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "stats", "--no-stream", "--format", "{{json .}}", id)
	cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profile))
	output, err := cmd.Output()
	if ctx.Err() != nil {
		return containerStats{}, fmt.Errorf("stats timed out")
	}
	if err != nil {
		return containerStats{}, fmt.Errorf("stats unavailable: %w", err)
	}
	return parseStats(output)
}

func parseStats(output []byte) (containerStats, error) {
	var value containerStats
	if err := json.Unmarshal(output, &value); err != nil {
		return value, fmt.Errorf("invalid stats response: %w", err)
	}
	if strings.TrimSpace(value.CPU) == "" || strings.TrimSpace(value.Memory) == "" || strings.TrimSpace(value.Network) == "" {
		return value, fmt.Errorf("incomplete stats response")
	}
	return value, nil
}

func (m *model) pollStats() tea.Cmd {
	if m.statsBusy {
		return nil
	}
	if backend, ok := m.currentBackend().(overviewBackend); ok {
		p := m.currentProfile()
		if p == nil || !isRunning(p.Status) {
			return nil
		}
		profile := m.currentProfileName()
		m.statsBusy = true
		return func() tea.Msg {
			values, err := backend.AllStats(profile)
			return statsMsg{profile: profile, all: values, aggregate: true, err: err, at: time.Now()}
		}
	}
	c := m.selectedContainer()
	if c == nil || !isRunning(c.State) {
		return nil
	}
	backend, ok := m.currentBackend().(statsBackend)
	if !ok {
		return nil
	}
	profile, id := m.currentProfileName(), c.ID
	m.statsBusy = true
	return func() tea.Msg {
		value, err := backend.Stats(profile, id)
		return statsMsg{profile: profile, id: id, value: value, err: err, at: time.Now()}
	}
}

func (m model) resourceLines(width int) []string {
	c := m.selectedContainer()
	if c == nil {
		return nil
	}
	if !isRunning(c.State) {
		return []string{"usage   stopped"}
	}
	if m.overall.profile == m.currentProfileName() && m.overall.aggregate {
		m.stats = statsMsg{profile: m.overall.profile, id: c.ID, err: m.overall.err, at: m.overall.at}
		found := false
		for _, value := range m.overall.all {
			if value.ID == c.ID {
				m.stats.value = value
				found = true
				break
			}
		}
		if !found && m.stats.err == nil {
			return []string{"usage   waiting for sample…"}
		}
	}
	if m.stats.id != c.ID || m.stats.profile != m.currentProfileName() {
		return []string{"usage   loading…"}
	}
	if m.stats.err != nil {
		return []string{truncate("usage   "+m.stats.err.Error(), width)}
	}
	if time.Since(m.stats.at) > 3*statsInterval {
		return []string{"usage   stale; waiting for update"}
	}
	return []string{truncate("cpu     "+m.stats.value.CPU, width), truncate("memory  "+m.stats.value.Memory, width), truncate("net I/O "+m.stats.value.Network, width)}
}
