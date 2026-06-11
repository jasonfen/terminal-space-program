package render

import "github.com/charmbracelet/lipgloss"

// MarkerType enumerates the orbital-marker vocabulary shared across the
// orbit screen (ADR 0020). Every marker renders as a single colored glyph
// stamped at its projected point via Canvas.SetCellOverlayColored — color
// encodes the type, brightness/variant encodes the state (MarkerState).
type MarkerType int

const (
	MarkerApoapsis MarkerType = iota
	MarkerPeriapsis
	MarkerAscendingNode
	MarkerDescendingNode
	MarkerPerilune // periapsis within an upcoming SOI Pass (ADR 0019)
	MarkerClosestApproach
	MarkerManeuver // per-node colour exception — see MarkerColor
)

// MarkerState modifies a marker's brightness/variant to convey what is
// happening to it (ADR 0020 B): nominal renders at full type colour, a
// counterfactual (e.g. the no-burn arc of ADR 0019's dual-arc) dims, and
// an alarm (e.g. a perilune below the surface → impact) overrides to the
// bright alert red regardless of type.
type MarkerState int

const (
	MarkerNominal MarkerState = iota
	MarkerCounterfactual
	MarkerAlarm
)

// markerCounterfactualDim scales a marker's RGB for the counterfactual
// state so the dimmed arc stays readable but visibly secondary to the
// nominal one drawn in the same SOI.
const markerCounterfactualDim = 0.5

// MarkerGlyph returns the font-safe geometric glyph rune for a marker
// type (ADR 0020 decision A). The set is deliberately basic so it renders
// in virtually any monospace font (honouring the v0.11.4 palette caution
// against exotic Unicode that shows as tofu). An unknown type falls back
// to '?' rather than 0, so a mis-wired caller draws something visible
// instead of silently nothing.
func MarkerGlyph(t MarkerType) rune {
	switch t {
	case MarkerApoapsis:
		return '▲'
	case MarkerPeriapsis:
		return '▼'
	case MarkerAscendingNode:
		return '◇'
	case MarkerDescendingNode:
		return '◆'
	case MarkerPerilune:
		return '⊕'
	case MarkerClosestApproach:
		return '✕'
	case MarkerManeuver:
		return 'Δ'
	}
	return '?'
}

// MarkerColor resolves the rendered colour for a marker from its type and
// state (ADR 0020 decision B). Colour encodes type; state adjusts
// brightness: MarkerAlarm overrides to ColorAlert (bright red, "what's
// wrong" beats "what it is"); MarkerCounterfactual dims the type colour;
// MarkerNominal uses it as-is.
//
// base is consulted only for MarkerManeuver — the one type whose colour is
// positional (it matches the post-burn leg, ManeuverSegmentColor, an
// ADR-0006-era design) rather than fixed by type. For every other type
// base is ignored.
func MarkerColor(t MarkerType, state MarkerState, base lipgloss.Color) lipgloss.Color {
	if state == MarkerAlarm {
		return ColorAlert
	}
	c := markerTypeColor(t, base)
	if state == MarkerCounterfactual {
		return Shade(c, markerCounterfactualDim)
	}
	return c
}

func markerTypeColor(t MarkerType, base lipgloss.Color) lipgloss.Color {
	switch t {
	case MarkerApoapsis:
		return ColorMarkerApoapsis
	case MarkerPeriapsis:
		return ColorMarkerPeriapsis
	case MarkerAscendingNode:
		return ColorMarkerAscendingNode
	case MarkerDescendingNode:
		return ColorMarkerDescendingNode
	case MarkerPerilune:
		return ColorMarkerPerilune
	case MarkerClosestApproach:
		return ColorMarkerClosestApproach
	case MarkerManeuver:
		// Documented exception (ADR 0020): the maneuver marker's colour is
		// the post-burn leg colour passed by the caller, not a fixed type
		// hue. Fall back to the planned-node cyan if the caller passes no
		// base (e.g. an unresolved leg).
		if base == "" {
			return ColorPlannedNode
		}
		return base
	}
	return ColorTrajectory
}
