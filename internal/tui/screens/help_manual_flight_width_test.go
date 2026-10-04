package screens

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

// TestHelpManualFlightRowsKeepTwoCellsAtDesignSize (#548 review line 90):
// the widest MANUAL FLIGHT row (the `alt on a Mac` note) is 138 cells,
// 2 to spare at the 140x40 floor. The new alt+b / alt+f row sits well
// inside it. Pin the margin so the next reword that eats it goes red here
// instead of truncating on a player's terminal. Unrendered rows, measured
// with lipgloss.Width (glyph-aware).
func TestHelpManualFlightRowsKeepTwoCellsAtDesignSize(t *testing.T) {
	h := NewHelp(chipTestTheme())
	page := -1
	for i, s := range helpSections {
		if s.header == "MANUAL FLIGHT" {
			page = i + 1
		}
	}
	if page < 0 {
		t.Fatal("no MANUAL FLIGHT page")
	}
	h.OpenPage(page)
	widest := 0
	for _, ln := range h.bodyLines(keylayout.QWERTY) {
		if w := lipgloss.Width(ln); w > widest {
			widest = w
		}
	}
	if widest < 130 {
		t.Fatalf("control failed: widest row %d cells, the Mac note should be ~138", widest)
	}
	if widest > 138 {
		t.Errorf("widest MANUAL FLIGHT row is %d cells, want at most 138 (2 spare at 140)", widest)
	}
}
