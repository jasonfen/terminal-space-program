// Package sim — descent-half tests (issue #348 §3 / ADR 0043): the
// ground-contact forecast, its live response to a burn, and the burn-
// margin arithmetic the surface view alarms on.

package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// bodyForTest fetches a catalog body by ID from the world's system.
func bodyForTest(t *testing.T, w *World, id string) bodies.CelestialBody {
	t.Helper()
	for _, b := range w.System().Bodies {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("body %q not in the loaded system", id)
	return bodies.CelestialBody{}
}

// dropTestCraft parks the world's active craft `altM` above the Moon's
// surface at rest in the inertial frame — an airless primary, so drag is
// out of the picture and the fall is a pure two-body radial free-fall
// with a closed-form answer.
func dropTestCraft(t *testing.T, altM float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	c.Primary = bodyForTest(t, w, "moon")
	c.Landed = false
	c.Crashed = false
	c.State.R = orbital.Vec3{X: c.Primary.RadiusMeters() + altM}
	c.State.V = orbital.Vec3{}
	c.State.M = c.TotalMass()
	return w, c
}

// radialFallSeconds is the closed-form two-body radial free-fall time
// from rest at r0 down to r1 (Kepler's rectilinear ellipse):
//
//	t = √(r0³/2μ) · [ arccos√(r1/r0) + √( (r1/r0)(1 − r1/r0) ) ]
//
// The reference the integrated forecast is scored against.
func radialFallSeconds(r0, r1, mu float64) float64 {
	x := r1 / r0
	return math.Sqrt(r0*r0*r0/(2*mu)) * (math.Acos(math.Sqrt(x)) + math.Sqrt(x*(1-x)))
}

// TestPredictImpactBallisticRadialDrop: the analytic case. A vessel
// dropped from rest 10 km over the Moon must impact straight below
// where it started (the acceleration is purely radial, so the fall
// never leaves the r̂ line), exactly on the mean radius, at the
// closed-form radial free-fall time.
func TestPredictImpactBallisticRadialDrop(t *testing.T) {
	const altM = 10_000.0
	_, c := dropTestCraft(t, altM)
	radius := c.Primary.RadiusMeters()
	mu := c.Primary.GravitationalParameter()

	imp, ok := PredictImpact(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("PredictImpact found no ground contact for a vessel dropped from rest")
	}

	// Straight down: same direction as the start, no lateral component.
	if math.Abs(imp.Point.Y) > 1 || math.Abs(imp.Point.Z) > 1 {
		t.Errorf("impact point %v drifted off the radial line (want Y=Z=0)", imp.Point)
	}
	if got := imp.Point.Norm(); math.Abs(got-radius) > 1e-6*radius {
		t.Errorf("impact point |R| = %.3f m, want the mean radius %.3f m", got, radius)
	}

	want := radialFallSeconds(radius+altM, radius, mu)
	got := imp.TimeToImpact.Seconds()
	if math.Abs(got-want) > 0.01*want {
		t.Errorf("time to impact = %.2f s, want %.2f s (±1%%)", got, want)
	}

	// Impact speed from energy: v = √(2μ(1/r₁ − 1/r₀)). The Moon's spin
	// contributes ~4 m/s of co-rotation at the equator, so score the
	// surface-relative speed loosely against the inertial closed form.
	wantV := math.Sqrt(2 * mu * (1/radius - 1/(radius+altM)))
	if math.Abs(imp.SpeedMps-wantV) > 10 {
		t.Errorf("impact speed = %.1f m/s, want ≈ %.1f m/s", imp.SpeedMps, wantV)
	}

	// The drawn arc starts where the craft is and ends where it lands.
	if len(imp.Path) < 2 {
		t.Fatalf("path has %d points, want a drawable run", len(imp.Path))
	}
	if imp.Path[0] != c.State.R {
		t.Errorf("path[0] = %v, want the craft's current position %v", imp.Path[0], c.State.R)
	}
	if imp.Path[len(imp.Path)-1] != imp.Point {
		t.Errorf("path ends at %v, want the impact point %v", imp.Path[len(imp.Path)-1], imp.Point)
	}
}

// TestPredictImpactShiftsUnderThrust: the forecast is taken from the
// LIVE state, so a burn moves it. A lateral Δv walks the impact point
// downrange; an upward Δv buys flight time. This is what makes the
// ground marker track a powered descent instead of standing still.
func TestPredictImpactShiftsUnderThrust(t *testing.T) {
	const altM = 10_000.0
	_, c := dropTestCraft(t, altM)
	base, ok := PredictImpact(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("baseline drop found no ground contact")
	}

	// Lateral burn: +Y, orthogonal to the +X radial. The contact point
	// must slide that way along the surface.
	c.State.V = orbital.Vec3{Y: 50}
	lateral, ok := PredictImpact(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("lateral-burn state found no ground contact")
	}
	if lateral.Point.Y <= base.Point.Y {
		t.Errorf("impact point Y %.1f did not move downrange of the ballistic %.1f after a +Y burn",
			lateral.Point.Y, base.Point.Y)
	}
	if lateral.Point.Sub(base.Point).Norm() < 100 {
		t.Errorf("impact point moved only %.1f m under a 50 m/s lateral burn — marker would look frozen",
			lateral.Point.Sub(base.Point).Norm())
	}

	// Retro (upward) burn: same place-ish, but later.
	c.State.V = orbital.Vec3{X: 40}
	up, ok := PredictImpact(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("upward-burn state found no ground contact")
	}
	if up.TimeToImpact <= base.TimeToImpact {
		t.Errorf("time to impact %v did not grow past the ballistic %v after an upward burn",
			up.TimeToImpact, base.TimeToImpact)
	}
}

// TestPredictImpactNoneForStableOrbit: a circular parking orbit never
// reaches the ground, so the forecast reports nothing rather than
// inventing a contact at the horizon's edge.
func TestPredictImpactNoneForStableOrbit(t *testing.T) {
	_, c := dropTestCraft(t, 100_000)
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	if imp, ok := PredictImpact(c, DescentPredictHorizon); ok {
		t.Errorf("circular orbit reported ground contact at %v (t+%v)", imp.Point, imp.TimeToImpact)
	}
}

// TestDescentCorridorForGating: the corridor is gated on the FORECAST,
// not on a phase label — it stands up for a falling vessel that reaches
// the ground inside the horizon, and stands down for one that is
// climbing, landed, or in a stable orbit.
func TestDescentCorridorForGating(t *testing.T) {
	_, c := dropTestCraft(t, 10_000)
	// Already falling: a vessel exactly at rest in the INERTIAL frame has
	// no radial rate at all (its air-relative velocity is the pure
	// horizontal −ω×r), which is the stand-down case, not the descent one.
	c.State.V = orbital.Vec3{X: -5}
	dc, ok := DescentCorridorFor(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("falling vessel: corridor stood down, want instruments")
	}
	if math.Abs(dc.AltitudeM-10_000) > 1 {
		t.Errorf("AltitudeM = %.1f, want 10000", dc.AltitudeM)
	}
	if dc.DescentRateMps <= 0 {
		t.Errorf("DescentRateMps = %.3f, want a positive (falling) rate", dc.DescentRateMps)
	}
	if dc.Impact.TimeToImpact <= 0 {
		t.Errorf("TimeToImpact = %v, want positive", dc.Impact.TimeToImpact)
	}

	// Climbing: the same trajectory run upward.
	c.State.V = orbital.Vec3{X: 200}
	if _, ok := DescentCorridorFor(c, DescentPredictHorizon); ok {
		t.Error("climbing vessel: corridor stood up, want stand-down")
	}

	// Landed: no descent to instrument.
	c.State.V = orbital.Vec3{}
	c.Landed = true
	if _, ok := DescentCorridorFor(c, DescentPredictHorizon); ok {
		t.Error("landed vessel: corridor stood up, want stand-down")
	}
	c.Landed = false

	// Stable circular orbit: falling nowhere.
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	if _, ok := DescentCorridorFor(c, DescentPredictHorizon); ok {
		t.Error("circular orbit: corridor stood up, want stand-down")
	}
}

// TestDescentCorridorAlarmsWhenItCannotStop: the end-to-end alarm. A
// vessel falling fast with very little altitude left, thrust or not,
// integrates to MarginInsufficient via DeriveMarginState — the state the
// surface view paints red. Issue #377 moved the corridor's Margin off
// the old frozen scalars, so this now drives the alarm chain
// through PredictPoweredStop + DeriveMarginState directly rather than via
// DescentCorridorFor (which no longer computes Margin at all — see its
// doc comment).
func TestDescentCorridorAlarmsWhenItCannotStop(t *testing.T) {
	_, c := dropTestCraft(t, 500)
	c.State.V = orbital.Vec3{X: -300} // 300 m/s straight down, 500 m to go
	c.Thrust = 0                      // no engine at all: can't even begin a stop
	c.Isp = 0

	dc, ok := DescentCorridorFor(c, DescentPredictHorizon)
	if !ok {
		t.Fatal("corridor stood down for a 300 m/s descent 500 m up")
	}
	if dc.Impact.TimeToImpact > 3*time.Second {
		t.Errorf("TimeToImpact = %v, want under ~2 s at 300 m/s from 500 m", dc.Impact.TimeToImpact)
	}

	stop, stopOK := PredictPoweredStop(c, DescentPredictHorizon)
	if !stopOK {
		t.Fatal("PredictPoweredStop refused for a thrustless craft — want StopFuelLimited, not StopUndetermined")
	}
	if stop.Outcome != StopFuelLimited {
		t.Errorf("Outcome = %v, want StopFuelLimited (no engine at all)", stop.Outcome)
	}
	margin := DeriveMarginState(stop, stopOK, dc.AltitudeM, BurnAtCue{}, false)
	if margin.State != MarginInsufficient {
		t.Errorf("Margin.State = %v, want MarginInsufficient", margin.State)
	}
	if margin.Limiter != LimitFuel {
		t.Errorf("Limiter = %v, want fuel (no engine at all reads as the fuel side binding)", margin.Limiter)
	}
}

// fallGroundError lets c fall with no thrust and returns, in metres on the
// ground, the distance between the forecast's ground-frame impact point (taken
// now) and where the vessel really touches down, plus the gap between the
// inertial and ground-frame forecast points (what the old arc drew wrong).
func fallGroundError(t *testing.T, w *World, c *spacecraft.Spacecraft) (miss, inertialGap float64) {
	t.Helper()
	imp, ok := PredictImpact(c, 3*time.Hour)
	if !ok {
		t.Fatal("no impact forecast")
	}
	t0 := w.Clock.SimTime
	for i := 0; i < 6*3600*20 && !c.Landed && !c.Crashed && c.State.R.Norm() > c.Primary.RadiusMeters(); i++ {
		w.Tick()
	}
	radius := c.Primary.RadiusMeters()
	if c.State.R.Norm() > radius+1e3 && !c.Crashed && !c.Landed {
		t.Fatalf("never reached the ground, alt %.0f m", c.State.R.Norm()-radius)
	}
	fLat, fLon := w.surfaceLatLon(imp.GroundPoint, c, t0)
	iLat, iLon := w.surfaceLatLon(imp.Point, c, t0)
	aLat, aLon := w.surfaceLatLon(c.State.R, c, w.Clock.SimTime)
	return HaversineM(fLat, fLon, aLat, aLon, radius), HaversineM(fLat, fLon, iLat, iLon, radius)
}

// The V view draws the forecast against today's ground, so the impact point
// must be where the turning body brings the ground to meet the vessel. Mars
// spins 241 m/s at 5N: 14 km of lag per minute of fall, red with unspin removed.
// 3 km on Mars (6 km on the Moon, where the ground-frame correction is 14 km of
// a 19 km miss) allows the forecast's own integrator error over a 30-50 minute
// fall; the rotation is the rest.
func TestPredictImpactGroundFrameMatchesTouchdown(t *testing.T) {
	for _, f := range []struct {
		body       string
		air        bool
		tol        float64
		minInertia float64 // inertial point lags by at least this
	}{
		{"mars", true, 3e3, 100e3},
		{"moon", false, 6e3, 10e3},
	} {
		w, c := entryWorld(t, "", f.body, "Ground-Station-Lander", 150e3, f.air)
		grazing(c)
		miss, gap := fallGroundError(t, w, c)
		t.Logf("%s: ground-frame miss %.0f m, inertial-vs-ground gap %.0f m", f.body, miss, gap)
		if miss > f.tol {
			t.Errorf("%s: ground-frame impact misses touchdown by %.0f m, want <= %.0f", f.body, miss, f.tol)
		}
		if gap < f.minInertia {
			t.Errorf("%s: inertial point only %.0f m from the ground-frame one, want >= %.0f", f.body, gap, f.minInertia)
		}
	}
}

// A body that does not spin draws the inertial path unchanged.
func TestUnspinNoSpinIsIdentity(t *testing.T) {
	p := orbital.Vec3{X: 1e6, Y: 2e6, Z: 3e6}
	if got := unspin(p, bodies.CelestialBody{}, 1234); got != p {
		t.Errorf("unspun %v, want %v", got, p)
	}
}

// ADR 0069 decision 8: the forecast of a vessel that works its chute flies the
// canopy below the opening height (a stowed one falls bare), and a fall shorter
// than one forecast sub-step still has a cue to light at.
func TestForecastFliesTheArmedChuteAndFindsAShortCue(t *testing.T) {
	b := boosterFall{altM: 1500, downMps: 60, fuelKg: 2562}
	for state, bare := range map[spacecraft.ChuteState]bool{spacecraft.ChuteStowed: true, spacecraft.ChuteArmed: false} {
		_, c, _ := fallWorld(t, "dragon-crew", b)
		c.ChuteState = state
		imp, ok := PredictImpact(c, 10*time.Minute)
		if !ok {
			t.Fatalf("chute %v: no impact", state)
		}
		if bare != (imp.SpeedMps > 30) {
			t.Errorf("chute %v: impact at %.1f m/s, want bare=%v (over 30) or the canopy's few m/s", state, imp.SpeedMps, bare)
		}
	}
	_, c, _ := fallWorld(t, "dragon-crew", boosterFall{altM: 20, downMps: 20, fuelKg: 2562})
	noChute(c)
	var ground, ok bool
	c.OnLandingBrake(func() { _, ground, ok = burnAtCue(c, DescentPredictHorizon, brakeThrottle(c), burnAtRefineIters) })
	if !ground || !ok {
		t.Errorf("a 20 m fall at 20 m/s the SuperDracos can stop: ground=%v ok=%v, want a cue to light at", ground, ok)
	}
}
