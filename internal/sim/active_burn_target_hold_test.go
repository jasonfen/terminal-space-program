package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestActiveBurnHoldsWhenTargetUnresolved — #294 review finding 1. A
// restored in-flight target-relative ActiveBurn whose ghost ref never
// re-latches used to slip through attitudeContext's BurnPrograde
// fallback unnoticed: the MODE degraded, but nothing stopped stepThrust
// from thrusting (and debiting DVRemaining) along that fallback
// direction — real fuel spent in a direction nobody commanded. It must
// instead HOLD: no thrust, no Δv consumed, no fuel burned, EndTime
// paused (mirrors the fuel-stall pause in burn_pause_resume_test.go),
// and LastNodeTargetRefusal stamped so the stall is legible.
func TestActiveBurnHoldsWhenTargetUnresolved(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:             spacecraft.BurnTarget,
		DVRemaining:      500,
		EndTime:          w.Clock.SimTime.Add(2 * time.Minute),
		PrimaryID:        c.Primary.ID,
		Throttle:         1,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}
	w.Ghosts = nil // the ghost never resolves
	dvBefore := c.ActiveBurn.DVRemaining
	endBefore := c.ActiveBurn.EndTime
	fuelBefore := c.ActiveStageFuel()

	for i := 0; i < 10; i++ {
		w.Tick()
	}

	if c.ActiveBurn == nil {
		t.Fatal("held burn was torn down — want it kept alive")
	}
	if c.ActiveBurn.DVRemaining != dvBefore {
		t.Errorf("DVRemaining moved while held: %.3f -> %.3f (want unchanged — no thrust)", dvBefore, c.ActiveBurn.DVRemaining)
	}
	if c.ActiveStageFuel() != fuelBefore {
		t.Errorf("fuel burned while held: %.6f -> %.6f", fuelBefore, c.ActiveStageFuel())
	}
	if !c.ActiveBurn.EndTime.After(endBefore) {
		t.Errorf("EndTime not advanced while held (%v -> %v) — duration window must pause", endBefore, c.ActiveBurn.EndTime)
	}
	if w.LastNodeTargetRefusal == nil {
		t.Error("LastNodeTargetRefusal not stamped while the burn was held")
	}
	if w.LastNodeTargetRefusal != nil && w.LastNodeTargetRefusal.Cancelled {
		t.Error("a merely-pending hold must not read as Cancelled")
	}
}

// TestActiveBurnResumesOnceTargetResolves — the flip side: once the
// ghost re-latches, the held burn resumes thrusting and its Δv falls
// normally, proving the hold is a pause, not a silent stall forever.
func TestActiveBurnResumesOnceTargetResolves(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:             spacecraft.BurnTarget,
		DVRemaining:      500,
		EndTime:          w.Clock.SimTime.Add(5 * time.Minute),
		PrimaryID:        c.Primary.ID,
		Throttle:         1,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}
	w.Ghosts = nil
	w.Tick()
	if c.ActiveBurn == nil || c.ActiveBurn.DVRemaining != 500 {
		t.Fatalf("burn thrust (or vanished) while target unresolved: %+v", c.ActiveBurn)
	}

	// The ghost's report resumes.
	w.Ghosts = []Ghost{{
		Owner: "SHA256:gern", CraftID: 987654, PrimaryID: c.Primary.ID,
		Pos: w.BodyPosition(c.Primary).Add(c.State.R).Add(orbital.Vec3{X: 1e6}),
		Vel: c.State.V,
	}}

	done := false
	for i := 0; i < 20; i++ {
		w.Tick()
		if c.ActiveBurn == nil || c.ActiveBurn.DVRemaining != 500 {
			done = true
			break
		}
	}
	if !done {
		t.Fatal("burn never resumed thrusting after the target resolved")
	}
	if c.ActiveBurn == nil {
		t.Fatal("burn vanished right as it resumed — want it to keep running")
	}
	if c.ActiveBurn.DVRemaining >= 500 {
		t.Errorf("DVRemaining did not fall after the target resolved: %.3f", c.ActiveBurn.DVRemaining)
	}
}
