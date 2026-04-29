package planner

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestLambertCurtisExample52 reproduces Curtis "Orbital Mechanics for
// Engineering Students" Example 5.2 — a geocentric Lambert problem with
// known v1, v2 published in the textbook. All values converted from km
// to SI (m, m/s, m³/s²). Tolerance: 0.5% of |v|.
func TestLambertCurtisExample52(t *testing.T) {
	r1 := orbital.Vec3{X: 5000e3, Y: 10000e3, Z: 2100e3}
	r2 := orbital.Vec3{X: -14600e3, Y: 2500e3, Z: 7000e3}
	dt := 3600.0
	mu := 398600e9 // 398 600 km³/s² → m³/s²

	v1, v2, err := LambertSolve(r1, r2, dt, mu, false)
	if err != nil {
		t.Fatalf("LambertSolve: %v", err)
	}

	// Curtis published values (km/s → m/s).
	wantV1 := orbital.Vec3{X: -5992.5, Y: 1925.4, Z: 3245.6}
	wantV2 := orbital.Vec3{X: -3312.5, Y: -4196.6, Z: -385.29}

	const relTol = 5e-3 // 0.5% — Curtis values rounded to 4 sig figs
	if d := v1.Sub(wantV1).Norm() / wantV1.Norm(); d > relTol {
		t.Errorf("v1 mismatch: got %+v, want %+v (rel err %.2e)", v1, wantV1, d)
	}
	if d := v2.Sub(wantV2).Norm() / wantV2.Norm(); d > relTol {
		t.Errorf("v2 mismatch: got %+v, want %+v (rel err %.2e)", v2, wantV2, d)
	}
}

// TestLambertRoundTrip: solve Lambert for (r1, r2, dt), then propagate
// (r1, v1) forward by dt via Verlet sub-stepping. The resulting r should
// be within integrator tolerance of r2. This catches sign errors or
// branch-selection mistakes that the textbook check alone would miss.
func TestLambertRoundTrip(t *testing.T) {
	r1 := orbital.Vec3{X: 5000e3, Y: 10000e3, Z: 2100e3}
	r2 := orbital.Vec3{X: -14600e3, Y: 2500e3, Z: 7000e3}
	dt := 3600.0
	mu := 398600e9

	v1, _, err := LambertSolve(r1, r2, dt, mu, false)
	if err != nil {
		t.Fatalf("LambertSolve: %v", err)
	}

	// Sub-step Verlet at period/100 cadence.
	state := physics.StateVector{R: r1, V: v1}
	period := orbitalPeriod(state, mu)
	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}
	nSteps := int(math.Ceil(dt / maxStep))
	if nSteps < 200 {
		nSteps = 200 // floor for short transfers — Verlet drift dominates
	}
	step := dt / float64(nSteps)
	for i := 0; i < nSteps; i++ {
		state = physics.StepVerlet(state, mu, step)
	}

	// 2% of |r2| accommodates Verlet-not-symplectic drift on a half-orbit
	// transfer; the Lambert math itself is much tighter (caught by
	// TestLambertCurtisExample52 above).
	if d := state.R.Sub(r2).Norm() / r2.Norm(); d > 0.02 {
		t.Errorf("round-trip: predicted r differs from r2 by %.2e (rel)", d)
	}
}

// TestLambertMultiRevN1: a long-duration transfer that couldn't fit in
// the single-rev domain. Round-trip via Verlet should still land at r2
// within integrator tolerance, confirming the N=1 branch returns a
// physically meaningful v1.
func TestLambertMultiRevN1(t *testing.T) {
	r1 := orbital.Vec3{X: 7e6}
	// Nudge r2 ~10° off antipodal so the transfer plane is well-defined.
	r2 := orbital.Vec3{X: -1e7 * math.Cos(math.Pi*10/180), Y: 1e7 * math.Sin(math.Pi*10/180)}
	mu := 398600e9

	// Circular period at r≈8e6 is ~2π·sqrt(8e6³/μ) ≈ 7100 s. Pick
	// dt = 20000 s — about 2.8 circular periods, comfortably into the
	// N=1 domain while staying inside N=1's bracket.
	dt := 20000.0

	v1, _, err := LambertSolveRev(r1, r2, dt, mu, 1, false)
	if err != nil {
		t.Fatalf("LambertSolveRev N=1: %v", err)
	}

	// Round-trip: propagate (r1, v1) for dt via Verlet. Should land near r2.
	state := physics.StateVector{R: r1, V: v1}
	period := 2 * math.Pi * math.Sqrt(math.Pow(r1.Norm(), 3)/mu)
	maxStep := period / 100.0
	nSteps := int(math.Ceil(dt / maxStep))
	if nSteps < 1000 {
		nSteps = 1000 // multi-rev drifts more with Verlet, force fine sub-stepping
	}
	step := dt / float64(nSteps)
	for i := 0; i < nSteps; i++ {
		state = physics.StepVerlet(state, mu, step)
	}
	if d := state.R.Sub(r2).Norm() / r2.Norm(); d > 0.05 {
		t.Errorf("N=1 round-trip: r mismatch %.2e (rel) — v1=%+v", d, v1)
	}
}

// TestLambertMultiRevRejectsNegative: N < 0 should error rather than
// panic or silently degrade to single-rev.
func TestLambertMultiRevRejectsNegative(t *testing.T) {
	r1 := orbital.Vec3{X: 7e6}
	r2 := orbital.Vec3{X: -1e7}
	if _, _, err := LambertSolveRev(r1, r2, 3600, 398600e9, -1, false); err == nil {
		t.Error("expected error for nRev=-1")
	}
}

// TestLambertSolveRetrogradeRoundTrips: round-trip the retrograde
// branch the same way TestLambertRoundTrip does for prograde — the
// flag must produce a physically consistent (r1, v1) → r2 transfer,
// just along the opposite arc around the line of nodes. v0.7.5+.
func TestLambertSolveRetrogradeRoundTrips(t *testing.T) {
	r1 := orbital.Vec3{X: 5000e3, Y: 10000e3, Z: 2100e3}
	r2 := orbital.Vec3{X: -14600e3, Y: 2500e3, Z: 7000e3}
	dt := 3600.0
	mu := 398600e9

	v1, _, err := LambertSolve(r1, r2, dt, mu, true)
	if err != nil {
		t.Fatalf("LambertSolve(retrograde): %v", err)
	}

	state := physics.StateVector{R: r1, V: v1}
	period := orbitalPeriod(state, mu)
	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}
	nSteps := int(math.Ceil(dt / maxStep))
	if nSteps < 200 {
		nSteps = 200
	}
	step := dt / float64(nSteps)
	for i := 0; i < nSteps; i++ {
		state = physics.StepVerlet(state, mu, step)
	}
	if d := state.R.Sub(r2).Norm() / r2.Norm(); d > 0.02 {
		t.Errorf("retrograde round-trip: r mismatch %.2e (rel) — v1=%+v", d, v1)
	}
}

// TestLambertSolveRetrogradeProducesDifferentVelocity: the prograde
// and retrograde solutions for the same (r1, r2, dt) describe
// different transfer arcs, so the returned v1 vectors must differ
// in direction. Locks in that the flag actually selects a different
// branch instead of silently returning the prograde result.
func TestLambertSolveRetrogradeProducesDifferentVelocity(t *testing.T) {
	r1 := orbital.Vec3{X: 5000e3, Y: 10000e3, Z: 2100e3}
	r2 := orbital.Vec3{X: -14600e3, Y: 2500e3, Z: 7000e3}
	dt := 3600.0
	mu := 398600e9

	v1Pro, _, err := LambertSolve(r1, r2, dt, mu, false)
	if err != nil {
		t.Fatalf("prograde: %v", err)
	}
	v1Retro, _, err := LambertSolve(r1, r2, dt, mu, true)
	if err != nil {
		t.Fatalf("retrograde: %v", err)
	}
	// Same |v1| isn't guaranteed (different transfer ellipses), but
	// the directions must clearly differ — sanity-check by requiring
	// the relative difference to exceed a generous threshold.
	delta := v1Pro.Sub(v1Retro).Norm()
	if delta < 0.1*v1Pro.Norm() {
		t.Errorf("prograde and retrograde v1 are essentially identical: pro=%+v retro=%+v", v1Pro, v1Retro)
	}
}

// TestLambertEarthToMarsHohmann: a near-coplanar Hohmann-like transfer
// from 1 AU circular to 1.524 AU circular. Lambert is genuinely
// degenerate at exactly 180° (the transfer plane is undetermined) so we
// nudge the geometry by 1° off antipodal — close enough that |v1| should
// still match the analytical Hohmann perihelion velocity within 1.5%.
func TestLambertEarthToMarsHohmann(t *testing.T) {
	const AU = 1.495978707e11
	const muSun = 1.32712440018e20

	const dthetaOff = math.Pi - 0.0175 // 179°, just shy of antipodal
	r1 := orbital.Vec3{X: AU}
	r2 := orbital.Vec3{X: 1.524 * AU * math.Cos(dthetaOff), Y: 1.524 * AU * math.Sin(dthetaOff)}

	a_t := (1 + 1.524) / 2 * AU
	dt := math.Pi * math.Sqrt(a_t*a_t*a_t/muSun) // half-period of transfer ellipse

	v1, _, err := LambertSolve(r1, r2, dt, muSun, false)
	if err != nil {
		t.Fatalf("LambertSolve: %v", err)
	}

	wantV1Mag := math.Sqrt(muSun * (2/AU - 1/a_t))
	gotV1Mag := v1.Norm()
	if d := math.Abs(gotV1Mag-wantV1Mag) / wantV1Mag; d > 0.015 {
		t.Errorf("Earth→Mars Hohmann |v1|: got %.1f m/s, want %.1f m/s (rel %.2e)",
			gotV1Mag, wantV1Mag, d)
	}
}
