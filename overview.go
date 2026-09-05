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
	"github.com/charmbracelet/x/ansi"
)

type overviewBackend interface {
	AllStats(string) ([]containerStats, error)
	Storage(string) ([]storageRow, error)
}
type cleanupBackend interface {
	Cleanup(string) error
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

// Cleanup removes only Docker resources which are unused. It never removes a
// running container or data from a volume still attached to a container.
func (execBackend) Cleanup(profile string) error {
	for _, args := range [][]string{
		{"system", "prune", "--all", "--force"},
		{"volume", "prune", "--all", "--force"},
	} {
		if _, err := dockerUsageOutput(profile, 30*time.Second, args...); err != nil {
			return fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}

type cleanupMsg struct {
	profile string
	err     error
}

func (m *model) cleanupCmd() tea.Cmd {
	if m.cleanupRunning {
		return nil
	}
	backend, ok := m.currentBackend().(cleanupBackend)
	if !ok {
		return nil
	}
	profile := m.currentProfileName()
	m.cleanupRunning = true
	return func() tea.Msg {
		return cleanupMsg{profile: profile, err: backend.Cleanup(profile)}
	}
}

func (m model) cleanupSummary() []string {
	if m.storage.profile != m.currentProfileName() || m.storage.err != nil {
		return []string{"Docker will identify reclaimable resources when cleanup runs."}
	}
	var lines []string
	for _, row := range m.storage.rows {
		if row.Reclaimable != "" && !strings.HasPrefix(row.Reclaimable, "0B") {
			lines = append(lines, fmt.Sprintf("%s: %s reclaimable", sanitizeText(row.Type), sanitizeText(row.Reclaimable)))
		}
	}
	if len(lines) == 0 {
		return []string{"Docker reports no reclaimable storage."}
	}
	return lines
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
	width := min(78, m.width-4)
	contentWidth := max(1, width-4)
	lines := []string{
		titleStyle.Render("docker usage overview"),
		mutedStyle.Render("profile: " + sanitizeText(m.currentProfileName())),
		"",
	}
	p := m.currentProfile()
	if p == nil || !isRunning(p.Status) {
		lines = append(lines, statusStyle.Render("colima is stopped or unavailable."))
	} else {
		lines = append(lines, fmt.Sprintf("vm allocated: %d cpu · %s ram · %s disk", p.CPUs, humanBytes(p.Memory), humanBytes(p.Disk)))
		if m.overall.profile != p.Name {
			lines = append(lines, mutedStyle.Render("containers: loading…"))
		} else if m.overall.err != nil {
			lines = append(lines, errorStyle.Render("containers unavailable: "+sanitizeText(m.overall.err.Error())))
		} else if time.Since(m.overall.at) > 3*statsInterval {
			lines = append(lines, statusStyle.Render("containers: stale; waiting for update"))
		} else {
			cpu, mem, err := statsTotals(m.overall.all)
			if err != nil {
				lines = append(lines, errorStyle.Render("container totals unavailable: "+sanitizeText(err.Error())))
			} else {
				lines = append(lines, fmt.Sprintf("%d running · cpu %.2f%% · ram %s", len(m.overall.all), cpu, humanBytes(int64(mem))))
			}
		}
		lines = append(lines, mutedStyle.Render("cpu: 100% = one core; excludes vm/docker overhead."), "", overviewStorageHeader())
		if m.storage.profile != p.Name {
			lines = append(lines, mutedStyle.Render("loading storage…"))
		} else if m.storage.err != nil {
			lines = append(lines, errorStyle.Render("storage unavailable: "+sanitizeText(m.storage.err.Error())))
		} else {
			for _, row := range m.storage.rows {
				lines = append(lines, fmt.Sprintf("%-22s %-10s %s", overviewText(row.Type), overviewText(row.Size), overviewText(row.Reclaimable)))
			}
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("storage sampled %ds ago (refreshes every 30s).", int(time.Since(m.storage.at).Seconds()))))
		}
		lines = append(lines, mutedStyle.Render("storage categories may share data; not host disk use."))
	}
	if m.cleanupRunning {
		lines = append(lines, "", logHeadingStyle.Render("cleanup"), statusStyle.Render("cleaning up reclaimable storage…"))
	} else {
		lines = append(lines, "", logHeadingStyle.Render("keyboard shortcuts"), overviewFooter())
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], contentWidth, "")
	}
	// Keep the close hint visible even in a short terminal.
	if len(lines) > m.height-6 {
		lines = append(lines[:m.height-7], lines[len(lines)-1])
	}
	return lipgloss.NewStyle().Width(width).Padding(1, 2).Border(lipgloss.RoundedBorder()).BorderForeground(accent).Background(popupBackground).Render(strings.Join(lines, "\n"))
}

func overviewStorageHeader() string {
	return logHeadingStyle.Render("docker storage") + strings.Repeat(" ", 9) + mutedStyle.Render("used") + strings.Repeat(" ", 8) + mutedStyle.Render("reclaimable")
}

func overviewText(value string) string {
	return strings.ToLower(sanitizeText(value))
}

func overviewFooter() string {
	return selectedStyle.Render("c") + mutedStyle.Render(" clean up reclaimable storage") + mutedStyle.Render("  ·  ") + selectedStyle.Render("esc / q / u") + mutedStyle.Render(" close")
}
