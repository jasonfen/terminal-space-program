// Package screens: the airless-ascent predicate (ADR 0051 decision 10,
// correction C4): "periapsis below the surface and climbing", with no
// atmosphere test of its own. Groundwork only (ADR 0051 slice 1): nothing
// renders against this yet: deriveFlightPhase and shouldShowLaunchHUD are
// untouched (audit C74: shouldShowLaunchHUD's atmosphere test makes
// deriveFlightPhase read PhaseDescent for every airless ascent, which is
// exactly the #454 gap this predicate exists to let slice 2 close).

package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestIsAirlessAscentApolloOffMoonPad: an Apollo-Stack a few metres off the
// Moon pad, climbing: periapsis of that suborbital arc is deep below the
// surface and the craft is climbing outward. True.
func TestIsAirlessAscentApolloOffMoonPad(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := sysBody(t, w, "Moon")
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	c.Primary = *moon
	c.Landed = false
	c.State = physics.StateVector{
		R: orbital.Vec3{X: moon.RadiusMeters() + 10, Y: 0, Z: 0},
		V: orbital.Vec3{X: 50, Y: 0, Z: 0}, // climbing straight up
	}
	c.State.M = c.TotalMass()
	if !isAirlessAscent(c) {
		t.Error("Apollo-Stack climbing off the Moon pad: got false, want true")
	}
}

// TestIsAirlessAscentStableLunarOrbit: a circular 210 km lunar orbit has
// periapsis well above the surface. False.
func TestIsAirlessAscentStableLunarOrbit(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := sysBody(t, w, "Moon")
	mu := moon.GravitationalParameter()
	r := moon.RadiusMeters() + 210_000
	vCirc := math.Sqrt(mu / r)
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	c.Landed = false
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r, Y: 0, Z: 0},
		V: orbital.Vec3{X: 0, Y: vCirc, Z: 0},
	}
	c.State.M = c.TotalMass()
	if isAirlessAscent(c) {
		t.Error("stable 210 km lunar orbit: got true, want false (periapsis above the surface)")
	}
}

// TestIsAirlessAscentLanderFallingTowardMoon: descending (radial-in
// velocity). False regardless of periapsis.
func TestIsAirlessAscentLanderFallingTowardMoon(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := sysBody(t, w, "Moon")
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	c.Landed = false
	c.State = physics.StateVector{
		R: orbital.Vec3{X: moon.RadiusMeters() + 20_000, Y: 0, Z: 0},
		V: orbital.Vec3{X: -120, Y: 30, Z: 0}, // falling
	}
	c.State.M = c.TotalMass()
	if isAirlessAscent(c) {
		t.Error("lander falling toward the Moon: got true, want false (descending)")
	}
}

// TestIsAirlessAscentClimbAfterDeorbitHop: C4's own scenario, a lander
// climbing back toward an apoapsis after a deorbit burn or hop, periapsis
// still below the surface. True: "it is the same situation" as an ascent.
func TestIsAirlessAscentClimbAfterDeorbitHop(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := sysBody(t, w, "Moon")
	mu := moon.GravitationalParameter()
	// A shallow ellipse: periapsis 2 km below the surface, apoapsis 25 km
	// above it. Place the craft near periapsis, past it, climbing out.
	rp := moon.RadiusMeters() - 2_000
	ra := moon.RadiusMeters() + 25_000
	a := (rp + ra) / 2
	// vis-viva at r = rp (periapsis speed, all tangential there); nudge the
	// evaluation point slightly outbound so vUp > 0 without leaving the
	// ellipse's energy: use r a hair past periapsis and derive velocity as
	// mostly tangential plus a small radial-out component consistent with
	// still being deep in this same low ellipse (below the 25 km apoapsis).
	r := rp + 1_000
	vTangential := math.Sqrt(mu * (2/r - 1/a))
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	c.Landed = false
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r, Y: 0, Z: 0},
		V: orbital.Vec3{X: 5, Y: vTangential, Z: 0}, // small radial-out (climbing)
	}
	c.State.M = c.TotalMass()
	if !isAirlessAscent(c) {
		t.Error("climb to apoapsis after a deorbit burn/hop: got false, want true (same situation as ascent, C4)")
	}
}

// TestIsAirlessAscentEarthAscent: the predicate carries no atmosphere test
// of its own, so an Earth ascent (periapsis deep below the surface,
// climbing) reads exactly the same as a Moon ascent: true. Slice 2, not
// this slice, decides how this combines with shouldShowLaunchHUD's
// atmosphere-gated branch at the call site.
func TestIsAirlessAscentEarthAscent(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := sysBody(t, w, "Earth")
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *earth
	c.Landed = false
	c.State = physics.StateVector{
		R: orbital.Vec3{X: earth.RadiusMeters() + 8_000, Y: 0, Z: 0},
		V: orbital.Vec3{X: 200, Y: 400, Z: 0},
	}
	c.State.M = c.TotalMass()
	if !isAirlessAscent(c) {
		t.Error("Earth ascent: got false, want true (no atmosphere test, same rule as an airless ascent)")
	}
}

// TestIsAirlessAscentLandedIsFalse: a Landed vessel is never ascending,
// whatever its stale/co-rotation state vector would otherwise say.
func TestIsAirlessAscentLandedIsFalse(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := sysBody(t, w, "Moon")
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	c.Primary = *moon
	c.Landed = true
	c.State = physics.StateVector{
		R: orbital.Vec3{X: moon.RadiusMeters(), Y: 0, Z: 0},
		V: orbital.Vec3{},
	}
	c.State.M = c.TotalMass()
	if isAirlessAscent(c) {
		t.Error("Landed vessel: got true, want false")
	}
}

// TestIsAirlessAscentNilCraft: nil-safe, matching the sibling predicates
// (shouldShowLaunchHUD, shouldShowDescentHUD, craftHasOrbit).
func TestIsAirlessAscentNilCraft(t *testing.T) {
	if isAirlessAscent(nil) {
		t.Error("nil craft: got true, want false")
	}
}

// TestIsAirlessAscentNearApoapsisEdge: at the exact instant of apoapsis
// (vUp == 0) on a low ellipse whose periapsis is below the surface, the
// predicate still reads true: the craft hasn't started descending yet,
// and C4's "it is the same situation" throughout the climb includes its
// peak. Mirrors deriveFlightPhase's own vUp >= 0 (not > 0) choice for its
// atmospheric-ascent branch.
func TestIsAirlessAscentNearApoapsisEdge(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := sysBody(t, w, "Moon")
	mu := moon.GravitationalParameter()
	rp := moon.RadiusMeters() - 2_000
	ra := moon.RadiusMeters() + 25_000
	a := (rp + ra) / 2
	vApo := math.Sqrt(mu * (2/ra - 1/a)) // purely tangential at apoapsis
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	c.Landed = false
	c.State = physics.StateVector{
		R: orbital.Vec3{X: ra, Y: 0, Z: 0},
		V: orbital.Vec3{X: 0, Y: vApo, Z: 0}, // vUp == 0 exactly
	}
	c.State.M = c.TotalMass()
	if !isAirlessAscent(c) {
		t.Error("exactly at apoapsis (vUp == 0), periapsis below surface: got false, want true")
	}
}

// TestIsAirlessAscentHyperbolicNoPanic: a hyperbolic/degenerate state must
// not panic. It exists purely as a robustness guard; the returned answer
// isn't specified by the ADR for this state.
func TestIsAirlessAscentHyperbolicNoPanic(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := sysBody(t, w, "Moon")
	mu := moon.GravitationalParameter()
	r := moon.RadiusMeters() + 5_000
	vEsc := math.Sqrt(2 * mu / r)
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	c.Landed = false
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r, Y: 0, Z: 0},
		V: orbital.Vec3{X: vEsc * 1.5, Y: 0, Z: 0}, // well past escape, radial
	}
	c.State.M = c.TotalMass()
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("isAirlessAscent panicked on a hyperbolic/degenerate state: %v", rec)
		}
	}()
	_ = isAirlessAscent(c)
}

// sysBody looks up a body by name in w's default system, addressable so
// FindBody's pointer receiver can be called (mirrors phaseCraftOn's own
// `sys := w.System()` pattern in orbit_flight_phase_test.go).
func sysBody(t *testing.T, w *sim.World, name string) *bodies.CelestialBody {
	t.Helper()
	sys := w.System()
	b := sys.FindBody(name)
	if b == nil {
		t.Fatalf("%s not in default system", name)
	}
	return b
}
