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
// (markerGlyphPos). ADR 0041 §3 amendment (GM Q2, #591): the one exception
// to "draw order decides, no arbitration".
func (v *OrbitView) drawMarkerClear(pos orbital.Vec3, t render.MarkerType, state render.MarkerState, base lipgloss.Color, tag widgets.CellTag) {
	drawMarkerAt(v.canvas, pos, v.markerGlyphPos(pos), t, state, base, tag)
}

// markerGlyphPos is where the glyph of a marker whose true point is pos is
// drawn. Off every small body it is pos itself. On one it is the cell just
// outside the body's cells, on the side the marker was already on (the
// horizontal side when the offset is wider than tall in cell terms, else the
// vertical side; dead centre goes up and to the right). The body keeps its
// own cell, and only the glyph moves: the click target stays at pos.
func (v *OrbitView) markerGlyphPos(pos orbital.Vec3) orbital.Vec3 {
	px, py, ok := v.canvas.Project(pos)
	if !ok {
		return pos
	}
	mc, mr := px/2, py/4
	for _, s := range v.smallBodies {
		bx, by, on := v.canvas.Project(s.pos)
		if !on {
			continue
		}
		c0, c1 := floorDiv(bx-s.r, 2), floorDiv(bx+s.r, 2)
		r0, r1 := floorDiv(by-s.r, 4), floorDiv(by+s.r, 4)
		if mc < c0 || mc > c1 || mr < r0 || mr > r1 {
			continue
		}
		dx, dy := px-bx, py-by
		col, row := mc, mr
		switch {
		case dx == 0 && dy == 0:
			col, row = c1+1, r0-1
		case 2*absInt(dx) >= absInt(dy): // |dx|/2 >= |dy|/4, in cells
			if dx > 0 {
				col = c1 + 1
			} else {
				col = c0 - 1
			}
		default:
			if dy < 0 {
				row = r0 - 1
			} else {
				row = r1 + 1
			}
		}
		if col < 0 || col >= v.canvas.Cols() || row < 0 || row >= v.canvas.Rows() {
			return pos // no room to step off: the marker keeps its cell
		}
		return v.canvas.Unproject(col*2+1, row*4+2)
	}
	return pos
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
