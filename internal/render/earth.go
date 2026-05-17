package render

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// Earth-surface palette. v0.8.5.7 polish pass nudges the colors
// toward the iconic "blue marble" look — deeper, slightly more
// saturated ocean; biome-split land (boreal taiga / temperate /
// tropical jungle) so high-latitude continents read browner and
// equatorial ones read deeper green; warmer Sahara-tan desert;
// bluer ice. ColorEarthLand is the temperate default — code
// shifts to boreal / tropical based on the pixel's latitude when
// rendering. The existing public palette entry (#5BB3FF) stays
// the HUD-label face.
const (
	ColorEarthOcean         = lipgloss.Color("#1F5F94") // deeper saturated blue
	ColorEarthLand          = lipgloss.Color("#5C8C4A") // temperate green (default land)
	ColorEarthLandTropical  = lipgloss.Color("#3E7A35") // tropical jungle, deeper / saturated
	ColorEarthLandBoreal    = lipgloss.Color("#6B7A56") // boreal taiga, browner / drabber
	ColorEarthDesert        = lipgloss.Color("#C8A06A") // brighter Sahara tan
	ColorEarthIce           = lipgloss.Color("#E5EEF6") // slightly bluer ice
	ColorEarthCloud         = lipgloss.Color("#F2F5FA")
	ColorEarthAtmosphere    = lipgloss.Color("#6FA8D6") // atmospheric limb tint
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
// projection clamps cleanly. The closure returned by TextureFor
// bakes the body's sub-observer longitude (lon0) in, so the canvas
// painter doesn't need to thread sim time through every pixel call.
type BodyTexture func(dx, dy, pxRadius int) lipgloss.Color

// EarthCenterLonEpoch is the sub-observer longitude at J2000 — the
// longitude that sat dead-center on the visible hemisphere when
// Earth was rendered statically (v0.7.6 — v0.8.4). Now the epoch
// reference for SubObserverLongitudeDeg ("Earth at sim-time-zero
// looks the same as it did pre-rotation"). v0.8.5+ threads sim-time
// rotation through the lon0 parameter; this constant is just the
// epoch offset.
const EarthCenterLonEpoch = -30.0

// continentEllipse approximates a land mass as a lat/lon-axis-aligned
// ellipse. v0.7.6+: continents are decomposed into multiple ellipses
// so shapes read as recognisable rather than amorphous blobs. Color
// lets each ellipse pick land / desert / ice independently.
type continentEllipse struct {
	lat, lon float64        // ellipse center, degrees
	aLat     float64        // semi-axis along latitude, degrees
	aLon     float64        // semi-axis along longitude, degrees
	color    lipgloss.Color // fill color (land / desert / ice / etc.)
}

// earthContinents (the v0.7.6 ellipse-table approximation) was
// retired in v0.8.5.7's grid-rasteriser pass — the polygon list
// in earth_grid.go produces a recognisable continental outline at
// the rendering resolution and is the new source of truth for
// land / desert / ice classification.

// earthClouds are a few hand-placed pale streaks suggesting global
// cloud bands. Static for v0.7.2.1; rotation tied to sim time is a
// follow-up if it adds enough to justify the threading work.
// v0.7.6+: additional bands for ITCZ + extratropical storm tracks so
// the disk reads with weather rather than flat continents.
var earthClouds = []continentEllipse{
	{18, -130, 3, 18, ColorEarthCloud}, // North Pacific
	{-22, 50, 4, 28, ColorEarthCloud},  // Indian Ocean
	{30, -45, 4, 14, ColorEarthCloud},  // North Atlantic
	{-12, -100, 4, 15, ColorEarthCloud}, // South Pacific ITCZ
	{55, 10, 3, 22, ColorEarthCloud},   // North European
	{-50, -10, 3, 30, ColorEarthCloud}, // Southern Ocean storm track
	{-50, 90, 3, 35, ColorEarthCloud},  // Roaring 40s (Indian Ocean)
	{8, -40, 2, 12, ColorEarthCloud},   // Equatorial Atlantic ITCZ
	{8, 140, 3, 18, ColorEarthCloud},   // Western Pacific ITCZ
}

// EarthPixelColor returns the surface color for a pixel at offset
// (dx, dy) inside an Earth disk of pixel radius pxRadius. Caller is
// responsible for clipping pixels to the disk.
//
// Projection: orthographic with sub-observer point at
// (subLatDeg, subLonDeg). v0.8.5.7+ takes both — the canvas
// computes them from the camera direction + body axis tilt + sim
// time, so ViewTop on a tilted Earth shows polar regions and
// ViewRight/Left/Bottom show the equator with surface features
// drifting across.
//
// Source: v0.8.5.7's ellipse-table approximation was upgraded to
// a polygon-rasterised 144×72 mask (earthGrid) — the per-pixel
// path looks the cell up directly, so coastlines / islands /
// peninsulas read recognisably instead of as smooth ellipse
// blobs. Resolution order: ice cap > atmospheric limb (over
// non-ice) > cloud > biome-shaded continent / desert > ocean.
//
// v0.8.5.7+: temperate land auto-shifts to tropical (|lat| < 23°)
// or boreal (|lat| ≥ 55°); the outer ~8% of the disk (r² > 0.92)
// blends to ColorEarthAtmosphere over non-ice pixels to give the
// disk a recognisable blue-marble halo.
func EarthPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorEarthOcean
	}
	// Disk radial coord — used for the limb tint band.
	nx := float64(dx) / float64(pxRadius)
	ny := float64(dy) / float64(pxRadius)
	r2 := nx*nx + ny*ny

	lat, absLon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg)
	if !ok {
		return ColorEarthOcean
	}

	// Look up the rasterised continental mask.
	cell := earthCellAt(lat, absLon)
	var color lipgloss.Color
	switch cell {
	case cellIce:
		color = ColorEarthIce
	case cellLand:
		color = ColorEarthLand
		// Biome shift by absolute latitude: tropical (deeper
		// jungle), temperate (default), boreal (browner taiga).
		absLat := lat
		if absLat < 0 {
			absLat = -absLat
		}
		switch {
		case absLat < 23:
			color = ColorEarthLandTropical
		case absLat >= 55:
			color = ColorEarthLandBoreal
		}
	case cellDesert:
		color = ColorEarthDesert
	default:
		color = ColorEarthOcean
	}
	// Clouds layer on top of land or ocean alike — except over
	// polar ice, where the ice already reads bright.
	if color != ColorEarthIce {
		for _, c := range earthClouds {
			if inEllipse(lat, absLon, c) {
				color = c.color
				break
			}
		}
	}
	// Atmospheric limb tint. Real Earth from space has a visible
	// blue halo at the disk edge from atmospheric scattering;
	// painting the outer ~8% of the disk in a sky-blue tint reads
	// as that halo. Skip for ice (poles already bright) and for
	// clouds (already light enough that the tint adds no
	// contrast).
	if r2 > 0.92 && color != ColorEarthIce && color != ColorEarthCloud {
		color = ColorEarthAtmosphere
	}
	return color
}

func inEllipse(lat, lon float64, c continentEllipse) bool {
	dlat := lat - c.lat
	dlon := lon - c.lon
	// Wrap dlon to (-180, 180] so a continent straddling the
	// dateline (none today, but cheap to handle) reads correctly.
	for dlon > 180 {
		dlon -= 360
	}
	for dlon < -180 {
		dlon += 360
	}
	return (dlat/c.aLat)*(dlat/c.aLat)+(dlon/c.aLon)*(dlon/c.aLon) < 1.0
}

// TextureFor returns the per-pixel texture function for the given
// body, or nil if the body should render as a solid disk (small
// radius, unsupported body). The dispatch hook for body-specific
// surface rendering — Mars caps, Jupiter banding, Saturn cloud
// bands plug in via additional cases here.
//
// v0.8.5.7+ takes the full sub-observer point (subLatDeg,
// subLonDeg) and bakes it into the returned closure, so the
// canvas painter calls a 3-arg BodyTexture without threading view
// + sim time through each pixel. Callers should compute the
// sub-observer point via SubObserverPointDeg(b, simTime, camDir)
// once per body per frame.
//
// v0.9.6+ takes an optional light: when non-nil and the body is not
// the Sun, every pixel is darkened by light.FactorAt to paint the
// day/night terminator (+ eclipse dimming). nil disables shading and
// is the back-compat path (e.g. BodyHasTexture). The Sun is always
// exempt — it is the light source and keeps its own limb darkening.
func TextureFor(b bodies.CelestialBody, pxRadius int, subLatDeg, subLonDeg float64, light *SolarLight) BodyTexture {
	if pxRadius < BodyTextureMinRadius {
		return nil
	}
	base := bodyTextureBase(b, subLatDeg, subLonDeg)
	if base == nil || b.ID == "sun" || light == nil {
		return base
	}
	return func(dx, dy, r int) lipgloss.Color {
		return Shade(base(dx, dy, r), light.FactorAt(dx, dy, r, subLatDeg, subLonDeg))
	}
}

// bodyTextureBase returns the unlit per-pixel surface shader for a
// body, or nil if the body has no texture. Split out of TextureFor so
// the v0.9.6 lighting wrapper has a single base closure to darken.
func bodyTextureBase(b bodies.CelestialBody, subLatDeg, subLonDeg float64) BodyTexture {
	switch b.ID {
	case "sun":
		return func(dx, dy, r int) lipgloss.Color {
			return SunPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "earth":
		return func(dx, dy, r int) lipgloss.Color {
			return EarthPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "moon":
		return func(dx, dy, r int) lipgloss.Color {
			return MoonPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "mars":
		return func(dx, dy, r int) lipgloss.Color {
			return MarsPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "jupiter":
		return func(dx, dy, r int) lipgloss.Color {
			return JupiterPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "saturn":
		return func(dx, dy, r int) lipgloss.Color {
			return SaturnPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "uranus":
		return func(dx, dy, r int) lipgloss.Color {
			return UranusPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "neptune":
		return func(dx, dy, r int) lipgloss.Color {
			return NeptunePixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "io":
		return func(dx, dy, r int) lipgloss.Color {
			return IoPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "europa":
		return func(dx, dy, r int) lipgloss.Color {
			return EuropaPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "ganymede":
		return func(dx, dy, r int) lipgloss.Color {
			return GanymedePixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	case "callisto":
		return func(dx, dy, r int) lipgloss.Color {
			return CallistoPixelColor(dx, dy, r, subLatDeg, subLonDeg)
		}
	}
	return nil
}

// BodyHasTexture reports whether TextureFor would return non-nil.
// Convenience wrapper for callers that just need the boolean
// (e.g. "should I suppress the body-identity glyph?"). The
// sub-observer point doesn't affect the gate, so the boolean form
// omits it.
func BodyHasTexture(b bodies.CelestialBody, pxRadius int) bool {
	return TextureFor(b, pxRadius, 0, 0, nil) != nil
}
