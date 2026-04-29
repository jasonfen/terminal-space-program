package render

import (
	"math"

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

// jupiterCenterLon picks the sub-observer longitude so the Great
// Red Spot is visible (centered on its rough longitude). Static
// for v0.7.6; rotation tied to sim time would be especially
// dramatic on Jupiter's ~10-hour day, but threading sim time
// through the texture function is a follow-up.
const jupiterCenterLon = 25.0

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
// at sub-observer-relative coordinates. v0.7.6+.
func JupiterPixelColor(dx, dy, pxRadius int) lipgloss.Color {
	if pxRadius < 1 {
		return ColorJupiterZone
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
	cosLat := math.Sqrt(1.0 - ny*ny)

	// Default to the polar haze if the band lookup misses (shouldn't
	// happen given the table covers [-90, 90], but keeps the path safe).
	color := ColorJupiterPole
	for _, b := range jupiterBands {
		if lat >= b.latMin && lat < b.latMax {
			color = b.color
			break
		}
	}

	// Great Red Spot — needs absolute longitude, requires the
	// projection step. Skip when cosLat is degenerate (poles).
	if cosLat >= 1e-3 {
		sinLonRel := nx / cosLat
		if sinLonRel < -1 {
			sinLonRel = -1
		} else if sinLonRel > 1 {
			sinLonRel = 1
		}
		relLon := math.Asin(sinLonRel) * 180.0 / math.Pi
		absLon := jupiterCenterLon + relLon
		for absLon > 180 {
			absLon -= 360
		}
		for absLon <= -180 {
			absLon += 360
		}
		if inEllipse(lat, absLon, greatRedSpot) {
			color = greatRedSpot.color
		}
	}

	return color
}
