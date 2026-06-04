package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSpawnCustomStagesBuildsFromStack — v0.10.1 stack-configurator
// path: a SpawnSpec carrying CustomStages builds the craft via
// NewFromStages (ignoring LoadoutID), places it in orbit like any
// other spawn, and adds it as the active slate craft.
func TestSpawnCustomStagesBuildsFromStack(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sic, _ := spacecraft.BuildStage(spacecraft.StageModuleSICID)
	sivb, _ := spacecraft.BuildStage(spacecraft.StageModuleSIVBID)
	csm, _ := spacecraft.BuildStage(spacecraft.StageModuleCSMID)

	before := len(w.Crafts)
	c, err := w.SpawnCraft(SpawnSpec{
		// LoadoutID set but must be ignored when CustomStages present.
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		CustomStages: []spacecraft.Stage{sic, sivb, csm},
		ParentBodyID: "earth",
		AltitudeM:    400e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(custom): %v", err)
	}
	if len(w.Crafts) != before+1 || w.ActiveCraft() != c {
		t.Fatalf("custom craft not added as active (slate %d→%d)", before, len(w.Crafts))
	}
	if len(c.Stages) != 3 {
		t.Errorf("custom craft stage count = %d, want 3", len(c.Stages))
	}
	if c.LoadoutID != "" {
		t.Errorf("custom craft LoadoutID = %q, want empty (Saturn-V must be ignored)", c.LoadoutID)
	}
	if c.Stages[0].Name != "S-IC" || c.Stages[2].Name != "CSM" {
		t.Errorf("stack order wrong: bottom=%q top=%q", c.Stages[0].Name, c.Stages[2].Name)
	}
	if c.Primary.ID != "earth" {
		t.Errorf("primary = %q, want earth", c.Primary.ID)
	}
	// The custom stack must still decouple through the v0.9.1 chain.
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Errorf("custom craft StageActive: %v", err)
	}
	if len(w.ActiveCraft().Stages) != 2 {
		t.Errorf("after one decouple: %d stages, want 2", len(w.ActiveCraft().Stages))
	}
}

// TestSpawnCustomLanderSplitStages — v0.13: a configurator-built
// [Descent, Ascent] lander single-pops the descent (leaving the ascent as
// the surviving core) exactly like the standalone Lander loadout, so a
// custom Apollo-LM separates the way the player expects. Regression for
// the playtest report "the lander in the vessel spawner does not have a
// descent or ascent stage like the Apollo stack does."
func TestSpawnCustomLanderSplitStages(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	descent, ok := spacecraft.BuildStage(spacecraft.StageModuleLanderDescentID)
	if !ok {
		t.Fatal("BuildStage(lander-descent) failed")
	}
	ascent, ok := spacecraft.BuildStage(spacecraft.StageModuleLanderAscentID)
	if !ok {
		t.Fatal("BuildStage(lander-ascent) failed")
	}
	c, err := w.SpawnCraft(SpawnSpec{
		CustomStages: []spacecraft.Stage{descent, ascent}, // bottom → top
		ParentBodyID: "earth",
		AltitudeM:    400e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(custom lander): %v", err)
	}
	if c.Stages[0].Name != "Descent" || c.Stages[1].Name != "Ascent" {
		t.Fatalf("stack order wrong: bottom=%q top=%q", c.Stages[0].Name, c.Stages[1].Name)
	}

	// Stage once: drops the Descent as its own craft, leaving the Ascent.
	_, jidx, err := w.StageActive(w.ActiveCraftIdx)
	if err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if dropped := w.Crafts[jidx].Stages; len(dropped) != 1 || dropped[0].Name != "Descent" {
		t.Errorf("dropped craft = %d stages (top %q), want [Descent]", len(dropped), dropped[len(dropped)-1].Name)
	}
	if rem := w.ActiveCraft().Stages; len(rem) != 1 || rem[0].Name != "Ascent" {
		t.Errorf("remaining active = %d stages (bottom %q), want [Ascent]", len(rem), rem[0].Name)
	}
}

// TestSpawnEmptyCustomStagesFallsThroughToLoadout — defence in
// depth: an empty CustomStages slice must NOT be treated as a
// custom craft (the form layer rejects empty stacks before here,
// but SpawnCraft must not panic / mis-build if one slips through).
func TestSpawnEmptyCustomStagesFallsThroughToLoadout(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutICPSID,
		CustomStages: nil,
		ParentBodyID: "earth",
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if c.LoadoutID != spacecraft.LoadoutICPSID {
		t.Errorf("empty CustomStages should fall through to LoadoutID, got %q", c.LoadoutID)
	}
}
