package main

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
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
	if count <= 0 || len(m.logs) == 0 {
		return nil
	}
	if m.logScroll >= len(m.logs) {
		return m.logs[:min(count, len(m.logs))]
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
	if len(m.logPartial) > maxLogPartialBytes {
		m.logPartial = m.logPartial[len(m.logPartial)-maxLogPartialBytes:]
		m.partialTrimmed = true
	}
	for _, line := range parts[:len(parts)-1] {
		if m.partialTrimmed {
			line = "[log line truncated] " + line
			m.partialTrimmed = false
		}
		m.appendLogLine(strings.TrimSuffix(line, "\r"))
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
	m.logs = append(m.logs, line)
	m.logBytes += len(line)
	for len(m.logs) > maxLogLines || m.logBytes > maxLogBytes {
		m.logBytes -= len(m.logs[0])
		m.logs = m.logs[1:]
		m.logsTruncated = true
	}
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

func (m *model) reloadSelectedLogs(all ...bool) tea.Cmd {
	fromStart := len(all) > 0 && all[0]
	m.stopLogs()
	m.logs, m.logPartial, m.logScroll, m.logBytes, m.logsTruncated, m.partialTrimmed = nil, "", 0, 0, false, false
	m.logFromStart = fromStart
	m.err = nil
	if m.status == "logs failed" {
		m.status = "ready"
	}
	if m.selectedID() == "" {
		return nil
	}
	reader, err := m.currentBackend().OpenLogs(m.currentProfileName(), m.selectedID(), m.follow, fromStart)
	if err != nil {
		m.err, m.status = err, "logs failed"
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
