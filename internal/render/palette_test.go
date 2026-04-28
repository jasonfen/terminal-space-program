package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestColorForKnownBodies: every body in Sol must carry a Color field
// in its JSON. Catches the case where a moon is added to sol.json but
// its color value is missed. Pre-v0.7.1 this checked the bodyPalette
// table; the table is now a fallback only.
func TestColorForKnownBodies(t *testing.T) {
	sys, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for _, s := range sys {
		if s.Name != "Sol" {
			continue
		}
		for _, b := range s.Bodies {
			if b.Color == "" {
				t.Errorf("Sol body %q (%s) has no Color field — add `color` to its sol.json entry",
					b.ID, b.EnglishName)
			}
		}
	}
}

// TestColorForJSONFieldMatchesPaletteTable: transitional check while
// the bodyPalette table still exists. For every Sol body, the JSON
// color must match the legacy table entry — drift between the two
// sources of truth indicates someone updated one without the other.
// Drop this test alongside the bodyPalette table in v0.8.
func TestColorForJSONFieldMatchesPaletteTable(t *testing.T) {
	sys, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for _, s := range sys {
		if s.Name != "Sol" {
			continue
		}
		for _, b := range s.Bodies {
			tableC, ok := bodyPalette[b.ID]
			if !ok {
				continue
			}
			if string(tableC) != b.Color {
				t.Errorf("Sol body %q: JSON color %q != table %q", b.ID, b.Color, string(tableC))
			}
		}
	}
}

// TestColorForPrefersJSONField: when a body carries an explicit Color,
// ColorFor must return it verbatim, ignoring the table fallback.
func TestColorForPrefersJSONField(t *testing.T) {
	b := bodies.CelestialBody{
		ID:       "earth",
		BodyType: "Planet",
		Color:    "#123456",
	}
	got := ColorFor(b)
	if string(got) != "#123456" {
		t.Errorf("ColorFor with explicit Color = %q, want #123456", string(got))
	}
}

// TestColorForFallsBackToTable: a body without a Color field must
// resolve via the bodyPalette table (preserves backward compat for
// test fixtures and any callers constructing CelestialBody literals).
func TestColorForFallsBackToTable(t *testing.T) {
	b := bodies.CelestialBody{
		ID:       "earth",
		BodyType: "Planet",
		// Color intentionally empty
	}
	got := ColorFor(b)
	want := bodyPalette["earth"]
	if got != want {
		t.Errorf("ColorFor with empty Color = %q, want table value %q", string(got), string(want))
	}
}

// TestGlyphForBodyTypes: v0.5.12 — different body types resolve to
// distinct identity glyphs. Star → ☉, gas giant → ◉, terrestrial → ●,
// moon → ○.
func TestGlyphForBodyTypes(t *testing.T) {
	star := bodies.CelestialBody{BodyType: "Star"}
	gas := bodies.CelestialBody{BodyType: "Planet", MeanRadius: 70000}
	terr := bodies.CelestialBody{BodyType: "Planet", MeanRadius: 6371}
	moon := bodies.CelestialBody{BodyType: "Moon"}
	cases := []struct {
		b    bodies.CelestialBody
		want rune
	}{
		{star, '☉'},
		{gas, '◉'},
		{terr, '●'},
		{moon, '○'},
	}
	for _, c := range cases {
		got := GlyphFor(c.b)
		if got != c.want {
			t.Errorf("%+v → %q, want %q", c.b, got, c.want)
		}
	}
}

// TestBodyRingsForSaturn: v0.5.11 — Saturn has rendered rings; other
// bodies don't.
func TestBodyRingsForSaturn(t *testing.T) {
	if _, _, ok := BodyRings("saturn"); !ok {
		t.Error("BodyRings(\"saturn\") should return ok=true")
	}
	if _, _, ok := BodyRings("earth"); ok {
		t.Error("BodyRings(\"earth\") should return ok=false (no rings)")
	}
}

// TestStellarTintBuckets: spot-check that StellarTint returns
// distinct colors across the temperature ladder. Catches accidental
// collapse of the bucket boundaries (e.g. wrong threshold ordering).
func TestStellarTintBuckets(t *testing.T) {
	cases := []struct {
		tempK float64
		name  string
	}{
		{2500, "M-dwarf"},
		{4500, "K"},
		{5778, "G (sun)"},
		{6800, "F"},
		{9000, "A"},
		{20000, "B/O"},
	}
	seen := make(map[string]string)
	for _, c := range cases {
		got := string(StellarTint(c.tempK))
		if prev, ok := seen[got]; ok {
			t.Errorf("StellarTint collision: %s and %s both → %s",
				prev, c.name, got)
		}
		seen[got] = c.name
	}
}
