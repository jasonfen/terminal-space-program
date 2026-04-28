package render

import (
	"math"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// Earth-surface palette. The base color is intentionally darker than
// the v0.5.1 #5BB3FF "Earth blue" so land + cloud splotches read
// against it; the existing palette entry stays the public face for
// HUD labels and the body-info screen.
const (
	ColorEarthOcean = lipgloss.Color("#3D7AAB")
	ColorEarthLand  = lipgloss.Color("#5C8C4A")
	ColorEarthCloud = lipgloss.Color("#E8EFF5")
)

// BodyTextureMinRadius is the minimum body pixel radius at which the
// per-pixel body textures render. Below this, the body falls back to
// a solid-color disk because (a) the disk has too few cells to read
// a continent / mare shape, and (b) the textured fill is more
// expensive per pixel than a plain disk.
const BodyTextureMinRadius = 12

// EarthTextureMinRadius is the legacy name for BodyTextureMinRadius,
// preserved for callers / tests that referenced the Earth-specific
// constant. Drop in v0.8 alongside other v0.7 cleanup.
const EarthTextureMinRadius = BodyTextureMinRadius

// BodyTexture returns the per-pixel surface color for a textured
// body. Implementations live in body-specific files (earth.go,
// moon.go, ...) and assume the caller has already gated the pixel
// to inside the disk; for points within 1 px of the limb the
// projection clamps cleanly.
type BodyTexture func(dx, dy, pxRadius int) lipgloss.Color

// continentEllipse approximates a land mass as a lat/lon-axis-aligned
// ellipse. Rough enough to be recognisable but small enough that the
// table fits in a screen.
type continentEllipse struct {
	lat, lon float64 // ellipse center, degrees
	aLat     float64 // semi-axis along latitude, degrees
	aLon     float64 // semi-axis along longitude, degrees
}

// earthContinents is a deliberately coarse approximation — at the
// resolutions we render (≤64 px radius), real coastline detail is
// sub-pixel anyway. Goal is a recognisable layout, not cartographic
// fidelity.
var earthContinents = []continentEllipse{
	{45, -100, 25, 28}, // North America
	{-15, -60, 25, 13}, // South America
	{50, 80, 18, 75},   // Eurasia
	{5, 20, 30, 18},    // Africa
	{-25, 135, 11, 16}, // Australia
	{73, -42, 8, 14},   // Greenland
}

// earthClouds are a few hand-placed pale streaks suggesting global
// cloud bands. Static for v0.7.2.1; rotation tied to sim time is a
// follow-up if it adds enough to justify the threading work.
var earthClouds = []continentEllipse{
	{18, -130, 4, 18}, // North Pacific
	{-22, 50, 4, 28},  // Indian Ocean
	{30, -45, 5, 14},  // North Atlantic
	{-12, -100, 5, 15}, // South Pacific ITCZ
	{55, 10, 4, 22},   // North European
}

// EarthPixelColor returns the surface color for a pixel at offset
// (dx, dy) inside an Earth disk of pixel radius pxRadius. Caller is
// responsible for clipping pixels to the disk; for points within
// 1 px of the edge the projection clamps to the limb.
//
// Resolution order: cloud > land > ocean. Clouds layer over land
// where they overlap so a cloud sitting over a continent reads
// correctly (the surface beneath isn't visible anyway).
func EarthPixelColor(dx, dy, pxRadius int) lipgloss.Color {
	if pxRadius < 1 {
		return ColorEarthOcean
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
	// Orthographic projection: ny = sin(lat), nx = cos(lat)*sin(lon).
	// Visible hemisphere only — this function never sees the far side.
	lat := math.Asin(ny) * 180.0 / math.Pi
	cosLat := math.Sqrt(1.0 - ny*ny)
	if cosLat < 1e-3 {
		// Pole. Differentiate north (Arctic Ocean) from south
		// (Antarctica) so the body's top/bottom doesn't read as
		// identical caps.
		if ny > 0 {
			return ColorEarthOcean
		}
		return ColorEarthLand
	}
	sinLonRel := nx / cosLat
	if sinLonRel < -1 {
		sinLonRel = -1
	} else if sinLonRel > 1 {
		sinLonRel = 1
	}
	lon := math.Asin(sinLonRel) * 180.0 / math.Pi

	for _, c := range earthClouds {
		if inEllipse(lat, lon, c) {
			return ColorEarthCloud
		}
	}
	for _, c := range earthContinents {
		if inEllipse(lat, lon, c) {
			return ColorEarthLand
		}
	}
	return ColorEarthOcean
}

func inEllipse(lat, lon float64, c continentEllipse) bool {
	dlat := lat - c.lat
	dlon := lon - c.lon
	return (dlat/c.aLat)*(dlat/c.aLat)+(dlon/c.aLon)*(dlon/c.aLon) < 1.0
}

// TextureFor returns the per-pixel texture function for the given
// body, or nil if the body should render as a solid disk (small
// radius, unsupported body). The dispatch hook for body-specific
// surface rendering — Mars caps, Jupiter banding, Saturn cloud
// bands plug in via additional cases here.
func TextureFor(b bodies.CelestialBody, pxRadius int) BodyTexture {
	if pxRadius < BodyTextureMinRadius {
		return nil
	}
	switch b.ID {
	case "earth":
		return EarthPixelColor
	case "moon":
		return MoonPixelColor
	}
	return nil
}

// BodyHasTexture reports whether TextureFor would return non-nil.
// Convenience wrapper for callers that just need the boolean
// (e.g. "should I suppress the body-identity glyph?").
func BodyHasTexture(b bodies.CelestialBody, pxRadius int) bool {
	return TextureFor(b, pxRadius) != nil
}
