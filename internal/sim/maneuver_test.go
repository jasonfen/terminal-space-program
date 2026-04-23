package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func TestPlanNodeKeepsSortedByTriggerTime(t *testing.T) {
	w := mustWorld(t)
	base := w.Clock.SimTime

	w.PlanNode(ManeuverNode{TriggerTime: base.Add(60 * time.Second), DV: 10, Mode: spacecraft.BurnPrograde})
	w.PlanNode(ManeuverNode{TriggerTime: base.Add(30 * time.Second), DV: 20, Mode: spacecraft.BurnPrograde})
	w.PlanNode(ManeuverNode{TriggerTime: base.Add(120 * time.Second), DV: 30, Mode: spacecraft.BurnPrograde})

	times := []time.Duration{
		w.Nodes[0].TriggerTime.Sub(base),
		w.Nodes[1].TriggerTime.Sub(base),
		w.Nodes[2].TriggerTime.Sub(base),
	}
	wanted := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second}
	for i := range times {
		if times[i] != wanted[i] {
			t.Errorf("sort[%d]: got %v, want %v", i, times[i], wanted[i])
		}
	}
}

func TestExecuteDueNodesFiresPastNodesAndPopsThem(t *testing.T) {
	w := mustWorld(t)
	dvBefore := w.Craft.OrbitalSpeed()
	fuelBefore := w.Craft.Fuel
	_ = dvBefore
	_ = fuelBefore

	// Plan a node in the past so the next Tick fires it.
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(-time.Second),
		DV:          50,
		Mode:        spacecraft.BurnPrograde,
	})
	if len(w.Nodes) != 1 {
		t.Fatalf("precondition: expected 1 node, got %d", len(w.Nodes))
	}

	w.executeDueNodes()

	if len(w.Nodes) != 0 {
		t.Errorf("after executeDueNodes, expected 0 nodes, got %d", len(w.Nodes))
	}
	// Fuel should have been consumed (rocket equation > 0 for any dv > 0
	// with positive Isp).
	if w.Craft.Fuel >= fuelBefore {
		t.Errorf("expected fuel decrease, got %g → %g", fuelBefore, w.Craft.Fuel)
	}
}

func TestExecuteDueNodesLeavesFutureNodes(t *testing.T) {
	w := mustWorld(t)
	// One past, one future.
	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(-1 * time.Second), DV: 10, Mode: spacecraft.BurnPrograde})
	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(60 * time.Second), DV: 20, Mode: spacecraft.BurnPrograde})

	w.executeDueNodes()

	if len(w.Nodes) != 1 {
		t.Fatalf("expected 1 surviving node, got %d", len(w.Nodes))
	}
	if w.Nodes[0].DV != 20 {
		t.Errorf("surviving node: got dv=%g, want 20", w.Nodes[0].DV)
	}
}

func TestClearNodesRemovesAll(t *testing.T) {
	w := mustWorld(t)
	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(10 * time.Second)})
	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(20 * time.Second)})
	w.ClearNodes()
	if len(w.Nodes) != 0 {
		t.Errorf("after ClearNodes: got %d nodes, want 0", len(w.Nodes))
	}
}

// TestNodeInertialPositionMatchesFuturePropagation verifies that the node
// preview position equals what the craft's future state would have been
// at that time if untouched — i.e., the preview is along the current
// orbit, not some offset.
func TestNodeInertialPositionMatchesFuturePropagation(t *testing.T) {
	w := mustWorld(t)
	dt := 300.0 // 5 min forward
	n := ManeuverNode{TriggerTime: w.Clock.SimTime.Add(time.Duration(dt) * time.Second)}

	want := w.propagateCraft(dt)
	wantInertial := w.BodyPosition(w.Craft.Primary).Add(want.R)
	got := w.NodeInertialPosition(n)

	if got.Sub(wantInertial).Norm() > 1e-3 {
		t.Errorf("NodeInertialPosition drift %.3e m", got.Sub(wantInertial).Norm())
	}
}

// TestNodeInertialPositionReturnsCraftInertialForPastNode confirms the
// past-due short-circuit: no propagation, just return where the craft
// currently is.
func TestNodeInertialPositionReturnsCraftInertialForPastNode(t *testing.T) {
	w := mustWorld(t)
	n := ManeuverNode{TriggerTime: w.Clock.SimTime.Add(-time.Second)}
	got := w.NodeInertialPosition(n)
	want := w.CraftInertial()
	if got.Sub(want).Norm() > 1e-6 {
		t.Errorf("past node: got %+v, want %+v", got, want)
	}
}

// TestPropagateCraftPreservesCircularRadius: propagating a circular LEO
// orbit by any dt should return a point on the same radius, within the
// integrator's 1% tolerance (matches predictor_test).
func TestPropagateCraftPreservesCircularRadius(t *testing.T) {
	w := mustWorld(t)
	r0 := w.Craft.State.R.Norm()
	for _, dt := range []float64{60, 600, 3000} {
		state := w.propagateCraft(dt)
		r := state.R.Norm()
		if math.Abs(r-r0)/r0 > 0.01 {
			t.Errorf("dt=%g: r=%g drifted >1%% from r0=%g", dt, r, r0)
		}
	}
}

func mustWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}
