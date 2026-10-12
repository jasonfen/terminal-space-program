package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// moonInView renders the default system framed on Earth, zoomed out until the
// Moon is on the canvas as a small disk, and returns the view with the Moon's
// projected pixel position and drawn radius.
func moonInView(t *testing.T) (v *OrbitView, w *sim.World, moonPos orbital.Vec3, px, py, r int) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := bodyByID(t, w, "earth")
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: idx}
	w.ViewMode = sim.ViewTop
	v = NewOrbitView(plainTheme())
	v.Resize(DesignWidth, DesignHeight)
	v.declutter = true
	v.Render(w, 0, DesignWidth, DesignHeight)
	_, moon := bodyByID(t, w, "moon")
	moonPos = w.BodyPosition(moon)
	for i := 0; i < 20; i++ {
		if _, _, on := v.canvas.Project(moonPos); on {
			break
		}
		v.ZoomOut()
		v.Render(w, 0, DesignWidth, DesignHeight)
	}
	v.Render(w, 0, DesignWidth, DesignHeight)
	m, ok := smallBodyNamed(v, "moon")
	if !ok {
		t.Fatal("setup: the Moon is not a small disk in this frame")
	}
	var on bool
	px, py, on = v.canvas.Project(moonPos)
	if !on {
		t.Fatal("setup: the Moon is off the canvas")
	}
	return v, w, moonPos, px, py, m.r
}

// glyphCell finds the cell holding rune g in the canvas string.
func glyphCell(v *OrbitView, g rune) (col, row int, found bool) {
	for y, line := range strings.Split(ansi.Strip(v.canvas.String()), "\n") {
		for x, ch := range []rune(line) {
			if ch == g {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

// A marker the size of a cell erased the small body it marked (2026-09-02
// review, appeal 28-29: the red ⊕ sat on the Moon and no Moon was left). When
// a marker would land on a small body it steps one cell out from the body's
// edge, on the side it was already on (GM Q2). Seven marker kinds, eight
// approach directions each: none may touch the disk's cells.
func TestMarkerNeverCoversASmallBody(t *testing.T) {
	kinds := []render.MarkerType{render.MarkerPerilune, render.MarkerClosestApproach, render.MarkerSOIEntry,
		render.MarkerSOIExit, render.MarkerApoapsis, render.MarkerPeriapsis, render.MarkerManeuver}
	offsets := [][2]int{{0, 0}, {-1, 0}, {1, 0}, {0, -1}, {0, 1}, {-2, -3}, {2, 3}, {-2, 3}, {2, -3}}
	for _, kind := range kinds {
		for _, off := range offsets {
			v, _, moonPos, px, py, r := moonInView(t)
			g := render.MarkerGlyph(kind)
			if _, _, found := glyphCell(v, g); found {
				t.Fatalf("setup: %c is already on the canvas before the marker is drawn", g)
			}
			// A true point on the Moon, nudged by off pixels.
			at := v.canvas.Unproject(px+off[0], py+off[1])
			_ = moonPos
			v.drawMarkerClear(at, kind, render.MarkerNominal, "", widgets.CellTag{})
			col, row, found := glyphCell(v, g)
			if !found {
				t.Fatalf("%c with true point %+v: marker not drawn", g, off)
			}
			c0, c1 := floorDiv(px-r, 2), floorDiv(px+r, 2)
			r0, r1 := floorDiv(py-r, 4), floorDiv(py+r, 4)
			if col >= c0 && col <= c1 && row >= r0 && row <= r1 {
				t.Errorf("%c (offset %+v) landed at cell (%d,%d), inside the Moon's cells cols %d-%d rows %d-%d", g, off, col, row, c0, c1, r0, r1)
			}
			// One cell out, not a long way off.
			if col < c0-1 || col > c1+1 || row < r0-1 || row > r1+1 {
				t.Errorf("%c (offset %+v) stepped to (%d,%d), more than one cell from the Moon's cells cols %d-%d rows %d-%d", g, off, col, row, c0, c1, r0, r1)
			}
			// The body keeps its own cell: its glyph is still there.
			if _, _, moonGlyph := glyphCell(v, '○'); !moonGlyph {
				t.Errorf("%c (offset %+v) erased the Moon's own ○", g, off)
			}
			// The marker keeps its own colour.
			if v.canvas.CountOverlayColor(render.MarkerColor(kind, render.MarkerNominal, "")) == 0 {
				t.Errorf("%c lost its own colour", g)
			}
		}
	}
}

// The click target steps off with the glyph (review #629 M2, superseding the
// GM Q2 "stays at the true point"): the drawn glyph's cell resolves to the
// marker's node, and the small body's own cell no longer does.
func TestMarkerOnSmallBodyClicksWhereItIsDrawn(t *testing.T) {
	v, _, _, px, py, _ := moonInView(t)
	at := v.canvas.Unproject(px, py)
	v.drawMarkerClear(at, render.MarkerManeuver, render.MarkerNominal, "", widgets.CellTag{NodeIdx: 7})
	col, row, found := glyphCell(v, render.MarkerGlyph(render.MarkerManeuver))
	if !found {
		t.Fatal("marker not drawn")
	}
	if col == px/2 && row == py/4 {
		t.Fatal("setup: the glyph did not step off the body's cell")
	}
	if hit := v.canvas.HitAt(col, row); hit.NodeIdx != 7 {
		t.Errorf("click on the drawn glyph at (%d,%d) resolves to node %d, want 7", col, row, hit.NodeIdx)
	}
	if hit := v.canvas.HitAt(px/2, py/4); hit.NodeIdx != 0 {
		t.Errorf("click on the small body's cell still resolves to node %d, want the body", hit.NodeIdx)
	}
}

// A marker over a body that is big on screen stays where it is: the disk
// survives one cell of glyph.
func TestMarkerOverABigBodyStays(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	idx, earth := bodyByID(t, w, "earth")
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: idx}
	w.ViewMode = sim.ViewTop
	v := NewOrbitView(plainTheme())
	v.Resize(DesignWidth, DesignHeight)
	v.declutter = true
	v.Render(w, 0, DesignWidth, DesignHeight)
	px, py, on := v.canvas.Project(w.BodyPosition(earth))
	if !on {
		t.Fatal("setup: Earth is off the canvas")
	}
	if _, ok := smallBodyNamed(v, "earth"); ok {
		t.Fatal("setup: Earth counts as a small body in this frame")
	}
	v.drawMarkerClear(v.canvas.Unproject(px, py), render.MarkerPerilune, render.MarkerNominal, "", widgets.CellTag{})
	col, row, found := glyphCell(v, render.MarkerGlyph(render.MarkerPerilune))
	if !found || col != px/2 || row != py/4 {
		t.Errorf("marker over Earth's centre at (%d,%d) found=%v, want it to stay at (%d,%d)", col, row, found, px/2, py/4)
	}
}
