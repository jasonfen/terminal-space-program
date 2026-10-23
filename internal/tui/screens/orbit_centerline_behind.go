package screens

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// centerlineTitleRange is the title row's port range (ADR 0055 A6): negative
// while their port is behind the plane through yours.
func centerlineTitleRange(st sim.ProximityState) string {
	if st.PortAxialM < 0 {
		return "-" + readout.PortDistance(st.PortRangeM)
	}
	return readout.PortDistance(st.PortRangeM)
}

// drawCenterlineBehind draws the reverse glyph in place of the ring (at any
// scale) and reports true while their port is behind yours, so the caller
// skips the ring and the standoff cross. centre is where the ring would sit.
func drawCenterlineBehind(c *widgets.Canvas, st sim.ProximityState, centre orbital.Vec3) bool {
	if st.PortAxialM >= 0 {
		return false
	}
	c.SetCellOverlayColored(centre, sim.NavballGlyphAntiTarget, render.ColorTarget)
	return true
}
