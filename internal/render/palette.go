// Package render holds the visual constant tables shared across screens
// — body color palette, UI tier colors for status / nodes / trajectory.
//
// v0.7.1+: per-body color lives on bodies.CelestialBody.Color (a hex
// string in systems/*.json). The hardcoded bodyPalette table here
// stays as a backward-compat fallback for callers / tests that
// construct CelestialBody literals without a Color field; v0.8 will
// drop the table once v0.7.1+ saves are in the wild.
package render

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// UI tier colors — used for non-body UI elements that need consistent
// semantic colors across screens. Independent of the body palette so
// editing one doesn't shift the other.
//
// Far-side rendering convention (v0.10.6+): the orbit-trace draw path
// renders the far-side arc (depth < 0 relative to the orbit's
// primary, under the canvas's active basis) at stride*2 stipple in
// the **same hue** as the near-side arc. Terminals can't do alpha,
// so per-sample stride flips are the lossless equivalent of KSP's
// dim-the-back-arc rendering. Callers don't pick a separate "back"
// colour; the canvas helper (DrawEllipseOffsetFarSideDashed) handles
// it automatically. ColorBodyOrbit is the one new entry the
// convention surfaced — body orbits had no dedicated colour before
// v0.10.6, defaulting to whatever Plot wrote (terminal default).
var (
	ColorAlert        = lipgloss.Color("#FF5F5F") // hard errors, peri-below-surface
	ColorWarning      = lipgloss.Color("#FFAF00") // warp clamps, near-collision
	ColorPlannedNode  = lipgloss.Color("#5FD7FF") // maneuver-node markers
	ColorTrajectory   = lipgloss.Color("#FFFFFF") // fallback trajectory preview
	ColorCurrentOrbit = lipgloss.Color("#A8B8C8") // craft's live Keplerian ellipse — pale slate, distinct from any body palette and from maneuver-leg colors
	ColorBodyOrbit    = lipgloss.Color("#6E6E6E") // heliocentric body orbits — dim grey backdrop (KSP-aligned: body orbits are quiet so craft / maneuver layers pop). v0.10.6+
	ColorCraftMarker  = lipgloss.Color("#FFD93D") // craft icon when zoomed out (orbit too small to render) — saturated yellow distinct from Sun gold-white, amber leg, and every body color
	ColorForeignSOI   = lipgloss.Color("#D75FFF") // post-SOI-crossing trajectory segments
	ColorDim          = lipgloss.Color("#5F5F5F") // background / inactive
	ColorFlame        = lipgloss.Color("#FF8C42") // engine-firing flame trail (warm orange — distinct from craftmarker yellow)
	ColorTarget       = lipgloss.Color("#3DDC84") // active TARGET craft + its orbit — vivid green, matches DOCK READY callout, distinct from craft yellow / current-orbit slate / planned-node cyan
	// v0.11.5: RCS puffs paint as a bright-white origin pixel + dim-grey
	// tip in both OrbitView and LaunchView, so a small cold thruster
	// puff reads visually distinct from the hot fuel-coloured main flame.
	// Pre-v0.11.5 puffs used amber `ColorWarning` + orange `ColorFlame`
	// which clashed with the new fuel-type flame palette.
	ColorRCSPuffOrigin = lipgloss.Color("#FFFFFF") // bright white — thruster ignition pixel
	ColorRCSPuffTip    = ColorDim                  // dim grey — fading trail tip
)

// maneuverSegmentPalette cycles through distinct colors per planted
// maneuver node so the player can read which post-burn leg belongs
// to which burn. Indices wrap around once exhausted; the cycle is
// short enough that two-burn (Hohmann) and three-burn (Hohmann +
// mid-course correction) plans get unique colors.
var maneuverSegmentPalette = []lipgloss.Color{
	lipgloss.Color("#5FD7FF"), // cyan — first maneuver leg
	lipgloss.Color("#5FFF87"), // mint — second
	lipgloss.Color("#FFAF00"), // amber — third
	lipgloss.Color("#FF87D7"), // pink  — fourth
}

// ManeuverSegmentColor returns the color for the post-maneuver-N
// orbit leg. N=0 is the orbit immediately after the first planted
// burn fires; N=1 the orbit after the second; etc. Wraps around the
// palette table.
func ManeuverSegmentColor(n int) lipgloss.Color {
	if n < 0 {
		n = 0
	}
	return maneuverSegmentPalette[n%len(maneuverSegmentPalette)]
}

// bodyPalette maps a body's ID → its rendered color. IDs are the same
// ones in the systems/*.json files (e.g. "earth", "moon", "io").
//
// Picks reminiscent of each body's actual appearance in visible light.
// Non-Sol stars use a temperature-keyed tint via StellarTint() rather
// than a per-system entry here.
//
// As of v0.7.1 the canonical source for body color is each body's
// JSON `color` field (loaded into CelestialBody.Color). This table is
// retained as a backward-compat fallback for callers that construct
// CelestialBody literals without a Color (e.g. test fixtures). v0.8
// will drop it once v0.7.1+ saves are in the wild.
var bodyPalette = map[string]lipgloss.Color{
	// Sol & inner planets
	"sun":     "#FFE9A6", // gold-white
	"mercury": "#9C9C9C", // grey
	"venus":   "#E8D08F", // pale yellow / cloud-tops
	"earth":   "#5BB3FF", // ocean blue (continents would need a per-pixel render)
	"mars":    "#C1440E", // rust

	// Earth's moon
	"moon": "#C8C8C8", // pale grey

	// Mars moons
	"phobos": "#5A5048", // dark grey
	"deimos": "#5A5048", // dark grey

	// Jupiter & Galilean
	"jupiter":  "#C9925E", // banded ochre
	"io":       "#E8D940", // sulfur yellow
	"europa":   "#E5DBC6", // off-white ice
	"ganymede": "#A48E6A", // tan
	"callisto": "#5C4A36", // dark tan

	// Saturn + named moons
	"saturn":    "#E0CFA1", // pale gold
	"titan":     "#D67F2A", // burnt orange (haze)
	"enceladus": "#FFFFFF", // bright icy white

	// Outer ice giants
	"uranus":  "#7DD3D8", // cyan
	"neptune": "#3D5BFF", // deep blue
}

// ColorFor returns the palette color for a body. Resolution order:
//   1. theme.json `bodies` override (v0.7.2+) keyed by body ID.
//   2. b.Color (per-body JSON field, v0.7.1+) when non-empty.
//   3. bodyPalette table (legacy hardcoded source-of-truth).
//   4. StellarTint by temperature for stars without an explicit entry.
//   5. Per-bodyType default.
//   6. ColorTrajectory for unrecognised types.
func ColorFor(b bodies.CelestialBody) lipgloss.Color {
	if hex, ok := bodyOverrides[b.ID]; ok {
		return lipgloss.Color(hex)
	}
	if b.Color != "" {
		return lipgloss.Color(b.Color)
	}
	if c, ok := bodyPalette[b.ID]; ok {
		return c
	}
	if b.Temperature > 0 && (b.BodyType == "Star" || b.StellarClass != "") {
		return StellarTint(b.Temperature)
	}
	switch b.BodyType {
	case "Star":
		return "#FFE9A6"
	case "Planet":
		return "#7BA1B6" // generic muted blue-grey
	case "Moon":
		return "#9A9A9A" // generic grey
	default:
		return ColorTrajectory
	}
}

// StellarTint maps a star's effective temperature (Kelvin) to a
// rough visible-light color. Used for non-Sol stars that don't have
// a hand-picked entry in bodyPalette. Banding is coarse — five
// buckets covering M-dwarf through O-class.
func StellarTint(tempK float64) lipgloss.Color {
	switch {
	case tempK < 3700: // M (red dwarf)
		return "#FF6E40"
	case tempK < 5200: // K (orange)
		return "#FFB04A"
	case tempK < 6000: // G (yellow — sun-like)
		return "#FFE9A6"
	case tempK < 7500: // F (yellow-white)
		return "#F8F2D6"
	case tempK < 10000: // A (white)
		return "#E5ECFF"
	default: // B / O (blue-white)
		return "#9CB8FF"
	}
}

// Style returns a lipgloss.Style with the body's color as the
// foreground. Convenience wrapper for HUD / body-info text — call
// Style(b).Render("Earth") to print a colored label.
func Style(b bodies.CelestialBody) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(ColorFor(b))
}

// GlyphFor returns the body-identity Unicode glyph for the given
// body. Used as a single-cell overlay on top of the body's drawille
// disk so different body types read distinctly even at small pixel-
// radius. v0.5.12+.
//
//   - Star  → ☉ (sun symbol)
//   - Gas giant (radius > 20 000 km) → ◉ (fisheye)
//   - Terrestrial planet → ● (filled circle)
//   - Moon → ○ (open circle)
//
// Returns 0 (zero rune) when no overlay is appropriate (e.g. system
// primary already has a ring+dot draw style and shouldn't be
// double-glyphed).
func GlyphFor(b bodies.CelestialBody) rune {
	switch b.BodyType {
	case "Star":
		return '☉'
	case "Moon":
		return '○'
	case "Planet":
		if b.MeanRadius > 20000 {
			return '◉'
		}
		return '●'
	}
	return 0
}

// Saturn ring palette. v0.8.5.7+ — the C / B / A bands have
// different brightness (B is brightest, A medium, C dim), and the
// thin F ring sits just outside A as a bright accent. The Cassini
// Division between B and A is a visible gap (no band drawn), and
// the F ring's narrow width is the "thread loose around Saturn"
// look from Cassini photos.
const (
	ColorSaturnRingC = lipgloss.Color("#806E50") // C ring, dim grey-tan
	ColorSaturnRingB = lipgloss.Color("#E8D9A8") // B ring, brightest pale gold
	ColorSaturnRingA = lipgloss.Color("#B89968") // A ring, medium ochre
	ColorSaturnRingF = lipgloss.Color("#F0E0B8") // F ring, thin bright thread
)

// RingBand is one annular band of a body's ring system: an inner
// and outer radius (meters from body center) plus the band color.
// v0.8.5.7+ — multi-band ring rendering for Saturn.
type RingBand struct {
	InnerR float64        // meters from body center
	OuterR float64        // meters from body center
	Color  lipgloss.Color // band color (passed through to canvas pixel tags)
}

// BodyRings returns the inner and outer radii of a body's ring
// system overall (union of all bands), or ok=false if the body
// has no renderable rings. Backward-compat wrapper for callers
// that just need the outer extent (e.g. "should I render rings
// at the current zoom?"). New code should prefer BodyRingBands
// for the per-band detail.
func BodyRings(bodyID string) (innerR, outerR float64, ok bool) {
	bands := BodyRingBands(bodyID)
	if len(bands) == 0 {
		return 0, 0, false
	}
	innerR = bands[0].InnerR
	outerR = bands[len(bands)-1].OuterR
	for _, b := range bands {
		if b.InnerR < innerR {
			innerR = b.InnerR
		}
		if b.OuterR > outerR {
			outerR = b.OuterR
		}
	}
	return innerR, outerR, true
}

// BodyRingBands returns the per-band ring layout for ringed bodies.
// Bands are returned in order from innermost to outermost so
// callers can iterate radially. Saturn's layout: C ring (dim) →
// B ring (brightest) → Cassini Division (gap, not in list) →
// A ring (medium) → F ring (thin bright thread). v0.8.5.7+.
//
// Numbers are textbook radii (NASA Cassini fact-sheet); the
// equatorial-plane projection means callers see them foreshortened
// per the canvas view direction.
func BodyRingBands(bodyID string) []RingBand {
	switch bodyID {
	case "saturn":
		return []RingBand{
			// C ring: 74,000 km – 92,000 km (dim, grey-tan).
			{InnerR: 74000e3, OuterR: 92000e3, Color: ColorSaturnRingC},
			// B ring: 92,000 km – 117,500 km (brightest, pale gold).
			{InnerR: 92000e3, OuterR: 117500e3, Color: ColorSaturnRingB},
			// Cassini Division: 117,500 km – 122,000 km — skip; a
			// visible gap reads in the rendered ring annulus.
			// A ring: 122,000 km – 137,000 km (medium ochre).
			{InnerR: 122000e3, OuterR: 137000e3, Color: ColorSaturnRingA},
			// F ring: ~140,000 km, very narrow (~500 km wide). Thin
			// bright accent just outside A.
			{InnerR: 140000e3, OuterR: 140500e3, Color: ColorSaturnRingF},
		}
	}
	return nil
}
