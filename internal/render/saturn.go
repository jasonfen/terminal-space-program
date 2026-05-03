package render

import (
	"math"

	"github.com/charmbracelet/lipgloss"
)

// Saturn-surface palette. Pale gold base (matching the legacy
// palette entry) with subtler banding than Jupiter — Saturn's
// zones / belts are washed out by a thicker upper haze, so the
// contrast is gentler and the bands are wider and fewer.
const (
	ColorSaturnZone = lipgloss.Color("#E8D9A8") // bright zone (warm pale gold)
	ColorSaturnBelt = lipgloss.Color("#B89968") // dark belt (muted ochre)
	ColorSaturnPole = lipgloss.Color("#9A8458") // muted polar haze, slightly darker
	ColorSaturnSpot = lipgloss.Color("#D9B070") // hexagonal storm + occasional bright ovals
)

// saturnBands is the latitude-banded color scheme. Fewer bands
// than Jupiter and lower contrast — Saturn's atmosphere is hazier,
// so the eye reads broader washes. The narrow polar hexagon at
// ~78°N gets a dedicated band; we don't try to render the actual
// hexagonal shape (sub-pixel at our resolutions) but the band
// color is the iconic muted gold.
var saturnBands = []struct {
	latMin, latMax float64
	color          lipgloss.Color
}{
	{-90, -65, ColorSaturnPole},  // South polar haze
	{-65, -38, ColorSaturnZone},  // South temperate zone
	{-38, -15, ColorSaturnBelt},  // South equatorial belt
	{-15, 15, ColorSaturnZone},   // Equatorial zone (brightest)
	{15, 38, ColorSaturnBelt},    // North equatorial belt
	{38, 65, ColorSaturnZone},    // North temperate zone
	{65, 78, ColorSaturnPole},    // North polar pre-hexagon haze
	{78, 90, ColorSaturnSpot},    // Hexagonal polar vortex (lat ~78°N)
}

// SaturnPixelColor returns the surface color for a pixel at
// (dx, dy) inside a Saturn disk of pixel radius pxRadius. Mirrors
// JupiterPixelColor's banded structure with softer contrast and
// no large-spot overlay (Saturn's storms are short-lived and
// rarely dramatic at our scale). v0.8.5+ takes lon0Deg though
// Saturn's ~10.6h day is fast enough that the bands themselves
// dominate the visible feel; lon0 mostly matters for the polar
// hexagon's rotation.
func SaturnPixelColor(dx, dy, pxRadius int, lon0Deg float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorSaturnZone
	}
	nx := float64(dx) / float64(pxRadius)
	ny := float64(dy) / float64(pxRadius)
	if nx < -1 {
		nx = -1
	} else if nx > 1 {
		nx = 1
	}
	if ny < -1 {
		ny = -1
	} else if ny > 1 {
		ny = 1
	}
	lat := math.Asin(ny) * 180.0 / math.Pi
	color := ColorSaturnPole
	for _, b := range saturnBands {
		if lat >= b.latMin && lat < b.latMax {
			color = b.color
			break
		}
	}
	// lon0 has no per-pixel feature lookup today (no GRS-equivalent),
	// but the parameter stays in the signature so future Cassini-
	// division-style features or moving white-spot accents can plug
	// in via the same closure.
	_ = lon0Deg
	return color
}
