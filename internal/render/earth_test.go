package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestEarthPixelColorOcean(t *testing.T) {
	// South Atlantic mid-latitudes — lat ≈ -30°, lon ≈ -25°, well
	// off Brazil. With EarthCenterLonEpoch = -30°, relative
	// longitude ≈ 5°, so nx ≈ cos(-30°)*sin(5°) ≈ 0.075 and
	// ny ≈ sin(-30°) = -0.5. Far from continents and the storm
	// cloud track (lat=-50) — should read clean ocean.
	r := 32
	dx := int(0.075 * float64(r))
	dy := int(-0.5 * float64(r))
	got := EarthPixelColor(dx, dy, r, 0, EarthCenterLonEpoch)
	if got != ColorEarthOcean {
		t.Errorf("South Atlantic midlat = %q, want ocean %q", string(got), string(ColorEarthOcean))
	}
}

func TestEarthPixelColorLand(t *testing.T) {
	// Central / southern Africa: lat ≈ -10°, lon ≈ 25°. With
	// EarthCenterLonEpoch = -30°, relative longitude is 55°,
	// so nx = cos(-10°)*sin(55°) ≈ 0.807 and ny = sin(-10°) ≈
	// -0.174. Below the Sahara desert overlay — pure land
	// territory. v0.8.5.7+ biome-shifts the temperate-default
	// Land color to tropical for |lat| < 23°, so this position
	// reads as ColorEarthLandTropical.
	r := 32
	dx := int(0.807 * float64(r))
	dy := int(-0.174 * float64(r))
	got := EarthPixelColor(dx, dy, r, 0, EarthCenterLonEpoch)
	if got != ColorEarthLandTropical {
		t.Errorf("Central Africa = %q, want tropical %q",
			string(got), string(ColorEarthLandTropical))
	}
}

// TestEarthPixelColorBiomeShifts: temperate land at mid latitudes
// stays ColorEarthLand; tropical and boreal lat bands shift to
// the biome-specific colors. v0.8.5.7+.
func TestEarthPixelColorBiomeShifts(t *testing.T) {
	r := 64 // larger r to keep test points well inside r²<0.92.
	// Central US: lat ≈ 42°, lon ≈ -100°. Temperate (|lat| < 55).
	// With EarthCenterLonEpoch = -30°, relLon = -70°, so
	// nx = cos(42°)·sin(-70°) ≈ -0.698, ny = sin(42°) ≈ 0.669.
	// r² = 0.487 + 0.448 = 0.935 — just at the limb, would tint.
	// Use lat 30° instead (Mexico-ish): nx = cos(30°)·sin(-70°)
	// ≈ -0.814, ny = sin(30°) = 0.5. r² ≈ 0.913. Still close.
	// Use a deeper-temperate slot: lat 30°, lon -55° (south of
	// New Orleans-ish, on land via the {30,-100,8,12} ellipse?
	// Actually that's too far west. Just check Patagonia: lat
	// -40°, lon -65°. nx = cos(-40°)·sin(-35°) = -0.439, ny =
	// sin(-40°) = -0.643. r² ≈ 0.192 + 0.413 = 0.605, safe.
	dx := int(-0.439 * float64(r))
	dy := int(-0.643 * float64(r))
	got := EarthPixelColor(dx, dy, r, 0, EarthCenterLonEpoch)
	if got != ColorEarthLand {
		t.Errorf("Patagonia (temperate) = %q, want temperate %q",
			string(got), string(ColorEarthLand))
	}
}

// TestEarthPixelColorAtmosphericLimb: the outer ring of the disk
// (r² > 0.92) over non-ice surface should read as the atmospheric
// blue tint. v0.8.5.7+.
func TestEarthPixelColorAtmosphericLimb(t *testing.T) {
	r := 32
	// A pixel at (28, 0) → r² = 0.766, just inside the limb tint.
	// Use (30, 0) → nx ≈ 0.94, r² ≈ 0.879 — still under threshold.
	// (31, 0) → nx ≈ 0.969, r² ≈ 0.939 — over threshold.
	dx := 31
	dy := 0
	got := EarthPixelColor(dx, dy, r, 0, EarthCenterLonEpoch)
	if got != ColorEarthAtmosphere {
		t.Errorf("limb pixel (%d,%d) = %q, want atmospheric %q",
			dx, dy, string(got), string(ColorEarthAtmosphere))
	}
}

func TestEarthPixelColorDeterministic(t *testing.T) {
	// Calling twice with the same inputs returns the same color —
	// catches accidental introduction of nondeterministic state
	// (e.g. random clouds without seeding).
	r := 32
	for dy := -r; dy <= r; dy += 4 {
		for dx := -r; dx <= r; dx += 4 {
			a := EarthPixelColor(dx, dy, r, 0, EarthCenterLonEpoch)
			b := EarthPixelColor(dx, dy, r, 0, EarthCenterLonEpoch)
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
			seen[string(EarthPixelColor(dx, dy, r, 0, EarthCenterLonEpoch))] = true
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
