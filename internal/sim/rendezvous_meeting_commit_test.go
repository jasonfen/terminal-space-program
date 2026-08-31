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
// node's arrival [well] out [past the 4h horizon]." Plants a real
// Meeting Burn via PlanMeetingBurn, picking the LARGEST-lap Ok row on
// the small-lag fixture: more laps means a smaller Δv spread over more
// holder periods (ADR 0045 §2's own wait-vs-Δv ladder), so on a ~90 min
// LEO period even the smallest such fixture's highest lap count
// (meetingCandidateLaps' own 20) naturally waits tens of hours — no
// need to force the scenario by hand.
//
// Review finding 2 (this test previously overwrote MeetingArrivalSec by
// hand to exactly 8h, so nothing pinned the field's NATURAL value — a
// vacuous test the reviewer flagged specifically): this version reads
// MeetingArrivalSec straight off the planted node and asserts
// RendezvousCommitWithPlan commits to TriggerTime + that natural value,
// with a non-vacuous precondition guard that it is actually beyond the
// 4h horizon (so the test still exercises the "beyond horizon" property
// it's named for, without hand-editing the field under test).
//
// RendezvousCommitWithPlan must commit to that arrival directly: if it
// instead ran rendezvousCommitFromPlantedNode's horizon-bounded search
// (the trim-nudge sibling's behaviour), an encounter well past the 4h
// window could never be found, and the commit would refuse.
func TestRendezvousCommitWithPlan_MeetingNode_ArrivalBeyondHorizon(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()

	ladder, err := w.RecommendMeetingLadder(planner.MeetingTheirOrbit)
	if err != nil {
		t.Fatalf("RecommendMeetingLadder err: %v", err)
	}
	// Pick the LARGEST-lap Ok row (rows are appended in meetingCandidateLaps'
	// own ascending order, so the last Ok row is the largest), not the
	// first — the whole point is a wait that clears the 4h horizon.
	var pick int
	found := false
	for _, row := range ladder.Rows {
		if row.Ok {
			pick, found = row.Laps, true
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
	node := c.Nodes[0]

	// Non-vacuous precondition: the NATURAL MeetingArrivalSec this specific
	// lap row solved to must already exceed rendezvousCommitHorizonSec, or
	// this test isn't exercising the "beyond horizon" property it claims to.
	if node.MeetingArrivalSec <= rendezvousCommitHorizonSec {
		t.Fatalf("setup: node.MeetingArrivalSec = %.1f s, want > %.1f s (the 4h horizon) — laps=%d didn't naturally wait long enough to stress the bound",
			node.MeetingArrivalSec, rendezvousCommitHorizonSec, pick)
	}
	wantTau := node.TriggerTime.Add(time.Duration(node.MeetingArrivalSec * float64(time.Second)))

	plan, ok := w.RendezvousCommitWithPlan()
	if !ok {
		t.Fatal("RendezvousCommitWithPlan: ok=false, want true (structural gates all pass)")
	}
	if plan.Tau.IsZero() {
		t.Fatal("plan.Tau is zero — the planted Meeting Burn node was not honored")
	}
	if diff := plan.Tau.Sub(wantTau); diff < -time.Second || diff > time.Second {
		t.Errorf("plan.Tau = %v, want %v (node.TriggerTime + its own MeetingArrivalSec) — got a difference of %v; "+
			"a horizon-bounded search would have refused entirely rather than land near this", plan.Tau, wantTau, diff)
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

// rendezvousPhaseLagWorld mirrors rendezvousSmallLagWorld (rendezvous_test.go)
// but takes the lag angle as a parameter — the small-lag fixture's fixed
// -0.5° is too close to co-orbital for Finding 2's bug to show measurably
// (a tangential burn frozen at "now" is still nearly tangential a few tens
// of seconds later at a nearly-identical point on a nearly-matched orbit).
func rendezvousPhaseLagWorld(t *testing.T, angle float64) *World {
	t.Helper()
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	target.State.R = rotateAboutAxis(active.State.R, axis, angle)
	target.State.V = rotateAboutAxis(active.State.V, axis, angle)
	target.Primary = active.Primary
	return w
}

// TestPlanMeetingBurn_CommittedArrivalMatchesTrueClosestApproach is
// review Finding 2's own regression test, reproducing the reviewer's
// exact measured scenario: rendezvousTwoCraftWorld with a 30° phase lag.
//
// Before the fix, PlanMeetingBurn solved the burn tangentially at the
// craft's position NOW but planted it as a BurnVector node — a FROZEN
// inertial direction (spacecraft.NodeBurnDirection) — that fires
// leadBuffer later at a different point on the orbit, where the frozen
// direction is no longer tangential; MeetingArrivalSec was then
// re-anchored by subtracting leadBuffer from the "solved at now" row's
// TArrival, which doesn't land on the resulting closest approach either.
// Measured on this exact fixture: the row/commit reported
// AchievableCA/CommittedCA = 0.0 m, but propagating the planted node
// exactly as rendezvousCommitFromPlantedMeetingNode does gave 974.9 m at
// the committed τ, with the TRUE minimum separation of 46.3 m occurring
// ten seconds later — a burn advertised as a meeting that actually
// misses by very roughly a kilometer at the wrong instant.
//
// After the fix (BurnPrograde/Retrograde re-derived at fire time, solved
// from the state at TriggerTime — see PlanMeetingBurn's own doc
// comment), the committed plan's CommittedCA must be small AND must sit
// at the actual local-minimum separation — verified here by an
// independent fine-grained scan around the committed arrival using the
// SAME post-burn state rendezvousCommitFromPlantedMeetingNode derives,
// so this test cannot pass merely because the solver's own prediction
// agrees with itself (the self-consistency trap the planner package's
// own TestMeetingLadder_IterateSelfConsistent doc comment warns about —
// this scan uses the node's ACTUAL fire-time direction, not the
// solver's stored one).
func TestPlanMeetingBurn_CommittedArrivalMatchesTrueClosestApproach(t *testing.T) {
	w := rendezvousPhaseLagWorld(t, -30*math.Pi/180)
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
	node := c.Nodes[0]

	plan, ok := w.RendezvousCommitWithPlan()
	if !ok || plan.Tau.IsZero() {
		t.Fatalf("RendezvousCommitWithPlan: ok=%v Tau=%v, want a committed plan", ok, plan.Tau)
	}

	// Independent scan: rebuild the SAME post-burn state
	// rendezvousCommitFromPlantedMeetingNode itself derives (target
	// Kepler-propagated to TriggerTime, then the node's OWN direction
	// mode applied via postBurnStateWithTarget — BurnPrograde/Retrograde
	// re-resolved at that state, not a stored vector), then sweep a
	// ±60 s window around the node's own MeetingArrivalSec looking for
	// the TRUE local-minimum separation.
	rT, vT, tok := w.TargetStateRelativeToActivePrimary()
	if !tok {
		t.Fatal("TargetStateRelativeToActivePrimary: ok=false")
	}
	mu := c.Primary.GravitationalParameter()
	dt := node.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	targetAtTrigger, tok2 := physics.KeplerStep(physics.StateVector{R: rT, V: vT}, mu, dt)
	if !tok2 {
		t.Fatal("KeplerStep (target to TriggerTime) failed")
	}
	postState, _, pok := w.postBurnStateWithTarget(node, targetAtTrigger.R, targetAtTrigger.V)
	if !pok {
		t.Fatal("postBurnStateWithTarget failed")
	}
	trueMin := math.Inf(1)
	trueMinOffset := 0.0
	for offset := -60.0; offset <= 60.0; offset += 1.0 {
		tArr := node.MeetingArrivalSec + offset
		if tArr <= 0 {
			continue
		}
		moverArr, mok := physics.KeplerStep(postState, mu, tArr)
		holderArr, hok := physics.KeplerStep(targetAtTrigger, mu, tArr)
		if !mok || !hok {
			continue
		}
		if d := moverArr.R.Sub(holderArr.R).Norm(); d < trueMin {
			trueMin, trueMinOffset = d, offset
		}
	}
	if math.IsInf(trueMin, 1) {
		t.Fatal("scan: no offset produced a valid Kepler propagation")
	}

	const smallCAM = 5_000.0 // 5 km — same "small" family as the planner's own calibration bound
	if plan.CommittedCA > smallCAM {
		t.Errorf("plan.CommittedCA = %.1f m, want small (<%.0f m) — a mismatched frozen burn direction would report the honest large miss instead", plan.CommittedCA, smallCAM)
	}
	if trueMin > smallCAM {
		t.Errorf("independently-scanned true minimum = %.1f m at offset %.1fs from the committed arrival, want small (<%.0f m)", trueMin, trueMinOffset, smallCAM)
	}
	if math.Abs(trueMinOffset) > 5 {
		t.Errorf("true minimum occurs %.1fs from the committed arrival, want within a few seconds — "+
			"the committed τ should already sit at the actual closest approach, not miss it by a fixed offset "+
			"(finding 2's bug landed the true minimum 10s after the reported one)", trueMinOffset)
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
