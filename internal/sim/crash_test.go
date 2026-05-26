// Package sim — v0.11.4+ Crashed / Soft-Landed lifecycle tests
// (ADR 0004). Exercises the surface-arrival predicate at the
// physics.ClampToSurface call site: CanSoftLand catalog gate +
// V_CRIT velocity ceiling + NOSE_TOL alignment together decide
// Landed vs Crashed.

package sim

import (
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
