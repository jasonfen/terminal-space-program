package sim

import (
	"testing"
)

func TestFocusDefaultsToSystem(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.Focus.Kind != FocusSystem {
		t.Errorf("default focus kind: got %v, want FocusSystem", w.Focus.Kind)
	}
	if name := w.FocusName(); name != "System-wide" {
		t.Errorf("default FocusName: got %q, want System-wide", name)
	}
}

func TestCycleFocusForwardWrapsThroughBodiesAndCraftAndBack(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Start at System. Cycle forward through every body, then Craft
	// (visible in Sol), then back to System.
	nBodies := len(w.System().Bodies)
	expected := []Focus{{Kind: FocusSystem}}
	for i := 0; i < nBodies; i++ {
		expected = append(expected, Focus{Kind: FocusBody, BodyIdx: i})
	}
	expected = append(expected, Focus{Kind: FocusCraft})
	expected = append(expected, Focus{Kind: FocusSystem})

	for i, want := range expected[1:] {
		w.CycleFocus(true)
		if w.Focus != want {
			t.Errorf("step %d: got %+v, want %+v", i, w.Focus, want)
		}
	}
}

func TestCycleFocusBackwardFromSystemLandsOnCraft(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.CycleFocus(false)
	if w.Focus.Kind != FocusCraft {
		t.Errorf("backward from system: got %+v, want FocusCraft", w.Focus)
	}
}

func TestCycleFocusSkipsCraftOutsideSol(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Advance to a non-Sol system. CraftVisibleHere() returns false there,
	// so the focus cycle must not include FocusCraft.
	w.CycleSystem()
	if w.CraftVisibleHere() {
		t.Fatal("precondition: craft should not be visible outside Sol")
	}
	w.ResetFocus()
	targets := w.focusTargets()
	for _, f := range targets {
		if f.Kind == FocusCraft {
			t.Errorf("focusTargets includes FocusCraft in non-Sol system: %+v", targets)
		}
	}
}

func TestResetFocusFromBodyReturnsToSystem(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Focus = Focus{Kind: FocusBody, BodyIdx: 3}
	w.ResetFocus()
	if w.Focus.Kind != FocusSystem {
		t.Errorf("after reset: got %+v, want FocusSystem", w.Focus)
	}
}

func TestCycleSystemResetsFocus(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Focus = Focus{Kind: FocusBody, BodyIdx: 5}
	w.CycleSystem()
	if w.Focus.Kind != FocusSystem {
		t.Errorf("after system switch: got %+v, want FocusSystem", w.Focus)
	}
}

func TestFocusPositionForSystemIsOrigin(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	p := w.FocusPosition()
	if p.Norm() != 0 {
		t.Errorf("FocusSystem position: got %+v, want origin", p)
	}
}

func TestFocusPositionForBodyMatchesBodyPosition(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	// Index 3 is typically Earth in Sol (Sun, Mercury, Venus, Earth, …).
	// We don't assume the order — just verify that FocusPosition tracks
	// BodyPosition for whichever body we select.
	idx := len(sys.Bodies) - 1
	w.Focus = Focus{Kind: FocusBody, BodyIdx: idx}
	got := w.FocusPosition()
	want := w.BodyPosition(sys.Bodies[idx])
	if got.Sub(want).Norm() > 1e-6 {
		t.Errorf("FocusPosition(%s): got %+v, want %+v",
			sys.Bodies[idx].EnglishName, got, want)
	}
}

func TestFocusPositionForCraftMatchesCraftInertial(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Focus = Focus{Kind: FocusCraft}
	got := w.FocusPosition()
	want := w.CraftInertial()
	if got.Sub(want).Norm() > 1e-6 {
		t.Errorf("FocusCraft position: got %+v, want %+v", got, want)
	}
}

func TestFocusZoomRadiusNonzeroForAllTargets(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for _, f := range w.focusTargets() {
		w.Focus = f
		if r := w.FocusZoomRadius(); r <= 0 {
			t.Errorf("FocusZoomRadius for %+v: got %g, want > 0", f, r)
		}
	}
}
