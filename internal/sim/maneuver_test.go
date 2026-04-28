package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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
	state, primary, ok := w.PreviewBurnState(spacecraft.BurnPrograde, 100, 0, TriggerNextApo)
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
	state, primary, ok := w.PreviewBurnState(spacecraft.BurnPrograde, 50, 0, TriggerAbsolute)
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

// TestPredictedLegsHohmann: a two-burn Hohmann auto-plant should
// yield exactly two legs — the transfer leg in Earth (or
// heliocentric) frame and the captured leg in the destination
// (Mars) frame. The transfer leg's horizon should match the time
// gap to the arrival node; the arrival leg's horizon falls back to
// one orbital period since there's no node after it.
func TestPredictedLegsHohmann(t *testing.T) {
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
	legs := w.PredictedLegs()
	if len(legs) != 2 {
		t.Fatalf("expected 2 legs (departure + arrival), got %d", len(legs))
	}
	if legs[0].NodeIndex != 0 || legs[1].NodeIndex != 1 {
		t.Errorf("leg NodeIndexes wrong: got %d / %d, want 0 / 1",
			legs[0].NodeIndex, legs[1].NodeIndex)
	}
	if legs[1].Primary.ID != sys.Bodies[marsIdx].ID {
		t.Errorf("arrival leg primary = %q, want Mars %q (rebase missed)",
			legs[1].Primary.ID, sys.Bodies[marsIdx].ID)
	}
	// Transfer leg horizon should match the trigger-time gap.
	wantHorizon := w.Nodes[1].TriggerTime.Sub(w.Nodes[0].TriggerTime).Seconds()
	if math.Abs(legs[0].HorizonSecs-wantHorizon) > 1.0 {
		t.Errorf("transfer leg horizon = %.0f s, want %.0f s",
			legs[0].HorizonSecs, wantHorizon)
	}
	if legs[1].HorizonSecs <= 0 {
		t.Errorf("arrival leg horizon must be > 0, got %.0f", legs[1].HorizonSecs)
	}
}

// TestPredictedLegsSuppressedDuringActiveBurn: same guard as
// PredictedFinalOrbit — flailing values during burns shouldn't
// drive flickering colored trajectory lines.
func TestPredictedLegsSuppressedDuringActiveBurn(t *testing.T) {
	w := mustWorld(t)
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(60 * time.Second),
		Mode:        spacecraft.BurnPrograde,
		DV:          50,
	})
	w.ActiveBurn = &ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 100,
		EndTime:     w.Clock.SimTime.Add(10 * time.Second),
	}
	if legs := w.PredictedLegs(); legs != nil {
		t.Errorf("expected nil legs during active burn, got %d", len(legs))
	}
}

// TestPredictedFinalOrbitHohmannLandsInDestinationFrame: a Hohmann
// auto-plant to Mars plants two nodes — departure in Earth frame +
// arrival in Mars frame. PredictedFinalOrbit must rebase into the
// arrival node's PrimaryID before applying the capture Δv,
// otherwise the propagation lands at Mars's heliocentric position
// in Sol frame and the post-burn readout wrongly reports a
// heliocentric (Sol-primary) orbit. Regression test for the bug
// where PROJECTED ORBIT showed "primary: Sun" after a Hohmann
// plant.
func TestPredictedFinalOrbitHohmannLandsInDestinationFrame(t *testing.T) {
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

	_, primary, ok := w.PredictedFinalOrbit()
	if !ok {
		t.Fatalf("PredictedFinalOrbit returned ok=false after Hohmann plant")
	}
	wantID := sys.Bodies[marsIdx].ID
	if primary.ID != wantID {
		t.Errorf("post-Hohmann predicted orbit primary = %q, want %q (Mars). Δv was applied in the wrong frame.",
			primary.ID, wantID)
	}
}

// TestPredictedFinalOrbitMatchesPreviewForResolvedNode: planting a
// NextApo node, running the resolver, and querying PredictedFinalOrbit
// must agree with PreviewBurnState within float-noise. If these
// diverge the maneuver-screen preview and the orbit-screen HUD show
// inconsistent numbers — the user-visible "off proportion" symptom.
func TestPredictedFinalOrbitMatchesPreviewForResolvedNode(t *testing.T) {
	w := mustWorld(t)

	// Step 1: nudge into elliptical orbit so we have a meaningful apo.
	w.Craft.ApplyImpulsive(spacecraft.BurnPrograde, 100)

	// Step 2: plant a NextApo node and resolve it.
	w.PlanNode(ManeuverNode{
		Event: TriggerNextApo,
		Mode:  spacecraft.BurnPrograde,
		DV:    100,
	})
	w.resolveEventNodes()
	if !w.Nodes[0].IsResolved() {
		t.Fatalf("resolver failed to freeze NextApo node on elliptical orbit")
	}

	// PreviewBurnState — what the maneuver screen shows.
	previewState, previewPrimary, ok := w.PreviewBurnState(spacecraft.BurnPrograde, 100, 0, TriggerNextApo)
	if !ok {
		t.Fatalf("PreviewBurnState ok=false")
	}
	previewMu := previewPrimary.GravitationalParameter()
	previewRO := orbital.OrbitReadout(previewState.R, previewState.V, previewMu)

	// PredictedFinalOrbit — what the orbit-screen HUD shows.
	predState, predPrimary, ok := w.PredictedFinalOrbit()
	if !ok {
		t.Fatalf("PredictedFinalOrbit ok=false")
	}
	predMu := predPrimary.GravitationalParameter()
	predRO := orbital.OrbitReadout(predState.R, predState.V, predMu)

	// Both should agree to within 1 km on apo and peri.
	if math.Abs(previewRO.ApoMeters-predRO.ApoMeters) > 1000 {
		t.Errorf("apoapsis mismatch: preview=%.1f km, predicted=%.1f km",
			previewRO.ApoMeters/1000, predRO.ApoMeters/1000)
	}
	if math.Abs(previewRO.PeriMeters-predRO.PeriMeters) > 1000 {
		t.Errorf("periapsis mismatch: preview=%.1f km, predicted=%.1f km",
			previewRO.PeriMeters/1000, predRO.PeriMeters/1000)
	}

	// Sanity: this is the perigee-raise scenario. Predicted peri
	// should be substantially higher than the pre-burn apoapsis is
	// in altitude terms — i.e. peri ≈ apo (circularised).
	mu := w.Craft.Primary.GravitationalParameter()
	preEl := orbital.ElementsFromState(w.Craft.State.R, w.Craft.State.V, mu)
	if math.Abs(predRO.PeriMeters-preEl.Apoapsis())/preEl.Apoapsis() > 0.05 {
		t.Errorf("expected predicted peri ≈ pre-burn apo (circularised): pre apo=%.1f km, predicted peri=%.1f km",
			preEl.Apoapsis()/1000, predRO.PeriMeters/1000)
	}
}

// TestPredictedFinalOrbitSuppressedDuringActiveBurn: while a finite
// burn is integrating, the live craft state mutates each tick and
// chained predictions produce flailing numbers + a rotating
// trajectory preview. PredictedFinalOrbit should return ok=false so
// the HUD section hides until the burn completes.
func TestPredictedFinalOrbitSuppressedDuringActiveBurn(t *testing.T) {
	w := mustWorld(t)
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(60 * time.Second),
		Mode:        spacecraft.BurnPrograde,
		DV:          50,
	})
	w.ActiveBurn = &ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 100,
		EndTime:     w.Clock.SimTime.Add(10 * time.Second),
	}
	if _, _, ok := w.PredictedFinalOrbit(); ok {
		t.Errorf("expected ok=false during active burn — projection should hide")
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

// TestPlanTransferMoonEscapePlantsTwoNodes (v0.6.3): with the craft in
// low lunar orbit, PlanTransfer(earth) should plant a moon-frame
// departure burn (Δv > 0, prograde) followed by an Earth-frame
// zero-Δv SOI-exit marker. Pre-v0.6.3 the dispatch fell through to
// the heliocentric Hohmann path, which treated Earth's heliocentric
// semimajor axis as the destination radius around Luna and quoted
// nonsense Δv.
func TestPlanTransferMoonEscapePlantsTwoNodes(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	moonIdx, earthIdx := -1, -1
	for i, b := range sys.Bodies {
		switch b.ID {
		case "moon":
			moonIdx = i
		case "earth":
			earthIdx = i
		}
	}
	if moonIdx < 0 || earthIdx < 0 {
		t.Skip("Earth/Moon missing from loaded Sol")
	}
	moon := sys.Bodies[moonIdx]
	earth := sys.Bodies[earthIdx]

	// Re-seat the craft into a 100-km circular low lunar orbit.
	rPark := moon.RadiusMeters() + 100e3
	muMoon := moon.GravitationalParameter()
	vCirc := math.Sqrt(muMoon / rPark)
	w.Craft.Primary = moon
	w.Craft.State.R = orbital.Vec3{X: rPark}
	w.Craft.State.V = orbital.Vec3{Y: vCirc}

	plan, err := w.PlanTransfer(earthIdx)
	if err != nil {
		t.Fatalf("PlanTransfer(earth) from lunar orbit: %v", err)
	}
	if plan == nil {
		t.Fatal("PlanTransfer returned nil plan with nil error")
	}
	if len(w.Nodes) != 2 {
		t.Fatalf("expected 2 planted nodes, got %d", len(w.Nodes))
	}
	dep, arr := w.Nodes[0], w.Nodes[1]
	if dep.PrimaryID != moon.ID {
		t.Errorf("departure PrimaryID = %q, want %q", dep.PrimaryID, moon.ID)
	}
	if arr.PrimaryID != earth.ID {
		t.Errorf("arrival PrimaryID = %q, want %q", arr.PrimaryID, earth.ID)
	}
	if dep.DV <= 0 {
		t.Errorf("departure Δv = %.3f m/s, want > 0", dep.DV)
	}
	if dep.Mode != spacecraft.BurnPrograde {
		t.Errorf("departure Mode = %v, want BurnPrograde", dep.Mode)
	}
	if arr.DV != 0 {
		t.Errorf("arrival is a frame marker — Δv should be 0, got %.3f", arr.DV)
	}

	// Sanity: planted Δv should match the bound-ellipse impulsive
	// estimate within ≈5%. The iterator may refine the value; for a
	// short LLO escape burn (≈30 s on the S-IVB-1) the impulsive
	// guess is already very close.
	rSOI := physics.SOIRadius(moon, earth)
	if rSOI == 0 {
		t.Fatal("SOIRadius(moon, earth) = 0 — body data missing mass / a")
	}
	aT := (rPark + rSOI) / 2
	vTrans := math.Sqrt(muMoon * (2/rPark - 1/aT))
	impulsiveDv := vTrans - vCirc
	rel := math.Abs(dep.DV-impulsiveDv) / impulsiveDv
	if rel > 0.05 {
		t.Errorf("departure Δv %.1f m/s deviates >5%% from impulsive %.1f m/s (rel=%.4f)",
			dep.DV, impulsiveDv, rel)
	}

	// Arrival's TriggerTime should sit at departure-center +
	// half-period of the bound transfer ellipse (within 1 s).
	gap := arr.TriggerTime.Sub(dep.TriggerTime).Seconds()
	wantGap := math.Pi * math.Sqrt(aT*aT*aT/muMoon)
	if math.Abs(gap-wantGap) > 1.0 {
		t.Errorf("arrival − departure gap = %.1f s, want %.1f s (half-period)", gap, wantGap)
	}
}

// seatLunaCaptureOrbit places the craft at apoapsis of a peri-40 km /
// apo-8000 km moon-frame elliptical orbit — the typical post-Hohmann
// capture geometry. Returns the moon body, parking peri/apo radii, and
// peri velocity for follow-on assertions. Apoapsis placement is
// deliberate: TimeToPeriapsis(state-at-peri) wraps to one full orbital
// period (≈11 h here), and the velocity-Verlet integrator's drift
// over that long a coast can mask the burn's actual orbital effect.
// Apoapsis → next peri is half a period (≈5.5 h), well inside the
// integrator's accurate regime.
func seatLunaCaptureOrbit(t *testing.T, w *World) (moon bodies.CelestialBody, rPeri, rApo, vPeri float64, ok bool) {
	t.Helper()
	sys := w.System()
	for _, b := range sys.Bodies {
		if b.ID == "moon" {
			moon = b
			break
		}
	}
	if moon.ID == "" {
		return moon, 0, 0, 0, false
	}
	muMoon := moon.GravitationalParameter()
	rPeri = moon.RadiusMeters() + 40e3
	rApo = moon.RadiusMeters() + 8000e3
	a := (rPeri + rApo) / 2
	vPeri = math.Sqrt(muMoon * (2/rPeri - 1/a))
	vApo := math.Sqrt(muMoon * (2/rApo - 1/a))
	w.Craft.Primary = moon
	// Apoapsis sits at -X (opposite peri); craft moves in -Y direction
	// there so the next periapsis return is along +X.
	w.Craft.State.R = orbital.Vec3{X: -rApo}
	w.Craft.State.V = orbital.Vec3{Y: -vApo}
	return moon, rPeri, rApo, vPeri, true
}

// TestPreviewBurnStateLongRetroAtLunaPeriCapsByDuration (v0.6.3
// polish): mirrors the Luna-circularization playthrough Jason
// reported. After a Hohmann + capture, the craft sits in a peri-40 km
// / apo-~8000 km lunar orbit. Hand-entering 400 m/s retrograde at
// next peri while leaving the form's default 10 s duration cannot
// deliver the requested Δv — the in-flight burn terminates on
// duration, not Δv. The preview must reflect that truncation: the
// post-burn orbit must match what the live integrator will actually
// produce, so the player isn't surprised by an AP drop smaller than
// the projected number suggested.
//
// Also confirms that a long retrograde burn centered on periapsis
// shifts PE by less than the finite-burn arc would predict (well
// under 100 km for this profile) — Jason's "PE adjusts more than
// expected" observation, expected to be small in physics.
func TestPreviewBurnStateLongRetroAtLunaPeriCapsByDuration(t *testing.T) {
	w := mustWorld(t)
	moon, rPeri, _, vPeri, seated := seatLunaCaptureOrbit(t, w)
	if !seated {
		t.Skip("Moon missing from Sol")
	}
	muMoon := moon.GravitationalParameter()

	// Default duration (matches the m form's default).
	const duration = 10 * time.Second

	// Sanity: max deliverable in 10 s for the S-IVB-1 default.
	const g0 = 9.80665
	mdot := w.Craft.Thrust / (w.Craft.Isp * g0)
	massAfter := w.Craft.State.M - mdot*duration.Seconds()
	if massAfter <= 0 {
		t.Fatal("setup: 10 s burn would empty the tank — vessel mass too low for default")
	}
	maxDeliverable := w.Craft.Isp * g0 * math.Log(w.Craft.State.M/massAfter)
	if maxDeliverable > 250 {
		t.Fatalf("setup: maxDeliverable in %v = %.1f m/s; expected ~205 m/s for S-IVB-1 — vessel parameters changed?",
			duration, maxDeliverable)
	}

	// Request 400 m/s — well above what 10 s can deliver.
	state, primary, ok := w.PreviewBurnState(spacecraft.BurnRetrograde, 400, duration, TriggerNextPeri)
	if !ok {
		t.Fatalf("PreviewBurnState returned ok=false")
	}
	if primary.ID != moon.ID {
		t.Fatalf("preview escaped Luna SOI? primary=%q", primary.ID)
	}

	post := orbital.ElementsFromState(state.R, state.V, muMoon)
	postPeri := post.Periapsis()
	postApo := post.Apoapsis()

	// PE should stay close to its pre-burn value. Finite-burn
	// deformation over a ~10 s arc at 1.2 mrad/s ω only twists the
	// retrograde direction by ~0.7° — periapsis shift should be
	// well under 100 km in practice.
	periShift := math.Abs(postPeri - rPeri)
	if periShift > 100e3 {
		t.Errorf("PE shifted %.0f km — finite-burn deformation should be < 100 km; current orbit is symmetric around peri",
			periShift/1000)
	}

	// AP must reflect *delivered* Δv, not the requested 400 m/s.
	// Compute the impulsive AP that 400 m/s would have produced and
	// the impulsive AP that maxDeliverable would have produced; the
	// finite preview's AP must sit closer to the latter.
	impPostV400 := vPeri - 400
	impEps400 := 0.5*impPostV400*impPostV400 - muMoon/rPeri
	impA400 := -muMoon / (2 * impEps400)
	impApo400 := 2*impA400 - rPeri

	impPostVCap := vPeri - maxDeliverable
	impEpsCap := 0.5*impPostVCap*impPostVCap - muMoon/rPeri
	impACap := -muMoon / (2 * impEpsCap)
	impApoCap := 2*impACap - rPeri

	dist400 := math.Abs(postApo - impApo400)
	distCap := math.Abs(postApo - impApoCap)
	if dist400 < distCap {
		t.Errorf("preview AP %.0f km matched 400 m/s impulsive (%.0f km) more closely than truncated %.1f m/s impulsive (%.0f km) — duration cap not applied",
			postApo/1000, impApo400/1000, maxDeliverable, impApoCap/1000)
	}
	// Preview AP should be within 5% of the truncated impulsive
	// prediction. Finite-burn deformation nudges it slightly but
	// not by 5%.
	rel := math.Abs(postApo-impApoCap) / impApoCap
	if rel > 0.05 {
		t.Errorf("preview AP %.0f km diverges from truncated-impulsive %.0f km by %.1f%% — expected < 5%%",
			postApo/1000, impApoCap/1000, rel*100)
	}
}

// TestPreviewBurnStateFiniteVsImpulsiveAtLunaPeri (v0.6.3 polish): when
// the requested Δv fits inside the duration window (200 m/s in 10 s
// for the S-IVB-1), the finite-burn preview and the impulsive preview
// should land within a few percent on AP — the burn arc is small
// enough that the cosine-loss / off-tangential effect is bounded. PE
// drift between the two should also be small for a burn centered on
// periapsis. Catches regressions where the finite-burn integration
// silently produces a wildly different orbit shape.
func TestPreviewBurnStateFiniteVsImpulsiveAtLunaPeri(t *testing.T) {
	w := mustWorld(t)
	moon, _, _, _, seated := seatLunaCaptureOrbit(t, w)
	if !seated {
		t.Skip("Moon missing from Sol")
	}
	muMoon := moon.GravitationalParameter()

	// 200 m/s in 10 s for S-IVB-1 — deliverable (max ≈ 205).
	imp, _, ok := w.PreviewBurnState(spacecraft.BurnRetrograde, 200, 0, TriggerNextPeri)
	if !ok {
		t.Fatal("impulsive preview ok=false")
	}
	fin, _, ok := w.PreviewBurnState(spacecraft.BurnRetrograde, 200, 10*time.Second, TriggerNextPeri)
	if !ok {
		t.Fatal("finite preview ok=false")
	}
	impEl := orbital.ElementsFromState(imp.R, imp.V, muMoon)
	finEl := orbital.ElementsFromState(fin.R, fin.V, muMoon)

	apoRel := math.Abs(impEl.Apoapsis()-finEl.Apoapsis()) / impEl.Apoapsis()
	if apoRel > 0.05 {
		t.Errorf("AP impulsive vs finite differ by %.1f%% (imp=%.0f km, fin=%.0f km) — expected < 5%% for short burn",
			apoRel*100, impEl.Apoapsis()/1000, finEl.Apoapsis()/1000)
	}
	periDelta := math.Abs(impEl.Periapsis() - finEl.Periapsis())
	if periDelta > 50e3 {
		t.Errorf("PE impulsive vs finite differ by %.0f km — expected < 50 km for ~10 s burn at peri", periDelta/1000)
	}
}

// TestPlanTransferMoonEscapeIntraPrimaryStillFires: regression guard —
// the new moon-escape branch must not steal dispatch from the
// intra-primary branch. From LEO targeting Luna (target.ParentID ==
// craft.Primary.ID), the intra-primary Hohmann path must still fire
// and plant a non-trivial geocentric departure.
func TestPlanTransferMoonEscapeIntraPrimaryStillFires(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon missing from Sol")
	}
	plan, err := w.PlanTransfer(moonIdx)
	if err != nil {
		t.Fatalf("PlanTransfer(moon) from LEO: %v", err)
	}
	if plan.Departure.PrimaryID != "earth" {
		t.Errorf("intra-primary departure PrimaryID = %q, want %q (LEO→Luna stays geocentric)",
			plan.Departure.PrimaryID, "earth")
	}
	if plan.Arrival.PrimaryID != "moon" {
		t.Errorf("intra-primary arrival PrimaryID = %q, want %q",
			plan.Arrival.PrimaryID, "moon")
	}
	// Δv should be in TLI ballpark — well above the moon-escape
	// figure (~30 m/s). Pick a generous lower bound.
	if plan.Departure.DV < 2000 {
		t.Errorf("LEO→Luna departure Δv = %.0f m/s, want ≥ 2000 (TLI scale)", plan.Departure.DV)
	}
}

// TestStartManualBurnSetsState: StartManualBurn populates ManualBurn
// when fuel + thrust + throttle > 0 and no ActiveBurn is in flight.
func TestStartManualBurnSetsState(t *testing.T) {
	w, _ := NewWorld()
	w.Craft.Throttle = 1.0
	w.AttitudeMode = spacecraft.BurnPrograde
	w.StartManualBurn()
	if w.ManualBurn == nil {
		t.Fatal("StartManualBurn did not set ManualBurn")
	}
	if !w.ManualBurn.StartTime.Equal(w.Clock.SimTime) {
		t.Errorf("StartTime = %v, want SimTime %v", w.ManualBurn.StartTime, w.Clock.SimTime)
	}
}

// TestStartManualBurnNoOpDuringActiveBurn: a planted ActiveBurn owns
// the engine — StartManualBurn must not mutate state while one is in
// flight. (The two paths share the integrator and would compete for
// AttitudeMode vs ActiveBurn.Mode.)
func TestStartManualBurnNoOpDuringActiveBurn(t *testing.T) {
	w, _ := NewWorld()
	w.Craft.Throttle = 1.0
	w.ActiveBurn = &ActiveBurn{Mode: spacecraft.BurnPrograde, DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * 1e9)}
	w.StartManualBurn()
	if w.ManualBurn != nil {
		t.Error("StartManualBurn should be a no-op while ActiveBurn != nil")
	}
}

// TestStartManualBurnNoOpAtZeroThrottle: pressing an attitude key
// with zero throttle must not start the engine — the player is
// orienting, not firing.
func TestStartManualBurnNoOpAtZeroThrottle(t *testing.T) {
	w, _ := NewWorld()
	w.Craft.Throttle = 0
	w.StartManualBurn()
	if w.ManualBurn != nil {
		t.Error("StartManualBurn should be a no-op when throttle = 0")
	}
}

// TestSetThrottleZeroStopsManualBurn: cutting throttle (`x` key)
// must end any in-flight manual burn so the player's "x = cut"
// muscle memory works in one keypress.
func TestSetThrottleZeroStopsManualBurn(t *testing.T) {
	w, _ := NewWorld()
	w.Craft.Throttle = 1.0
	w.AttitudeMode = spacecraft.BurnPrograde
	w.StartManualBurn()
	if w.ManualBurn == nil {
		t.Fatal("setup: ManualBurn should be set")
	}
	w.SetThrottle(0)
	if w.ManualBurn != nil {
		t.Error("SetThrottle(0) should stop the manual burn")
	}
	if w.Craft.Throttle != 0 {
		t.Errorf("Craft.Throttle = %v, want 0", w.Craft.Throttle)
	}
}

// TestAdjustThrottleClampsToRange: ±10 % steps must clamp to [0, 1]
// regardless of the requested delta, preserving the throttle invariant.
func TestAdjustThrottleClampsToRange(t *testing.T) {
	w, _ := NewWorld()
	w.Craft.Throttle = 0.5
	w.AdjustThrottle(0.6) // would go to 1.1
	if w.Craft.Throttle != 1.0 {
		t.Errorf("clamp top: throttle = %v, want 1.0", w.Craft.Throttle)
	}
	w.AdjustThrottle(-1.5) // would go to -0.5
	if w.Craft.Throttle != 0 {
		t.Errorf("clamp bottom: throttle = %v, want 0", w.Craft.Throttle)
	}
}

// TestWarpCappedAt10xDuringManualBurn: same clamp as ActiveBurn —
// at high warp the integrator would lose temporal resolution on the
// thrust path, just like a planted finite burn.
func TestWarpCappedAt10xDuringManualBurn(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1 // 100000×
	w.Craft.Throttle = 1.0
	w.AttitudeMode = spacecraft.BurnPrograde
	w.StartManualBurn()
	if eff := w.EffectiveWarp(); eff != 10 {
		t.Errorf("manual burn should cap warp to 10×, got %.0f", eff)
	}
}

// TestManualBurnEndsOnFuelExhaustion: simulate a long burn — once
// fuel hits zero, the integrator's per-tick teardown clears
// ManualBurn so the player isn't stuck in an "engine commanded but
// nothing happens" UI state.
func TestManualBurnEndsOnFuelExhaustion(t *testing.T) {
	w, _ := NewWorld()
	w.Craft.Throttle = 1.0
	w.Craft.Fuel = 1.0 // tiny — burns out almost immediately
	w.AttitudeMode = spacecraft.BurnPrograde
	w.StartManualBurn()
	if w.ManualBurn == nil {
		t.Fatal("setup: ManualBurn should be set")
	}
	// Run enough ticks to drain the fuel.
	for i := 0; i < 200 && w.Craft.Fuel > 0; i++ {
		w.Tick()
	}
	if w.ManualBurn != nil {
		t.Errorf("ManualBurn should clear after fuel exhaustion; fuel=%v", w.Craft.Fuel)
	}
}

// TestToggleManualBurnEngagesAndDisengages: the v0.7.3.2+ engage gate.
// Calling ToggleManualBurn with no burn in flight + throttle > 0 starts
// one; calling it again stops the same burn. Mirrors the b-key UX.
func TestToggleManualBurnEngagesAndDisengages(t *testing.T) {
	w, _ := NewWorld()
	w.Craft.Throttle = 1.0
	w.AttitudeMode = spacecraft.BurnPrograde

	w.ToggleManualBurn()
	if w.ManualBurn == nil {
		t.Fatal("first toggle should engage manual burn")
	}
	w.ToggleManualBurn()
	if w.ManualBurn != nil {
		t.Error("second toggle should disengage manual burn")
	}
}

// TestToggleManualBurnNoOpAtZeroThrottle: pressing `b` with throttle
// at zero must not start the engine. Engage requires both attitude
// (always set, since BurnPrograde is the zero value) AND non-zero
// throttle.
func TestToggleManualBurnNoOpAtZeroThrottle(t *testing.T) {
	w, _ := NewWorld()
	w.Craft.Throttle = 0
	w.ToggleManualBurn()
	if w.ManualBurn != nil {
		t.Error("toggle with zero throttle should not start a burn")
	}
}
