package sim

import "testing"

// rendezvousThreeCraftWorld extends rendezvousSmallLagWorld (active=0,
// peer A=1, matched-orbit small lag, target bound to A) with a third
// craft, peer B, whose state is cloned from A's CURRENT (pre-nudge)
// state — same primary, same R, same V. This is deliberate: it makes
// B, at plant time, dynamically indistinguishable from A. So a burn
// computed and planted against A will, if incorrectly replayed against
// B's identical snapshot, "succeed" (find a real closest approach) —
// not because the binding was honored, but purely because B's state
// happens to coincide with A's. That is exactly what makes this a good
// regression fixture for the AdvisoryKey-only match bug (PR #392
// review): the bug doesn't just risk refusing when it shouldn't, it can
// produce a plausible-looking phantom commit. Only checking the node's
// own TargetCraftID/TargetGhostOwner binding (not the physics outcome)
// tells the two apart.
func rendezvousThreeCraftWorld(t *testing.T) *World {
	t.Helper()
	w := rendezvousSmallLagWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 700e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 3 {
		t.Fatalf("expected 3 crafts after spawn, got %d", len(w.Crafts))
	}
	// SpawnCraft snaps the active craft to the newly spawned one
	// (spawn.go SetActiveCraftIdx); restore it to the original craft
	// (idx 0) so this fixture's "active=0, A=1, B=2" shape holds.
	w.ActiveCraftIdx = 0
	a := w.Crafts[1]
	b := w.Crafts[2]
	b.State.R = a.State.R
	b.State.V = a.State.V
	b.Primary = a.Primary
	// rendezvousSmallLagWorld already points the target at A (idx 1);
	// re-assert it here so the fixture's starting condition is explicit
	// regardless of what SpawnCraft does to the active target slot.
	w.SetTargetCraft(1)
	return w
}

// TestRendezvousCommitIgnoresNudgePlantedAgainstDifferentPeer (PR #392
// review): PlanRendezvousNudge deliberately stamps TargetCraftID/
// TargetGhostOwner at plant time (rendezvous.go ~606-611) precisely so
// a later target switch doesn't retarget the burn — but before this
// fix, RendezvousCommit's Source 1 (plantedAdvisoryNode) matched by
// AdvisoryKey alone, ignoring that binding. Engage toward peer B after
// a nudge was planted against peer A would replay A's node against
// B's target state, committing a τ/CA describing a course that will
// never be flown — the exact phantom-co-warp class #276 was about.
//
// The fixture (rendezvousThreeCraftWorld) clones B's state from A's own
// state, so the bug doesn't merely risk a wrong refusal here — it
// produces a spurious SUCCESSFUL commit toward B, since replaying A's
// node against B's (identical) snapshot finds the same real closest
// approach A's own node was computed for. Only the binding check tells
// the two peers apart. This pins: plant against A, retarget to B,
// Engage — must refuse (falls to the current-course path, which
// refuses on this fixture's matched, zero-drift orbits, mirroring the
// no-node precondition below). Retargeting back to A must still honor
// the node, exactly as the existing pinned tests assert.
func TestRendezvousCommitIgnoresNudgePlantedAgainstDifferentPeer(t *testing.T) {
	w := rendezvousThreeCraftWorld(t)

	// Precondition: matched orbits, zero relative drift, nothing planted
	// yet — Engage toward A refuses on the current course.
	if _, _, ok := w.RendezvousCommit(); ok {
		t.Fatal("precondition: RendezvousCommit should refuse toward A with no planted node")
	}

	adv, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("small-lag geometry yielded no useful nudge (%v); not exercising this path", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if adv == nil || !adv.Ok {
		t.Fatalf("expected a successful plant against A, got %+v", adv)
	}
	c := w.ActiveCraft()
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 planted node bound to A, got %d", len(c.Nodes))
	}
	nodeForA := c.Nodes[0]
	if nodeForA.TargetCraftID != w.Crafts[1].ID {
		t.Fatalf("planted node TargetCraftID = %d, want A's ID %d", nodeForA.TargetCraftID, w.Crafts[1].ID)
	}

	// Sanity: confirm the fixture actually set up the "B looks just like
	// A" trap — otherwise a refusal toward B below would prove nothing.
	if w.Crafts[2].State.R != w.Crafts[1].State.R || w.Crafts[2].State.V != w.Crafts[1].State.V {
		t.Fatal("fixture invariant broken: B's state must clone A's for this regression to be meaningful")
	}

	// Retarget to peer B. The node planted against A is still queued
	// (retargeting doesn't touch c.Nodes), but it must be ignored now —
	// even though B's state would make the stale node's search "succeed"
	// if the binding weren't checked.
	w.SetTargetCraft(2)
	if w.Target.CraftID != w.Crafts[2].ID {
		t.Fatalf("target not switched to B: Target.CraftID = %d, want %d", w.Target.CraftID, w.Crafts[2].ID)
	}
	if len(w.ActiveCraft().Nodes) != 1 {
		t.Fatalf("retargeting should not touch the planted node queue, got %d nodes", len(w.ActiveCraft().Nodes))
	}

	if tau, ca, ok := w.RendezvousCommit(); ok {
		t.Fatalf("RendezvousCommit toward B honored a nudge planted against A "+
			"(stale AdvisoryKey-only match) — committed tau=%v ca=%.3f describing a course "+
			"that will never be flown toward B", tau, ca)
	}

	// Retarget back to A: the same node, still queued, must be honored
	// again exactly as the pinned single-peer tests assert.
	w.SetTargetCraft(1)
	tau, ca, ok := w.RendezvousCommit()
	if !ok {
		t.Fatal("RendezvousCommit toward A (re-targeted back) refused despite its own matching planted node")
	}
	if !tau.After(w.Clock.SimTime) || ca <= 0 {
		t.Errorf("tau=%v ca=%.3f — expected a future tau with positive CA once retargeted back to A", tau, ca)
	}
}
