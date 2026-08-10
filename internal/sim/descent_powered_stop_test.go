// Package sim — tests for the integrated stop-burn forecast (issue
// #377): PredictPoweredStop, PredictBurnAt, and DeriveMarginState. See
// descent_test.go for the ballistic-forecast (PredictImpact) and
// ComputeBurnMargin arithmetic tests this file complements.

package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// --- Analytic verification -------------------------------------------
//
// A purely vertical powered stop under constant g, zero drag, and the
// ideal (Tsiolkovsky) rocket equation has a closed form:
//
//	ds/dt = g - F/m(t),   m(t) = m0 - mdot·t,   mdot = F/(Isp·g0)
//
// Integrating once (∫F/m(t)dt = Isp·g0·ln(m0/m(t)), the rocket
// equation's own Δv):
//
//	s(t) = s0 + g·t - Isp·g0·ln(a/(a-t)),   a = m0/mdot
//
// and integrating again for the distance fallen:
//
//	d(t) = s0·t + g·t²/2 - Isp·g0·[t·ln(a) + t - a·ln(a) + (a-t)·ln(a-t)]
//
// Both are independent of PredictPoweredStop's own RK4 integration —
// this is the analytic cross-check issue #377 asks for, not a
// self-consistency test against the production code path.

func analyticStopSpeed(t, s0, g, ispg0, a float64) float64 {
	return s0 + g*t - ispg0*math.Log(a/(a-t))
}

func analyticStopDistance(t, s0, g, ispg0, a float64) float64 {
	bracket := t*math.Log(a) + t - a*math.Log(a) + (a-t)*math.Log(a-t)
	return s0*t + g*t*t/2 - ispg0*bracket
}

// analyticStopTime bisects analyticStopSpeed for its zero crossing in
// (0, a) — the closed-form time to null s0 under constant g and the
// ideal rocket equation.
func analyticStopTime(t *testing.T, s0, g, ispg0, a float64) float64 {
	t.Helper()
	lo, hi := 0.0, a*0.999 // stay strictly short of exhausting all mass
	if analyticStopSpeed(hi, s0, g, ispg0, a) > 0 {
		t.Fatalf("setup: even burning to %.1f s (~all mass) doesn't null %.1f m/s — pick a stronger engine", hi, s0)
	}
	for i := 0; i < 100; i++ {
		mid := (lo + hi) / 2
		if analyticStopSpeed(mid, s0, g, ispg0, a) > 0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// hugeRadiusAirlessBody is a synthetic primary for the analytic case: a
// huge radius (g changes ~1e-5 relative over a few-km descent — constant
// to any tolerance this test cares about), no atmosphere (zero drag,
// matching physics.DragAccel's nil-Atmosphere short-circuit), and no
// rotation (SideralRotation/SideralOrbit left zero, so
// physics.AtmosphereOmega returns the zero vector and
// AirRelativeVelocity is exactly the inertial velocity — no incidental
// horizontal component from body spin to break the "purely vertical"
// idealisation).
func hugeRadiusAirlessBody(desiredGMps2 float64) bodies.CelestialBody {
	const radiusM = 1.0e9
	mu := desiredGMps2 * radiusM * radiusM
	massKg := mu / bodies.G
	return bodies.CelestialBody{
		ID:         "analytic-test-body",
		MeanRadius: radiusM / 1000.0,
		Mass:       bodies.Mass{Value: massKg / 1e28, Exponent: 28},
	}
}

// TestPredictPoweredStopMatchesAnalyticConstantGCase is issue #377's
// required analytic cross-check: constant g, no drag, ideal rocket
// equation, purely vertical (so the closed form applies exactly — no
// horizontal component for the integrator to spend accel on that the
// 1-D analytic model doesn't have). PredictPoweredStop's RK4 integration
// must land within a tight tolerance of the closed-form time and
// distance, independently computed above.
func TestPredictPoweredStopMatchesAnalyticConstantGCase(t *testing.T) {
	const (
		g       = 1.62 // moon-like, but on a body large enough to hold ~constant
		thrustN = 40_000.0
		ispSec  = 300.0
		massKg0 = 10_000.0
		fuelKg  = 5_000.0
		v0      = 100.0   // m/s, straight down
		altM    = 5_000.0 // start altitude
	)
	body := hugeRadiusAirlessBody(g)
	gotG := body.GravitationalParameter() / (body.RadiusMeters() * body.RadiusMeters())

	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Primary = body
	c.Landed = false
	c.Crashed = false
	c.Stages = nil // flat DryMass/Fuel/Thrust/Isp fields govern, not a stale stage
	c.Thrust = thrustN
	c.Isp = ispSec
	c.DryMass = massKg0 - fuelKg
	c.Fuel = fuelKg
	c.Monoprop = 0
	c.State.R = orbital.Vec3{X: body.RadiusMeters() + altM}
	c.State.V = orbital.Vec3{X: -v0}
	c.State.M = c.TotalMass()

	got, ok := PredictPoweredStop(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("PredictPoweredStop refused the analytic case")
	}
	if got.Outcome != StopStopped {
		t.Fatalf("Outcome = %v, want StopStopped", got.Outcome)
	}

	g0 := stdGravityMps2
	ispg0 := ispSec * g0
	mdot := thrustN / ispg0
	a := massKg0 / mdot

	wantT := analyticStopTime(t, v0, gotG, ispg0, a)
	wantDist := analyticStopDistance(wantT, v0, gotG, ispg0, a)
	wantMargin := altM - wantDist

	if diff := math.Abs(got.ElapsedSec - wantT); diff > 0.01*wantT {
		t.Errorf("ElapsedSec = %.3f s, want %.3f s (analytic, ±1%%)", got.ElapsedSec, wantT)
	}
	if diff := math.Abs(got.MarginM - wantMargin); diff > 0.01*altM {
		t.Errorf("MarginM = %.1f m, want %.1f m (analytic, ±1%% of start altitude)", got.MarginM, wantMargin)
	}
}

// --- The #377 regression --------------------------------------------

// TestPredictPoweredStopLunaHorizontalRegression is the regression that
// proves the point: a stressed lunar lander (TWR 1.1 at the Moon's own
// g — thin margin over hover, but nowhere near "obviously can't fly",
// Isp 311 s; not the repo's oversized default seed craft) carrying
// 1600 m/s of HORIZONTAL speed plus a mundane 41 m/s vertical descent
// rate at 100 km altitude.
//
// BEFORE (today's frozen-scalar ComputeBurnMargin, which only ever sees
// the VERTICAL descent rate): the ratio is enormous — the model has no
// idea 1.6 km/s of horizontal speed exists at all, so it reads
// comfortably OK.
//
// AFTER (PredictPoweredStop, integrating the real burn): pointing
// retrograde against the TRUE (mostly horizontal) velocity spends most
// of the burn's authority fighting the horizontal component while
// gravity keeps pulling the craft down almost unopposed — it crashes,
// still carrying most of its speed. (A stronger lander CAN kill 1.6 km/s
// inside 100 km once the real vector math is run — TWR 1.3+ at this
// altitude/rate stops clean per this file's own sweep — which is exactly
// why hand-derived scalar corrections are the wrong tool here: whether
// this specific craft can stop depends on the coupled 3-D burn, not a
// ratio.)
//
// The exact numbers are pinned below (computed, not hand-derived) so a
// future change to either model has to explain why they moved.
func TestPredictPoweredStopLunaHorizontalRegression(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Primary = bodyForTest(t, w, "moon")
	c.Landed = false
	c.Crashed = false
	c.Stages = nil

	moonG := c.Primary.GravitationalParameter() / (c.Primary.RadiusMeters() * c.Primary.RadiusMeters())
	const (
		massKg0    = 15_000.0
		fuelKg     = 10_000.0
		ispSec     = 311.0
		twrAtMoonG = 1.1
		vertMps    = 41.0
		horizMps   = 1604.0
		altM       = 100_000.0
	)
	thrustN := twrAtMoonG * moonG * massKg0

	c.Thrust = thrustN
	c.Isp = ispSec
	c.DryMass = massKg0 - fuelKg
	c.Fuel = fuelKg
	c.Monoprop = 0
	c.State.R = orbital.Vec3{X: c.Primary.RadiusMeters() + altM}
	c.State.V = orbital.Vec3{X: -vertMps, Y: horizMps}
	c.State.M = c.TotalMass()

	// BEFORE: the frozen-scalar model, fed only the vertical rate — the
	// exact call DescentCorridorFor used to make (pre-#377).
	before := ComputeBurnMargin(c.Thrust, c.TotalMass(), moonG, vertMps, altM, c.RemainingDeltaV())
	if before.State != MarginOK {
		t.Fatalf("setup: want the OLD model to call this comfortably OK (that's the point of the regression), got State=%v Ratio=%.2f",
			before.State, before.Ratio)
	}
	if before.Ratio < 5 {
		t.Fatalf("setup: want the OLD model's ratio to read \"comfortably\" OK (≥5), got %.2f — scenario isn't dramatic enough", before.Ratio)
	}
	t.Logf("BEFORE (frozen-scalar, vertical-only): Ratio=%.2f State=%v — comfortably OK, blind to the %.0f m/s horizontal component",
		before.Ratio, before.State, horizMps)

	// AFTER: integrate the real burn.
	after, ok := PredictPoweredStop(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("PredictPoweredStop refused — want a definite Crashed verdict")
	}
	if after.Outcome != StopCrashed {
		t.Fatalf("Outcome = %v, want StopCrashed — a 2x-TWR lander cannot kill %.0f m/s of mostly-horizontal speed inside %.0f km",
			after.Outcome, horizMps, altM/1000)
	}
	t.Logf("AFTER (integrated): Outcome=%v MarginM=%.0f (short by %.0f m) ImpactSpeedMps=%.0f ElapsedSec=%.1f",
		after.Outcome, after.MarginM, -after.MarginM, after.ImpactSpeedMps, after.ElapsedSec)

	if after.MarginM >= 0 {
		t.Errorf("MarginM = %.1f, want negative (short-by, not a survived stop)", after.MarginM)
	}
	// The alarm chain: this reads CAN'T STOP / thrust, not the old
	// model's comfortable multi-x ratio.
	margin := DeriveMarginState(after, ok, altM, BurnAtCue{}, false)
	if margin.State != MarginInsufficient {
		t.Errorf("DeriveMarginState = %v, want MarginInsufficient", margin.State)
	}
	if margin.Limiter != LimitThrust {
		t.Errorf("Limiter = %v, want thrust (ran into the ground, not out of propellant)", margin.Limiter)
	}
}

// --- Responsiveness to thrust -----------------------------------------

// TestPredictPoweredStopMarginRespondsToSpeedShed mirrors
// TestPredictImpactShiftsUnderThrust's shape for the new forecast: full-
// throttle retrograde thrust bleeds surface-relative speed over time,
// and the stop margin has to visibly IMPROVE in response — not just
// passively shrink as altitude is spent — the same "responds to thrust"
// property the impact marker already has (issue #377 acceptance: "Burning
// full throttle makes the stop margin stop shrinking").
func TestPredictPoweredStopMarginRespondsToSpeedShed(t *testing.T) {
	_, c := dropTestCraft(t, 50_000)
	c.Thrust = 30_000
	c.Isp = 300
	c.DryMass, c.Fuel, c.Monoprop = 4_000, 6_000, 0
	c.Stages = nil
	c.State.V = orbital.Vec3{X: -50, Y: 900}
	c.State.M = c.TotalMass()

	before, ok := PredictPoweredStop(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("PredictPoweredStop refused before the speed was shed")
	}

	// The same thing full-throttle retrograde thrust does over time:
	// less surface-relative speed to kill. Mass is held fixed so the
	// improvement can't be credited to the (also-real, but separate)
	// mass-loss effect — isolating the velocity term.
	c.State.V = orbital.Vec3{X: -50, Y: 600}
	after, ok := PredictPoweredStop(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("PredictPoweredStop refused after the speed was shed")
	}

	if after.MarginM <= before.MarginM {
		t.Errorf("MarginM did not improve after shedding 300 m/s of horizontal speed: before=%.0f after=%.0f",
			before.MarginM, after.MarginM)
	}
}

// TestDeriveMarginStateBands pins the OK / TIGHT / CAN'T STOP mapping
// (issue #377 §4) against synthetic PoweredStopPrediction values, so the
// alarm ladder's thresholds are asserted directly rather than only
// through an end-to-end integration.
func TestDeriveMarginStateBands(t *testing.T) {
	const altitudeM = 10_000.0
	cases := []struct {
		name    string
		stop    PoweredStopPrediction
		stopOK  bool
		burnAt  BurnAtCue
		hasBA   bool
		want    MarginState
		limiter MarginLimiter
	}{
		{"stopped, comfortable margin", PoweredStopPrediction{Outcome: StopStopped, MarginM: 5000}, true, BurnAtCue{}, false, MarginOK, LimitNone},
		{"stopped, thin margin (<10% altitude)", PoweredStopPrediction{Outcome: StopStopped, MarginM: 500}, true, BurnAtCue{}, false, MarginTight, LimitNone},
		{"stopped, comfortable margin but imminent burn-at", PoweredStopPrediction{Outcome: StopStopped, MarginM: 5000}, true, BurnAtCue{InSec: 2}, true, MarginTight, LimitNone},
		{"crashed", PoweredStopPrediction{Outcome: StopCrashed, MarginM: -200}, true, BurnAtCue{}, false, MarginInsufficient, LimitThrust},
		{"fuel-limited", PoweredStopPrediction{Outcome: StopFuelLimited, MarginM: 3000}, true, BurnAtCue{}, false, MarginInsufficient, LimitFuel},
		{"undetermined (step cap)", PoweredStopPrediction{}, false, BurnAtCue{}, false, MarginInsufficient, LimitThrust},
	}
	for _, c := range cases {
		got := DeriveMarginState(c.stop, c.stopOK, altitudeM, c.burnAt, c.hasBA)
		if got.State != c.want {
			t.Errorf("%s: State = %v, want %v", c.name, got.State, c.want)
		}
		if got.Limiter != c.limiter {
			t.Errorf("%s: Limiter = %v, want %v", c.name, got.Limiter, c.limiter)
		}
	}
}

// --- burn at -----------------------------------------------------------

// TestPredictBurnAtFindsALaterSafeStart: a craft with plenty of stopping
// capability right now (comfortable margin) should have a burn-at cue
// LATER than "now" — there's room to wait — and the cued altitude must
// sit strictly below the craft's current altitude on the same ballistic
// coast (it's a point further along the fall).
func TestPredictBurnAtFindsALaterSafeStart(t *testing.T) {
	_, c := dropTestCraft(t, 50_000)
	c.Thrust = 60_000
	c.Isp = 311
	c.DryMass, c.Fuel, c.Monoprop = 5_000, 10_000, 0
	c.Stages = nil
	c.State.V = orbital.Vec3{X: -20, Y: 300}
	c.State.M = c.TotalMass()

	cue, ok := PredictBurnAt(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("PredictBurnAt found no safe future start for a comfortably-stoppable descent")
	}
	if cue.InSec <= 0 {
		t.Errorf("InSec = %.2f, want > 0 (a later moment than now)", cue.InSec)
	}
	if cue.AltitudeM >= 50_000 {
		t.Errorf("AltitudeM = %.0f, want below the craft's current 50000 m altitude", cue.AltitudeM)
	}
}

// TestPredictBurnAtHiddenWhenAlreadyUnstoppable: once the current
// instant is already unstoppable, PredictBurnAt reports ok=false — the
// corridor's own CAN'T STOP alarm already carries the message, and a
// "burn at Xs ago" row would only muddy it.
func TestPredictBurnAtHiddenWhenAlreadyUnstoppable(t *testing.T) {
	_, c := dropTestCraft(t, 500)
	c.Thrust = 0
	c.Isp = 0
	c.State.V = orbital.Vec3{X: -300}

	if _, ok := PredictBurnAt(c, DescentPredictHorizon); ok {
		t.Error("PredictBurnAt returned a cue for an already-unstoppable descent, want ok=false")
	}
}

// --- Step-size clamp (PR #382 review finding 2) -----------------------

// TestStopStepSizeSecondsNeverExceedsFuelCap is the review's finding 2
// regression: the adaptive-shrink floor (stopAdaptiveMinDt) must never
// widen a step back out past the fuel-exhaustion cap
// (fuelTimeLeftSec = fuelKg/mdot). The reviewer's own reproduction —
// ttx = 0.002, and an adaptive cap that would compute to 0.001 before
// the floor raises it to stopAdaptiveMinDt (0.01) — is pinned directly:
// speedPre/aAvail is chosen so stopAdaptiveShrinkFrac*speedPre/aAvail =
// 0.001 (below the 0.01 floor), with fuelTimeLeftSec tighter still at
// 0.002. A step that size integrates full thrust (poweredStopAccel)
// for 5x longer than the tank actually sustains it — mass bookkeeping
// stays correct (burned is separately clamped to fuelKg in the caller),
// but the IMPULSE the RK4 step applies would not be.
func TestStopStepSizeSecondsNeverExceedsFuelCap(t *testing.T) {
	const (
		dt              = 1.0
		fuelTimeLeftSec = 0.002 // ttx: the tank runs dry after 2 ms at full thrust
		speedPre        = 0.002
		aAvail          = 1.0 // stopAdaptiveShrinkFrac(0.5)*speedPre/aAvail = 0.001, below stopAdaptiveMinDt
	)
	got := stopStepSizeSeconds(dt, fuelTimeLeftSec, speedPre, aAvail)
	if got > fuelTimeLeftSec {
		t.Errorf("stopStepSizeSeconds = %v, want <= fuelTimeLeftSec (%v) — the adaptive floor widened the step past what the tank sustains",
			got, fuelTimeLeftSec)
	}
	if got != fuelTimeLeftSec {
		t.Errorf("stopStepSizeSeconds = %v, want exactly fuelTimeLeftSec (%v): the fuel cap is the tightest bound here", got, fuelTimeLeftSec)
	}
}

// TestStopStepSizeSecondsAdaptiveShrinkStillWorks: the floor-raise fix
// must not defeat the adaptive shrink's actual job — when fuel ISN'T the
// binding constraint, a step near the stop-speed floor still shrinks
// (down to stopAdaptiveMinDt when the raw computation would go smaller
// still), so the loop keeps converging instead of overshooting.
func TestStopStepSizeSecondsAdaptiveShrinkStillWorks(t *testing.T) {
	const (
		dt              = 1.0
		fuelTimeLeftSec = 1000.0 // ample — not the binding constraint
	)
	// Raw adaptive computation (0.5*speedPre/aAvail) undershoots the
	// floor: the floor must win, and nothing here should widen it back
	// toward fuelTimeLeftSec or dt.
	if got := stopStepSizeSeconds(dt, fuelTimeLeftSec, 0.0001, 1.0); got != stopAdaptiveMinDt {
		t.Errorf("stopStepSizeSeconds = %v, want the floor %v (fuel is not binding here)", got, stopAdaptiveMinDt)
	}
	// Raw adaptive computation comfortably clears the floor: use it as-is.
	if got, want := stopStepSizeSeconds(dt, fuelTimeLeftSec, 1.0, 1.0), 0.5; got != want {
		t.Errorf("stopStepSizeSeconds = %v, want %v (plain adaptive shrink, no floor involved)", got, want)
	}
	// Neither cap engages: full dt.
	if got := stopStepSizeSeconds(dt, fuelTimeLeftSec, 1000.0, 1.0); got != dt {
		t.Errorf("stopStepSizeSeconds = %v, want dt (%v) — neither cap should have engaged", got, dt)
	}
}
