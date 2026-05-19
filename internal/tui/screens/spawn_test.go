package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

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
	s.Reset(nil, "")
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
	// Cycling forward once more wraps back to the first real loadout.
	s.HandleKey("right")
	if s.IsCustomSelected() {
		t.Error("Custom should wrap back to a real loadout")
	}
	if s.SelectedLoadoutID() != spacecraft.LoadoutOrder[0] {
		t.Errorf("after wrap, loadout = %q, want %q",
			s.SelectedLoadoutID(), spacecraft.LoadoutOrder[0])
	}
}

// TestSpawnStackFieldReachableOnlyInCustom — the STACK field (Tab
// stop 5) must be unreachable for catalog loadouts and reachable
// only once Custom is selected.
func TestSpawnStackFieldReachableOnlyInCustom(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "")

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

// TestSpawnStackAddRemove — on the STACK field, ←/→ moves the part
// picker, [a] appends the picked part on top, [x] removes the top.
func TestSpawnStackAddRemove(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "")
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

// TestSpawnRenderShowsStackEditor — the STACK block appears only
// for Custom and reflects added parts.
func TestSpawnRenderShowsStackEditor(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "")

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
