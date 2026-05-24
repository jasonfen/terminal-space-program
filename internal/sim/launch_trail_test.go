// Package sim — v0.11.0 Slice 1 regression suite for the
// LaunchTrail breadcrumb sampler. Tests exercise the 1 s sim-time
// cadence, the 256-point FIFO cap, and survival across a manual
// cycle out + back. Clearing-on-route / clearing-on-release /
// clearing-on-hand-off are already covered by launch_route_test.go.
package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLaunchTrailFirstSampleOnSessionOpen — tracer: a single tick
// after a route opens a session produces exactly one trail sample
// (the empty-buffer special case in the cadence rule). The sample's
// SampledAt matches the post-tick sim-time.
func TestLaunchTrailFirstSampleOnSessionOpen(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}

	w.Tick()

	if !w.LaunchSessionActive {
		t.Fatalf("setup: route didn't open a session")
	}
	if len(w.LaunchTrail) != 1 {
		t.Fatalf("LaunchTrail len = %d, want 1 (empty-buffer first sample)",
			len(w.LaunchTrail))
	}
	p := w.LaunchTrail[0]
	if !p.SampledAt.Equal(w.Clock.SimTime) {
		t.Errorf("SampledAt = %v, want %v (post-tick sim-time)",
			p.SampledAt, w.Clock.SimTime)
	}
}

// TestLaunchTrailCadence — after the first empty-buffer sample, the
// next sample requires ≥1 s of sim-time to have elapsed. Warp up so
// one tick advances sim-time by 5 s, then verify exactly one extra
// sample appears per subsequent tick.
func TestLaunchTrailCadence(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Warp 100× → 5 s per tick (BaseStep 50 ms × 100). Comfortably
	// above the 1 s cadence so each post-route tick produces a sample.
	w.Clock.WarpUp() // 10×
	w.Clock.WarpUp() // 100×

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}

	w.Tick() // route + first sample.
	if len(w.LaunchTrail) != 1 {
		t.Fatalf("after route: trail = %d, want 1", len(w.LaunchTrail))
	}
	w.Tick() // gap = 5 s ≥ 1 s → second sample.
	if len(w.LaunchTrail) != 2 {
		t.Errorf("after second tick (5 s gap): trail = %d, want 2", len(w.LaunchTrail))
	}
	w.Tick() // third sample.
	if len(w.LaunchTrail) != 3 {
		t.Errorf("after third tick: trail = %d, want 3", len(w.LaunchTrail))
	}
}

// TestLaunchTrailFIFOCap — the FIFO cap (256) holds across overflow.
// Pre-fills the trail to capacity with synthetic older samples, then
// ticks once to force an append; the trail must stay at exactly 256
// and the new sample must be at the end (oldest evicted from front).
func TestLaunchTrailFIFOCap(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Clock.WarpUp() // 10×
	w.Clock.WarpUp() // 100× — 5 s/tick, comfortably over the cadence gate.

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}

	w.Tick() // route + first real sample.
	realSample := w.LaunchTrail[0]

	// Pre-fill the front with older synthetic samples so the trail
	// is exactly at the cap before the next sampler call.
	for len(w.LaunchTrail) < launchTrailCap {
		oldest := w.LaunchTrail[0]
		filler := oldest
		filler.SampledAt = oldest.SampledAt.Add(-time.Second)
		w.LaunchTrail = append([]TrailPoint{filler}, w.LaunchTrail...)
	}
	if len(w.LaunchTrail) != launchTrailCap {
		t.Fatalf("setup: pre-fill produced %d samples, want %d", len(w.LaunchTrail), launchTrailCap)
	}

	w.Tick() // 5 s gap → appends a new sample, FIFO evicts oldest.

	if len(w.LaunchTrail) != launchTrailCap {
		t.Errorf("after FIFO overflow: trail = %d, want exactly %d (cap)",
			len(w.LaunchTrail), launchTrailCap)
	}
	// The pre-existing real sample should still be present somewhere
	// in the trail (we evicted only the oldest synthetic). Verify by
	// checking the second-to-last sample's SampledAt matches it.
	if w.LaunchTrail[launchTrailCap-2].SampledAt != realSample.SampledAt {
		t.Errorf("real sample evicted unexpectedly — eviction order wrong")
	}
}

// TestLaunchTrailSurvivesManualCycleOut — manual `v` cycle out of
// ViewLaunch clears the sentinel but must NOT clear LaunchTrail.
// Player intuition: cycle out to OrbitView mid-ascent to read
// instruments, cycle back to ViewLaunch and the breadcrumbs are
// still there. Locks the asymmetry vs auto-release / hand-off,
// which DO clear.
func TestLaunchTrailSurvivesManualCycleOut(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Clock.WarpUp()
	w.Clock.WarpUp() // 100×

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}

	w.Tick() // 1 sample.
	w.Tick() // 2 samples.
	w.Tick() // 3 samples.
	nBefore := len(w.LaunchTrail)
	if nBefore < 2 {
		t.Fatalf("setup: expected ≥2 trail samples, got %d", nBefore)
	}

	w.CycleViewMode()

	if w.LaunchSessionActive {
		t.Error("setup invariant: manual cycle should have cleared session sentinel")
	}
	if len(w.LaunchTrail) != nBefore {
		t.Errorf("trail cleared by manual cycle: %d → %d, want %d preserved",
			nBefore, len(w.LaunchTrail), nBefore)
	}
}
