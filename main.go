package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func main() {
	if os.Getenv("COLIMUI_NO_COLOR") != "1" {
		lipgloss.SetColorProfile(termenv.TrueColor)
	}
	if _, err := tea.NewProgram(initialModel(), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
