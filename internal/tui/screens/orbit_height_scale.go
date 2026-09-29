package screens

import "github.com/jasonfen/terminal-space-program/internal/bodies"

// designCanvasRows is the canvas height of the Design Size (140x40 terminal
// minus 3 chrome rows). Height-scaled sizes are normalised to it.
const designCanvasRows = 37

// designCanvasCols is the canvas width of the Design Size (140 less the two border columns).
const designCanvasCols = 138

// heightScaledCells scales a size given in cells or pixels at the Design
// Size (base, valid at 37 canvas rows) to a canvas canvasRows tall.
//
// Contract:
//   - Normalised: at canvasRows == 37 it returns base exactly, so 140x40
//     never changes. About 1.25x at 46 rows (181x49).
//   - Whole units: the result is rounded to an integer, so a one-row resize
//     moves it by 0 or 1 unit at most and never in fractional steps.
//   - Monotone, and never below base: a terminal shorter than the Design
//     Size does not shrink anything (below 40 rows is out of scope).
//
// Shared by the planet-with-moons texture floor (orbit.go) and, later, the
// navball disk and panel sizing.
func heightScaledCells(base, canvasRows int) int {
	if canvasRows <= designCanvasRows {
		return base
	}
	// Round half up in integer arithmetic.
	return (base*canvasRows*2 + designCanvasRows) / (designCanvasRows * 2)
}

// bodyHasChildren reports whether any body in the system orbits b. Mirrors
// the terminal-body test in sim.FocusZoomRadius (children keep the SOI fit).
func bodyHasChildren(all []bodies.CelestialBody, b bodies.CelestialBody) bool {
	for _, o := range all {
		if o.ParentID == b.ID {
			return true
		}
	}
	return false
}
