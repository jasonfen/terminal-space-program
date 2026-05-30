// Package sim — v0.11.4-followup tests for per-Stage CanSoftLand
// propagation across the StageActive decouple path. The Apollo
// Stack mission flow is the headline regression: the Lander stage
// in the middle of the chain must produce a soft-land-capable
// jettisoned craft when the player decouples it after TLI.

package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestApolloStackLanderDecoupleCarriesCanSoftLand — walks through
// the Apollo mission staging sequence (drop S-IC, S-II, S-IVB,
// then Lander) and asserts:
//
//  1. While flying the composite, the active craft's CanSoftLand
//     reflects the *current bottom stage* — false through the
//     ascent / coast / TLI chain (none of those stages can land).
//  2. After the Lander stage decouples, the new jettisoned craft
//     in the slate carries CanSoftLand=true. The surviving active
//     (CSM alone) re-derives CanSoftLand=false via SyncFields.
//
// This is the "playable as designed" loop ADR 0004 promised — and
// the v0.11.4-followup that made it actually work end-to-end.
func TestApolloStackLanderDecoupleCarriesCanSoftLand(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	apollo := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	apollo.Primary = w.Crafts[0].Primary
	apollo.State = w.Crafts[0].State
	w.Crafts[0] = apollo
	w.ActiveCraftIdx = 0

	// Stage 0 = S-IC bottom. Composite's CanSoftLand = false
	// (S-IC doesn't land).
	if apollo.CanSoftLand {
		t.Errorf("Apollo Stack at spawn: CanSoftLand=true, want false (S-IC is bottom)")
	}

	// Decouple S-IC, S-II, S-IVB in sequence (DecouplePlan [1,1,1,2] —
	// three single pops) — each should leave the active craft with
	// CanSoftLand=false until the bottom becomes Descent (the LM's
	// soft-land-capable bottom stage).
	for i, expectedStage := range []string{"S-II", "S-IVB", "Descent"} {
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("decouple #%d: %v", i, err)
		}
		active := w.Crafts[0]
		if active.Stages[0].Name != expectedStage {
			t.Fatalf("after decouple #%d: bottom stage = %q, want %q",
				i, active.Stages[0].Name, expectedStage)
		}
		// Through S-II + S-IVB the bottom isn't the LM yet →
		// CanSoftLand=false. At Descent (the third iteration), bottom
		// IS the soft-land-capable descent stage → CanSoftLand=true.
		wantSoftLand := expectedStage == "Descent"
		if active.CanSoftLand != wantSoftLand {
			t.Errorf("after decouple to %q: CanSoftLand=%v, want %v",
				expectedStage, active.CanSoftLand, wantSoftLand)
		}
	}

	// Fourth press: the plan's trailing 2 releases the LM (Descent +
	// Ascent) as one 2-stage craft, leaving the CSM core. The
	// jettisoned LM's bottom (Descent) carries CanSoftLand; the
	// surviving CSM does not.
	beforeSlateLen := len(w.Crafts)
	_, jettIdx, err := w.StageActive(0)
	if err != nil {
		t.Fatalf("decouple LM: %v", err)
	}
	if len(w.Crafts) != beforeSlateLen+1 {
		t.Fatalf("slate length after LM decouple: got %d, want %d",
			len(w.Crafts), beforeSlateLen+1)
	}
	jett := w.Crafts[jettIdx]
	if !jett.CanSoftLand {
		t.Errorf("jettisoned LM craft: CanSoftLand=false, want true (Descent bottom carries the flag)")
	}
	if len(jett.Stages) != 2 || jett.Stages[0].Name != "Descent" || jett.Stages[1].Name != "Ascent" {
		t.Errorf("jettisoned LM stages = %d-stage, want [Descent, Ascent]", len(jett.Stages))
	}
	active := w.Crafts[0]
	if active.Stages[0].Name != "CSM" {
		t.Errorf("surviving active after LM decouple: bottom stage = %q, want %q",
			active.Stages[0].Name, "CSM")
	}
	if active.CanSoftLand {
		t.Errorf("surviving CSM: CanSoftLand=true, want false (no landing gear)")
	}
}

// TestFalcon9FirstStageDecoupleCarriesCanSoftLand — pin the
// recovery-flow regression: F9-S1 (bottom) carries
// CanSoftLand=true; F9-S2 (top) doesn't. After decouple, the
// jettisoned F9-S1 craft retains its soft-land qualification while
// the surviving F9-S2 active loses it.
func TestFalcon9FirstStageDecoupleCarriesCanSoftLand(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	f9 := spacecraft.NewFromLoadout(spacecraft.LoadoutFalcon9ID)
	f9.Primary = w.Crafts[0].Primary
	f9.State = w.Crafts[0].State
	w.Crafts[0] = f9
	w.ActiveCraftIdx = 0

	if !f9.CanSoftLand {
		t.Errorf("F9 at spawn: CanSoftLand=false, want true (F9-S1 bottom carries flag)")
	}

	_, jettIdx, err := w.StageActive(0)
	if err != nil {
		t.Fatalf("decouple F9-S1: %v", err)
	}
	jett := w.Crafts[jettIdx]
	if !jett.CanSoftLand {
		t.Errorf("jettisoned F9-S1 craft: CanSoftLand=false, want true")
	}
	active := w.Crafts[0]
	if active.CanSoftLand {
		t.Errorf("surviving F9-S2: CanSoftLand=true, want false (second stage isn't a lander)")
	}
}
