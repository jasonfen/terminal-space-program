// Package screens: the sub-orbital-climb predicate (ADR 0051 decision 10,
// correction C4): "periapsis below the surface and climbing", with no
// atmosphere test of its own. Renamed from isAirlessAscent in slice 2: the
// old name was wrong on its own terms, since the predicate deliberately
// carries no atmosphere test and reads an Earth ascent the same as a Moon
// ascent. Groundwork only until slice 2 wires a consumer: deriveFlightPhase
// and shouldShowLaunchHUD are untouched (audit C74: shouldShowLaunchHUD's
// atmosphere test makes deriveFlightPhase read PhaseDescent for every
// airless ascent, which is exactly the #454 gap this predicate exists to
// let slice 2 close).

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

// TestIsSubOrbitalClimb: one row per situation, each pinning the predicate's
// answer for a craft at a given state around a named body.
func TestIsSubOrbitalClimb(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// A shallow lunar ellipse: periapsis 2 km below the surface, apoapsis
	// 25 km above it.
	shallow := func(moon *bodies.CelestialBody) (rp, ra, a, mu float64) {
		mu = moon.GravitationalParameter()
		rp = moon.RadiusMeters() - 2_000
		ra = moon.RadiusMeters() + 25_000
		return rp, ra, (rp + ra) / 2, mu
	}
	cases := []struct {
		name    string
		loadout string
		body    string
		landed  bool
		state   func(b *bodies.CelestialBody) (r, v orbital.Vec3)
		want    bool
	}{
		// An Apollo-Stack a few metres off the Moon pad, climbing: periapsis of
		// that suborbital arc is deep below the surface. True.
		{"Apollo-Stack climbing off the Moon pad", spacecraft.LoadoutApolloStackID, "Moon", false,
			func(b *bodies.CelestialBody) (orbital.Vec3, orbital.Vec3) {
				return orbital.Vec3{X: b.RadiusMeters() + 10}, orbital.Vec3{X: 50}
			}, true},
		// A circular 210 km lunar orbit has periapsis well above the surface. False.
		{"stable 210 km lunar orbit", spacecraft.LoadoutLanderID, "Moon", false,
			func(b *bodies.CelestialBody) (orbital.Vec3, orbital.Vec3) {
				r := b.RadiusMeters() + 210_000
				return orbital.Vec3{X: r}, orbital.Vec3{Y: math.Sqrt(b.GravitationalParameter() / r)}
			}, false},
		// Descending (radial-in velocity). False regardless of periapsis.
		{"lander falling toward the Moon", spacecraft.LoadoutLanderID, "Moon", false,
			func(b *bodies.CelestialBody) (orbital.Vec3, orbital.Vec3) {
				return orbital.Vec3{X: b.RadiusMeters() + 20_000}, orbital.Vec3{X: -120, Y: 30}
			}, false},
		// C4's own scenario: climbing back toward apoapsis after a deorbit burn
		// or hop, periapsis still below the surface. True: the same situation
		// as an ascent. Placed a hair past periapsis with a small radial-out
		// component, still deep in the same low ellipse.
		{"climb after a deorbit hop", spacecraft.LoadoutLanderID, "Moon", false,
			func(b *bodies.CelestialBody) (orbital.Vec3, orbital.Vec3) {
				rp, _, a, mu := shallow(b)
				r := rp + 1_000
				return orbital.Vec3{X: r}, orbital.Vec3{X: 5, Y: math.Sqrt(mu * (2/r - 1/a))}
			}, true},
		// No atmosphere test of its own: an Earth ascent reads exactly like a
		// Moon ascent. True.
		{"Earth ascent", spacecraft.LoadoutLanderID, "Earth", false,
			func(b *bodies.CelestialBody) (orbital.Vec3, orbital.Vec3) {
				return orbital.Vec3{X: b.RadiusMeters() + 8_000}, orbital.Vec3{X: 200, Y: 400}
			}, true},
		// A Landed vessel is never ascending, whatever its stale/co-rotation
		// state vector would otherwise say.
		{"landed vessel", spacecraft.LoadoutApolloStackID, "Moon", true,
			func(b *bodies.CelestialBody) (orbital.Vec3, orbital.Vec3) {
				return orbital.Vec3{X: b.RadiusMeters()}, orbital.Vec3{}
			}, false},
		// At the exact instant of apoapsis (vUp == 0) on a low ellipse with
		// periapsis below the surface the predicate still reads true: the craft
		// has not started descending. Mirrors deriveFlightPhase's vUp >= 0.
		{"exactly at apoapsis (vUp == 0)", spacecraft.LoadoutLanderID, "Moon", false,
			func(b *bodies.CelestialBody) (orbital.Vec3, orbital.Vec3) {
				_, ra, a, mu := shallow(b)
				return orbital.Vec3{X: ra}, orbital.Vec3{Y: math.Sqrt(mu * (2/ra - 1/a))}
			}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := sysBody(t, w, c.body)
			cr := spacecraft.NewFromLoadout(c.loadout)
			cr.Primary = *b
			cr.Landed = c.landed
			r, v := c.state(b)
			cr.State = physics.StateVector{R: r, V: v}
			cr.State.M = cr.TotalMass()
			if got := isSubOrbitalClimb(cr); got != c.want {
				t.Errorf("isSubOrbitalClimb = %v, want %v", got, c.want)
			}
		})
	}

	// Robustness only: a hyperbolic/degenerate state (well past escape,
	// radial) must not panic. The answer is not specified by the ADR.
	t.Run("hyperbolic state does not panic", func(t *testing.T) {
		moon := sysBody(t, w, "Moon")
		r := moon.RadiusMeters() + 5_000
		cr := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
		cr.Primary = *moon
		cr.State = physics.StateVector{R: orbital.Vec3{X: r}, V: orbital.Vec3{X: math.Sqrt(2*moon.GravitationalParameter()/r) * 1.5}}
		cr.State.M = cr.TotalMass()
		_ = isSubOrbitalClimb(cr)
	})
}

// TestIsSubOrbitalClimbNilCraft: nil-safe, matching the sibling predicates
// (shouldShowLaunchHUD, shouldShowDescentHUD, craftHasOrbit).
func TestIsSubOrbitalClimbNilCraft(t *testing.T) {
	if isSubOrbitalClimb(nil) {
		t.Error("nil craft: got true, want false")
	}
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
