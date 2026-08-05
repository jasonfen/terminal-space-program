// Package sim — tests for the launch/surface view's DESCENDING hint
// (issue #348 §4, launch_hint.go): the once-per-crossing discipline
// mirroring proximity.go's proximityHint, gated on sim.DescentCorridorFor
// (the #354 gate, not a second descending test).

package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
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
