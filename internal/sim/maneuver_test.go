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

// TestFiniteNodeStartsActiveBurn: a node with Duration > 0 should not
// instantly mutate velocity; instead it sets World.ActiveBurn so the
// integrator burn loop runs across subsequent ticks.
func TestFiniteNodeStartsActiveBurn(t *testing.T) {
	w := mustWorld(t)
	vBefore := w.Craft.OrbitalSpeed()
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(-time.Second),
		DV:          50,
		Mode:        spacecraft.BurnPrograde,
		Duration:    60 * time.Second,
	})
	w.executeDueNodes()

	if w.ActiveBurn == nil {
		t.Fatalf("expected ActiveBurn to be set after finite node fired")
	}
	if w.ActiveBurn.DVRemaining != 50 {
		t.Errorf("DVRemaining = %g, want 50", w.ActiveBurn.DVRemaining)
	}
	if v := w.Craft.OrbitalSpeed(); math.Abs(v-vBefore) > 1e-9 {
		t.Errorf("velocity changed by %g during executeDueNodes; finite burn should defer to integrator", v-vBefore)
	}
	if len(w.Nodes) != 0 {
		t.Errorf("finite node should be popped from queue, got %d remaining", len(w.Nodes))
	}
}

// TestFiniteBurnDeliversDeltaVAcrossTicks: across enough warp-1 ticks
// for the requested duration, an active burn should deliver close to
// the requested Δv, consume fuel, and clear ActiveBurn when done.
func TestFiniteBurnDeliversDeltaVAcrossTicks(t *testing.T) {
	w := mustWorld(t)
	vBefore := w.Craft.OrbitalSpeed()
	fuelBefore := w.Craft.Fuel

	const targetDV = 5.0 // small enough to finish well within 60s budget
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime,
		DV:          targetDV,
		Mode:        spacecraft.BurnPrograde,
		Duration:    60 * time.Second,
	})

	// Warp 1×, BaseStep 50 ms → 60s window = 1200 ticks. Add safety margin.
	for i := 0; i < 2000 && w.ActiveBurn != nil || i == 0; i++ {
		w.Tick()
		if i > 0 && w.ActiveBurn == nil {
			break
		}
	}

	if w.ActiveBurn != nil {
		t.Fatalf("ActiveBurn should be cleared after Δv delivered or duration elapsed; remaining=%g", w.ActiveBurn.DVRemaining)
	}
	dv := w.Craft.OrbitalSpeed() - vBefore
	// Speed change isn't pure thrust Δv — gravity rotates v during the
	// burn. Within a fraction of an orbital period the magnitude change
	// should be within ~20% of target Δv (LEO period ≈ 5500s, burn ≈ 15s
	// at our default 1 kN thrust to deliver 5 m/s on 1 ton craft).
	if dv < targetDV*0.5 || dv > targetDV*1.5 {
		t.Errorf("speed change after finite burn: got %.3f m/s, expected ~%.3f m/s", dv, targetDV)
	}
	if w.Craft.Fuel >= fuelBefore {
		t.Errorf("fuel did not decrease: %g → %g", fuelBefore, w.Craft.Fuel)
	}
}

// TestFiniteBurnEndsAtDurationWhenDVNotMet: if the engine cannot deliver
// the requested Δv within the duration budget, the burn should still
// terminate at EndTime.
func TestFiniteBurnEndsAtDurationWhenDVNotMet(t *testing.T) {
	w := mustWorld(t)
	// Request way more Δv than the engine can produce in 1 second
	// (1 kN / 1000 kg = 1 m/s² → ~1 m/s in 1 s; ask for 10 000).
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime,
		DV:          10000,
		Mode:        spacecraft.BurnPrograde,
		Duration:    1 * time.Second,
	})

	// 50 ms base step × 30 ticks = 1.5 s — past the 1 s window.
	for i := 0; i < 60; i++ {
		w.Tick()
		if w.ActiveBurn == nil {
			break
		}
	}
	if w.ActiveBurn != nil {
		t.Errorf("burn should terminate at EndTime even if DV unmet; got DVRemaining=%g", w.ActiveBurn.DVRemaining)
	}
}

// TestPlanTransferLandsTwoNodes: PlanTransfer for a valid target body
// should plant exactly two ManeuverNodes (departure + arrival) with
// matching PrimaryIDs and a sensible time gap matching the returned
// TransferDt. Validates the sim → planner integration end-to-end.
func TestPlanTransferLandsTwoNodes(t *testing.T) {
	w := mustWorld(t)

	// Find Mars's index in Sol's body list.
	sys := w.System()
	marsIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Mars" {
			marsIdx = i
			break
		}
	}
	if marsIdx < 0 {
		t.Skip("Mars not in loaded Sol system — adjust if bodies changed")
	}

	plan, err := w.PlanTransfer(marsIdx)
	if err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	if plan == nil {
		t.Fatal("PlanTransfer returned nil plan with nil error")
	}
	if len(w.Nodes) != 2 {
		t.Fatalf("expected 2 planted nodes, got %d", len(w.Nodes))
	}
	if w.Nodes[0].PrimaryID != w.Craft.Primary.ID {
		t.Errorf("first (departure) node PrimaryID = %q, want craft primary %q",
			w.Nodes[0].PrimaryID, w.Craft.Primary.ID)
	}
	if w.Nodes[1].PrimaryID != sys.Bodies[marsIdx].ID {
		t.Errorf("second (arrival) node PrimaryID = %q, want mars %q",
			w.Nodes[1].PrimaryID, sys.Bodies[marsIdx].ID)
	}
	gap := w.Nodes[1].TriggerTime.Sub(w.Nodes[0].TriggerTime)
	if gap != plan.TransferDt {
		t.Errorf("planted-node time gap = %v, want plan.TransferDt = %v",
			gap, plan.TransferDt)
	}
}

// TestPlanTransferPlantsFiniteBurns: as of v0.3.4, auto-plant produces
// finite burns sized from craft thrust + mass (Duration > 0). An all-
// impulsive plant would feel instant to the player, defeating the
// finite-burn machinery that v0.2.1 introduced.
func TestPlanTransferPlantsFiniteBurns(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	marsIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Mars" {
			marsIdx = i
			break
		}
	}
	if marsIdx < 0 {
		t.Skip("Mars not in loaded Sol system")
	}
	if _, err := w.PlanTransfer(marsIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	if len(w.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(w.Nodes))
	}
	for i, n := range w.Nodes {
		if n.Duration <= 0 {
			t.Errorf("node %d Duration = %v, want > 0 (finite burn)", i, n.Duration)
		}
	}
}

// TestPlanTransferRejectsBadTargets: invalid index / system-primary /
// out-of-range targets surface as errors without planting.
func TestPlanTransferRejectsBadTargets(t *testing.T) {
	w := mustWorld(t)
	cases := []struct {
		name string
		idx  int
	}{
		{"system primary", 0},
		{"negative index", -1},
		{"out of range", 999},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := len(w.Nodes)
			if _, err := w.PlanTransfer(c.idx); err == nil {
				t.Errorf("expected error for %s", c.name)
			}
			if len(w.Nodes) != before {
				t.Errorf("PlanTransfer planted nodes despite error path: %d → %d",
					before, len(w.Nodes))
			}
		})
	}
}

// TestPorkchopGridForMoonTargetIsHeliocentric: regression for the
// v0.5.5 fix. Pre-fix bodyEphemeris returned moon's parent-relative
// position as if heliocentric, so PorkchopGrid for a moon target
// solved Lambert with one endpoint near the system origin and
// produced wildly wrong Δv (often <1 km/s — geometrically suspicious
// for a heliocentric transfer to anything in Earth's vicinity).
//
// Sanity gate: the cheapest Δv on a Earth → Luna porkchop grid must
// at minimum cover a typical Earth-escape (~3 km/s). If we see <1 km/s
// the bug has regressed.
func TestPorkchopGridForMoonTargetIsHeliocentric(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	moonIdx := -1
	for i := range sys.Bodies {
		if sys.Bodies[i].ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon missing from Sol")
	}
	depDays := []float64{0, 5, 10}
	tofDays := []float64{3, 5, 7}
	grid, err := w.PorkchopGrid(moonIdx, depDays, tofDays)
	if err != nil {
		t.Fatalf("PorkchopGrid: %v", err)
	}
	best := math.Inf(1)
	for _, row := range grid {
		for _, v := range row {
			if !math.IsNaN(v) && v < best {
				best = v
			}
		}
	}
	if math.IsInf(best, 1) {
		t.Fatal("entire porkchop grid was NaN — Lambert never converged")
	}
	if best < 1000 {
		t.Errorf("Earth → Luna best porkchop Δv = %.1f m/s, suspiciously cheap (heliocentric-vs-parent-relative bug?)", best)
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

// TestPlanTransferAtPlantsTwoNodes: PlanTransferAt for an arbitrary
// (depDay, tofDay) pair plants a departure + arrival with the correct
// primaries, finite durations, and a time gap matching tofDay.
func TestPlanTransferAtPlantsTwoNodes(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	marsIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Mars" {
			marsIdx = i
			break
		}
	}
	if marsIdx < 0 {
		t.Skip("Mars not in loaded Sol system")
	}

	const depDay, tofDay = 30.0, 260.0
	plan, err := w.PlanTransferAt(marsIdx, depDay, tofDay)
	if err != nil {
		t.Fatalf("PlanTransferAt: %v", err)
	}
	if plan == nil {
		t.Fatal("PlanTransferAt returned nil plan with nil error")
	}
	if len(w.Nodes) != 2 {
		t.Fatalf("expected 2 planted nodes, got %d", len(w.Nodes))
	}
	if w.Nodes[0].PrimaryID != w.Craft.Primary.ID {
		t.Errorf("departure PrimaryID = %q, want craft primary %q",
			w.Nodes[0].PrimaryID, w.Craft.Primary.ID)
	}
	if w.Nodes[1].PrimaryID != sys.Bodies[marsIdx].ID {
		t.Errorf("arrival PrimaryID = %q, want mars %q",
			w.Nodes[1].PrimaryID, sys.Bodies[marsIdx].ID)
	}
	gap := w.Nodes[1].TriggerTime.Sub(w.Nodes[0].TriggerTime).Seconds()
	wantGap := tofDay * 86400.0
	if math.Abs(gap-wantGap) > 1.0 {
		t.Errorf("planted-node gap = %.0f s, want %.0f s", gap, wantGap)
	}
	for i, n := range w.Nodes {
		if n.Duration <= 0 {
			t.Errorf("node %d Duration = %v, want > 0 (finite)", i, n.Duration)
		}
	}
}

// TestPlanTransferAtMatchesPorkchopGridCell: planting at an explicit
// (depDay, tofDay) should produce total Δv within tolerance of the
// porkchop grid's scored Δv for the same cell — the whole point of
// Enter-to-plant is that the cursor's number is what gets planted.
func TestPlanTransferAtMatchesPorkchopGridCell(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	marsIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Mars" {
			marsIdx = i
			break
		}
	}
	if marsIdx < 0 {
		t.Skip("Mars not in loaded Sol system")
	}
	depDays := []float64{30}
	tofDays := []float64{260}
	grid, err := w.PorkchopGrid(marsIdx, depDays, tofDays)
	if err != nil {
		t.Fatalf("PorkchopGrid: %v", err)
	}
	want := grid[0][0]
	if math.IsNaN(want) {
		t.Skip("porkchop cell did not converge — pick different depDay/tofDay")
	}
	plan, err := w.PlanTransferAt(marsIdx, depDays[0], tofDays[0])
	if err != nil {
		t.Fatalf("PlanTransferAt: %v", err)
	}
	got := plan.Departure.DV + plan.Arrival.DV
	if math.Abs(got-want)/want > 1e-6 {
		t.Errorf("plan Δv = %.3f m/s, grid cell Δv = %.3f m/s (rel diff %.2e)",
			got, want, math.Abs(got-want)/want)
	}
}

// TestPlanTransferAtRejectsBadInputs: out-of-range targets, zero TOF,
// and system-primary index all error without planting.
func TestPlanTransferAtRejectsBadInputs(t *testing.T) {
	w := mustWorld(t)
	cases := []struct {
		name   string
		idx    int
		depDay float64
		tofDay float64
	}{
		{"system primary", 0, 0, 100},
		{"negative index", -1, 0, 100},
		{"zero tof", 2, 0, 0},
		{"negative tof", 2, 0, -5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := len(w.Nodes)
			if _, err := w.PlanTransferAt(c.idx, c.depDay, c.tofDay); err == nil {
				t.Errorf("expected error for %s", c.name)
			}
			if len(w.Nodes) != before {
				t.Errorf("planted nodes despite error path: %d → %d",
					before, len(w.Nodes))
			}
		})
	}
}

// TestRefinePlanErrorsWithoutArrival: RefinePlan with no pending
// arrival node (fresh world, no transfer planted) returns an error
// and doesn't mutate Nodes.
func TestRefinePlanErrorsWithoutArrival(t *testing.T) {
	w := mustWorld(t)
	before := len(w.Nodes)
	if _, _, err := w.RefinePlan(); err == nil {
		t.Errorf("RefinePlan on empty-plan world: expected error")
	}
	if len(w.Nodes) != before {
		t.Errorf("RefinePlan planted/removed nodes on error path: %d → %d",
			before, len(w.Nodes))
	}
}

// TestRefinePlanUpdatesArrivalAfterPlanTransferAt: after planting a
// Lambert-based transfer via PlanTransferAt (so the planted Δv uses
// real ephemerides rather than Hohmann abstract math), RefinePlan
// immediately — before any drift — should give an arrival Δv close
// to the original, because the Lambert re-solve uses the same
// geometry. Also verifies the correction burn is inserted as a new
// node.
func TestRefinePlanUpdatesArrivalAfterPlanTransferAt(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	marsIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Mars" {
			marsIdx = i
			break
		}
	}
	if marsIdx < 0 {
		t.Skip("Mars not in loaded Sol system")
	}
	// depDay=0 so PlanTransferAt's Lambert r1 = Earth(t=0) matches the
	// craft's heliocentric position when RefinePlan runs immediately.
	// Any nonzero depDay would move Earth forward, and RefinePlan's
	// r1 (craft_helio_now ≈ Earth_now) would not match — the two
	// Lambert solutions would land on different trajectories and
	// arrival Δv would diverge legitimately.
	plan, err := w.PlanTransferAt(marsIdx, 0, 260)
	if err != nil {
		t.Fatalf("PlanTransferAt: %v", err)
	}
	origArr := plan.Arrival.DV
	origNodeCount := len(w.Nodes)

	corr, arr, err := w.RefinePlan()
	if err != nil {
		t.Fatalf("RefinePlan: %v", err)
	}
	if arr <= 0 {
		t.Errorf("refined arrival Δv = %.3f, want > 0", arr)
	}
	if corr < 0 {
		t.Errorf("correction Δv = %.3f, want ≥ 0", corr)
	}
	// RefinePlan ran Lambert with the same (r_craft ≈ Earth_now, r_mars_at_arrival,
	// tof = 30+260 days − 0) geometry as PlanTransferAt, so arrival Δv
	// should match exactly (same Lambert inputs → same Lambert v2).
	if math.Abs(arr-origArr)/origArr > 1e-4 {
		t.Errorf("refined arrival Δv = %.3f m/s, original = %.3f m/s (rel diff %.2e)",
			arr, origArr, math.Abs(arr-origArr)/origArr)
	}
	if len(w.Nodes) != origNodeCount+1 {
		t.Errorf("node count after refine = %d, want %d (orig + correction)",
			len(w.Nodes), origNodeCount+1)
	}
}
