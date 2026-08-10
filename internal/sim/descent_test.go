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

// TestComputeBurnMarginThrustLimited pins the arithmetic on a seeded
// scenario with round numbers:
//
//	F/m = 2000/1000 = 2 m/s²,  g = 1 m/s²  → a_net = 1 m/s²
//	v = 20 m/s over h = 100 m  → a_req = 400/200 = 2 m/s²
//	accel ratio = 1/2 = 0.50
//	stop Δv     = 20 · 2/1 = 40 m/s, of 100 available → 2.50
//
// The thrust side is the smaller, so it binds — and at 0.50 the stack
// cannot stop: the alarm state.
func TestComputeBurnMarginThrustLimited(t *testing.T) {
	m := ComputeBurnMargin(2000, 1000, 1, 20, 100, 100)
	if m.Ratio != 0.5 {
		t.Errorf("Ratio = %v, want 0.50", m.Ratio)
	}
	if m.Limiter != LimitThrust {
		t.Errorf("Limiter = %v, want thrust", m.Limiter)
	}
	if m.State != MarginInsufficient {
		t.Errorf("State = %v, want MarginInsufficient (ratio < 1)", m.State)
	}
	if m.NetAccelMps2 != 1 || m.ReqAccelMps2 != 2 || m.ReqDVMps != 40 {
		t.Errorf("net=%v req=%v dv=%v, want 1 / 2 / 40", m.NetAccelMps2, m.ReqAccelMps2, m.ReqDVMps)
	}
}

// TestComputeBurnMarginFuelLimited: same stack, half the descent rate,
// so the acceleration side is comfortable (a_net 1 vs a_req 0.5 → 2.00)
// but the 30 m/s of propellant left only just covers the 20 m/s stop
// burn (30/20 = 1.50). The smaller ratio wins and names FUEL, and 1.50
// sits exactly on the OK/TIGHT boundary — pinned here so the band edge
// can't drift silently.
func TestComputeBurnMarginFuelLimited(t *testing.T) {
	m := ComputeBurnMargin(2000, 1000, 1, 10, 100, 30)
	if m.Ratio != 1.5 {
		t.Errorf("Ratio = %v, want 1.50", m.Ratio)
	}
	if m.Limiter != LimitFuel {
		t.Errorf("Limiter = %v, want fuel", m.Limiter)
	}
	if m.State != MarginOK {
		t.Errorf("State = %v, want MarginOK at exactly the 1.5 boundary", m.State)
	}
	if m.ReqDVMps != 20 {
		t.Errorf("ReqDVMps = %v, want 20", m.ReqDVMps)
	}
}

// TestComputeBurnMarginTightBand: between 1.0 and 1.5 the stop is
// flyable but the reserve is thin — the middle rung of the alarm ladder.
func TestComputeBurnMarginTightBand(t *testing.T) {
	// a_net = 2 − 1 = 1; a_req = 100/200 = 0.5 → 2.00 on thrust.
	// Δv: stop costs 10·2/1 = 20 m/s, of 25 available → 1.25 on fuel.
	m := ComputeBurnMargin(2000, 1000, 1, 10, 100, 25)
	if m.Ratio != 1.25 {
		t.Errorf("Ratio = %v, want 1.25", m.Ratio)
	}
	if m.State != MarginTight {
		t.Errorf("State = %v, want MarginTight in [1, 1.5)", m.State)
	}
}

// TestComputeBurnMarginCannotHover: a stack that can't out-thrust local
// gravity has no altitude at which the stop is flyable — ratio 0 and the
// alarm, never a soothing large number from the propellant side.
func TestComputeBurnMarginCannotHover(t *testing.T) {
	m := ComputeBurnMargin(500, 1000, 1.6, 5, 10_000, 5000)
	if m.Ratio != 0 {
		t.Errorf("Ratio = %v, want 0 for a sub-hover TWR", m.Ratio)
	}
	if m.State != MarginInsufficient || m.Limiter != LimitThrust {
		t.Errorf("State/Limiter = %v/%v, want insufficient/thrust", m.State, m.Limiter)
	}
	if !math.IsInf(m.ReqDVMps, 1) {
		t.Errorf("ReqDVMps = %v, want +Inf (the stop can't be flown at all)", m.ReqDVMps)
	}
}

// TestComputeBurnMarginUndefinedInputs: not descending / no altitude /
// no mass yields MarginNone, so the readout can print an em dash rather
// than a fabricated ratio.
func TestComputeBurnMarginUndefinedInputs(t *testing.T) {
	for _, c := range []struct {
		name                                        string
		thrust, mass, g, descent, altitude, availDV float64
	}{
		{"not descending", 2000, 1000, 1, 0, 100, 100},
		{"on the ground", 2000, 1000, 1, 20, 0, 100},
		{"no mass", 2000, 0, 1, 20, 100, 100},
	} {
		m := ComputeBurnMargin(c.thrust, c.mass, c.g, c.descent, c.altitude, c.availDV)
		if m.State != MarginNone {
			t.Errorf("%s: State = %v, want MarginNone", c.name, m.State)
		}
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
// ComputeBurnMargin's frozen scalars, so this now drives the alarm chain
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
