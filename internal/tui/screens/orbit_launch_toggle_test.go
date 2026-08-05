package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestLaunchRoundTripRestoresMap is the `V` toggle's promise: press to
// enter the launch/surface view, press to leave, and the map is exactly
// as it was — same view mode, same manual zoom, same pan. Mirrors
// TestProximityRoundTripRestoresMap, but the mechanism differs: ViewLaunch
// is a whole separate screen (LaunchView, not OrbitView) so app.go never
// calls OrbitView.Render at all while ViewMode == ViewLaunch — this test
// reproduces that by simply not calling v.Render in between, the same way
// the real dispatch in app.go never does.
func TestLaunchRoundTripRestoresMap(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	// Give the craft a real orbit basis so ViewOrbitFlat's Framing Event
	// has something non-degenerate to fit; also lets FocusZoomRadius avoid
	// the pad's special-cased near-zero fit.
	c.Landed = false
	c.OnPad = false

	v := newProximityTestView(t, 80, 24)
	w.ViewMode = sim.ViewOrbitFlat
	v.Render(w, 0, 80, 24)
	v.ZoomIn()
	v.ZoomIn()
	v.PanRight()
	v.PanUp()
	v.Render(w, 0, 80, 24)
	mapZoom := v.userZoom
	mapPan := v.panOffset
	mapScale := v.canvas.Scale()
	if mapZoom == 1 {
		t.Fatal("setup: zoom stayed at 1, the test can't tell restore from reset")
	}
	if mapPan.Norm() == 0 {
		t.Fatal("setup: pan stayed at zero, the test can't tell restore from clear")
	}

	// Enter the launch/surface view. app.go never calls OrbitView.Render
	// while ViewMode == ViewLaunch (it dispatches to LaunchView instead),
	// so — matching that — v.Render is deliberately NOT called here.
	if entered, refusal := w.ToggleLaunchView(); !entered || refusal != "" {
		t.Fatalf("enter: entered=%v refusal=%q", entered, refusal)
	}

	// Leave, and render the map again.
	if entered, refusal := w.ToggleLaunchView(); entered || refusal != "" {
		t.Fatalf("leave: entered=%v refusal=%q", entered, refusal)
	}
	v.Render(w, 0, 80, 24)

	if w.ViewMode != sim.ViewOrbitFlat {
		t.Errorf("ViewMode = %s, want orbit-flat", w.ViewMode)
	}
	if v.userZoom != mapZoom {
		t.Errorf("map zoom = %v, want %v (round trip must not disturb it)", v.userZoom, mapZoom)
	}
	if d := v.panOffset.Sub(mapPan).Norm(); d > 1 {
		t.Errorf("map pan moved by %.1f m across the round trip", d)
	}
	if got := v.canvas.Scale(); math.Abs(got-mapScale)/mapScale > 1e-9 {
		t.Errorf("map scale = %.6e, want %.6e", got, mapScale)
	}
}
