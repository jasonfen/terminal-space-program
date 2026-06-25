package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestPropulsionSummaryScaleHint — ADR 0014 Slice D. propulsionSummary
// appends a spawn-form scale hint driven by the loadout's Scale() tag:
// real fleet reads ~9.4 km/s to orbit, the stripped-back fleet ~3.4 km/s.
// An unset ScaleClass normalizes to real (it must not read stripped-back).
// This is a display hint only — it never gates which craft can spawn.
func TestPropulsionSummaryScaleHint(t *testing.T) {
	real := spacecraft.Loadout{ScaleClass: bodies.ScaleReal}
	if got := propulsionSummary(real); !strings.Contains(got, "real scale") ||
		!strings.Contains(got, "9.4 km/s") {
		t.Errorf("real-scale hint missing: %q", got)
	}

	stripped := spacecraft.Loadout{ScaleClass: bodies.ScaleStrippedBack}
	if got := propulsionSummary(stripped); !strings.Contains(got, "stripped-back scale") ||
		!strings.Contains(got, "3.4 km/s") {
		t.Errorf("stripped-back hint missing: %q", got)
	}

	// Unset ScaleClass normalizes to real, not stripped-back.
	unset := spacecraft.Loadout{}
	if got := propulsionSummary(unset); !strings.Contains(got, "real scale") ||
		strings.Contains(got, "stripped-back") {
		t.Errorf("unset ScaleClass should read real-scale: %q", got)
	}
}

// selectCustom cycles the CRAFT TYPE field to the synthetic
// "Custom…" row (one past the last catalog loadout).
func selectCustom(s *SpawnCraft) {
	s.fieldIdx = 0
	for !s.IsCustomSelected() {
		s.HandleKey("right")
	}
}

// TestSpawnCustomEntryReachableAndEmpty — cycling past every catalog
// loadout lands on the Custom entry; before any part is added the
// stack is empty (the one confirm state the caller must reject) and
// SelectedLoadoutID is blank so the spawn path takes the custom
// branch.
func TestSpawnCustomEntryReachableAndEmpty(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")
	selectCustom(s)

	if !s.IsCustomSelected() {
		t.Fatal("could not reach the Custom entry by cycling CRAFT TYPE")
	}
	if s.SelectedLoadoutID() != "" {
		t.Errorf("Custom SelectedLoadoutID = %q, want empty", s.SelectedLoadoutID())
	}
	if !s.CustomStackEmpty() {
		t.Error("fresh Custom selection should report an empty stack")
	}
	if s.SelectedCustomStages() != nil {
		t.Error("empty custom stack should yield nil stages")
	}
	// Cycling forward once more wraps back to the first real loadout — which,
	// under ADR 0031 grouping, is the first row in grouped display order
	// (orderedLoadoutIDs[0]), not LoadoutOrder[0].
	s.HandleKey("right")
	if s.IsCustomSelected() {
		t.Error("Custom should wrap back to a real loadout")
	}
	if want := s.orderedLoadoutIDs()[0]; s.SelectedLoadoutID() != want {
		t.Errorf("after wrap, loadout = %q, want %q (grouped order)",
			s.SelectedLoadoutID(), want)
	}
}

// TestSpawnStackFieldReachableOnlyInCustom — the STACK field (Tab
// stop 5) must be unreachable for catalog loadouts and reachable
// only once Custom is selected.
func TestSpawnStackFieldReachableOnlyInCustom(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")

	// Non-custom: Tab through a full cycle never lands on stackFieldIdx.
	for i := 0; i < 12; i++ {
		s.HandleKey("tab")
		if s.fieldIdx == stackFieldIdx {
			t.Fatalf("STACK field reached on a non-custom loadout (fieldIdx=%d)", s.fieldIdx)
		}
	}

	selectCustom(s)
	seen := false
	for i := 0; i < 12; i++ {
		s.HandleKey("tab")
		if s.fieldIdx == stackFieldIdx {
			seen = true
			break
		}
	}
	if !seen {
		t.Error("STACK field never reached while Custom selected")
	}

	// Leaving Custom while parked on STACK must not strand the cursor.
	s.fieldIdx = stackFieldIdx
	s.loadoutIdx = 0 // off Custom
	s.HandleKey("tab")
	if s.fieldIdx == stackFieldIdx {
		t.Error("cursor stranded on STACK after leaving Custom")
	}
}

// TestSpawnTabFromCustomReachesStackFirst — the STACK editor renders
// directly below CRAFT TYPE, so Tab from the Custom craft-type must land
// on STACK next (not skip past it to POSITION). Shift+Tab back returns to
// CRAFT TYPE, and Tab onward from STACK continues to POSITION (field 1).
func TestSpawnTabFromCustomReachesStackFirst(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")
	selectCustom(s)

	if s.fieldIdx != 0 {
		t.Fatalf("after selecting Custom, focus should be on CRAFT TYPE (0), got %d", s.fieldIdx)
	}
	s.HandleKey("tab")
	if s.fieldIdx != stackFieldIdx {
		t.Errorf("Tab from Custom CRAFT TYPE = field %d, want STACK (%d)", s.fieldIdx, stackFieldIdx)
	}
	// Shift+Tab returns to CRAFT TYPE.
	s.HandleKey("shift+tab")
	if s.fieldIdx != 0 {
		t.Errorf("shift+Tab from STACK = field %d, want CRAFT TYPE (0)", s.fieldIdx)
	}
	// Tab past STACK continues to POSITION (field 1).
	s.HandleKey("tab") // → STACK
	s.HandleKey("tab") // → POSITION
	if s.fieldIdx != 1 {
		t.Errorf("Tab from STACK = field %d, want POSITION (1)", s.fieldIdx)
	}
}

// TestSpawnStackAddRemove — on the STACK field, ←/→ moves the part
// picker, [a] appends the picked part on top, [x] removes the top.
func TestSpawnStackAddRemove(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")
	selectCustom(s)
	s.fieldIdx = stackFieldIdx

	first := s.pickedPartID()
	s.HandleKey("a") // add first catalog part
	s.HandleKey("right")
	second := s.pickedPartID()
	if second == first {
		t.Fatal("part picker did not advance on right")
	}
	s.HandleKey("a") // add second part on top

	stack := s.SelectedCustomStages()
	if len(stack) != 2 {
		t.Fatalf("stack size = %d, want 2", len(stack))
	}
	wantBottom, _ := spacecraft.BuildStage(first)
	wantTop, _ := spacecraft.BuildStage(second)
	if stack[0].Name != wantBottom.Name {
		t.Errorf("bottom = %q, want %q (first added)", stack[0].Name, wantBottom.Name)
	}
	if stack[1].Name != wantTop.Name {
		t.Errorf("top = %q, want %q (added on top)", stack[1].Name, wantTop.Name)
	}

	s.HandleKey("x") // remove top
	if got := s.SelectedCustomStages(); len(got) != 1 || got[0].Name != wantBottom.Name {
		t.Errorf("after remove-top: %v, want single %q", got, wantBottom.Name)
	}
	// Add/remove are inert off the STACK field (no global collision).
	s.fieldIdx = 0
	s.HandleKey("x")
	if len(s.SelectedCustomStages()) != 1 {
		t.Error("[x] mutated the stack while off the STACK field")
	}
}

// TestSpawnListsSavedDesigns — v0.24 / ADR 0029: saved designs appear as
// CRAFT TYPE rows after "Custom…"; cycling onto one reports it via
// SelectedDesignID (and clears SelectedLoadoutID), and the row renders.
func TestSpawnListsSavedDesigns(t *testing.T) {
	designs := []spacecraft.Design{
		{Loadout: spacecraft.LoadoutDef{ID: "mun-hopper", Name: "Mun Hopper", Parts: []spacecraft.PartRef{{PartID: "x"}}}},
	}
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", designs, "")

	steps := 0
	for !s.IsDesignSelected() && steps < len(spacecraft.LoadoutOrder)+5 {
		s.HandleKey("right")
		steps++
	}
	if !s.IsDesignSelected() {
		t.Fatal("could not reach a saved-design row by cycling CRAFT TYPE")
	}
	if s.SelectedDesignID() != "mun-hopper" {
		t.Errorf("SelectedDesignID = %q, want mun-hopper", s.SelectedDesignID())
	}
	if s.SelectedLoadoutID() != "" {
		t.Errorf("a design row must blank SelectedLoadoutID, got %q", s.SelectedLoadoutID())
	}
	if !strings.Contains(s.Render(80), "Mun Hopper") {
		t.Error("rendered CRAFT TYPE list does not show the saved design")
	}
}

// pickPart cycles the part-picker to the given catalog id (STACK field
// must be focused). Bounded so a missing id can't loop forever.
func pickPart(s *SpawnCraft, id string) {
	for i := 0; i < len(spacecraft.StageCatalogOrder)+1; i++ {
		if s.pickedPartID() == id {
			return
		}
		s.HandleKey("right")
	}
}

// TestSpawnDockSeamFromCSMLMModule — v0.14 / ADR 0011. Adding the
// composite "CSM+LM" pick drops the four Apollo stages AND pre-sets the
// Dock Seam, so SelectedNosePayloadPlan reports the LM (top 2) as a nose
// payload without the player marking it by hand.
func TestSpawnDockSeamFromCSMLMModule(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")
	selectCustom(s)
	s.fieldIdx = stackFieldIdx

	pickPart(s, spacecraft.StageModuleApolloCSMLMID)
	s.HandleKey("a")

	if got := len(s.SelectedCustomStages()); got != 4 {
		t.Fatalf("CSM+LM pick added %d stages, want 4", got)
	}
	plan := s.SelectedNosePayloadPlan()
	if len(plan) != 1 || plan[0] != 2 {
		t.Errorf("SelectedNosePayloadPlan = %v, want [2] (the LM as nose payload)", plan)
	}
	if !strings.Contains(s.Render(80), "dock seam") {
		t.Error("rendered stack does not show the dock seam divider")
	}
}

// TestSpawnDockSeamCycleAndClamp — [d] cycles the Dock Seam over a
// general custom stack, and removing stages clamps it so the core keeps
// at least one stage. v0.14 / ADR 0011.
func TestSpawnDockSeamCycleAndClamp(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")
	selectCustom(s)
	s.fieldIdx = stackFieldIdx

	// Two single-stage parts → no seam yet.
	first := s.pickedPartID()
	s.HandleKey("a")
	s.HandleKey("right")
	if s.pickedPartID() == first {
		t.Fatal("part picker did not advance")
	}
	s.HandleKey("a")
	if s.SelectedNosePayloadPlan() != nil {
		t.Errorf("fresh 2-stack has a seam %v, want none", s.SelectedNosePayloadPlan())
	}

	// [d] marks the top stage as a nose payload; again wraps back to none.
	s.HandleKey("d")
	if plan := s.SelectedNosePayloadPlan(); len(plan) != 1 || plan[0] != 1 {
		t.Errorf("after one [d]: plan = %v, want [1]", plan)
	}
	s.HandleKey("d")
	if s.SelectedNosePayloadPlan() != nil {
		t.Errorf("after wrap [d]: plan = %v, want none", s.SelectedNosePayloadPlan())
	}

	// Set the seam to 1, then remove a stage so only 1 remains — the seam
	// must clamp to 0 (a 1-stage stack can't have a nose payload).
	s.HandleKey("d") // seam = 1 (of 2)
	s.HandleKey("x") // remove top → 1 stage left
	if s.SelectedNosePayloadPlan() != nil {
		t.Errorf("seam survived shrinking to a 1-stage stack: %v", s.SelectedNosePayloadPlan())
	}
}

// TestSpawnRenderShowsStackEditor — the STACK block appears only
// for Custom and reflects added parts.
func TestSpawnRenderShowsStackEditor(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "")

	if strings.Contains(s.Render(80), "STACK (bottom → top)") {
		t.Error("STACK editor rendered for a non-custom loadout")
	}
	selectCustom(s)
	out := s.Render(80)
	if !strings.Contains(out, "Custom…") {
		t.Error("Custom row missing from CRAFT TYPE list")
	}
	if !strings.Contains(out, "STACK (bottom → top)") {
		t.Error("STACK editor not rendered when Custom selected")
	}
	s.fieldIdx = stackFieldIdx
	s.HandleKey("a")
	picked := s.SelectedCustomStages()[0].Name
	if !strings.Contains(s.Render(80), picked) {
		t.Errorf("rendered stack does not list the added part %q", picked)
	}
}
