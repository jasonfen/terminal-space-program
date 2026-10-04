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
// angle: which way north points, and how open the orbit ring looks (the angle
// between the plane and the line of sight).
//
//	N ↑            north is up the screen (⊙ toward you, ⊗ away from you)
//	plane ◠ 67° open  (─ a flat line near 0°, ○ a full circle near 90°)
//
// North is the world +Z axis (the ecliptic pole, the same axis the Right and
// Left views stand on end). The plane is the active vessel's orbit plane;
// with no orbit (landed, escape) only the north row is drawn.

const (
	// cueInPlaneMin: below this share of its length lying in the screen
	// plane, an axis is "pointing at/away from the camera" (⊙/⊗).
	cueInPlaneMin = 0.2
	// cueFlatDeg / cueRoundDeg: the plane row's glyph bands. Under 5° the
	// ring is a flat line (─), over 85° a full circle (○), ◠ between.
	cueFlatDeg  = 5.0
	cueRoundDeg = 85.0
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
	rows = append(rows, planeCueRow(c))
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

// planeCueRow is the plane row for |normal . depth| = c: the angle the ring
// looks open, asin(c), 0 = a flat line, 90 = a full circle, with the glyph
// banded narrowly at the ends.
func planeCueRow(c float64) string {
	deg := math.Asin(math.Min(math.Abs(c), 1)) * 180 / math.Pi
	glyph := "◠"
	switch {
	case deg < cueFlatDeg:
		glyph = "─"
	case deg > cueRoundDeg:
		glyph = "○"
	}
	return fmt.Sprintf("plane %s %.0f° open", glyph, deg)
}
