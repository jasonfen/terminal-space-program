package sim

import (
	"strings"
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

// TestSpawnCustomLanderSplitStages — v0.13: the configurator's single
// "lander" pick (BuildModule) builds a [Descent, Ascent] lander as one
// vessel; staging then single-pops the descent (leaving the ascent as the
// surviving core) exactly like the standalone Lander loadout, so a custom
// Apollo-LM separates the way the player expects. Regression for the
// playtest thread: the spawner lander now has the descent/ascent split and
// is added as one vessel.
func TestSpawnCustomLanderSplitStages(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// One "lander" pick expands to the 2-stage LM (bottom → top).
	landerStages, ok := spacecraft.BuildModule(spacecraft.StageModuleLanderID)
	if !ok || len(landerStages) != 2 {
		t.Fatalf("BuildModule(lander) = %d stages (ok=%v), want 2", len(landerStages), ok)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		CustomStages: landerStages,
		ParentBodyID: "earth",
		AltitudeM:    400e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(custom lander): %v", err)
	}
	if c.Stages[0].Name != "Descent" || c.Stages[1].Name != "Ascent" {
		t.Fatalf("stack order wrong: bottom=%q top=%q", c.Stages[0].Name, c.Stages[1].Name)
	}
	crewTend(c) // testing the staging split, not comms

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

// TestSpawnNosePayloadCompositeAssembles — v0.14 / ADR 0011. A custom
// stack carrying a NosePayloadPlan spawns as an assembled docked
// composite, not a linear chain: the core fires at Stages[0], the nose
// payload is recorded as a DockedComponent, and Undock releases it as a
// coherent multi-stage craft. This is the fix for the playtest report —
// the CSM+LM spawns in the post-transposition shape so the LM leaves via
// Undock (and only splits Descent/Ascent on the surface), instead of the
// linear single-pop that stranded the Descent in orbit.
func TestSpawnNosePayloadCompositeAssembles(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stages, ok := spacecraft.BuildModule(spacecraft.StageModuleApolloCSMLMID)
	if !ok || len(stages) != 4 {
		t.Fatalf("BuildModule(csm-lm) = %d stages (ok=%v), want 4", len(stages), ok)
	}

	c, err := w.SpawnCraft(SpawnSpec{
		CustomStages:    stages,
		NosePayloadPlan: []int{2}, // top 2 (the LM) ride as a docked nose payload
		ParentBodyID:    "moon",
		AltitudeM:       100e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(composite): %v", err)
	}

	// Full stack, core at the bottom and firing.
	wantNames := []string{"SM", "CM", "Descent", "Ascent"}
	for i, want := range wantNames {
		if c.Stages[i].Name != want {
			t.Errorf("Stages[%d] = %q, want %q", i, c.Stages[i].Name, want)
		}
	}
	if c.Thrust <= 0 || c.Stages[0].Name != "SM" {
		t.Errorf("composite firing core = %q @ %.0fN, want SM with thrust", c.Stages[0].Name, c.Thrust)
	}
	// Flies as the core, not the top payload stage. nextCraftName adds the
	// usual "-N" slate suffix, so check the prefix.
	if !strings.HasPrefix(c.Name, "CSM") {
		t.Errorf("composite name = %q, want a CSM* name (flies as the core, not the top payload stage)", c.Name)
	}

	// Two docked components: the [SM, CM] core and the [Descent, Ascent] LM.
	if len(c.DockedComponents) != 2 {
		t.Fatalf("DockedComponents = %d, want 2 (core + LM)", len(c.DockedComponents))
	}
	core, lm := c.DockedComponents[0], c.DockedComponents[1]
	if len(core.Stages) != 2 || core.Stages[0].Name != "SM" {
		t.Errorf("core component = %d stages (bottom %q), want [SM, CM]", len(core.Stages), core.Stages[0].Name)
	}
	if len(lm.Stages) != 2 || lm.Stages[0].Name != "Descent" {
		t.Errorf("LM component = %d stages (bottom %q), want [Descent, Ascent]", len(lm.Stages), lm.Stages[0].Name)
	}

	// Undock releases the LM as a coherent 2-stage craft (NOT a single
	// Descent stranded in orbit) and leaves the SM/CM core flying.
	if !w.Undock(w.ActiveCraftIdx) {
		t.Fatal("Undock of the spawned composite returned false")
	}
	var freedLM, freedCore *spacecraft.Spacecraft
	for _, cc := range w.Crafts {
		switch {
		case len(cc.Stages) == 2 && cc.Stages[0].Name == "Descent":
			freedLM = cc
		case len(cc.Stages) == 2 && cc.Stages[0].Name == "SM":
			freedCore = cc
		}
	}
	if freedLM == nil {
		t.Error("Undock did not release a 2-stage [Descent, Ascent] LM")
	}
	if freedCore == nil {
		t.Error("Undock did not leave a 2-stage [SM, CM] core")
	}
}

// TestSpawnNosePayloadAbsentIsLinear — control for ADR 0011: the SAME
// stack with no NosePayloadPlan spawns as a plain linear craft (no
// DockedComponents), staging single-pops, and a malformed seam degrades
// to linear rather than erroring.
func TestSpawnNosePayloadAbsentIsLinear(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stages, _ := spacecraft.BuildModule(spacecraft.StageModuleApolloCSMLMID)

	linear, err := w.SpawnCraft(SpawnSpec{
		CustomStages: stages,
		ParentBodyID: "moon",
		AltitudeM:    100e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(linear): %v", err)
	}
	if len(linear.DockedComponents) != 0 {
		t.Errorf("no-plan custom craft has %d DockedComponents, want 0 (linear)", len(linear.DockedComponents))
	}

	// A malformed seam (≥ stack length) must fall back to linear, not panic
	// or strand the spawn.
	bad, err := w.SpawnCraft(SpawnSpec{
		CustomStages:    stages,
		NosePayloadPlan: []int{len(stages)}, // whole stack = payload → no core
		ParentBodyID:    "moon",
		AltitudeM:       100e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(malformed seam): %v", err)
	}
	if len(bad.DockedComponents) != 0 {
		t.Errorf("malformed seam produced %d DockedComponents, want 0 (linear fallback)", len(bad.DockedComponents))
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
