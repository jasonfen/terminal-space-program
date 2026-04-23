package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("terminal-space-program %s (%s)\n", version.Version, version.Commit)
		return
	}

	app, err := tui.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
		os.Exit(1)
	}
}
