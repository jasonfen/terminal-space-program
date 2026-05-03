package render

import (
	"testing"
)

func TestIoPixelColorBaseAndPatera(t *testing.T) {
	// Off-feature lat/lon → sulfur base.
	got := IoPixelColor(0, 0, 32, 0, 0)
	if got != ColorIoBase {
		t.Errorf("Io center = %q, want base %q", string(got), string(ColorIoBase))
	}
	// Disk should contain at least one patera pixel — six paterae
	// across the table at r=32 must drop something visible.
	r := 32
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(IoPixelColor(dx, dy, r, 0, 0))] = true
		}
	}
	if !seen[string(ColorIoPatera)] && !seen[string(ColorIoFresh)] {
		t.Error("Io disk has no dark or fresh patera at r=32 — table coverage too sparse")
	}
}

func TestEuropaPixelColorIceAndLineae(t *testing.T) {
	// Off-linea sample → ice base. lat 50, lon -100 → far from
	// every linea entry in the table.
	r := 32
	dy := int(0.766 * float64(r))   // sin(50°)
	dx := int(-0.633 * float64(r))  // cos(50°)*sin(-100°) ≈ -0.633
	got := EuropaPixelColor(dx, dy, r, 0, 0)
	if got != ColorEuropaIce {
		t.Errorf("Europa off-linea = %q, want ice %q", string(got), string(ColorEuropaIce))
	}
	// Disk must contain at least one linea pixel at r=32.
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(EuropaPixelColor(dx, dy, r, 0, 0))] = true
		}
	}
	if !seen[string(ColorEuropaLine)] {
		t.Error("Europa disk shows no lineae at r=32 — table too sparse")
	}
}

func TestGanymedeHasDarkAndBright(t *testing.T) {
	r := 32
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(GanymedePixelColor(dx, dy, r, 0, 0))] = true
		}
	}
	for _, want := range []string{string(ColorGanymedeBright), string(ColorGanymedeDark)} {
		if !seen[want] {
			t.Errorf("Ganymede disk missing color %q", want)
		}
	}
}

func TestCallistoBaseAndCraters(t *testing.T) {
	r := 32
	r2 := r * r
	seen := map[string]bool{}
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r2 {
				continue
			}
			seen[string(CallistoPixelColor(dx, dy, r, 0, 0))] = true
		}
	}
	if !seen[string(ColorCallistoBase)] {
		t.Error("Callisto disk missing dark base color")
	}
	if !seen[string(ColorCallistoCrater)] {
		t.Error("Callisto disk missing crater accent — table too sparse")
	}
}

func TestGalileanTexturesDeterministic(t *testing.T) {
	tests := []struct {
		name string
		fn   func(int, int, int, float64, float64) string
	}{
		{"io", func(dx, dy, r int, lat, lon float64) string { return string(IoPixelColor(dx, dy, r, lat, lon)) }},
		{"europa", func(dx, dy, r int, lat, lon float64) string { return string(EuropaPixelColor(dx, dy, r, lat, lon)) }},
		{"ganymede", func(dx, dy, r int, lat, lon float64) string { return string(GanymedePixelColor(dx, dy, r, lat, lon)) }},
		{"callisto", func(dx, dy, r int, lat, lon float64) string { return string(CallistoPixelColor(dx, dy, r, lat, lon)) }},
	}
	r := 32
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for dy := -r; dy <= r; dy += 4 {
				for dx := -r; dx <= r; dx += 4 {
					a := tt.fn(dx, dy, r, 0, 12.5)
					b := tt.fn(dx, dy, r, 0, 12.5)
					if a != b {
						t.Fatalf("non-deterministic at (%d,%d): %q vs %q", dx, dy, a, b)
					}
				}
			}
		})
	}
}
