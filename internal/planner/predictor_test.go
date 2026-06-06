package planner

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestPredictCircularReturnsToStart: a circular orbit propagated for
// exactly one period should return to (approximately) the starting point.
// Validates that Predict drives StepVerlet correctly.
func TestPredictCircularReturnsToStart(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	mu := earth.GravitationalParameter()
	r0 := earth.RadiusMeters() + 200e3
	v0 := math.Sqrt(mu / r0)

	start := physics.StateVector{
		R: orbital.Vec3{X: r0},
		V: orbital.Vec3{Y: v0},
	}
	period := 2 * math.Pi * math.Sqrt(r0*r0*r0/mu)
	pts := Predict(start, mu, period, 64)
	if len(pts) != 64 {
		t.Fatalf("expected 64 points, got %d", len(pts))
	}
	// Last point should be near start (within 1% of r0 radial distance).
	closure := pts[len(pts)-1].Sub(pts[0]).Norm()
	if closure > r0*0.01 {
		t.Errorf("orbit closure %.3e m (%.4f%% of r0)", closure, closure/r0*100)
	}
}

// TestPredictPreservesRadiusForCircularOrbit: the predictor should keep
// a circular orbit circular. Sample many points around one period and
// assert |r| stays within 1% of r0 — the physical invariant that
// matters for the shadow-trajectory preview.
func TestPredictPreservesRadiusForCircularOrbit(t *testing.T) {
	mu := 3.986e14
	r0 := 7e6
	v0 := math.Sqrt(mu / r0)
	start := physics.StateVector{R: orbital.Vec3{X: r0}, V: orbital.Vec3{Y: v0}}

	period := 2 * math.Pi * math.Sqrt(r0*r0*r0/mu)
	pts := Predict(start, mu, period, 128)

	maxDev := 0.0
	for _, p := range pts {
		d := math.Abs(p.Norm() - r0)
		if d > maxDev {
			maxDev = d
		}
	}
	if maxDev/r0 > 0.01 {
		t.Errorf("predictor radial excursion %.3e m (%.4f%% of r0)",
			maxDev, maxDev/r0*100)
	}
}

// TestPredictLargeHorizonSmallSamples — Predict must honor its documented
// dt < period/100 sub-step invariant even when the caller asks for a long
// horizon with very few samples. The old unconditional `nSub > 256` clamp
// capped sub-steps at 256 regardless of horizon, so a 100-period / 2-sample
// request produced dt ≈ 0.39·period — far past the Verlet stability knee —
// and the orbit diverged energetically (radius ran off toward escape).
// Raising the clamp to a perf ceiling that still respects the invariant
// keeps dt at period/100, where symplectic Verlet conserves energy and the
// radius stays put. We assert radius preservation, not positional closure:
// over 100 periods Verlet accrues harmless *phase* drift even with a
// correct dt, but the *energy* (the radius of a circular orbit) must not
// diverge — that divergence is exactly the clamp bug. (#91)
func TestPredictLargeHorizonSmallSamples(t *testing.T) {
	mu := 3.986e14
	r0 := 6.371e6 + 200e3
	v0 := math.Sqrt(mu / r0)
	start := physics.StateVector{R: orbital.Vec3{X: r0}, V: orbital.Vec3{Y: v0}}
	period := 2 * math.Pi * math.Sqrt(r0*r0*r0/mu)

	// 100 periods, only 2 samples (start + end) — forces nSub past the old
	// 256 cap. The end radius of this circular orbit must stay near r0.
	pts := Predict(start, mu, 100*period, 2)
	endR := pts[len(pts)-1].Norm()
	if d := math.Abs(endR-r0) / r0; d > 0.02 {
		t.Errorf("100-period/2-sample end radius %.3e m = %.2f·r0 — sub-step clamp violated dt<period/100 and the orbit diverged", endR, endR/r0)
	}
}
