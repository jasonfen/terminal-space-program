package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0045 S7 (#400) acceptance tests: Engage becomes the agreement to
// meet, and a planted Meeting Planner node's own arrival — however far
// out — is what it commits to, never a re-search within the 4h window
// that bounds the current-course fallback.

// TestRendezvousCommitWithPlan_MeetingNode_ArrivalBeyondHorizon is the
// #400 acceptance test: "Engage commits to a planted Meeting Planner
// node's arrival 8h out." Plants a real Meeting Burn via PlanMeetingBurn
// (so BurnDirUnit/DV/TriggerTime all come from the actual solver), then
// overrides the node's MeetingArrivalSec to exactly 8h — well past
// rendezvousCommitHorizonSec (4h) — for a deterministic scenario
// regardless of which lap row the small-lag fixture happens to solve.
// RendezvousCommitWithPlan must commit to TriggerTime+8h directly: if it
// instead ran rendezvousCommitFromPlantedNode's horizon-bounded search
// (the trim-nudge sibling's behaviour), an encounter 8h out — double the
// 4h window — could never be found, and the commit would refuse.
func TestRendezvousCommitWithPlan_MeetingNode_ArrivalBeyondHorizon(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()

	ladder, err := w.RecommendMeetingLadder(planner.MeetingTheirOrbit)
	if err != nil {
		t.Fatalf("RecommendMeetingLadder err: %v", err)
	}
	var pick int
	found := false
	for _, row := range ladder.Rows {
		if row.Ok {
			pick, found = row.Laps, true
			break
		}
	}
	if !found {
		t.Fatalf("expected at least one Ok row: %+v", ladder.Rows)
	}
	if _, err := w.PlanMeetingBurn(planner.MeetingTheirOrbit, pick); err != nil {
		t.Fatalf("PlanMeetingBurn err: %v", err)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 planted node, got %d", len(c.Nodes))
	}

	// Override to an 8h wait, deterministic and well past the 4h horizon
	// regardless of what lap row the solver actually picked above.
	const eightHours = 8 * 3600.0
	c.Nodes[0].MeetingArrivalSec = eightHours
	wantTau := c.Nodes[0].TriggerTime.Add(time.Duration(eightHours * float64(time.Second)))

	plan, ok := w.RendezvousCommitWithPlan()
	if !ok {
		t.Fatal("RendezvousCommitWithPlan: ok=false, want true (structural gates all pass)")
	}
	if plan.Tau.IsZero() {
		t.Fatal("plan.Tau is zero — the planted Meeting Burn node was not honored")
	}
	if diff := plan.Tau.Sub(wantTau); diff < -time.Second || diff > time.Second {
		t.Errorf("plan.Tau = %v, want %v (node.TriggerTime + 8h) — got a difference of %v; "+
			"a horizon-bounded search would have refused entirely (8h > 4h) rather than land near this", plan.Tau, wantTau, diff)
	}
	if plan.CommittedCA <= 0 {
		t.Errorf("plan.CommittedCA = %.3f, want > 0", plan.CommittedCA)
	}
	if plan.MeetingPlaceLabel != planner.MeetingTheirOrbit.String() {
		t.Errorf("plan.MeetingPlaceLabel = %q, want %q", plan.MeetingPlaceLabel, planner.MeetingTheirOrbit.String())
	}
	if plan.MeetingLaps != pick {
		t.Errorf("plan.MeetingLaps = %d, want %d", plan.MeetingLaps, pick)
	}
}

// TestRendezvousCommitWithPlan_MeetingNode_NoMeetingArrivalFallsThrough
// is a companion to the test above: a node carrying AdvisoryKeyMeetingBurn
// but no MeetingArrivalSec (the "predates this field" / "didn't clear the
// lead buffer" case PlanMeetingBurn's own doc comment names) must not be
// treated as a zero-wait commit — it should read as "no plan info" and
// let RendezvousCommitWithPlan fall through to Source 3 exactly as if
// nothing had been planted.
func TestRendezvousCommitWithPlan_MeetingNode_NoMeetingArrivalFallsThrough(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()
	if _, err := w.PlanMeetingBurn(planner.MeetingTheirOrbit, ladderFirstOkLap(t, w)); err != nil {
		t.Fatalf("PlanMeetingBurn err: %v", err)
	}
	c.Nodes[0].MeetingArrivalSec = 0 // simulate a pre-#400 node

	// This geometry is the zero-relative-drift stalemate (rendezvousSmallLagWorld's
	// own doc comment / TestRendezvousCommitMatchedOrbitsRefusesPhantomNudge):
	// Source 3's current-course search finds nothing either, so falling
	// through correctly still yields ok=true with a ZERO Tau (agreed, no
	// plan), never a phantom commit to "now".
	plan, ok := w.RendezvousCommitWithPlan()
	if !ok {
		t.Fatal("RendezvousCommitWithPlan: ok=false, want true (structural gates pass)")
	}
	if !plan.Tau.IsZero() {
		t.Errorf("plan.Tau = %v, want zero — a MeetingArrivalSec<=0 node must not commit to a zero-second wait", plan.Tau)
	}
}

// ladderFirstOkLap is a small helper: the first Ok row's lap count from a
// MeetingTheirOrbit ladder on w, or a fatal test failure.
func ladderFirstOkLap(t *testing.T, w *World) int {
	t.Helper()
	ladder, err := w.RecommendMeetingLadder(planner.MeetingTheirOrbit)
	if err != nil {
		t.Fatalf("RecommendMeetingLadder err: %v", err)
	}
	for _, row := range ladder.Rows {
		if row.Ok {
			return row.Laps
		}
	}
	t.Fatalf("expected at least one Ok row: %+v", ladder.Rows)
	return 0
}

// rendezvousBeyondFourHourWorld mirrors rendezvousBeyondTwoHourWorld
// (rendezvous_horizon_test.go) but widens the phase offset so the true
// first closest approach sits PAST rendezvousCommitHorizonSec (4h)
// instead of merely past the old 2h K cap — the geometry
// rendezvousCommitCurrentCourse's own horizon bound needs to be tested
// against directly.
func rendezvousBeyondFourHourWorld(t *testing.T) (*World, orbital.Vec3State, orbital.Vec3State) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("Earth")
	if earth == nil {
		t.Fatal("setup: Earth missing from Sol")
	}
	mu := earth.GravitationalParameter()

	rA := earth.RadiusMeters() + 500e3
	vA := math.Sqrt(mu / rA)
	a := w.Crafts[0]
	a.Primary = *earth
	a.State.R = orbital.Vec3{X: rA}
	a.State.V = orbital.Vec3{Y: vA}

	const phaseDeg = 80.0
	th := phaseDeg * math.Pi / 180
	rB := earth.RadiusMeters() + 650e3
	vB := math.Sqrt(mu / rB)
	b := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	b.Primary = *earth
	b.State = physics.StateVector{
		R: orbital.Vec3{X: rB * math.Cos(th), Y: rB * math.Sin(th)},
		V: orbital.Vec3{X: -vB * math.Sin(th), Y: vB * math.Cos(th)},
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)

	stateA := orbital.Vec3State{R: a.State.R, V: a.State.V}
	stateB := orbital.Vec3State{R: b.State.R, V: b.State.V}
	return w, stateA, stateB
}

// TestRendezvousCommitCurrentCourse_BoundedToFourHours is the #400
// acceptance test: "a test that rendezvousCommitCurrentCourse still
// refuses beyond 4h (the search window is untouched)." Source 3 (the
// current-course fallback) is a SEARCH restricted to
// rendezvousCommitHorizonSec, unlike Source 2's Meeting-Planner commit
// above, which happily committed to 8h. This scenario's TRUE first
// closest approach — confirmed via a generous 10x horizon — sits well
// past 4h (planner.NextClosestApproach's own documented edge-snap
// behaviour for a still-converging window that hasn't reached its
// minimum: it returns the WINDOW EDGE with ok=true rather than an error,
// see rendezvous_horizon_test.go's TestRendezvousAdvisoryMatchesTargetChipHorizon
// for the same behaviour pinned at the 2h/4h boundary). So "refuses
// beyond 4h" is verified here as "never commits to a τ past the 4h
// horizon", which is the load-bearing property either way the search
// resolves: a regression that widened or removed the bound would let
// this test's τ jump out to the TRUE ~11h encounter instead of staying
// capped at the window edge.
func TestRendezvousCommitCurrentCourse_BoundedToFourHours(t *testing.T) {
	w, stateA, stateB := rendezvousBeyondFourHourWorld(t)
	earth := w.Crafts[0].Primary
	mu := earth.GravitationalParameter()

	// Non-vacuous guard: confirm the TRUE closest approach sits well past
	// the 4h horizon under test — a wide, generous search window (10x)
	// stands in for "no bound at all".
	const wideHorizonSec = 10 * rendezvousCommitHorizonSec
	wideTCA, _, _, err := planner.NextClosestApproach(stateA, stateB, earth, mu, wideHorizonSec)
	if err != nil {
		t.Fatalf("setup: wide-horizon NextClosestApproach: %v", err)
	}
	if wideTCA <= 2*rendezvousCommitHorizonSec {
		t.Fatalf("setup: true tCA = %.0f s, want > %.0f s (double the 4h horizon, comfortable margin) — test wouldn't stress the bound", wideTCA, 2*rendezvousCommitHorizonSec)
	}

	active := w.ActiveCraft()
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		t.Fatal("TargetStateRelativeToActivePrimary: ok=false")
	}
	tau, _, cok := w.rendezvousCommitCurrentCourse(active, rT, vT, mu)
	if !cok {
		t.Fatal("rendezvousCommitCurrentCourse: ok=false — expected the edge-snap result, not an outright refusal")
	}
	if got := tau.Sub(w.Clock.SimTime).Seconds(); got > rendezvousCommitHorizonSec+1 {
		t.Errorf("committed τ is %.0f s out, want <= %.0f s (the 4h horizon) — the true encounter at %.0f s must not have been found",
			got, rendezvousCommitHorizonSec, wideTCA)
	}
}
