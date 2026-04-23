package orbital

import (
	"math"
	"testing"
)

// TestSolveKeplerConverges checks M → E → M' round-trip residual is below
// 1e-10 across the plan's required eccentricity grid at sampled M values.
func TestSolveKeplerConverges(t *testing.T) {
	cases := []float64{0.0, 0.1, 0.5, 0.9}
	for _, e := range cases {
		for Mdeg := 0.0; Mdeg < 360; Mdeg += 7.5 {
			M := Mdeg * math.Pi / 180.0
			E := SolveKepler(M, e)
			Mcheck := E - e*math.Sin(E)
			// Compare modulo 2π (normalization may wrap by one period).
			diff := math.Mod(Mcheck-M, 2*math.Pi)
			if diff > math.Pi {
				diff -= 2 * math.Pi
			} else if diff < -math.Pi {
				diff += 2 * math.Pi
			}
			if math.Abs(diff) > 1e-10 {
				t.Errorf("e=%.1f M=%.3f: round-trip residual %.2e", e, M, diff)
			}
		}
	}
}

// TestSolveKeplerAtZeroEccentricity: for circular orbits E == M exactly.
func TestSolveKeplerAtZeroEccentricity(t *testing.T) {
	for Mdeg := -180.0; Mdeg <= 180; Mdeg += 30 {
		M := Mdeg * math.Pi / 180.0
		E := SolveKepler(M, 0.0)
		// After normalization M is in [-π, π]; E must match.
		normM := math.Mod(M, 2*math.Pi)
		if normM > math.Pi {
			normM -= 2 * math.Pi
		} else if normM < -math.Pi {
			normM += 2 * math.Pi
		}
		if math.Abs(E-normM) > 1e-12 {
			t.Errorf("e=0 M=%.3f: E=%.12f ≠ M=%.12f", M, E, normM)
		}
	}
}

// TestTrueAnomalyConsistency: at periapsis (E=0) ν=0; at apoapsis (E=π) ν=π.
func TestTrueAnomalyConsistency(t *testing.T) {
	if ν := TrueAnomaly(0, 0.5); math.Abs(ν) > 1e-12 {
		t.Errorf("ν at E=0 should be 0, got %.12f", ν)
	}
	if ν := TrueAnomaly(math.Pi, 0.5); math.Abs(math.Abs(ν)-math.Pi) > 1e-12 {
		t.Errorf("|ν| at E=π should be π, got %.12f", ν)
	}
}
