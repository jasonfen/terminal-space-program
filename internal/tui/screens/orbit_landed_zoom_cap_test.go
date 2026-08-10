package screens

import (
	"testing"

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
// Zoomed to setUserZoom's max (1e4×, well past the cap for any body
// in the catalog) so the assertion binds — per the issue, at wide
// zoom (before the cap engages) scale stability holds even with the
// bug present, which would make this test pass vacuously.
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

	for i := 1; i < frames; i++ {
		if scales[i] != scales[0] {
			t.Fatalf("canvas scale not stable across idle frames on a landed vessel: frame 0 scale=%.9e, frame %d scale=%.9e (zoom-cap flip from Altitude() sign noise — #376)",
				scales[0], i, scales[i])
		}
	}
}
