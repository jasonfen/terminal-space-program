package render

import (
	"testing"
)

func TestUranusPixelColorBands(t *testing.T) {
	// Equator → base color.
	got := UranusPixelColor(0, 0, 32, 0)
	if got != ColorUranusBase {
		t.Errorf("Uranus equator = %q, want base %q", string(got), string(ColorUranusBase))
	}
	// Pole (lat ~85°) → polar haze.
	r := 32
	dy := int(0.996 * float64(r))
	got = UranusPixelColor(0, dy, r, 0)
	if got != ColorUranusPole {
		t.Errorf("Uranus pole = %q, want pole %q", string(got), string(ColorUranusPole))
	}
}

func TestNeptunePixelColorBandsAndSpot(t *testing.T) {
	// Equator → base.
	got := NeptunePixelColor(0, 0, 32, 0)
	if got != ColorNeptuneBase {
		t.Errorf("Neptune equator = %q, want base %q", string(got), string(ColorNeptuneBase))
	}
	// Disk must contain at least one Great Dark Spot pixel at r=32.
	r := 32
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(NeptunePixelColor(dx, dy, r, 0))] = true
		}
	}
	if !seen[string(ColorNeptuneSpot)] {
		t.Error("Neptune disk missing Great Dark Spot at r=32")
	}
	if !seen[string(ColorNeptuneCloud)] {
		t.Error("Neptune disk missing scooter band at r=32")
	}
}

func TestIceGiantsDeterministic(t *testing.T) {
	r := 32
	for dy := -r; dy <= r; dy += 4 {
		for dx := -r; dx <= r; dx += 4 {
			au, bu := UranusPixelColor(dx, dy, r, 7.5), UranusPixelColor(dx, dy, r, 7.5)
			if au != bu {
				t.Fatalf("Uranus non-deterministic at (%d,%d)", dx, dy)
			}
			an, bn := NeptunePixelColor(dx, dy, r, 7.5), NeptunePixelColor(dx, dy, r, 7.5)
			if an != bn {
				t.Fatalf("Neptune non-deterministic at (%d,%d)", dx, dy)
			}
		}
	}
}
