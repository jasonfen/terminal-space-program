package screens

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
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
