package render

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestMarkerGlyph(t *testing.T) {
	cases := []struct {
		t    MarkerType
		want rune
	}{
		{MarkerApoapsis, '▲'},
		{MarkerPeriapsis, '▼'},
		{MarkerAscendingNode, '◇'},
		{MarkerDescendingNode, '◆'},
		{MarkerPerilune, '⊕'},
		{MarkerClosestApproach, '✕'},
		{MarkerManeuver, 'Δ'},
	}
	for _, c := range cases {
		if got := MarkerGlyph(c.t); got != c.want {
			t.Errorf("MarkerGlyph(%d) = %q, want %q", c.t, got, c.want)
		}
	}
	// Unknown type renders something visible, never the zero rune.
	if got := MarkerGlyph(MarkerType(999)); got == 0 {
		t.Errorf("MarkerGlyph(unknown) = 0, want a visible fallback glyph")
	}
}

func TestMarkerColorByType(t *testing.T) {
	cases := []struct {
		t    MarkerType
		want lipgloss.Color
	}{
		{MarkerApoapsis, ColorMarkerApoapsis},
		{MarkerPeriapsis, ColorMarkerPeriapsis},
		{MarkerAscendingNode, ColorMarkerAscendingNode},
		{MarkerDescendingNode, ColorMarkerDescendingNode},
		{MarkerPerilune, ColorMarkerPerilune},
		{MarkerClosestApproach, ColorMarkerClosestApproach},
	}
	for _, c := range cases {
		if got := MarkerColor(c.t, MarkerNominal, ""); got != c.want {
			t.Errorf("MarkerColor(%d, nominal) = %q, want %q", c.t, got, c.want)
		}
	}
}

func TestMarkerColorManeuverUsesBase(t *testing.T) {
	// The maneuver marker is the documented positional-colour exception:
	// it must echo the per-node base colour, not a fixed type hue.
	base := lipgloss.Color("#ABCDEF")
	if got := MarkerColor(MarkerManeuver, MarkerNominal, base); got != base {
		t.Errorf("MarkerColor(maneuver, base=%q) = %q, want the base", base, got)
	}
	// No base supplied → fall back to the planned-node cyan, not empty.
	if got := MarkerColor(MarkerManeuver, MarkerNominal, ""); got != ColorPlannedNode {
		t.Errorf("MarkerColor(maneuver, no base) = %q, want ColorPlannedNode %q", got, ColorPlannedNode)
	}
}

func TestMarkerColorAlarmOverridesType(t *testing.T) {
	// Alarm beats type for every marker, including the maneuver exception.
	for _, mt := range []MarkerType{MarkerApoapsis, MarkerPerilune, MarkerManeuver} {
		if got := MarkerColor(mt, MarkerAlarm, lipgloss.Color("#123456")); got != ColorAlert {
			t.Errorf("MarkerColor(%d, alarm) = %q, want ColorAlert %q", mt, got, ColorAlert)
		}
	}
}

func TestMarkerColorCounterfactualDims(t *testing.T) {
	// Counterfactual must darken the nominal type colour (each channel
	// strictly lower for a non-black hue), matching ADR 0020's dim=
	// counterfactual rule used by the dual-arc render.
	nominal := MarkerColor(MarkerApoapsis, MarkerNominal, "")
	dim := MarkerColor(MarkerApoapsis, MarkerCounterfactual, "")
	if dim == nominal {
		t.Fatalf("counterfactual colour %q must differ from nominal %q", dim, nominal)
	}
	if dim != Shade(nominal, markerCounterfactualDim) {
		t.Errorf("counterfactual = %q, want Shade(nominal, %v) = %q",
			dim, markerCounterfactualDim, Shade(nominal, markerCounterfactualDim))
	}
}
