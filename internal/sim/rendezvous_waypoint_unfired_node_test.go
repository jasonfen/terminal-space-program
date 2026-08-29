package sim

import "testing"

// TestRendezvousNextWaypointIgnoresUnfiredPlantedNode (PR #392 review,
// finding 2): rendezvousNextWaypoint's own pinned doc comment asserts a
// mid-coast waypoint "honors a nudge only once its burn has actually
// fired" — an unfired advisory nudge is not. But its ghost path used to
// reuse RendezvousCommit verbatim, which was a safe cross-purpose reuse
// only as long as RendezvousCommit itself never preferred an unfired
// node. Finding 1 gave RendezvousCommit exactly that preference, so
// pressing K mid-coast would instantly re-derive the standing
// waypoint/τ from the unfired node's hypothetical post-burn course —
// delete the node (or have it refuse to fire) and the pair would coast
// toward an encounter that will never occur, silently contradicting the
// comment's own asserted invariant.
//
// This pins that planting a rendezvous nudge mid-coast does NOT move
// the derived waypoint: the ghost path must call
// rendezvousCommitCurrentCourse (current-course-only, #276), never
// RendezvousCommit.
func TestRendezvousNextWaypointIgnoresUnfiredPlantedNode(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	sister := w.Crafts[1]

	ghost := ghostShapedLike(w, w.ActiveCraft().Primary, sister.State.R, sister.State.V,
		"SHA256:gern", "gern", 77)
	w.Ghosts = []Ghost{ghost}
	w.SetTargetGhost(ghost.Owner, ghost.CraftID)

	partner := &CoWarpPeer{
		Owner: ghost.Owner, Handle: ghost.Handle, SubspaceTime: w.Clock.SimTime,
		ArmedTowardViewer: true,
		Crafts:            []CoWarpCraft{{Primary: w.ActiveCraft().Primary.ID, R: sister.State.R, V: sister.State.V}},
	}

	// Precondition: matched orbit, zero relative drift, no node planted
	// yet — neither the ghost path nor the peer-set fallback finds a
	// future approach (mirrors the RendezvousCommit precondition in
	// TestRendezvousCommitHonorsPlantedNudge).
	if tau, ca, ok := w.rendezvousNextWaypoint(partner); ok {
		t.Fatalf("precondition: rendezvousNextWaypoint should find nothing on the matched-orbit, "+
			"no-node baseline; got tau=%v ca=%.3f", tau, ca)
	}

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

	// Contrast case, not the thing under test: confirm the plant is
	// real by checking Engage's own commit (RendezvousCommit) now finds
	// an encounter via the planted-node path (finding 1).
	if _, _, ok := w.RendezvousCommit(); !ok {
		t.Fatal("expected RendezvousCommit to honor the freshly planted nudge (finding 1) — " +
			"if this fails, the plant itself didn't take and the test below is meaningless")
	}

	// The thing under test: the mid-coast waypoint must NOT move just
	// because a node got planted — it hasn't FIRED yet.
	if tau, ca, ok := w.rendezvousNextWaypoint(partner); ok {
		t.Errorf("rendezvousNextWaypoint derived a waypoint (τ=%v ca=%.0f) from an UNFIRED "+
			"planted node — a mid-coast waypoint must honor a nudge only once fired (finding 2)",
			tau, ca)
	}
}
