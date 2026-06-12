package sim

import "testing"

// TestCycleViewModeWraps: starts at the zero-value (ViewTilted,
// v0.10.6+) and cycles forward through Tilted → Top → Right →
// Bottom → Left → OrbitFlat → Launch → Tilted in canonical order.
// v0.11.0+ appends ViewLaunch to the cycle (NOT prepended) so
// ViewTilted stays the zero-value default for freshly spawned
// worlds.
func TestCycleViewModeWraps(t *testing.T) {
	w := mustWorld(t)
	if w.ViewMode != ViewTilted {
		t.Fatalf("default ViewMode = %v, want ViewTilted", w.ViewMode)
	}
	want := []ViewMode{ViewTop, ViewRight, ViewBottom, ViewLeft, ViewOrbitFlat, ViewLaunch, ViewTilted}
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

// TestNudgeViewTiltPhiWraps (ADR 0021 G): { / } keys come in as ±5°
// nudges through NudgeViewTiltPhi; unlike Theta's clamp, Phi must
// wrap at 360° so the player can spin the yaw a full turn in either
// direction without pinning at an edge.
func TestNudgeViewTiltPhiWraps(t *testing.T) {
	w := mustWorld(t)
	// Plain in-range nudge from the 0° default.
	if got := w.NudgeViewTiltPhi(ViewTiltPhiStep); got != 5 {
		t.Errorf("Phi = %v, want 5 (single nudge)", got)
	}
	// A full lap of +5° presses lands back where it started — no clamp.
	for i := 0; i < 72; i++ {
		w.NudgeViewTiltPhi(ViewTiltPhiStep)
	}
	if w.ViewTilt.Phi != 5 {
		t.Errorf("after a full +360° lap: Phi = %v, want 5 (wrap, not clamp)", w.ViewTilt.Phi)
	}
	// Crossing zero downward wraps to the top of the range.
	if got := w.NudgeViewTiltPhi(-ViewTiltPhiStep * 2); got != 355 {
		t.Errorf("Phi = %v, want 355 (5 - 10 wraps below zero)", got)
	}
	// And the result stays normalized to [0, 360) — never 360 itself.
	w.ViewTilt.Phi = 355
	if got := w.NudgeViewTiltPhi(ViewTiltPhiStep); got != 0 {
		t.Errorf("Phi = %v, want 0 (355 + 5 normalizes to 0, not 360)", got)
	}
}
