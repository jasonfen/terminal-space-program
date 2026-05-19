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
	// v0.9.4+: active craft renames to the new bottom stage (S-II
	// after S-IC drops) — the loadout-level "Saturn V" name no
	// longer matches reality once stages decouple.
	if active.Name != active.Stages[0].Name {
		t.Errorf("active name after staging: got %q, want %q (new bottom stage's name)",
			active.Name, active.Stages[0].Name)
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
	// v0.9.1.1+: jettisoned stage spawns offset retrograde from the
	// active craft so checkDocking doesn't immediately re-fuse the
	// pair. Position offset must exceed DockingDistM (50 m); velocity
	// offset must exceed DockingVMS (0.1 m/s).
	posDelta := jett.State.R.Sub(beforePos).Norm()
	if posDelta <= 50 {
		t.Errorf("jettisoned position offset = %.1f m, want > 50 m (outside DockingDistM)", posDelta)
	}
	velDelta := jett.State.V.Sub(beforeVel).Norm()
	if velDelta <= 0.1 {
		t.Errorf("jettisoned velocity offset = %.3f m/s, want > 0.1 m/s (outside DockingVMS)", velDelta)
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

// TestStageActiveDoesNotImmediatelyReDock — regression for the
// v0.9.1 staging bug: pre-fix, the jettisoned stage spawned at
// the active craft's exact (R, V), inside both docking gates
// (DockingDistM=50 m, DockingVMS=0.1 m/s), and the very next tick
// fused the pair right back into one craft.
//
// Verify that after StageActive, running the docking proximity
// check leaves the slate at 2 craft (active + jettisoned) and not
// 1 (re-fused composite).
func TestStageActiveDoesNotImmediatelyReDock(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	saturn := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	saturn.Primary = w.Crafts[0].Primary
	saturn.State = w.Crafts[0].State
	w.Crafts[0] = saturn
	w.ActiveCraftIdx = 0

	if _, _, err := w.StageActive(0); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("post-stage slate count: got %d, want 2", len(w.Crafts))
	}
	// Run checkDocking explicitly — pre-fix this would fuse the pair.
	w.checkDocking()
	if len(w.Crafts) != 2 {
		t.Errorf("post-stage + post-checkDocking slate count: got %d, want 2 "+
			"(jettisoned stage was re-fused into active — staging separation "+
			"insufficient to clear DockingDistM/DockingVMS gates)",
			len(w.Crafts))
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

// TestApolloStackDecoupleChainLeavesCSM — the v0.10.1 Apollo-Stack
// is [S-IC, S-II, S-IVB, LM, CSM]. Four decouples drop S-IC → S-II
// → S-IVB → LM; the LM jettison spawns a separate controllable
// slate craft (payload separation), and the active craft is left
// as the single-stage CSM core. The fifth decouple refuses
// (ErrStageOnlyOne — can't drop the only/last stage).
func TestApolloStackDecoupleChainLeavesCSM(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	if len(stack.Stages) != 5 {
		t.Fatalf("Apollo-Stack should start 5-stage, got %d", len(stack.Stages))
	}

	wantDropped := []string{"S-IC", "S-II", "S-IVB", "LM"}
	for i, name := range wantDropped {
		_, jettIdx, err := w.StageActive(0)
		if err != nil {
			t.Fatalf("decouple %d (%s): %v", i, name, err)
		}
		jett := w.Crafts[jettIdx]
		if jett.Stages[0].Name != name {
			t.Errorf("decouple %d dropped %q, want %q", i, jett.Stages[0].Name, name)
		}
		// LM separation must yield a real, distinct slate craft the
		// player can switch to and fly (payload separation).
		if name == "LM" && jett.Stages[0].Thrust <= 0 {
			t.Errorf("separated LM has no engine (Thrust=%v) — not controllable",
				jett.Stages[0].Thrust)
		}
	}

	core := w.Crafts[0]
	if len(core.Stages) != 1 || core.Stages[0].Name != "CSM" {
		t.Fatalf("surviving core: %d stage(s) named %q, want 1× CSM",
			len(core.Stages), core.Stages[0].Name)
	}
	if _, _, err := w.StageActive(0); !errors.Is(err, ErrStageOnlyOne) {
		t.Errorf("dropping the CSM core: err = %v, want ErrStageOnlyOne", err)
	}
}
