package screens

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// overBodyScene builds the grill's line-over-planet scenes: your vessel (and
// optionally a Target vessel) in a low equatorial orbit of parentID in system,
// the camera on that body in the Right view, where an equatorial orbit is a
// line straight across the planet's face, zoomed in until the disk is wide.
// It returns the view, the world, and the inscribed cell square of the disk.
func overBodyScene(t *testing.T, system, parentID string, withTarget bool) (v *OrbitView, w *sim.World, col0, row0, col1, row1 int) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(w.Systems) && w.Systems[w.SystemIdx].Name != system; i++ {
		w.CycleSystem()
	}
	if w.Systems[w.SystemIdx].Name != system {
		t.Fatalf("could not browse to %s", system)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: parentID, AltitudeM: 100e3}); err != nil {
		t.Fatal(err)
	}
	own := len(w.Crafts) - 1
	if withTarget {
		if _, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: parentID, AltitudeM: 200e3, Inclination: 5}); err != nil {
			t.Fatal(err)
		}
		other := len(w.Crafts) - 1
		w.ActiveCraftIdx = own
		w.SetTargetCraft(other)
	}
	w.ActiveCraftIdx = own
	idx, body := bodyByID(t, w, parentID)
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: idx}
	w.ViewMode = sim.ViewRight
	v = NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	v.declutter = true
	v.Render(w, 0, DesignWidth, DesignHeight)
	for i := 0; i < 7; i++ {
		v.ZoomIn()
	}
	v.Render(w, 0, DesignWidth, DesignHeight)
	px, py, on := v.canvas.Project(w.BodyPosition(body))
	if !on {
		t.Fatal("setup: the body is off the canvas")
	}
	r := BodyPixelRadius(body, false, v.canvas.Scale(), v.canvas.Cols()*2+v.canvas.Rows()*4)
	// The inscribed square of the disk: every cell in it is all disk.
	half := int(float64(r) * 0.7)
	return v, w, (px - half) / 2, (py - half) / 4, (px + half) / 2, (py + half) / 4
}

// A solid orbit has to stay a line when it crosses a planet whose colour
// matches it: yellow over Io, green over Kern (Jason 2026-10-10, #580). The
// line wins the cells it touches (the knockout) and the planet keeps the rest.
// Measured on these scenes: 40 and 40 line-coloured cells in the disk's
// inscribed square (one per column the line crosses); with the planet winning
// its cells again (the pre-v0.51.3 vote, knockout switched off) 0 and 0. The
// floor is 30, so a regression that hands the cells back goes red and ordinary
// drift does not (review #582 L1: a one-cell floor is not a guard).
func TestSolidOrbitVisibleAcrossMatchingColourBody(t *testing.T) {
	for _, tc := range []struct {
		name, system, body string
		target             bool
		colour             lipgloss.Color
		floor              int
	}{
		{"yellow over Io", "Sol", "io", false, render.ColorCurrentOrbit, 30},
		{"green over Kern", "Lumen", "kern", true, render.ColorTarget, 30},
	} {
		v, _, c0, r0, c1, r1 := overBodyScene(t, tc.system, tc.body, tc.target)
		got := v.canvas.CountColorIn(tc.colour, c0, r0, c1, r1)
		t.Logf("%s: %d orbit-coloured cells on the disk (inscribed square cols %d-%d rows %d-%d)", tc.name, got, c0, c1, r0, r1)
		if got < tc.floor {
			t.Errorf("%s: only %d orbit-coloured cells on the disk, want at least %d", tc.name, got, tc.floor)
		}
	}
}
