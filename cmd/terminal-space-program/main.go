package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("terminal-space-program %s (%s)\n", version.Version, version.Commit)
		return
	}

	// Surface user-overlay + theme warnings before bubbletea takes the
	// screen. Loading is cheap (<5 KB JSON each) so the double-load
	// when tui.New rehydrates is negligible.
	if _, warnings, err := bodies.LoadAllWithWarnings(); err == nil {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "terminal-space-program: skipping %s: %v\n", w.Path, w.Err)
		}
	}
	if _, warnings, err := render.LoadTheme(); err == nil {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "terminal-space-program: skipping theme %s: %v\n", w.Path, w.Err)
		}
	}

	app, err := tui.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
		os.Exit(1)
	}

	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseAllMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
		os.Exit(1)
	}
}
