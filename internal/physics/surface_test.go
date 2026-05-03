package physics

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestClampToSurfaceAboveNoOp: a craft above the surface is returned
// unchanged with hit=false.
func TestClampToSurfaceAboveNoOp(t *testing.T) {
	earth := earthWithAtm()
	r := earth.RadiusMeters() + 500e3 // 500 km altitude
	in := StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: 7600},
		M: 50000,
	}
	out, hit := ClampToSurface(in, earth)
	if hit {
		t.Fatalf("hit reported above surface")
	}
	if out != in {
		t.Errorf("state mutated above surface: got %+v want %+v", out, in)
	}
}

// TestClampToSurfaceBelowSnapsToSurface: a craft below the surface is
// projected back to |r| = RadiusMeters along its current r̂ and velocity
// is zeroed (the "landed" state).
func TestClampToSurfaceBelowSnapsToSurface(t *testing.T) {
	earth := earthWithAtm()
	radius := earth.RadiusMeters()
	// Position 100 km under the surface in a non-axis-aligned direction.
	dir := orbital.Vec3{X: 1, Y: 1, Z: 0}
	dirMag := dir.Norm()
	deep := radius - 100e3
	in := StateVector{
		R: orbital.Vec3{X: dir.X / dirMag * deep, Y: dir.Y / dirMag * deep},
		V: orbital.Vec3{X: -50, Y: -200, Z: 30},
		M: 50000,
	}
	out, hit := ClampToSurface(in, earth)
	if !hit {
		t.Fatalf("expected hit below surface")
	}
	if math.Abs(out.R.Norm()-radius) > 1e-6 {
		t.Errorf("projected |r|: got %g, want %g", out.R.Norm(), radius)
	}
	// r̂ preserved.
	wantX := dir.X / dirMag * radius
	wantY := dir.Y / dirMag * radius
	if math.Abs(out.R.X-wantX) > 1e-6 || math.Abs(out.R.Y-wantY) > 1e-6 || math.Abs(out.R.Z) > 1e-6 {
		t.Errorf("direction not preserved: got %+v", out.R)
	}
	if out.V != (orbital.Vec3{}) {
		t.Errorf("velocity not zeroed: got %+v", out.V)
	}
	if out.M != in.M {
		t.Errorf("mass changed: got %g want %g", out.M, in.M)
	}
}

// TestClampToSurfaceAtRZero: degenerate r=0 input picks +X for surface
// direction so callers never see NaN.
func TestClampToSurfaceAtRZero(t *testing.T) {
	earth := earthWithAtm()
	radius := earth.RadiusMeters()
	in := StateVector{R: orbital.Vec3{}, V: orbital.Vec3{X: 100}, M: 50000}
	out, hit := ClampToSurface(in, earth)
	if !hit {
		t.Fatalf("expected hit at r=0")
	}
	if out.R.X != radius || out.R.Y != 0 || out.R.Z != 0 {
		t.Errorf("r=0 fallback: got %+v want {%g 0 0}", out.R, radius)
	}
	if out.V != (orbital.Vec3{}) {
		t.Errorf("velocity not zeroed: got %+v", out.V)
	}
}

// TestClampToSurfaceExactlyAtSurfaceNoOp: |r| == radius is on the surface,
// not below — no clamp, no zero-velocity.
func TestClampToSurfaceExactlyAtSurfaceNoOp(t *testing.T) {
	earth := earthWithAtm()
	radius := earth.RadiusMeters()
	in := StateVector{
		R: orbital.Vec3{X: radius},
		V: orbital.Vec3{Y: 7800},
		M: 50000,
	}
	out, hit := ClampToSurface(in, earth)
	if hit {
		t.Fatalf("hit reported at exactly r=radius")
	}
	if out != in {
		t.Errorf("state mutated at surface: got %+v want %+v", out, in)
	}
}

// TestClampToSurfaceZeroRadiusNoOp: a body with no radius (defensive) is
// treated as no-clamp so we never divide by zero or snap to the origin.
func TestClampToSurfaceZeroRadiusNoOp(t *testing.T) {
	earth := earthWithAtm()
	earth.MeanRadius = 0
	in := StateVector{R: orbital.Vec3{X: 1000}, V: orbital.Vec3{Y: 1}, M: 50000}
	out, hit := ClampToSurface(in, earth)
	if hit {
		t.Fatalf("hit reported with zero-radius body")
	}
	if out != in {
		t.Errorf("state mutated with zero-radius body")
	}
}
