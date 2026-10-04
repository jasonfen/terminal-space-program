package screens

import (
	"fmt"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// B11 / G9 Q7: the Orientation Cue. Six projections of one orbit look alike
// (Right/Left and Top/Bottom are mirror pairs), so two short rows sit above
// the `view:` label and say the two things a pilot wants from a camera
// angle: which way north points, and whether the orbit plane is seen edge-on
// or face-on.
//
//	N ↑            north is up the screen (⊙ toward you, ⊗ away from you)
//	plane ─ edge-on   (○ face-on, ◠ tilted)
//
// North is the world +Z axis (the ecliptic pole, the same axis the Right and
// Left views stand on end). The plane is the active vessel's orbit plane;
// with no orbit (landed, escape) only the north row is drawn.

const (
	// cueInPlaneMin: below this share of its length lying in the screen
	// plane, an axis is "pointing at/away from the camera" (⊙/⊗).
	cueInPlaneMin = 0.2
	// cueEdgeOn / cueFaceOn: |normal·depth| thresholds for the plane row.
	// Face-on is stricter than the draft's 0.8 so the default 30° Tilted
	// view (cos 30° = 0.87) reads "tilted" rather than "face-on".
	cueEdgeOn = 0.2
	cueFaceOn = 0.95
)

// northArrows are the eight screen directions, counter-clockwise from →.
var northArrows = [8]string{"→", "↗", "↑", "↖", "←", "↙", "↓", "↘"}

// orientationCue returns the cue rows for a projection basis: the north row
// and, when the active vessel has an orbit, the plane row.
func orientationCue(b widgets.Basis, w *sim.World) []string {
	north := orbital.Vec3{Z: 1}
	nx, ny := north.Dot(b.X), north.Dot(b.Y)
	depth := north.Dot(b.DepthAxis())
	var row string
	if math.Hypot(nx, ny) < cueInPlaneMin {
		if depth >= 0 {
			row = "N ⊙"
		} else {
			row = "N ⊗"
		}
	} else {
		sector := int(math.Round(math.Atan2(ny, nx)/(math.Pi/4))+8) % 8
		row = "N " + northArrows[sector]
		// Out of the screen plane as well (the Tilted view): say by how much.
		if a := math.Abs(depth); a >= 0.1 {
			row += fmt.Sprintf(" %.0f°", math.Asin(math.Min(a, 1))*180/math.Pi)
		}
	}
	rows := []string{row}
	el, ok := activeCraftElements(w)
	if !ok {
		return rows
	}
	xHat, yHat := orbital.PerifocalBasis(el)
	c := math.Abs(xHat.Cross(yHat).Dot(b.DepthAxis()))
	switch {
	case c < cueEdgeOn:
		rows = append(rows, "plane ─ edge-on")
	case c > cueFaceOn:
		rows = append(rows, "plane ○ face-on")
	default:
		rows = append(rows, "plane ◠ tilted")
	}
	return rows
}

// stampOrientationCue writes the cue rows into the canvas's bottom-left,
// directly above the `view:` label row, in the label's colour.
func (v *OrbitView) stampOrientationCue(w *sim.World) {
	rows := orientationCue(viewBasis(w), w)
	last := v.canvas.Rows() - 1
	for i, text := range rows {
		row := last - len(rows) + i
		if row < 0 {
			continue
		}
		v.canvas.SetCellLabelColored(0, row, text, v.theme.Primary.GetForeground())
	}
}
