package sim

import (
	"errors"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestStageActiveOnSaturnVPopsBottomStage — Saturn-V starts as a
// 3-stage craft. Pressing space jettisons S-IC; the active craft
// becomes 2-stage (S-II + S-IVB). A new passive craft appears at
// the end of the slate carrying the dropped S-IC's residual fuel +
// position/velocity.
func TestStageActiveOnSaturnVPopsBottomStage(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Replace the default LEO craft with a Saturn-V to exercise the
	// 3-stage path. NewFromLoadout handles Stages population.
	saturn := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	saturn.Primary = w.Crafts[0].Primary
	saturn.State = w.Crafts[0].State
	w.Crafts[0] = saturn
	w.ActiveCraftIdx = 0

	beforeStageCount := len(saturn.Stages)
	beforeBottomFuel := saturn.Stages[0].FuelMass
	beforePos := saturn.State.R
	beforeVel := saturn.State.V

	newActive, jettIdx, err := w.StageActive(0)
	if err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if newActive != 0 {
		t.Errorf("newActive: got %d, want 0 (active stays in place)", newActive)
	}
	active := w.Crafts[newActive]
	if len(active.Stages) != beforeStageCount-1 {
		t.Errorf("active stage count: got %d, want %d (one popped)",
			len(active.Stages), beforeStageCount-1)
	}
	if jettIdx != len(w.Crafts)-1 {
		t.Errorf("jettisoned idx: got %d, want %d (end of slate)",
			jettIdx, len(w.Crafts)-1)
	}
	jett := w.Crafts[jettIdx]
	if len(jett.Stages) != 1 {
		t.Errorf("jettisoned single-stage: got %d", len(jett.Stages))
	}
	if jett.Stages[0].FuelMass != beforeBottomFuel {
		t.Errorf("jettisoned fuel: got %.0f, want %.0f (residual from active)",
			jett.Stages[0].FuelMass, beforeBottomFuel)
	}
	if jett.State.R != beforePos {
		t.Errorf("jettisoned position should match pre-decouple: got %v, want %v",
			jett.State.R, beforePos)
	}
	if jett.State.V != beforeVel {
		t.Errorf("jettisoned velocity should match pre-decouple")
	}
	if jett.Throttle != 0 {
		t.Errorf("jettisoned should be passive (Throttle=0): got %v", jett.Throttle)
	}
}

// TestStageActiveSingleStageRefuses — single-stage craft (default
// S-IVB-1 LEO spawn) has no lower stage to drop. StageActive must
// return ErrStageOnlyOne and leave the slate unchanged.
func TestStageActiveSingleStageRefuses(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if len(w.Crafts[0].Stages) != 1 {
		t.Fatalf("default craft should be single-stage, got %d stages",
			len(w.Crafts[0].Stages))
	}
	beforeSlate := len(w.Crafts)
	_, _, err = w.StageActive(0)
	if !errors.Is(err, ErrStageOnlyOne) {
		t.Errorf("StageActive on single-stage craft: got %v, want ErrStageOnlyOne", err)
	}
	if len(w.Crafts) != beforeSlate {
		t.Errorf("slate size changed after refused stage: got %d, want %d",
			len(w.Crafts), beforeSlate)
	}
}

// TestStageActiveBadIdx — out-of-range craftIdx returns ErrStageNoCraft.
func TestStageActiveBadIdx(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, _, err := w.StageActive(99); !errors.Is(err, ErrStageNoCraft) {
		t.Errorf("StageActive(99): got %v, want ErrStageNoCraft", err)
	}
	if _, _, err := w.StageActive(-1); !errors.Is(err, ErrStageNoCraft) {
		t.Errorf("StageActive(-1): got %v, want ErrStageNoCraft", err)
	}
}

// TestStageActiveAdvancesEngineToNextStage — after staging a Saturn-V,
// the new bottom stage (S-II) becomes the firing engine. Flat
// Thrust + Isp fields reflect S-II numbers.
func TestStageActiveAdvancesEngineToNextStage(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	saturn := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	saturn.Primary = w.Crafts[0].Primary
	saturn.State = w.Crafts[0].State
	w.Crafts[0] = saturn
	w.ActiveCraftIdx = 0

	// S-IC bottom stage thrust pre-stage.
	if saturn.Thrust != 35100000 {
		t.Errorf("pre-stage S-IC thrust: got %.0f, want 35,100,000", saturn.Thrust)
	}
	if _, _, err := w.StageActive(0); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	// After staging, S-II should be firing — 5,140 kN thrust.
	active := w.Crafts[0]
	if active.Thrust != 5140000 {
		t.Errorf("post-stage thrust (S-II): got %.0f, want 5,140,000", active.Thrust)
	}
	if active.Isp != 421 {
		t.Errorf("post-stage Isp (S-II): got %.0f, want 421", active.Isp)
	}
}
