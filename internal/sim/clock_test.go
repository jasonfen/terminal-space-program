package sim

import (
	"testing"
	"time"
)

// TestClockAdvanceLowWarpRotationTracksSim: at warp ≤
// RotationCapWarp, SimTime and RotationTime advance in lockstep.
func TestClockAdvanceLowWarpRotationTracksSim(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClock(start, time.Second)
	c.WarpIdx = 3 // 1000× — at or below the cap (10000×)
	c.Advance()
	if !c.RotationTime.Equal(c.SimTime) {
		t.Errorf("at warp 1000×, RotationTime %v != SimTime %v",
			c.RotationTime, c.SimTime)
	}
	expectedDelta := time.Duration(1000 * float64(time.Second))
	if c.SimTime.Sub(start) != expectedDelta {
		t.Errorf("SimTime advance = %v, want %v",
			c.SimTime.Sub(start), expectedDelta)
	}
}

// TestClockAdvanceHighWarpRotationCaps: at warp > RotationCapWarp,
// SimTime advances at the requested warp; RotationTime advances at
// the cap.
func TestClockAdvanceHighWarpRotationCaps(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClock(start, time.Second)
	c.WarpIdx = 5 // 100000× — well above the cap (10000×)
	c.Advance()
	wantSim := time.Duration(100000 * float64(time.Second))
	wantRot := time.Duration(RotationCapWarp * float64(time.Second))
	if c.SimTime.Sub(start) != wantSim {
		t.Errorf("SimTime = +%v, want +%v", c.SimTime.Sub(start), wantSim)
	}
	if c.RotationTime.Sub(start) != wantRot {
		t.Errorf("RotationTime = +%v, want +%v",
			c.RotationTime.Sub(start), wantRot)
	}
}

// TestClockAdvanceLagPersists: dropping back to low warp doesn't
// catch RotationTime up — the lag accrued at high warp persists.
// This is the documented side-effect of the "freeze rotation at
// high warp" model.
func TestClockAdvanceLagPersists(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClock(start, time.Second)
	c.WarpIdx = 5 // 100000×
	c.Advance()
	lagAfterHigh := c.SimTime.Sub(c.RotationTime)
	if lagAfterHigh <= 0 {
		t.Fatalf("expected RotationTime to lag SimTime, got lag %v", lagAfterHigh)
	}
	// Drop to warp 1× and tick.
	c.WarpIdx = 0
	c.Advance()
	lagAfterLow := c.SimTime.Sub(c.RotationTime)
	if lagAfterLow != lagAfterHigh {
		t.Errorf("warp drop to 1×: lag changed from %v to %v (should persist)",
			lagAfterHigh, lagAfterLow)
	}
}

// TestClockAdvancePausedFreezesBoth: paused clock leaves both
// SimTime and RotationTime unchanged.
func TestClockAdvancePausedFreezesBoth(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClock(start, time.Second)
	c.Paused = true
	c.WarpIdx = 5
	c.Advance()
	if !c.SimTime.Equal(start) {
		t.Errorf("paused: SimTime advanced to %v", c.SimTime)
	}
	if !c.RotationTime.Equal(start) {
		t.Errorf("paused: RotationTime advanced to %v", c.RotationTime)
	}
}
