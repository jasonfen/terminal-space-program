package screens

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// smallBodyMaxR is the largest drawn radius, in braille dots, that still
// counts as a small body for marker placement (GM Q2, #591): under the 4-dot
// true-size threshold, so a body on the floor or tier path, a few cells
// across at most.
const smallBodyMaxR = 3

// smallBodyDisk is one small body drawn this frame: where, and how big.
type smallBodyDisk struct {
	id  string
	pos orbital.Vec3
	r   int
}

// recordSmallBody notes a drawn body for this frame's marker pass. Bodies
// larger than smallBodyMaxR are not recorded: a marker over a big disk is
// one cell on a big disk and the disk survives it.
func (v *OrbitView) recordSmallBody(id string, pos orbital.Vec3, r int) {
	if r <= smallBodyMaxR {
		v.smallBodies = append(v.smallBodies, smallBodyDisk{id: id, pos: pos, r: r})
	}
}

// drawMarkerClear is drawMarker for the map: same glyph, colour and click
// tag, but a glyph that would land on a small body's cells steps off it
// (markerGlyphPos).
func (v *OrbitView) drawMarkerClear(pos orbital.Vec3, t render.MarkerType, state render.MarkerState, base lipgloss.Color, tag widgets.CellTag) {
	drawMarkerAt(v.canvas, pos, pos, t, state, base, tag)
}

// drawnBodyRadius is the radius the map draws body i at: mapBodyPixelRadius,
// except a body that sits inside its parent's drawn disk keeps its size
// without the 2-dot floor (a top-level planet's parent is the star, so
// Mercury at the whole-system view stays inside the Sun's disk too). At the whole-system view every moon is inside its
// planet's disk, and a floored moon would recolour the planet's cells with
// its own (GM Q1: Mars turned Phobos-brown, Saturn's glyph Enceladus-white).
func drawnBodyRadius(sys *bodies.System, i int, pos orbital.Vec3, w *sim.World, scale float64, canvasReach int) int {
	b := sys.Bodies[i]
	r := mapBodyPixelRadius(b, i == 0, scale, canvasReach)
	if r > minBodyDrawnRadius || i == 0 {
		return r
	}
	parent := sys.ParentOf(b)
	if parent == nil || parent.ID == b.ID {
		return r
	}
	pr := mapBodyPixelRadius(*parent, parent.ID == sys.Primary().ID, scale, canvasReach)
	if pos.Sub(w.BodyPosition(*parent)).Norm()*scale <= float64(pr) {
		return BodyPixelRadius(b, false, scale, canvasReach)
	}
	return r
}
