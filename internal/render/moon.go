package render

import (
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
//
// v0.8.5.7+ takes the full sub-observer point. For the tidally-
// locked Moon, both subLat and subLon advance with orbital phase
// from a heliocentric observer's POV — the near side rotates into /
// out of view as Luna orbits Earth.
func MoonPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorMoonHighland
	}
	lat, absLon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg)
	if !ok {
		return ColorMoonHighland
	}
	for _, c := range moonCraters {
		if inEllipse(lat, absLon, c) {
			return ColorMoonRay
		}
	}
	for _, c := range moonMaria {
		if inEllipse(lat, absLon, c) {
			return ColorMoonMare
		}
	}
	return ColorMoonHighland
}
