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
