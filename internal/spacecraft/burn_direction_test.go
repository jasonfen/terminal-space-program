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
		MeanRadius:      6371,                                    // km
		Mass:            bodies.Mass{Value: 5.972, Exponent: 24}, // kg
		SideralRotation: 23.9345,                                 // hours (sidereal day)
	}
}

// TestApplyPitchTrimZeroIsNoop — zero trim returns dir unchanged.
func TestApplyPitchTrimZeroIsNoop(t *testing.T) {
	r := orbital.Vec3{X: 6.371e6}
	dir := orbital.Vec3{X: 1}
	got := ApplyPitchTrim(dir, r, orbital.Vec3{Z: 1}, 0)
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
	got := ApplyPitchTrim(radialOut, r, orbital.Vec3{Z: 1}, math.Pi/2)
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y-1) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Errorf("90° east trim of radial+: got %+v, want (0, 1, 0)", got)
	}
}

// TestApplyPitchTrimSmallEastBoost — at the equator, trimming +5°
// east gives a vector mostly +X with a small +Y component.
func TestApplyPitchTrimSmallEastBoost(t *testing.T) {
	r := orbital.Vec3{X: 6.371e6}
	radialOut := orbital.Vec3{X: 1}
	got := ApplyPitchTrim(radialOut, r, orbital.Vec3{Z: 1}, 5*math.Pi/180)
	wantX := math.Cos(5 * math.Pi / 180)
	wantY := math.Sin(5 * math.Pi / 180)
	if math.Abs(got.X-wantX) > 1e-9 {
		t.Errorf("5° east trim: X = %.6f, want %.6f", got.X, wantX)
	}
	if math.Abs(got.Y-wantY) > 1e-9 {
		t.Errorf("5° east trim: Y = %.6f, want %.6f", got.Y, wantY)
	}
}

// TestApplyHeadingTrimZeroIsNoop — zero offset (commanded heading ==
// due east) returns dir unchanged.
func TestApplyHeadingTrimZeroIsNoop(t *testing.T) {
	r := orbital.Vec3{X: 6.371e6}
	dir := orbital.Vec3{Y: 1}
	got := ApplyHeadingTrim(dir, r, orbital.Vec3{Z: 1}, 0)
	if got != dir {
		t.Errorf("zero heading offset altered dir: got %+v, want %+v", got, dir)
	}
}

// TestApplyHeadingTrimPoleIsNoop pins the pole no-op ApplyHeadingTrim
// inherits from the shared localHorizonFrame helper: at the pole (r
// parallel to spinAxis) east is undefined, so the rotation silently
// no-ops rather than dividing by zero, exactly like ApplyPitchTrim's
// own pole guard.
func TestApplyHeadingTrimPoleIsNoop(t *testing.T) {
	r := orbital.Vec3{Z: 6.371e6} // north pole, parallel to spinAxis.
	dir := orbital.Vec3{X: 1}
	got := ApplyHeadingTrim(dir, r, orbital.Vec3{Z: 1}, math.Pi/2)
	if got != dir {
		t.Errorf("pole heading trim altered dir: got %+v, want unchanged %+v", got, dir)
	}
}

// TestApplyHeadingTrimNorthOffsetTiltsToNorth and its south mirror below
// pin ApplyHeadingTrim's actual rotation direction, the same way
// TestApplyPitchTrimEastTiltsRadialEast pins ApplyPitchTrim's. The
// inclination identity alone cannot: h_z = r×dir only picks up dir's
// EASTWARD component (sin β), so cos i = sin β · cos φ is provably
// insensitive to whether the north/south component rotates the right
// way — heading 000° and 180° both land on i = 90° by that formula
// regardless of which one a sign error swapped. At the equator on +X,
// this frame's north resolves to +Z (matching
// TestApplyPitchTrimEastTiltsRadialEast's frame); commanding due north
// (offset -90° from east) must rotate the natural east direction (+Y)
// onto exactly +Z, not -Z.
func TestApplyHeadingTrimNorthOffsetTiltsToNorth(t *testing.T) {
	r := orbital.Vec3{X: 6.371e6}
	east := orbital.Vec3{Y: 1}
	got := ApplyHeadingTrim(east, r, orbital.Vec3{Z: 1}, -math.Pi/2) // commanded heading 000° (due north).
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y) > 1e-9 || math.Abs(got.Z-1) > 1e-9 {
		t.Errorf("heading 000° (offset -90° from due east): got %+v, want (0, 0, 1) (local north)", got)
	}
}

// TestApplyHeadingTrimSouthOffsetTiltsToSouth is the mirror: due south
// (offset +90°) must rotate east onto -Z, not +Z.
func TestApplyHeadingTrimSouthOffsetTiltsToSouth(t *testing.T) {
	r := orbital.Vec3{X: 6.371e6}
	east := orbital.Vec3{Y: 1}
	got := ApplyHeadingTrim(east, r, orbital.Vec3{Z: 1}, math.Pi/2) // commanded heading 180° (due south).
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y) > 1e-9 || math.Abs(got.Z+1) > 1e-9 {
		t.Errorf("heading 180° (offset +90° from due east): got %+v, want (0, 0, -1) (local south)", got)
	}
}

// TestApplyHeadingTrimInclinationIdentity pins ApplyHeadingTrim against
// the closed-form launch-azimuth identity cos(i) = sin(beta)*cos(phi):
// the inclination an ascent launched at compass bearing beta (000° =
// north, 090° = east, 180° = south, 270° = west) from geographic
// latitude phi reaches, given it harvests the planet's due-east
// surface-rotation velocity rotated onto the commanded heading. This
// is the identity ADR 0049 decision 8 and CONTEXT.md's Inclination
// Floor entry both cite; the 28.6° pad and the four headings (three
// plus the 270°/retrograde case) are the ADR's own worked examples.
//
// Expected inclinations are computed from the formula alone (sin/cos
// of beta and phi), independent of ApplyHeadingTrim. "got" is derived
// the same general way internal/orbital.ElementsFromState computes
// inclination from a state vector — cross r with the rotated
// direction and read the specific angular momentum's angle to the
// spin axis, h_z/|h| — rather than re-deriving sin(beta)*cos(phi) from
// the implementation's own components, which would prove nothing.
func TestApplyHeadingTrimInclinationIdentity(t *testing.T) {
	const padLatDeg = 28.6 // ADR 0049 decision 9/10's worked "28.6° pad" example.
	phi := padLatDeg * math.Pi / 180
	const radius = 6.371e6
	// On the pad's meridian (longitude 0, spinAxis = Z): up = r̂ =
	// (cos φ, 0, sin φ), so east = spinAxis × up resolves to exactly
	// (0, 1, 0) at any latitude on this meridian — the natural
	// due-east surface-rotation direction ApplyHeadingTrim rotates
	// away from.
	r := orbital.Vec3{X: math.Cos(phi) * radius, Z: math.Sin(phi) * radius}
	spinAxis := orbital.Vec3{Z: 1}
	east := orbital.Vec3{Y: 1}

	cases := []struct {
		name       string
		headingDeg float64 // commanded compass bearing (β).
	}{
		{"due east: the inclination floor", 90},
		{"due north: polar", 0},
		{"due south: polar", 180},
		{"due west (retrograde of due east)", 270},
	}
	const tolDeg = 1e-9
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			beta := c.headingDeg * math.Pi / 180
			wantIDeg := math.Acos(math.Sin(beta)*math.Cos(phi)) * 180 / math.Pi

			offset := beta - math.Pi/2 // ApplyHeadingTrim's offset-from-due-east parameter.
			dir := ApplyHeadingTrim(east, r, spinAxis, offset)

			h := r.Cross(dir)
			hNorm := h.Norm()
			if hNorm == 0 {
				t.Fatalf("degenerate angular momentum for heading %.0f°: dir=%+v", c.headingDeg, dir)
			}
			gotIDeg := math.Acos(h.Z/hNorm) * 180 / math.Pi

			if math.Abs(gotIDeg-wantIDeg) > tolDeg {
				t.Errorf("heading %.0f° from a %.1f° pad: inclination = %.10f°, want %.10f° (cos i = sin(%.0f°)·cos(%.1f°), tolerance %.0e°)",
					c.headingDeg, padLatDeg, gotIDeg, wantIDeg, c.headingDeg, padLatDeg, tolDeg)
			}
		})
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
	want := ApplyPitchTrim(orbital.Vec3{X: 1}, r, orbital.Vec3{Z: 1}, 5*math.Pi/180)
	if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 || math.Abs(got.Z-want.Z) > 1e-9 {
		t.Errorf("pitch trim on radial+: got %+v, want %+v", got, want)
	}
}

// TestBurnDirectionAppliesHeadingBeforePitch pins the ADR 0049 decision
// 8/9 ordering at the BurnDirectionWithTarget call site: HeadingTrim
// rotates the natural direction about local up BEFORE PitchTrim rotates
// about local north. The two orders give numerically different results
// once the natural direction already has both a radial and a horizontal
// component (an ascending BurnSurfacePrograde burn does, once the craft
// is climbing and moving east) and both trims are nonzero, so this test
// would catch the two calls being swapped in BurnDirectionWithTarget —
// exactly the silent regression the ADR warns a later refactor could
// introduce.
func TestBurnDirectionAppliesHeadingBeforePitch(t *testing.T) {
	earth := testEarth()
	r := orbital.Vec3{X: earth.RadiusMeters()}
	v := orbital.Vec3{X: 100, Y: 8000} // climbing (radial) and moving east (surface-relative).
	s := &Spacecraft{Primary: earth}
	s.State.R = r
	s.State.V = v
	s.HeadingTrim = math.Pi // 180° offset from due east: commanded bearing 270° (west).
	s.PitchTrim = 10 * math.Pi / 180

	spinAxis := orbital.Vec3{Z: 1}
	omega := physics.AtmosphereOmega(earth)
	vSurf := v.Sub(omega.Cross(r))
	natural := vSurf.Scale(1 / vSurf.Norm())

	got := s.BurnDirection(BurnSurfacePrograde)

	headingFirst := ApplyPitchTrim(ApplyHeadingTrim(natural, r, spinAxis, s.HeadingTrim), r, spinAxis, s.PitchTrim)
	pitchFirst := ApplyHeadingTrim(ApplyPitchTrim(natural, r, spinAxis, s.PitchTrim), r, spinAxis, s.HeadingTrim)

	if math.Abs(got.X-headingFirst.X) > 1e-9 || math.Abs(got.Y-headingFirst.Y) > 1e-9 || math.Abs(got.Z-headingFirst.Z) > 1e-9 {
		t.Errorf("BurnDirection = %+v, want heading-before-pitch composition %+v", got, headingFirst)
	}
	// Sanity: confirm this input actually distinguishes the two orders,
	// so the assertion above is a real guard and not a coincidence.
	if math.Abs(headingFirst.X-pitchFirst.X) < 1e-6 && math.Abs(headingFirst.Y-pitchFirst.Y) < 1e-6 && math.Abs(headingFirst.Z-pitchFirst.Z) < 1e-6 {
		t.Fatalf("test setup doesn't distinguish order: heading-first %+v ~= pitch-first %+v", headingFirst, pitchFirst)
	}
}

// TestDirectionUnitTargetPrograde — target ahead in +Y, faster:
// KSP convention has target-prograde = unit(v_active − v_target),
// which here is −Y (active is the slower one).
func TestDirectionUnitTargetPrograde(t *testing.T) {
	rA := orbital.Vec3{X: 7e6}
	vA := orbital.Vec3{Y: 7500}
	rT := orbital.Vec3{X: 7e6, Y: 1000}
	vT := orbital.Vec3{Y: 7600}
	got := DirectionUnitTarget(BurnTargetPrograde, rA, vA, rT, vT)
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y-(-1)) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Errorf("target prograde: got %+v, want (0, -1, 0)", got)
	}
}

// TestDirectionUnitTargetRetrograde — flip of TargetPrograde: the
// closing / null-v_rel axis, unit(v_target − v_active) = +Y here.
func TestDirectionUnitTargetRetrograde(t *testing.T) {
	rA := orbital.Vec3{X: 7e6}
	vA := orbital.Vec3{Y: 7500}
	rT := orbital.Vec3{X: 7e6, Y: 1000}
	vT := orbital.Vec3{Y: 7600}
	got := DirectionUnitTarget(BurnTargetRetrograde, rA, vA, rT, vT)
	if math.Abs(got.X) > 1e-9 || math.Abs(got.Y-1) > 1e-9 || math.Abs(got.Z) > 1e-9 {
		t.Errorf("target retrograde: got %+v, want (0, 1, 0)", got)
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
