package render

import (
	"strings"
	"testing"
)

const gridSGR = "38;2;200;200;200" // ColorNavballGrid #C8C8C8

func cellAt(t *testing.T, out string, row, col int) navballCell {
	t.Helper()
	lines := strings.Split(out, "\n")
	cells := navballCells(lines[row])
	if col >= len(cells) {
		t.Fatalf("row %d has %d cells, want col %d", row, len(cells), col)
	}
	return cells[col]
}

// TestNavballRungsNumbered10: with the nose 30 degrees up (sub-observer
// lat 30) on the 24x12 ball, the rungs sit one per row at the foreshortened
// positions: +60 on row 3, +50 on row 4, +40 on row 5, +20 on row 7, +10 on
// row 8. Each is a two-digit number on the two cells straddling the centre
// vertical (cols 11, 12) in the grid grey, flanked by dashes.
func TestNavballRungsNumbered10(t *testing.T) {
	forceTrueColor(t)
	out := NavballString(24, 12, 30, -90, nil)
	for _, tc := range []struct {
		row    int
		digits string
	}{{3, "60"}, {4, "50"}, {5, "40"}, {7, "20"}, {8, "10"}} {
		a, b := cellAt(t, out, tc.row, 11), cellAt(t, out, tc.row, 12)
		if a.glyph+b.glyph != tc.digits {
			t.Errorf("row %d cols 11-12 = %q%q, want rung %q", tc.row, a.glyph, b.glyph, tc.digits)
			continue
		}
		if !strings.Contains(a.sgr, gridSGR) || !strings.Contains(b.sgr, gridSGR) {
			t.Errorf("row %d rung digits not grid grey: %q %q", tc.row, a.sgr, b.sgr)
		}
		if g := cellAt(t, out, tc.row, 9).glyph; g != "─" {
			t.Errorf("row %d col 9 = %q, want a rung dash", tc.row, g)
		}
	}
	// Ten-degree spacing: one rung per row means no row carries two numbers
	// and consecutive rungs sit on consecutive rows.
	if strings.Count(out, "─") == 0 {
		t.Fatal("no rung dashes at all")
	}
}

// TestNavballRungYieldsToHorizon: a rung that would land on the horizon
// line's row is dropped, so the line stays whole (sub-observer lat 30 puts
// the -10 rung on the horizon row, row 9).
func TestNavballRungYieldsToHorizon(t *testing.T) {
	forceTrueColor(t)
	out := NavballString(24, 12, 30, -90, nil)
	cells := navballCells(strings.Split(out, "\n")[9])
	for _, c := range []int{10, 11, 12, 13} {
		if !strings.Contains(cells[c].sgr, horizonSGR) {
			t.Errorf("row 9 col %d (%q) should be the unbroken horizon line, sgr=%q", c, cells[c].glyph, cells[c].sgr)
		}
	}
}

// TestNavballMarkerWinsRungCell: a glyph on a rung cell replaces the rung
// cell (markers win a contested cell). The +60 rung digit cell (row 3,
// col 11) sits at lat 60 on the centre meridian.
func TestNavballMarkerWinsRungCell(t *testing.T) {
	forceTrueColor(t)
	m := NavballMarker{LatDeg: 60, LonDeg: -90, Glyph: '⊕', Color: ColorNavballMarkerPrograde}
	out := NavballString(24, 12, 30, -90, []NavballMarker{m})
	hit := false
	for c, cell := range navballCells(strings.Split(out, "\n")[3]) {
		if cell.glyph == "⊕" {
			hit = true
			if c != 11 && c != 12 {
				t.Errorf("marker at col %d, want it on the rung's digit cells 11-12", c)
			}
		}
	}
	if !hit {
		t.Fatal("marker glyph missing from the contested rung cell")
	}
	// The other rung digit survives, the marker cell is not a digit.
	a, b := cellAt(t, out, 3, 11), cellAt(t, out, 3, 12)
	if a.glyph == "6" && b.glyph == "0" {
		t.Error("rung digits intact, marker did not win")
	}
}

// TestNavballRungsSurviveBackFace: the ladder is a front-hemisphere
// feature; at the pole view (sub-observer lat 90, the pad) the rungs ring
// below the centre and none is drawn above it.
func TestNavballRungsPoleView(t *testing.T) {
	forceTrueColor(t)
	out := NavballString(24, 12, 90, -90, nil)
	for r, line := range strings.Split(out, "\n") {
		has := strings.Contains(line, "─")
		if r <= 6 && has {
			t.Errorf("row %d (at/above centre) carries a rung at the pole view: %q", r, line)
		}
	}
	a, b := cellAt(t, out, 7, 11), cellAt(t, out, 7, 12)
	if a.glyph+b.glyph != "80" {
		t.Errorf("row 7 cols 11-12 = %q%q, want the 80 rung just below the centre", a.glyph, b.glyph)
	}
}
