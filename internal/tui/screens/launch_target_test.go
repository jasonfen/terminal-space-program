package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The launch view shows your Target like the map does (Jason's playtest
// 2026-10-07: "when I target a vessel, its orbit should also render and the
// color of its glyph should change to something more visible. currently I
// see it as plain old white", in the launch view): the targeted vessel's
// glyph and its orbit draw in TARGET green; other vessels keep their look.

func TestLaunchViewDrawsTargetOrbitInTargetGreen(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraftIdx
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 900e3}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	w.ActiveCraftIdx = active
	w.SetTargetCraft(len(w.Crafts) - 1)
	v := NewLaunchView(launchThemeForTest(), NewOrbitView(launchThemeForTest()))
	v.Render(w, DesignWidth, DesignHeight)
	if n := v.canvas.CountColor(render.ColorTarget); n < 20 {
		t.Errorf("target orbit: %d cells in TARGET green, want an orbit line (>= 20)", n)
	}

	// Untargeted, the same vessel's orbit is not drawn in TARGET green.
	w.Target = sim.Target{}
	v.Render(w, DesignWidth, DesignHeight)
	if n := v.canvas.CountColor(render.ColorTarget); n != 0 {
		t.Errorf("no target: %d cells in TARGET green, want 0", n)
	}
}

func TestLaunchViewDrawsTargetGlyphInTargetGreen(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraftIdx
	if _, err := w.SpawnCraft(sim.SpawnSpec{Alongside: true}); err != nil {
		t.Fatalf("spawn alongside: %v", err)
	}
	w.ActiveCraftIdx = active
	w.SetTargetCraft(len(w.Crafts) - 1)
	v := NewLaunchView(launchThemeForTest(), NewOrbitView(launchThemeForTest()))
	v.Render(w, DesignWidth, DesignHeight)
	if n := v.canvas.CountOverlayColor(render.ColorTarget); n < 1 {
		t.Errorf("target vessel glyph not drawn in TARGET green (overlay cells in green: %d)", n)
	}
}
