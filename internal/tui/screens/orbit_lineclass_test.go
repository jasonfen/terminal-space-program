package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestOwnOrbitAndNodeLegDrawDistinctClassesInSameFrame is the call-site half
// of ADR 0041 §2's line-style vocabulary audit: the active vessel's live
// orbit (Real class, bright render.ColorCurrentOrbit, via DrawEllipseClass)
// and a planted transfer's leg (Planned class, its per-leg colour, via
// PlotPolylineClass) must both ink in the SAME render — proving the two
// call sites are wired and don't clobber each other. The two patterns'
// pixel-level distinctness (contiguous run vs dash/gap cadence) is pinned
// directly in internal/tui/widgets/lineclass_test.go; this test is the
// screens-level proof that both call sites fire together on one frame.
//
// Uses the Kern→Cursor transfer fixture (shared with
// TestPredictedLegsPersistDuringActiveBurn) focused on the CRAFT rather
// than the destination body, and zoomed out from the default craft-orbit
// fit: at the tight default fit the leg's home-frame samples (anchored at
// their own future clock, when Kern has moved along its own orbit — see
// CONTEXT.md / the "inertial leg vs rebased arc" note) fall outside the
// tiny window fit to the parking orbit alone. Zooming out is exactly what
// a player would do to see both at once; it isn't a workaround for
// anything this PR changed.
func TestOwnOrbitAndNodeLegDrawDistinctClassesInSameFrame(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	setupKernCursorTransfer(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusCraft}

	v.Render(w, 0, 200, 60) // establishes baseScale before zooming out
	for i := 0; i < 5; i++ {
		v.ZoomOut()
	}
	v.Render(w, 0, 200, 60)

	if n := v.canvas.CountColor(render.ColorCurrentOrbit); n < 5 {
		t.Errorf("own craft's live orbit (Real class, bright) inked only %d cells alongside a planted leg", n)
	}
	legColor := render.ManeuverSegmentColor(0)
	if n := v.canvas.CountColor(legColor); n < 5 {
		t.Errorf("planted transfer leg (Planned class, dashed) inked only %d cells alongside the live orbit", n)
	}
}
