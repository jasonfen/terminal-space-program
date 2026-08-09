package bodies

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestHashViewExcludesOrbitStandOff is the load-bearing save-compat
// guard for ADR 0044: OrbitStandOffKm is a spawn-form courtesy (a
// star's hand-authored Orbit Floor stand-off), not semantic catalog
// shape, so the catalog hash must be identical whether or not a body
// carries it — otherwise authoring it (or tuning it later) rejects
// every existing save with ErrCatalogMismatch. Mirrors
// TestHashViewExcludesTexture in catalog_texture_test.go.
func TestHashViewExcludesOrbitStandOff(t *testing.T) {
	plain := []System{{
		Name: "Test",
		Bodies: []CelestialBody{
			{ID: "sun", Name: "Sun", BodyType: "Star", MeanRadius: 695700},
		},
	}}
	withStandOff := []System{{
		Name: "Test",
		Bodies: []CelestialBody{
			{ID: "sun", Name: "Sun", BodyType: "Star", MeanRadius: 695700, OrbitStandOffKm: 10_000_000},
		},
	}}

	a, err := json.Marshal(hashView(plain))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(hashView(withStandOff))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("hashView differs with vs. without OrbitStandOffKm:\n plain: %s\n   std: %s", a, b)
	}
}

// TestHashViewDoesNotMutateInputOrbitStandOff confirms hashView copies
// rather than aliases — zeroing OrbitStandOffKm for the hash must not
// strip it off the live catalog the spawn form reads.
func TestHashViewDoesNotMutateInputOrbitStandOff(t *testing.T) {
	in := []System{{
		Name:   "Test",
		Bodies: []CelestialBody{{ID: "sun", BodyType: "Star", OrbitStandOffKm: 10_000_000}},
	}}
	_ = hashView(in)
	if in[0].Bodies[0].OrbitStandOffKm != 10_000_000 {
		t.Fatal("hashView zeroed OrbitStandOffKm on its input; must copy")
	}
}
