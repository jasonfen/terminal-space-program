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
	ColorEarthDesert = lipgloss.Color("#B59565")
	ColorEarthIce   = lipgloss.Color("#E8EFF5")
	ColorEarthCloud = lipgloss.Color("#F2F5FA")
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

// earthCenterLon is the sub-observer longitude — the longitude that
// sits dead-center on the visible hemisphere. v0.7.6+: changed from
// 0° (Africa-centered, hides the entire western hemisphere) to -30°
// so the visible hemisphere shows the Americas + Atlantic + W.
// Europe + Africa, which reads as recognisable Earth at a glance.
// Sim-time-driven rotation is a follow-up; static centerLon keeps
// the LOC small.
const earthCenterLon = -30.0

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
// responsible for clipping pixels to the disk; for points within
// 1 px of the edge the projection clamps to the limb.
//
// Projection: orthographic with sub-observer point at (lat=0,
// lon=earthCenterLon). v0.7.6+ derives absolute longitude from the
// pixel's relative longitude + earthCenterLon, so continents stored
// in absolute coordinates render at the right place on the disk.
//
// Resolution order: ice cap > cloud > continents (in table order) >
// ocean. Polar caps win over everything else in their lat band so
// Antarctic ice doesn't get masked by a stray continent edge.
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
	// Orthographic projection: ny = sin(lat), nx = cos(lat)*sin(lon_rel).
	lat := math.Asin(ny) * 180.0 / math.Pi

	// Polar ice caps. Antarctic Circle ≈ -66.5°, but we render a
	// slightly larger cap (-70°) so the visual hits the eye even
	// at small pxRadius. Arctic ice (above ~80°N) is mostly Arctic
	// Ocean — Greenland is handled separately as ice in the
	// continent table.
	if lat < -70 {
		return ColorEarthIce
	}
	if lat > 82 {
		return ColorEarthIce
	}

	cosLat := math.Sqrt(1.0 - ny*ny)
	if cosLat < 1e-3 {
		// Pole. Differentiate north (Arctic Ocean) from south
		// (Antarctica) — handled by the lat caps above, but keep
		// the edge case safe.
		if ny > 0 {
			return ColorEarthIce
		}
		return ColorEarthIce
	}
	sinLonRel := nx / cosLat
	if sinLonRel < -1 {
		sinLonRel = -1
	} else if sinLonRel > 1 {
		sinLonRel = 1
	}
	relLon := math.Asin(sinLonRel) * 180.0 / math.Pi
	absLon := earthCenterLon + relLon
	// Wrap to (-180, 180] so the continent table lookups are stable.
	for absLon > 180 {
		absLon -= 360
	}
	for absLon <= -180 {
		absLon += 360
	}

	// Continents painted first so clouds can layer over them.
	color := ColorEarthOcean
	covered := false
	for _, c := range earthContinents {
		if inEllipse(lat, absLon, c) {
			color = c.color
			covered = true
		}
	}
	// Clouds layer on top of land or ocean alike — except over
	// polar ice, which is already returned above.
	for _, c := range earthClouds {
		if inEllipse(lat, absLon, c) {
			return c.color
		}
	}
	_ = covered
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
func TextureFor(b bodies.CelestialBody, pxRadius int) BodyTexture {
	if pxRadius < BodyTextureMinRadius {
		return nil
	}
	switch b.ID {
	case "earth":
		return EarthPixelColor
	case "moon":
		return MoonPixelColor
	case "mars":
		return MarsPixelColor
	case "jupiter":
		return JupiterPixelColor
	}
	return nil
}

// BodyHasTexture reports whether TextureFor would return non-nil.
// Convenience wrapper for callers that just need the boolean
// (e.g. "should I suppress the body-identity glyph?").
func BodyHasTexture(b bodies.CelestialBody, pxRadius int) bool {
	return TextureFor(b, pxRadius) != nil
}
