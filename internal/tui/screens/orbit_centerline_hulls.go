package screens

import (
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// drawCenterlineHulls is S2's hook (ADR 0055 A5): both hulls end-on, drawn on
// the scope canvas c before the ring and the cross so they win shared cells.
// scale is dots per metre. Empty until S2.
func (v *OrbitView) drawCenterlineHulls(c *widgets.Canvas, w *sim.World, st sim.ProximityState, scale float64) {
}
