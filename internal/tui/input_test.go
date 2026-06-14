package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

// TestNormalizeKeyQWERTZ — the ingest chokepoint (ADR 0022): a 'y' typed on a
// QWERTZ keyboard reaches the matcher as 'z' (and vice versa), so the QWERTY-
// authored Keymap and raw-string handlers fire under the right physical key.
func TestNormalizeKeyQWERTZ(t *testing.T) {
	rune1 := func(s string) tea.KeyMsg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	cases := []struct{ in, want string }{
		{"y", "z"}, {"z", "y"}, {"Y", "Z"}, {"Z", "Y"},
		{"x", "x"}, {"w", "w"}, {".", "."}, {"1", "1"},
	}
	for _, c := range cases {
		got := normalizeKey(keylayout.QWERTZ, rune1(c.in)).String()
		if got != c.want {
			t.Errorf("normalizeKey(QWERTZ, %q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeKeyPassthrough — QWERTY is identity, and non-rune keys (arrows,
// ctrl combos, function keys) are never touched regardless of layout.
func TestNormalizeKeyPassthrough(t *testing.T) {
	special := []tea.KeyMsg{
		{Type: tea.KeyUp},
		{Type: tea.KeyCtrlC},
		{Type: tea.KeyF1},
		{Type: tea.KeyShiftUp},
		{Type: tea.KeyRunes, Runes: []rune("z")}, // identity under QWERTY
	}
	for _, m := range special {
		if got := normalizeKey(keylayout.QWERTY, m); got.String() != m.String() {
			t.Errorf("QWERTY normalizeKey altered %q → %q", m.String(), got.String())
		}
	}
	// Under QWERTZ, a special (non-Runes) key still passes through.
	up := tea.KeyMsg{Type: tea.KeyUp}
	if got := normalizeKey(keylayout.QWERTZ, up); got.String() != up.String() {
		t.Errorf("QWERTZ normalizeKey altered a special key %q → %q", up.String(), got.String())
	}
}
