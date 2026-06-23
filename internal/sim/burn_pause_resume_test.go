package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// twoStageBurner replaces the active craft's stages with a tiny lower
// stage (dries almost immediately) atop a well-fuelled upper stage, and
// plants a running prograde finite burn whose Δv far exceeds what the
// lower stage can deliver. Returns the craft. Shared by the pause/resume
// tests.
func twoStageBurner(t *testing.T, w *World, dvRemaining float64) *spacecraft.Spacecraft {
	t.Helper()
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	c.Stages = []spacecraft.Stage{
		{Name: "lower", DryMass: 500, FuelMass: 20, FuelCapacity: 20, Thrust: 200000, Isp: 250},
		{Name: "upper", DryMass: 800, FuelMass: 4000, FuelCapacity: 4000, Thrust: 200000, Isp: 300},
	}
	crewTend(c) // synthetic stages carry no command source; crew-tend so commands aren't comms-gated
	c.SyncFields()
	c.State.M = c.TotalMass()
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: dvRemaining,
		EndTime:     w.Clock.SimTime.Add(10 * time.Minute),
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
	}
	return c
}

// tickUntil advances the world until pred holds or maxTicks elapse;
// returns the tick count and whether pred was satisfied.
func tickUntil(w *World, maxTicks int, pred func() bool) (int, bool) {
	for i := 1; i <= maxTicks; i++ {
		w.Tick()
		if pred() {
			return i, true
		}
	}
	return maxTicks, false
}

// TestBurnStallsWhenStageDriesNotCancelled: a planted burn whose firing
// stage runs dry with Δv still owed must NOT be torn down — it stalls
// (BurnStalled), keeping its remaining Δv, instead of silently
// cancelling. This is the v0.12.x pause-and-resume contract; pre-fix the
// burn was discarded the moment ActiveStageFuel hit zero.
func TestBurnStallsWhenStageDriesNotCancelled(t *testing.T) {
	w := mustWorld(t)
	c := twoStageBurner(t, w, 200)

	_, ok := tickUntil(w, 200, func() bool { return c.ActiveStageFuel() <= 0 })
	if !ok {
		t.Fatal("lower stage never ran dry within 200 ticks")
	}
	if c.ActiveBurn == nil {
		t.Fatal("ActiveBurn was torn down on fuel exhaustion — want it kept alive (stalled)")
	}
	if !c.BurnStalled() {
		t.Errorf("BurnStalled() = false, want true (stage dry, Δv owed)")
	}
	if c.ActiveBurn.DVRemaining <= 0 {
		t.Errorf("DVRemaining = %.1f, want > 0 (tiny lower stage can't deliver 200 m/s)", c.ActiveBurn.DVRemaining)
	}
}

// TestStalledBurnPausesDurationWindow: while stalled, the burn delivers
// no Δv and its EndTime is pushed forward each tick, so the duration
// window is paused (not consumed) and the burn won't time out before the
// player gets a chance to stage.
func TestStalledBurnPausesDurationWindow(t *testing.T) {
	w := mustWorld(t)
	c := twoStageBurner(t, w, 200)

	if _, ok := tickUntil(w, 200, func() bool { return c.BurnStalled() }); !ok {
		t.Fatal("burn never stalled within 200 ticks")
	}
	dvAtStall := c.ActiveBurn.DVRemaining
	endAtStall := c.ActiveBurn.EndTime

	for i := 0; i < 20; i++ {
		w.Tick()
	}
	if c.ActiveBurn == nil {
		t.Fatal("stalled burn was torn down while coasting — want it kept alive")
	}
	if c.ActiveBurn.DVRemaining != dvAtStall {
		t.Errorf("DVRemaining moved while stalled: %.3f → %.3f (want unchanged — no thrust)", dvAtStall, c.ActiveBurn.DVRemaining)
	}
	if !c.ActiveBurn.EndTime.After(endAtStall) {
		t.Errorf("EndTime not advanced while stalled (%v → %v) — duration window must pause", endAtStall, c.ActiveBurn.EndTime)
	}
}

// TestBurnResumesAfterStaging: once the spent lower stage is decoupled,
// the now-fuelled upper stage picks up the same burn automatically —
// DVRemaining resumes falling and the burn completes (ActiveBurn cleared)
// without re-planting. This is the payoff: a multi-stage burn (e.g. an
// Apollo TLI the S-IVB can't finish alone) carries across staging.
func TestBurnResumesAfterStaging(t *testing.T) {
	w := mustWorld(t)
	c := twoStageBurner(t, w, 200)

	if _, ok := tickUntil(w, 200, func() bool { return c.BurnStalled() }); !ok {
		t.Fatal("burn never stalled within 200 ticks")
	}
	dvAtStall := c.ActiveBurn.DVRemaining

	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	c = w.ActiveCraft() // active idx unchanged, but re-fetch defensively
	if c.ActiveBurn == nil {
		t.Fatal("staging dropped the ActiveBurn — it must survive decouple to resume")
	}
	if c.ActiveStageFuel() <= 0 {
		t.Fatal("upper stage has no fuel after staging — test setup wrong")
	}

	// Thrust should resume: burn runs to completion (DVRemaining → 0,
	// ActiveBurn cleared) within a bounded number of ticks.
	_, done := tickUntil(w, 600, func() bool { return c.ActiveBurn == nil })
	if !done {
		t.Errorf("burn did not complete after staging (DVRemaining stuck at %.1f, started resume at %.1f)",
			func() float64 {
				if c.ActiveBurn != nil {
					return c.ActiveBurn.DVRemaining
				}
				return 0
			}(), dvAtStall)
	}
}

// TestThrottleCutAbortsStalledBurn: throttle-cut (x) clears a stalled
// burn — the only way to abandon a transfer the spent stage couldn't
// finish, and required so a dangling stalled burn doesn't permanently
// block StartManualBurn.
func TestThrottleCutAbortsStalledBurn(t *testing.T) {
	w := mustWorld(t)
	c := twoStageBurner(t, w, 200)
	if _, ok := tickUntil(w, 200, func() bool { return c.BurnStalled() }); !ok {
		t.Fatal("burn never stalled within 200 ticks")
	}

	w.SetThrottle(0)
	if c.ActiveBurn != nil {
		t.Errorf("throttle-cut left a stalled ActiveBurn in place — want it aborted")
	}
}

// TestThrottleCutLeavesRunningBurn: throttle-cut must NOT cancel a
// normally-running planted burn (fuel aboard, thrusting) — only the
// stalled state is cancellable. Preserves the long-standing "planted
// burns keep running through x" behaviour.
func TestThrottleCutLeavesRunningBurn(t *testing.T) {
	w := mustWorld(t)
	c := twoStageBurner(t, w, 200)
	// One tick: lower stage still has fuel, burn is running (not stalled).
	w.Tick()
	if c.BurnStalled() {
		t.Skip("lower stage dried in a single tick; can't exercise the running case")
	}
	if c.ActiveBurn == nil {
		t.Fatal("running burn unexpectedly torn down")
	}

	w.SetThrottle(0)
	if c.ActiveBurn == nil {
		t.Errorf("throttle-cut cancelled a running planted burn — want it left running")
	}
}
