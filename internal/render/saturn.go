package render

import (
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
// JupiterPixelColor's banded structure with softer contrast.
// v0.8.5.7+ takes the full sub-observer point so the polar
// hexagon stays at lat ≈ 78°N regardless of view direction
// (top view sees the hex pole-on; side views see it near the
// limb).
func SaturnPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorSaturnZone
	}
	lat, _, _ := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg, screenUpX, screenUpY)
	color := ColorSaturnPole
	for _, b := range saturnBands {
		if lat >= b.latMin && lat < b.latMax {
			color = b.color
			break
		}
	}
	return color
}
