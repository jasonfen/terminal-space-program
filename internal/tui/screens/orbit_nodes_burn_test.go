package screens

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

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
