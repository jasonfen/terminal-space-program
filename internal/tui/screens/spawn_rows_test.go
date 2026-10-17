package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// catalogPlainRows renders the real CRAFT TYPE catalog (craftTypeLines, the
// seam Render uses) at the given width and returns the colourless text of
// every catalog row, headers and the Custom entry excluded.
func catalogPlainRows(t *testing.T, th Theme, width int) (plain, raw []string) {
	t.Helper()
	s := NewSpawnCraft(th)
	s.showAll = true
	lines, _ := s.craftTypeLines(width)
	for _, l := range lines {
		if l.IsHeader {
			continue
		}
		p := ansiRE.ReplaceAllString(l.Text, "")
		if strings.Contains(p, "Custom…") {
			continue
		}
		plain = append(plain, p)
		raw = append(raw, l.Text)
	}
	if len(plain) < 10 {
		t.Fatalf("catalog rendered only %d rows; instrument is not seeing the catalog", len(plain))
	}
	return plain, raw
}

// cellOffset is the display-cell offset of sub in row, or -1 (lipgloss.Width,
// not byte offset, so glyphs like the ▸ marker count right).
func cellOffset(row, sub string) int {
	i := strings.Index(row, sub)
	if i < 0 {
		return -1
	}
	return lipgloss.Width(row[:i])
}

// TestSpawnRowsColumnTheirNumbers: dry mass, fuel mass and the engine figure
// each start at one cell, row to row, at both the compact (140x40) and the
// roomy (181x49) catalog widths (#595).
func TestSpawnRowsColumnTheirNumbers(t *testing.T) {
	for _, width := range []int{90, 130} {
		plain, _ := catalogPlainRows(t, Theme{}, width)
		// Each marker must be seen on at least two rows (the instrument can
		// return a positive) and sit at one offset on all of them.
		for _, marker := range []string{"dry ", "fuel ", "kN @"} {
			seen := map[int][]string{}
			for _, row := range plain {
				if off := cellOffset(row, marker); off >= 0 {
					seen[off] = append(seen[off], strings.TrimSpace(row))
				}
			}
			total := 0
			for _, rows := range seen {
				total += len(rows)
			}
			if total < 2 {
				t.Fatalf("width %d: marker %q found on %d rows; instrument cannot see the column", width, marker, total)
			}
			if len(seen) != 1 {
				t.Errorf("width %d: %q starts at %d different offsets, want 1: %v", width, marker, len(seen), seen)
			}
		}
	}
}

// TestSpawnRowTailIsDim: the descriptive tail (stage count and role) is the
// Dim style and the name/figures are not (#595). Colour forced; the Dim and
// Primary styles are given distinct colours so the instrument can tell them.
func TestSpawnRowTailIsDim(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#112233"))
	pri := lipgloss.NewStyle().Foreground(lipgloss.Color("#445566"))
	th := Theme{Dim: dim, Primary: pri, Warning: pri}

	// Positive first: the instrument sees the Dim SGR prefix on a Dim run.
	dimOpen := strings.TrimSuffix(dim.Render("x"), "x\x1b[0m")
	if !strings.Contains(dimOpen, "38;2;17;34;51") {
		t.Fatalf("colour not forced: Dim.Render(x) = %q", dim.Render("x"))
	}

	plain, raw := catalogPlainRows(t, th, 130)
	checked := 0
	for i, p := range plain {
		if !strings.Contains(p, "Falcon 9") || strings.Contains(p, "Dragon") {
			continue // the plain Falcon 9 row; the Dragon stacks carry 4 stages
		}
		checked++
		r := raw[i]
		tailAt := strings.Index(r, "2 stages")
		if tailAt < 0 {
			t.Fatalf("Falcon 9 row has no stage-count tail: %q", p)
		}
		if !strings.HasSuffix(r[:tailAt], dimOpen) {
			t.Errorf("tail %q does not start a Dim run; row=%q", "2 stages", r)
		}
		nameAt := strings.Index(r, "Falcon 9")
		if strings.HasSuffix(r[:nameAt], dimOpen) {
			t.Errorf("name is Dim; only the tail should be: %q", r)
		}
	}
	if checked != 1 {
		t.Fatalf("expected exactly one Falcon 9 row, saw %d", checked)
	}
}
