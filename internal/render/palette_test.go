package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestColorForKnownBodies: every body in Sol must resolve to a
// non-default palette color. Catches the case where a moon is added
// to sol.json but its bodyPalette entry is missed.
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
			if _, hit := bodyPalette[b.ID]; !hit {
				t.Errorf("Sol body %q (%s) has no palette entry — add to bodyPalette",
					b.ID, b.EnglishName)
			}
		}
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
