package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// version is replaced with the Git tag in release builds.
var version = "dev"

const noColorEnv = "COLIMUI_NO_COLOR"

// cliCommand is one subcommand; this table drives both dispatch and the help.
type cliCommand struct {
	names []string
	about string
	run   func(stdout io.Writer) error
}

func cliCommands() []cliCommand {
	return []cliCommand{
		{names: []string{"update"}, about: "update to the latest release", run: func(io.Writer) error { return update() }},
		{names: []string{"menubar"}, about: "run the macOS menu bar item", run: func(io.Writer) error { return runMenubar(execBackend{}) }},
		{names: []string{"version", "--version", "-v"}, about: "print the version", run: func(stdout io.Writer) error {
			_, err := fmt.Fprintf(stdout, "colimui %s\n", version)
			return err
		}},
		{names: []string{"help", "--help", "-h"}, about: "show this help", run: func(stdout io.Writer) error {
			_, err := io.WriteString(stdout, usage())
			return err
		}},
	}
}

func usage() string {
	var b strings.Builder
	b.WriteString("colimui - terminal UI for Colima and Docker\n\nUsage:\n")
	fmt.Fprintf(&b, "  %-18s %s\n", "colimui", "open the TUI")
	for _, c := range cliCommands() {
		about := c.about
		if len(c.names) > 1 {
			about += " (also " + strings.Join(c.names[1:], ", ") + ")"
		}
		fmt.Fprintf(&b, "  %-18s %s\n", "colimui "+c.names[0], about)
	}
	b.WriteString("\nEnvironment:\n")
	fmt.Fprintf(&b, "  %-18s %s\n", autoStopEnv, "idle auto-stop for this run: a duration (45m, 2h) or off")
	fmt.Fprintf(&b, "  %-18s %s\n", noColorEnv, "set to 1 to disable colors")
	return b.String()
}

// runCLI dispatches command-line arguments and returns the exit code; usage
// errors exit 2 so a typo never silently opens the TUI.
func runCLI(args []string, stdout, stderr io.Writer) int {
	for _, c := range cliCommands() {
		if !slices.Contains(c.names, args[0]) {
			continue
		}
		if len(args) > 1 {
			fmt.Fprintf(stderr, "colimui %s: unexpected argument %q\n", c.names[0], args[1])
			return 2
		}
		if err := c.run(stdout); err != nil {
			fmt.Fprintf(stderr, "colimui %s: %v\n", c.names[0], err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stderr, "colimui: unknown command %q\n\n%s", args[0], usage())
	return 2
}

func main() {
	if len(os.Args) > 1 {
		os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
	}
	if os.Getenv(noColorEnv) != "1" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	m, err := configuredModel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "colimui:", err)
		os.Exit(1)
	}
	if m.menubar && menubarSupported {
		if err := spawnMenubar(); err != nil {
			m.err = fmt.Errorf("menu bar item: %w", err)
		}
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// configuredModel applies the saved config file and environment overrides.
func configuredModel() (model, error) {
	m := initialModel()
	m.settingsFile = settingsPath()
	saved, err := loadSettings(m.settingsFile)
	if err != nil {
		return m, err
	}
	m.logTimestamps, m.logWrap = saved.LogTimestamps, saved.LogWrap
	m.menubar = saved.Menubar
	m.autoStopPinned = strings.TrimSpace(os.Getenv(autoStopEnv)) != ""
	m.autoStopAfter, m.autoStop, err = resolveAutoStop(os.Getenv(autoStopEnv), saved, m.settingsFile)
	return m, err
}
