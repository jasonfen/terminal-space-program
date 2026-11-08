package sim

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// flyLanding plants N on the deorbit world, engages Site+ and ticks until the
// vessel lands or crashes. It returns the true distance from the real site and
// the speed over the ground / inertial speed on the last tick before contact.
func flyLanding(t *testing.T, w *World, c *spacecraft.Spacecraft, maxTicks int, each func(i int)) (distM, lastSpeed float64, landed bool) {
	t.Helper()
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxTicks; i++ {
		lastSpeed = c.State.V.Norm()
		w.Tick()
		if each != nil {
			each(i)
		}
		if c.Landed || c.Crashed {
			break
		}
	}
	s := w.Sites[0]
	return HaversineM(c.LandedLatDeg, c.LandedLonDeg, s.LatDeg, s.LonDeg, c.Primary.RadiusMeters()), lastSpeed, c.Landed
}

// ADR 0067 guard 2: the Lander flown by the autopilot from the S11 fixture
// lands under 10 m/s within 50 m of a modern site, through brake, approach and
// final in that order, and says the true distance in its Event Flash.
func TestLandingAutopilotLandsTheLanderOnTheFlag(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	var phases []spacecraft.LandingPhase
	vz100, seen100 := 0.0, false
	dist, v, landed := flyLanding(t, w, c, 2*3600*4, func(int) {
		if p := c.Landing.Phase; len(phases) == 0 || phases[len(phases)-1] != p {
			phases = append(phases, p)
		}
		if f := apFrameOf(c); !seen100 && c.Landing.Phase == spacecraft.PhaseFinal && f.alt <= 100 {
			vz100, seen100 = f.vz, true
		}
	})
	// the schedule's top: 3 m/s down at 100 m (found in the live check falling
	// at 12 m/s there before the profile's feed-forward)
	if !seen100 || math.Abs(vz100+FinalRateAt100mMps) > 1.5 {
		t.Errorf("at 100 m the descent rate is %.2f m/s (seen=%v), want %.1f within 1.5", vz100, seen100, -FinalRateAt100mMps)
	}
	if !landed || dist > 50 || v >= CrashVCritMps {
		t.Errorf("landed=%v dist=%.1f m speed=%.2f m/s, want landed within 50 m under %v m/s", landed, dist, v, CrashVCritMps)
	}
	want := []spacecraft.LandingPhase{spacecraft.PhaseCoast, spacecraft.PhaseBrake, spacecraft.PhaseApproach, spacecraft.PhaseFinal, spacecraft.PhaseCoast}
	if len(phases) != len(want) { // the last Coast is the autopilot standing down at contact
		t.Fatalf("phases %v, want %v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phases %v, want %v", phases, want)
		}
	}
	if len(w.PendingLandingEvents) != 1 || math.Abs(w.PendingLandingEvents[0].DistanceM-dist) > 1 || w.PendingLandingEvents[0].Crashed {
		t.Errorf("landing events %+v, want one touchdown at %.1f m", w.PendingLandingEvents, dist)
	}
}

// ADR 0067 guard 3: unattended. The same landing with another vessel active
// (the autopilot flies from the vessel's own Target) ends the same way.
func TestLandingAutopilotFliesUnattended(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatal(err)
	}
	w.SetActiveCraftIdx(len(w.Crafts) - 1)
	if w.ActiveCraft() == c {
		t.Fatal("setup: the lander is still active")
	}
	for i := 0; i < 2*3600*4 && !c.Landed && !c.Crashed; i++ {
		w.Tick()
	}
	s := w.Sites[0]
	dist := HaversineM(c.LandedLatDeg, c.LandedLonDeg, s.LatDeg, s.LonDeg, c.Primary.RadiusMeters())
	if !c.Landed || dist > 50 {
		t.Errorf("unattended: landed=%v crashed=%v dist=%.1f m, want landed within 50 m", c.Landed, c.Crashed, dist)
	}
}

// ADR 0067 guard 4 (playtest Q4a: the keys are e/q): e/q hand over a descent rate; from 100 m the throttle holds
// the commanded rate within 0.2 m/s with the nose still the autopilot's.
func TestLandingAutopilotRODHoldsTheCommandedRate(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	var cmd float64
	var held, checked int
	flyLanding(t, w, c, 2*3600*4, func(int) {
		ap := c.Landing
		if c.Landed || c.Crashed || !ap.Engaged {
			return
		}
		f := apFrameOf(c)
		if !ap.ROD && f.alt <= 100 {
			// P66: the first press starts the hold at the rate the vessel is
			// falling and steps it by one click toward a slower descent
			if !w.LandingROD(RODStepMps) {
				t.Fatal("LandingROD refused an engaged autopilot")
			}
			cmd = ap.RODRateMps
			if want := fmt.Sprintf("Site+ ROD \u2212%.1f m/s", -cmd); w.siteHoldLabel() != want {
				t.Errorf("hold reads %q, want %q", w.siteHoldLabel(), want)
			}
			return
		}
		if ap.ROD && f.alt > 40 {
			checked++
			if math.Abs(f.vz-cmd) <= 0.2 {
				held++
			}
		}
	})
	if checked < 20 || cmd >= 0 {
		t.Fatalf("setup: ROD checked %d ticks at cmd %.2f", checked, cmd)
	}
	if held < checked*3/4 { // the first seconds are the transient to the new rate
		t.Errorf("rate held within 0.2 m/s of %.2f on %d of %d ticks, want 3/4 of them", cmd, held, checked)
	}
	if !c.Landed {
		t.Errorf("ROD landing ended landed=%v crashed=%v", c.Landed, c.Crashed)
	}
}

// ADR 0067 guard 5: out of reach reads exactly when the law saturates, with
// the equal case at the near rung.
func TestLandingAutopilotSaturationBoundary(t *testing.T) {
	near := SiteShiftRungNearM
	floor := 0.55
	for _, tc := range []struct {
		name         string
		theta, along float64
		spent, want  bool
	}{
		{"full throttle, long by the rung", 1, near, false, false},
		{"full throttle, long past the rung", 1, near + 1, false, true},
		{"full throttle, short past the rung", 1, -near - 1, false, false},
		{"just under full, long past the rung", 1 - 0.01, near + 1, false, false},
		{"hover floor, short by the rung", floor, -near, false, false},
		{"hover floor, short past the rung", floor, -near - 1, false, true},
		{"hover floor, long past the rung", floor, near + 1, false, false},
		{"pitch spent, mid throttle, short by the rung", 0.8, -near, true, false},
		{"pitch spent, mid throttle, short past the rung", 0.8, -near - 1, true, true},
		{"pitch spent, mid throttle, long past the rung", 0.8, near + 1, true, false},
	} {
		if got := apSaturated(tc.theta, floor, tc.along, tc.spent); got != tc.want {
			t.Errorf("%s: apSaturated(%.2f, %.2f, %.0f) = %v, want %v", tc.name, tc.theta, floor, tc.along, got, tc.want)
		}
	}
}

// ADR 0067 decision 10: a site beyond the brake's steering authority refuses
// with the signed shortfall; one the aim is walked past in flight is flagged
// out of reach and still lands softly at the nearest point.
func TestLandingAutopilotOutOfReach(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2*3600*4 && (len(c.Nodes) > 0 || c.ActiveBurn != nil); i++ {
		w.Tick() // fly the deorbit burn
	}
	c.Crewed, c.Controllable = true, true // the fixture's crew flag does not survive a tick
	// a flag 20% further along the track than the stop point
	s := w.Sites[0]
	w.plantSite(c, SiteFlag, "far", s.LatDeg, s.LonDeg-4)
	w.SetTargetSite(w.Sites[len(w.Sites)-1].ID)
	err := w.EngageLandingAutopilot()
	var reach *SiteHoldReachError
	if !errors.As(err, &reach) || c.Landing != nil && c.Landing.Engaged {
		t.Fatalf("engage on a far flag: err=%v engaged=%v, want a SiteHoldReachError and no engagement", err, c.Landing != nil && c.Landing.Engaged)
	}
	t.Logf("refusal along=%.0f cross=%.0f", reach.AlongM, reach.CrossM)
	// the real site, then walk the aim 150 km short mid-brake: past what the
	// reserve throttle can shorten the stop by (measured ~44 km, 10% of the
	// stopping distance), so the law saturates and lands at the nearest point
	w.SetTargetSite(s.ID)
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	var flagged bool
	for i := 0; i < 2*3600*4 && !c.Landed && !c.Crashed; i++ {
		w.Tick()
		if c.Landing.Phase == spacecraft.PhaseBrake && c.Landing.AimAlongM == 0 {
			c.Landing.AimAlongM = -150000
		}
		flagged = flagged || c.Landing.OutOfReach
	}
	dist := HaversineM(c.LandedLatDeg, c.LandedLonDeg, s.LatDeg, s.LonDeg, c.Primary.RadiusMeters())
	if !flagged || !c.Landed || dist < 30000 || dist > 60000 {
		t.Errorf("aim walked out of reach: flagged=%v landed=%v crashed=%v dist=%.0f m, want flagged, landed soft ~44 km short of the flag", flagged, c.Landed, c.Crashed, dist)
	}
	if len(w.PendingLandingEvents) != 1 || !w.PendingLandingEvents[0].OutOfReach {
		t.Errorf("events %+v, want one out-of-reach landing", w.PendingLandingEvents)
	}
}

// ADR 0067 decision 5: engaged and coasting, the autopilot does nothing until
// the brake's cue, glides warp down so no tick steps past it, and lights within
// a tick of it.
func TestLandingAutopilotLightsAtTheCueUnderWarp(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2*3600*4 && (len(c.Nodes) > 0 || c.ActiveBurn != nil); i++ {
		w.Tick()
	}
	c.Crewed, c.Controllable = true, true
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	w.Clock.WarpIdx = len(WarpFactors) - 1
	var lit bool
	for i := 0; i < 100000 && !lit; i++ {
		w.Tick()
		if c.ManualBurn != nil {
			lit = true
		} else if c.Landing.Phase != spacecraft.PhaseCoast {
			t.Fatal("left Coast without a burn")
		}
	}
	if !lit {
		t.Fatal("never lit")
	}
	// the brake is lit at the latest instant that still stops above the
	// ground: the reserve-throttle stop from the lit state keeps a margin of at
	// least 0 (not late) and under ~2.5 s of sink (not early; the stop margin
	// moves ~0.5 km a second here)
	pred, ok := predictPoweredStopFromAt(c.State, c, apStopHorizon, DeorbitBrakeThrottle)
	if !ok || pred.Outcome != StopStopped || pred.MarginM < 0 || pred.MarginM > 1500 {
		t.Errorf("lit with stop outcome=%v margin=%.0f m, want stopped with margin in [0, 1500] m", pred.Outcome, pred.MarginM)
	}
}

// ADR 0067 decision 5: a hold key takes the nose (autopilot off); z/x give the
// raw throttle back with the steering kept; q re-engages from where it stands.
func TestLandingAutopilotHandover(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	flyLanding(t, w, c, 2*3600*4, func(int) {
		if c.Landing.Phase == spacecraft.PhaseCoast || c.Landed || c.Crashed || c.Landing.ThrottleHeld {
			return
		}
		if !w.LandingHandOverThrottle() {
			t.Fatal("LandingHandOverThrottle refused an engaged autopilot")
		}
		c.Throttle = 0.75 // the pilot's raw throttle
	})
	if c.Landing.Engaged {
		t.Fatal("setup: the autopilot did not stand down at contact")
	}
	// throttle held: the autopilot never wrote it again
	w2, c2 := deorbitWorld(t, 150, 0)
	var seen []float64
	flyLanding(t, w2, c2, 2*3600*4, func(int) {
		if c2.Landing.Phase == spacecraft.PhaseCoast || c2.Landed || c2.Crashed {
			return
		}
		if !c2.Landing.ThrottleHeld {
			w2.LandingHandOverThrottle()
			c2.Throttle = 0.75
		}
		if len(seen) < 200 {
			seen = append(seen, c2.Throttle)
		}
	})
	for _, th := range seen {
		if th != 0.75 {
			t.Fatalf("throttle moved to %v under ThrottleHeld, want the pilot's 0.75", th)
		}
	}
	if len(seen) < 100 {
		t.Fatalf("setup: only %d held ticks", len(seen))
	}
	// a hold key: autopilot off, and q engages again
	w3, c3 := deorbitWorld(t, 150, 0)
	if _, err := w3.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	if err := w3.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	w3.DisengageLandingAutopilot(c3)
	if c3.Landing.Engaged {
		t.Error("DisengageLandingAutopilot left it engaged")
	}
	if err := w3.EngageLandingAutopilot(); err != nil || !c3.Landing.Engaged {
		t.Errorf("re-engage: err=%v engaged=%v", err, c3.Landing.Engaged)
	}

	// q while engaged takes everything back: the raw throttle and the ROD mode
	// go, the phase stays (ADR 0067 decision 5: q re-engages from where you are)
	w4, c4 := deorbitWorld(t, 150, 0)
	var hadHeld, hadROD bool
	var phaseBefore spacecraft.LandingPhase
	flyLanding(t, w4, c4, 2*3600*4, func(int) {
		ap := c4.Landing
		switch {
		case !hadHeld && ap.Phase == spacecraft.PhaseBrake:
			w4.LandingHandOverThrottle()
			c4.Throttle = 0.75
			phaseBefore, hadHeld = ap.Phase, true
			if err := w4.EngageLandingAutopilot(); err != nil || ap.ThrottleHeld || ap.Phase != phaseBefore {
				t.Errorf("q after z: err=%v held=%v phase=%v, want throttle taken back in phase %v", err, ap.ThrottleHeld, ap.Phase, phaseBefore)
			}
		case hadHeld && !hadROD && ap.Phase == spacecraft.PhaseFinal:
			hadROD = w4.LandingROD(+RODStepMps)
			if !ap.ROD {
				t.Fatal("setup: Z did not latch the descent rate in final")
			}
			if err := w4.EngageLandingAutopilot(); err != nil || ap.ROD || ap.Phase != spacecraft.PhaseFinal {
				t.Errorf("q after Z: err=%v ROD=%v phase=%v, want ROD off in final", err, ap.ROD, ap.Phase)
			}
		}
	})
	if !hadHeld || !hadROD {
		t.Fatalf("setup: brake handover %v, final ROD %v", hadHeld, hadROD)
	}
}

// x or z pressed on the coast must not freeze the brake: there is no throttle
// to hand over before the light, so the autopilot still trims it (review F1:
// the latched 90% brake crashed 2,573 m from the flag).
func TestLandingAutopilotCoastThrottleKeyIsNoOp(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	pressed := false
	dist, _, landed := flyLanding(t, w, c, 2*3600*4, func(int) {
		if !pressed && c.Landing.Phase == spacecraft.PhaseCoast {
			w.LandingHandOverThrottle() // what x/z do first
			w.SetThrottle(0)
			pressed = true
		}
	})
	if !pressed || c.Landing.ThrottleHeld {
		t.Fatalf("setup: pressed=%v held=%v", pressed, c.Landing.ThrottleHeld)
	}
	if !landed || dist > 50 {
		t.Errorf("x on the coast: landed=%v %.0f m from the flag, want a landing within 50 m", landed, dist)
	}
}

// Z/X latch a descent rate only in approach and final; in brake the key is the
// raw throttle's and the orbital sink rate is not latched (review F4: -69 m/s).
func TestLandingAutopilotRODNotInBrake(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	braked, final := false, false
	flyLanding(t, w, c, 2*3600*4, func(int) {
		ap := c.Landing
		if !braked && ap.Phase == spacecraft.PhaseBrake {
			braked = true
			if w.LandingROD(+RODStepMps) || ap.ROD {
				t.Errorf("Z in brake latched ROD %.1f m/s, want the key left to the throttle", ap.RODRateMps)
			}
		}
		if !final && ap.Phase == spacecraft.PhaseFinal { // the equal side of the gate
			final = true
			if !w.LandingROD(+RODStepMps) || !ap.ROD {
				t.Error("Z in final did not latch ROD")
			}
		}
	})
	if !braked || !final {
		t.Fatalf("setup: braked=%v final=%v", braked, final)
	}
}

// ADR 0067 decision 5: RCS pulses are blocked while the autopilot owns the
// engine, and Site+ is no pulse direction in this slice.
func TestLandingAutopilotBlocksRCS(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	c.Crewed, c.Controllable = true, true
	c.EngineMode = spacecraft.EngineRCS
	c.Monoprop = 100
	if !w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Fatal("setup: an RCS pulse does not fire on the unengaged Lander")
	}
	c.Crewed, c.Controllable = true, true // the pulse re-derives the crew flag from the stages
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	if w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Error("an RCS pulse fired while the autopilot owns the engine")
	}
	if w.FireRCSPulse(spacecraft.BurnSite) {
		t.Error("BurnSite fired as an RCS pulse")
	}
}

// Found in the live check: a coast at 100000x flies the exact Kepler orbit,
// not the 1x integrator the deorbit plan was solved on, so the brake lights
// seconds later with the stop kilometres long. The law still lands within 50 m
// of the flag (it first stood 939 m off, then 69 m, before the surplus-margin
// and closing-speed terms).
func TestLandingAutopilotLandsAfterAHighWarpCoast(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	w.Clock.BaseStep = 50 * time.Millisecond // the game's own step
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4000000 && !c.Landed && !c.Crashed; i++ {
		w.Clock.WarpIdx = len(WarpFactors) - 1
		if c.Landing.Phase != spacecraft.PhaseCoast {
			w.Clock.WarpIdx = 1 // the brake is warp-capped at 10x in the game
		}
		w.Tick()
	}
	s := w.Sites[0]
	d := HaversineM(c.LandedLatDeg, c.LandedLonDeg, s.LatDeg, s.LonDeg, c.Primary.RadiusMeters())
	if !c.Landed || d > 50 {
		t.Errorf("after a 100000x coast: landed=%v crashed=%v dist=%.0f m, want landed within 50 m", c.Landed, c.Crashed, d)
	}
}

// F2: stepping to another vessel and back re-enters SITE for an engaged
// autopilot, so q (which resolves by frame) stays the autopilot's key.
func TestLandingAutopilotSurvivesVesselSwitch(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	w.setNavMode(NavSite)
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatal(err)
	}
	other, home := 1-w.ActiveCraftIdx, w.ActiveCraftIdx
	if w.Crafts[home] != c {
		other, home = w.ActiveCraftIdx, 1-w.ActiveCraftIdx
		w.SetActiveCraftIdx(home)
	}
	w.setNavMode(NavSite)
	phase := c.Landing.Phase
	w.SetActiveCraftIdx(other)
	w.SetActiveCraftIdx(home)
	if w.NavMode != NavSite {
		t.Fatalf("NavMode after ] [ = %v, want SITE", w.NavMode)
	}
	if err := w.EngageLandingAutopilot(); err != nil || !c.Landing.Engaged || c.Landing.Phase != phase {
		t.Errorf("q after ] [: err=%v engaged=%v phase %v -> %v", err, c.Landing.Engaged, phase, c.Landing.Phase)
	}
}

// Playtest P3: N on a site alone (no ;, no q, no other key) lands the vessel.
// It plants the burn, engages Site+ and puts the navball on SITE.
func TestPlanDeorbitAndEngageLandsWithNoOtherKey(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitAndEngage(); err != nil {
		t.Fatal(err)
	}
	if !c.Landing.Engaged || c.AttitudeMode != spacecraft.BurnSite || w.NavMode != NavSite {
		t.Fatalf("after N: engaged=%v hold=%v nav=%v, want Site+ on SITE", c.Landing.Engaged, c.AttitudeMode, w.NavMode)
	}
	if _, err := w.PlanDeorbitAndEngage(); err != nil || !c.Landing.Engaged { // N again re-solves, stays engaged
		t.Fatalf("second N: err=%v engaged=%v", err, c.Landing.Engaged)
	}
	for i := 0; i < 2*3600*4 && !c.Landed && !c.Crashed; i++ {
		w.Tick()
	}
	s := w.Sites[0]
	if d := HaversineM(c.LandedLatDeg, c.LandedLonDeg, s.LatDeg, s.LonDeg, c.Primary.RadiusMeters()); !c.Landed || d > 50 {
		t.Errorf("landed=%v %.1f m from the flag, want within 50 m", c.Landed, d)
	}
}

// afterDeorbitBurn plants N's burn and ticks until it has fired: the vessel is
// falling toward the flag with the engine dark and no node planted.
func afterDeorbitBurn(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2*3600*4 && (len(c.Nodes) > 0 || c.ActiveBurn != nil); i++ {
		w.Tick()
	}
	if len(c.Nodes) > 0 || c.ActiveBurn != nil {
		t.Fatal("setup: the deorbit burn never fired")
	}
	c.Crewed, c.Controllable = true, true // as the cue test: the burn drains the fixture's link
	return w, c
}

// Fix E (P7): q is a nose hold. It never lights, cuts or throttles the engine
// and never caps warp; the pilot lights the brake and the nose follows the law.
func TestNoseHoldSteersOnlyAndNeverTouchesTheEngine(t *testing.T) {
	w, c := afterDeorbitBurn(t)
	if err := w.EngageLandingNoseHold(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		w.Tick()
	}
	if c.ManualBurn != nil || c.Landing.Phase != spacecraft.PhaseCoast {
		t.Errorf("engine dark: burn=%v phase=%v, want nothing lit and Coast (wait)", c.ManualBurn != nil, c.Landing.Phase)
	}
	if lim, capped := w.landingAutopilotWarpLimit(); capped {
		t.Errorf("nose hold capped warp at %v", lim)
	}
	c.Throttle = 0.9 // the pilot lights the brake by hand
	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
	for i := 0; i < 200; i++ {
		w.Tick()
		if c.Throttle != 0.9 {
			t.Fatalf("tick %d: throttle moved to %v under the nose hold, want the pilot's 0.9", i, c.Throttle)
		}
	}
	if c.Landing.Phase != spacecraft.PhaseBrake {
		t.Errorf("phase %v, want Brake once the engine is lit", c.Landing.Phase)
	}
	if d := c.SiteDir.Dot(apFrameOf(c).retro()); d < 0.9 || math.Abs(c.SiteDir.Norm()-1) > 1e-9 {
		t.Errorf("nose %v dot retro %.3f, want the guidance direction near surface retrograde", c.SiteDir, d)
	}
	if c.AttitudeMode != spacecraft.BurnSite {
		t.Errorf("attitude %v, want the hold kept", c.AttitudeMode)
	}
	if w.LandingROD(+1) || w.LandingTakeEngine() || w.LandingHandOverThrottle() {
		t.Error("e/q, b or z/x were consumed by the nose hold; they are plain keys under it")
	}
}

// P9: under the hand nose hold the final phase holds local vertical, with no
// lean and no autopilot forecast; the N autopilot's final law is untouched.
func TestNoseHoldFinalPhaseHoldsStraightUp(t *testing.T) {
	w, c := afterDeorbitBurn(t)
	if err := w.EngageLandingNoseHold(); err != nil {
		t.Fatal(err)
	}
	c.Throttle = 0.9
	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
	w.Tick()
	c.Landing.Phase = spacecraft.PhaseFinal
	// drift sideways: apFinal would lean against it
	f := apFrameOf(c)
	side := f.up.Cross(c.State.V).Unit() // some horizontal
	c.State.V = c.State.V.Sub(f.vh).Add(side.Scale(2.9))
	w.Tick()
	f = apFrameOf(c)
	if c.Landing.Phase != spacecraft.PhaseFinal {
		t.Fatalf("phase %v, want Final", c.Landing.Phase)
	}
	if d := c.SiteDir.Dot(f.up); d < 1-1e-9 {
		t.Errorf("nose dot up = %v, want straight up (1)", d)
	}
	if c.Landing.ForecastOK {
		t.Error("nose hold final left an autopilot forecast set")
	}
}

// Fix E: N from anywhere. Falling after the deorbit fired it engages the full
// autopilot with no new node, and over a nose hold it keeps the phase.
func TestNEngagesMidDescentWithNoNewBurn(t *testing.T) {
	w, c := afterDeorbitBurn(t)
	plan, err := w.PlanDeorbitAndEngage()
	if err != nil || plan != nil {
		t.Fatalf("N mid-descent: plan=%v err=%v, want engaged in place (nil, nil)", plan, err)
	}
	if len(c.Nodes) != 0 || !c.Landing.Engaged || c.Landing.Nose || w.NavMode != NavSite {
		t.Errorf("nodes=%d engaged=%v nose=%v nav=%v, want no node, full autopilot on SITE", len(c.Nodes), c.Landing.Engaged, c.Landing.Nose, w.NavMode)
	}
	w2, c2 := afterDeorbitBurn(t)
	if err := w2.EngageLandingNoseHold(); err != nil {
		t.Fatal(err)
	}
	c2.Landing.Phase = spacecraft.PhaseApproach
	if err := w2.EngageLandingAutopilot(); err != nil || c2.Landing.Nose || c2.Landing.Phase != spacecraft.PhaseApproach {
		t.Errorf("N over the nose hold: err=%v nose=%v phase=%v, want the auto in Approach", err, c2.Landing.Nose, c2.Landing.Phase)
	}
}
