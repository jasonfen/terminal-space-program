package render

import (
	"testing"
)

// TestTextureForDispatch checks the radius gate and that distinct
// textured bodies produce distinct surfaces through the data-driven
// engine (ADR 0024 PR4 — the old per-body switch is gone; textures
// come from sol.json).
func TestTextureForDispatch(t *testing.T) {
	earth := solCatalogBody(t, "earth")
	moon := solCatalogBody(t, "moon")

	if TextureFor(earth, BodyTextureMinRadius-1, 0, 0, 0, 1, nil) != nil {
		t.Error("Earth below threshold should have no texture")
	}
	if TextureFor(earth, BodyTextureMinRadius, 0, 0, 0, 1, nil) == nil {
		t.Error("Earth at threshold should have texture")
	}
	if TextureFor(moon, BodyTextureMinRadius-1, 0, 0, 0, 1, nil) != nil {
		t.Error("Moon below threshold should have no texture")
	}
	if TextureFor(moon, BodyTextureMinRadius, 0, 0, 0, 1, nil) == nil {
		t.Error("Moon at threshold should have texture")
	}
	// Every migrated body resolves to a texture at a readable radius.
	for _, id := range []string{"sun", "mars", "jupiter", "saturn", "uranus", "neptune", "io", "europa", "ganymede", "callisto"} {
		if TextureFor(solCatalogBody(t, id), 64, 0, 0, 0, 1, nil) == nil {
			t.Errorf("%s should have a texture", id)
		}
	}

	// Earth and Moon must render different surfaces — sanity that the
	// engine isn't collapsing distinct specs. (0,0) is the disk center:
	// Earth → lat 0, lon 0 (ocean/land), Moon → highland.
	const r = 32
	earthTex := TextureFor(earth, r, 0, 0, 0, 1, nil)
	moonTex := TextureFor(moon, r, 0, 0, 0, 1, nil)
	if string(earthTex(0, 0, r)) == string(moonTex(0, 0, r)) {
		t.Error("Earth and Moon textures returned identical color at disk center")
	}
}
