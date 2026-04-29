package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestMoonPixelColorHighland(t *testing.T) {
	// Lunar far southwestern highland — clear of all maria and craters.
	// Lat ≈ -50, lon ≈ 60. ny = sin(-50°) ≈ -0.766;
	// nx = cos(-50°)*sin(60°) ≈ 0.557.
	r := 32
	dx := int(0.557 * float64(r))
	dy := int(-0.766 * float64(r))
	got := MoonPixelColor(dx, dy, r)
	if got != ColorMoonHighland {
		t.Errorf("highland sample = %q, want %q", string(got), string(ColorMoonHighland))
	}
}

func TestMoonPixelColorMare(t *testing.T) {
	// Mare Imbrium, lat ≈ 33°, lon ≈ -16°. ny = sin(33°) ≈ 0.545;
	// nx = cos(33°)*sin(-16°) ≈ -0.231.
	r := 32
	dx := int(-0.231 * float64(r))
	dy := int(0.545 * float64(r))
	got := MoonPixelColor(dx, dy, r)
	if got != ColorMoonMare {
		t.Errorf("Mare Imbrium sample = %q, want %q", string(got), string(ColorMoonMare))
	}
}

func TestMoonPixelColorCraterRay(t *testing.T) {
	// Tycho, lat ≈ -43°, lon ≈ -11°. ny = sin(-43°) ≈ -0.682;
	// nx = cos(-43°)*sin(-11°) ≈ -0.140.
	r := 32
	dx := int(-0.140 * float64(r))
	dy := int(-0.682 * float64(r))
	got := MoonPixelColor(dx, dy, r)
	if got != ColorMoonRay {
		t.Errorf("Tycho sample = %q, want %q", string(got), string(ColorMoonRay))
	}
}

func TestMoonPixelColorDeterministic(t *testing.T) {
	r := 32
	for dy := -r; dy <= r; dy += 4 {
		for dx := -r; dx <= r; dx += 4 {
			a := MoonPixelColor(dx, dy, r)
			b := MoonPixelColor(dx, dy, r)
			if a != b {
				t.Fatalf("non-deterministic at (%d,%d): %q vs %q", dx, dy, string(a), string(b))
			}
		}
	}
}

func TestMoonPixelColorMultiColor(t *testing.T) {
	r := 32
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(MoonPixelColor(dx, dy, r))] = true
		}
	}
	for _, want := range []string{string(ColorMoonHighland), string(ColorMoonMare), string(ColorMoonRay)} {
		if !seen[want] {
			t.Errorf("disk at r=%d missing color %q", r, want)
		}
	}
}

func TestTextureForDispatch(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", BodyType: "Planet"}
	moon := bodies.CelestialBody{ID: "moon", BodyType: "Moon"}
	mars := bodies.CelestialBody{ID: "mars", BodyType: "Planet"}

	if TextureFor(earth, BodyTextureMinRadius-1) != nil {
		t.Error("Earth below threshold should have no texture")
	}
	if TextureFor(earth, BodyTextureMinRadius) == nil {
		t.Error("Earth at threshold should have texture")
	}
	if TextureFor(moon, BodyTextureMinRadius-1) != nil {
		t.Error("Moon below threshold should have no texture")
	}
	if TextureFor(moon, BodyTextureMinRadius) == nil {
		t.Error("Moon at threshold should have texture")
	}
	// v0.7.6+: Mars and Jupiter now have textures.
	if TextureFor(mars, 64) == nil {
		t.Error("Mars should have texture (added in v0.7.6)")
	}
	jupiter := bodies.CelestialBody{ID: "jupiter", BodyType: "Planet"}
	if TextureFor(jupiter, 64) == nil {
		t.Error("Jupiter should have texture (added in v0.7.6)")
	}
	saturn := bodies.CelestialBody{ID: "saturn", BodyType: "Planet"}
	if TextureFor(saturn, 64) != nil {
		t.Error("Saturn should not have texture (not yet implemented)")
	}

	// Earth and Moon must dispatch to different functions — sanity
	// check that the switch is not collapsing.
	earthTex := TextureFor(earth, 32)
	moonTex := TextureFor(moon, 32)
	const r = 32
	// Look at the same pixel offset on both: (0, 0) is the disk
	// center. Earth at (0,0) → lat=0, lon=0 → over Africa → land
	// color. Moon at (0,0) → lat=0, lon=0 → highland.
	if string(earthTex(0, 0, r)) == string(moonTex(0, 0, r)) {
		t.Error("Earth and Moon textures returned identical color at disk center")
	}
}
