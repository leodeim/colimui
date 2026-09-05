package main

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestLogSearchAndTimestamps(t *testing.T) {
	m := model{logs: []string{"2026-09-05T12:00:00.123Z ERROR failed", "plain message", "2026-09-05T12:01:00Z error again"}, logQuery: "ERROR"}
	got := m.visibleLogs(10)
	if len(got) != 2 {
		t.Fatalf("matches: %v", got)
	}
	if m.logText(got[0]) != "ERROR failed" {
		t.Fatal("timestamp not hidden")
	}
	m.logTimestamps = true
	if m.logText(got[0]) != got[0] {
		t.Fatal("timestamp not shown")
	}
	if m.logText("plain message") != "plain message" {
		t.Fatal("plain line changed")
	}
	m.logQuery = "absent"
	if len(m.visibleLogs(10)) != 0 {
		t.Fatal("nonmatching lines displayed")
	}
}

func TestLogPausePreservesBuffer(t *testing.T) {
	m := newModel(&fakeBackend{}, nil)
	m.logs = []string{"one", "two"}
	m.follow = true
	u, cmd := m.key(shortcutKey("f"))
	m = u.(model)
	if cmd != nil || m.follow || len(m.logs) != 2 {
		t.Fatal("pause lost logs")
	}
	m.follow = true
	u, _ = m.key(tea.KeyMsg{Type: tea.KeyPgUp})
	m = u.(model)
	if m.follow || m.logScroll != 2 {
		t.Fatal("scroll did not pause")
	}
	u, _ = m.key(shortcutKey("L"))
	m = u.(model)
	u, _ = m.key(shortcutKey("数据库"))
	m = u.(model)
	u, _ = m.key(tea.KeyMsg{Type: tea.KeyBackspace})
	m = u.(model)
	if m.logQuery != "数据" {
		t.Fatal("unicode editing failed")
	}
	u, _ = m.key(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(model)
	if m.logSearchEditing || m.logQuery != "" {
		t.Fatal("cancel failed")
	}
}

func TestLogMemoryNoticeAndPartialAttribution(t *testing.T) {
	m := model{containers: []container{{ID: "a"}}}
	m.appendLogs("good\n" + strings.Repeat("x", maxLogPartialBytes+20))
	if m.logs[0] != "good" {
		t.Fatal("truncation attributed to wrong line")
	}
	if !m.partialTrimmed || len(m.logPartial) > maxLogPartialBytes {
		t.Fatal("partial not bounded")
	}
	if !strings.Contains(m.renderLogs(8, 80), "logs truncated") {
		t.Fatal("missing immediate notice")
	}
	m.finishLogs()
	if !strings.HasPrefix(m.logs[1], "[log line truncated]") {
		t.Fatal("missing line marker")
	}
	for i := 0; i < maxLogLines+1; i++ {
		m.appendLogLine("line")
	}
	if len(m.logs) > maxLogLines || m.logBytes > maxLogBytes || !m.logsTruncated {
		t.Fatal("retention limits failed")
	}
	m.appendLogLine(strings.Repeat("x", maxLogBytes+1))
	if m.logBytes > maxLogBytes {
		t.Fatal("byte limit failed")
	}
}
