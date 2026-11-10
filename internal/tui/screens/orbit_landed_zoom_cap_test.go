package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLandedZoomCapStableAcrossTicks is the regression test for #376:
// a landed vessel's altitude is signed float noise around zero —
// integrateLanded rebuilds R = radius * unit(lat, lon) every tick, so
// |R| - radius is zero to float precision but flips sign tick to
// tick — and orbit.go:786 gated the surface zoom cap on
// `c.Altitude() <= 0`, a coin flip on that noise. At a zoom past the
// point where the cap actually binds, this made the canvas scale (and
// with it every drawn line) flip between two values roughly every
// other frame. The fix additionally gates on `c.Landed`, which is
// stable regardless of the sign noise.
//
// Zoomed to setUserZoom's max so the assertion binds — per the issue, at
// wide zoom scale stability holds even with the bug present, which would
// make this test pass vacuously. The v0.8.5 landed cap that #376 was
// about is gone (the ground now draws at true size past the textured
// disk), so the scale must also be the unclamped one: a cap back in
// place would show as scale != baseScale * userZoom.
func TestLandedZoomCapStableAcrossTicks(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(80, 24)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moonIdx := moonIdxOf(t, w)
	moonID := w.System().Bodies[moonIdx].ID

	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: moonID,
		Launchpad:    true,
		Latitude:     10,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}
	if w.Focus.Kind != sim.FocusCraft {
		t.Fatalf("setup: expected FocusCraft after a launchpad spawn, got %v", w.Focus.Kind)
	}

	// Fit once, then zoom in to the manual-zoom ceiling so the surface
	// cap (canvasReach / primary radius) is well and truly binding.
	v.Render(w, 0, 80, 24)
	for i := 0; i < 60; i++ {
		v.ZoomIn()
	}

	const frames = 12
	var scales [frames]float64
	for i := 0; i < frames; i++ {
		v.Render(w, 0, 80, 24)
		scales[i] = v.canvas.Scale()
		w.Tick()
	}

	if want := v.baseScale * v.userZoom; scales[0] != want {
		t.Fatalf("landed vessel's scale %.6e is not the unclamped %.6e: a surface zoom cap is back", scales[0], want)
	}
	for i := 1; i < frames; i++ {
		if scales[i] != scales[0] {
			t.Fatalf("canvas scale not stable across idle frames on a landed vessel: frame 0 scale=%.9e, frame %d scale=%.9e (zoom-cap flip from Altitude() sign noise — #376)",
				scales[0], i, scales[i])
		}
	}
}

// mapMetresPerCell renders the map and reads how many metres one cell spans.
func mapMetresPerCell(v *OrbitView, w *sim.World) float64 {
	v.Render(w, 0, DesignWidth, DesignHeight)
	return 2 / v.canvas.Scale()
}

// Max map zoom on a body reaches the LZ-circle scale (21.5 m a cell, ADR 0068
// decision 8) on every rocky body with a surface: at the old 1e4 cap Earth
// read 106 m a cell, Mars 56 and the Moon 29.
func TestMaxZoomReachesLZCircleScale(t *testing.T) {
	for _, name := range []string{"Earth", "Mars", "Moon"} {
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range w.System().Bodies {
			if b.EnglishName == name {
				w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: i}
			}
		}
		v := NewOrbitView(Theme{})
		v.Resize(DesignWidth, DesignHeight)
		v.Render(w, 0, DesignWidth, DesignHeight)
		v.setUserZoom(1e9)
		if m := mapMetresPerCell(v, w); m > 21.5 {
			t.Errorf("%s at max zoom is %.1f m a cell, want 21.5 or finer", name, m)
		}
	}
}

// A vessel landed on a shipped zone zooms to the circle scale, the circles
// draw about their LZs, and the ground fills the canvas under them (a full
// block in every edge cell). Old landed cap: LC-13 stopped at 30 km a cell.
func TestLandedVesselZoomsToLZCircles(t *testing.T) {
	for _, tc := range []struct{ zone, body string }{{"LC-13", "earth"}, {"Tranquility Base", "moon"}} {
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatal(err)
		}
		var zone sim.Site
		for _, z := range w.ShippedZones() {
			if z.Name == tc.zone {
				zone = z
			}
		}
		c, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: tc.body,
			Launchpad: true, Latitude: zone.LatDeg, LongitudeOffset: zone.LonDeg})
		if err != nil || !c.Landed {
			t.Fatalf("%s: spawn landed: %v landed=%v", tc.zone, err, c != nil && c.Landed)
		}
		w.ClearTarget()
		hidden := NewOrbitView(Theme{})
		st := hidden.settings
		st.SetChip(settings.ChipMarksFlags, false)
		hidden.SetSettings(st)
		_, base := lzMapCells(t, hidden, w, zone, 1e9)
		v := NewOrbitView(Theme{})
		_, ink := lzMapCells(t, v, w, zone, 1e9)
		if m := 2 / v.canvas.Scale(); m > 21.5 {
			t.Fatalf("%s: landed at max zoom is %.1f m a cell, want 21.5 or finer", tc.zone, m)
		}
		if a, b := newInk(ink[1], base[1]), newInk(ink[2], base[2]); a+b < 2 {
			t.Errorf("%s: landed at max zoom, circles inked %d + %d points of 16", tc.zone, a, b)
		}
		rows := strings.Split(ansi.Strip(v.canvas.String()), "\n")
		last := len(rows) - 1
		for _, p := range [][2]int{{0, 0}, {0, last / 2}, {len([]rune(rows[0])) - 1, 0}, {len([]rune(rows[last])) - 1, last}} {
			if got := []rune(rows[p[1]])[p[0]]; got != '\u28ff' {
				t.Errorf("%s: edge cell (%d,%d) is %q, want the ground's full block", tc.zone, p[0], p[1], got)
			}
		}
	}
}
