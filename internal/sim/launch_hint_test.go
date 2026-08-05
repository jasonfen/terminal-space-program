// Package sim — tests for the launch/surface view's DESCENDING hint
// (issue #348 §4, launch_hint.go): the once-per-crossing discipline
// mirroring proximity.go's proximityHint, gated on sim.DescentCorridorFor
// (the #354 gate, not a second descending test).

package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLaunchHintArmsWhileDescending: a trajectory DescentCorridorFor
// forecasts as reaching the ground arms the hint.
func TestLaunchHintArmsWhileDescending(t *testing.T) {
	w, c := dropTestCraft(t, 10_000)
	c.State.V = orbital.Vec3{X: -5} // falling, per TestDescentCorridorForGating

	w.updateLaunchHint()
	if !w.LaunchHintActive() {
		t.Fatal("hint not active for a falling vessel with ground in the forecast")
	}
}

// TestLaunchHintClearsWhenNotDescending: a stable circular orbit never
// arms the hint, and climbing clears an armed one.
func TestLaunchHintClearsWhenNotDescending(t *testing.T) {
	w, c := dropTestCraft(t, 10_000)

	// Circular orbit: falling nowhere.
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	w.updateLaunchHint()
	if w.LaunchHintActive() {
		t.Error("hint active on a stable circular orbit")
	}

	// Arm it by falling...
	c.State.V = orbital.Vec3{X: -5}
	w.updateLaunchHint()
	if !w.LaunchHintActive() {
		t.Fatal("precondition: hint did not arm while falling")
	}

	// ...then climb: the crossing ends and the hint clears.
	c.State.V = orbital.Vec3{X: 200}
	w.updateLaunchHint()
	if w.LaunchHintActive() {
		t.Error("hint still active after climbing clear of the impact forecast")
	}
}

// TestLaunchHintSuppressedInView: the chip advertises a key; inside the
// view it advertises, it has nothing to say — mirrors
// TestProximityHintSuppressedInView.
func TestLaunchHintSuppressedInView(t *testing.T) {
	w, c := dropTestCraft(t, 10_000)
	c.State.V = orbital.Vec3{X: -5}
	w.updateLaunchHint()
	if !w.LaunchHintActive() {
		t.Fatal("precondition: hint not active while falling")
	}
	w.ViewMode = ViewLaunch
	if w.LaunchHintActive() {
		t.Error("hint still active inside the launch/surface view")
	}
}

// TestLaunchHintDismissDiscipline: answering the hint by entering the
// view (ToggleLaunchView) retires it for the rest of THIS descent — even
// after toggling back out while still falling — and a fresh descent
// (the crossing ending, then a new one starting) offers it again. Mirrors
// TestProximityHintHysteresis's once-per-crossing assertions.
func TestLaunchHintDismissDiscipline(t *testing.T) {
	w, c := dropTestCraft(t, 10_000)
	c.State.V = orbital.Vec3{X: -5}
	w.updateLaunchHint()
	if !w.LaunchHintActive() {
		t.Fatal("precondition: hint not active while falling")
	}

	// Answer it.
	if entered, refusal := w.ToggleLaunchView(); !entered || refusal != "" {
		t.Fatalf("enter: entered=%v refusal=%q", entered, refusal)
	}
	w.ToggleLaunchView() // back to the map, still falling
	w.updateLaunchHint()
	if w.LaunchHintActive() {
		t.Error("hint came back after the player acted on it, same descent still in progress")
	}

	// End the crossing (touchdown) and start a fresh one: offered again.
	c.Landed = true
	w.updateLaunchHint()
	c.Landed = false
	c.State.V = orbital.Vec3{X: -5}
	w.updateLaunchHint()
	if !w.LaunchHintActive() {
		t.Error("hint did not re-arm on a fresh descent")
	}
}

// TestLaunchHintDismissSurvivesArming is the review regression for
// finding 6: a dismiss issued BEFORE the crossing arms must still count
// for that crossing.
//
// The state machine used to enter the crossing with a whole-struct
// reassignment — `w.launchHint = launchHint{inside: true}` — which threw
// the dismiss away. A player who pressed [V] a moment before the forecast
// resolved therefore got the chip anyway, on the descent they had already
// answered. proximityHint never had this: it sets `inside` in place and
// clears `dismissed` only when the crossing genuinely ENDS.
func TestLaunchHintDismissSurvivesArming(t *testing.T) {
	w, c := dropTestCraft(t, 10_000)

	// Not falling yet — the crossing has not started, so there is nothing
	// to be inside of.
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	w.updateLaunchHint()
	if w.launchHint.inside {
		t.Fatal("precondition: the crossing armed on a circular orbit")
	}

	// The player enters the surface view first, THEN the descent starts.
	if entered, refusal := w.ToggleLaunchView(); !entered || refusal != "" {
		t.Fatalf("enter: entered=%v refusal=%q", entered, refusal)
	}
	w.ToggleLaunchView() // straight back out to the map
	if !w.launchHint.dismissed {
		t.Fatal("precondition: entering the view did not record a dismiss")
	}

	c.State.V = orbital.Vec3{X: -5}
	w.updateLaunchHint()
	if !w.launchHint.inside {
		t.Fatal("the crossing did not arm once the vessel started falling")
	}
	if !w.launchHint.dismissed {
		t.Error("arming the crossing discarded the dismiss recorded a moment earlier")
	}
	if w.LaunchHintActive() {
		t.Error("the chip re-asked a question the player had already answered")
	}

	// The dismiss is still per-crossing, not permanent: end this one and
	// the next descent offers the hint again.
	c.Landed = true
	w.updateLaunchHint()
	c.Landed = false
	c.State.V = orbital.Vec3{X: -5}
	w.updateLaunchHint()
	if !w.LaunchHintActive() {
		t.Error("the dismiss leaked past the crossing it belonged to")
	}
}

// TestLaunchHintSkipsTheForecastWhenNotFalling is the review regression
// for finding 7. The hint's gate used to call DescentCorridorFor — up to
// ~1000 integrator sub-steps — on EVERY 50 ms tick, measured at ~70 µs
// against an ~81 µs whole tick. Nearly all of that was spent on states
// where the cheap half of the gate already knows the answer.
//
// Asserted structurally (the cheap predicate agrees with the full one on
// every non-falling state, so short-circuiting on it cannot change
// behaviour) rather than by timing, which would be a flaky way to state
// it. BenchmarkUpdateLaunchHint below is where the cost itself is
// watched.
func TestLaunchHintSkipsTheForecastWhenNotFalling(t *testing.T) {
	w, c := dropTestCraft(t, 10_000)
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()

	for _, tc := range []struct {
		name string
		set  func()
	}{
		{"circular", func() { c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)} }},
		{"climbing", func() { c.State.V = orbital.Vec3{X: 200} }},
		{"landed", func() { c.Landed = true }},
		{"crashed", func() { c.Landed = false; c.Crashed = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.Landed, c.Crashed = false, false
			tc.set()
			_, cheap := descentKinematics(c)
			_, full := DescentCorridorFor(c, DescentPredictHorizon)
			if cheap {
				t.Skipf("%s is not a state the cheap gate rejects", tc.name)
			}
			if full {
				t.Errorf("the cheap gate rejected %s but the full gate accepted it — short-circuiting would change behaviour", tc.name)
			}
			w.updateLaunchHint()
			if w.LaunchHintActive() {
				t.Errorf("hint active on %s", tc.name)
			}
		})
	}
}

// BenchmarkUpdateLaunchHint watches the per-tick cost the review measured
// at ~70 µs. The two cases are the ones that matter: a craft that is not
// falling (the overwhelmingly common state, which must now cost a dot
// product) and one that is (which pays for the forecast, but only on
// every launchHintForecastEveryTicks-th tick).
func BenchmarkUpdateLaunchHint(b *testing.B) {
	newWorld := func() (*World, *spacecraft.Spacecraft) {
		w, err := NewWorld()
		if err != nil {
			b.Fatal(err)
		}
		c := w.ActiveCraft()
		c.Landed = false
		c.State.R = orbital.Vec3{X: c.Primary.RadiusMeters() + 400e3}
		c.State.M = c.TotalMass()
		return w, c
	}

	b.Run("circular", func(b *testing.B) {
		w, c := newWorld()
		mu := c.Primary.GravitationalParameter()
		c.State.V = orbital.Vec3{Y: math.Sqrt(mu / c.State.R.Norm())}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w.updateLaunchHint()
		}
	})
	b.Run("falling", func(b *testing.B) {
		w, c := newWorld()
		c.State.V = orbital.Vec3{X: -50}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			w.updateLaunchHint()
		}
	})
}
