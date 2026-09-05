package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	ID             string
	Name           string
	Image          string
	State          string
	Status         string
	Command        string
	Ports          string
	ComposeProject string
	ComposeService string
}

func (c container) listName() string {
	if c.ComposeService != "" {
		return c.ComposeService
	}
	return c.Name
}

type refreshMsg struct {
	profileName string
	requestID   uint64
	profiles    []profile
	containers  []container
	err         error
}

type actionMsg struct {
	requestID uint64
	label     string
	err       error
}

type activeAction struct {
	containerID string
	label       string
}

type tickMsg time.Time

type spinnerTickMsg time.Time

type logsMsg struct {
	reader *logReader
	data   []byte
	done   bool
	err    error
}

type updateCheckMsg struct {
	version string
}

type actionMenuItem struct {
	label    string
	shortcut string
	enabled  bool
}

type tickFactory func() tea.Cmd

type model struct {
	profiles         []profile
	profileIndex     int
	containers       []container
	containerIndex   int
	focus            int
	width            int
	height           int
	status           string
	err              error
	confirmDelete    bool
	deleteProfile    string
	deleteID         string
	logs             []string
	logPartial       string
	logBytes         int
	logsTruncated    bool
	partialTrimmed   bool
	logScroll        int
	logFromStart     bool
	follow           bool
	reader           *logReader
	expanded         map[string]bool
	refreshID        uint64
	appliedRefreshID uint64
	nextActionID     uint64
	activeActions    map[uint64]activeAction
	spinnerFrame     int
	updateVersion    string
	actionMenu       bool
	actionIndex      int
	backend          Backend
	tick             tickFactory
}

func initialModel() model {
	return newModel(execBackend{}, defaultTick)
}

func newModel(backend Backend, tick tickFactory) model {
	if backend == nil {
		backend = execBackend{}
	}
	if tick == nil {
		tick = defaultTick
	}
	return model{focus: 0, status: "loading", expanded: make(map[string]bool), refreshID: 1, backend: backend, tick: tick}
}

func (m model) currentBackend() Backend {
	if m.backend != nil {
		return m.backend
	}
	return execBackend{}
}

func (m model) nextTick() tea.Cmd {
	if m.tick != nil {
		return m.tick()
	}
	return defaultTick()
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
