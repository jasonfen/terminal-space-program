package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// solCatalogBody fetches a body from the embedded Sol system by id.
// The per-body texture data lives in sol.json (ADR 0024 PR4), so tests
// that exercise textured Sol bodies load the real catalog rather than
// constructing a bare body — a bare CelestialBody{ID:"earth"} carries
// no Texture and renders flat.
func solCatalogBody(t *testing.T, id string) bodies.CelestialBody {
	t.Helper()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for _, s := range systems {
		if s.Name != "Sol" {
			continue
		}
		for _, b := range s.Bodies {
			if b.ID == id {
				return b
			}
		}
	}
	t.Fatalf("Sol body %q not found", id)
	return bodies.CelestialBody{}
}

func TestBodyHasTextureGate(t *testing.T) {
	earth := solCatalogBody(t, "earth")
	mars := solCatalogBody(t, "mars")
	jupiter := solCatalogBody(t, "jupiter")
	// Venus carries no texture block (not among the 12 migrated bodies).
	venus := solCatalogBody(t, "venus")

	if BodyHasTexture(earth, EarthTextureMinRadius-1) {
		t.Error("Earth below threshold should not use texture")
	}
	if !BodyHasTexture(earth, EarthTextureMinRadius) {
		t.Error("Earth at threshold should use texture")
	}
	if !BodyHasTexture(earth, 64) {
		t.Error("Earth at large radius should use texture")
	}
	if !BodyHasTexture(mars, 64) {
		t.Error("Mars should use texture")
	}
	if !BodyHasTexture(jupiter, 64) {
		t.Error("Jupiter should use texture")
	}
	if BodyHasTexture(venus, 64) {
		t.Error("Venus should not use texture (no texture block)")
	}
}
