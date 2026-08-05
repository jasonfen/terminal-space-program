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
