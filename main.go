package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// version is replaced with the Git tag in release builds.
var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("colimui %s\n", version)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "update" {
		if err := update(); err != nil {
			fmt.Fprintln(os.Stderr, "update failed:", err)
			os.Exit(1)
		}
		return
	}
	if os.Getenv("COLIMUI_NO_COLOR") != "1" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	m := initialModel()
	m.settingsFile = settingsPath()
	var err error
	if m.autoStopAfter, m.autoStop, err = resolveAutoStop(os.Getenv(autoStopEnv), m.settingsFile); err != nil {
		fmt.Fprintln(os.Stderr, "colimui:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
