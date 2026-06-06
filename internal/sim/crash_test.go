// Package sim — v0.11.4+ Crashed / Soft-Landed lifecycle tests
// (ADR 0004). Exercises the surface-arrival predicate at the
// physics.ClampToSurface call site: CanSoftLand catalog gate +
// V_CRIT velocity ceiling + NOSE_TOL alignment together decide
// Landed vs Crashed.

package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// crashTestPrimaryFor returns Earth from the default system catalog.
// Tests parameterise impact state on top of a real body so the
// inverse-projection sub-craft-point math has a meaningful surface.
func crashTestPrimaryFor(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	return w, c
}

// localUpAtImpact and a downward-velocity helper let each test build
// a controlled impact state with a single line.
func setImpactState(c *spacecraft.Spacecraft, vDownMps float64, nose orbital.Vec3) {
	radius := c.Primary.RadiusMeters()
	// Just below the surface to trigger ClampToSurface (the predicate
	// site reads V_impact pre-clamp, R pre-clamp). Sub-radius by a
	// few metres; clamped state pops back to radius.
	r := orbital.Vec3{X: radius - 5, Y: 0, Z: 0}
	// Velocity directed radially inward (toward planet center) at the
	// specified magnitude.
	v := orbital.Vec3{X: -vDownMps, Y: 0, Z: 0}
	c.State.R = r
	c.State.V = v
	c.State.M = c.TotalMass()
	c.CurrentAttitudeDir = nose
	c.Landed = false
	c.Crashed = false
}

// TestSaturnVCapsuleCrashesOnSurfaceImpact — a vessel without
// CanSoftLand is Crashed on any surface contact, regardless of how
// gently it arrived. The lifecycle is gated on the catalog flag —
// a Saturn-V capsule that grazes the surface at 5 m/s is still
// a destructive impact (no landing gear, no controlled descent
// programme). ADR 0004 alternative (a) rejected for this reason.
func TestSaturnVCapsuleCrashesOnSurfaceImpact(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = false
	// Gentle impact: 5 m/s (well under V_CRIT=10), nose straight up.
	localUp := orbital.Vec3{X: 1}
	setImpactState(c, 5, localUp)
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if c.Landed {
		t.Errorf("Saturn-V capsule with !CanSoftLand and gentle impact: Landed=true, want Crashed=true")
	}
	if !c.Crashed {
		t.Errorf("Saturn-V capsule with !CanSoftLand: Crashed=false, want true")
	}
}

// TestFalcon9SoftLandsBelowVCrit — a CanSoftLand vessel arriving
// below V_CRIT with nose aligned to local-up qualifies for the
// soft-touchdown branch. Sets Landed=true; does NOT set Crashed.
// LandedLatDeg/LonDeg are populated from the inverse projection of
// the impact point.
func TestFalcon9SoftLandsBelowVCrit(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = true
	// 2 m/s touchdown speed (real F9 lands at ~2 m/s), nose up.
	localUp := orbital.Vec3{X: 1}
	setImpactState(c, 2, localUp)
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Landed {
		t.Errorf("Falcon-9 CanSoftLand + V=2 m/s + nose up: Landed=false, want true")
	}
	if c.Crashed {
		t.Errorf("Falcon-9 soft touchdown: Crashed=true, want false")
	}
}

// TestFalcon9CrashesAboveVCrit — even a CanSoftLand vessel crashes
// if it arrives above V_CRIT. ADR 0004 alternative (d) rejected for
// this reason: vessel-type gating alone allows a "lander" hitting
// at 500 m/s to be classified as a landing. V_CRIT is the kinematic
// floor that prevents that.
func TestFalcon9CrashesAboveVCrit(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = true
	// 50 m/s impact — well above V_CRIT=10 m/s.
	localUp := orbital.Vec3{X: 1}
	setImpactState(c, 50, localUp)
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Crashed {
		t.Errorf("Falcon-9 CanSoftLand + V=50 m/s: Crashed=false, want true")
	}
	if c.Landed {
		t.Errorf("Falcon-9 50 m/s impact: Landed=true, want Crashed-only")
	}
}

// TestFalcon9CrashesSideways — even a CanSoftLand vessel under
// V_CRIT crashes if the nose isn't aligned with local-up. A Falcon
// 9 tipped past 45° from vertical is going to come down on its side
// — no soft touchdown. ADR 0004 alternative (b) covered this gate.
func TestFalcon9CrashesSideways(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = true
	// 2 m/s impact (under V_CRIT) but nose horizontal (perpendicular
	// to local-up). cosNose = 0; well below NOSE_TOL=0.7.
	noseSideways := orbital.Vec3{Y: 1}
	setImpactState(c, 2, noseSideways)
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Crashed {
		t.Errorf("Falcon-9 CanSoftLand + V=2 m/s + nose sideways: Crashed=false, want true")
	}
	if c.Landed {
		t.Errorf("Falcon-9 sideways impact: Landed=true, want Crashed-only")
	}
}

// TestFalcon9SoftLandsAtExactNoseTol — a CanSoftLand vessel touching
// down below V_CRIT with the nose at *exactly* the documented minimum
// alignment (cosNose == NOSE_TOL == 0.7, ~45° off vertical) must land,
// not crash. The route comments document "land when nose > NOSE_TOL" and
// the velocity gates use >= (reject at-or-above), so the nose gate should
// reject strictly below the tolerance. The old `<= CrashNoseTol` crashed
// the boundary case it should accept. Exercises the pure
// classifySurfaceArrival predicate directly: integrateOneCraft slews
// CurrentAttitudeDir at the tick top, which would perturb an
// exactly-on-the-boundary nose and mask the gate under test. (#90)
func TestFalcon9SoftLandsAtExactNoseTol(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = true
	// Use a unit preClampR so the predicate's localUp = R/|R| is exactly
	// {1,0,0}; then nose.Dot(localUp) is just the normalized nose.X.
	// classifySurfaceArrival is pure and frame-agnostic, so the small
	// radius is irrelevant to the gate.
	preClampR := orbital.Vec3{X: 1}
	localUp := preClampR.Scale(1 / preClampR.Norm())

	// `<` vs `<=` differ only at bit-exact equality, and the predicate
	// re-normalizes the nose — so whether any given vector lands exactly
	// on CrashNoseTol depends on the platform's float rounding (arm64 FMA
	// vs amd64). Rather than hardcode a magic component that's only exact
	// on one arch, search at runtime for a nose whose *post-normalization*
	// dot is bit-identical to CrashNoseTol on whatever machine runs this.
	noseAtTol := exactNoseTolDir(t, localUp)
	c.CurrentAttitudeDir = noseAtTol
	preClampV := orbital.Vec3{X: -2} // 2 m/s inward, below V_CRIT
	outcome, _, _ := classifySurfaceArrival(c, preClampR, preClampV, w.Clock.SimTime)
	if outcome != outcomeLanded {
		t.Errorf("CanSoftLand + V=2 m/s + nose at exact NOSE_TOL: outcome=%v, want outcomeLanded", outcome)
	}
}

// exactNoseTolDir returns a nose direction whose normalized dot with
// localUp is bit-exactly CrashNoseTol — i.e. a nose sitting precisely on
// the soft-land boundary the gate compares against. Found by search so
// the construction is portable across float-rounding differences
// (arm64 FMA vs amd64); fails the test if no such vector exists in the
// neighbourhood (which would itself be a signal worth investigating).
func exactNoseTolDir(t *testing.T, localUp orbital.Vec3) orbital.Vec3 {
	t.Helper()
	const x = CrashNoseTol
	y0 := math.Sqrt(1 - x*x) // nominal complement; dot ≈ tol near here
	for i := -200000; i <= 200000; i++ {
		y := y0 + float64(i)*1e-16
		n := orbital.Vec3{X: x, Y: y}
		if n.Scale(1 / n.Norm()).Dot(localUp) == CrashNoseTol {
			return n
		}
	}
	t.Fatalf("could not construct a nose with normalized dot == CrashNoseTol near y=%.17g", y0)
	return orbital.Vec3{}
}

// TestCrashedVesselSkipsIntegration — once a vessel is Crashed,
// subsequent integrateOneCraft ticks are a no-op (no gravity, no
// drag, no thrust, no slew). The wreckage sits at its clamped
// position until end-flight removal. Regression guard for the
// "Crashed integrator bypass" branch in integrateOneCraft.
func TestCrashedVesselSkipsIntegration(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = false
	localUp := orbital.Vec3{X: 1}
	setImpactState(c, 50, localUp)
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Crashed {
		t.Fatal("setup: expected Crashed after 50 m/s impact on non-CanSoftLand vessel")
	}
	snapshotR := c.State.R
	snapshotV := c.State.V
	// Tick again — Crashed branch should leave state untouched.
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if c.State.R != snapshotR {
		t.Errorf("Crashed vessel position drifted: snapshot=%+v, post-tick=%+v",
			snapshotR, c.State.R)
	}
	if c.State.V != snapshotV {
		t.Errorf("Crashed vessel velocity drifted: snapshot=%+v, post-tick=%+v",
			snapshotV, c.State.V)
	}
}
