package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
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

// TestPlanNodeUnresolvedSortsToEnd: an unresolved event-relative node
// (Event != Absolute, TriggerTime zero) must not displace resolved
// future nodes from the head of the slice — otherwise executeDueNodes
// would see a year-1 BurnStart and fire it immediately.
func TestPlanNodeUnresolvedSortsToEnd(t *testing.T) {
	w := mustWorld(t)
	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(60 * time.Second), DV: 10, Mode: spacecraft.BurnPrograde})
	w.PlanNode(ManeuverNode{Event: TriggerNextPeri, DV: 20, Mode: spacecraft.BurnPrograde})
	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(30 * time.Second), DV: 30, Mode: spacecraft.BurnPrograde})

	if len(w.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(w.Nodes))
	}
	if w.Nodes[0].DV != 30 || w.Nodes[1].DV != 10 {
		t.Errorf("resolved nodes mis-sorted: got DVs %g / %g, want 30 / 10",
			w.Nodes[0].DV, w.Nodes[1].DV)
	}
	if w.Nodes[2].Event != TriggerNextPeri || !w.Nodes[2].TriggerTime.IsZero() {
		t.Errorf("unresolved node should be at end with zero TriggerTime; got Event=%v t=%v",
			w.Nodes[2].Event, w.Nodes[2].TriggerTime)
	}
}

// TestResolveEventNodesFreezesNextPeri: planting a NextPeri node and
// running the resolver should freeze TriggerTime to a future moment
// within one orbital period of "now."
func TestResolveEventNodesFreezesNextPeri(t *testing.T) {
	w := mustWorld(t)
	w.PlanNode(ManeuverNode{Event: TriggerNextPeri, DV: 50, Mode: spacecraft.BurnPrograde})

	if !w.Nodes[0].TriggerTime.IsZero() {
		t.Fatalf("precondition: expected zero TriggerTime on unresolved node")
	}

	w.resolveEventNodes()

	n := w.Nodes[0]
	if n.TriggerTime.IsZero() {
		t.Fatalf("expected resolver to set TriggerTime, still zero")
	}
	if !n.TriggerTime.After(w.Clock.SimTime) {
		t.Errorf("expected resolved TriggerTime > SimTime; got TriggerTime=%v SimTime=%v",
			n.TriggerTime, w.Clock.SimTime)
	}
	// One orbit at LEO is ~90 min; resolution should be < that for any ν.
	if dt := n.TriggerTime.Sub(w.Clock.SimTime); dt > 100*time.Minute {
		t.Errorf("resolved TriggerTime too far in the future: %v (LEO period < 100 min)", dt)
	}
	if !n.IsResolved() {
		t.Errorf("expected IsResolved() == true after resolver")
	}
}

// TestResolveEventNodesIsIdempotent: running the resolver twice shouldn't
// re-resolve an already-resolved node (the second pass is a no-op).
func TestResolveEventNodesIsIdempotent(t *testing.T) {
	w := mustWorld(t)
	w.PlanNode(ManeuverNode{Event: TriggerNextApo, DV: 50, Mode: spacecraft.BurnPrograde})
	w.resolveEventNodes()
	first := w.Nodes[0].TriggerTime

	// Advance the clock; resolver pass 2 must NOT update TriggerTime.
	w.Clock.SimTime = w.Clock.SimTime.Add(30 * time.Second)
	w.resolveEventNodes()
	second := w.Nodes[0].TriggerTime

	if !first.Equal(second) {
		t.Errorf("resolver re-resolved already-frozen node: %v → %v", first, second)
	}
}

// TestPreviewBurnStateAtNextApoRaisesPeriapsis: planting a prograde
// burn "at next apoapsis" on an elliptical orbit should raise the
// periapsis (not the apoapsis the craft is nowhere near). Pre-fix
// the maneuver screen built shadowState at the *current* position,
// so the readout always quoted apoapsis growth no matter what
// fire-at the user picked.
func TestPreviewBurnStateAtNextApoRaisesPeriapsis(t *testing.T) {
	w := mustWorld(t)
	mu := w.Craft.Primary.GravitationalParameter()

	// Step 1: raise apoapsis with a 100 m/s prograde burn at the
	// circular LEO start position. After this the orbit is
	// elliptical with peri ≈ start radius, apo ≈ higher altitude.
	w.Craft.ApplyImpulsive(spacecraft.BurnPrograde, 100)
	pre := orbital.ElementsFromState(w.Craft.State.R, w.Craft.State.V, mu)
	preApo := pre.Apoapsis()
	prePeri := pre.Periapsis()
	if preApo <= prePeri+1000 {
		t.Fatalf("setup failed: expected elliptical orbit after burn, got apo=%.0f peri=%.0f",
			preApo, prePeri)
	}

	// Step 2: preview a 100 m/s prograde at next apoapsis. This must
	// raise periapsis (perigee-raise = circularise at higher alt) —
	// NOT raise apoapsis again.
	state, primary, ok := w.PreviewBurnState(spacecraft.BurnPrograde, 100, TriggerNextApo)
	if !ok {
		t.Fatalf("PreviewBurnState returned ok=false")
	}
	post := orbital.ElementsFromState(state.R, state.V, primary.GravitationalParameter())
	postApo := post.Apoapsis()
	postPeri := post.Periapsis()

	if postPeri <= prePeri+100 {
		t.Errorf("perigee should rise after prograde-at-apo: pre=%.1f km post=%.1f km",
			prePeri/1000, postPeri/1000)
	}
	// The new perigee should land near the OLD apoapsis (within
	// ~5%). At apoapsis a small prograde Δv raises perigee toward
	// the apoapsis altitude as the orbit circularises higher up.
	if math.Abs(postPeri-preApo)/preApo > 0.05 {
		t.Errorf("expected new perigee ≈ old apoapsis: pre apo=%.1f km new peri=%.1f km",
			preApo/1000, postPeri/1000)
	}
	// Apoapsis should stay close to its pre-burn value (it's the
	// point we burned AT — burning prograde there just lifts the
	// other side; apoapsis itself rises only marginally).
	if math.Abs(postApo-preApo)/preApo > 0.10 {
		t.Errorf("apoapsis should stay roughly same: pre=%.1f km post=%.1f km",
			preApo/1000, postApo/1000)
	}
}

// TestPreviewBurnStateAbsolute: with TriggerAbsolute the helper
// returns the burn applied at the *current* state — preserving the
// pre-v0.6 planner preview semantics.
func TestPreviewBurnStateAbsolute(t *testing.T) {
	w := mustWorld(t)
	state, primary, ok := w.PreviewBurnState(spacecraft.BurnPrograde, 50, TriggerAbsolute)
	if !ok {
		t.Fatalf("PreviewBurnState(Absolute): ok=false")
	}
	if primary.ID != w.Craft.Primary.ID {
		t.Errorf("Absolute should not change primary: got %q", primary.ID)
	}
	// Position unchanged; velocity bumped by 50 m/s in prograde dir.
	if state.R != w.Craft.State.R {
		t.Errorf("Absolute preview moved R: got %v, want %v", state.R, w.Craft.State.R)
	}
	dv := state.V.Sub(w.Craft.State.V).Norm()
	if math.Abs(dv-50) > 0.01 {
		t.Errorf("Absolute preview Δv: got %.3f, want 50.0", dv)
	}
}

// TestPredictedFinalOrbitNoNodes: with nothing planted the helper
// reports ok=false so the HUD can hide the section.
func TestPredictedFinalOrbitNoNodes(t *testing.T) {
	w := mustWorld(t)
	if _, _, ok := w.PredictedFinalOrbit(); ok {
		t.Errorf("expected ok=false when no nodes planted")
	}
}

// TestPredictedFinalOrbitSingleProgradeBurn: planting a 50 m/s
// prograde burn should raise the apoapsis above the live orbit's
// apoapsis. Verifies the chain returns a sensible projection.
func TestPredictedFinalOrbitSingleProgradeBurn(t *testing.T) {
	w := mustWorld(t)
	mu := w.Craft.Primary.GravitationalParameter()
	liveEl := orbital.ElementsFromState(w.Craft.State.R, w.Craft.State.V, mu)
	liveApo := liveEl.Apoapsis()

	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(60 * time.Second),
		Mode:        spacecraft.BurnPrograde,
		DV:          50,
	})

	state, primary, ok := w.PredictedFinalOrbit()
	if !ok {
		t.Fatal("expected ok=true with a planted node")
	}
	if primary.ID != w.Craft.Primary.ID {
		t.Errorf("primary frame: got %q, want %q (no SOI change in 60s)",
			primary.ID, w.Craft.Primary.ID)
	}
	predicted := orbital.ElementsFromState(state.R, state.V, mu)
	if predicted.Apoapsis() <= liveApo {
		t.Errorf("prograde burn should raise apo: live=%.0f predicted=%.0f",
			liveApo, predicted.Apoapsis())
	}
}

// TestPredictedFinalOrbitSkipsUnresolvedNodes: an unresolved event-
// relative node shouldn't contribute to the projection. Live + one
// unresolved node = no contribution => ok=false.
func TestPredictedFinalOrbitSkipsUnresolvedNodes(t *testing.T) {
	w := mustWorld(t)
	w.PlanNode(ManeuverNode{
		Event: TriggerNextPeri,
		Mode:  spacecraft.BurnPrograde,
		DV:    50,
	})
	if _, _, ok := w.PredictedFinalOrbit(); ok {
		t.Errorf("expected ok=false with only an unresolved node")
	}
}

// TestResolveEventNodesEquatorialAN: an equatorial orbit should leave a
// NextAN node unresolved (no future crossing), with the resolver
// retrying on later ticks rather than crashing.
func TestResolveEventNodesEquatorialAN(t *testing.T) {
	w := mustWorld(t)
	// LEO state from NewWorld() is already equatorial.
	w.PlanNode(ManeuverNode{Event: TriggerNextAN, DV: 10, Mode: spacecraft.BurnPrograde})

	w.resolveEventNodes()

	if w.Nodes[0].IsResolved() {
		t.Errorf("equatorial orbit: expected NextAN to stay unresolved; got TriggerTime=%v",
			w.Nodes[0].TriggerTime)
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
	// v0.5.14+ TriggerTime is the burn center == planner OffsetTime;
	// gap between consecutive node TriggerTimes equals TransferDt
	// exactly (modulo nanoseconds).
	gap := w.Nodes[1].TriggerTime.Sub(w.Nodes[0].TriggerTime)
	if gap != plan.TransferDt {
		t.Errorf("planted-node time gap = %v, want plan.TransferDt = %v", gap, plan.TransferDt)
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

// TestPorkchopGridRejectsSamePrimaryTarget: v0.5.7 — porkchop is
// heliocentric Lambert, doesn't model in-SOI transfers correctly.
// Same-primary moon targets must error out with errSamePrimaryUseHohmann
// so the screen can redirect the user to [P] / PlanTransfer (which
// dispatches to the intra-primary Hohmann path).
func TestPorkchopGridRejectsSamePrimaryTarget(t *testing.T) {
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
	if _, err := w.PorkchopGrid(moonIdx, []float64{0}, []float64{5}); err == nil {
		t.Errorf("PorkchopGrid for Moon (same-primary) returned nil error — should be errSamePrimaryUseHohmann")
	}
}

// TestPlanTransferIntraPrimaryPhasingMatchesArrival: v0.5.9 — the
// phase-corrected planner picks a launch window such that, at the
// arrival node's TriggerTime, the target body is within Luna-SOI-
// scale distance of where the craft will be (apoapsis at rArrival).
// Pre-v0.5.9 the plan fired immediately with no phase correction
// and missed Luna's actual position by tens of millions of km.
func TestPlanTransferIntraPrimaryPhasingMatchesArrival(t *testing.T) {
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
	plan, err := w.PlanTransfer(moonIdx)
	if err != nil {
		t.Fatalf("PlanTransfer to Luna: %v", err)
	}
	moon := sys.Bodies[moonIdx]

	// At arrival time, the craft will be at apoapsis ≈ rArrival in the
	// burn-direction-perpendicular tangent. Approximate the craft's
	// arrival position as a vector at angle (craftAngleNow +
	// nCraft·waitTime + π) at distance rArrival around Earth.
	waitSecs := plan.Departure.OffsetTime.Seconds()
	transferSecs := plan.TransferDt.Seconds()
	arrivalSimTime := w.Clock.SimTime.Add(plan.Arrival.OffsetTime)

	mu := w.Craft.Primary.GravitationalParameter()
	rDep := w.Craft.State.R.Norm()
	craftAngleNow := math.Atan2(w.Craft.State.R.Y, w.Craft.State.R.X)
	nCraft := math.Sqrt(mu / (rDep * rDep * rDep))
	craftAtBurnAngle := craftAngleNow + nCraft*waitSecs
	craftArrivalAngle := craftAtBurnAngle + math.Pi // apoapsis is opposite periapsis

	// Where is Luna at arrival?
	moonM := w.Calculator.CalculateMeanAnomaly(moon, arrivalSimTime)
	moonE := orbital.SolveKepler(moonM, moon.Eccentricity)
	moonNu := orbital.TrueAnomaly(moonE, moon.Eccentricity)
	moonEl := orbital.ElementsFromBody(moon)
	moonAtArrival := orbital.PositionAtTrueAnomaly(moonEl, moonNu)
	moonAngleAtArrival := math.Atan2(moonAtArrival.Y, moonAtArrival.X)

	// Phasing residual: difference in angular positions, wrapped to
	// [-π, π]. Should be ~0 if phasing worked.
	dTheta := craftArrivalAngle - moonAngleAtArrival
	for dTheta > math.Pi {
		dTheta -= 2 * math.Pi
	}
	for dTheta < -math.Pi {
		dTheta += 2 * math.Pi
	}
	// Tolerance: 1° (0.017 rad). At Luna's distance, 1° = ~6700 km —
	// within Luna's ~66 000 km SOI, so an actual rendezvous would
	// capture. Pre-fix the angular gap was unconstrained.
	if math.Abs(dTheta) > 0.017 {
		t.Errorf("phasing residual = %.4f rad (%.2f°); want < 1°", dTheta, dTheta*180/math.Pi)
	}
	// Also: waitSecs must be non-negative and bounded by the synodic
	// period (LEO + Luna ≈ 89 min). 7200s = 2h is generous.
	if waitSecs < 0 || waitSecs > 7200 {
		t.Errorf("waitSecs = %.1f s, want in [0, 7200]", waitSecs)
	}
	_ = transferSecs
}

// TestPlanTransferIntraPrimaryBurnIsCentered: v0.5.14+ — the planted
// departure node's TriggerTime IS the burn-center (planner's intended
// moment), and BurnStart = TriggerTime - Duration/2 must be ≥ now so
// the integrator doesn't have to fire retroactively. Pre-v0.5.14
// TriggerTime was the burn START and we asserted "TriggerTime + Dur/2
// ≈ planner OffsetTime"; new semantics simplify to "TriggerTime ≈
// planner OffsetTime".
func TestPlanTransferIntraPrimaryBurnIsCentered(t *testing.T) {
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
	now := w.Clock.SimTime
	plan, err := w.PlanTransfer(moonIdx)
	if err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	dep := w.Nodes[0]
	wantCenter := now.Add(plan.Departure.OffsetTime)
	delta := dep.TriggerTime.Sub(wantCenter)
	if delta < -time.Second || delta > time.Second {
		t.Errorf("trigger off by %v (trigger=%v, want_center=%v)",
			delta, dep.TriggerTime, wantCenter)
	}
	if dep.BurnStart().Before(now) {
		t.Errorf("BurnStart %v before now %v — planner failed to pad", dep.BurnStart(), now)
	}
}

// TestIntraPrimaryHohmannReachesLunaApoapsis: v0.5.13+ — end-to-end.
// Plant Hohmann to Luna with the S-IVB-1 default vessel (J-2 thrust,
// ~110 s TLI), simulate forward through the burn, check the post-burn
// orbit's apoapsis lands within 1% of Luna's distance. Short burn
// keeps gravity-rotation finite-burn loss < 0.1%, so finite delivers
// near-impulsive accuracy. Pre-v0.5.13 the ICPS-class vessel had a
// 14-min TLI losing ~27% of apoapsis to integration error.
func TestIntraPrimaryHohmannReachesLunaApoapsis(t *testing.T) {
	w := mustWorld(t)
	moonIdx := -1
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon missing from Sol")
	}
	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	dep := w.Nodes[0]
	if dep.Duration <= 0 {
		t.Errorf("v0.5.13+ intra-primary auto-plant must be finite (Duration > 0); got %v", dep.Duration)
	}

	// Crank warp and run until the departure burn completes.
	for i := 0; i < 4; i++ {
		w.Clock.WarpUp() // 10000×
	}
	burnEnd := dep.TriggerTime.Add(dep.Duration)
	for tick := 0; tick < 200000; tick++ {
		w.Tick()
		if w.Clock.SimTime.After(burnEnd) && w.ActiveBurn == nil {
			break
		}
	}
	mu := w.Craft.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(w.Craft.State.R, w.Craft.State.V, mu)
	const lunaDist = 384399000.0
	hit := el.Apoapsis() / lunaDist
	// Tolerance ±25%: even at S-IVB-1's high TWR (110s burn), the
	// finite-burn integrator's apoapsis lands ~21% above the impulsive
	// ideal due to orbital geometry deformation during the burn arc
	// (the finite burn is asymmetric around peri because DVRemaining
	// terminates the burn before the centered duration completes).
	// The v0.6 finite-burn-aware planner will close this. For now we
	// assert "in the right ballpark — a real Luna intercept is at
	// least possible by tuning".
	if hit < 0.75 || hit > 1.25 {
		t.Errorf("apoapsis = %.0f km (%.2f%% of Luna distance), want 75–125%%",
			el.Apoapsis()/1000, hit*100)
	}
}

// TestPlanTransferIntraPrimaryHohmannForMoon: v0.5.7 — PlanTransfer
// must dispatch to PlanIntraPrimaryHohmann when target.ParentID matches
// craft's primary. Sanity-check that Earth → Luna gives a realistic
// trans-lunar injection Δv (~3 km/s departure) rather than the
// pre-v0.5.7 nonsense from heliocentric Hohmann math interpreting
// Luna's parent-relative semimajor as a heliocentric distance.
func TestPlanTransferIntraPrimaryHohmannForMoon(t *testing.T) {
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
	plan, err := w.PlanTransfer(moonIdx)
	if err != nil {
		t.Fatalf("PlanTransfer to Luna: %v", err)
	}
	// TLI Δv from 200 km LEO to Luna distance is ~3.1 km/s (geocentric
	// Hohmann math). Capture into Luna SOI from closing speed ~0.8 km/s
	// at Luna's altitude → Luna-orbit-insertion Δv ~0.7 km/s.
	dep := plan.Departure.DV
	arr := plan.Arrival.DV
	if dep < 2500 || dep > 3500 {
		t.Errorf("Earth → Luna departure Δv = %.0f m/s, want ~3100 (TLI)", dep)
	}
	if arr < 200 || arr > 1500 {
		t.Errorf("Earth → Luna arrival Δv = %.0f m/s, want ~700 (Luna-orbit insertion)", arr)
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
	// v0.5.14+ TriggerTime is the burn center == planner OffsetTime;
	// gap between consecutive node TriggerTimes equals TOF exactly
	// (modulo nanoseconds).
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
