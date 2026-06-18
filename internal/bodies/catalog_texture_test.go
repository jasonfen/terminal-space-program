package bodies

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestHashViewExcludesTexture is the load-bearing save-compat guard for
// ADR 0024: the catalog hash must be identical whether or not bodies
// carry a cosmetic texture block, so adding textures (PR2+) never
// rejects an existing save with ErrCatalogMismatch.
func TestHashViewExcludesTexture(t *testing.T) {
	plain := []System{{
		Name: "Test",
		Bodies: []CelestialBody{
			{ID: "a", Name: "A", MeanRadius: 100},
			{ID: "b", Name: "B", MeanRadius: 200},
		},
	}}
	withTex := []System{{
		Name: "Test",
		Bodies: []CelestialBody{
			{ID: "a", Name: "A", MeanRadius: 100, Texture: &Texture{
				Base:       "#123456",
				Continents: []TextureEllipse{{Lat: 1, Lon: 2, LatR: 3, LonR: 4, Color: "#abcdef"}},
			}},
			{ID: "b", Name: "B", MeanRadius: 200, Texture: &Texture{Base: "#000000"}},
		},
	}}

	a, err := json.Marshal(hashView(plain))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(hashView(withTex))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("hashView differs with vs. without texture:\n plain: %s\n   tex: %s", a, b)
	}
}

// TestHashViewDoesNotMutateInput confirms hashView copies rather than
// aliases — zeroing texture for the hash must not strip textures off
// the live catalog the renderer reads.
func TestHashViewDoesNotMutateInput(t *testing.T) {
	in := []System{{
		Name:   "Test",
		Bodies: []CelestialBody{{ID: "a", Texture: &Texture{Base: "#123456"}}},
	}}
	_ = hashView(in)
	if in[0].Bodies[0].Texture == nil {
		t.Fatal("hashView zeroed the texture on its input; must copy")
	}
}
