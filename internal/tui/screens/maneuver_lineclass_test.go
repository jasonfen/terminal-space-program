package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestManeuverShadowTrajectoryDrawsWithPlannedClass: the maneuver planner's
// post-burn preview (ADR 0041 §2 — a predicted trajectory, Planned class)
// used to plot every one of its 256 fixed samples via a raw untagged Plot
// call, one dot per sample, with no dash/gap pattern at all. This pins the
// call site itself: raising the planned Δv (which raises the shadow orbit,
// separating it from the current-orbit ellipse already on the canvas) must
// add new ink to the render — proving the Render method's shadow-
// trajectory loop's PlotPolylineClass(..., ClassPlanned) call actually
// fires rather than silently no-op'ing.
// The pixel-level dash-vs-solid distinction itself (contiguous run vs
// dash/gap cadence) is pinned directly against the primitive in
// internal/tui/widgets/lineclass_test.go, which is a cleaner place to
// assert exact pixel geometry than through this screen's ANSI-free
// headless render.
func TestManeuverShadowTrajectoryDrawsWithPlannedClass(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 400e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}

	m := NewManeuver(Theme{})
	m.dvInput.SetValue("0")
	before := countBraille(m.Render(w, 120, 40, 0))

	m.dvInput.SetValue("400") // a clear raise, separating the shadow from the current orbit
	after := countBraille(m.Render(w, 120, 40, 0))

	if after <= before {
		t.Errorf("raising the planned Δv added no shadow-trajectory ink (%d→%d) — Planned-class call site not firing", before, after)
	}
}

// TestManeuverTargetOrbitStaysGreenRealClass: the maneuver canvas's target-
// craft orbit is Real class (ADR 0041 §2) same as the active craft's — the
// TARGET treatment is a colour swap via DrawEllipseClass(..., ClassReal,
// ..., render.ColorTarget), not a different pattern. This is the maneuver-
// screen sibling of TestTargetedGhostOrbitPromoted (orbit_ghost_orbit_test.go).
func TestManeuverTargetOrbitStaysGreenRealClass(t *testing.T) {
	w := targetMarkerWorld(t)
	m := NewManeuver(Theme{})
	m.Render(w, 120, 40, 0)

	if n := m.canvas.CountColor(render.ColorTarget); n == 0 {
		t.Error("target craft's orbit inked no TARGET-colour cells on the maneuver canvas")
	}
}
