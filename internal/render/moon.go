package render

import (
	"github.com/charmbracelet/lipgloss"
)

// Lunar-surface palette. Highland is the lighter regolith that
// dominates the southern half + far side; mare is the dark basalt
// flooding the northeastern near side ("man in the moon" pattern);
// ray is the very-bright ejecta around fresh impacts (Tycho,
// Copernicus). v0.8.5.7 polish pass: highland warmed slightly
// toward tan to match Apollo / LRO photography (real lunar
// regolith reads slightly warmer than neutral grey), and mare
// deepened a touch for contrast against the warmer highland.
const (
	ColorMoonHighland = lipgloss.Color("#BFB8AA") // warm grey-tan regolith
	ColorMoonMare     = lipgloss.Color("#4A4A55") // darker basalt for contrast
	ColorMoonRay      = lipgloss.Color("#EFEAE0") // bright ejecta, slightly warm
)

// moonMaria is the mare layout in selenographic coordinates
// (positive lat = north, positive lon = east). v0.8.5.7+ extends
// the original near-side-only set with the few far-side maria
// (Moscoviense, Ingenii, Orientale rim) plus the South Pole-Aitken
// basin, the largest impact structure on the Moon and the dominant
// feature visible from a polar / far-side viewing angle. Each
// entry is a center + ellipse semi-axes in degrees. Order doesn't
// matter — any-mare is just a darker patch.
var moonMaria = []continentEllipse{
	// Near-side maria (the iconic "face").
	{17, 59, 7, 9, ColorMoonMare},     // Mare Crisium
	{-4, 51, 8, 9, ColorMoonMare},     // Mare Fecunditatis
	{-14, 35, 5, 6, ColorMoonMare},    // Mare Nectaris
	{8, 31, 9, 11, ColorMoonMare},     // Mare Tranquillitatis
	{28, 18, 8, 10, ColorMoonMare},    // Mare Serenitatis
	{33, -16, 13, 17, ColorMoonMare},  // Mare Imbrium
	{19, -57, 18, 25, ColorMoonMare},  // Oceanus Procellarum (largest near-side)
	{-24, -39, 6, 6, ColorMoonMare},   // Mare Humorum
	{-21, -17, 8, 13, ColorMoonMare},  // Mare Nubium
	{56, 1, 4, 35, ColorMoonMare},     // Mare Frigoris (long thin band along the north)
	{13, 4, 3, 6, ColorMoonMare},      // Mare Vaporum
	{-10, -23, 4, 5, ColorMoonMare},   // Mare Cognitum
	{44, -32, 4, 6, ColorMoonMare},    // Sinus Iridum (bay off Imbrium)
	// Far-side maria — fewer because the far side is older / thicker
	// crust that mostly resisted basalt flooding, but the largest
	// features still register as dark patches.
	{-19, -95, 5, 7, ColorMoonMare},   // Mare Orientale (huge bullseye on western limb)
	{27, 147, 4, 5, ColorMoonMare},    // Mare Moscoviense (true far-side mare)
	{-34, 163, 4, 6, ColorMoonMare},   // Mare Ingenii
	{-21, 129, 3, 4, ColorMoonMare},   // Tsiolkovskiy crater (mare-floored)
	// South Pole-Aitken basin: largest impact structure on the Moon
	// (~2500 km diameter, 13 km deep). Renders as a broad dark
	// region wrapping the far-side south pole — visible from any
	// southern / polar view, the iconic "dark side" feature.
	{-53, -170, 18, 25, ColorMoonMare}, // SPA basin core
	{-52, 170, 14, 22, ColorMoonMare},  // SPA basin extending across antimeridian
	{-58, -150, 12, 18, ColorMoonMare}, // SPA basin southern lobe
}

// moonCraters is bright-rayed crater accents — small but high-
// contrast so the disk doesn't read as uniform highland. v0.8.5.7+
// extends the near-side set (Tycho / Copernicus / Kepler /
// Aristarchus) with far-side and polar craters so polar / far-side
// views also have visible identity features.
var moonCraters = []continentEllipse{
	// Near-side bright rayed craters.
	{-43, -11, 3, 3, ColorMoonRay}, // Tycho — most prominent, near south
	{10, -20, 2, 2, ColorMoonRay},  // Copernicus
	{8, -38, 2, 2, ColorMoonRay},   // Kepler
	{24, -48, 1, 1, ColorMoonRay},  // Aristarchus
	// Far-side bright craters.
	{-4, -157, 2, 3, ColorMoonRay},  // Korolev rim
	{-36, -151, 2, 2, ColorMoonRay}, // Apollo
	{1, -130, 3, 4, ColorMoonRay},   // Hertzsprung (large)
	{6, 141, 2, 2, ColorMoonRay},    // Mendeleev rim
	{-23, 117, 2, 2, ColorMoonRay},  // near Tsiolkovskiy rim
	// Polar craters — visible from ViewTop's polar perspective.
	// These are the bright accents that give the polar view
	// identity beyond a uniform highland disk.
	{73, 4, 1, 5, ColorMoonRay},    // Goldschmidt (north polar)
	{72, -50, 1, 4, ColorMoonRay},  // Pythagoras (north polar)
	{65, 175, 2, 5, ColorMoonRay},  // Plaskett area (north polar far-side)
	{-85, -40, 1, 6, ColorMoonRay}, // Cabeus area (south polar)
	{-87, 30, 1, 8, ColorMoonRay},  // Shackleton + neighbors (south pole)
	{-79, 110, 1, 5, ColorMoonRay}, // Schrödinger rim (south polar far-side)
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
func MoonPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorMoonHighland
	}
	lat, absLon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg, screenUpX, screenUpY)
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
