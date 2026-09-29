// navball_scale_test.go (#482 S3, ADR 0051 W5): the navball disk and panel
// scale with canvas height, stay put at the Design Size, and never grow
// into the rows NAVIGATION + TARGET can occupy at full height.

package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Canvas rows are terminal rows minus 3 chrome rows (140x40 -> 37,
// 181x49 -> 46, 181x70 -> 67).
func TestNavballGeometryPinnedSizes(t *testing.T) {
	cases := []struct {
		canvasRows         int
		diskCols, diskRows int
		panelW, panelH     int
	}{
		{designCanvasRows, 24, 12, 34, 19}, // 140x40: byte-for-byte today's panel
		{38, 24, 12, 34, 19},
		{46, 30, 15, 40, 22}, // 181x49
		{67, 44, 22, 54, 29}, // 181x70
	}
	for _, c := range cases {
		g := navballGeometry(c.canvasRows)
		if g.diskCols != c.diskCols || g.diskRows != c.diskRows || g.panelW != c.panelW || g.panelH != c.panelH {
			t.Errorf("canvas %d rows: disk %dx%d panel %dx%d, want disk %dx%d panel %dx%d",
				c.canvasRows, g.diskCols, g.diskRows, g.panelW, g.panelH,
				c.diskCols, c.diskRows, c.panelW, c.panelH)
		}
	}
}

// Cells are about 2:1, so the disk stays round only while cols == 2 x rows.
// The scale is monotone, in whole cells, and never below the Design Size.
func TestNavballGeometryShape(t *testing.T) {
	prev := navballGeometry(designCanvasRows)
	for r := designCanvasRows; r <= 140; r++ {
		g := navballGeometry(r)
		if g.diskCols != 2*g.diskRows {
			t.Fatalf("canvas %d rows: disk %dx%d is not 2:1", r, g.diskCols, g.diskRows)
		}
		if g.diskRows < prev.diskRows || g.diskRows-prev.diskRows > 1 {
			t.Fatalf("canvas %d rows: disk rows %d after %d, want monotone steps of 0 or 1", r, g.diskRows, prev.diskRows)
		}
		if g.diskRows < navballBaseDiskRows {
			t.Fatalf("canvas %d rows: disk shrank below the Design Size", r)
		}
		prev = g
	}
}

// The cap: the panel top always sits below the full-height NAVIGATION +
// TARGET stack. The real scaler never reaches the cap at any height
// (the panel grows slower than the canvas), so the binding case is driven
// through navballGeometryCapped with a tighter budget, and the real
// composition is checked at every height in TestNavballClearsFullRightStack.
func TestNavballGeometryCapBinds(t *testing.T) {
	const canvasRows = 67 // would scale to a 22-row disk, 29-row panel
	free := navballGeometry(canvasRows)
	for _, max := range []int{29, 25, 22, 19} {
		g := navballGeometryCapped(canvasRows, max)
		if g.panelH > max {
			t.Errorf("cap %d: panel is %d rows tall, over the cap", max, g.panelH)
		}
	}
	if g := navballGeometryCapped(canvasRows, 22); g.panelH >= free.panelH {
		t.Errorf("a 22-row cap left the panel at %d rows (uncapped %d)", g.panelH, free.panelH)
	}
	// Never below the Design Size disk even when the cap is tighter still.
	if g := navballGeometryCapped(canvasRows, 5); g.diskRows != navballBaseDiskRows {
		t.Errorf("tiny cap shrank the disk to %d rows", g.diskRows)
	}
	// The real budget is derived from the boxes' max line counts.
	want := (navigationBoxMaxLines + 2) + chipGap + (targetBoxMaxLines + 2)
	if got := navballFullRightStackRows(); got != want {
		t.Errorf("full right stack = %d rows, want %d", got, want)
	}
}

func navballTestView(t *testing.T, termW, termH int, mode settings.EmptyReadingsMode) (*OrbitView, *sim.World) {
	t.Helper()
	v := NewOrbitView(chipTestTheme())
	s := settings.Default()
	s.SetEmptyReadings(mode)
	v.SetSettings(s)
	v.Resize(termW, termH)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return v, w
}

// panelTopScreenRow is the screen row of the navball panel's top border,
// read back from the Mode toggle's hit box (panel row 1, border above it).
func panelTopScreenRow(t *testing.T, v *OrbitView) int {
	t.Helper()
	for _, b := range v.navballControls {
		if b.id == NavballControlMode {
			return b.row - 1
		}
	}
	t.Fatal("navball not drawn: no Mode control recorded")
	return 0
}

// Under Full (both right boxes at max lines) the navball panel top sits
// below TARGET's bottom border at every height from the Design Size up,
// and its size does not depend on the target. Terminal-vs-canvas rows is
// the trap here: v.Resize takes terminal rows.
func TestNavballClearsFullRightStack(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}, {181, 60}, {181, 70}, {200, 100}, {240, 130}} {
		v, w := navballTestView(t, sz[0], sz[1], settings.EmptyFull)
		w.Target = sim.Target{Kind: sim.TargetNone}
		_ = v.Render(w, 0, sz[0], sz[1])
		var target *chipRect
		for i := range v.chipRects {
			if v.chipRects[i].id == settings.ChipTarget {
				target = &v.chipRects[i]
			}
		}
		if target == nil {
			t.Fatalf("%dx%d: Full must draw TARGET with no target", sz[0], sz[1])
		}
		if top := panelTopScreenRow(t, v); top <= target.rowEnd {
			t.Errorf("%dx%d: navball top row %d is not below TARGET's bottom row %d", sz[0], sz[1], top, target.rowEnd)
		}
		canvasRows := sz[1] - 3
		g := navballGeometry(canvasRows)
		if g.panelH > navballMaxPanelRows(canvasRows) {
			t.Errorf("%dx%d: panel %d rows exceeds the budget %d", sz[0], sz[1], g.panelH, navballMaxPanelRows(canvasRows))
		}
	}
}

// Pressing t (a target appearing) and switching Empty readings never
// resize or move the navball: same hit boxes in every state.
func TestNavballIgnoresTargetAndEmptyReadings(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}, {181, 70}} {
		var ref []navballControlBox
		for i, mode := range []settings.EmptyReadingsMode{settings.EmptyFull, settings.EmptyTidy, settings.EmptyCompact} {
			for _, withTarget := range []bool{false, true} {
				v, w := navballTestView(t, sz[0], sz[1], mode)
				if withTarget {
					w.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: 0}
				} else {
					w.Target = sim.Target{Kind: sim.TargetNone}
				}
				_ = v.Render(w, 0, sz[0], sz[1])
				got := append([]navballControlBox(nil), v.navballControls...)
				if len(got) == 0 {
					t.Fatalf("%dx%d: navball not drawn", sz[0], sz[1])
				}
				if i == 0 && !withTarget {
					ref = got
					continue
				}
				if len(got) != len(ref) {
					t.Fatalf("%dx%d mode %v target %v: %d controls, want %d", sz[0], sz[1], mode, withTarget, len(got), len(ref))
				}
				for k := range got {
					if got[k] != ref[k] {
						t.Fatalf("%dx%d mode %v target %v: control %d moved: %+v vs %+v", sz[0], sz[1], mode, withTarget, k, got[k], ref[k])
					}
				}
			}
		}
	}
}

// Hit boxes follow the scaled panel: every body row is a click target for
// exactly one button, the eight buttons appear in order and each owns at
// least 2 rows, and a click at each box's centre resolves to it.
func TestNavballScaledHitBoxes(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}, {181, 70}} {
		v, w := navballTestView(t, sz[0], sz[1], settings.EmptyTidy)
		_ = v.Render(w, 0, sz[0], sz[1])
		g := navballGeometry(sz[1] - 3)
		top := panelTopScreenRow(t, v)
		rowsOf := map[NavballControlID][]int{}
		for _, b := range v.navballControls {
			mid := (b.colStart + b.colEnd) / 2
			if id, ok := v.HitNavballControl(mid, b.row); !ok || id != b.id {
				t.Errorf("%dx%d: centre of %d at (%d,%d) resolved to %d ok=%v", sz[0], sz[1], b.id, mid, b.row, id, ok)
			}
			if b.colEnd-1 >= sz[0]-1 || b.colStart < sz[0]-g.panelW {
				t.Errorf("%dx%d: box %d cols [%d,%d) outside the panel", sz[0], sz[1], b.id, b.colStart, b.colEnd)
			}
			rowsOf[b.id] = append(rowsOf[b.id], b.row)
		}
		bodyStart := top + 2 // border, toggle row
		next := bodyStart
		for _, ax := range navballAxisRow {
			rows := rowsOf[ax.id]
			if len(rows) < navballBtnRows {
				t.Errorf("%dx%d: button %s owns %d rows, want >= %d", sz[0], sz[1], ax.label, len(rows), navballBtnRows)
			}
			for _, r := range rows {
				if r != next {
					t.Errorf("%dx%d: button %s row %d, want contiguous row %d", sz[0], sz[1], ax.label, r, next)
				}
				next++
			}
		}
		if next != bodyStart+g.bodyRows {
			t.Errorf("%dx%d: buttons cover %d body rows, want %d", sz[0], sz[1], next-bodyStart, g.bodyRows)
		}
	}
}

// The notice bay's right bound follows the panel width: at 181x49 the
// bay clears the (wider) navball's left edge.
func TestBayClearsScaledNavball(t *testing.T) {
	for _, cRows := range []int{designCanvasRows, 46, 67} {
		v := NewOrbitView(chipTestTheme())
		const cCols = 179
		navballReserved := navballGeometry(cRows).panelH + 1
		chips := []builtChip{
			{corner: cornerTopLeft, lines: []string{"ENGINE", "  a", "  b"}, priority: chipPriorityCore},
			{corner: cornerBay, lines: []string{"SOI PASS", "  body: Moon"}},
		}
		v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, navballReserved, 0, 0, chips)
		if len(v.chipRects) != 2 {
			t.Fatalf("%d rows: recorded %d rects, want 2", cRows, len(v.chipRects))
		}
		navballLeft := cCols - navballGeometry(cRows).panelW
		bay := v.chipRects[1]
		if bay.colEnd >= navballLeft {
			t.Errorf("%d rows: bay colEnd %d does not clear the navball's left edge %d", cRows, bay.colEnd, navballLeft)
		}
	}
}

// Markers and button labels still sit right on the bigger disk: a marker
// at the sub-observer lands on the disk's centre cell, one 90 degrees up
// lands on the top row, every SAS glyph and label is present, and each
// panel row is the declared width.
func TestNavballScaledDiskMarkersAndLabels(t *testing.T) {
	v := NewOrbitView(Theme{Primary: lipgloss.NewStyle(), Dim: lipgloss.NewStyle(), Warning: lipgloss.NewStyle()})
	for _, canvasRows := range []int{designCanvasRows, 46, 67} {
		g := navballGeometry(canvasRows)
		markers := []render.NavballMarker{
			{LatDeg: 0, LonDeg: 0, Glyph: 'X'},
			{LatDeg: 90, LonDeg: 0, Glyph: 'U'},
		}
		disk := render.NavballString(g.diskCols, g.diskRows, 0, 0, markers)
		diskRows := strings.Split(stripANSI(disk), "\n")
		if len(diskRows) != g.diskRows {
			t.Fatalf("%d rows: disk has %d rows, want %d", canvasRows, len(diskRows), g.diskRows)
		}
		if got := []rune(diskRows[g.diskRows/2])[g.diskCols/2]; got != 'X' {
			t.Errorf("%d rows: centre marker landed as %q, want X", canvasRows, got)
		}
		if !strings.ContainsRune(diskRows[0], 'U') {
			t.Errorf("%d rows: 90 degree marker not on the top row: %q", canvasRows, diskRows[0])
		}
		panel, _ := v.buildNavballPanel(g, disk, sim.NavOrbit, false, false)
		plain := stripANSI(panel)
		for _, ax := range navballAxisRow {
			if !strings.ContainsRune(plain, ax.glyph) || !strings.Contains(plain, ax.label) {
				t.Errorf("%d rows: panel missing %c %s", canvasRows, ax.glyph, ax.label)
			}
		}
		lines := strings.Split(panel, "\n")
		if len(lines) != g.panelH {
			t.Errorf("%d rows: panel %d rows, want %d", canvasRows, len(lines), g.panelH)
		}
		for i, l := range lines {
			if lipgloss.Width(l) != g.panelW || len(splitStyledCells(l)) != g.panelW {
				t.Errorf("%d rows: panel row %d is %d wide, want %d", canvasRows, i, lipgloss.Width(l), g.panelW)
			}
		}
	}
}
