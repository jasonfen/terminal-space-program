package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Jupiter-surface palette. Banded ochre clouds — alternating zones
// (bright) and belts (dark) running parallel to the equator, with
// the Great Red Spot painted on top.
const (
	ColorJupiterZone = lipgloss.Color("#D7B98C") // bright zone (warm)
	ColorJupiterBelt = lipgloss.Color("#8B6240") // dark belt (warm)
	ColorJupiterPole = lipgloss.Color("#7A6450") // muted polar haze
	ColorJupiterGRS  = lipgloss.Color("#A03A28") // Great Red Spot
)

// JupiterCenterLonEpoch is the sub-observer longitude at J2000 —
// what the static-Jupiter renderer used in v0.7.6 — v0.8.4 (25°
// puts the Great Red Spot near the visible center). v0.8.5+ threads
// sim-time rotation via the lon0 parameter so Jupiter's ~10-hour
// day reads kinetically; this constant is just the epoch offset.
const JupiterCenterLonEpoch = 25.0

// jupiterBands is the latitude-banded color scheme. Each entry is
// (latMin, latMax, color). Bands are in degrees, ordered south to
// north so a single sweep can find the matching band per pixel.
// Naming follows the standard terrestrial-astronomer layout:
// SPR (south polar region), STZ/STB, STrZ/SEB, EZ, NEB/NTrZ,
// NTB/NTZ, NPR.
var jupiterBands = []struct {
	latMin, latMax float64
	color          lipgloss.Color
}{
	{-90, -55, ColorJupiterPole},  // South Polar Region
	{-55, -38, ColorJupiterBelt},  // South Temperate Belt
	{-38, -22, ColorJupiterZone},  // South Tropical Zone
	{-22, -8, ColorJupiterBelt},   // South Equatorial Belt
	{-8, 8, ColorJupiterZone},     // Equatorial Zone (brightest)
	{8, 18, ColorJupiterBelt},     // North Equatorial Belt
	{18, 32, ColorJupiterZone},    // North Tropical Zone
	{32, 50, ColorJupiterBelt},    // North Temperate Belt
	{50, 65, ColorJupiterZone},    // North Temperate Zone
	{65, 90, ColorJupiterPole},    // North Polar Region
}

// greatRedSpot — the iconic anticyclone in the South Tropical Zone
// (lat ≈ -22°). Drifts ~zonally on real Jupiter; static here.
var greatRedSpot = continentEllipse{
	lat: -22, lon: 0, aLat: 6, aLon: 14, color: ColorJupiterGRS,
}

// JupiterPixelColor returns the surface color for a pixel at
// (dx, dy) inside a Jupiter disk of pixel radius pxRadius.
// Banded zones from a latitude lookup; the GRS is layered on top
// at sub-observer-relative coordinates. v0.8.5.7+ takes the full
// sub-observer point so the GRS sweeps correctly across the disk
// for any view direction (Jupiter's 3° tilt is small but the
// math handles it uniformly with the rest of the planets).
func JupiterPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorJupiterZone
	}
	lat, absLon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg)
	color := ColorJupiterPole
	for _, b := range jupiterBands {
		if lat >= b.latMin && lat < b.latMax {
			color = b.color
			break
		}
	}
	if ok && inEllipse(lat, absLon, greatRedSpot) {
		color = greatRedSpot.color
	}
	return color
}
