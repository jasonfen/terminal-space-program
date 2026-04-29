package render

import (
	"math"

	"github.com/charmbracelet/lipgloss"
)

// Lunar-surface palette. Highland is the lighter regolith that
// dominates the southern half + far side; mare is the dark basalt
// flooding the northeastern near side ("man in the moon" pattern);
// ray is the very-bright ejecta around fresh impacts (Tycho,
// Copernicus). Picks tuned so all three read distinctly against the
// canvas dark background.
const (
	ColorMoonHighland = lipgloss.Color("#C8C8C8") // matches the legacy bodyPalette["moon"]
	ColorMoonMare     = lipgloss.Color("#5A5A65") // dark basalt, slightly cool grey-blue
	ColorMoonRay      = lipgloss.Color("#F0F0F0") // crater ejecta + fresh ray systems
)

// moonMaria is the canonical near-side mare layout in selenographic
// coordinates (positive lat = north, positive lon = east). Each entry
// is a center + ellipse axes in degrees. Coverage roughly matches
// what's visible from Earth — we don't model the spacecraft's
// viewing angle, so the rendered face is always the near side.
var moonMaria = []continentEllipse{
	{17, 59, 7, 9, ColorMoonMare},     // Mare Crisium
	{-4, 51, 8, 9, ColorMoonMare},     // Mare Fecunditatis
	{-14, 35, 5, 6, ColorMoonMare},    // Mare Nectaris
	{8, 31, 9, 11, ColorMoonMare},     // Mare Tranquillitatis
	{28, 18, 8, 10, ColorMoonMare},    // Mare Serenitatis
	{33, -16, 13, 17, ColorMoonMare},  // Mare Imbrium
	{19, -57, 18, 25, ColorMoonMare},  // Oceanus Procellarum (largest)
	{-24, -39, 6, 6, ColorMoonMare},   // Mare Humorum
	{-21, -17, 8, 13, ColorMoonMare},  // Mare Nubium
	{56, 1, 4, 35, ColorMoonMare},     // Mare Frigoris (long thin band along the north)
}

// moonCraters is a handful of bright rayed craters hand-placed so the
// Moon doesn't read as just light + dark splotches. Ellipses are
// small (1-3°) — these are bright accents, not large features.
var moonCraters = []continentEllipse{
	{-43, -11, 3, 3, ColorMoonRay}, // Tycho — most prominent, near south
	{10, -20, 2, 2, ColorMoonRay},  // Copernicus
	{8, -38, 2, 2, ColorMoonRay},   // Kepler
	{24, -48, 1, 1, ColorMoonRay},  // Aristarchus
}

// MoonPixelColor returns the surface color for a pixel at offset
// (dx, dy) inside a Moon disk of pixel radius pxRadius. Mirrors
// EarthPixelColor's structure: orthographic (dx,dy) → (lat, lon)
// projection, then ellipse-table lookup for mare / crater / highland
// classification. Resolution order: bright crater > mare > highland.
func MoonPixelColor(dx, dy, pxRadius int) lipgloss.Color {
	if pxRadius < 1 {
		return ColorMoonHighland
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
	if cosLat < 1e-3 {
		// Pole / limb — return highland. The Moon doesn't have
		// distinct polar features at this resolution.
		return ColorMoonHighland
	}
	sinLonRel := nx / cosLat
	if sinLonRel < -1 {
		sinLonRel = -1
	} else if sinLonRel > 1 {
		sinLonRel = 1
	}
	lon := math.Asin(sinLonRel) * 180.0 / math.Pi

	for _, c := range moonCraters {
		if inEllipse(lat, lon, c) {
			return ColorMoonRay
		}
	}
	for _, c := range moonMaria {
		if inEllipse(lat, lon, c) {
			return ColorMoonMare
		}
	}
	return ColorMoonHighland
}
