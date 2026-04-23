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

// TestStubsReturnErr: planner/hohmann.go and planner/lambert.go must
// return ErrNotImplemented rather than panicking or silently succeeding.
func TestStubsReturnErr(t *testing.T) {
	if _, _, _, err := HohmannTransfer(7e6, 4.2e7, 3.986e14); err == nil {
		t.Error("HohmannTransfer returned nil error; stubbed implementation should return ErrNotImplemented")
	}
	if _, _, err := LambertSolve(
		orbital.Vec3{X: 7e6}, orbital.Vec3{X: 4.2e7}, 1000, 3.986e14); err == nil {
		t.Error("LambertSolve returned nil error; stubbed implementation should return ErrNotImplemented")
	}
}
