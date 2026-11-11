package sim

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
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
			if want := fmt.Sprintf("Site+ ROD \u2212%.1f m/s", -cmd); w.siteHoldLabel(w.ActiveCraft()) != want {
				t.Errorf("hold reads %q, want %q", w.siteHoldLabel(w.ActiveCraft()), want)
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

// P33: under the q nose hold the arrows' trims lean the nose and the brake
// forecast behind miss: includes them; | (zeroing both) gives both back.
func TestNoseHoldTrimsMoveMissAndPitch(t *testing.T) {
	w, c := afterDeorbitBurn(t)
	if err := w.EngageLandingNoseHold(); err != nil {
		t.Fatal(err)
	}
	c.Throttle = 0.9
	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
	read := func() (missM, elevDeg float64) {
		w.Tick()
		lb, ok := w.LandBesideReading()
		if !ok || !lb.MissOK {
			t.Fatal("no miss reading in the brake")
		}
		dir := c.BurnDirectionWithTarget(spacecraft.BurnSite, orbital.Vec3{}, orbital.Vec3{})
		return lb.MissM, math.Asin(dir.Dot(apFrameOf(c).up)) / rad
	}
	for i := 0; i < 30; i++ {
		read()
	}
	miss0, el0 := read()
	c.PitchTrim = -2 * spacecraft.PitchTrimStepRad // two left arrows
	miss1, el1 := read()
	t.Logf("trim 0: miss %.0f m, nose %.1f deg; two lefts: miss %.0f m, nose %.1f deg", miss0, el0, miss1, el1)
	if math.Abs(miss1-miss0) < 100 {
		t.Errorf("two lean presses moved miss: by %.0f m, want it to follow the trimmed brake", miss1-miss0)
	}
	if d := math.Abs(el1 - el0); d < 1 { // the lean rotates in the east-up plane, so an off-axis track shows part of the 10 degrees
		t.Errorf("two lean presses moved the nose %.1f deg, want at least 1", d)
	}
	c.PitchTrim, c.HeadingTrim = 0, 0
	miss2, el2 := read()
	if math.Abs(miss2-miss0) > math.Abs(miss1-miss0)/4 || math.Abs(el2-el0) > 1 {
		t.Errorf("after | miss %.0f (was %.0f), nose %.1f (was %.1f): want both back", miss2, miss0, el2, el0)
	}
}

// Review-2 L5: N pressed in a hover goes straight to Final, which nothing
// ever gave a track plane, so the aim's axes hung on the instantaneous drift
// direction and the computer's site swung with it. The plane is fixed at the
// engage.
func TestLandingEngageInAHoverFixesTheTrackPlane(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	drift := func(d orbital.Vec3) {
		vGround := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
		c.State.V = c.State.V.Sub(vGround).Add(d) // co-moving with the ground, plus d
	}
	up := c.State.R.Unit()
	e1 := up.Cross(orbital.Vec3{Z: 1}).Unit()
	e2 := up.Cross(e1).Unit()
	drift(e1.Scale(1)) // 1 m/s over the ground: under FinalPhaseGroundSpeedMps
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	ap := c.Landing
	if ap.Phase != spacecraft.PhaseFinal {
		t.Fatalf("setup: hover engaged in %v, want Final", ap.Phase.Word())
	}
	if ap.TrackN.Norm() == 0 {
		t.Errorf("no track plane fixed at a hover engage")
	}
	ap.AimAlongM = 100
	siteR := siteVec(c, w.Sites[0].LatDeg, w.Sites[0].LonDeg, w.Clock.SimTime).Scale(c.Primary.RadiusMeters())
	before := computerSitePoint(c, siteR)
	drift(e2.Scale(1)) // the drift turns 90 degrees
	if d := computerSitePoint(c, siteR).Sub(before).Norm(); d > 1e-6 {
		t.Errorf("computer's site moved %.1f m when the drift direction turned", d)
	}
}

// flyEntry plans N, engages Site+ and ticks to contact on a world from
// entryWorld; it returns the Event Flash the landing raised.
func flyEntry(t *testing.T, w *World, c *spacecraft.Spacecraft) (LandingEvent, *DeorbitPlan) {
	t.Helper()
	plan, err := w.PlanDeorbitOntoSite()
	if err != nil {
		t.Fatalf("N: %v", err)
	}
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatalf("engage: %v", err)
	}
	for i := 0; i < 4*3600*20 && !c.Landed && !c.Crashed; i++ {
		w.Tick()
	}
	if len(w.PendingLandingEvents) == 0 {
		t.Fatalf("no landing flash (landed=%v crashed=%v)", c.Landed, c.Crashed)
	}
	return w.PendingLandingEvents[len(w.PendingLandingEvents)-1], plan
}

// ADR 0069 guard 1: a lander that cannot stop from orbit on its tank gets a plan
// on a body with air, and the flown landing is within 50 m. Both vessels were
// refused before (Ground-Station Lander: no solution; Endpoint Station: brake
// needs 1779 km). With the air off the same orbit still refuses.
func TestEntryPlanLandsOnMarsOnTheFlag(t *testing.T) {
	for _, name := range []string{"Ground-Station-Lander", "Endpoint-Station"} {
		w, c := entryWorld(t, "", "mars", name, 150e3, true)
		ev, plan := flyEntry(t, w, c)
		if ev.Crashed || !c.Landed || ev.DistanceM > 50 {
			t.Errorf("%s: landed=%v crashed=%v %.1f m from the flag", name, c.Landed, ev.Crashed, ev.DistanceM)
		}
		t.Logf("Mars %s: plan %.1f m/s retro in %v, long %.0f m; landed %.1f m from the flag, oor=%v", name, plan.DV, plan.In.Round(time.Second), plan.LongM, ev.DistanceM, ev.OutOfReach)

		w2, _ := entryWorld(t, "", "mars", name, 150e3, false)
		if _, err := w2.PlanDeorbitOntoSite(); err == nil {
			t.Errorf("%s: no air, yet N planned", name)
		}
	}
}

// #936: a landing the final phase brings to the flag is not out of reach, with
// or without air. The air case latched the brake phase's saturated reading.
func TestLandingFlashNotOutOfReachWhenOnTheFlag(t *testing.T) {
	for _, air := range []bool{false, true} {
		w, c := entryWorld(t, "Lumen", "rust", "Endpoint-Station", 80e3, air)
		ev, _ := flyEntry(t, w, c)
		if ev.Crashed || ev.DistanceM > 25 {
			t.Fatalf("air=%v: landed %.1f m from the flag (crashed=%v)", air, ev.DistanceM, ev.Crashed)
		}
		if ev.OutOfReach {
			t.Errorf("air=%v: flash says out of reach for a landing %.1f m from the flag", air, ev.DistanceM)
		}
	}
}

// boosterFall is one arrival of the one-burn fixture: where the Falcon 9 first
// stage meets the air over LC-13's LZ-1, in metres and m/s of ground-relative
// motion (north/east offsets of the vessel from the LZ, and its sideways drift).
type boosterFall struct{ altM, downMps, offN, offE, driftN, driftE, fuelKg float64 }

// boosterWorld is a Falcon 9 first stage (part f9-s1) as its own vessel, falling
// over Earth at b, with LC-13 LZ-1 as its target. There is no dropped-stage
// vessel yet (ADR 0069 slice 8), so the fixture builds the vessel itself.
func boosterWorld(t *testing.T, b boosterFall) (*World, *spacecraft.Spacecraft, Site) {
	t.Helper()
	return fallWorld(t, spacecraft.StageModuleF9S1ID, b)
}

// fallWorld is boosterWorld for any catalog module: the stage alone, falling.
func fallWorld(t *testing.T, module string, b boosterFall) (*World, *spacecraft.Spacecraft, Site) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.navErrorOff = true
	st, ok := spacecraft.BuildModule(module)
	if !ok {
		t.Fatalf("no %s module", module)
	}
	st[0].FuelMass = b.fuelKg
	c, err := w.SpawnCraft(SpawnSpec{CustomStages: st, ParentBodyID: "earth", AltitudeM: 200e3})
	if err != nil {
		t.Fatal(err)
	}
	for i, x := range w.Crafts {
		if x == c {
			w.SetActiveCraftIdx(i)
		}
	}
	c.Landed, c.Crashed, c.Controllable = false, false, true
	var lc13 Site
	for _, z := range shippedZones {
		if z.Name == "LC-13" {
			lc13 = z
		}
	}
	w.SetTargetSiteLZ(lc13.ID, 1)
	R := c.Primary.RadiusMeters()
	la, lo := lc13.lzLatLon(1, R)
	hat := siteVec(c, la, lo, w.Clock.SimTime)
	ax := render.BodyRotationAxisWorld(c.Primary)
	axis := orbital.Vec3{X: ax.X, Y: ax.Y, Z: ax.Z}
	east := axis.Cross(hat).Unit()
	north := hat.Cross(east).Unit()
	up := hat.Add(east.Scale(b.offE / R)).Add(north.Scale(b.offN / R)).Unit()
	c.State.R = up.Scale(R + b.altM)
	rel := up.Scale(-b.downMps).Add(east.Scale(b.driftE)).Add(north.Scale(b.driftN))
	c.State.V = rel.Add(physics.AtmosphereOmega(c.Primary).Cross(c.State.R))
	c.State.M = c.TotalMass()
	c.CurrentAttitudeDir = rel.Scale(-1).Unit()
	return w, c, lc13
}

// flyBooster engages N and ticks to contact. It returns the contact speed
// (ground-relative, the tick before contact), the touchdown distance from the
// LZ, the largest throttle the vessel ran, and whether it landed.
func flyBooster(t *testing.T, w *World, c *spacecraft.Spacecraft, lc13 Site) (speed, distM, peakThrottle float64, landed bool) {
	t.Helper()
	if plan, err := w.PlanDeorbitAndEngage(); err != nil || plan != nil {
		t.Fatalf("N on the falling booster: plan=%v err=%v, want engaged in place", plan, err)
	}
	for i := 0; i < 20*60*4 && !c.Landed && !c.Crashed; i++ {
		speed = physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary).Norm()
		w.Tick()
		if c.ManualBurn != nil { // lit: before the light the core throttle is the spawn default
			peakThrottle = math.Max(peakThrottle, c.EffectiveThrottle())
		}
	}
	la, lo := lc13.lzLatLon(1, c.Primary.RadiusMeters())
	return speed, HaversineM(c.LandedLatDeg, c.LandedLonDeg, la, lo, c.Primary.RadiusMeters()), peakThrottle, c.Landed
}

// tenFalls are the ten seeded arrivals of guard 6: the ADR's 250 m/s at the
// ignition height, mixed offsets and drifts, 6.5 t of propellant.
func tenFalls() []boosterFall {
	rng := rand.New(rand.NewSource(69))
	out := make([]boosterFall, 10)
	for i := range out {
		out[i] = boosterFall{
			altM: 4000 + 1500*rng.Float64(), downMps: 235 + 25*rng.Float64(),
			offN: 100 * (2*rng.Float64() - 1), offE: 100 * (2*rng.Float64() - 1),
			driftN: 6 * (2*rng.Float64() - 1), driftE: 6 * (2*rng.Float64() - 1), fuelKg: 6500,
		}
	}
	return out
}

// ADR 0069 guard 6: a marked vessel's contact speed is under 10 m/s over ten
// seeded landings, and it burns on its one engine of nine, not more.
func TestOneBurnTenSeededBoosterLandings(t *testing.T) {
	for i, b := range tenFalls() {
		w, c, lc13 := boosterWorld(t, b)
		if c.LandingThrottle() != 1.0/9 {
			t.Fatalf("f9-s1 landing throttle %v, want 1/9", c.LandingThrottle())
		}
		v, d, peak, landed := flyBooster(t, w, c, lc13)
		t.Logf("landing %d: from %.0f m at %.0f m/s, off %.0f/%.0f m: contact %.2f m/s, %.1f m from LZ-1, peak throttle %.3f, landed=%v", i+1, b.altM, b.downMps, b.offN, b.offE, v, d, peak, landed)
		if !landed || v >= CrashVCritMps {
			t.Errorf("landing %d: landed=%v contact %.2f m/s, want landed under %v", i+1, landed, v, CrashVCritMps)
		}
		if peak > c.LandingThrottle()+1e-9 {
			t.Errorf("landing %d: throttle reached %.3f, over the landing throttle %.3f", i+1, peak, c.LandingThrottle())
		}
	}
}

// The landing throttle is a hard cap: a burn that falls behind (60 m/s extra
// at the light) does not borrow the other eight engines. The vessel runs at its
// cap and hits hard.
func TestOneBurnLandingThrottleIsACap(t *testing.T) {
	w, c, lc13 := boosterWorld(t, boosterFall{altM: 4600, downMps: 250, fuelKg: 6500})
	if plan, err := w.PlanDeorbitAndEngage(); err != nil || plan != nil {
		t.Fatalf("N: plan=%v err=%v", plan, err)
	}
	for i := 0; i < 400 && c.ManualBurn == nil; i++ {
		w.Tick()
	}
	if c.ManualBurn == nil {
		t.Fatal("the burn never lit")
	}
	c.State.V = c.State.V.Add(c.State.R.Unit().Scale(-60))
	peak := 0.0
	for i := 0; i < 4000 && !c.Landed && !c.Crashed; i++ {
		w.Tick()
		if c.ManualBurn != nil {
			peak = math.Max(peak, c.EffectiveThrottle())
		}
	}
	_ = lc13
	if peak > c.LandingThrottle()+1e-9 || peak < c.LandingThrottle()-0.005 {
		t.Errorf("a burn behind ran at %.4f, want pinned at the cap %.4f", peak, c.LandingThrottle())
	}
	if c.Landed {
		t.Error("a burn 60 m/s behind landed on one engine anyway")
	}
}

// ADR 0069 decision 10: the marked vessel has no descent-rate schedule. The same
// stage is down to the schedule's few m/s at 100 m with the mark off (nine
// engines, ADR 0067, stopped 120 m up), and still on the one burn's own profile,
// ending at the ground at 2 m/s, with it on. Unmarked Moon and Mars landers are pinned by the
// existing landing tests above, which pass unchanged.
func TestOneBurnMarkReplacesTheScheduleOnlyWhereMarked(t *testing.T) {
	vzAt100 := func(mark float64) float64 {
		w, c, _ := boosterWorld(t, boosterFall{altM: 4600, downMps: 250, fuelKg: 6500})
		c.Stages[0].LandingThrottle = mark
		if _, err := w.PlanDeorbitAndEngage(); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 4000 && !c.Landed && !c.Crashed; i++ {
			w.Tick()
			if f := apFrameOf(c); c.Landing.Phase != spacecraft.PhaseCoast && f.alt <= 100 {
				return f.vz
			}
		}
		t.Fatalf("mark %v: never reached 100 m under the autopilot", mark)
		return 0
	}
	if vz := vzAt100(0); vz < -2*FinalRateAt100mMps {
		t.Errorf("unmarked at 100 m falls %.2f m/s, want the schedule's slow few m/s (stop held 120 m up)", vz)
	}
	if vz := vzAt100(1.0 / 9); vz > -10*FinalRateAt100mMps {
		t.Errorf("marked at 100 m falls %.2f m/s, want the one burn's profile (~50 m/s), far past the schedule", vz)
	}
}

// The marks ship on the Falcon and Packet stages and Dragon. A Dragon's
// Dracos cannot brake (1.6 kN), its SuperDracos can (ADR 0069 slice 6).
func TestOneBurnMarksOnTheCatalogParts(t *testing.T) {
	want := map[string]float64{spacecraft.StageModuleF9S1ID: 1.0 / 9, "fh-boosters": 1.0 / 9, "packet-1": 0.4, "dragon-crew": 1, "dragon-cargo": 1}
	for id, lt := range want {
		st, ok := spacecraft.BuildModule(id)
		if !ok {
			t.Fatalf("no module %s", id)
		}
		if got := st[0].LandingThrottle; math.Abs(got-lt) > 1e-12 {
			t.Errorf("%s landing throttle %v, want %v", id, got, lt)
		}
	}
	for _, id := range []string{"lander", "csm", "f9-s2"} {
		if st, ok := spacecraft.BuildModule(id); ok && st[0].LandingThrottle != 0 {
			t.Errorf("%s is marked (%v), want unmarked", id, st[0].LandingThrottle)
		}
	}
	w, c := entryWorld(t, "", "earth", "Falcon-9-Dragon", 200e3, true)
	c.Stages = c.Stages[len(c.Stages)-1:]
	c.SyncFields()
	if c.LandingThrottle() != 1 {
		t.Fatalf("Dragon landing throttle %v, want 1", c.LandingThrottle())
	}
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Errorf("Dragon engage: %v, want engaged on the SuperDracos", err)
	}
	// sabotage: without the SuperDracos the same vessel refuses on thrust under weight
	c.Landing = nil
	c.Stages[0].SuperThrust, c.Stages[0].SuperIsp = 0, 0
	if err := w.EngageLandingAutopilot(); !errors.Is(err, ErrSiteHoldUnderWeight) {
		t.Errorf("Dragon without SuperDracos engage: %v, want %v", err, ErrSiteHoldUnderWeight)
	}
}

// ADR 0069 decision 6: a Dragon falling over Earth lands under N on the
// SuperDracos at under 10 m/s with its engine mode untouched, burning on the
// SuperDraco figures; the same fall without them cannot stop.
func TestDragonLandsOnSuperDracos(t *testing.T) {
	b := boosterFall{altM: 4600, downMps: 250, offN: 60, offE: -40, fuelKg: 2562}
	w, c, lc13 := fallWorld(t, "dragon-crew", b)
	noChute(c) // this pins the engine landing; the canopy has its own tests
	massBefore := c.TotalMass()
	if c.EngineMode != spacecraft.EngineMain {
		t.Fatalf("mode %v, want main", c.EngineMode)
	}
	v, d, peak, landed := flyBooster(t, w, c, lc13)
	t.Logf("Dragon: contact %.2f m/s, %.1f m from LZ-1, peak throttle %.3f, landed=%v, mass %.0f -> %.0f kg", v, d, peak, landed, massBefore, c.TotalMass())
	if !landed || v >= CrashVCritMps {
		t.Errorf("landed=%v contact %.2f m/s, want landed under %v", landed, v, CrashVCritMps)
	}
	if c.EngineMode != spacecraft.EngineMain {
		t.Errorf("mode %v after the landing, want main (N does not switch it)", c.EngineMode)
	}
	if c.Thrust != 1600 {
		t.Errorf("thrust %v after the landing, want the Dracos' 1600 back", c.Thrust)
	}
	// red without the SuperDracos
	w2, c2, lc := fallWorld(t, "dragon-crew", b)
	noChute(c2)
	c2.Stages[0].SuperThrust, c2.Stages[0].SuperIsp = 0, 0
	if _, err := w2.PlanDeorbitAndEngage(); err == nil {
		v2, _, _, landed2 := flyBooster2(w2, c2, lc)
		if landed2 && v2 < CrashVCritMps {
			t.Errorf("Dragon on its Dracos alone landed softly (%.2f m/s)", v2)
		}
	}
}

// noChute strips the canopy from c, leaving the engines.
func noChute(c *spacecraft.Spacecraft) {
	c.Stages[0].HasParachute = false
	c.SyncFields()
}

func flyBooster2(w *World, c *spacecraft.Spacecraft, lc13 Site) (speed, distM, peak float64, landed bool) {
	for i := 0; i < 20*60*4 && !c.Landed && !c.Crashed; i++ {
		speed = physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary).Norm()
		w.Tick()
	}
	return speed, 0, 0, c.Landed
}

// The profile at its edges, 100 m up and straight down: at exactly the contact
// speed the thrust is gravity's (the equal case: nothing left to shed), at 20 m/s
// it adds (v^2 - v_td^2) / 2h = 1.98 m/s^2, and under it the thrust is less than
// gravity so the vessel is let down to the contact speed.
func TestOneBurnProfileAtTheContactSpeed(t *testing.T) {
	for _, tc := range []struct{ down, wantExtra float64 }{
		{OneBurnContactMps, 0},
		{20, (400 - OneBurnContactMps*OneBurnContactMps) / 200},
		{1, -OneBurnLetDownGain}, // slower than the contact speed: let down to it, never hover
	} {
		w, c, _ := boosterWorld(t, boosterFall{altM: 4600, downMps: 250, fuelKg: 6500})
		if _, err := w.PlanDeorbitAndEngage(); err != nil {
			t.Fatal(err)
		}
		up := c.State.R.Unit()
		c.State.R = up.Scale(c.Primary.RadiusMeters() + 100)
		c.State.V = up.Scale(-tc.down).Add(physics.AtmosphereOmega(c.Primary).Cross(c.State.R))
		lat, lon, _, _ := w.apTarget(c)
		f := apFrameOf(c)
		_, theta := w.apOneBurn(c, c.Landing, f, lat, lon)
		if got, want := theta*f.aMax-f.g, tc.wantExtra; math.Abs(got-want) > 0.05 {
			t.Errorf("at %.1f m/s down the net thrust is %.3f m/s^2 over gravity, want %.3f", tc.down, got, want)
		}
	}
}

// dragonFromOrbit is a Dragon with its trunk dropped, in a circular orbit round
// body, 150 degrees of arc behind a flag, N not yet pressed.
func dragonFromOrbit(t *testing.T, body string, altM float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, c := entryWorld(t, "", body, "Dragon", altM, true)
	c.Stages = c.Stages[len(c.Stages)-1:]
	c.SyncFields()
	return w, c
}

// ADR 0069 decision 8 (T7): N on a Dragon over Mars runs arm, open, cut, land on
// the flag with no other key, at every warp. The deorbit burn is N's own and
// flies on the SuperDracos (seconds, not the Dracos' 17 minutes). The hold row
// reads `Site+ auto chute` under the canopy.
func TestNFliesTheChuteOnMars(t *testing.T) {
	for _, idx := range []int{0, 1, 2, 4} {
		w, c := dragonFromOrbit(t, "mars", 150e3)
		if _, err := w.PlanDeorbitAndEngage(); err != nil {
			t.Fatalf("warp %v: N: %v", WarpFactors[idx], err)
		}
		if c.ChuteState != spacecraft.ChuteArmed {
			t.Fatalf("warp %v: chute %v after N, want ARMED", WarpFactors[idx], c.ChuteState)
		}
		if d := c.Nodes[0].Duration; d > 10*time.Second {
			t.Errorf("warp %v: the deorbit burn is planned %v long, want seconds on the SuperDracos", WarpFactors[idx], d)
		}
		w.Clock.WarpIdx = idx
		seen := []spacecraft.ChuteState{c.ChuteState}
		hold, speed := "", 0.0
		for i := 0; i < 6*3600*20 && !c.Landed && !c.Crashed; i++ {
			speed = physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary).Norm()
			w.Tick()
			if c.ChuteState != seen[len(seen)-1] {
				seen = append(seen, c.ChuteState)
			}
			if c.ChuteState == spacecraft.ChuteDeployed && hold == "" {
				hold = w.siteHoldLabel(c)
			}
		}
		ev := w.PendingLandingEvents[len(w.PendingLandingEvents)-1]
		t.Logf("Mars Dragon warp %vx: chute %v, contact %.2f m/s, %.1f m from the flag", WarpFactors[idx], seen, speed, ev.DistanceM)
		if want := []spacecraft.ChuteState{spacecraft.ChuteArmed, spacecraft.ChuteDeployed, spacecraft.ChuteCut}; !reflect.DeepEqual(seen, want) {
			t.Errorf("warp %v: chute went %v, want %v", WarpFactors[idx], seen, want)
		}
		if !c.Landed || speed >= CrashVCritMps || ev.DistanceM > 43 {
			t.Errorf("warp %v: landed=%v contact %.2f m/s %.1f m from the flag, want landed under %v m/s inside the 43 m LZ circle", WarpFactors[idx], c.Landed, speed, ev.DistanceM, CrashVCritMps)
		}
		if hold != "Site+ auto chute" {
			t.Errorf("warp %v: hold row under the canopy %q, want %q", WarpFactors[idx], hold, "Site+ auto chute")
		}
	}
}

// N on a chute vessel with no landing engine (a CSM) plants the deorbit burn and stops
// there, as before: the chute stays stowed. The unmarked Ground-Station Lander
// has no chute and flies as it did.
func TestNLeavesVesselsWithoutLandingEnginesAlone(t *testing.T) {
	w, c := entryWorld(t, "", "earth", "Capsule", 200e3, true)
	c.Stages, _ = spacecraft.BuildModule("csm") // a chute and a 91 kN engine that cannot land
	c.SyncFields()
	plan, err := w.PlanDeorbitAndEngage()
	if plan == nil || !errors.Is(err, ErrSiteHoldUnderWeight) || len(c.Nodes) != 1 {
		t.Errorf("CSM: plan=%v err=%v nodes=%d, want the deorbit burn planted and the engage refused", plan, err, len(c.Nodes))
	}
	if c.ChuteState != spacecraft.ChuteStowed {
		t.Errorf("chute %v after N on a capsule with no landing engine, want STOWED", c.ChuteState)
	}
	w2, c2 := entryWorld(t, "", "mars", "Ground-Station-Lander", 150e3, true)
	if _, err := w2.PlanDeorbitAndEngage(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6*3600*20 && !c2.Landed && !c2.Crashed; i++ {
		w2.Tick()
	}
	if ev := w2.PendingLandingEvents[len(w2.PendingLandingEvents)-1]; !c2.Landed || ev.DistanceM > 43 || c2.ChuteState != spacecraft.ChuteStowed {
		t.Errorf("Ground-Station Lander: landed=%v %.1f m from the flag chute %v, want landed on the flag with no chute touched", c2.Landed, ev.DistanceM, c2.ChuteState)
	}
}

// The cut rule (ADR 0069 decision 8): under the canopy a one-burn vessel cuts
// when the cue is within the burn the aim error needs, and never when the tank
// cannot hover that long. The equal case cuts.
func TestChuteCutRuleBoundaries(t *testing.T) {
	w, c := dragonFromOrbit(t, "mars", 150e3)
	if _, err := w.PlanDeorbitAndEngage(); err != nil {
		t.Fatal(err)
	}
	c.ChuteState = spacecraft.ChuteDeployed
	f := apFrame{g: 3.7, vz: -36}
	rt := w.apRuntimeFor(c)
	rt.cueErrM = 100
	need := math.Sqrt(6 * 100 / (f.g * math.Tan(apMaxTiltRad)))
	var afford float64
	c.OnLandingBrake(func() {
		v := math.Max(-f.vz, OneBurnContactMps)
		afford = ChuteAimFuelFrac * c.Isp * stdGravityMps2 * c.ActiveStageFuel() / (c.TotalMass() * f.g) * (v + OneBurnContactMps) / (2 * v)
	})
	if need > afford {
		t.Fatalf("setup: need %.1f s over afford %.1f s", need, afford)
	}
	cut := func(in time.Duration) (r bool) {
		c.OnLandingBrake(func() { r = w.chuteAimCut(c, f, in) })
		return r
	}
	lead := time.Duration(need * float64(time.Second))
	if !cut(lead) { // the equal case
		t.Errorf("did not cut %v from the cue with exactly that needed", lead)
	}
	if cut(lead + time.Nanosecond) {
		t.Errorf("cut %v from the cue with %v needed", lead+time.Nanosecond, lead)
	}
	if !cut(0) {
		t.Error("did not cut at the cue itself")
	}
	rt.cueErrM = 1e6 // an error the tank cannot hover out
	if cut(0) {
		t.Error("cut to steer an error the tank cannot afford")
	}
	c.ChuteState = spacecraft.ChuteArmed
	rt.cueErrM = 100
	if cut(0) {
		t.Error("cut a chute that has not opened")
	}
}

// ADR 0069 slice 9: N lands a Dragon inside the 43 m LZ circle at every warp,
// on Earth with and without the canopy and on Mars, from orbit, and over LC-13
// from 4.6 km at 250 m/s and 72 m off; the tank is never run dry. Before the
// entry stopped depending on the tick it was 678 m (Earth at 1x), 150-196 m
// (engines only) and 67 m (the LC-13 fall).
func TestDragonLandsInsideTheCircleAtEveryWarp(t *testing.T) {
	const circle = 43.0 // equal case: 43 m is inside
	check := func(label string, w *World, c *spacecraft.Spacecraft) {
		t.Helper()
		for i := 0; i < 6*3600*20 && !c.Landed && !c.Crashed; i++ {
			w.Tick()
		}
		d := w.PendingLandingEvents[len(w.PendingLandingEvents)-1].DistanceM
		fuel := c.ActiveStageFuel()
		t.Logf("%s: landed=%v %.1f m from the flag, fuel left %.0f kg", label, c.Landed, d, fuel)
		if !c.Landed || d > circle || fuel <= 0 {
			t.Errorf("%s: landed=%v %.1f m from the flag (want within %v m), fuel left %.0f kg", label, c.Landed, d, circle, fuel)
		}
	}
	for idx, f := range WarpFactors {
		for _, tc := range []struct {
			body  string
			chute bool
		}{{"earth", true}, {"earth", false}, {"mars", true}} {
			w, c := dragonFromOrbit(t, tc.body, 200e3)
			if !tc.chute {
				noChute(c)
			}
			if _, err := w.PlanDeorbitAndEngage(); err != nil {
				t.Fatalf("%s chute=%v %vx: N: %v", tc.body, tc.chute, f, err)
			}
			w.Clock.WarpIdx = idx
			check(fmt.Sprintf("%s chute=%v %vx", tc.body, tc.chute, f), w, c)
		}
		w, c, _ := fallWorld(t, "dragon-crew", boosterFall{altM: 4600, downMps: 250, offN: 60, offE: -40, fuelKg: 2562})
		noChute(c)
		if plan, err := w.PlanDeorbitAndEngage(); err != nil || plan != nil {
			t.Fatalf("LC-13 fall %vx: N: plan=%v err=%v", f, plan, err)
		}
		w.Clock.WarpIdx = idx
		check(fmt.Sprintf("LC-13 fall %vx", f), w, c)
	}
}
