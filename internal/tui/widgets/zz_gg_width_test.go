package widgets

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

// GG mock: what the width libraries the game lays out with say about the
// 256 block mosaics. Anything other than 1 would break every bordered row.
func TestGGOctantWidths(t *testing.T) {
	bad := 0
	for m, r := range octantRune {
		lw, aw, rw := lipgloss.Width(string(r)), ansi.StringWidth(string(r)), runewidth.RuneWidth(r)
		if lw != 1 || aw != 1 || rw != 1 {
			bad++
			if bad < 6 {
				t.Logf("mask %02x U+%05X lipgloss=%d ansi=%d runewidth=%d", m, r, lw, aw, rw)
			}
		}
	}
	t.Logf("non-1 widths: %d of 256; braille U+28FF lipgloss=%d", bad, lipgloss.Width("⣿"))
}

// Positive control: the same three calls must be able to say "2".
func TestGGWidthInstrumentSeesWide(t *testing.T) {
	const han = "漢"
	if lipgloss.Width(han) != 2 || ansi.StringWidth(han) != 2 || runewidth.RuneWidth(0x6f22) != 2 {
		t.Fatalf("width instrument cannot report 2: %d %d %d", lipgloss.Width(han), ansi.StringWidth(han), runewidth.RuneWidth(0x6f22))
	}
}
