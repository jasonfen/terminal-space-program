package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// forceTrueColor pins the lipgloss colour profile for the test. Without it
// `go test` renders every style as bare text and any colour assertion is
// vacuous (the UX-review lesson; see project_lipgloss_notty_vacuous_tests).
func forceTrueColor(t *testing.T) {
	t.Helper()
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })
}

var navballCellRE = regexp.MustCompile(`(?:\x1b\[([0-9;]*)m)?([^\x1b])(?:\x1b\[0m)?`)

// navballCell is one rendered cell: its glyph and the SGR parameter string
// that styled it ("" when unstyled).
type navballCell struct {
	sgr   string
	glyph string
}

func navballCells(line string) []navballCell {
	var out []navballCell
	for _, m := range navballCellRE.FindAllStringSubmatch(line, -1) {
		out = append(out, navballCell{sgr: m[1], glyph: m[2]})
	}
	return out
}

// horizonSGR is the SGR parameter string lipgloss emits for the horizon
// colour (#E6D2A0) under the TrueColor profile.
const horizonSGR = "38;2;230;210;160"

// TestNavballHorizonSanity proves the instrument: with the profile forced,
// the horizon colour string is findable in a tilted ball (where the old
// balance rule did fire) and the cell parser sees full rows.
func TestNavballHorizonSanity(t *testing.T) {
	forceTrueColor(t)
	out := NavballString(24, 12, 20, 0, nil)
	if !strings.Contains(out, horizonSGR) {
		t.Fatalf("horizon SGR %q not found in a tilted ball; instrument broken: %q", horizonSGR, out)
	}
	for i, line := range strings.Split(out, "\n") {
		if n := len(navballCells(line)); n != 24 {
			t.Fatalf("row %d parsed to %d cells, want 24", i, n)
		}
	}
}

// TestNavballHorizonIsContinuousRow: the horizon is one continuous pale
// line, independent of where the equator falls inside a cell (G8 Q4). A
// level nose (equator exactly on a cell boundary) and a 0.3 degree nose
// (equator a fraction of a dot off it, either side) must paint the SAME
// row, every occupied column of it, in the horizon colour, and no other
// row may carry that colour.
func TestNavballHorizonIsContinuousRow(t *testing.T) {
	forceTrueColor(t)
	const cols, rows = 24, 12
	for _, tc := range []struct {
		name   string
		subLat float64
	}{
		{"level", 0},
		{"nose 0.3 up", 0.3},
		{"nose 0.3 down", -0.3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lines := strings.Split(NavballString(cols, rows, tc.subLat, 0, nil), "\n")
			const horizonRow = rows / 2 // dot row 0 is the top dot of cell row 6
			for r, line := range lines {
				for c, cell := range navballCells(line) {
					isHorizon := strings.Contains(cell.sgr, horizonSGR)
					switch {
					case r == horizonRow && cell.glyph != " " && cell.glyph != "+" && !isHorizon: // the reticle may sit on the line
						t.Errorf("row %d col %d (%q) is not horizon colour: sgr=%q", r, c, cell.glyph, cell.sgr)
					case r != horizonRow && isHorizon:
						t.Errorf("row %d col %d carries horizon colour; the line must be one row (%d)", r, c, horizonRow)
					}
				}
			}
		})
	}
}

// TestNavballHorizonStableAcrossSubDegreeDrift sweeps the nose through
// +/-1 degree in 0.1 steps (the SAS-dither range): the centre row stays the
// horizon row throughout, so the line never changes colour or row between
// near-identical attitudes (the 06 vs 07 capture flicker).
func TestNavballHorizonStableAcrossSubDegreeDrift(t *testing.T) {
	forceTrueColor(t)
	for i := -10; i <= 10; i++ {
		lat := float64(i) / 10
		lines := strings.Split(NavballString(24, 12, lat, 0, nil), "\n")
		for c, cell := range navballCells(lines[6]) {
			if cell.glyph == " " || cell.glyph == "+" {
				continue
			}
			if !strings.Contains(cell.sgr, horizonSGR) {
				t.Errorf("subLat %.1f: row 6 col %d not horizon (sgr=%q)", lat, c, cell.sgr)
			}
		}
	}
}
