package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Mars-surface palette. Base is the hand-eye "rust" tone; darker
// basaltic regions and lighter polar caps layer on top via the
// continent-ellipse machinery shared with Earth (see earth.go's
// continentEllipse type).
const (
	ColorMarsRust   = lipgloss.Color("#B7553A") // base regolith
	ColorMarsDark   = lipgloss.Color("#7A3422") // dark albedo features
	ColorMarsBright = lipgloss.Color("#D9A07A") // bright high-albedo (Arabia)
	ColorMarsIce    = lipgloss.Color("#F0E8E0") // polar caps (CO₂ frost)
)

// MarsCenterLonEpoch is the sub-observer longitude at J2000 — what
// the static-Mars renderer used in v0.7.6 — v0.8.4 (-45° centers
// the prime-meridian region with Syrtis Major on the right limb,
// matching the iconic "Mars from Earth" view). v0.8.5+ threads
// sim-time rotation via the lon0 parameter; this constant is just
// the epoch offset.
const MarsCenterLonEpoch = -45.0

// marsFeatures is the single source of truth for Mars surface
// detail. Layered in table order: dark albedo first, then bright
// regions, then polar caps. Each entry uses the shared
// continentEllipse type so the painter is identical to Earth's.
//
// Names check out against telescope-era charts: Syrtis Major,
// Solis Lacus, Acidalia Planitia, Mare Cimmerium. Polar caps wax
// and wane seasonally on real Mars; here they're static.
var marsFeatures = []continentEllipse{
	// Arabia Terra — bright high-albedo region just W of Syrtis.
	{15, 25, 18, 22, ColorMarsBright},
	// Syrtis Major Planum — dark roughly-triangular region.
	{8, 70, 15, 14, ColorMarsDark},
	// Solis Lacus — circular dark spot ("eye of Mars").
	{-25, -85, 8, 12, ColorMarsDark},
	// Acidalia Planitia — large dark region in northern hemisphere.
	{45, -25, 14, 18, ColorMarsDark},
	// Mare Cimmerium — long dark band across southern mid-lats.
	{-25, 130, 10, 35, ColorMarsDark},
	// Mare Erythraeum — south of Solis.
	{-30, -50, 10, 22, ColorMarsDark},
	// Hellas Planitia — large bright impact basin (south).
	{-42, 70, 10, 14, ColorMarsBright},
	// North polar cap.
	{82, 0, 10, 180, ColorMarsIce},
	// South polar cap (slightly larger — ~year-round CO₂).
	{-80, 0, 12, 180, ColorMarsIce},
}

// MarsPixelColor returns the surface color for a pixel at offset
// (dx, dy) inside a Mars disk of pixel radius pxRadius. Mirrors
// EarthPixelColor's projection — orthographic, sub-observer at
// (subLatDeg, subLonDeg). v0.8.5.7+ takes the full sub-observer
// point so view-aware rendering shows poles from above and
// equator from the side.
func MarsPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorMarsRust
	}
	lat, absLon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg, screenUpX, screenUpY)
	if !ok {
		return ColorMarsIce
	}
	color := ColorMarsRust
	for _, f := range marsFeatures {
		if inEllipse(lat, absLon, f) {
			color = f.color
		}
	}
	return color
}
