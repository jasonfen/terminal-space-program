package sim

import "testing"

// TestCycleViewModeWraps: starts at the zero-value (ViewTilted,
// v0.10.6+) and cycles forward through Tilted → Top → Right →
// Bottom → Left → OrbitFlat → Tilted in canonical order. The
// pre-v0.10.6 zero-value was ViewTop; the new prepend makes
// freshly-spawned worlds open on the perspective-tilt view.
func TestCycleViewModeWraps(t *testing.T) {
	w := mustWorld(t)
	if w.ViewMode != ViewTilted {
		t.Fatalf("default ViewMode = %v, want ViewTilted", w.ViewMode)
	}
	want := []ViewMode{ViewTop, ViewRight, ViewBottom, ViewLeft, ViewOrbitFlat, ViewTilted}
	for i, expect := range want {
		w.CycleViewMode()
		if w.ViewMode != expect {
			t.Errorf("after %d cycles: ViewMode = %v, want %v", i+1, w.ViewMode, expect)
		}
	}
}

// TestViewModeStringDistinct: each enumerated mode renders a distinct
// human label. Catches accidental case duplications when new modes
// land.
func TestViewModeStringDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range AllViewModes {
		s := m.String()
		if s == "" || s == "?" {
			t.Errorf("ViewMode(%d) returned empty / unknown string %q", m, s)
		}
		if seen[s] {
			t.Errorf("duplicate label %q", s)
		}
		seen[s] = true
	}
}

// TestDefaultViewTiltMatchesGrilledSpec (v0.10.6+): the freshly
// constructed World opens at θ=25°, φ=0 — the values the
// design pass and the grilled spec committed to. Bumping these
// defaults is a player-visible behaviour change and should require
// updating this test deliberately.
func TestDefaultViewTiltMatchesGrilledSpec(t *testing.T) {
	w := mustWorld(t)
	if w.ViewTilt.Theta != 25 {
		t.Errorf("Theta = %v, want 25", w.ViewTilt.Theta)
	}
	if w.ViewTilt.Phi != 0 {
		t.Errorf("Phi = %v, want 0", w.ViewTilt.Phi)
	}
	if w.ViewMode != ViewTilted {
		t.Errorf("ViewMode = %v, want ViewTilted (the new zero-value default)", w.ViewMode)
	}
}

// TestNudgeViewTiltThetaClamps (v0.10.6+): shift+↑/↓ keys come in
// as ±5° nudges through NudgeViewTiltTheta; the clamp must keep
// Theta inside [ViewTiltThetaMinDeg, ViewTiltThetaMaxDeg] so a
// stuck-key spam can't drive the basis into a degenerate state.
func TestNudgeViewTiltThetaClamps(t *testing.T) {
	w := mustWorld(t)
	for i := 0; i < 30; i++ {
		w.NudgeViewTiltTheta(ViewTiltThetaStep)
	}
	if w.ViewTilt.Theta != ViewTiltThetaMaxDeg {
		t.Errorf("after spam up: Theta = %v, want %v", w.ViewTilt.Theta, ViewTiltThetaMaxDeg)
	}
	for i := 0; i < 30; i++ {
		w.NudgeViewTiltTheta(-ViewTiltThetaStep)
	}
	if w.ViewTilt.Theta != ViewTiltThetaMinDeg {
		t.Errorf("after spam down: Theta = %v, want %v", w.ViewTilt.Theta, ViewTiltThetaMinDeg)
	}
	// Round-trip in the middle of the range — no clamp should fire.
	w.NudgeViewTiltTheta(ViewTiltThetaStep * 4) // 0 → 20
	if got := w.NudgeViewTiltTheta(ViewTiltThetaStep); got != 25 {
		t.Errorf("Theta = %v, want 25 (in-range nudge)", got)
	}
}
