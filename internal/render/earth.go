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

// earthContinents is a deliberately coarse approximation — at the
// resolutions we render (≤64 px radius), real coastline detail is
// sub-pixel anyway. v0.7.6+ uses multi-ellipse decomposition to give
// each continent a recognisable shape: a main mass plus extras for
// peninsulas, deserts, and offshore islands. Order matters — later
// entries paint over earlier ones, so deserts go after their parent
// continent's land mass.
var earthContinents = []continentEllipse{
	// North America. Main bulk + Mexico tapering + Alaska + Florida.
	{50, -100, 18, 30, ColorEarthLand},
	{30, -100, 8, 12, ColorEarthLand},  // Mexico
	{63, -150, 7, 18, ColorEarthLand},  // Alaska
	{28, -82, 5, 4, ColorEarthLand},    // Florida
	// South America. Andes spine + Brazilian bulge + tapering southern cone.
	{-5, -65, 13, 14, ColorEarthLand},  // Amazon basin
	{-25, -55, 12, 8, ColorEarthLand},  // central
	{-40, -65, 10, 6, ColorEarthLand},  // Patagonia tapering
	// Africa. Bulky north + tapering south + horn.
	{15, 15, 20, 20, ColorEarthLand},   // northern Africa main
	{-10, 25, 18, 14, ColorEarthLand},  // central / southern
	{8, 45, 6, 7, ColorEarthLand},      // Horn
	// Sahara desert sits over northern Africa land — paint after.
	{22, 10, 8, 22, ColorEarthDesert},
	// Arabia desert over Africa-Asia junction.
	{25, 45, 8, 8, ColorEarthDesert},
	// Eurasia. Main bulk + Indian subcontinent + Iberian + SE Asia.
	{55, 90, 18, 70, ColorEarthLand},   // Russia / Central Asia / Europe
	{42, 0, 6, 8, ColorEarthLand},      // Iberia + W. Europe extension
	{20, 78, 11, 8, ColorEarthLand},    // Indian subcontinent
	{15, 105, 8, 9, ColorEarthLand},    // SE Asia / Indochina
	// Central Asian deserts (Gobi-ish — visible only on Asia-side).
	{42, 95, 8, 18, ColorEarthDesert},
	// Australia + Tasmania.
	{-25, 135, 9, 14, ColorEarthLand},
	{-42, 147, 2, 3, ColorEarthLand},   // Tasmania
	// Australian outback — desert overlay.
	{-25, 133, 5, 8, ColorEarthDesert},
	// Greenland (mostly ice).
	{72, -40, 8, 12, ColorEarthIce},
	// Iceland (small but iconic in N. Atlantic).
	{65, -19, 2, 4, ColorEarthLand},
	// UK + Ireland (matters for the Atlantic-centered view).
	{54, -3, 4, 5, ColorEarthLand},
	// Madagascar.
	{-19, 47, 6, 3, ColorEarthLand},
	// Japan.
	{36, 138, 6, 4, ColorEarthLand},
}

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
// Resolution order: ice cap > atmospheric limb (over non-ice) >
// cloud > biome-shaded continent > ocean.
//
// v0.8.5.7+: temperate ColorEarthLand auto-shifts to tropical
// (|lat| < 23°) or boreal (|lat| ≥ 55°); the outer ~8% of the
// disk (r² > 0.92) blends to ColorEarthAtmosphere over non-ice
// pixels to give the disk a recognisable blue-marble halo.
func EarthPixelColor(dx, dy, pxRadius int, subLatDeg, subLonDeg float64) lipgloss.Color {
	if pxRadius < 1 {
		return ColorEarthOcean
	}
	// Disk radial coord — used for the limb tint band.
	nx := float64(dx) / float64(pxRadius)
	ny := float64(dy) / float64(pxRadius)
	r2 := nx*nx + ny*ny

	lat, absLon, ok := projectPixelToLatLon(dx, dy, pxRadius, subLatDeg, subLonDeg)
	// Polar ice caps. Antarctic Circle ≈ -66.5°, but we render a
	// slightly larger cap (-70°) so the visual hits the eye even
	// at small pxRadius. Arctic ice (above ~80°N) is mostly Arctic
	// Ocean — Greenland is handled separately as ice in the
	// continent table. Ice wins even at the limb so polar caps
	// stay readable.
	if lat < -70 {
		return ColorEarthIce
	}
	if lat > 82 {
		return ColorEarthIce
	}
	if !ok {
		// Sub-observer pole — degenerate longitude, fall back to
		// ocean (callers usually mask the pole as ice anyway).
		return ColorEarthOcean
	}

	// Continents painted first so clouds can layer over them.
	color := ColorEarthOcean
	for _, c := range earthContinents {
		if inEllipse(lat, absLon, c) {
			color = c.color
		}
	}
	// Land biome variation by latitude. Tropical / boreal shifts
	// only apply to the temperate-default Land color — desert and
	// ice continent entries (Sahara, Greenland) stay the explicit
	// override they were tagged with.
	if color == ColorEarthLand {
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
	}
	// Clouds layer on top of land or ocean alike — except over
	// polar ice, which is already returned above.
	for _, c := range earthClouds {
		if inEllipse(lat, absLon, c) {
			color = c.color
			break
		}
	}
	// Atmospheric limb tint. Real Earth from space has a visible
	// blue halo at the disk edge from atmospheric scattering;
	// painting the outer ~8% of the disk in a sky-blue tint reads
	// as that halo and makes the body feel atmospheric rather
	// than a flat textured circle. Skip for ice (poles already
	// bright at the limb) and for clouds (already light enough
	// that the tint adds no contrast).
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
func TextureFor(b bodies.CelestialBody, pxRadius int, subLatDeg, subLonDeg float64) BodyTexture {
	if pxRadius < BodyTextureMinRadius {
		return nil
	}
	switch b.ID {
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
	return TextureFor(b, pxRadius, 0, 0) != nil
}
