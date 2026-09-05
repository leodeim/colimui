package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type overviewBackend interface {
	AllStats(string) ([]containerStats, error)
	Storage(string) ([]storageRow, error)
}
type storageRow struct{ Type, Size, Reclaimable string }
type storageMsg struct {
	profile string
	rows    []storageRow
	err     error
	at      time.Time
}

func dockerUsageOutput(profile string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_CONTEXT="+dockerContext(profile))
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("request timed out")
	}
	return out, err
}

func (execBackend) AllStats(profile string) ([]containerStats, error) {
	out, err := dockerUsageOutput(profile, statsTimeout, "stats", "--no-stream", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	return parseAllStats(out)
}
func parseAllStats(out []byte) ([]containerStats, error) {
	var values []containerStats
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		v, err := parseStats(line)
		if err != nil {
			return nil, err
		}
		if v.ID == "" {
			return nil, fmt.Errorf("stats missing container ID")
		}
		values = append(values, v)
	}
	return values, nil
}
func (execBackend) Storage(profile string) ([]storageRow, error) {
	out, err := dockerUsageOutput(profile, 10*time.Second, "system", "df", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	return parseStorage(out)
}
func parseStorage(out []byte) ([]storageRow, error) {
	var rows []storageRow
	for _, line := range bytes.Split(bytes.TrimSpace(out), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var row storageRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, err
		}
		if row.Type == "" || row.Size == "" {
			return nil, fmt.Errorf("incomplete storage response")
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("empty storage response")
	}
	return rows, nil
}
func (m *model) pollStorage() tea.Cmd {
	if !m.usageOverview || m.storageBusy {
		return nil
	}
	p := m.currentProfile()
	if p == nil || !isRunning(p.Status) {
		return nil
	}
	if m.storage.profile == p.Name && time.Since(m.storageRequested) < 30*time.Second {
		return nil
	}
	backend, ok := m.currentBackend().(overviewBackend)
	if !ok {
		return nil
	}
	m.storageBusy = true
	m.storageRequested = time.Now()
	profile := p.Name
	return func() tea.Msg {
		rows, err := backend.Storage(profile)
		return storageMsg{profile: profile, rows: rows, err: err, at: time.Now()}
	}
}

func usageBytes(value string) (float64, error) {
	value = strings.TrimSpace(value)
	i := 0
	for i < len(value) && (value[i] >= '0' && value[i] <= '9' || value[i] == '.') {
		i++
	}
	n, err := strconv.ParseFloat(value[:i], 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, fmt.Errorf("invalid byte size %q", value)
	}
	unit := strings.TrimSpace(value[i:])
	factors := map[string]float64{"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30, "TiB": 1 << 40}
	factor, ok := factors[unit]
	if !ok {
		return 0, fmt.Errorf("unknown byte unit %q", unit)
	}
	return n * factor, nil
}
func statsTotals(values []containerStats) (float64, float64, error) {
	var cpu, memory float64
	for _, v := range values {
		c, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v.CPU), "%"), 64)
		if err != nil || c < 0 || math.IsInf(c, 0) || math.IsNaN(c) {
			return 0, 0, fmt.Errorf("invalid CPU sample")
		}
		used, _, ok := strings.Cut(v.Memory, "/")
		if !ok {
			return 0, 0, fmt.Errorf("invalid memory sample")
		}
		b, err := usageBytes(used)
		if err != nil {
			return 0, 0, err
		}
		cpu += c
		memory += b
	}
	return cpu, memory, nil
}

func (m model) renderUsageOverview() string {
	lines := []string{titleStyle.Render("Docker usage overview"), "profile: " + m.currentProfileName()}
	p := m.currentProfile()
	if p == nil || !isRunning(p.Status) {
		lines = append(lines, "Colima is stopped or unavailable.")
	} else {
		lines = append(lines, fmt.Sprintf("VM allocated: %d CPU · %s RAM · %s disk", p.CPUs, humanBytes(p.Memory), humanBytes(p.Disk)))
		if m.overall.profile != p.Name {
			lines = append(lines, "Containers: loading…")
		} else if m.overall.err != nil {
			lines = append(lines, "Containers unavailable: "+m.overall.err.Error())
		} else if time.Since(m.overall.at) > 3*statsInterval {
			lines = append(lines, "Containers: stale; waiting for update")
		} else {
			cpu, mem, err := statsTotals(m.overall.all)
			if err != nil {
				lines = append(lines, "Container totals unavailable: "+err.Error())
			} else {
				lines = append(lines, fmt.Sprintf("%d running · CPU %.2f%% · RAM %s", len(m.overall.all), cpu, humanBytes(int64(mem))))
			}
		}
		lines = append(lines, "CPU: 100% = one core; excludes VM/daemon overhead.", "Docker storage          Used       Reclaimable")
		if m.storage.profile != p.Name {
			lines = append(lines, "Loading storage…")
		} else if m.storage.err != nil {
			lines = append(lines, "Storage unavailable: "+m.storage.err.Error())
		} else {
			for _, row := range m.storage.rows {
				lines = append(lines, fmt.Sprintf("%-22s %-10s %s", sanitizeText(row.Type), sanitizeText(row.Size), sanitizeText(row.Reclaimable)))
			}
			lines = append(lines, fmt.Sprintf("Storage sampled %ds ago (refreshes every 30s).", int(time.Since(m.storage.at).Seconds())))
		}
		lines = append(lines, "Storage categories may share data; not host disk use.")
	}
	lines = append(lines, "esc / q / u close")
	width := min(78, m.width-4)
	for i := range lines {
		lines[i] = truncate(lines[i], width-4)
	}
	// Keep the close hint visible even in a short terminal.
	if len(lines) > m.height-4 {
		lines = append(lines[:m.height-5], lines[len(lines)-1])
	}
	return lipgloss.NewStyle().Width(width).Padding(0, 1).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Render(strings.Join(lines, "\n"))
}
