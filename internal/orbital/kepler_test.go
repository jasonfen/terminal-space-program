package orbital

import (
	"math"
	"testing"
)

// TestOrbitReadoutEllipse: a known ~200-km LEO at periapsis should
// have apo > peri > 0, e ≈ 0.05, and Hyperbolic = false.
func TestOrbitReadoutEllipse(t *testing.T) {
	// Same parameters as the events_test fixture: a=6.578e6, e=0.05,
	// state at ν=0 (periapsis).
	a := 6.578e6
	e := 0.05
	mu := 3.986004418e14
	p := a * (1 - e*e)
	r := p / (1 + e) // = a(1-e) at peri
	rVec := Vec3{X: r}
	// At peri the velocity is purely transverse (no radial component).
	vp := math.Sqrt(mu/p) * (1 + e)
	vVec := Vec3{Y: vp}

	ro := OrbitReadout(rVec, vVec, mu)
	if ro.Hyperbolic {
		t.Errorf("LEO at peri: got Hyperbolic=true")
	}
	expectApo := a * (1 + e)
	expectPeri := a * (1 - e)
	if math.Abs(ro.ApoMeters-expectApo) > 1.0 {
		t.Errorf("apoapsis: got %.3f, want %.3f", ro.ApoMeters, expectApo)
	}
	if math.Abs(ro.PeriMeters-expectPeri) > 1.0 {
		t.Errorf("periapsis: got %.3f, want %.3f", ro.PeriMeters, expectPeri)
	}
	if math.Abs(ro.Eccentricity-e) > 1e-6 {
		t.Errorf("eccentricity: got %.6f, want %.6f", ro.Eccentricity, e)
	}
	// AN/DN π apart (modulo 2π).
	delta := math.Abs(ro.DescNode - ro.AscNode)
	for delta > 2*math.Pi {
		delta -= 2 * math.Pi
	}
	if math.Abs(delta-math.Pi) > 1e-9 {
		t.Errorf("DN-AN: got %.6f, want π", delta)
	}
}

// TestOrbitReadoutHyperbolic: e ≥ 1 should set Hyperbolic = true.
func TestOrbitReadoutHyperbolic(t *testing.T) {
	mu := 3.986004418e14
	r := Vec3{X: 7e6}
	// v_escape = √(2μ/r); add 20% to push hyperbolic.
	vEsc := math.Sqrt(2*mu/r.Norm()) * 1.2
	v := Vec3{Y: vEsc}
	ro := OrbitReadout(r, v, mu)
	if !ro.Hyperbolic {
		t.Errorf("expected Hyperbolic=true for v=1.2×v_esc, got Eccentricity=%.3f", ro.Eccentricity)
	}
	if ro.Eccentricity < 1 {
		t.Errorf("expected e>=1, got %.3f", ro.Eccentricity)
	}
}

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
