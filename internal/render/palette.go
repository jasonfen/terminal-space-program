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
var (
	ColorAlert        = lipgloss.Color("#FF5F5F") // hard errors, peri-below-surface
	ColorWarning      = lipgloss.Color("#FFAF00") // warp clamps, near-collision
	ColorPlannedNode  = lipgloss.Color("#5FD7FF") // maneuver-node markers
	ColorTrajectory   = lipgloss.Color("#FFFFFF") // fallback trajectory preview
	ColorCurrentOrbit = lipgloss.Color("#A8B8C8") // craft's live Keplerian ellipse — pale slate, distinct from any body palette and from maneuver-leg colors
	ColorCraftMarker  = lipgloss.Color("#FFD93D") // craft icon when zoomed out (orbit too small to render) — saturated yellow distinct from Sun gold-white, amber leg, and every body color
	ColorForeignSOI   = lipgloss.Color("#D75FFF") // post-SOI-crossing trajectory segments
	ColorDim          = lipgloss.Color("#5F5F5F") // background / inactive
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
//   1. b.Color (per-body JSON field, v0.7.1+) when non-empty.
//   2. bodyPalette table (legacy hardcoded source-of-truth).
//   3. StellarTint by temperature for stars without an explicit entry.
//   4. Per-bodyType default.
//   5. ColorTrajectory for unrecognised types.
func ColorFor(b bodies.CelestialBody) lipgloss.Color {
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

// BodyRings returns the inner and outer ring radii (meters from body
// center) for ringed bodies, or ok=false if the body has no
// renderable rings. Numbers are face-on simplifications — our
// equatorial-plane projection always shows rings as concentric
// circles, not the inclination-dependent ellipses real Saturn rings
// project to.
func BodyRings(bodyID string) (innerR, outerR float64, ok bool) {
	switch bodyID {
	case "saturn":
		// Saturn's main ring system: inner edge of D ring ~67k km,
		// outer edge of A ring ~140k km. Use B–A range for a single
		// visible ring outline (the brightest part).
		return 92000e3, 137000e3, true
	}
	return 0, 0, false
}
