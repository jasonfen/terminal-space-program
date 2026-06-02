package sim

import (
	"errors"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// stageNames returns the Name of each stage, for readable failure output.
func stageNames(stages []spacecraft.Stage) []string {
	out := make([]string, len(stages))
	for i, s := range stages {
		out[i] = s.Name
	}
	return out
}

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

// TestApolloStackManualFlipDropsLMAsOneCraft — regression for the
// LM-splits-when-staging playtest bug. At the pre-transposition stack
// [Descent, Ascent, SM, CM], pressing space (the canonical manual-flip
// first step) must drop the LM as a SINGLE 2-stage [Descent, Ascent]
// craft and leave the [SM, CM] core firing the SPS — NOT peel the
// descent and ascent off one stage at a time (which would split the
// lander). The DecouplePlan's trailing "2" group is what keeps it whole.
func TestApolloStackManualFlipDropsLMAsOneCraft(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	// Drop the three Saturn stages → pre-transposition [Descent, Ascent, SM, CM].
	for i := 0; i < 3; i++ {
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("Saturn drop #%d: %v", i, err)
		}
	}
	pre := w.Crafts[0]
	if len(pre.Stages) != 4 || pre.Stages[0].Name != "Descent" {
		t.Fatalf("pre-transposition stack = %d stages, bottom %q; want [Descent,Ascent,SM,CM]",
			len(pre.Stages), pre.Stages[0].Name)
	}

	// One more press: the trailing "2" releases the LM as a 2-stage craft.
	_, lmIdx, err := w.StageActive(0)
	if err != nil {
		t.Fatalf("LM drop: %v", err)
	}
	lm := w.Crafts[lmIdx]
	if len(lm.Stages) != 2 || lm.Stages[0].Name != "Descent" || lm.Stages[1].Name != "Ascent" {
		t.Errorf("dropped LM = %d-stage %v, want 2-stage [Descent, Ascent] (lander must not split)",
			len(lm.Stages), stageNames(lm.Stages))
	}
	// The surviving core is [SM, CM] with the SPS firing.
	core := w.Crafts[0]
	if len(core.Stages) != 2 || core.Stages[0].Name != "SM" || core.Thrust != 91000 {
		t.Errorf("surviving core = %v Thrust=%.0f, want [SM, CM] firing SPS (91000)",
			stageNames(core.Stages), core.Thrust)
	}
}

// TestTransposeClearsDecouplePlan — after a one-shot transpose (D), the
// leftover loadout plan (the trailing LM "2" group, unconsumed because
// the player transposed instead of staging the LM off) must be cleared.
// Otherwise the next space press would pop the [SM, CM] core as a group
// instead of jettisoning the SM alone after TEI.
func TestTransposeClearsDecouplePlan(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	for i := 0; i < 3; i++ {
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("Saturn drop #%d: %v", i, err)
		}
	}
	// After 3 drops the unconsumed plan is the trailing [2].
	if got := w.Crafts[0].DecouplePlan; len(got) != 1 || got[0] != 2 {
		t.Fatalf("pre-transpose leftover plan = %v, want [2]", got)
	}
	if err := w.Transpose(0); err != nil {
		t.Fatalf("Transpose: %v", err)
	}
	if got := w.Crafts[0].DecouplePlan; got != nil {
		t.Errorf("post-transpose DecouplePlan = %v, want nil (cleared)", got)
	}
	// A space press now jettisons the SM alone (single-pop), not [SM, CM].
	_, _, err = w.StageActive(0)
	if err != nil {
		t.Fatalf("post-transpose stage: %v", err)
	}
	if dropped := w.Crafts[len(w.Crafts)-1]; len(dropped.Stages) != 1 || dropped.Stages[0].Name != "SM" {
		t.Errorf("post-transpose stage dropped %v, want single SM", stageNames(dropped.Stages))
	}
}

// TestTransposeRejectsWrongShape — Transpose only fires on the
// pre-transposition [Descent, Ascent, SM, CM] stack. Called on a fresh
// full Apollo Stack (the launch vehicle still attached) it must refuse
// with ErrTransposeNotReady rather than reorder a launch-config stack.
func TestTransposeRejectsWrongShape(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	if err := w.Transpose(0); !errors.Is(err, ErrTransposeNotReady) {
		t.Errorf("Transpose on full stack: err = %v, want ErrTransposeNotReady", err)
	}
	// Stack must be untouched (still 7 stages, S-IC at bottom).
	if len(w.Crafts[0].Stages) != 7 || w.Crafts[0].Stages[0].Name != "S-IC" {
		t.Errorf("rejected transpose mutated the stack: %d stages, bottom %q",
			len(w.Crafts[0].Stages), w.Crafts[0].Stages[0].Name)
	}
}

// TestApolloStackDecoupleChainThenTranspose — the v0.12 / ADR 0009
// Apollo-Stack is [S-IC, S-II, S-IVB, Descent, Ascent, SM, CM] with a
// DecouplePlan [1,1,1]. Three single decouples drop S-IC → S-II → S-IVB,
// leaving the pre-transposition stack [Descent, Ascent, SM, CM]. The
// transpose key (D) then reorders it to [SM, CM, Descent, Ascent] (SM =
// firing core) with the LM registered as a docked nose payload, which
// Undock releases as a 2-stage LM craft, leaving the SM/CM core.
func TestApolloStackDecoupleChainThenTranspose(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	if len(stack.Stages) != 7 {
		t.Fatalf("Apollo-Stack should start 7-stage, got %d", len(stack.Stages))
	}

	// First three presses drop single launch-vehicle stages.
	for i, name := range []string{"S-IC", "S-II", "S-IVB"} {
		_, jettIdx, err := w.StageActive(0)
		if err != nil {
			t.Fatalf("decouple %d (%s): %v", i, name, err)
		}
		jett := w.Crafts[jettIdx]
		if len(jett.Stages) != 1 || jett.Stages[0].Name != name {
			t.Errorf("decouple %d dropped %d-stage %q, want 1× %q",
				i, len(jett.Stages), jett.Stages[0].Name, name)
		}
	}

	// After the three Saturn drops the active craft is the
	// pre-transposition stack [Descent, Ascent, SM, CM].
	pre := w.Crafts[0]
	wantPre := []string{"Descent", "Ascent", "SM", "CM"}
	if len(pre.Stages) != len(wantPre) {
		t.Fatalf("pre-transposition stack: %d stages, want %d", len(pre.Stages), len(wantPre))
	}
	for i, n := range wantPre {
		if pre.Stages[i].Name != n {
			t.Errorf("pre-transposition stage %d = %q, want %q", i, pre.Stages[i].Name, n)
		}
	}

	// Transpose: reorder so the SM is the firing core, LM becomes a
	// docked nose payload.
	if err := w.Transpose(0); err != nil {
		t.Fatalf("Transpose: %v", err)
	}
	core := w.Crafts[0]
	wantPost := []string{"SM", "CM", "Descent", "Ascent"}
	for i, n := range wantPost {
		if core.Stages[i].Name != n {
			t.Errorf("post-transposition stage %d = %q, want %q", i, core.Stages[i].Name, n)
		}
	}
	if core.Stages[0].Name != "SM" || core.Thrust != 91000 {
		t.Errorf("firing engine after transpose: Stages[0]=%q Thrust=%.0f, want SM/91000",
			core.Stages[0].Name, core.Thrust)
	}
	if len(core.DockedComponents) != 2 {
		t.Fatalf("transposed composite: %d docked components, want 2 (core + LM)", len(core.DockedComponents))
	}

	// Undock releases the LM as a 2-stage [Descent, Ascent] craft; the
	// SM/CM core survives and still fires the SPS.
	if !w.Undock(0) {
		t.Fatal("Undock after transpose returned false")
	}
	var lm, csm *spacecraft.Spacecraft
	for _, c := range w.Crafts {
		switch {
		case len(c.Stages) == 2 && c.Stages[0].Name == "Descent":
			lm = c
		case len(c.Stages) == 2 && c.Stages[0].Name == "SM":
			csm = c
		}
	}
	if lm == nil {
		t.Fatal("no 2-stage [Descent, Ascent] LM craft after undock")
	}
	if lm.Stages[1].Name != "Ascent" || lm.Stages[0].Thrust <= 0 {
		t.Errorf("LM after undock: stages [%q,%q] Thrust=%.0f, want [Descent,Ascent] firing",
			lm.Stages[0].Name, lm.Stages[1].Name, lm.Stages[0].Thrust)
	}
	if lm.DecouplePlan != nil {
		t.Errorf("undocked LM DecouplePlan = %v, want nil (single-pop boundaries)", lm.DecouplePlan)
	}
	if csm == nil || csm.Stages[1].Name != "CM" || csm.Thrust != 91000 {
		t.Fatalf("surviving CSM core after undock not [SM,CM] firing: %+v", csm)
	}
}

// TestStageActivePreservesAttitudeOnDroppedStage — Slice v0.11.3:
// the jettisoned stage inherits the active craft's CurrentAttitudeDir
// at decouple time, so the dropped stage's launch-view sprite renders
// at the angle it shed at (no snap-to-vertical or zero-cmd jitter).
func TestStageActivePreservesAttitudeOnDroppedStage(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	saturn := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	saturn.Primary = w.Crafts[0].Primary
	saturn.State = w.Crafts[0].State
	// Set a non-trivial pitched attitude (gravity-turn moment): the
	// nose points 45° between +X and +Z. Magnitude doesn't have to be
	// unit for the inheritance check.
	saturn.CurrentAttitudeDir = orbital.Vec3{X: 0.5, Y: 0, Z: 0.5}
	w.Crafts[0] = saturn
	w.ActiveCraftIdx = 0

	parentCmd := saturn.CurrentAttitudeDir
	_, jettIdx, err := w.StageActive(0)
	if err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	jett := w.Crafts[jettIdx]
	if jett.CurrentAttitudeDir != parentCmd {
		t.Errorf("jettisoned CurrentAttitudeDir: got %+v, want %+v (parent's at decouple)",
			jett.CurrentAttitudeDir, parentCmd)
	}
}
