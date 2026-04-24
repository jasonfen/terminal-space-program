package planner

import (
	"math"
	"testing"
)

// TestEscapeBurnDeltaVZeroVInf: a v∞=0 escape (parabolic) requires
// Δv = (sqrt(2) − 1) · v_circ — Curtis Eq 8.42 boundary case.
func TestEscapeBurnDeltaVZeroVInf(t *testing.T) {
	const muEarth = 3.986e14
	const rPark = 6.578e6 // 200 km LEO
	dv, err := EscapeBurnDeltaV(0, muEarth, rPark)
	if err != nil {
		t.Fatalf("EscapeBurnDeltaV: %v", err)
	}
	vCirc := math.Sqrt(muEarth / rPark)
	want := (math.Sqrt2 - 1) * vCirc
	if d := math.Abs(dv-want) / want; d > 1e-9 {
		t.Errorf("parabolic escape Δv: got %.3f, want %.3f", dv, want)
	}
}

// TestEscapeBurnDeltaVPositiveVInf: at v∞ = 3 km/s from LEO the
// formula gives Δv ≈ 3.61 km/s (textbook Earth→Mars departure).
func TestEscapeBurnDeltaVPositiveVInf(t *testing.T) {
	const muEarth = 3.986e14
	const rPark = 6.578e6 // 200 km LEO
	dv, err := EscapeBurnDeltaV(3000, muEarth, rPark)
	if err != nil {
		t.Fatalf("EscapeBurnDeltaV: %v", err)
	}
	// v_peri = sqrt(9e6 + 2·3.986e14/6.578e6) = sqrt(9e6 + 1.212e8) ≈ 11420 m/s
	// v_circ = sqrt(3.986e14/6.578e6) ≈ 7785 m/s
	// Δv ≈ 3635 m/s
	want := 3635.0
	if d := math.Abs(dv-want) / want; d > 0.01 {
		t.Errorf("Δv at v∞=3 km/s: got %.1f, want ≈%.1f m/s (rel %.2e)", dv, want, d)
	}
}

// TestEscapeBurnDeltaVRejectsBadInputs: non-positive mu / r and
// negative v∞ all surface as errors.
func TestEscapeBurnDeltaVRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name              string
		vInf, mu, r       float64
	}{
		{"negative vInf", -1, 1e14, 7e6},
		{"zero mu", 1, 0, 7e6},
		{"negative mu", 1, -1, 7e6},
		{"zero r", 1, 1e14, 0},
		{"negative r", 1, 1e14, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := EscapeBurnDeltaV(c.vInf, c.mu, c.r); err == nil {
				t.Errorf("expected error for %s", c.name)
			}
		})
	}
}

// TestCaptureBurnDeltaVMatchesEscape: by symmetry, capture Δv into a
// given orbit equals the escape Δv from that same orbit at the same
// hyperbolic excess.
func TestCaptureBurnDeltaVMatchesEscape(t *testing.T) {
	const muMars = 4.282837e13
	const rCap = 9.378e6 // ~3000 km Mars orbit
	const vInf = 2650.0
	esc, _ := EscapeBurnDeltaV(vInf, muMars, rCap)
	cap, _ := CaptureBurnDeltaV(vInf, muMars, rCap)
	if math.Abs(esc-cap) > 1e-12 {
		t.Errorf("capture %v != escape %v", cap, esc)
	}
}
