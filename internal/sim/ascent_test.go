// Package sim — ascent-half tests (issue #348 §3 / ADR 0043): the
// nose-vs-prograde geometry, the forward path a climbing vessel draws
// ahead of itself, the Q-band arithmetic, and the gating that keeps the
// ascent cues off a descending or coasting vessel.

package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ascendTestCraft parks the world's active craft `altM` above `bodyID`'s
// surface with a purely radial (+X) inertial velocity of `climbMps` PLUS
// the body's own co-rotation term at that point — so a vessel built this
// way has an air-relative velocity that is EXACTLY climbMps along +X
// (physics.AirRelativeVelocity cancels the co-rotation term out again),
// which is what makes the "vertical ascent" tests below exact rather
// than approximate.
func ascendTestCraft(t *testing.T, bodyID string, altM, climbMps float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	c.Primary = bodyForTest(t, w, bodyID)
	c.Landed = false
	c.Crashed = false
	rHat := orbital.Vec3{X: 1}
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + altM)
	corotate := physics.AtmosphereOmega(c.Primary).Cross(c.State.R)
	c.State.V = corotate.Add(rHat.Scale(climbMps))
	c.State.M = c.TotalMass()
	return w, c
}

// TestAttitudeVectorsForVerticalAscentCoincide: a vessel climbing
// straight up (nose along local-up, no co-rotation-relative crosstrack
// drift) has a surface-relative prograde that is ALSO exactly local-up
// — the classic "no gravity turn yet" moment right off the pad. Nose and
// prograde must read as the same direction.
func TestAttitudeVectorsForVerticalAscentCoincide(t *testing.T) {
	_, c := ascendTestCraft(t, "earth", 1_000, 50)
	rHat := orbital.Vec3{X: 1}
	c.CurrentAttitudeDir = rHat

	vec, ok := AttitudeVectorsFor(c)
	if !ok {
		t.Fatal("AttitudeVectorsFor: ok=false for a vessel with defined nose + velocity")
	}
	if d := vec.NoseDir.Dot(vec.ProgradeDir); d < 1-1e-9 {
		t.Errorf("vertical ascent: nose·prograde = %.9f, want ~1 (coincident)", d)
	}
}

// TestAttitudeVectorsForPitchedOverDiverge: the same climb, but the nose
// is pitched 45° off the (still-vertical) velocity vector — the gravity
// turn in progress. Nose and prograde must read as clearly distinct
// directions, not a numerically-negligible wobble.
func TestAttitudeVectorsForPitchedOverDiverge(t *testing.T) {
	_, c := ascendTestCraft(t, "earth", 1_000, 50)
	// 45° off +X (local-up) toward +Y.
	c.CurrentAttitudeDir = orbital.Vec3{X: math.Cos(math.Pi / 4), Y: math.Sin(math.Pi / 4)}

	vec, ok := AttitudeVectorsFor(c)
	if !ok {
		t.Fatal("AttitudeVectorsFor: ok=false for a vessel with defined nose + velocity")
	}
	if d := vec.NoseDir.Dot(vec.ProgradeDir); d > math.Cos(math.Pi/8) {
		t.Errorf("45° pitched-over ascent: nose·prograde = %.4f, want clearly below cos(22.5°) (%.4f)",
			d, math.Cos(math.Pi/8))
	}
}

// TestAttitudeVectorsForUndefinedCases: no commanded nose yet, or sitting
// dead still relative to the ground (zero air-relative velocity) — both
// report ok=false rather than a fabricated direction.
func TestAttitudeVectorsForUndefinedCases(t *testing.T) {
	_, c := ascendTestCraft(t, "earth", 1_000, 50)
	c.CurrentAttitudeDir = orbital.Vec3{}
	if _, ok := AttitudeVectorsFor(c); ok {
		t.Error("zero CurrentAttitudeDir: want ok=false")
	}

	_, c2 := ascendTestCraft(t, "earth", 1_000, 0) // climbMps=0 → air-relative V is zero
	c2.CurrentAttitudeDir = orbital.Vec3{X: 1}
	if _, ok := AttitudeVectorsFor(c2); ok {
		t.Error("zero air-relative velocity: want ok=false")
	}
}

// TestPredictAscentPathNoImpactForStableOrbit: an ascent that has already
// reached a stable circular orbit never touches ground inside the
// horizon — PredictImpact would report nothing at all (ok=false, no
// path), but the ascent arc still has a real path worth drawing.
func TestPredictAscentPathNoImpactForStableOrbit(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Primary = bodyForTest(t, w, "earth")
	c.Landed = false
	c.Crashed = false
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300_000
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	c.State.M = c.TotalMass()

	path, ok := PredictAscentPath(c, AscentPredictHorizon)
	if !ok {
		t.Fatal("PredictAscentPath: ok=false for a valid orbital state")
	}
	if path.ImpactFound {
		t.Error("stable circular orbit reported ground contact")
	}
	if len(path.Path) < 2 {
		t.Fatalf("path has %d points, want a drawable run", len(path.Path))
	}
	if path.Path[0] != c.State.R {
		t.Errorf("path[0] = %v, want the craft's current position %v", path.Path[0], c.State.R)
	}
}

// TestPredictAscentPathFindsImpactForLoftedHop: a suborbital hop (well
// under escape velocity, zero angular momentum) arcs up and falls
// straight back down — the ascent arc must find that contact just like
// PredictImpact would once the vessel starts actually falling.
func TestPredictAscentPathFindsImpactForLoftedHop(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Primary = bodyForTest(t, w, "moon")
	c.Landed = false
	c.Crashed = false
	const altM = 10_000.0
	c.State.R = orbital.Vec3{X: c.Primary.RadiusMeters() + altM}
	c.State.V = orbital.Vec3{X: 50} // well under lunar escape velocity
	c.State.M = c.TotalMass()

	path, ok := PredictAscentPath(c, AscentPredictHorizon)
	if !ok {
		t.Fatal("PredictAscentPath: ok=false for a valid ballistic state")
	}
	if !path.ImpactFound {
		t.Fatal("lofted hop should fall back to the surface inside the horizon")
	}
	if path.Path[0] != c.State.R {
		t.Errorf("path[0] = %v, want the craft's current position %v", path.Path[0], c.State.R)
	}
	if path.Path[len(path.Path)-1] != path.Impact {
		t.Errorf("path ends at %v, want the impact point %v", path.Path[len(path.Path)-1], path.Impact)
	}
}

// TestAscentQBandForGatesOnAtmosphere: the Q-band instrument only exists
// on a body with an atmosphere at all (issue #348 §3) — an airless body
// (the Moon) reports ok=false regardless of how the vessel is flying.
func TestAscentQBandForGatesOnAtmosphere(t *testing.T) {
	w, c := ascendTestCraft(t, "moon", 1_000, 50)
	if _, ok := AscentQBandFor(w, c); ok {
		t.Error("airless body: AscentQBandFor ok=true, want false")
	}
}

// TestAscentQBandForSeededAnalyticQ pins AscentQBandFor's current-Q
// arithmetic against Earth's catalog atmosphere constants by hand:
// ρ(alt) = ρ₀·exp(-alt/H), Q = ½·ρ·v². ascendTestCraft's construction
// makes the air-relative speed exactly climbMps, so this is a genuine
// closed-form check of the wiring, not a re-statement of
// physics.DynamicPressure's own formula.
func TestAscentQBandForSeededAnalyticQ(t *testing.T) {
	const altM = 5_000.0
	const climbMps = 100.0
	w, c := ascendTestCraft(t, "earth", altM, climbMps)
	atm := c.Primary.Atmosphere
	if atm == nil {
		t.Fatal("setup: earth should carry catalog atmosphere data")
	}

	qb, ok := AscentQBandFor(w, c)
	if !ok {
		t.Fatal("AscentQBandFor: ok=false for a body with an atmosphere")
	}
	if qb.AtmosphereDepthM != atm.CutoffAltitude {
		t.Errorf("AtmosphereDepthM = %v, want the catalog cutoff %v", qb.AtmosphereDepthM, atm.CutoffAltitude)
	}
	if math.Abs(qb.CurrentAltM-altM) > 1 {
		t.Errorf("CurrentAltM = %v, want %v", qb.CurrentAltM, altM)
	}

	rho := atm.SurfaceDensity * math.Exp(-altM/atm.ScaleHeight)
	wantQ := 0.5 * rho * climbMps * climbMps
	if math.Abs(qb.CurrentQPa-wantQ) > 1e-6*wantQ {
		t.Errorf("CurrentQPa = %.6f, want %.6f (analytic ρ·v² formula)", qb.CurrentQPa, wantQ)
	}

	if qb.HasMaxQ {
		t.Error("HasMaxQ = true before any session has ratcheted LaunchMaxQ")
	}
	w.LaunchMaxQ = 12_345
	w.LaunchMaxQAltM = 8_000
	qb2, ok := AscentQBandFor(w, c)
	if !ok {
		t.Fatal("AscentQBandFor: ok=false on second call")
	}
	if !qb2.HasMaxQ || qb2.MaxQPa != 12_345 || qb2.MaxQAltM != 8_000 {
		t.Errorf("MaxQ fields = (%v, %v, has=%v), want (12345, 8000, true)", qb2.MaxQPa, qb2.MaxQAltM, qb2.HasMaxQ)
	}
}

// TestAscentQBandForStandsDownOncePeriapsisClearsAtmosphere (#449): a
// vessel that circularized into a stable orbit fully above the
// atmosphere still swings a nonzero radial (climb) rate on every
// periapsis→apoapsis half, purely from orbital mechanics — nothing to
// do with the atmosphere. Before the fix, that made the ATMOSPHERE chip
// (HasQBand) flicker back on once per orbit forever, long after the
// vessel could ever see the atmosphere again. Gating on periapsis
// altitude instead of the instantaneous climb rate fixes it, without
// touching the still-correct "climbed clear, showing the past-the-top
// band" case for an orbit whose periapsis is still inside the
// atmosphere (a real re-entry is coming next orbit).
func TestAscentQBandForStandsDownOncePeriapsisClearsAtmosphere(t *testing.T) {
	w, c := ascendTestCraft(t, "earth", 218_000, 50)
	mu := c.Primary.GravitationalParameter()
	R := c.Primary.RadiusMeters()
	atm := c.Primary.Atmosphere

	// orbitAt builds a Kepler orbit with periapsis/apoapsis altitudes
	// rp/ra, evaluated at true anomaly nu (radians) — nu = 90° sits on
	// the climbing half, where the radial rate is at its largest
	// nonzero value for a given eccentricity.
	orbitAt := func(rpAltM, raAltM, nu float64) {
		rp := R + rpAltM
		ra := R + raAltM
		a := (rp + ra) / 2
		e := (ra - rp) / (ra + rp)
		p := a * (1 - e*e)
		rMag := p / (1 + e*math.Cos(nu))
		rHat := orbital.Vec3{X: math.Cos(nu), Y: math.Sin(nu)}
		c.State.R = rHat.Scale(rMag)
		h := math.Sqrt(mu * p)
		vr := (mu / h) * e * math.Sin(nu)
		vt := (mu / h) * (1 + e*math.Cos(nu))
		thetaHat := orbital.Vec3{X: -math.Sin(nu), Y: math.Cos(nu)}
		c.State.V = rHat.Scale(vr).Add(thetaHat.Scale(vt))
	}

	// Stable orbit, periapsis (210km) and apoapsis (226km) both clear of
	// the 150km cutoff: the reported bug. Must stand down for good.
	orbitAt(210_000, 226_000, math.Pi/2)
	if alt := c.Altitude(); alt <= atm.CutoffAltitude {
		t.Fatalf("test setup: altitude %.0fm should be above the %.0fm cutoff", alt, atm.CutoffAltitude)
	}
	// Pin the test's own premise: nu=90 must actually be on the climbing
	// half (climb rate above the floor), or this test would keep passing
	// even if it stopped exercising the reported symptom.
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	if climbRate := vRel.Dot(rHat); climbRate < climbRateFloorMps {
		t.Fatalf("test setup: climb rate %.3f m/s at nu=90°, want above the %.1f m/s floor (this orbitAt call must land on the climbing half)", climbRate, climbRateFloorMps)
	}
	if qb, ok := AscentQBandFor(w, c); ok {
		t.Errorf("stable orbit above the atmosphere: AscentQBandFor ok=true (HasMaxQ=%v), want false — must not flicker back on every orbit", qb.HasMaxQ)
	}
	// End to end: the player-visible path is AscentCueFor -> HasQBand,
	// not AscentQBandFor directly. AscentCueFor's own `ok` still stands
	// up here (its climb-rate gate is a deliberately separate, still-
	// open follow-up — see AscentQBandFor's doc comment) — this pins
	// that HasQBand specifically is what the fix reaches.
	if cue, ok := AscentCueFor(w, c, AscentPredictHorizon); !ok || cue.HasQBand {
		t.Errorf("AscentCueFor(stable orbit above atmosphere) = (HasQBand=%v, ok=%v), want (false, true)", cue.HasQBand, ok)
	}

	// Still-elliptical orbit whose periapsis (100km) sits INSIDE the
	// 150km cutoff: a real re-entry is coming next orbit, so the chip
	// staying live on the climbing half is still the correct call.
	orbitAt(100_000, 226_000, math.Pi/2)
	if _, ok := AscentQBandFor(w, c); !ok {
		t.Error("periapsis still inside the atmosphere: AscentQBandFor ok=false, want true (re-entry is still coming)")
	}
}

// TestAscentQBandForHyperbolicDepartureStandsDownByAltitude (#449
// review finding 3): an outbound hyperbola (or an SOI-exiting orbit)
// can have a "periapsis" — Elements.Periapsis() on a hyperbolic orbit
// is still well-defined, the closest approach of the unbound path —
// that sits INSIDE the atmosphere's cutoff even though the vessel is
// headed away for good and will never come back down through it. Gating
// on periapsis alone (as the first version of this fix did) kept the
// chip live all the way to SOI exit for that shape. This mirrors
// shouldShowLaunchHUD's existing el.E>=1||el.A<=0 split: go by current
// altitude instead of periapsis once the orbit is hyperbolic/degenerate.
func TestAscentQBandForHyperbolicDepartureStandsDownByAltitude(t *testing.T) {
	w, c := ascendTestCraft(t, "earth", 400_000, 50)
	mu := c.Primary.GravitationalParameter()
	R := c.Primary.RadiusMeters()
	atm := c.Primary.Atmosphere

	// A hyperbolic departure with periapsis (100km) INSIDE the
	// atmosphere, vinf = 3 km/s outbound, evaluated near periapsis
	// where the vessel is still climbing fast.
	rp := R + 100_000.0
	vinf := 3_000.0
	eps := vinf * vinf / 2
	a := -mu / (2 * eps)
	e := 1 - rp/a
	nu := math.Pi / 6 // still inbound-to-outbound climb, well short of asymptote
	p := a * (1 - e*e)
	rMag := p / (1 + e*math.Cos(nu))
	rHat := orbital.Vec3{X: math.Cos(nu), Y: math.Sin(nu)}
	c.State.R = rHat.Scale(rMag)
	h := math.Sqrt(mu * p)
	vr := (mu / h) * e * math.Sin(nu)
	vt := (mu / h) * (1 + e*math.Cos(nu))
	thetaHat := orbital.Vec3{X: -math.Sin(nu), Y: math.Cos(nu)}
	c.State.V = rHat.Scale(vr).Add(thetaHat.Scale(vt))

	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if el.E < 1 {
		t.Fatalf("test setup: e=%.4f, want a hyperbolic (e>=1) orbit", el.E)
	}
	if alt := c.Altitude(); alt < atm.CutoffAltitude {
		t.Fatalf("test setup: altitude %.0fm should already be above the %.0fm cutoff", alt, atm.CutoffAltitude)
	}
	periAltM := el.Periapsis() - R
	if periAltM >= atm.CutoffAltitude {
		t.Fatalf("test setup: periapsis altitude %.0fm should be INSIDE the %.0fm cutoff (that's the case this test targets)", periAltM, atm.CutoffAltitude)
	}

	if _, ok := AscentQBandFor(w, c); ok {
		t.Error("hyperbolic departure, high above the atmosphere, periapsis inside it: AscentQBandFor ok=true, want false (no re-entry is coming — it's a departure, not a launch)")
	}
}

// TestAscentCueForGatingMatrix is the ascent mirror of
// TestDescentCorridorForGating: it stands up for a climbing vessel and
// stands down for a falling, coasting, or landed one — and, critically,
// AscentCueFor and DescentCorridorFor are never both true for the same
// state, so the surface view can't stack the two instrument blocks.
func TestAscentCueForGatingMatrix(t *testing.T) {
	w, c := ascendTestCraft(t, "earth", 1_000, 50)
	c.CurrentAttitudeDir = orbital.Vec3{X: 1}

	assertGates := func(name string, wantAscent, wantDescent bool) {
		t.Helper()
		_, ascOK := AscentCueFor(w, c, AscentPredictHorizon)
		_, descOK := DescentCorridorFor(c, DescentPredictHorizon)
		if ascOK != wantAscent {
			t.Errorf("%s: AscentCueFor ok=%v, want %v", name, ascOK, wantAscent)
		}
		if descOK != wantDescent {
			t.Errorf("%s: DescentCorridorFor ok=%v, want %v", name, descOK, wantDescent)
		}
		if ascOK && descOK {
			t.Errorf("%s: ascent AND descent both stood up — the gates must be mutually exclusive", name)
		}
	}

	// Climbing: ascent up, descent down.
	assertGates("climbing", true, false)

	// Falling: mirror the velocity's radial component negative.
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	corotate := physics.AtmosphereOmega(c.Primary).Cross(c.State.R)
	c.State.V = corotate.Add(rHat.Scale(-50))
	assertGates("falling", false, true)

	// Coasting in a stable circular orbit: falling nowhere, climbing
	// nowhere — both stand down.
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	assertGates("circular orbit", false, false)

	// Landed: neither instrument applies.
	c.State.V = corotate.Add(rHat.Scale(50))
	c.Landed = true
	assertGates("landed", false, false)
	c.Landed = false
}

// TestAscentCueForAirlessBodyHasCuesButNoQBand: the nose/prograde markers
// and the predicted arc don't depend on an atmosphere existing at all —
// only the Q band does (issue #348 §3's explicit gate). A climbing
// vessel over the airless Moon gets the first two but not the third.
func TestAscentCueForAirlessBodyHasCuesButNoQBand(t *testing.T) {
	w, c := ascendTestCraft(t, "moon", 1_000, 50)
	c.CurrentAttitudeDir = orbital.Vec3{X: 1}

	cue, ok := AscentCueFor(w, c, AscentPredictHorizon)
	if !ok {
		t.Fatal("AscentCueFor: ok=false for a climbing vessel over an airless body")
	}
	if !cue.HasAttitude {
		t.Error("HasAttitude = false, want true (nose + velocity both defined)")
	}
	if len(cue.Arc.Path) < 2 {
		t.Error("Arc.Path too short — the predicted path should still draw over an airless body")
	}
	if cue.HasQBand {
		t.Error("HasQBand = true over an airless body, want false")
	}
}
