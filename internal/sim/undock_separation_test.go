package sim

import (
	"fmt"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// buildStack docks n ported RCS Tugs into a single composite at w.Crafts[0]
// and returns it, nose set along the core's velocity so every Undock has an
// axis. The docks themselves are fixtures (DockCrafts); the capture that
// normally precedes them is covered in docking_port_test.go.
func buildStack(t *testing.T, w *World, n int) *spacecraft.Spacecraft {
	t.Helper()
	if n < 2 {
		t.Fatalf("buildStack: n must be >= 2, got %d", n)
	}
	earth := w.Systems[0].FindBody("Earth")
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	a := spacecraft.NewFromLoadout("RCS-tug")
	a.Name = "Core"
	a.Primary = *earth
	a.State = physics.StateVector{R: r, V: v, M: a.TotalMass()}
	a.CurrentAttitudeDir = v.Unit()
	w.Crafts = []*spacecraft.Spacecraft{a}
	w.ActiveCraftIdx = 0
	w.stampCraftID(a)

	for i := 1; i < n; i++ {
		b := spacecraft.NewFromLoadout("RCS-tug")
		b.Name = "Comp"
		b.Primary = *earth
		b.State = physics.StateVector{R: r, V: v, M: b.TotalMass()}
		w.stampCraftID(b)
		w.Crafts = append(w.Crafts, b)
		placePortToPort(w.Crafts[0], b, 0.3)
		w.DockCrafts(0, len(w.Crafts)-1)
	}
	if len(w.Crafts) != 1 {
		t.Fatalf("buildStack: expected 1 composite after %d docks, got %d craft", n, len(w.Crafts))
	}
	if got := len(w.Crafts[0].DockedComponents); got != n {
		t.Fatalf("buildStack: composite has %d components, want %d", got, n)
	}
	return w.Crafts[0]
}

// TestUndockSeparationClearsDockingGates is the #342 regression test, restated
// for the port gate (ADR 0053): undocking a stack of n components must leave
// no pair inside the docking gate, at every n from a plain two-component split
// up to five. The hulls now part from contact rather than 75 m apart, so what
// keeps them from re-capturing is the springs (0.15 m/s relative, over the
// gate's 0.1) and the hull/port geometry, not distance; and that must hold
// WITHOUT the re-arm latch, which is only the backstop.
func TestUndockSeparationClearsDockingGates(t *testing.T) {
	// Instrument check: the same predicate does return true for a pair that
	// sits in the gate, so a "false" below means something.
	ctrlA := spacecraft.NewFromLoadout("RCS-tug")
	ctrlB := spacecraft.NewFromLoadout("RCS-tug")
	ctrlA.CurrentAttitudeDir = orbital.Vec3{Y: 1}
	placePortToPort(ctrlA, ctrlB, 0.3)
	if !dockPairGateHolds(ctrlA, ctrlB) {
		t.Fatal("control pair at 0.3 m port to port reads out of the gate: the predicate cannot return true")
	}

	for n := 2; n <= 5; n++ {
		n := n
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			w, _ := NewWorld()
			buildStack(t, w, n)

			if !w.Undock(0) {
				t.Fatalf("Undock returned false on a %d-component composite", n)
			}
			if len(w.Crafts) != n {
				t.Fatalf("expected %d craft after undock, got %d", n, len(w.Crafts))
			}
			w.localReArms = nil // the latch is the backstop, not the protection
			for i := 0; i < len(w.Crafts); i++ {
				for j := i + 1; j < len(w.Crafts); j++ {
					if dockPairGateHolds(w.Crafts[i], w.Crafts[j]) {
						t.Errorf("n=%d: components %d and %d are inside the docking gate straight after undock", n, i, j)
					}
				}
			}
			if startsMagnet(w) {
				t.Errorf("n=%d: checkDocking began re-capturing the just-undocked stack", n)
			}
			if len(w.Crafts) != n {
				t.Errorf("n=%d: slate count = %d after the post-undock scan, want %d (unchanged)", n, len(w.Crafts), n)
			}
		})
	}
}
