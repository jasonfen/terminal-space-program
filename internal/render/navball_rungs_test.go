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

// TestNavballRungsPoleView: the ladder is a front-hemisphere feature; just
// off the pole (sub-observer lat 88.9, the last degree before the pole hides them) the rungs
// ring below the centre and none is drawn above it.
func TestNavballRungsPoleView(t *testing.T) {
	forceTrueColor(t)
	out := NavballString(24, 12, 88.9, -90, nil)
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

// rowGlyphs joins a rendered row's cell glyphs.
func rowGlyphs(out string, row int) string {
	var sb strings.Builder
	for _, c := range navballCells(strings.Split(out, "\n")[row]) {
		sb.WriteString(c.glyph)
	}
	return sb.String()
}

func isDigit(g string) bool { return len(g) == 1 && g[0] >= '0' && g[0] <= '9' }

// TestNavballRungNeverShowsHalfNumber (wave C review, MEDIUM): when a marker
// takes one of a rung's two digit cells the surviving digit would read as a
// different number (`6△`, `4⊕`, `1E`). Sweep a marker across the +60 rung's
// cells and require that every row shows either both digits or none.
func TestNavballRungNeverShowsHalfNumber(t *testing.T) {
	forceTrueColor(t)
	hits := 0
	for lon := -100.0; lon <= -80.0; lon += 0.5 {
		for _, lat := range []float64{58, 60, 62} {
			m := NavballMarker{LatDeg: lat, LonDeg: lon, Glyph: 'E', Color: ColorNavballMarkerPrograde}
			out := NavballString(24, 12, 30, -90, []NavballMarker{m})
			for r := range strings.Split(out, "\n") {
				cells := navballCells(strings.Split(out, "\n")[r])
				for c, cell := range cells {
					if !isDigit(cell.glyph) {
						continue
					}
					l := c > 0 && isDigit(cells[c-1].glyph)
					rr := c+1 < len(cells) && isDigit(cells[c+1].glyph)
					if !l && !rr {
						t.Fatalf("marker lat %g lon %g: row %d col %d lone digit %q in %q", lat, lon, r, c, cell.glyph, rowGlyphs(out, r))
					}
				}
			}
			if r3 := rowGlyphs(out, 3); strings.ContainsRune(r3, 'E') {
				hits++
			}
		}
	}
	if hits == 0 {
		t.Fatal("instrument check: the marker never landed on row 3; sweep proves nothing")
	}
}

// TestNavballRungsSymmetricAtLevel (wave C review, LOW): at a level nose the
// ladder reads the same numbers above and below the horizon; a row shared by
// two rungs goes to the one nearer the horizon on both sides.
func TestNavballRungsSymmetricAtLevel(t *testing.T) {
	forceTrueColor(t)
	out := NavballString(24, 12, 0, -90, nil)
	above, below := map[string]bool{}, map[string]bool{}
	for r := range strings.Split(out, "\n") {
		cells := navballCells(strings.Split(out, "\n")[r])
		for c := 0; c+1 < len(cells); c++ {
			if isDigit(cells[c].glyph) && isDigit(cells[c+1].glyph) {
				n := cells[c].glyph + cells[c+1].glyph
				if r < 6 {
					above[n] = true
				} else {
					below[n] = true
				}
			}
		}
	}
	// The horizon row takes one of the 12 rows, so below has one row fewer
	// than above: exact mirroring is impossible, but a rung nearer the
	// horizon must never lose its row to a farther one on only one side.
	for _, n := range []string{"10", "20", "30"} {
		if !above[n] || !below[n] {
			t.Errorf("rung %s above=%v below=%v at level, want both (above %v below %v)", n, above[n], below[n], above, below)
		}
	}
	for n := range below {
		if !above[n] {
			t.Errorf("rung %s drawn below the horizon only (above %v below %v)", n, above, below)
		}
	}
}

// TestNavballRungsHiddenAtPole (wave C review, LOW): within RungPoleHideDeg
// of the pole the rungs are concentric circles and carry no information, so
// none is drawn; one step off the pole the ladder returns.
func TestNavballRungsHiddenAtPole(t *testing.T) {
	forceTrueColor(t)
	for _, lat := range []float64{90, 89.5, -90} {
		if out := NavballString(24, 12, lat, -90, nil); strings.Contains(out, "─") {
			t.Errorf("sub-observer lat %g draws rungs (%d dashes)", lat, strings.Count(out, "─"))
		}
	}
	if out := NavballString(24, 12, 80, -90, nil); !strings.Contains(out, "─") {
		t.Error("sub-observer lat 80 should still draw the ladder (instrument check)")
	}
}
