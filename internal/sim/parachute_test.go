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

// ADR 0069 guard 3: an armed chute does not open above ChuteOpenAltM at any
// speed, opens at exactly ChuteOpenAltM (the equal case: "passing 1.8 km" is
// at-or-below), and opens at once when armed lower, at terminal speed too.
func TestParachuteOpensPassingChuteOpenAlt(t *testing.T) {
	_, c := crashTestPrimaryFor(t)
	radius := c.Primary.RadiusMeters()
	at := func(altM, vDown float64) bool {
		c.ChuteState = spacecraft.ChuteArmed
		c.State.R = orbital.Vec3{X: radius + altM}
		c.State.V = orbital.Vec3{X: -vDown}
		return maybeDeployParachute(c)
	}
	for _, v := range []float64{0, 40, 3000} { // 3 km/s is q far above the old 1,000 Pa floor
		if at(ChuteOpenAltM+0.001, v) {
			t.Errorf("opened 1 mm above %v m at %v m/s", ChuteOpenAltM, v)
		}
		if c.ChuteState != spacecraft.ChuteArmed {
			t.Errorf("state = %v above the height, want ARMED", c.ChuteState)
		}
	}
	if !at(ChuteOpenAltM, 40) {
		t.Errorf("did not open at exactly %v m", ChuteOpenAltM)
	}
	if c.ChuteState != spacecraft.ChuteDeployed {
		t.Errorf("state = %v after opening, want DEPLOYED", c.ChuteState)
	}
	// armed low: at once, at the speed it would fall at without air (Earth, 40.2 m/s)
	if !at(1000, 40.2) {
		t.Errorf("armed at 1 km at terminal speed did not open")
	}
	// one chute, one height: a body with no air never opens it
	c.Primary.Atmosphere = nil
	if at(1000, 40.2) {
		t.Errorf("opened on a body with no air")
	}
}

// TestParachuteDeployOnlyFromArmed — a stowed chute never auto-deploys
// (the player must arm it first), and a deployed or cut chute is terminal
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
	c.ChuteState = spacecraft.ChuteCut
	if maybeDeployParachute(c) || c.ChuteState != spacecraft.ChuteCut {
		t.Errorf("a cut chute opened again, state=%v", c.ChuteState)
	}
}

// ADR 0069 decision 7: after the cut the vessel lands on the engine route, and
// the nose rule applies again. Same slow contact, nose sideways: the open chute
// lands it (nose waived), the cut chute on a CanSoftLand vessel crashes on the
// nose, level it lands on the engine, and without CanSoftLand it crashes.
func TestCutChuteLandsOnEngineRouteWithNoseRule(t *testing.T) {
	for _, tc := range []struct {
		name     string
		state    spacecraft.ChuteState
		soft     bool
		nose     orbital.Vec3
		wantLand bool
	}{
		{"open chute, nose sideways", spacecraft.ChuteDeployed, true, orbital.Vec3{Y: 1}, true},
		{"cut, nose sideways", spacecraft.ChuteCut, true, orbital.Vec3{Y: 1}, false},
		{"cut, nose up", spacecraft.ChuteCut, true, orbital.Vec3{X: 1}, true},
		{"cut, no landing engine", spacecraft.ChuteCut, false, orbital.Vec3{X: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, c := crashTestPrimaryFor(t)
			c.CanSoftLand = tc.soft
			setImpactState(c, 3, tc.nose)
			c.ChuteState = tc.state
			w.integrateOneCraft(c, w.Clock.BaseStep)
			if c.Landed != tc.wantLand || c.Crashed == tc.wantLand {
				t.Errorf("Landed=%v Crashed=%v, want landed=%v", c.Landed, c.Crashed, tc.wantLand)
			}
		})
	}
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
	setImpactState(c, 3, noseSideways) // 3 m/s over the ground; inertial |V| carries the surface's spin
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
// but never inflated (an airless body, where it never opens) Crashes like
// any non-CanSoftLand vessel. On a body with air an armed chute opens at
// once below ChuteOpenAltM, so contact there is never undeployed.
func TestArmedChuteStillCrashes(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = false
	c.Primary.Atmosphere = nil
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

	w.DockCrafts(0, 1) // fixture dock; the capture is covered in docking_port_test.go
	if len(w.Crafts) != 1 {
		t.Fatalf("fixture dock failed")
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

// TestChuteStateDoesNotCapWarp: a live chute no longer holds warp to 10x (it
// did until the 1.8 km height rule, StepDragSafe and the bisected opening made
// a coarse tick safe). The same orbit reads the same warp stowed, armed and
// deployed; TestDragonFlightRealTimeAtHeldWarp pins the descent itself.
func TestChuteStateDoesNotCapWarp(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1
	c := w.ActiveCraft()
	r := c.Primary.RadiusMeters() + 100_000 // inside Earth's 150 km cutoff
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(c.Primary.GravitationalParameter() / r)}
	c.HasParachute = true
	c.ChuteState = spacecraft.ChuteStowed
	stowed := w.EffectiveWarp()
	for _, st := range []spacecraft.ChuteState{spacecraft.ChuteArmed, spacecraft.ChuteDeployed} {
		c.ChuteState = st
		if eff := w.EffectiveWarp(); eff != stowed || eff <= 10 {
			t.Errorf("%v chute reads %.0fx, stowed reads %.0fx (want equal, above 10x)", st, eff, stowed)
		}
	}
}

// TestChuteOpeningFastInThickAirDoesNotOvershoot — #935 / ADR 0069 guard 4:
// a Token Pod on Kern with its chute armed at 5 km opens at ~1,375 m/s. One
// step's drag then exceeded the airspeed (k·v·dt = 354), reversed the
// vessel and threw it off at 7e8 m/s, neither landed nor crashed. The pod
// must only slow down and end on the ground, and the forecast's step must
// agree.
func TestChuteOpeningFastInThickAirDoesNotOvershoot(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	cycleToSystem(t, w, "Lumen")
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Token-Pod", ParentBodyID: "kern", AltitudeM: 100e3})
	if err != nil {
		t.Fatal(err)
	}
	for i, x := range w.Crafts {
		if x == c {
			w.SetActiveCraftIdx(i)
		}
	}
	rHat := orbital.Vec3{X: 1}
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + 5000)
	c.State.V = physics.AtmosphereOmega(c.Primary).Cross(c.State.R).Add(rHat.Scale(-1375))
	c.State.M = c.TotalMass()
	c.ArmParachute()
	start := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary).Norm()
	w.Clock.WarpIdx = 0
	for i := 0; i < 20000 && !c.Landed && !c.Crashed; i++ {
		w.Tick()
		if v := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary).Norm(); v > start*1.01 {
			t.Fatalf("tick %d: air-relative speed %.0f m/s exceeds the entry speed %.0f (drag step overshot)", i, v, start)
		}
	}
	if !c.Landed && !c.Crashed {
		t.Fatalf("pod is neither landed nor crashed: speed %.3g m/s", c.State.V.Norm())
	}
}

// TestPredictStepDragNeverReversesAirflow — the forecast's propagator takes
// the same drag-safe step: a chute-sized BC at 1,375 m/s in Kern air leaves
// the air-relative velocity pointing the way it did.
func TestPredictStepDragNeverReversesAirflow(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	cycleToSystem(t, w, "Lumen")
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Token-Pod", ParentBodyID: "kern", AltitudeM: 100e3})
	if err != nil {
		t.Fatal(err)
	}
	rHat := orbital.Vec3{X: 1}
	s := physics.StateVector{R: rHat.Scale(c.Primary.RadiusMeters() + 5000), M: 1}
	s.V = physics.AtmosphereOmega(c.Primary).Cross(s.R).Add(rHat.Scale(-1375))
	next := predictStep(s, c.Primary.GravitationalParameter(), atmosphereMaxStepSec, c.Primary, spacecraft.ChuteDeployedBC)
	rel := physics.AirRelativeVelocity(next.R, next.V, c.Primary)
	if rel.Dot(rHat) > 0 || rel.Norm() > 1375 {
		t.Fatalf("forecast step reversed or sped the airflow: v_rel %+v", rel)
	}
}

// The canopy opens where the path crosses ChuteOpenAltM, not at the end of the
// step that ends below it: a 0.5 s step at 950 m/s opened up to 475 m late, and
// the touchdown moved with the step (ADR 0069 slice 9). A straight 100 m/s fall
// from 1860 m, one 1 s step: it crosses at 0.6 s. Equal cases: starting at the
// height is not a crossing (it opens at once, maybeDeployParachute), ending at
// it is.
func TestChuteOpeningWithinFindsTheCrossing(t *testing.T) {
	radius := 6371e3
	fall := func(s physics.StateVector, h float64) physics.StateVector {
		s.R.X += s.V.X * h
		return s
	}
	at := func(alt float64) physics.StateVector {
		return physics.StateVector{R: orbital.Vec3{X: radius + alt}, V: orbital.Vec3{X: -100}}
	}
	tau, mid, ok := chuteOpeningWithin(at(ChuteOpenAltM+60), 1, radius, fall)
	if !ok || math.Abs(tau-0.6) > 1e-6 || mid.R.Norm()-radius < ChuteOpenAltM || mid.R.Norm()-radius > ChuteOpenAltM+1e-4 {
		t.Errorf("crossing at %.6f s, ok=%v, alt %.5f, want 0.6 s, just above %v", tau, ok, mid.R.Norm()-radius, ChuteOpenAltM)
	}
	if _, _, ok := chuteOpeningWithin(at(ChuteOpenAltM), 1, radius, fall); ok {
		t.Error("a step that starts at the height crossed it")
	}
	if tau, _, ok := chuteOpeningWithin(at(ChuteOpenAltM+100), 1, radius, fall); !ok || math.Abs(tau-1) > 1e-6 {
		t.Errorf("a step that ends exactly at the height: tau %.6f ok=%v, want 1 s", tau, ok)
	}
	if _, _, ok := chuteOpeningWithin(at(ChuteOpenAltM+100.001), 1, radius, fall); ok {
		t.Error("a step that ends 1 mm above the height crossed it")
	}
}
