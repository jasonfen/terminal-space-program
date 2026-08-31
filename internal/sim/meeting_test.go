package sim

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestPlanMeetingBurn_PlantsOneNode — the happy path for the sim-layer
// planter: MeetingTheirOrbit on a near-matched-orbit two-craft world
// (rendezvousSmallLagWorld, the same fixture family K's own nudge
// tests use) plants exactly one node on the ACTIVE craft.
//
// Review finding 2: the node plants as BurnPrograde/BurnRetrograde, not
// BurnVector — a fixed inertial BurnVector direction solved "now" is no
// longer tangential by the time the node actually fires (leadBuffer
// later); BurnPrograde/Retrograde re-derive the tangential direction at
// fire time from the craft's actual (r, v) instead (see
// PlanMeetingBurn's own doc comment). BurnDirUnit stays zero — it's
// populated only for BurnVector nodes (ManeuverNode's own doc comment).
func TestPlanMeetingBurn_PlantsOneNode(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()
	if len(c.Nodes) != 0 {
		t.Fatalf("precondition: active craft has %d nodes, expected 0", len(c.Nodes))
	}

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

	plan, err := w.PlanMeetingBurn(planner.MeetingTheirOrbit, pick)
	if err != nil {
		t.Fatalf("PlanMeetingBurn err: %v", err)
	}
	if !plan.ForActive {
		t.Fatalf("MeetingTheirOrbit must plant on the active craft: ForActive=false")
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 node planted, got %d", len(c.Nodes))
	}
	n := c.Nodes[0]
	if n.Mode != spacecraft.BurnPrograde && n.Mode != spacecraft.BurnRetrograde {
		t.Errorf("Mode = %v, want BurnPrograde or BurnRetrograde", n.Mode)
	}
	if n.AdvisoryKey != AdvisoryKeyMeetingBurn {
		t.Errorf("AdvisoryKey = %q, want %q", n.AdvisoryKey, AdvisoryKeyMeetingBurn)
	}
	if math.Abs(n.DV-plan.DV) > 1e-6 {
		t.Errorf("node DV = %.3f, want %.3f (plan.DV)", n.DV, plan.DV)
	}
	if n.BurnDirUnit.Norm() != 0 {
		t.Errorf("BurnDirUnit = %+v, want zero — populated only for BurnVector nodes, and this node is Prograde/Retrograde", n.BurnDirUnit)
	}
	if n.MeetingArrivalSec <= 0 {
		t.Errorf("MeetingArrivalSec = %.1f, want > 0", n.MeetingArrivalSec)
	}
	if !n.TriggerTime.After(w.Clock.SimTime) {
		t.Errorf("TriggerTime not in the future")
	}
}

// TestPlanMeetingBurn_SecondPressReplaces — #293's "replace, don't
// stack" rule applies to the Meeting Burn's own advisory key too: a
// second PlanMeetingBurn call removes the first unfired node rather
// than queuing a second one behind it.
func TestPlanMeetingBurn_SecondPressReplaces(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()

	ladder, err := w.RecommendMeetingLadder(planner.MeetingTheirOrbit)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var lap1, lap2 int
	nOk := 0
	for _, row := range ladder.Rows {
		if !row.Ok {
			continue
		}
		if nOk == 0 {
			lap1 = row.Laps
		} else if nOk == 1 {
			lap2 = row.Laps
		}
		nOk++
	}
	if nOk < 2 {
		t.Skipf("need at least two Ok rows to exercise replace, got %d: %+v", nOk, ladder.Rows)
	}

	if _, err := w.PlanMeetingBurn(planner.MeetingTheirOrbit, lap1); err != nil {
		t.Fatalf("first plant err: %v", err)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("after first plant: %d nodes, want 1", len(c.Nodes))
	}
	if _, err := w.PlanMeetingBurn(planner.MeetingTheirOrbit, lap2); err != nil {
		t.Fatalf("second plant err: %v", err)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("after second plant: %d nodes, want 1 (replace, not stack)", len(c.Nodes))
	}
}

// TestPlanMeetingBurn_YourOrbit_NoPlant — #398 acceptance: "meet on
// your orbit" produces a plan for the PARTNER, not the active craft —
// at the sim layer this means NO node is planted on the active
// craft's own Nodes slate (there is no mechanism in this slice to
// plant on the partner's), and the returned plan says so.
func TestPlanMeetingBurn_YourOrbit_NoPlant(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	active := w.ActiveCraft()
	target, _, ok := w.craftByID(w.Target.CraftID)
	if !ok {
		t.Fatalf("precondition: target craft not resolved")
	}

	ladder, err := w.RecommendMeetingLadder(planner.MeetingYourOrbit)
	if err != nil {
		t.Fatalf("RecommendMeetingLadder err: %v", err)
	}
	if ladder.MoverIsA {
		t.Fatalf("MeetingYourOrbit must burn the partner: MoverIsA=true")
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

	activeNodesBefore := len(active.Nodes)
	targetNodesBefore := len(target.Nodes)

	plan, err := w.PlanMeetingBurn(planner.MeetingYourOrbit, pick)
	if err != nil {
		t.Fatalf("PlanMeetingBurn err: %v", err)
	}
	if plan.ForActive {
		t.Fatalf("expected ForActive=false for MeetingYourOrbit")
	}
	if plan.DV <= 0 {
		t.Errorf("expected a real plan (DV > 0), got DV=%.3f", plan.DV)
	}
	if len(active.Nodes) != activeNodesBefore {
		t.Errorf("active craft's Nodes changed (%d → %d) — MeetingYourOrbit must not plant on the active craft", activeNodesBefore, len(active.Nodes))
	}
	if len(target.Nodes) != targetNodesBefore {
		t.Errorf("target craft's Nodes changed (%d → %d) — this slice doesn't plant on the partner either (no delivery mechanism yet)", targetNodesBefore, len(target.Nodes))
	}
}

// TestRecommendMeetingLadder_NoTarget mirrors
// TestRecommendedRendezvousBurn's own no-target gate.
func TestRecommendMeetingLadder_NoTarget(t *testing.T) {
	w := mustWorld(t)
	_, err := w.RecommendMeetingLadder(planner.MeetingTheirOrbit)
	if !errors.Is(err, ErrRendezvousNoTarget) {
		t.Fatalf("err = %v, want ErrRendezvousNoTarget", err)
	}
}

// TestPlanMeetingBurn_UnaffordableRefusal — a near-zero Δv budget
// yields a refusal naming the specific lap row's own gate, not a
// generic collapse (mirrors #278's split for K's own refusals).
func TestPlanMeetingBurn_UnaffordableRefusal(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()
	drainCraftFuel(t, c)

	ladder, err := w.RecommendMeetingLadder(planner.MeetingTheirOrbit)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var pick int
	found := false
	for _, row := range ladder.Rows {
		if row.DV > 0 {
			pick, found = row.Laps, true
			break
		}
	}
	if !found {
		t.Skipf("no priced row to test against: %+v", ladder.Rows)
	}
	_, err = w.PlanMeetingBurn(planner.MeetingTheirOrbit, pick)
	if !errors.Is(err, ErrMeetingUnaffordable) {
		t.Fatalf("err = %v, want ErrMeetingUnaffordable", err)
	}
}

// rendezvousCrossingFixtureWorld reproduces the review round-2
// regression's fixture shape: the sister craft (target) rotated -10°
// off the active craft's matched circular orbit, with its velocity
// scaled ×0.99 so the pair isn't exactly co-orbital. This geometry has
// a natural crossing within the search horizon (NextClosestApproach
// converges to a real tCA) — before the revert, that's exactly the
// case MeetingCrossing would have anchored its (broken) solve on.
func rendezvousCrossingFixtureWorld(t *testing.T) *World {
	t.Helper()
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := -10 * math.Pi / 180
	target.State.R = rotateAboutAxis(active.State.R, axis, angle)
	target.State.V = rotateAboutAxis(active.State.V, axis, angle).Scale(0.99)
	target.Primary = active.Primary
	return w
}

// TestPlanMeetingBurn_Crossing_RefusesRatherThanMisplants is the
// review round-2 regression test for both HIGH findings at once: on a
// fixture where a natural crossing genuinely exists (verified below via
// the same NextClosestApproach the planner's own existence check uses),
// PlanMeetingBurn(MeetingCrossing, ...) must refuse — never plant a
// node whose advertised numbers (MeetingArrivalSec/DV/BurnDir) disagree
// with what actually happens when that node fires.
//
// Before the revert, PR #412's crossing-anchor implementation planted
// here successfully: it Kepler-propagated both craft to tCA and solved
// the tangential burn AT that instant, but the node itself always fires
// at TriggerTime = now + a slew lead (not at tCA), and the prograde/
// retrograde direction was picked by dotting the tCA-anchored BurnDir
// against the mover's velocity at TriggerTime — two different epochs.
// On this fixture that produced a planted node whose advertised
// AchievableCA (tens of km) bore no relation to the actual miss
// (independently re-derived here via rendezvousCommitFromPlantedMeetingNode,
// the same function Engage's commit path uses) — megametres off. This
// test would have failed loudly against that implementation (PlanMeetingBurn
// returning nil error, or returning one but still having queued a
// node); it passes now because MeetingCrossing refuses before any of
// that machinery runs.
func TestPlanMeetingBurn_Crossing_RefusesRatherThanMisplants(t *testing.T) {
	w := rendezvousCrossingFixtureWorld(t)
	c := w.ActiveCraft()

	// Non-vacuous precondition: a natural crossing must actually exist
	// here (tCA > 0, within the horizon), or this fixture doesn't
	// exercise the "a crossing exists but is unsolved" path — it would
	// only prove the (uninteresting) ErrMeetingNoCrossing branch.
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		t.Fatalf("setup: TargetStateRelativeToActivePrimary ok=false")
	}
	mu := c.Primary.GravitationalParameter()
	tCA, _, _, err := planner.NextClosestApproach(
		orbital.Vec3State{R: c.State.R, V: c.State.V},
		orbital.Vec3State{R: rT, V: vT},
		c.Primary, mu, rendezvousCommitHorizonSec)
	if err != nil || tCA <= 0 {
		t.Fatalf("setup: NextClosestApproach tCA=%.1f err=%v, want a real positive crossing time", tCA, err)
	}

	for _, laps := range []int{2, 3, 5, 10, 20} {
		plan, err := w.PlanMeetingBurn(planner.MeetingCrossing, laps)
		if !errors.Is(err, ErrMeetingCrossingNotImplemented) {
			t.Errorf("laps=%d: err = %v, want ErrMeetingCrossingNotImplemented", laps, err)
		}
		if plan != nil {
			t.Errorf("laps=%d: plan = %+v, want nil", laps, plan)
		}
	}
	if len(c.Nodes) != 0 {
		t.Fatalf("expected zero nodes planted, got %d: %+v", len(c.Nodes), c.Nodes)
	}
}

// drainCraftFuel zeroes the active stage's propellant so
// RemainingDeltaV() reads ~0, forcing every ladder row unaffordable.
func drainCraftFuel(t *testing.T, c *spacecraft.Spacecraft) {
	t.Helper()
	for i := range c.Stages {
		c.Stages[i].FuelMass = 0
		c.Stages[i].MonopropMass = 0
	}
	c.SyncFields()
	if dv := c.RemainingDeltaV(); dv > 1 {
		t.Fatalf("drainCraftFuel: RemainingDeltaV() = %.1f, want ~0", dv)
	}
}
