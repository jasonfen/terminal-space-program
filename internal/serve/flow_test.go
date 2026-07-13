package serve

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

const testFP = "SHA256:flowtest"

func newFlowFixture(t *testing.T) (*sessiondir.Store, tea.Model) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	store, err := sessiondir.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	game, _, err := newSessionApp()
	if err != nil {
		t.Fatalf("newSessionApp: %v", err)
	}
	return store, newGuestFlow(store, testFP, game, nil)
}

func typeString(m tea.Model, s string) tea.Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func pressEnter(m tea.Model) tea.Model {
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return m
}

// The flow opens on the calibration card; declining shows the font
// help and the next key disconnects.
func TestFlowCardDecline(t *testing.T) {
	_, m := newFlowFixture(t)
	out := stripANSI(m.View())
	if !strings.Contains(out, "connection check") || !strings.Contains(out, "[y/n]") {
		t.Fatalf("expected the calibration card first, got:\n%s", out)
	}
	m = typeString(m, "n")
	if out := stripANSI(m.View()); !strings.Contains(out, "braille glyphs") {
		t.Fatalf("expected font/TERM help after decline, got:\n%s", out)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd == nil {
		t.Fatal("expected quit after any key on the help text")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg, got %T", cmd())
	}
}

// Full happy path: card → code (case/dash-insensitive) → prefilled
// handle, edited → game running with the enrolled identity.
func TestFlowEnrollHappyPath(t *testing.T) {
	store, m := newFlowFixture(t)
	inv, err := store.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}

	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	m = typeString(m, "y")
	if out := stripANSI(m.View()); !strings.Contains(out, "invite code:") {
		t.Fatalf("expected code prompt after card accept, got:\n%s", out)
	}

	m = typeString(m, strings.ToLower(strings.ReplaceAll(inv.Code, "-", "")))
	m = pressEnter(m)
	out := stripANSI(m.View())
	if !strings.Contains(out, "your handle:") || !strings.Contains(out, "dave") {
		t.Fatalf("expected prefilled handle prompt, got:\n%s", out)
	}

	// Edit "dave" → "david" (backspace the e, append id) and join.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = typeString(m, "id")
	m = pressEnter(m)

	m, _ = m.Update(sim.TickMsg(time.Now()))
	if out := stripANSI(m.View()); !strings.Contains(out, "warp 1x") {
		t.Fatalf("expected the game after enroll, got:\n%s", firstLines(out, 3))
	}
	p, err := store.FindPlayer(testFP)
	if err != nil || p.Handle != "david" {
		t.Errorf("FindPlayer = %+v, %v; want handle david", p, err)
	}
	if _, err := store.Peek(inv.Code); err == nil {
		t.Error("invite code survived enrollment; want one-time")
	}
}

// A bogus code keeps the player on the prompt with a clear error.
func TestFlowBadCode(t *testing.T) {
	_, m := newFlowFixture(t)
	m = typeString(m, "y")
	m = typeString(m, "XXXX-XXXX")
	m = pressEnter(m)
	out := stripANSI(m.View())
	if !strings.Contains(out, "unknown or already-used code") {
		t.Fatalf("expected code error, got:\n%s", out)
	}
	if !strings.Contains(out, "invite code:") {
		t.Error("player fell out of the code prompt on a bad code")
	}
}

// The shared size gate holds behind the flow too: enrolling on an
// undersized pty lands on the gate, not a broken frame.
func TestFlowEnrollIntoSizeGate(t *testing.T) {
	store, m := newFlowFixture(t)
	inv, err := store.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = typeString(m, "y")
	m = typeString(m, inv.Code)
	m = pressEnter(m)
	m = pressEnter(m) // accept prefilled handle
	if out := stripANSI(m.View()); !strings.Contains(out, "TERMINAL TOO SMALL") {
		t.Errorf("expected the size gate on an 80×24 enroll, got:\n%s", firstLines(out, 5))
	}
}
