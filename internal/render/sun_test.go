package render

import (
	"testing"
)

func TestSunPixelColorCoreAndLimb(t *testing.T) {
	// Disk center → core (brightest).
	if got := SunPixelColor(0, 0, 32, 0, 0); got != ColorSunCore {
		t.Errorf("center pixel = %q, want core %q",
			string(got), string(ColorSunCore))
	}
	// Limb (r² = 0.94 at (31, 0)/32) → ColorSunLimb.
	if got := SunPixelColor(31, 0, 32, 0, 0); got != ColorSunLimb {
		t.Errorf("limb pixel = %q, want limb %q",
			string(got), string(ColorSunLimb))
	}
}

func TestSunPixelColorMidBandHasSurfaceAndSpot(t *testing.T) {
	// Sweep mid-disk pixels; expect both surface yellow and at
	// least one sunspot color in the result set.
	r := 32
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(SunPixelColor(dx, dy, r, 0, 0))] = true
		}
	}
	for _, want := range []string{
		string(ColorSunCore), string(ColorSunSurface),
		string(ColorSunLimb), string(ColorSunSpot),
	} {
		if !seen[want] {
			t.Errorf("Sun disk missing color %q", want)
		}
	}
}

func TestSunPixelColorDeterministic(t *testing.T) {
	r := 32
	for dy := -r; dy <= r; dy += 4 {
		for dx := -r; dx <= r; dx += 4 {
			a := SunPixelColor(dx, dy, r, 0, 17.5)
			b := SunPixelColor(dx, dy, r, 0, 17.5)
			if a != b {
				t.Fatalf("non-deterministic at (%d,%d): %q vs %q",
					dx, dy, string(a), string(b))
			}
		}
	}
}
