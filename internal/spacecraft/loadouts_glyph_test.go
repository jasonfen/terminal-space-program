package spacecraft

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/render"
)

// reservedMarkerGlyphs is the set of orbital-marker runes (ADR 0020) that
// no vessel may use — a craft sharing a marker's glyph is exactly the
// ship-looks-like-an-apsis collision this guard exists to prevent.
func reservedMarkerGlyphs() map[rune]render.MarkerType {
	types := []render.MarkerType{
		render.MarkerApoapsis,
		render.MarkerPeriapsis,
		render.MarkerAscendingNode,
		render.MarkerDescendingNode,
		render.MarkerPerilune,
		render.MarkerClosestApproach,
		render.MarkerManeuver,
	}
	set := make(map[rune]render.MarkerType, len(types))
	for _, t := range types {
		set[render.MarkerGlyph(t)] = t
	}
	return set
}

// TestCatalogGlyphsAreUnifiedVesselGlyph locks two invariants together:
// every loadout + stage + stage-module glyph is the single VesselGlyph
// (vessels are told apart by Colour, not shape), and therefore none of
// them collides with a reserved orbital-marker glyph. A new catalog
// entry that hand-picks a marker shape trips this test.
func TestCatalogGlyphsAreUnifiedVesselGlyph(t *testing.T) {
	reserved := reservedMarkerGlyphs()

	check := func(label, glyph string) {
		t.Helper()
		if glyph != VesselGlyph {
			t.Errorf("%s glyph = %q, want the unified VesselGlyph %q", label, glyph, VesselGlyph)
		}
		for _, r := range glyph {
			if mt, clash := reserved[r]; clash {
				t.Errorf("%s glyph %q collides with reserved marker glyph (marker type %d)", label, glyph, mt)
			}
		}
	}

	for id, l := range Loadouts {
		check("loadout "+id, l.Glyph)
		for _, s := range l.Stages {
			check("loadout "+id+" stage "+s.Name, s.Glyph)
		}
	}
	for id, m := range StageCatalog {
		check("stage module "+id, m.Glyph)
	}

	// Sanity: VesselGlyph itself must not be a marker glyph.
	for _, r := range VesselGlyph {
		if _, clash := reserved[r]; clash {
			t.Fatalf("VesselGlyph %q is itself a reserved marker glyph", VesselGlyph)
		}
	}
}
