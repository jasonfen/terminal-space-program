// Package sim — v0.11.4+ Crashed / Soft-Landed lifecycle tests
// (ADR 0004). Exercises the surface-arrival predicate at the
// physics.ClampToSurface call site: CanSoftLand catalog gate +
// V_CRIT velocity ceiling + NOSE_TOL alignment together decide
// Landed vs Crashed.

package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
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

// ADR 0066 guard 4: every soft touchdown plants one flag at the touchdown
// lat/lon, named for the vessel; a second touchdown numbers " site 2"; a pad
// spawn plants none.
func TestSoftTouchdownPlantsFlagAndRepeatNumbersIt(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	if len(w.Sites) != 0 {
		t.Fatalf("setup: a fresh world has %d sites, want 0", len(w.Sites))
	}
	c.CanSoftLand = true
	setImpactState(c, 2, orbital.Vec3{X: 1})
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Landed {
		t.Fatal("setup: no soft touchdown")
	}
	if len(w.Sites) != 1 {
		t.Fatalf("want one flag, got %+v", w.Sites)
	}
	s := w.Sites[0]
	if s.Kind != SiteFlag || s.Name != c.Name+" site" || s.LatDeg != c.LandedLatDeg || s.LonDeg != c.LandedLonDeg || s.CraftID != c.ID {
		t.Errorf("flag = %+v, want Kind flag, name %q at the touchdown lat/lon", s, c.Name+" site")
	}
	w.leavePad(c)
	setImpactState(c, 2, orbital.Vec3{X: 1})
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if len(w.Sites) != 2 || w.Sites[1].Name != c.Name+" site 2" || w.Sites[1].ID == w.Sites[0].ID {
		t.Errorf("second touchdown: want %q with a fresh id, got %+v", c.Name+" site 2", w.Sites)
	}
	if w.NameCounters[c.Name+" site"] != 2 {
		t.Errorf("never-reuse counter = %d, want 2", w.NameCounters[c.Name+" site"])
	}
}

func TestPadSpawnPlantsNoFlag(t *testing.T) {
	w, _ := crashTestPrimaryFor(t)
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth", Launchpad: true, Latitude: 28.6}); err != nil {
		t.Fatal(err)
	}
	if len(w.Sites) != 0 {
		t.Errorf("a launchpad spawn planted %+v, want no sites", w.Sites)
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

// TestFalcon9SoftLandsAtExactNoseTol — a CanSoftLand vessel touching
// down below V_CRIT with the nose at *exactly* the documented minimum
// alignment (cosNose == NOSE_TOL == 0.7, ~45° off vertical) must land,
// not crash. The route comments document "land when nose > NOSE_TOL" and
// the velocity gates use >= (reject at-or-above), so the nose gate should
// reject strictly below the tolerance. The old `<= CrashNoseTol` crashed
// the boundary case it should accept. Exercises the pure
// classifySurfaceArrival predicate directly: integrateOneCraft slews
// CurrentAttitudeDir at the tick top, which would perturb an
// exactly-on-the-boundary nose and mask the gate under test. (#90)
func TestFalcon9SoftLandsAtExactNoseTol(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = true
	// Use a unit preClampR so the predicate's localUp = R/|R| is exactly
	// {1,0,0}; then nose.Dot(localUp) is just the normalized nose.X.
	// classifySurfaceArrival is pure and frame-agnostic, so the small
	// radius is irrelevant to the gate.
	preClampR := orbital.Vec3{X: 1}
	localUp := preClampR.Scale(1 / preClampR.Norm())

	// `<` vs `<=` differ only at bit-exact equality, and the predicate
	// re-normalizes the nose — so whether any given vector lands exactly
	// on CrashNoseTol depends on the platform's float rounding (arm64 FMA
	// vs amd64). Rather than hardcode a magic component that's only exact
	// on one arch, search at runtime for a nose whose *post-normalization*
	// dot is bit-identical to CrashNoseTol on whatever machine runs this.
	noseAtTol := exactNoseTolDir(t, localUp)
	c.CurrentAttitudeDir = noseAtTol
	preClampV := orbital.Vec3{X: -2} // 2 m/s inward, below V_CRIT
	outcome, _, _ := classifySurfaceArrival(c, preClampR, preClampV, w.Clock.SimTime)
	if outcome != outcomeLanded {
		t.Errorf("CanSoftLand + V=2 m/s + nose at exact NOSE_TOL: outcome=%v, want outcomeLanded", outcome)
	}
}

// exactNoseTolDir returns a nose direction whose normalized dot with
// localUp is bit-exactly CrashNoseTol — i.e. a nose sitting precisely on
// the soft-land boundary the gate compares against. Found by search so
// the construction is portable across float-rounding differences
// (arm64 FMA vs amd64); fails the test if no such vector exists in the
// neighbourhood (which would itself be a signal worth investigating).
func exactNoseTolDir(t *testing.T, localUp orbital.Vec3) orbital.Vec3 {
	t.Helper()
	const x = CrashNoseTol
	y0 := math.Sqrt(1 - x*x) // nominal complement; dot ≈ tol near here
	for i := -200000; i <= 200000; i++ {
		y := y0 + float64(i)*1e-16
		n := orbital.Vec3{X: x, Y: y}
		if n.Scale(1/n.Norm()).Dot(localUp) == CrashNoseTol {
			return n
		}
	}
	t.Fatalf("could not construct a nose with normalized dot == CrashNoseTol near y=%.17g", y0)
	return orbital.Vec3{}
}

// TestCrashedVesselSkipsIntegration - once Crashed, integrateOneCraft does no
// gravity, drag, thrust or slew: velocity stays zero and the wreck stays on
// its crater (ADR 0066 decision 8 pins position to the stored impact point,
// where it used to freeze in the inertial frame and drift off the crater).
func TestCrashedVesselSkipsIntegration(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = false
	localUp := orbital.Vec3{X: 1}
	setImpactState(c, 50, localUp)
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Crashed {
		t.Fatal("setup: expected Crashed after 50 m/s impact on non-CanSoftLand vessel")
	}
	snapshotV := c.State.V
	// Tick again - Crashed branch pins the position, nothing else.
	w.integrateOneCraft(c, w.Clock.BaseStep)
	wantR, _ := surfacePointState(c.Primary, c.LandedLatDeg, c.LandedLonDeg, w.Clock.SimTime)
	if c.State.R != wantR {
		t.Errorf("Crashed vessel not on its crater: got %+v, want %+v", c.State.R, wantR)
	}
	if c.State.V != snapshotV {
		t.Errorf("Crashed vessel velocity drifted: snapshot=%+v, post-tick=%+v",
			snapshotV, c.State.V)
	}
}

// ADR 0066 guard 5: a crash stores its lat/lon, plants one wreck site there,
// and the wreck stays on the crater while the ground turns (an hour of 1 s
// ticks moved it 1,668 km at Earth's equator before the body-fixed pin).
func TestCrashStoresImpactPlantsWreckAndPinsToCrater(t *testing.T) {
	w, c := crashTestPrimaryFor(t)
	c.CanSoftLand = false
	setImpactState(c, 50, orbital.Vec3{X: 1})
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Crashed {
		t.Fatal("setup: no crash")
	}
	if c.LandedLatDeg == 0 && c.LandedLonDeg == 0 {
		t.Fatal("crash did not store the impact lat/lon")
	}
	if len(w.Sites) != 1 || w.Sites[0].Kind != SiteWreck || w.Sites[0].LatDeg != c.LandedLatDeg ||
		w.Sites[0].LonDeg != c.LandedLonDeg || w.Sites[0].Name != c.Name+" wreck" {
		t.Fatalf("want one wreck site at the impact point named %q, got %+v", c.Name+" wreck", w.Sites)
	}
	craterLat, craterLon := c.LandedLatDeg, c.LandedLonDeg
	for i := 0; i < 3600; i++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(time.Second)
		w.integrateOneCraft(c, time.Second)
	}
	u := c.State.R.Unit()
	lat, lon := render.WorldToBodyFixed(c.Primary, render.Vec3{X: u.X, Y: u.Y, Z: u.Z}, w.Clock.SimTime)
	dLat := (lat - craterLat) * math.Pi / 180
	dLon := (lon - craterLon) * math.Pi / 180 * math.Cos(craterLat*math.Pi/180)
	if off := c.Primary.RadiusMeters() * math.Hypot(dLat, dLon); off > 1 {
		t.Errorf("wreck is %.1f m from its crater after an hour, want within 1 m", off)
	}
}
