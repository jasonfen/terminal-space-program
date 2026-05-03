package render

import (
	"math"

	"github.com/charmbracelet/lipgloss"
)

// Galilean-moon palette. Each moon gets two-to-three colors so
// the disks read distinctly against the Jovian backdrop and from
// each other. v0.8.5 textured-bodies trickle.
const (
	// Io — sulfur-yellow base, dark patera deposits, fresh-flow
	// orange highlights. The "pizza moon" look.
	ColorIoBase   = lipgloss.Color("#E8D940") // sulfurous yellow (matches palette entry)
	ColorIoPatera = lipgloss.Color("#7A4A20") // dark volcanic deposits
	ColorIoFresh  = lipgloss.Color("#E07530") // fresh flow orange

	// Europa — bright water ice base with a network of dark linear
	// cracks (lineae). The smoothest world in the Solar System.
	ColorEuropaIce  = lipgloss.Color("#E5DBC6") // pale ice
	ColorEuropaLine = lipgloss.Color("#9A6F4A") // brown linea (cryomagma stains)

	// Ganymede — split between bright young grooved terrain and
	// dark ancient cratered terrain. Largest moon in the system.
	ColorGanymedeBright = lipgloss.Color("#C8B498") // grooved terrain
	ColorGanymedeDark   = lipgloss.Color("#6E5A3E") // ancient cratered terrain
	ColorGanymedeRay    = lipgloss.Color("#E0D2B0") // fresh impact ejecta

	// Callisto — uniformly dark, heavily cratered, with bright
	// crater rays from younger impacts (Valhalla, Asgard).
	ColorCallistoBase  = lipgloss.Color("#5C4A36") // dark base
	ColorCallistoCrater = lipgloss.Color("#9A8260") // bright crater rim / ray
)

// ioPaterae is a coarse layout of dark volcanic deposits on Io.
// Real Io has hundreds of paterae; we render six of the largest
// (Pele, Loki, Pillan, etc.) so the disk reads as "spotted" rather
// than uniformly yellow at telescopic resolutions.
var ioPaterae = []continentEllipse{
	{-19, 256, 6, 8, ColorIoPatera},  // Pele region
	{13, 309, 7, 9, ColorIoPatera},   // Loki Patera (largest)
	{-12, 243, 5, 6, ColorIoFresh},   // Pillan (active)
	{45, 165, 5, 7, ColorIoPatera},   // North polar dark
	{-50, 80, 6, 8, ColorIoPatera},   // South polar dark
	{0, 30, 4, 5, ColorIoFresh},      // Equatorial fresh flow
}

// europaLineae are the dark crack-like bands streaking Europa.
// They run roughly equatorially, in arcs not great circles, but
// our orthographic projection treats them as lat/lon ellipses.
var europaLineae = []continentEllipse{
	{0, 0, 2, 80, ColorEuropaLine},     // Long equatorial linea
	{18, -45, 2, 60, ColorEuropaLine},  // Mid-northern arc
	{-22, 60, 2, 70, ColorEuropaLine},  // Mid-southern arc
	{42, 100, 2, 50, ColorEuropaLine},  // High-northern band
	{-40, -120, 2, 55, ColorEuropaLine}, // High-southern band
}

// ganymedeTerrain layers dark ancient terrain over a bright
// grooved-terrain base. Galileo Regio (~70° W) is the iconic dark
// patch; smaller dark blobs scatter the rest.
var ganymedeTerrain = []continentEllipse{
	{30, -70, 18, 22, ColorGanymedeDark},   // Galileo Regio (largest dark)
	{-15, 80, 14, 16, ColorGanymedeDark},   // Marius Regio
	{-50, -150, 12, 18, ColorGanymedeDark}, // Nicholson Regio
	{45, 150, 10, 14, ColorGanymedeDark},   // Perrine Regio
	// Bright crater ray accents.
	{0, 165, 3, 3, ColorGanymedeRay},    // Osiris-like
	{-25, -10, 2, 2, ColorGanymedeRay},  // Tros-like
}

// callistoCraters are bright crater + ring accents on Callisto's
// dark base. Valhalla is the giant multi-ring impact (~600 km
// across), iconic enough to render explicitly.
var callistoCraters = []continentEllipse{
	{15, 55, 5, 6, ColorCallistoCrater}, // Valhalla bright center
	{40, 145, 4, 5, ColorCallistoCrater}, // Asgard
	{-25, -90, 3, 4, ColorCallistoCrater},
	{50, -60, 2, 3, ColorCallistoCrater},
	{-40, 30, 3, 3, ColorCallistoCrater},
}

// projectPixelToLatLon does the standard orthographic dx,dy → lat,
// absLon transform shared by Galilean / Saturn-class textures.
// Returns (lat, absLon, ok). ok=false on degenerate poles where
// the longitude is undefined; callers should fall back to a base
// color in that case.
func projectPixelToLatLon(dx, dy, pxRadius int, lon0Deg float64) (lat, absLon float64, ok bool) {
	if pxRadius < 1 {
		return 0, 0, false
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
	lat = math.Asin(ny) * 180.0 / math.Pi
	cosLat := math.Sqrt(1.0 - ny*ny)
	if cosLat < 1e-3 {
		return lat, 0, false
	}
	sinLonRel := nx / cosLat
	if sinLonRel < -1 {
		sinLonRel = -1
	} else if sinLonRel > 1 {
		sinLonRel = 1
	}
	relLon := math.Asin(sinLonRel) * 180.0 / math.Pi
	absLon = lon0Deg + relLon
	for absLon > 180 {
		absLon -= 360
	}
	for absLon <= -180 {
		absLon += 360
	}
	return lat, absLon, true
}

// IoPixelColor — sulfur-yellow base + scattered dark paterae +
// occasional bright orange fresh flows. v0.8.5+.
func IoPixelColor(dx, dy, pxRadius int, lon0Deg float64) lipgloss.Color {
	lat, lon, ok := projectPixelToLatLon(dx, dy, pxRadius, lon0Deg)
	if !ok {
		return ColorIoBase
	}
	color := ColorIoBase
	for _, p := range ioPaterae {
		if inEllipse(lat, lon, p) {
			color = p.color
		}
	}
	return color
}

// EuropaPixelColor — pale ice with a few dark linear lineae.
// v0.8.5+.
func EuropaPixelColor(dx, dy, pxRadius int, lon0Deg float64) lipgloss.Color {
	lat, lon, ok := projectPixelToLatLon(dx, dy, pxRadius, lon0Deg)
	if !ok {
		return ColorEuropaIce
	}
	for _, l := range europaLineae {
		if inEllipse(lat, lon, l) {
			return ColorEuropaLine
		}
	}
	return ColorEuropaIce
}

// GanymedePixelColor — bright grooved terrain base + dark ancient
// regiones (Galileo, Marius, Nicholson) + bright crater rays.
// v0.8.5+.
func GanymedePixelColor(dx, dy, pxRadius int, lon0Deg float64) lipgloss.Color {
	lat, lon, ok := projectPixelToLatLon(dx, dy, pxRadius, lon0Deg)
	if !ok {
		return ColorGanymedeBright
	}
	color := ColorGanymedeBright
	for _, t := range ganymedeTerrain {
		if inEllipse(lat, lon, t) {
			color = t.color
		}
	}
	return color
}

// CallistoPixelColor — uniformly dark base + scattered bright
// crater rays (Valhalla, Asgard). v0.8.5+.
func CallistoPixelColor(dx, dy, pxRadius int, lon0Deg float64) lipgloss.Color {
	lat, lon, ok := projectPixelToLatLon(dx, dy, pxRadius, lon0Deg)
	if !ok {
		return ColorCallistoBase
	}
	for _, c := range callistoCraters {
		if inEllipse(lat, lon, c) {
			return ColorCallistoCrater
		}
	}
	return ColorCallistoBase
}
