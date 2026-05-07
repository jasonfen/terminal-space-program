package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// testEarth synthesises a minimal Earth-like body for tests that
// just need RadiusMeters / GravitationalParameter / SideralRotation.
func testEarth() bodies.CelestialBody {
	return bodies.CelestialBody{
		ID:              "earth",
		MeanRadius:      6371,                                                // km
		Mass:            bodies.Mass{Value: 5.972, Exponent: 24},             // kg
		SideralRotation: 23.9345,                                             // hours (sidereal day)
	}
}

// TestApplyPitchTrimZeroIsNoop — zero trim returns dir unchanged.
func TestApplyPitchTrimZeroIsNoop(t *testing.T) {
	r := orbital.Vec3{X: 6.371e6}
	dir := orbital.Vec3{X: 1}
	got := ApplyPitchTrim(dir, r, 0)
	if got != dir {
		t.Errorf("zero trim altered dir: got %+v, want %+v", got, dir)
	}
}

// TestApplyPitchTrimEastTiltsRadialEast — at the equator on +X,
// trimming +90° east should rotate radial+ (X-axis) to point along
// east (+Y axis). Confirms the rotation direction convention.
func TestApplyPitchTrimEastTiltsRadialEast(t *testing.T) {
	r := orbital.Vec3{X: 6.371e6}
	radialOut := orbital.Vec3{X: 1}
	got := ApplyPitchTrim(radialOut, r, math.Pi/2)
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y-1) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Errorf("90° east trim of radial+: got %+v, want (0, 1, 0)", got)
	}
}

// TestApplyPitchTrimSmallEastBoost — at the equator, trimming +5°
// east gives a vector mostly +X with a small +Y component.
func TestApplyPitchTrimSmallEastBoost(t *testing.T) {
	r := orbital.Vec3{X: 6.371e6}
	radialOut := orbital.Vec3{X: 1}
	got := ApplyPitchTrim(radialOut, r, 5*math.Pi/180)
	wantX := math.Cos(5 * math.Pi / 180)
	wantY := math.Sin(5 * math.Pi / 180)
	if math.Abs(got.X-wantX) > 1e-9 {
		t.Errorf("5° east trim: X = %.6f, want %.6f", got.X, wantX)
	}
	if math.Abs(got.Y-wantY) > 1e-9 {
		t.Errorf("5° east trim: Y = %.6f, want %.6f", got.Y, wantY)
	}
}

// TestBurnDirectionSurfacePrograde — once the craft has surface-
// relative velocity, BurnSurfacePrograde aligns to it.
func TestBurnDirectionSurfacePrograde(t *testing.T) {
	earth := testEarth()
	r := orbital.Vec3{X: earth.RadiusMeters()}
	v := orbital.Vec3{Y: 7800} // orbital-class east, in inertial frame
	s := &Spacecraft{Primary: earth}
	s.State.R = r
	s.State.V = v
	dir := s.BurnDirection(BurnSurfacePrograde)
	// v_surface = v - ω×r ≈ (0, 7335, 0). Direction = +Y.
	if math.Abs(dir.X) > 1e-6 || math.Abs(dir.Y-1) > 1e-6 || math.Abs(dir.Z) > 1e-6 {
		t.Errorf("surface prograde: got %+v, want ≈ (0, 1, 0)", dir)
	}
}

// TestBurnDirectionSurfaceProgradeAtRest — pre-launch, surface
// velocity is zero; surface prograde returns the zero vector so
// the burn no-ops until the player is moving.
func TestBurnDirectionSurfaceProgradeAtRest(t *testing.T) {
	earth := testEarth()
	r := orbital.Vec3{X: earth.RadiusMeters()}
	omega := physics.AtmosphereOmega(earth)
	v := omega.Cross(r) // exact surface co-rotation
	s := &Spacecraft{Primary: earth}
	s.State.R = r
	s.State.V = v
	dir := s.BurnDirection(BurnSurfacePrograde)
	if dir != (orbital.Vec3{}) {
		t.Errorf("surface prograde at rest: got %+v, want zero vector", dir)
	}
}

// TestBurnDirectionSurfaceRetrograde — flip of surface prograde.
func TestBurnDirectionSurfaceRetrograde(t *testing.T) {
	earth := testEarth()
	r := orbital.Vec3{X: earth.RadiusMeters()}
	v := orbital.Vec3{Y: 7800}
	s := &Spacecraft{Primary: earth}
	s.State.R = r
	s.State.V = v
	dir := s.BurnDirection(BurnSurfaceRetrograde)
	if math.Abs(dir.X) > 1e-6 || math.Abs(dir.Y-(-1)) > 1e-6 || math.Abs(dir.Z) > 1e-6 {
		t.Errorf("surface retrograde: got %+v, want ≈ (0, -1, 0)", dir)
	}
}

// TestBurnDirectionAppliesPitchTrim — trim feeds through any mode.
func TestBurnDirectionAppliesPitchTrim(t *testing.T) {
	earth := testEarth()
	r := orbital.Vec3{X: earth.RadiusMeters()}
	v := orbital.Vec3{Y: 100}
	s := &Spacecraft{Primary: earth}
	s.State.R = r
	s.State.V = v
	s.PitchTrim = 5 * math.Pi / 180
	got := s.BurnDirection(BurnRadialOut)
	want := ApplyPitchTrim(orbital.Vec3{X: 1}, r, 5*math.Pi/180)
	if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
		t.Errorf("pitch trim on radial+: got %+v, want %+v", got, want)
	}
}

// TestDirectionUnitTargetPrograde — target ahead in +Y, faster:
// v_target − v_active = +Y, so BurnTargetPrograde = +Y.
func TestDirectionUnitTargetPrograde(t *testing.T) {
	rA := orbital.Vec3{X: 7e6}
	vA := orbital.Vec3{Y: 7500}
	rT := orbital.Vec3{X: 7e6, Y: 1000}
	vT := orbital.Vec3{Y: 7600}
	got := DirectionUnitTarget(BurnTargetPrograde, rA, vA, rT, vT)
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y-1) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Errorf("target prograde: got %+v, want (0, 1, 0)", got)
	}
}

// TestDirectionUnitTargetRetrograde — flip of TargetPrograde.
func TestDirectionUnitTargetRetrograde(t *testing.T) {
	rA := orbital.Vec3{X: 7e6}
	vA := orbital.Vec3{Y: 7500}
	rT := orbital.Vec3{X: 7e6, Y: 1000}
	vT := orbital.Vec3{Y: 7600}
	got := DirectionUnitTarget(BurnTargetRetrograde, rA, vA, rT, vT)
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y-(-1)) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Errorf("target retrograde: got %+v, want (0, -1, 0)", got)
	}
}

// TestDirectionUnitTargetPosition — target at +Y offset:
// r_target − r_active points +Y, so BurnTarget = +Y.
func TestDirectionUnitTargetPosition(t *testing.T) {
	rA := orbital.Vec3{X: 7e6}
	vA := orbital.Vec3{Y: 7500}
	rT := orbital.Vec3{X: 7e6, Y: 1000}
	vT := orbital.Vec3{Y: 7500}
	got := DirectionUnitTarget(BurnTarget, rA, vA, rT, vT)
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y-1) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Errorf("target position: got %+v, want (0, 1, 0)", got)
	}
}

// TestDirectionUnitAntiTarget — flip of BurnTarget.
func TestDirectionUnitAntiTarget(t *testing.T) {
	rA := orbital.Vec3{X: 7e6}
	vA := orbital.Vec3{Y: 7500}
	rT := orbital.Vec3{X: 7e6, Y: 1000}
	vT := orbital.Vec3{Y: 7500}
	got := DirectionUnitTarget(BurnAntiTarget, rA, vA, rT, vT)
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y-(-1)) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Errorf("anti-target: got %+v, want (0, -1, 0)", got)
	}
}

// TestDirectionUnitTargetCoVelocityDegrades — identical velocities
// collapse the relative-velocity vector to zero; BurnTargetPrograde
// returns zero so the burn no-ops (the caller's |v_rel| readout
// surfaces "already matched").
func TestDirectionUnitTargetCoVelocityDegrades(t *testing.T) {
	rA := orbital.Vec3{X: 7e6}
	vA := orbital.Vec3{Y: 7500}
	rT := orbital.Vec3{X: 7e6, Y: 1000}
	vT := orbital.Vec3{Y: 7500}
	got := DirectionUnitTarget(BurnTargetPrograde, rA, vA, rT, vT)
	if got != (orbital.Vec3{}) {
		t.Errorf("co-velocity TargetPrograde: got %+v, want zero", got)
	}
}

// TestDirectionUnitTargetFallsThroughToBaseModes — non-target modes
// passed to DirectionUnitTarget delegate to DirectionUnit so callers
// can use the target API uniformly.
func TestDirectionUnitTargetFallsThroughToBaseModes(t *testing.T) {
	rA := orbital.Vec3{X: 7e6}
	vA := orbital.Vec3{Y: 7500}
	rT := orbital.Vec3{X: 7e6, Y: 1000}
	vT := orbital.Vec3{Y: 7600}
	got := DirectionUnitTarget(BurnPrograde, rA, vA, rT, vT)
	want := DirectionUnit(BurnPrograde, rA, vA)
	if got != want {
		t.Errorf("non-target fallthrough: got %+v, want %+v", got, want)
	}
}
