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
	h.OpenPage(5) // MANUAL FLIGHT (F1 opens on the index since #494)
	out := h.Render(120, 200, keylayout.QWERTZ)
	if !strings.Contains(out, "y / x") {
		t.Errorf("QWERTZ help missing relabelled throttle token 'y / x':\n%s", out)
	}
	if strings.Contains(out, "yoom") {
		t.Error("QWERTZ help mangled a description: 'zoom' became 'yoom'")
	}

	q := NewHelp(chipTestTheme())
	q.OpenPage(5)
	qOut := q.Render(120, 200, keylayout.QWERTY)
	if !strings.Contains(qOut, "z / x") {
		t.Errorf("QWERTY help should keep 'z / x' throttle token:\n%s", qOut)
	}
}

// TestHelpScrollsToLastRow: a page taller than the window hides its last
// row until End (the reported bug). Since #494 each section is its own
// page, so the long READOUT GLOSSARY page stands in for the old flat list.
func TestHelpScrollsToLastRow(t *testing.T) {
	h := NewHelp(chipTestTheme())
	h.OpenPage(15) // READOUT GLOSSARY
	const w, ht = 100, 12

	top := h.Render(w, ht, keylayout.QWERTY)
	if !strings.Contains(top, "READOUT GLOSSARY") {
		t.Error("page title (footer position line) missing from the page")
	}
	if strings.Contains(top, "orbit floor") {
		t.Fatalf("setup invalid: last row already visible at height %d", ht)
	}

	h.HandleKey(helpKey("end"))
	bottom := h.Render(w, ht, keylayout.QWERTY)
	if !strings.Contains(bottom, "orbit floor") {
		t.Errorf("last row not reachable after End:\n%s", bottom)
	}
}

// TestHelpScrollClamps — scroll can't go above the top or past the end.
func TestHelpScrollClamps(t *testing.T) {
	h := NewHelp(chipTestTheme())
	h.OpenPage(15)
	const w, ht = 100, 12
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
	h.OpenPage(15)
	h.Render(100, 12, keylayout.QWERTY)
	h.HandleKey(helpKey("end"))
	if h.scroll == 0 {
		t.Fatal("setup: expected a non-zero scroll after End")
	}
	h.ResetScroll()
	if h.scroll != 0 || h.page != helpIndexPage {
		t.Errorf("ResetScroll left scroll=%d page=%d, want 0 on the index", h.scroll, h.page)
	}
}

// TestHelpPageAdvancesViewport — PgDn moves a near-full viewport.
func TestHelpPageAdvancesViewport(t *testing.T) {
	h := NewHelp(chipTestTheme())
	h.OpenPage(15)
	const ht = 10
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
	for page := -1; page < helpPageCount(); page++ {
		h.ResetScroll()
		if page >= 0 {
			h.OpenPage(page)
		}
		out := h.Render(w, 30, keylayout.QWERTY)
		for i, ln := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(ln); lw > w {
				t.Errorf("page %d row %d width %d exceeds %d: %q", page, i, lw, w, ln)
			}
		}
	}
}

// TestHelpDocumentsReArmDockKey (#372): the F1 overlay is the source of
// truth for keybindings — the new re-arm docking key must be findable in
// it, at a height tall enough to reach the VESSEL section without scrolling
// past it in this check.
func TestHelpDocumentsReArmDockKey(t *testing.T) {
	h := NewHelp(chipTestTheme())
	h.OpenPage(9) // VESSEL
	out := h.Render(120, 400, keylayout.QWERTY)
	if !strings.Contains(out, "re-arm docking") {
		t.Errorf("help overlay missing the re-arm docking (`c`) entry:\n%s", out)
	}
}

// TestHelpTitleAndFooterAlwaysShown — chrome is sticky regardless of
// scroll position, even on a short terminal.
func TestHelpTitleAndFooterAlwaysShown(t *testing.T) {
	h := NewHelp(chipTestTheme())
	for _, ht := range []int{8, 20, 60} {
		h.ResetScroll()
		h.OpenPage(15)
		top := h.Render(80, ht, keylayout.QWERTY)
		if !strings.Contains(top, "READOUT GLOSSARY") || !strings.Contains(top, "close") {
			t.Errorf("height %d: title/footer missing at top:\n%s", ht, top)
		}
		h.HandleKey(helpKey("end"))
		bot := h.Render(80, ht, keylayout.QWERTY)
		if !strings.Contains(bot, "READOUT GLOSSARY") || !strings.Contains(bot, "close") {
			t.Errorf("height %d: title/footer missing at bottom:\n%s", ht, bot)
		}
	}
}

// TestHelpGlossaryHdgRowSaysCommandedVsMeasured (wave C review, LOW): the
// heading:/hdg glossary row must say which figure is commanded and which is
// measured, that they agree on the pad, and that the ORBIT ball's rungs are
// out-of-plane degrees (an ORBIT pitch trim moves the nose along the ball's
// equator, so its readout and the rungs are different axes).
func TestHelpGlossaryHdgRowSaysCommandedVsMeasured(t *testing.T) {
	var desc string
	for _, sec := range helpSections {
		for _, r := range sec.rows {
			if r[0] == "heading: / hdg" {
				desc = r[1]
			}
		}
	}
	if desc == "" {
		t.Fatal("no heading: / hdg glossary row")
	}
	for _, want := range []string{"commanded", "measured", "on the pad", "out of the orbit plane"} {
		if !strings.Contains(desc, want) {
			t.Errorf("glossary hdg row lacks %q: %q", want, desc)
		}
	}
}
