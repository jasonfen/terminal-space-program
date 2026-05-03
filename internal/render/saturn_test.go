package render

import (
	"testing"
)

func TestSaturnPixelColorEquatorialZone(t *testing.T) {
	// Disk center → lat=0 → equatorial zone (brightest).
	got := SaturnPixelColor(0, 0, 32, 0, 0)
	if got != ColorSaturnZone {
		t.Errorf("center pixel = %q, want zone %q", string(got), string(ColorSaturnZone))
	}
}

func TestSaturnPixelColorPolarHexagon(t *testing.T) {
	// lat ≈ 79.7° → north polar hexagon band. At r=64, dy=63 lands
	// just inside the [78, 90) band — at smaller radii the
	// integer-pixel quantisation jumps the limb past the band.
	r := 64
	dy := 63 // ny = 63/64 ≈ 0.984, lat ≈ 79.7°
	got := SaturnPixelColor(0, dy, r, 0, 0)
	if got != ColorSaturnSpot {
		t.Errorf("lat ≈ 79.7° pixel = %q, want polar-hex %q", string(got), string(ColorSaturnSpot))
	}
}

func TestSaturnPixelColorMultiBand(t *testing.T) {
	// Disk must contain at least zone + belt at r=32.
	r := 32
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(SaturnPixelColor(dx, dy, r, 0, 0))] = true
		}
	}
	for _, want := range []string{string(ColorSaturnZone), string(ColorSaturnBelt)} {
		if !seen[want] {
			t.Errorf("Saturn disk missing color %q", want)
		}
	}
}

func TestSaturnPixelColorDeterministic(t *testing.T) {
	r := 32
	for dy := -r; dy <= r; dy += 4 {
		for dx := -r; dx <= r; dx += 4 {
			a := SaturnPixelColor(dx, dy, r, 0, 17.5)
			b := SaturnPixelColor(dx, dy, r, 0, 17.5)
			if a != b {
				t.Fatalf("non-deterministic at (%d,%d): %q vs %q", dx, dy, string(a), string(b))
			}
		}
	}
}
