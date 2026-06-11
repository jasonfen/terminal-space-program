package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestCycleViewModeTargetGated: ViewTarget is reachable from the `v` cycle
// only when a body target is set — otherwise it has nothing to frame and is
// skipped, so a player not aiming at a body never lands on a dead view.
func TestCycleViewModeTargetGated(t *testing.T) {
	w := mustWorld(t)

	// No target: cycling from orbit-flat skips ViewTarget straight to launch.
	w.ViewMode = ViewOrbitFlat
	w.CycleViewMode()
	if w.ViewMode == ViewTarget {
		t.Fatalf("cycled into ViewTarget with no target set")
	}
	if w.ViewMode != ViewLaunch {
		t.Errorf("no-target cycle from orbit-flat = %v, want launch (target skipped)", w.ViewMode)
	}

	// With a body target: ViewTarget is reachable.
	moonIdx, _ := findMoon(t, w)
	w.SetTargetBody(moonIdx)
	w.ViewMode = ViewOrbitFlat
	w.CycleViewMode()
	if w.ViewMode != ViewTarget {
		t.Errorf("targeted cycle from orbit-flat = %v, want target", w.ViewMode)
	}
}

// TestTargetViewFraming: the ViewTarget camera centers on the target body
// and frames at least its SOI (so a close pass shows the perilune geometry),
// and reports ok=false with no body target.
func TestTargetViewFraming(t *testing.T) {
	w := mustWorld(t)
	if _, _, ok := w.TargetViewFraming(); ok {
		t.Fatal("framing returned ok=true with no target")
	}

	moonIdx, moon := findMoon(t, w)
	w.SetTargetBody(moonIdx)
	center, radius, ok := w.TargetViewFraming()
	if !ok {
		t.Fatal("framing returned ok=false with a body target")
	}
	if want := w.BodyPosition(moon); center.Sub(want).Norm() > 1 {
		t.Errorf("center = %v, want moon position %v", center, want)
	}
	if soi := physics.SOIRadius(moon, w.System().Bodies[0]); radius < soi {
		t.Errorf("radius %.0f < moon SOI %.0f — perilune geometry wouldn't be framed", radius, soi)
	}
}
