package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

// helpKey makes a KeyMsg from a key name for driving Help.HandleKey.
func helpKey(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// TestHelpRelabelsForQWERTZ — under a QWERTZ layout the throttle key token
// reads with y↔z swapped (the player's keycaps), while the description prose
// ("zoom in / out") keeps its letters. ADR 0022.
func TestHelpRelabelsForQWERTZ(t *testing.T) {
	h := NewHelp(chipTestTheme())
	out := h.Render(120, 200, keylayout.QWERTZ)
	if !strings.Contains(out, "y / x") {
		t.Errorf("QWERTZ help missing relabelled throttle token 'y / x':\n%s", out)
	}
	if strings.Contains(out, "yoom") {
		t.Error("QWERTZ help mangled a description: 'zoom' became 'yoom'")
	}

	q := NewHelp(chipTestTheme())
	qOut := q.Render(120, 200, keylayout.QWERTY)
	if !strings.Contains(qOut, "z / x") {
		t.Errorf("QWERTY help should keep 'z / x' throttle token:\n%s", qOut)
	}
}

// TestHelpScrollsToLastSection — the bottom section is unreachable in the
// top window but visible after scrolling to the end (the reported bug).
func TestHelpScrollsToLastSection(t *testing.T) {
	h := NewHelp(chipTestTheme())
	const w, ht = 100, 20

	top := h.Render(w, ht, keylayout.QWERTY)
	if !strings.Contains(top, "keybindings") {
		t.Error("title missing from the top of the overlay")
	}
	if strings.Contains(top, "click HUD") {
		t.Fatalf("setup invalid: last section already visible at height %d — pick a shorter height", ht)
	}

	h.HandleKey(helpKey("end"))
	bottom := h.Render(w, ht, keylayout.QWERTY)
	if !strings.Contains(bottom, "MOUSE") || !strings.Contains(bottom, "click HUD") {
		t.Errorf("last section not reachable after End:\n%s", bottom)
	}
}

// TestHelpScrollClamps — scroll can't go above the top or past the end.
func TestHelpScrollClamps(t *testing.T) {
	h := NewHelp(chipTestTheme())
	const w, ht = 100, 20
	h.Render(w, ht, keylayout.QWERTY) // populate geometry

	h.HandleKey(helpKey("up")) // already at top
	if h.scroll != 0 {
		t.Errorf("scroll went above the top: %d", h.scroll)
	}

	h.HandleKey(helpKey("end"))
	atEnd := h.scroll
	h.HandleKey(helpKey("down")) // past the end
	h.HandleKey(helpKey("pgdown"))
	if h.scroll != atEnd {
		t.Errorf("scroll ran past the end: %d, want %d", h.scroll, atEnd)
	}
	if h.scroll != h.maxScroll {
		t.Errorf("end scroll %d != maxScroll %d", h.scroll, h.maxScroll)
	}
}

// TestHelpResetScroll — opening returns to the top.
func TestHelpResetScroll(t *testing.T) {
	h := NewHelp(chipTestTheme())
	h.Render(100, 20, keylayout.QWERTY)
	h.HandleKey(helpKey("end"))
	if h.scroll == 0 {
		t.Fatal("setup: expected a non-zero scroll after End")
	}
	h.ResetScroll()
	if h.scroll != 0 {
		t.Errorf("ResetScroll left scroll at %d, want 0", h.scroll)
	}
}

// TestHelpPageAdvancesViewport — PgDn moves a near-full viewport.
func TestHelpPageAdvancesViewport(t *testing.T) {
	h := NewHelp(chipTestTheme())
	const ht = 24
	h.Render(100, ht, keylayout.QWERTY)
	before := h.scroll
	h.HandleKey(helpKey("pgdown"))
	moved := h.scroll - before
	if moved < h.viewH-2 || moved > h.viewH {
		t.Errorf("PgDn moved %d rows, want ~viewH (%d)", moved, h.viewH)
	}
}

// TestHelpTruncatesToWidth — no rendered row exceeds the width, so long
// rows never wrap and desync the 1-entry-per-row scroll math.
func TestHelpTruncatesToWidth(t *testing.T) {
	h := NewHelp(chipTestTheme())
	const w = 50
	out := h.Render(w, 30, keylayout.QWERTY)
	for i, ln := range strings.Split(out, "\n") {
		if lw := lipgloss.Width(ln); lw > w {
			t.Errorf("row %d width %d exceeds %d: %q", i, lw, w, ln)
		}
	}
}

// TestHelpTitleAndFooterAlwaysShown — chrome is sticky regardless of
// scroll position, even on a short terminal.
func TestHelpTitleAndFooterAlwaysShown(t *testing.T) {
	h := NewHelp(chipTestTheme())
	for _, ht := range []int{8, 20, 60} {
		h.ResetScroll()
		top := h.Render(80, ht, keylayout.QWERTY)
		if !strings.Contains(top, "keybindings") || !strings.Contains(top, "close") {
			t.Errorf("height %d: title/footer missing at top:\n%s", ht, top)
		}
		h.HandleKey(helpKey("end"))
		bot := h.Render(80, ht, keylayout.QWERTY)
		if !strings.Contains(bot, "keybindings") || !strings.Contains(bot, "close") {
			t.Errorf("height %d: title/footer missing at bottom:\n%s", ht, bot)
		}
	}
}
