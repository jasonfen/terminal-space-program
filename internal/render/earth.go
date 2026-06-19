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
//
// v0.11.2+ (ADR 0003): adds (screenUpX, screenUpY) — the canvas-frame
// unit vector in the direction body-local-north at the sub-observer
// projects on screen. Caller computes it once per body per frame
// from BodyRotationAxisWorld, the canvas basis, and camDir; for the
// pre-v0.11.2 "north is canvas-up" assumption pass (0, 1).
func TextureFor(b bodies.CelestialBody, pxRadius int, subLatDeg, subLonDeg, screenUpX, screenUpY float64, light *SolarLight) BodyTexture {
	if pxRadius < BodyTextureMinRadius {
		return nil
	}
	base := bodyTextureBase(b, subLatDeg, subLonDeg, screenUpX, screenUpY)
	// Stars are the light source — exempt from day/night shading. The
	// hardcoded "sun" stays; a data-driven body declaring a star kind
	// (ADR 0024 PR3, e.g. Lumen) is exempt the same way.
	isStar := b.ID == "sun" || (b.Texture != nil && b.Texture.Star != nil)
	if base == nil || isStar || light == nil {
		return base
	}
	return func(dx, dy, r int) lipgloss.Color {
		return Shade(base(dx, dy, r), light.FactorAt(dx, dy, r, subLatDeg, subLonDeg, screenUpX, screenUpY))
	}
}

// bodyTextureBase returns the unlit per-pixel surface shader for a
// body, or nil if the body has no texture. Split out of TextureFor so
// the v0.9.6 lighting wrapper has a single base closure to darken.
//
// ADR 0024: textures are fully data-driven. A body carrying a Texture
// spec (every textured body in the embedded catalog, plus user
// overlays) is rendered by the generic engine; bodies with no spec
// render as a flat solid disk. The pre-0024 hardcoded `switch b.ID`
// dispatch to per-body Go shaders was retired in PR4.
func bodyTextureBase(b bodies.CelestialBody, subLatDeg, subLonDeg, screenUpX, screenUpY float64) BodyTexture {
	if b.Texture == nil {
		return nil
	}
	ct := compileTexture(b.Texture, b.Color)
	return func(dx, dy, r int) lipgloss.Color {
		return ct.colorAt(dx, dy, r, subLatDeg, subLonDeg, screenUpX, screenUpY)
	}
}

// BodyHasTexture reports whether TextureFor would return non-nil.
// Convenience wrapper for callers that just need the boolean
// (e.g. "should I suppress the body-identity glyph?"). The
// sub-observer point doesn't affect the gate, so the boolean form
// omits it.
func BodyHasTexture(b bodies.CelestialBody, pxRadius int) bool {
	return TextureFor(b, pxRadius, 0, 0, 0, 1, nil) != nil
}
