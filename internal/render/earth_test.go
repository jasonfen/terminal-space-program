package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestEarthPixelColorOcean(t *testing.T) {
	// South Atlantic mid-latitudes — lat ≈ -30°, lon ≈ -25°, well
	// off Brazil. With v0.7.6+'s earthCenterLon = -30°, relative
	// longitude ≈ 5°, so nx ≈ cos(-30°)*sin(5°) ≈ 0.075 and
	// ny ≈ sin(-30°) = -0.5. Far from continents and the storm
	// cloud track (lat=-50) — should read clean ocean.
	r := 32
	dx := int(0.075 * float64(r))
	dy := int(-0.5 * float64(r))
	got := EarthPixelColor(dx, dy, r)
	if got != ColorEarthOcean {
		t.Errorf("South Atlantic midlat = %q, want ocean %q", string(got), string(ColorEarthOcean))
	}
}

func TestEarthPixelColorLand(t *testing.T) {
	// Central / southern Africa: lat ≈ -10°, lon ≈ 25°. With
	// v0.7.6+'s earthCenterLon = -30°, relative longitude is 55°,
	// so nx = cos(-10°)*sin(55°) ≈ 0.807 and ny = sin(-10°) ≈
	// -0.174. Below the Sahara desert overlay — pure ColorEarthLand
	// territory.
	r := 32
	dx := int(0.807 * float64(r))
	dy := int(-0.174 * float64(r))
	got := EarthPixelColor(dx, dy, r)
	if got != ColorEarthLand {
		t.Errorf("Central Africa = %q, want land %q", string(got), string(ColorEarthLand))
	}
}

func TestEarthPixelColorDeterministic(t *testing.T) {
	// Calling twice with the same inputs returns the same color —
	// catches accidental introduction of nondeterministic state
	// (e.g. random clouds without seeding).
	r := 32
	for dy := -r; dy <= r; dy += 4 {
		for dx := -r; dx <= r; dx += 4 {
			a := EarthPixelColor(dx, dy, r)
			b := EarthPixelColor(dx, dy, r)
			if a != b {
				t.Fatalf("non-deterministic at (%d,%d): %q vs %q", dx, dy, string(a), string(b))
			}
		}
	}
}

func TestEarthPixelColorMultiColor(t *testing.T) {
	// At a usable zoom (r=32) the disk must contain at least one
	// land pixel and one ocean pixel — otherwise the texture is no
	// better than a solid disk.
	r := 32
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(EarthPixelColor(dx, dy, r))] = true
		}
	}
	for _, want := range []string{string(ColorEarthOcean), string(ColorEarthLand), string(ColorEarthCloud)} {
		if !seen[want] {
			t.Errorf("disk at r=%d missing color %q", r, want)
		}
	}
}

func TestBodyHasTextureGate(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", BodyType: "Planet"}
	mars := bodies.CelestialBody{ID: "mars", BodyType: "Planet"}

	if render := BodyHasTexture(earth, EarthTextureMinRadius-1); render {
		t.Error("Earth below threshold should not use texture")
	}
	if render := BodyHasTexture(earth, EarthTextureMinRadius); !render {
		t.Error("Earth at threshold should use texture")
	}
	if render := BodyHasTexture(earth, 64); !render {
		t.Error("Earth at large radius should use texture")
	}
	// v0.7.6+: Mars and Jupiter now have textures too.
	if render := BodyHasTexture(mars, 64); !render {
		t.Error("Mars should use texture (added in v0.7.6)")
	}
	jupiter := bodies.CelestialBody{ID: "jupiter", BodyType: "Planet"}
	if render := BodyHasTexture(jupiter, 64); !render {
		t.Error("Jupiter should use texture (added in v0.7.6)")
	}
	venus := bodies.CelestialBody{ID: "venus", BodyType: "Planet"}
	if render := BodyHasTexture(venus, 64); render {
		t.Error("Venus should not use texture (not yet implemented)")
	}
}
