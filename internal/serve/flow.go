package serve

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
)

// guestFlow fronts a NOT-yet-enrolled ssh session (v0.27 S3, ADR
// 0034): calibration card → invite-code prompt → handle confirm/edit
// → game. Enrollment commits at the handle step (the code is peeked,
// not spent, at entry — a mid-flow disconnect doesn't burn it).
// Enrolled reconnects never see this model; the session handler hands
// them the game directly.
type guestFlow struct {
	store    *sessiondir.Store
	fp       string             // ssh public-key fingerprint (the identity)
	game     tea.Model          // the session's game (guest sink attached)
	onEnroll func(handle string) // presence/chip hook, fired at commit (may be nil)

	phase    flowPhase
	declined bool
	size     tea.WindowSizeMsg
	input    []rune // live text input (code, then handle)
	code     string // validated invite code, kept for the enroll commit
	errMsg   string
}

type flowPhase int

const (
	phaseCard flowPhase = iota
	phaseCode
	phaseHandle
)

func newGuestFlow(store *sessiondir.Store, fp string, game tea.Model, onEnroll func(handle string)) tea.Model {
	return guestFlow{store: store, fp: fp, game: game, onEnroll: onEnroll}
}

func (m guestFlow) Init() tea.Cmd { return nil }

func (m guestFlow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.size = v
		return m, nil
	case tea.KeyMsg:
		if m.declined {
			return m, tea.Quit // any key after the help text disconnects
		}
		switch v.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		}
		switch m.phase {
		case phaseCard:
			return m.updateCard(v)
		case phaseCode:
			return m.updateCode(v)
		case phaseHandle:
			return m.updateHandle(v)
		}
	}
	return m, nil
}

func (m guestFlow) updateCard(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "y", "Y":
		m.phase = phaseCode
		m.input = nil
		return m, nil
	case "n", "N":
		m.declined = true
		return m, nil
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m guestFlow) updateCode(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEnter:
		code := string(m.input)
		inv, err := m.store.Peek(code)
		if err != nil {
			m.errMsg = "unknown or already-used code — check with your host"
			m.input = nil
			return m, nil
		}
		m.code = code
		m.errMsg = ""
		m.phase = phaseHandle
		m.input = []rune(inv.Handle) // pre-bound handle, editable
		return m, nil
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil
	case tea.KeyRunes:
		for _, r := range k.Runes {
			if len(m.input) < 16 {
				m.input = append(m.input, r)
			}
		}
		return m, nil
	}
	return m, nil
}

func (m guestFlow) updateHandle(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEnter:
		p, err := m.store.Enroll(m.code, m.fp, string(m.input))
		if err != nil {
			// The code was spent between Peek and commit (or the handle
			// is empty) — back to the code prompt with the reason.
			m.errMsg = err.Error()
			m.phase = phaseCode
			m.input = nil
			return m, nil
		}
		if m.onEnroll != nil {
			m.onEnroll(p.Handle)
		}
		return m.startGame()
	case tea.KeyBackspace:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
		return m, nil
	case tea.KeyRunes:
		for _, r := range k.Runes {
			if len(m.input) < 24 {
				m.input = append(m.input, r)
			}
		}
		return m, nil
	}
	return m, nil
}

// startGame hands over to the game model: run its Init (tick loop)
// and replay the last known pty size so it lays out immediately.
func (m guestFlow) startGame() (tea.Model, tea.Cmd) {
	initCmd := m.game.Init()
	game, sizeCmd := m.game.Update(m.size)
	return game, tea.Batch(initCmd, sizeCmd)
}

func (m guestFlow) View() string {
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
	switch m.phase {
	case phaseCode:
		lines := []string{
			"",
			"  TERMINAL SPACE PROGRAM — join session",
			"",
			"  invite code: " + string(m.input) + "▌",
			"",
		}
		if m.errMsg != "" {
			lines = append(lines, "  "+m.errMsg, "")
		}
		lines = append(lines, "  (enter to submit, esc to disconnect)")
		return strings.Join(lines, "\n")
	case phaseHandle:
		return strings.Join([]string{
			"",
			"  TERMINAL SPACE PROGRAM — join session",
			"",
			"  your handle: " + string(m.input) + "▌",
			"",
			"  (edit if you like — enter to join, esc to disconnect)",
		}, "\n")
	}
	// phaseCard — the terminal capability check (v0.27 S2): braille
	// and color support are undetectable server-side, so we ask.
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
