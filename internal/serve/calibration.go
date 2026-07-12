package serve

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// calibrationModel fronts every ssh session with a one-shot terminal
// check (v0.27 S2, ADR 0034): the game's braille canvas and color
// chips are undetectable server-side, so the player confirms them
// before the game starts. "y" swaps to the wrapped game model; "n"
// shows font/TERM help and disconnects on the next key. Local play
// never sees this — the host's own terminal is theirs to judge.
type calibrationModel struct {
	game     tea.Model
	size     tea.WindowSizeMsg
	declined bool
}

// withCalibrationCard wraps a fresh session game behind the card.
func withCalibrationCard(game tea.Model) tea.Model {
	return calibrationModel{game: game}
}

func (m calibrationModel) Init() tea.Cmd { return nil }

func (m calibrationModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.size = v
		return m, nil
	case tea.KeyMsg:
		if m.declined {
			return m, tea.Quit // any key after the help text disconnects
		}
		switch v.String() {
		case "y", "Y":
			// Hand over to the game: run its Init (tick loop) and replay
			// the last known pty size so it lays out immediately.
			initCmd := m.game.Init()
			game, sizeCmd := m.game.Update(m.size)
			return game, tea.Batch(initCmd, sizeCmd)
		case "n", "N":
			m.declined = true
			return m, nil
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m calibrationModel) View() string {
	if m.declined {
		return strings.Join([]string{
			"",
			"  This game draws orbits with braille characters and ANSI colors.",
			"",
			"  · switch to a monospace font with braille glyphs",
			"    (JetBrains Mono, Fira Code, Menlo, and most Nerd Fonts work)",
			"  · make sure your terminal advertises color: TERM=xterm-256color",
			"",
			"  Reconnect when ready — press any key to disconnect.",
			"",
		}, "\n")
	}
	swatches := make([]string, 0, 6)
	for _, c := range []string{"1", "2", "3", "4", "5", "6"} {
		swatches = append(swatches, lipgloss.NewStyle().Foreground(lipgloss.Color(c)).Render("██"))
	}
	return strings.Join([]string{
		"",
		"  TERMINAL SPACE PROGRAM — connection check",
		"",
		"  braille:  ⠁⠃⠇⡇⣇⣧⣷⣿⣷⣧⣇⡇⠇⠃⠁   (should be a smooth ramp of dots)",
		"  colors:   " + strings.Join(swatches, " ") + "   (should be six distinct colors)",
		"",
		"  Can you see smooth braille dots and six distinct colors? [y/n]",
		"",
	}, "\n")
}
