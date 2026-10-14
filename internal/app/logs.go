package app

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	maxLogLines        = 10000
	maxLogBytes        = 8 << 20
	maxLogPartialBytes = 1 << 20
)

type logReader struct {
	cancel   context.CancelFunc
	out      io.ReadCloser
	cmd      *exec.Cmd
	waitDone chan struct{}
	waitMu   sync.Mutex
	waitErr  error
}

func (m model) visibleLogs(count int) []string {
	start, end := m.visibleLogRange(count)
	return m.filteredLogs()[start:end]
}

// visibleLogRange reports the window of filtered log indices shown for the
// current scroll position.
func (m model) visibleLogRange(count int) (int, int) {
	filtered := m.filteredLogs()
	if count <= 0 || len(filtered) == 0 {
		return 0, 0
	}
	if m.logScroll >= len(filtered) {
		return 0, min(count, len(filtered))
	}
	end := len(filtered) - m.logScroll
	start := max(0, end-count)
	return start, end
}

// logRowsIndexed turns logical log entries into bounded terminal rows, with
// the filtered log index behind each row. Wrapping here, rather than relying
// on the terminal, keeps the pane height stable; the indices let the mouse
// map rows back to lines.
func (m model) logRowsIndexed(count, width int) ([]string, []int) {
	if count <= 0 {
		return nil, nil
	}
	start, end := m.visibleLogRange(count)
	filtered := m.filteredLogs()
	var rows []string
	var indices []int
	for i := start; i < end; i++ {
		text := m.logText(filtered[i])
		if m.logWrap {
			for _, row := range strings.Split(ansi.HardwrapWc(text, width, true), "\n") {
				rows = append(rows, row)
				indices = append(indices, i)
			}
		} else {
			rows = append(rows, ansi.Truncate(text, width, ""))
			indices = append(indices, i)
		}
	}
	if len(rows) > count {
		rows = rows[len(rows)-count:]
		indices = indices[len(indices)-count:]
	}
	return rows, indices
}

func (m *model) appendLogs(data string) {
	data = m.logPartial + data
	parts := strings.Split(data, "\n")
	m.logPartial = strings.Clone(parts[len(parts)-1])
	for _, line := range parts[:len(parts)-1] {
		if m.partialTrimmed {
			line = "[log line truncated] " + line
			m.partialTrimmed = false
		}
		m.appendLogLine(strings.TrimSuffix(line, "\r"))
	}
	if len(m.logPartial) > maxLogPartialBytes {
		m.logPartial = strings.Clone(m.logPartial[len(m.logPartial)-maxLogPartialBytes:])
		m.logsTruncated = true
		m.partialTrimmed = true
	}
	if m.logScroll == 0 {
		return
	}
	m.logScroll = min(m.logScroll, max(0, len(m.logs)-1))
}

func (m *model) finishLogs() {
	if m.logPartial != "" {
		line := m.logPartial
		if m.partialTrimmed {
			line = "[log line truncated] " + line
			m.partialTrimmed = false
		}
		m.appendLogLine(strings.TrimSuffix(line, "\r"))
		m.logPartial = ""
	}
}

func (m *model) appendLogLine(line string) {
	m.logs = append(m.logs, strings.Clone(line))
	m.logBytes += len(line)
	for len(m.logs) > maxLogLines || m.logBytes > maxLogBytes {
		m.logBytes -= len(m.logs[0])
		m.logs[0] = ""
		m.logs = m.logs[1:]
		m.logsTruncated = true
		// Trimming shifts line indices, so any selection no longer matches.
		m.clearLogSelection()
	}
}

func (m *model) clearLogSelection() {
	m.logSelecting, m.logSelActive, m.logSelDragged = false, false, false
	m.logSelStart, m.logSelEnd = 0, 0
}

// resetLogs drops the retained buffer and all state derived from it.
func (m *model) resetLogs() {
	m.logs, m.logPartial, m.logScroll, m.logFromStart, m.logBytes, m.logsTruncated, m.partialTrimmed = nil, "", 0, false, 0, false, false
	m.clearLogSelection()
}

// clearLogs hides the selected container's lines up to now from every later
// tail, resume or reselection; only a full history load brings them back.
// The in-flight partial line is kept so the next chunk starts on a line boundary.
func (m *model) clearLogs() {
	if since := m.lastLogSince(); since != "" {
		if m.logCleared == nil {
			m.logCleared = make(map[string]string)
		}
		m.logCleared[m.selectedID()] = since
	}
	partial, trimmed := m.logPartial, m.partialTrimmed
	m.resetLogs()
	m.logPartial, m.partialTrimmed = partial, trimmed
	m.status = "logs cleared"
}

func (m model) clearCutoff() string {
	return m.logCleared[m.selectedID()]
}

// tailRequest asks for the selected container's latest lines after its clear cutoff.
func (m model) tailRequest() logRequest {
	return logRequest{follow: m.follow, since: m.clearCutoff()}
}

func (m *model) scrollLogs(key string) {
	switch key {
	case "pgup":
		m.logScroll = min(len(m.filteredLogs()), m.logScroll+10)
	case "pgdown":
		m.logScroll = max(0, m.logScroll-10)
	case "home":
		m.logScroll = len(m.logs)
	case "end":
		m.logScroll = 0
	}
}

func (m *model) reloadSelectedLogs(all ...bool) tea.Cmd {
	fromStart := len(all) > 0 && all[0]
	m.stopLogs()
	m.resetLogs()
	m.logFromStart = fromStart
	m.err = nil
	if m.status == "logs failed" {
		m.status = "ready"
	}
	// A group header has no stream; keep follow armed so it resumes on the
	// next container selection.
	if m.selectedID() == "" {
		return nil
	}
	req := m.tailRequest()
	if fromStart {
		delete(m.logCleared, m.selectedID())
		req = logRequest{follow: m.follow, all: true}
	}
	reader, err := m.currentBackend().OpenLogs(m.currentProfileName(), m.selectedID(), req)
	if err != nil {
		m.err, m.status = err, "logs failed"
		return nil
	}
	m.reader = reader
	return m.readLogsCmd()
}

// resumeFollowLogs reopens a follow stream after docker logs exits (the
// container stopped or restarted), resuming just past the newest retained
// timestamp so nothing is duplicated and the buffer is kept.
func (m *model) resumeFollowLogs() tea.Cmd {
	reader, err := m.currentBackend().OpenLogs(m.currentProfileName(), m.selectedID(), logRequest{follow: true, all: true, since: m.lastLogSince()})
	if err != nil {
		m.follow = false
		m.err, m.status = err, "logs failed"
		return nil
	}
	m.reader = reader
	return m.readLogsCmd()
}

// lastLogSince returns an RFC3339Nano instant just after the newest retained
// log timestamp (or the clear cutoff), for docker logs --since. Never
// substitute the host clock: it drifts from the VM's.
func (m model) lastLogSince() string {
	for i := len(m.logs) - 1; i >= 0; i-- {
		if stamp, _, ok := strings.Cut(m.logs[i], " "); ok {
			if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				return t.Add(time.Nanosecond).Format(time.RFC3339Nano)
			}
		}
	}
	return m.clearCutoff()
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
		if errors.Is(err, io.EOF) {
			err = r.exitError()
		} else if err == nil {
			err = r.exitError()
		}
		return logsMsg{reader: r, done: true, err: err}
	}
}

func startLogReader(cmd *exec.Cmd, cancel context.CancelFunc) (*logReader, error) {
	out, in := io.Pipe()
	reader := &logReader{cancel: cancel, out: out, cmd: cmd, waitDone: make(chan struct{})}
	cmd.Stdout = in
	cmd.Stderr = in
	if err := cmd.Start(); err != nil {
		_ = out.Close()
		_ = in.Close()
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		reader.waitMu.Lock()
		reader.waitErr = err
		reader.waitMu.Unlock()
		close(reader.waitDone)
		_ = in.Close()
	}()
	return reader, nil
}

func (r *logReader) exitError() error {
	if r.waitDone == nil {
		return nil
	}
	<-r.waitDone
	r.waitMu.Lock()
	defer r.waitMu.Unlock()
	return r.waitErr
}

// Docker timestamps are always captured; toggling only changes presentation.
func (m model) logText(line string) string {
	if !m.logTimestamps {
		if stamp, rest, ok := strings.Cut(line, " "); ok {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err == nil {
				line = rest
			}
		}
	}
	return sanitizeText(line)
}

func (m model) filteredLogs() []string {
	query := strings.ToLower(strings.TrimSpace(m.logQuery))
	result := make([]string, 0, len(m.logs))
	for _, line := range m.logs {
		if query == "" || strings.Contains(strings.ToLower(sanitizeText(line)), query) {
			result = append(result, line)
		}
	}
	return result
}

func (m model) logSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.logSearchEditing = false
	case tea.KeyEsc:
		m.logQuery, m.logSearchEditing = m.logSearchBefore, false
	case tea.KeyCtrlU:
		m.logQuery = ""
	case tea.KeyBackspace:
		r := []rune(m.logQuery)
		if len(r) > 0 {
			m.logQuery = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		if len([]rune(m.logQuery)) < 256 {
			m.logQuery += " "
		}
	case tea.KeyRunes:
		if len([]rune(m.logQuery))+len(msg.Runes) <= 256 {
			m.logQuery += string(msg.Runes)
		}
	}
	m.logScroll = 0
	// The query changes which lines the selection indices point at.
	m.clearLogSelection()
	return m, nil
}

func (m *model) pauseLogs() {
	if m.follow {
		m.stopLogs()
		m.finishLogs()
		m.follow = false
	}
}
