package sim

import (
	"testing"
)

// v0.9.0+ tests for the unified World.Target slot.

func TestTargetDefaultsToNone(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.Target.Kind != TargetNone {
		t.Errorf("default target kind: got %v, want TargetNone", w.Target.Kind)
	}
	if name := w.TargetName(); name != "" {
		t.Errorf("TargetName for None: got %q, want empty", name)
	}
}

func TestSetTargetBodyAndClear(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetBody(3) // some non-root body
	if w.Target.Kind != TargetBody || w.Target.BodyIdx != 3 {
		t.Errorf("SetTargetBody(3): got %+v", w.Target)
	}
	w.ClearTarget()
	if w.Target.Kind != TargetNone {
		t.Errorf("after ClearTarget: %+v, want TargetNone", w.Target)
	}
}

func TestSetTargetBodyRejectsRootAndOutOfRange(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetBody(0) // system primary
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetBody(0) should clear: got %+v", w.Target)
	}
	w.SetTargetBody(99999) // out of range
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetBody(99999) should clear: got %+v", w.Target)
	}
}

func TestSetTargetCraftRejectsActiveAndOutOfRange(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetCraft(w.ActiveCraftIdx) // self
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetCraft(active) should clear: got %+v", w.Target)
	}
	w.SetTargetCraft(99999)
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetCraft(99999) should clear: got %+v", w.Target)
	}
	w.SetTargetCraft(-1)
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetCraft(-1) should clear: got %+v", w.Target)
	}
}

// CycleTarget walks: None → (no sibling crafts in NewWorld's solo
// slate) → body 1 → … → body n-1 → None. Verify forward pass lands
// at every non-root body in order before wrapping.
func TestCycleTargetForwardWalksBodiesThenNone(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	nBodies := len(w.System().Bodies)
	if nBodies < 2 {
		t.Skip("system with too few bodies for a meaningful cycle")
	}
	// Start from None.
	w.ClearTarget()
	for i := 1; i < nBodies; i++ {
		w.CycleTarget(true)
		if w.Target.Kind != TargetBody || w.Target.BodyIdx != i {
			t.Errorf("step body %d: got %+v, want {TargetBody, %d}", i, w.Target, i)
		}
	}
	// One more cycle wraps back to None (NewWorld spawns a single
	// craft, so the slate has no sibling craft to insert before
	// wrapping).
	w.CycleTarget(true)
	if w.Target.Kind != TargetNone {
		t.Errorf("after wrap: got %+v, want TargetNone", w.Target)
	}
}

func TestCycleTargetBackwardFromNoneLandsOnLastEntry(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ClearTarget()
	w.CycleTarget(false)
	// Backward from None lands on the last entry of the cycle.
	// NewWorld has a single craft, so the last entry is the highest-
	// index body.
	nBodies := len(w.System().Bodies)
	if w.Target.Kind != TargetBody || w.Target.BodyIdx != nBodies-1 {
		t.Errorf("backward from None: got %+v, want {TargetBody, %d}", w.Target, nBodies-1)
	}
}

// CycleTarget should include every non-active craft in the slate
// AND visit them BEFORE any system body (the small list comes first
// so spawn-then-target lands in one keypress on Sol's 19-body
// catalog). Spawn an alongside sister craft and assert the very
// first cycle from None is a TargetCraft entry.
func TestCycleTargetIncludesSiblingCrafts(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	// SpawnCraft makes the new craft active — original at idx 0,
	// new at idx 1, ActiveCraftIdx==1. First forward cycle from None
	// must land on TargetCraft idx 0 (crafts before bodies).
	w.ClearTarget()
	w.CycleTarget(true)
	if w.Target.Kind != TargetCraft || w.Target.CraftIdx != 0 {
		t.Errorf("first CycleTarget after spawn: got %+v, want {TargetCraft, 0} (crafts must come before bodies)", w.Target)
	}
}

func TestTargetStateForBodyMatchesBodyPosition(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Pick the last body in the system — keeps the assertion robust
	// to body-order changes.
	idx := len(w.System().Bodies) - 1
	w.SetTargetBody(idx)
	st, ok := w.TargetState()
	if !ok {
		t.Fatal("TargetState for body: ok=false")
	}
	want := w.BodyPosition(w.System().Bodies[idx])
	if st.R.Sub(want).Norm() > 1e-6 {
		t.Errorf("TargetState.R: got %+v, want %+v", st.R, want)
	}
}

func TestTargetStateForNoneReturnsNotOk(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ClearTarget()
	if _, ok := w.TargetState(); ok {
		t.Error("TargetState for None: ok=true, want false")
	}
}

func TestTargetNameForBody(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	idx := len(w.System().Bodies) - 1
	w.SetTargetBody(idx)
	if got, want := w.TargetName(), w.System().Bodies[idx].EnglishName; got != want {
		t.Errorf("TargetName: got %q, want %q", got, want)
	}
}
