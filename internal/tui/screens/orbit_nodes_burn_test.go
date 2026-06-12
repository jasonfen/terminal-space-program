package screens

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// countNodeTaggedCells scans the orbit canvas for cells tagged as a
// planted-node marker (CellTag.NodeIdx > 0 — the same tag HitHudNode
// uses to route node clicks).
func countNodeTaggedCells(v *OrbitView) int {
	n := 0
	for row := 0; row < v.canvas.Rows(); row++ {
		for col := 0; col < v.canvas.Cols(); col++ {
			if v.canvas.HitAt(col, row).NodeIdx > 0 {
				n++
			}
		}
	}
	return n
}

// TestNodeMarkersSuppressedDuringActiveBurn — regression for the
// "crosshair circles the orbit and beyond mid-burn" bug. The node
// marker position is recomputed every frame from the live (burning)
// orbit; drawNodes must suppress the whole node overlay while a
// finite burn is firing (mirrors the v0.6.1 dashed-preview skip,
// now hoisted above the marker loop).
func TestNodeMarkersSuppressedDuringActiveBurn(t *testing.T) {
	v := NewOrbitView(Theme{HUDBox: lipgloss.NewStyle()})
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft from NewWorld")
	}
	// Two nodes at different points on the orbit so at least one is
	// clear of the primary disk regardless of phase.
	c.Nodes = append(c.Nodes,
		spacecraft.ManeuverNode{DV: 100, TriggerTime: w.Clock.SimTime.Add(15 * time.Minute)},
		spacecraft.ManeuverNode{DV: 100, TriggerTime: w.Clock.SimTime.Add(45 * time.Minute)},
	)

	v.Render(w, 0, 180, 48)
	idle := countNodeTaggedCells(v)
	if idle == 0 {
		t.Fatal("no node markers drawn while idle — test can't observe suppression")
	}

	// Now simulate an in-progress finite burn.
	c.ActiveBurn = &spacecraft.ActiveBurn{}
	v.Render(w, 0, 180, 48)
	if burning := countNodeTaggedCells(v); burning != 0 {
		t.Errorf("node markers still drawn during active burn: %d tagged cells (want 0)", burning)
	}

	// And they come back once the burn ends.
	c.ActiveBurn = nil
	v.Render(w, 0, 180, 48)
	if after := countNodeTaggedCells(v); after == 0 {
		t.Error("node markers did not return after the burn completed")
	}
}

// TestPredictedLegsPersistDuringActiveBurn — the companion to the marker
// suppression above: while the Δ crosshairs stay off during a burn, the
// dashed post-burn orbit (the "purple preview" of where you're heading) is
// frozen from the last coasting frame and replayed steady, instead of
// vanishing at ignition. The live recompute can't restore it — PredictedLegs
// short-circuits to nil under an ActiveBurn — so this pins the freeze path.
// Uses the Kern→Cursor transfer (the local-arc fixture) because its planted
// legs are framed visibly when focused on the destination, unlike a tightly
// framed LEO craft whose raised leg projects off-canvas.
func TestPredictedLegsPersistDuringActiveBurn(t *testing.T) {
	v := newSOIPassTestView()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	cursorIdx, _, _ := setupKernCursorTransfer(t, w)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: cursorIdx}
	c := w.ActiveCraft()

	// The first planted leg inks in this colour; the Δ node marker shares it
	// but is suppressed during a burn, so leg-colour cells isolate the dashed
	// leg.
	legColor := render.ManeuverSegmentColor(0)

	v.Render(w, 0, 200, 60)
	idle := v.canvas.CountColor(legColor)
	if idle == 0 {
		t.Fatal("no post-burn leg ink while coasting — test can't observe persistence")
	}
	if len(v.burnFrozenLegs) == 0 {
		t.Fatal("coasting render captured no frozen legs")
	}

	// Mid-burn: the frozen dashed leg must still be on the canvas, with the
	// same ink it had while coasting (the snapshot is replayed verbatim).
	c.ActiveBurn = &spacecraft.ActiveBurn{}
	if burning := v.canvas.CountColor(legColor); burning != idle {
		t.Errorf("dashed leg ink changed during the burn: %d cells, want %d (frozen replay)", burning, idle)
	}

	// Discriminator: clearing the snapshot and re-rendering reproduces the
	// pre-fix behaviour — the leg vanishes — proving the persistence above is
	// the freeze, not a live recompute (PredictedLegs returns nil under an
	// ActiveBurn anyway, and drawNodes never re-captures mid-burn).
	v.burnFrozenLegs = nil
	v.burnFrozenArcs = nil
	v.burnFrozenRings = nil
	v.Render(w, 0, 200, 60)
	if gone := v.canvas.CountColor(legColor); gone != 0 {
		t.Errorf("%d leg cells with the frozen snapshot cleared, want 0", gone)
	}
}
