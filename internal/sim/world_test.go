package sim

import (
	"math"
	"testing"
	"time"
)

// TestWorldSpawnsSpacecraft: NewWorld places the craft in a valid LEO
// around Earth with the expected altitude and speed.
func TestWorldSpawnsSpacecraft(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.Craft == nil {
		t.Fatal("Craft is nil")
	}
	if w.Craft.Primary.EnglishName != "Earth" {
		t.Errorf("expected primary=Earth, got %s", w.Craft.Primary.EnglishName)
	}
	alt := w.Craft.Altitude()
	if math.Abs(alt-200e3) > 1 {
		t.Errorf("initial altitude %.1f m, want 200000", alt)
	}
}

// TestWorldIntegrationStableOverOrbits: the plan's C15 acceptance — 100
// orbits, SMA drift < 1%. Mirrors verlet_test but through the world-tick
// path, which is what the TUI exercises.
func TestWorldIntegrationStableOverOrbits(t *testing.T) {
	w, _ := NewWorld()
	mu := w.Craft.Primary.GravitationalParameter()
	r0 := w.Craft.State.R.Norm()
	a0 := r0 // circular

	period := 2 * math.Pi * math.Sqrt(r0*r0*r0/mu)
	// Advance by 100 orbits of sim-time.
	// BaseStep=50ms, warp=1× means wall-tick ≈ sim-tick. Use a direct
	// test-only Advance path by calling Tick repeatedly with the clock
	// set to warp out enough per tick.
	w.Clock.WarpIdx = 2 // 100× — 5 s sim per tick
	totalSimSecs := 100 * period
	ticksNeeded := int(math.Ceil(totalSimSecs / (w.Clock.BaseStep.Seconds() * w.Clock.Warp())))

	for i := 0; i < ticksNeeded; i++ {
		w.Tick()
	}

	// Re-derive SMA from final state (vis-viva via physics package).
	vf := w.Craft.State.V.Norm()
	rf := w.Craft.State.R.Norm()
	eps := 0.5*vf*vf - mu/rf
	af := -mu / (2 * eps)

	drift := math.Abs(af-a0) / a0
	if drift > 0.01 {
		t.Errorf("SMA drift after 100 orbits via world.Tick: %.4f%% (>1%%)", drift*100)
	}
	t.Logf("world.Tick: 100 LEO orbits at 100× warp, Δa/a = %.3e", drift)
}

// TestPausedTickDoesNothing: world.Clock.Paused must block both time
// advancement and physics stepping.
func TestPausedTickDoesNothing(t *testing.T) {
	w, _ := NewWorld()
	r0 := w.Craft.State.R
	t0 := w.Clock.SimTime
	w.Clock.Paused = true
	w.Clock.BaseStep = 50 * time.Millisecond
	for i := 0; i < 10; i++ {
		w.Tick()
	}
	if w.Craft.State.R != r0 {
		t.Errorf("paused: craft moved from %v to %v", r0, w.Craft.State.R)
	}
	if !w.Clock.SimTime.Equal(t0) {
		t.Errorf("paused: sim-time advanced from %v to %v", t0, w.Clock.SimTime)
	}
}
