// Package sim — v0.11.4-followup regression for the airless-body
// surface-impact Kepler-step bypass.

package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestCanKeplerStepRejectsSubSurfacePeriapsisOnAirlessBody — playtest
// regression: a Lander on the Moon descending through 5 km altitude
// with ~100 m/s surface velocity has a sub-surface periapsis
// (impactor trajectory). At warp >1× with no engine fire, the
// analytic Kepler fast path took over and propagated the craft
// straight through the surface — the sub-step loop (which dispatches
// ClampToSurface) was skipped entirely. Altitude went negative,
// craft "sucked into" the moon, lifecycle predicate never fired.
//
// Fix: canKeplerStep rejects any orbit with periapsis below the
// primary's mean radius, airless or not, so Verlet handles the
// terminal descent and ClampToSurface gets a chance to classify
// the contact.
func TestCanKeplerStepRejectsSubSurfacePeriapsisOnAirlessBody(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("Moon not in default system")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	moonR := moon.RadiusMeters()
	// Place the craft at 5 km altitude, descending at 100 m/s
	// surface velocity (mostly horizontal in the playtest report,
	// but the impactor periapsis comes from any state where
	// peri < surface). Build the state inline.
	c.State = physics.StateVector{
		R: orbital.Vec3{X: moonR + 5_000, Y: 0, Z: 0},
		V: orbital.Vec3{X: 0, Y: 100, Z: 0},
		M: c.TotalMass(),
	}
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0

	// Confirm peri is actually sub-surface for this state — if
	// not, the test setup is wrong.
	mu := moon.GravitationalParameter()
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	periAlt := el.A*(1-el.E) - moonR
	if periAlt >= 0 {
		t.Fatalf("setup: expected sub-surface periapsis, got periAlt=%.0f m", periAlt)
	}

	// Crank to warp >1× so the Kepler-step gate's warp clause passes.
	w.Clock.WarpUp() // 10×
	if w.Clock.Warp() <= 1 {
		t.Fatalf("setup: warp should be > 1×, got %v", w.Clock.Warp())
	}

	if w.canKeplerStep(c, 5*time.Second) {
		t.Errorf("canKeplerStep returned true on airless-body impactor "+
			"(periAlt=%.0f m) — analytic propagation would skip the "+
			"sub-step loop and pass the craft through the surface", periAlt)
	}
}

// TestLunarDescentWithThrottleCyclingNeverGoesSubSurface — playtest
// regression. Player decelerating toward the moon at ~100 m/s
// surface velocity from 5 km altitude, cycling the engine off/on
// at 10% throttle, reported the craft suddenly "sucked into the
// moon" with altitude continuing negative. The fix gates the
// analytic Kepler fast path against sub-surface periapsis at
// every chunk boundary (not just at the start of the tick), and
// rewinds c.State on bail so the Verlet fallback doesn't
// double-step from a partially-advanced state.
//
// Pin: across a realistic descent simulation with throttle cycled
// every few ticks, |R| must never fall meaningfully below the
// primary's mean radius — the only sub-radius reading allowed is
// the transient float-precision delta inside a clamped state.
func TestLunarDescentWithThrottleCyclingNeverGoesSubSurface(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("Moon not in default system")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	moonR := moon.RadiusMeters()
	// User's reported state: 5 km altitude, ~100 m/s surface
	// velocity (mostly horizontal — descent rate is small).
	c.State = physics.StateVector{
		R: orbital.Vec3{X: moonR + 5_000, Y: 0, Z: 0},
		V: orbital.Vec3{X: -5, Y: 100, Z: 0},
		M: c.TotalMass(),
	}
	// Nose along radial-out for the predicate's nose-alignment
	// branch, AttitudeMode surface-retrograde to reflect the
	// player's "deceleration burn" framing.
	c.CurrentAttitudeDir = orbital.Vec3{X: 1}
	c.AttitudeMode = spacecraft.BurnSurfaceRetrograde
	c.Throttle = 0.1
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	// 100× — the high-warp descent regime where the bug surfaces.
	// The pre-fix code took an analytic Kepler step covering ~5 s
	// of sim time in a single tick, leaping past the surface
	// without invoking ClampToSurface.
	w.Clock.WarpUp() // 10
	w.Clock.WarpUp() // 100

	minAlt := math.Inf(1)
	const tolerance = 5.0 // metres below mean radius is acceptable (float slack)
	for i := 0; i < 300; i++ {
		// Cycle the engine — five ticks on, five ticks off — to
		// mirror the player's "near coasting off/on" framing.
		if i%10 < 5 {
			w.StartManualBurn()
		} else {
			w.StopManualBurn()
		}
		w.Tick()
		alt := c.State.R.Norm() - moonR
		if alt < minAlt {
			minAlt = alt
		}
		if c.Landed || c.Crashed {
			break
		}
	}
	if minAlt < -tolerance {
		t.Errorf("descent allowed |R| to fall %.0f m below mean radius — "+
			"Kepler fast path bypassed surface contact (sucked into moon bug)",
			-minAlt)
	}
}

// TestImpactorTrajectoryHitsSurfacePredicate — end-to-end pin: a
// Lander on impactor trajectory toward the Moon, ticked at warp,
// reaches the surface and gets classified by the lifecycle
// predicate (either Crashed or Landed depending on V_impact and
// nose alignment). Pre-fix, the craft would Kepler-step past the
// surface without ever firing ClampToSurface — neither flag would
// be set and altitude would go arbitrarily negative.
//
// The exact outcome (Crashed vs Landed) isn't the point — what
// matters is that the predicate runs at all. Asserts only that one
// of {Landed, Crashed} is true after the descent.
func TestImpactorTrajectoryHitsSurfacePredicate(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("Moon not in default system")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	moonR := moon.RadiusMeters()
	// 2 km altitude, descending vertically at 20 m/s — clearly an
	// impactor trajectory that should fire ClampToSurface within a
	// handful of ticks.
	c.State = physics.StateVector{
		R: orbital.Vec3{X: moonR + 2_000, Y: 0, Z: 0},
		V: orbital.Vec3{X: -20, Y: 0, Z: 0},
		M: c.TotalMass(),
	}
	// Nose along world +X (radial / local-up at this position) so
	// if the kinematic checks happen to qualify, the soft-land
	// branch is reachable — but the V_impact gate at terminal V > 10
	// m/s ensures Crashed is the most likely outcome.
	c.CurrentAttitudeDir = orbital.Vec3{X: 1}
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	w.Clock.WarpUp() // 10×

	// Tick until the predicate fires (Landed or Crashed true). With
	// BaseStep=50ms × 10× warp = 0.5 s/tick, a 2 km descent at
	// terminal velocity ~50 m/s should resolve within ~80 ticks;
	// cap at 200 so a future regression doesn't loop forever.
	for i := 0; i < 200; i++ {
		w.Tick()
		if c.Landed || c.Crashed {
			break
		}
	}
	if !c.Landed && !c.Crashed {
		altM := c.State.R.Norm() - moonR
		t.Errorf("after 200 ticks of impactor descent: Landed=%v Crashed=%v altitude=%.0f m — "+
			"surface-arrival predicate never fired (Kepler-step bypassed surface)",
			c.Landed, c.Crashed, altM)
	}
	// |R| must not be arbitrarily below the surface — clamp clamps
	// to exactly radius. Allow 1 m slack for floating point.
	if math.Abs(c.State.R.Norm()-moonR) > 1 {
		t.Errorf("post-contact |R|=%.0f, want ≈ moon radius %.0f (clamp didn't fire)",
			c.State.R.Norm(), moonR)
	}
}
