// Package screens — v0.11.4+ sub-scope 5 / 6 tests for the
// LaunchView empty-state message and mini-navball placement.

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestLaunchViewRendersNavballBottomRight — the LAUNCH view
// composites the OrbitView's framed navball panel into the
// bottom-right corner of its canvas. v0.11.4+ sub-scope 6 —
// the launch chase-cam reads as nav-poor without it (pitch is
// visual via the sprite lean, but heading and roll aren't
// readable). Pin a stable marker from the navball panel (the
// `SAS` label that the buildNavballPanel chrome always emits).
func TestLaunchViewRendersNavballBottomRight(t *testing.T) {
	w, _ := spawnSaturnVOnPad(t)
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	out := v.Render(w, 160, 50)
	// `[MAN]` (manual SAS) + `RCS` chrome labels are emitted by
	// buildNavballPanel for any active vessel state; their joint
	// presence in the launch render proves the navball panel is
	// composited into the canvas. Pin both as a "navball is here"
	// fingerprint without depending on a specific axis-marker.
	if !strings.Contains(out, "[MAN]") || !strings.Contains(out, "RCS") {
		t.Errorf("expected navball chrome ('[MAN]' and 'RCS') in LAUNCH render; got:\n%s", out)
	}
}

// TestLaunchViewShowsNoActiveVesselMessage — when ActiveCraft is
// nil (the slate is empty, reachable today via end-flight removing
// the only vessel), the LaunchView renders a centered dim "no
// active vessel" message inside the canvas instead of a blank
// scene + a `LAUNCH — ` title. v0.11.4+ sub-scope 5.
func TestLaunchViewShowsNoActiveVesselMessage(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Empty the slate so ActiveCraft() returns nil. The route to
	// this in-product is end-flight; here we mutate directly to
	// isolate the render-path behaviour.
	w.Crafts = nil
	w.ActiveCraftIdx = -1
	out := v.Render(w, 120, 40)
	if !strings.Contains(out, "no active vessel") {
		t.Errorf("expected 'no active vessel' in render with nil active; got:\n%s", out)
	}
}
