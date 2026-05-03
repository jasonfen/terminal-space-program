package sim

import (
	"testing"
)

// TestFocusDefaultsToCraft: v0.6.1 spawns the camera focused on the
// craft so the live orbit + maneuver previews are immediately in
// frame. Pre-v0.6.1 the default was FocusSystem, which dropped the
// player into a heliocentric view where the craft's LEO collapsed
// to a sub-pixel.
func TestFocusDefaultsToCraft(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.Focus.Kind != FocusCraft {
		t.Errorf("default focus kind: got %v, want FocusCraft", w.Focus.Kind)
	}
}

func TestCycleFocusForwardWrapsThroughBodiesAndCraftAndBack(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Reset to FocusSystem before exercising the cycle so this test
	// stays focused on cycling behavior rather than NewWorld's
	// default-focus choice (covered by TestFocusDefaultsToCraft).
	w.ResetFocus()
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
	w.ResetFocus() // start from FocusSystem regardless of NewWorld default.
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
	w.ResetFocus() // override v0.6.1's FocusCraft default for this test.
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

// TestFocusZoomRadiusTerminalMoonTightZoom: v0.8.5.7+ — terminal
// bodies (no children) get a tight 8×-radius zoom so the surface
// texture is clearly visible when focused. Bodies with children
// (Earth, Mars, Jupiter, Saturn) keep the SOI-radius view so their
// moons stay in frame.
func TestFocusZoomRadiusTerminalMoonTightZoom(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	var earthIdx, lunaIdx int = -1, -1
	for i, b := range sys.Bodies {
		switch b.ID {
		case "earth":
			earthIdx = i
		case "moon":
			lunaIdx = i
		}
	}
	if earthIdx == -1 || lunaIdx == -1 {
		t.Fatal("Sol system missing earth and/or moon")
	}
	// Luna (terminal): expect ~8× radius zoom.
	w.Focus = Focus{Kind: FocusBody, BodyIdx: lunaIdx}
	luna := sys.Bodies[lunaIdx]
	want := luna.RadiusMeters() * 8
	got := w.FocusZoomRadius()
	if got != want {
		t.Errorf("Luna focus zoom: got %g, want %g (8× body radius)", got, want)
	}
	// Earth (has Luna as child): zoom should be SOI, much larger
	// than 8× radius.
	w.Focus = Focus{Kind: FocusBody, BodyIdx: earthIdx}
	earth := sys.Bodies[earthIdx]
	got = w.FocusZoomRadius()
	if got <= earth.RadiusMeters()*8 {
		t.Errorf("Earth focus zoom = %g, expected SOI-class (much > 8× radius)", got)
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
