package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Wave 1 known issue 6: the planner's draft preview applies the same
// orbital.ApsisDefined gate as the resolver. A next apo / next peri draft
// on an exactly circular orbit names no point, so the committed node will
// not fire and the canvas must not draw the burn (projection.resolved is
// the flag both the canvas and the PROJECTED ORBIT text read, #585).
func TestDraftPreviewDrawsNoBurnForAnUndefinedApsis(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range []sim.TriggerEvent{sim.TriggerNextPeri, sim.TriggerNextApo} {
		m := NewManeuver(Theme{})
		m.ResetEditing()
		selectFireAt(t, m, ev)
		c := w.ActiveCraft()

		// Positive control: eccentric orbit, the draft is drawn.
		setOrbit(w, 1.05)
		if p := m.projection(w, c, 300); !p.resolved {
			t.Fatalf("%v on an eccentric orbit: draft not drawn, so this test proves nothing", ev)
		}
		// Circular: nothing to draw.
		setOrbit(w, 1.0)
		if p := m.projection(w, c, 300); p.resolved {
			t.Errorf("%v on a circular orbit: the draft preview draws a burn the node will not fire", ev)
		}
	}
}
