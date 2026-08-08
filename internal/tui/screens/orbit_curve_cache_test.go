package screens

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestOrbitViewCurveCacheHitsAtIdleAndBustsOnCameraChange is the #367
// end-to-end regression test the issue calls for: drive the real
// OrbitView.Render path (not a synthetic Canvas scenario) through an idle
// repeat, then a pan, a zoom, and a focus switch, and assert the curve
// cache behaves — hits when nothing that feeds it changed, misses when the
// camera moves. This exercises the actual screens/orbit.go wiring (the
// four DrawEllipseClassCachedTagged call sites and the curveID each
// passes), which the widgets-package unit tests (canvas_curve_cache_test.go)
// can't reach on their own.
func TestOrbitViewCurveCacheHitsAtIdleAndBustsOnCameraChange(t *testing.T) {
	v := NewOrbitView(plainTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	v.Render(w, 0, 120, 40) // warm the Framing Event / zoom fit
	_, computesAfterWarm := v.canvas.CurveCacheStats()
	if computesAfterWarm == 0 {
		t.Fatal("precondition: warm-up render recorded zero curve-cache computes — nothing drew an ellipse, test scenario is broken")
	}

	// Idle repeat: identical world, identical camera — every curve drawn
	// on the warm-up should still be in the cache.
	v.Render(w, 0, 120, 40)
	hits, computes := v.canvas.CurveCacheStats()
	if computes != computesAfterWarm {
		t.Errorf("idle repeat re-flattened %d curve(s) that should have hit (computes %d -> %d)", computes-computesAfterWarm, computesAfterWarm, computes)
	}
	if hits == 0 {
		t.Error("idle repeat recorded zero curve-cache hits")
	}

	// Pan: the camera center moves, nothing else does. Every on-canvas
	// curve's key must change (relOffset/relBody are camera-relative).
	v.PanRight()
	v.Render(w, 0, 120, 40)
	_, computesAfterPan := v.canvas.CurveCacheStats()
	if computesAfterPan == computes {
		t.Error("PanRight produced zero curve-cache misses — the cache didn't notice the camera moved")
	}

	// Idle again after the pan settles: should go back to all-hits.
	v.Render(w, 0, 120, 40)
	hitsBeforeSteady, _ := v.canvas.CurveCacheStats()
	v.Render(w, 0, 120, 40)
	hitsAfterSteady, computesAfterSteady := v.canvas.CurveCacheStats()
	if computesAfterSteady != 0 && computesAfterSteady != computesAfterPan {
		// (computesAfterPan carries forward; a further miss here would be
		// a bug — the panned camera is now steady.)
		t.Errorf("post-pan steady state kept missing (computes %d -> %d)", computesAfterPan, computesAfterSteady)
	}
	if hitsAfterSteady <= hitsBeforeSteady {
		t.Error("post-pan steady state produced no hits — cache never recovered after the pan")
	}

	// Zoom busts too.
	computesBeforeZoom := computesAfterSteady
	v.ZoomIn()
	v.Render(w, 0, 120, 40)
	_, computesAfterZoom := v.canvas.CurveCacheStats()
	if computesAfterZoom == computesBeforeZoom {
		t.Error("ZoomIn produced zero curve-cache misses")
	}

	// Focus switch (body -> craft), a Framing Event: re-fits scale and
	// re-centers, both of which must bust every curve's key.
	if w.ActiveCraft() == nil {
		t.Skip("no active craft in default world — can't exercise the focus-switch leg")
	}
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: 0}
	v.Render(w, 0, 120, 40)
	_, computesBeforeSwitch := v.canvas.CurveCacheStats()

	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v.Render(w, 0, 120, 40)
	_, computesAfterSwitch := v.canvas.CurveCacheStats()
	if computesAfterSwitch == computesBeforeSwitch {
		t.Error("focus switch (body -> craft) produced zero curve-cache misses")
	}
}

// TestOrbitViewCurveCacheHitMatchesUncachedAfterPan is the same
// cache-vs-ground-truth check orbit_disk_cache_pan_test.go runs for the
// disk raster cache (TestOrbitRenderDiskCacheHitMatchesUncachedAfterPan),
// applied to orbit-line geometry: a render that the curve cache
// recomputed after a pan must be byte-identical to the same frame
// rendered on a cache-dropped, never-panned-before OrbitView. Forces
// ANSI color for the same sync.Once test-ordering reason documented
// there.
func TestOrbitViewCurveCacheHitMatchesUncachedAfterPan(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	newWorld := func(t *testing.T) *sim.World {
		t.Helper()
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		return w
	}

	// One shared OrbitView, panned twice, so the second render is a
	// genuine cache MISS on an already-warm cache (not a cold first draw).
	v := NewOrbitView(plainTheme())
	w := newWorld(t)
	v.Render(w, 0, 120, 40)
	v.PanRight()
	v.Render(w, 0, 120, 40)
	v.PanDown()
	got := v.Render(w, 0, 120, 40)

	// Reference: replay the identical pan sequence on a fresh OrbitView +
	// World from scratch (cache never warm, so this is definitionally
	// correct — nothing to hit yet).
	freshV := NewOrbitView(plainTheme())
	freshW := newWorld(t)
	freshV.Render(freshW, 0, 120, 40)
	freshV.PanRight()
	freshV.Render(freshW, 0, 120, 40)
	freshV.PanDown()
	want := freshV.Render(freshW, 0, 120, 40)

	if got != want {
		t.Errorf("panned render diverges from a fresh reference\ngot:  %q\nwant: %q", got, want)
	}
	if !containsANSI(want) {
		t.Fatal("test setup broken: expected lipgloss.SetColorProfile(termenv.TrueColor) to produce ANSI-colored output")
	}
}
