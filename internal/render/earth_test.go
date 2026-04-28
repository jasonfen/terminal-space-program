package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestEarthPixelColorOcean(t *testing.T) {
	// Pacific Ocean, mid-latitudes south of the equator, away from any
	// cloud streak. Lat ~-30, lon ~-160 → nx ≈ -0.296, ny = -0.5.
	r := 32
	dx := int(-0.296 * float64(r))
	dy := int(-0.5 * float64(r))
	got := EarthPixelColor(dx, dy, r)
	if got != ColorEarthOcean {
		t.Errorf("Pacific midlat = %q, want ocean %q", string(got), string(ColorEarthOcean))
	}
}

func TestEarthPixelColorLand(t *testing.T) {
	// Center of Africa: lat ≈ 5°, lon ≈ 20°. ny = sin(5°) ≈ 0.087,
	// nx = cos(5°)*sin(20°) ≈ 0.341.
	r := 32
	dx := int(0.341 * float64(r))
	dy := int(0.087 * float64(r))
	got := EarthPixelColor(dx, dy, r)
	if got != ColorEarthLand {
		t.Errorf("Africa center = %q, want land %q", string(got), string(ColorEarthLand))
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
	if render := BodyHasTexture(mars, 64); render {
		t.Error("Mars should not use texture (Earth-only for now)")
	}
}
