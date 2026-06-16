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

// TestPerifocalBasisEquatorialIdentity (v0.6.4+): an equatorial
// circular orbit with Ω = ω = 0 has its perifocal x̂ aligned with
// world +X and ŷ with world +Y. The basis equals the identity in
// this degenerate case — equatorial-aligned cases are the frame
// transformations new readers double-check first.
func TestPerifocalBasisEquatorialIdentity(t *testing.T) {
	el := Elements{A: 1e7, E: 0, I: 0, Omega: 0, Arg: 0}
	xHat, yHat := PerifocalBasis(el)
	expectPFVec(t, "xHat", xHat, Vec3{X: 1})
	expectPFVec(t, "yHat", yHat, Vec3{Y: 1})
}

// TestPerifocalBasisOrthonormal: for arbitrary (Ω, i, ω) the
// returned basis must be orthonormal. Catches sign / column-mix
// errors in the rotation-matrix transcription that would foreshorten
// or skew the orbit-perpendicular projection.
func TestPerifocalBasisOrthonormal(t *testing.T) {
	cases := []Elements{
		{A: 1e7, E: 0.1, I: 30 * math.Pi / 180, Omega: 45 * math.Pi / 180, Arg: 60 * math.Pi / 180},
		{A: 4e8, E: 0.05, I: 5.145 * math.Pi / 180, Omega: 125.08 * math.Pi / 180, Arg: 318.15 * math.Pi / 180},
		{A: 1e6, E: 0.7, I: 90 * math.Pi / 180, Omega: 0, Arg: 0}, // polar
	}
	for _, el := range cases {
		xHat, yHat := PerifocalBasis(el)
		if math.Abs(xHat.Norm()-1) > 1e-9 {
			t.Errorf("|xHat| = %.10f, want 1 for el=%+v", xHat.Norm(), el)
		}
		if math.Abs(yHat.Norm()-1) > 1e-9 {
			t.Errorf("|yHat| = %.10f, want 1 for el=%+v", yHat.Norm(), el)
		}
		dot := xHat.X*yHat.X + xHat.Y*yHat.Y + xHat.Z*yHat.Z
		if math.Abs(dot) > 1e-9 {
			t.Errorf("xHat · yHat = %.10f, want 0 for el=%+v", dot, el)
		}
	}
}

// TestPerifocalBasisProjectsOrbitFlat: a point on the orbit at true
// anomaly ν projects onto the perifocal basis with zero orbit-normal
// component. The orbit is flat in (xHat, yHat) coords — exactly what
// the orbit-perpendicular view mode exploits.
func TestPerifocalBasisProjectsOrbitFlat(t *testing.T) {
	el := Elements{A: 1e7, E: 0.2, I: 30 * math.Pi / 180, Omega: 45 * math.Pi / 180, Arg: 60 * math.Pi / 180}
	xHat, yHat := PerifocalBasis(el)
	// Orbit-normal direction = xHat × yHat.
	zHat := Vec3{
		X: xHat.Y*yHat.Z - xHat.Z*yHat.Y,
		Y: xHat.Z*yHat.X - xHat.X*yHat.Z,
		Z: xHat.X*yHat.Y - xHat.Y*yHat.X,
	}
	for _, nu := range []float64{0, math.Pi / 2, math.Pi, 1.7} {
		r := PositionAtTrueAnomaly(el, nu)
		px := r.X*xHat.X + r.Y*xHat.Y + r.Z*xHat.Z
		py := r.X*yHat.X + r.Y*yHat.Y + r.Z*yHat.Z
		pz := r.X*zHat.X + r.Y*zHat.Y + r.Z*zHat.Z
		rMag := r.Norm()
		if rMag == 0 {
			continue
		}
		if math.Abs(pz)/rMag > 1e-9 {
			t.Errorf("orbit-normal component at ν=%.2f: %.6f / %.0f m (rel %.2e)",
				nu, pz, rMag, math.Abs(pz)/rMag)
		}
		recon := math.Sqrt(px*px + py*py)
		if math.Abs(recon-rMag)/rMag > 1e-9 {
			t.Errorf("ν=%.2f: in-plane projection %.0f m vs |r| %.0f m (rel %.2e)",
				nu, recon, rMag, math.Abs(recon-rMag)/rMag)
		}
	}
}

func expectPFVec(t *testing.T, name string, got, want Vec3) {
	t.Helper()
	const tol = 1e-12
	if math.Abs(got.X-want.X) > tol || math.Abs(got.Y-want.Y) > tol || math.Abs(got.Z-want.Z) > tol {
		t.Errorf("%s = %+v, want %+v", name, got, want)
	}
}

// TestEccentricityVectorPointsToPeriapsis pins the analytic-perilune
// foundation (ADR 0023 B): the eccentricity vector points from the focus to
// periapsis with magnitude e, and PeriapsisDirection · rp reconstructs the
// periapsis position. Placing the craft at periapsis (r along +x, v
// perpendicular, speed above circular) makes the answer +x by construction.
func TestEccentricityVectorPointsToPeriapsis(t *testing.T) {
	const mu = 3.986e14
	rp := 7.0e6
	vc := math.Sqrt(mu / rp)
	r := Vec3{X: rp}
	v := Vec3{Y: vc * 1.1} // 10 % above circular → current point is periapsis

	e := EccentricityVector(r, v, mu)
	wantE := 1.1*1.1 - 1 // (v²·r/μ − 1) with r·v = 0
	if math.Abs(e.Norm()-wantE) > 1e-9 {
		t.Errorf("|e| = %.9f, want %.9f", e.Norm(), wantE)
	}
	if el := ElementsFromState(r, v, mu); math.Abs(el.E-e.Norm()) > 1e-9 {
		t.Errorf("|e| = %.9f disagrees with Elements.E = %.9f", e.Norm(), el.E)
	}
	dir, ok := PeriapsisDirection(r, v, mu)
	if !ok {
		t.Fatal("PeriapsisDirection ok=false for an eccentric orbit")
	}
	if math.Abs(dir.X-1) > 1e-12 || math.Abs(dir.Y) > 1e-12 || math.Abs(dir.Z) > 1e-12 {
		t.Errorf("periapsis direction = %+v, want +x", dir)
	}
	// rp·dir reconstructs the periapsis position we started at.
	if peri := dir.Scale(rp); math.Abs(peri.X-rp) > 1e-3 || math.Abs(peri.Y) > 1e-3 {
		t.Errorf("rp·dir = %+v, want the start position %+v", peri, r)
	}
}

// TestPeriapsisDirectionCircularUndefined: a (near-)circular orbit has no
// defined periapsis direction, so PeriapsisDirection reports ok=false and
// the caller falls back rather than placing a marker on an arbitrary axis.
func TestPeriapsisDirectionCircularUndefined(t *testing.T) {
	const mu = 3.986e14
	rp := 7.0e6
	r := Vec3{X: rp}
	v := Vec3{Y: math.Sqrt(mu / rp)} // exactly circular
	if _, ok := PeriapsisDirection(r, v, mu); ok {
		t.Error("PeriapsisDirection ok=true for a circular orbit, want false")
	}
}
