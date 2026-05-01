package render

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// AtmosphereVisibilityCap is the body-pixel-radius threshold past
// which the haze ring is suppressed. Below the threshold the haze
// reads as a halo around a small disk; above it the body fills the
// view and a ring around it just adds clutter without adding depth.
const AtmosphereVisibilityCap = 20

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
// at the given scale (px/m). Returns 0 when haze shouldn't render.
func AtmosphereOuterPx(b bodies.CelestialBody, scale float64) int {
	outer := AtmosphereOuterMeters(b)
	if outer <= 0 || scale <= 0 {
		return 0
	}
	return int(outer * scale)
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

// AtmosphereVisible reports whether the haze ring should render. False
// for airless bodies and for bodies whose disk has grown past the
// visibility cap.
func AtmosphereVisible(b bodies.CelestialBody, pxRadius int) bool {
	if b.Atmosphere == nil {
		return false
	}
	return pxRadius < AtmosphereVisibilityCap
}
