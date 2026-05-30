// Package sim — v0.12 Slice 3 parachute lifecycle tests (ADR 0008).
// Exercises the q-threshold auto-deploy state machine and the second
// (non-engine) route into the Touchdown predicate that a deployed
// chute opens — a vessel without CanSoftLand can still soft-land under
// a canopy, with the nose-alignment gate waived.

package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestParachuteAutoDeploysAboveQMin — an armed chute auto-deploys the
// first tick dynamic pressure reaches ChuteDeployQMin; below the floor
// it stays armed.
func TestParachuteAutoDeploysAboveQMin(t *testing.T) {
	_, c := crashTestPrimaryFor(t)
	radius := c.Primary.RadiusMeters()

	// Below the floor: high above the atmosphere cutoff → q ≈ 0.
	c.ChuteState = spacecraft.ChuteArmed
	c.State.R = orbital.Vec3{X: radius + c.Primary.Atmosphere.CutoffAltitude + 1000}
	c.State.V = orbital.Vec3{Z: 50}
	if maybeDeployParachute(c) {
		t.Errorf("chute deployed above the atmosphere cutoff (q≈0), want stay armed")
	}
	if c.ChuteState != spacecraft.ChuteArmed {
		t.Errorf("state changed off the armed floor: %v", c.ChuteState)
	}

	// Above the floor: at the surface with a fast descent → q ≫ QMin.
	c.State.R = orbital.Vec3{X: radius}
	c.State.V = orbital.Vec3{X: -300}
	if !maybeDeployParachute(c) {
		t.Fatalf("chute did not deploy at the surface with a 300 m/s descent (q ≫ QMin)")
	}
	if c.ChuteState != spacecraft.ChuteDeployed {
		t.Errorf("state = %v, want Deployed", c.ChuteState)
	}
}

// TestParachuteDeployOnlyFromArmed — a stowed chute never auto-deploys
// (the player must arm it first), and a deployed chute is terminal
// (maybeDeployParachute is a no-op once up).
func TestParachuteDeployOnlyFromArmed(t *testing.T) {
	_, c := crashTestPrimaryFor(t)
	radius := c.Primary.RadiusMeters()
	// High-q state so the only thing gating deploy is the ChuteState.
	c.State.R = orbital.Vec3{X: radius}
	c.State.V = orbital.Vec3{X: -300}

	c.ChuteState = spacecraft.ChuteStowed
	if maybeDeployParachute(c) || c.ChuteState != spacecraft.ChuteStowed {
		t.Errorf("stowed chute auto-deployed without arming, state=%v", c.ChuteState)
	}
	c.ChuteState = spacecraft.ChuteDeployed
	if maybeDeployParachute(c) {
		t.Errorf("deployed chute reported a fresh transition (should be terminal no-op)")
	}
}

// coRotate adds the surface co-rotation velocity (ω×r, the same axis
// the chute's air-relative frame uses) to the craft's inertial velocity,
// so its air-relative velocity v_rel = V − ω×r equals whatever
// setImpactState set. Models a vessel descending *with* the air (the
// realistic chute case) rather than at rest in the inertial frame.
func coRotate(c *spacecraft.Spacecraft) {
	c.State.V = c.State.V.Add(physics.AtmosphereOmega(c.Primary).Cross(c.State.R))
}

// TestChuteRouteSoftLandsDespiteCoRotation — the marquee case + the
// Slice 3 verification regression guard: a capsule with NO CanSoftLand
// but a DEPLOYED chute, co-rotating with the ground (so its INERTIAL
// |V| is hundreds of m/s — the Earth-equator co-rotation) yet descending
// at only 3 m/s air-relative with the nose NOT aligned to local-up,
// still Touches Down (Landed). The chute route measures air-relative
// v_rel (3 m/s < V_CRIT) and waives NOSE_TOL — testing inertial |V|
// here would crash the splashdown (ADR 0008 §4, amended).
func TestChuteRouteSoftLandsDespiteCoRotation(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = false
	noseSideways := orbital.Vec3{Y: 1}
	setImpactState(c, 3, noseSideways) // 3 m/s air-relative descent
	coRotate(c)                        // huge inertial |V| from surface co-rotation
	c.ChuteState = spacecraft.ChuteDeployed
	// Guard the test's premise: inertial |V| must be well above V_CRIT,
	// else the v_rel-vs-inertial distinction isn't actually exercised.
	if c.State.V.Norm() <= CrashVCritMps {
		t.Fatalf("setup: inertial |V|=%.1f should exceed V_CRIT to exercise the co-rotation case", c.State.V.Norm())
	}
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Landed {
		t.Errorf("deployed chute + v_rel=3 m/s + !CanSoftLand: Landed=false, want true (inertial |V| must not crash it)")
	}
	if c.Crashed {
		t.Errorf("chute-route touchdown: Crashed=true, want false")
	}
}

// TestChuteRouteCrashesAboveVCrit — the chute route still enforces the
// velocity ceiling, in the air-relative frame. A deployed chute whose
// air-relative descent exceeds V_CRIT (never had time to slow) Crashes
// even while co-rotating.
func TestChuteRouteCrashesAboveVCrit(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = false
	localUp := orbital.Vec3{X: 1}
	setImpactState(c, 40, localUp) // 40 m/s air-relative > V_CRIT
	coRotate(c)
	c.ChuteState = spacecraft.ChuteDeployed
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Crashed {
		t.Errorf("deployed chute + v_rel=40 m/s: Crashed=false, want true")
	}
	if c.Landed {
		t.Errorf("deployed chute above V_CRIT: Landed=true, want Crashed-only")
	}
}

// TestArmedChuteStillCrashes — an ARMED (not yet deployed) chute gives
// no soft-land qualification. A capsule that hits with the chute armed
// but never inflated (e.g. airless body, or contact before the q floor)
// Crashes like any non-CanSoftLand vessel.
func TestArmedChuteStillCrashes(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = false
	localUp := orbital.Vec3{X: 1}
	setImpactState(c, 3, localUp)
	c.ChuteState = spacecraft.ChuteArmed
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Crashed {
		t.Errorf("armed (undeployed) chute + !CanSoftLand: Crashed=false, want true")
	}
	if c.Landed {
		t.Errorf("armed (undeployed) chute: Landed=true, want Crashed")
	}
}

// TestParachuteFullDescentEarthSplashdown — the end-to-end Slice 3
// verification: a re-entry capsule armed and dropped into Earth's
// atmosphere co-rotating with the ground deploys its chute, decelerates
// to terminal velocity, and soft-lands (Landed, not Crashed) despite a
// large inertial velocity from surface co-rotation. This is the arc the
// Slice 3 verification exercised by hand; pinned here so a regression in
// the auto-deploy gate, the BC bump, or the predicate frame is caught.
func TestParachuteFullDescentEarthSplashdown(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.ActiveCraft().Primary
	if earth.Atmosphere == nil {
		t.Fatal("default Earth primary should have an atmosphere")
	}
	R := earth.RadiusMeters()
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutCapsuleID)
	c.Primary = earth
	r0 := orbital.Vec3{X: R + 5000} // 5 km up, equator
	// Co-rotating horizontally + 200 m/s straight down (a fast entry that
	// crosses the q deploy floor immediately).
	c.State = physics.StateVector{
		R: r0,
		V: physics.AtmosphereOmega(earth).Cross(r0).Add(orbital.Vec3{X: -200}),
		M: c.TotalMass(),
	}
	c.ChuteState = spacecraft.ChuteArmed
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0

	var i int
	for i = 0; i < 300000; i++ {
		w.integrateOneCraft(c, w.Clock.BaseStep)
		if c.Landed || c.Crashed {
			break
		}
	}
	if c.ChuteState != spacecraft.ChuteDeployed {
		t.Errorf("chute should have auto-deployed during the descent, got %v", c.ChuteState)
	}
	if c.Crashed {
		t.Fatalf("capsule Crashed on Earth splashdown after %d ticks; want Landed (chute descent should soft-land)", i)
	}
	if !c.Landed {
		t.Fatalf("capsule neither Landed nor Crashed after %d ticks (did it reach the surface?)", i)
	}
}

// TestParachuteCapabilitySurvivesUndock — the per-stage parachute
// capability must ride through a dock/undock cycle. A CSM/Capsule that
// docks with another craft and later undocks (the Apollo arc) would
// otherwise lose HasParachute (DockedComponent didn't record it) and
// crash on its Earth splashdown. v0.12 Slice 3 (ADR 0008).
func TestParachuteCapabilitySurvivesUndock(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")

	a := w.Crafts[0]
	a.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	a.State.V = orbital.Vec3{Y: 7600}

	b := spacecraft.NewFromLoadout(spacecraft.LoadoutCapsuleID)
	b.Name = "Capsule"
	b.Primary = *earth
	b.State = physics.StateVector{
		R: a.State.R.Add(orbital.Vec3{X: 10}),
		V: a.State.V,
		M: b.TotalMass(),
	}
	if !b.HasParachute {
		t.Fatal("setup: capsule should have HasParachute before docking")
	}
	w.Crafts = append(w.Crafts, b)

	if _, _, ok := w.checkDocking(); !ok {
		t.Fatalf("expected dock to fire")
	}
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a composite")
	}
	// Find the restored Capsule and confirm it kept its chute capability.
	var capsule *spacecraft.Spacecraft
	for _, c := range w.Crafts {
		if c.Name == "Capsule" {
			capsule = c
		}
	}
	if capsule == nil {
		t.Fatal("Capsule not restored after undock")
	}
	if !capsule.HasParachute {
		t.Errorf("restored Capsule lost HasParachute across dock/undock")
	}
	if len(capsule.Stages) == 0 || !capsule.Stages[0].HasParachute {
		t.Errorf("restored Capsule's Stages[0] lost HasParachute (SyncFields mirror would re-clear it)")
	}
}

// TestWarpClampedDuringChuteDescent — a live (armed or deployed) chute
// inside the atmosphere caps warp to 10×, like an active burn. Without
// it a single high-warp tick (one giant sub-step) could leap past the q
// deploy window (auto-deploy missed → crash undeployed) or inflate the
// canopy in one integration step (the instant-inflation overshoot ADR
// 0008 banked). The clamp guarantees fine sub-steps so the per-tick /
// per-sub-step deploy check resolves the crossing. v0.12 Slice 3.
func TestWarpClampedDuringChuteDescent(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1 // max warp selected
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	// Circular orbit at 100 km — inside Earth's 150 km atmosphere cutoff,
	// but a sane orbit so the period clamp stays generous and the chute
	// cap is what's actually being measured.
	r := c.Primary.RadiusMeters() + 100_000
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	c.HasParachute = true

	c.ChuteState = spacecraft.ChuteArmed
	if eff := w.EffectiveWarp(); eff != 10 {
		t.Errorf("armed chute in atmosphere should cap warp to 10×, got %.0f", eff)
	}
	c.ChuteState = spacecraft.ChuteDeployed
	if eff := w.EffectiveWarp(); eff != 10 {
		t.Errorf("deployed chute in atmosphere should cap warp to 10×, got %.0f", eff)
	}
	// Stowed → no chute clamp; warp returns to the (generous) period cap.
	c.ChuteState = spacecraft.ChuteStowed
	if eff := w.EffectiveWarp(); eff <= 10 {
		t.Errorf("stowed chute should not force the 10× cap, got %.0f", eff)
	}
	// Live chute but above the atmosphere cutoff → no chute clamp.
	c.ChuteState = spacecraft.ChuteArmed
	c.State.R = orbital.Vec3{X: c.Primary.RadiusMeters() + c.Primary.Atmosphere.CutoffAltitude + 1000}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / c.State.R.Norm())}
	if eff := w.EffectiveWarp(); eff <= 10 {
		t.Errorf("armed chute above the atmosphere cutoff should not clamp, got %.0f", eff)
	}
}
