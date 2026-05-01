package render

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// AtmosphereMinHaloPx is the minimum pixel gap between the body's
// rendered edge and the haze ring. The physical atmosphere shell
// (cutoff + scale-height) is only a couple percent of a planet's
// radius, so without this floor the haze ring overlaps the body's
// own outline at every reasonable zoom and is invisible. With the
// floor the haze always reads as a thin halo just outside the disk.
const AtmosphereMinHaloPx = 2

// AtmosphereOuterMeters returns the distance from the body's center
// at which the haze ring is drawn. The shell sits at
// (R_body + CutoffAltitude + ScaleHeight) so it's slightly above the
// drag-cutoff altitude — visually marks "this is where the
// atmosphere fades to vacuum." Returns 0 when no atmosphere.
func AtmosphereOuterMeters(b bodies.CelestialBody) float64 {
	atm := b.Atmosphere
	if atm == nil {
		return 0
	}
	return b.RadiusMeters() + atm.CutoffAltitude + atm.ScaleHeight
}

// AtmosphereOuterPx projects AtmosphereOuterMeters into canvas pixels
// at the given scale (px/m), then floors the result to bodyPxRadius +
// AtmosphereMinHaloPx so the ring is always at least a couple pixels
// outside the body's rendered edge. Returns 0 when haze shouldn't
// render (no atmosphere or zero scale).
func AtmosphereOuterPx(b bodies.CelestialBody, scale float64, bodyPxRadius int) int {
	outer := AtmosphereOuterMeters(b)
	if outer <= 0 || scale <= 0 {
		return 0
	}
	physical := int(outer * scale)
	if floor := bodyPxRadius + AtmosphereMinHaloPx; physical < floor {
		return floor
	}
	return physical
}

// AtmosphereHazeColor returns the haze tint for the body. Falls back
// to ColorFor(b) when Atmosphere.Color is empty so a body that
// declares an atmosphere without an explicit haze color still gets a
// reasonable halo.
func AtmosphereHazeColor(b bodies.CelestialBody) lipgloss.Color {
	atm := b.Atmosphere
	if atm == nil {
		return lipgloss.Color("")
	}
	if atm.Color != "" {
		return lipgloss.Color(atm.Color)
	}
	return ColorFor(b)
}

// AtmosphereVisible reports whether the haze ring should render.
// False for airless bodies. v0.8.4 originally suppressed haze past
// 20 px body radius; with the AtmosphereMinHaloPx floor the ring
// stays readable as a thin halo at any zoom, so that suppression
// is no longer needed.
func AtmosphereVisible(b bodies.CelestialBody, pxRadius int) bool {
	return b.Atmosphere != nil
}
