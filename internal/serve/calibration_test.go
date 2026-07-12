package serve

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func newCardSession(t *testing.T) tea.Model {
	t.Helper()
	game, _, err := newSessionApp()
	if err != nil {
		t.Fatalf("newSessionApp: %v", err)
	}
	return withCalibrationCard(game)
}

// The card renders the braille ramp, color swatches, and the y/n ask.
func TestCalibrationCardRender(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	out := stripANSI(newCardSession(t).View())
	for _, want := range []string{"connection check", "⣿", "[y/n]"} {
		if !strings.Contains(out, want) {
			t.Errorf("card missing %q:\n%s", want, out)
		}
	}
}

// "y" swaps to the game with the pty size replayed — the next frame is
// the orbit screen, not the card.
func TestCalibrationAcceptStartsGame(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newCardSession(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m, _ = m.Update(sim.TickMsg(time.Now()))
	out := stripANSI(m.View())
	if strings.Contains(out, "[y/n]") {
		t.Fatal("still on the card after accepting")
	}
	if !strings.Contains(out, "warp 1x") {
		t.Errorf("expected the orbit screen after accept, got:\n%s", firstLines(out, 3))
	}
}

// "n" shows the font/TERM help; the next key disconnects.
func TestCalibrationDeclineHelpsThenQuits(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newCardSession(t)
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil {
		t.Fatal("decline should hold the session on the help text, not quit yet")
	}
	out := stripANSI(m.View())
	if !strings.Contains(out, "braille glyphs") || !strings.Contains(out, "press any key to disconnect") {
		t.Fatalf("expected font/TERM help after decline, got:\n%s", out)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("expected quit after any key on the help text")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}
}

// The shared size gate holds behind the card too: accepting on an
// undersized pty lands on the gate, not a broken frame (the "gate
// covered in the ssh session constructor" acceptance).
func TestCalibrationAcceptIntoSizeGate(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newCardSession(t)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if out := stripANSI(m.View()); !strings.Contains(out, "TERMINAL TOO SMALL") {
		t.Errorf("expected the size gate on an 80×24 ssh session, got:\n%s", firstLines(out, 5))
	}
}
