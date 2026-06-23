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
// the v0.12 / ADR 0009 Apollo mission staging sequence (drop S-IC,
// S-II, S-IVB, then transpose + undock the LM) and asserts:
//
//  1. While flying the composite, the active craft's CanSoftLand
//     reflects the *current bottom stage* — false through the
//     ascent / coast / TLI chain (none of those stages can land).
//  2. After transposition + undock, the released LM craft carries
//     CanSoftLand=true (its Descent bottom is soft-land-capable). The
//     surviving SM/CM core re-derives CanSoftLand=false via SyncFields
//     (the SM has no landing gear).
//
// This is the "playable as designed" loop ADR 0004 promised, now reached
// via the ADR 0009 transposition path rather than a bottom-up decouple.
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
	crewTendActive(w)

	// Stage 0 = S-IC bottom. Composite's CanSoftLand = false
	// (S-IC doesn't land).
	if apollo.CanSoftLand {
		t.Errorf("Apollo Stack at spawn: CanSoftLand=true, want false (S-IC is bottom)")
	}

	// Decouple S-IC, S-II, S-IVB in sequence (DecouplePlan [1,1,1] —
	// three single pops). The bottom becomes Descent after the third,
	// but Descent is the LM's soft-land-capable stage only while it is
	// Stages[0]; transposition then buries it behind the SM.
	for i, expectedStage := range []string{"S-II", "S-IVB", "Descent"} {
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("decouple #%d: %v", i, err)
		}
		active := w.Crafts[0]
		if active.Stages[0].Name != expectedStage {
			t.Fatalf("after decouple #%d: bottom stage = %q, want %q",
				i, active.Stages[0].Name, expectedStage)
		}
		wantSoftLand := expectedStage == "Descent"
		if active.CanSoftLand != wantSoftLand {
			t.Errorf("after decouple to %q: CanSoftLand=%v, want %v",
				expectedStage, active.CanSoftLand, wantSoftLand)
		}
	}

	// Transpose: the SM becomes the firing core (no landing gear), with
	// the LM as a docked nose payload — so the composite's CanSoftLand
	// drops back to false.
	if err := w.Transpose(0); err != nil {
		t.Fatalf("Transpose: %v", err)
	}
	if w.Crafts[0].CanSoftLand {
		t.Errorf("post-transposition composite (SM bottom): CanSoftLand=true, want false")
	}

	// Undock: the released LM craft carries CanSoftLand (Descent bottom);
	// the surviving SM/CM core does not.
	beforeSlateLen := len(w.Crafts)
	if !w.Undock(0) {
		t.Fatal("Undock after transpose returned false")
	}
	if len(w.Crafts) != beforeSlateLen+1 {
		t.Fatalf("slate length after undock: got %d, want %d",
			len(w.Crafts), beforeSlateLen+1)
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
	if !lm.CanSoftLand {
		t.Errorf("released LM craft: CanSoftLand=false, want true (Descent bottom carries the flag)")
	}
	if lm.Stages[1].Name != "Ascent" {
		t.Errorf("released LM stages = [%q, %q], want [Descent, Ascent]", lm.Stages[0].Name, lm.Stages[1].Name)
	}
	if csm == nil {
		t.Fatal("no surviving SM/CM core after undock")
	}
	if csm.CanSoftLand {
		t.Errorf("surviving SM/CM core: CanSoftLand=true, want false (no landing gear)")
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
	crewTendActive(w)

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
