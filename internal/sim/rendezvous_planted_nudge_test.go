package sim

import "testing"

// TestRendezvousCommitHonorsPlantedNudge (PR #392 review, finding 1):
// the #276 fix's own prescribed remedy — refuse, then "plant a nudge
// [K] first" — was a dead end. Planting a node doesn't touch
// active.State (only firing does), so a plant-then-Engage sequence hit
// the identical current-course refusal every time. RendezvousCommit
// must honor an ACTUALLY-PLANTED rendezvous-nudge node: once K has
// queued a burn, Engage commits to the encounter that burn leads to,
// not to the (still zero-drift) current course.
func TestRendezvousCommitHonorsPlantedNudge(t *testing.T) {
	w := rendezvousSmallLagWorld(t)

	// Precondition: this geometry has zero relative drift on the
	// current courses — the phantom-commit regression test
	// (rendezvous_matched_orbit_test.go) already pins that
	// RendezvousCommit refuses with nothing planted.
	if _, _, ok := w.RendezvousCommit(); ok {
		t.Fatal("precondition: RendezvousCommit should refuse with no planted node on matched orbits")
	}

	adv, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("small-lag geometry yielded no useful nudge (%v); not exercising the planted-node commit path", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if adv == nil || !adv.Ok {
		t.Fatalf("expected a successful plant, got %+v", adv)
	}
	c := w.ActiveCraft()
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 planted node, got %d", len(c.Nodes))
	}
	node := c.Nodes[0]

	tau, ca, ok := w.RendezvousCommit()
	if !ok {
		t.Fatal("RendezvousCommit refused despite a freshly planted rendezvous nudge — " +
			"the #276 refusal's own \"plant a nudge [K] first\" remedy is a dead end without this")
	}
	if !tau.After(w.Clock.SimTime) {
		t.Errorf("committed τ %v is not in the future (SimTime %v)", tau, w.Clock.SimTime)
	}
	if tau.Before(node.TriggerTime) {
		t.Errorf("committed τ %v precedes the node's own TriggerTime %v — "+
			"the encounter can only exist once the burn has actually fired", tau, node.TriggerTime)
	}
	if ca <= 0 {
		t.Errorf("committed CA = %.3f, want > 0", ca)
	}
	// Sanity bound, not exact equality: the planted node's TriggerTime
	// carries a small lead buffer past "now" (v0.10.0 slew lead-comp),
	// so the honest post-burn CA (searched from TriggerTime) need not
	// exactly match the advisory's own preview (which previews the burn
	// as if fired instantly at t=0) — but on this fixture's small,
	// slowly-evolving lag geometry they should stay in the same
	// ballpark.
	if adv.AchievableCA > 0 && (ca > adv.AchievableCA*5 || ca < adv.AchievableCA/5) {
		t.Errorf("committed CA %.0f m is far from the advisory's own preview %.0f m — "+
			"expected the same ballpark for this slowly-evolving geometry", ca, adv.AchievableCA)
	}
}

// TestRendezvousCommitPlantedNudgeIgnoredWhenPastDue: a planted node
// whose TriggerTime has already elapsed (but hasn't fired yet this
// tick — a same-tick race) must not be treated as a future course to
// search from; RendezvousCommit falls back to the current-course
// search instead of dividing by a non-positive dt.
func TestRendezvousCommitPlantedNudgeIgnoredWhenPastDue(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	adv, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("small-lag geometry yielded no useful nudge (%v); not exercising this path", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if adv == nil || !adv.Ok {
		t.Fatalf("expected a successful plant, got %+v", adv)
	}
	c := w.ActiveCraft()
	c.Nodes[0].TriggerTime = w.Clock.SimTime // due "now" — dt <= 0

	// Must not panic or misbehave; falls through to the (refusing,
	// zero-drift) current-course search.
	if _, _, ok := w.RendezvousCommit(); ok {
		t.Error("RendezvousCommit committed via a past-due planted node instead of falling back")
	}
}
