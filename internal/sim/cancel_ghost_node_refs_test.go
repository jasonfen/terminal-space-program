package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestCancelGhostNodeRefsDropsMatchingNode — #294 review finding 2,
// never-resolvable case (b): reconcileTargetLock's watchdog (internal/
// serve/reporting.go) gives up on Craft.Target pointing at a ghost ref,
// and any planted node bound to that SAME ref must be dropped too — not
// left wedging the queue behind an endless refusal flash the watchdog
// can no longer clear (it only ever cleared Craft.Target itself).
func TestCancelGhostNodeRefsDropsMatchingNode(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime
	c.Nodes = []spacecraft.ManeuverNode{
		{
			Mode: spacecraft.BurnTarget, DV: 50, TriggerTime: now.Add(time.Hour),
			TargetGhostOwner: "SHA256:gern", TargetCraftID: 987654,
		},
		{
			// Same owner, a DIFFERENT craft id — must survive untouched.
			Mode: spacecraft.BurnTarget, DV: 60, TriggerTime: now.Add(2 * time.Hour),
			TargetGhostOwner: "SHA256:gern", TargetCraftID: 555,
		},
	}

	w.CancelGhostNodeRefs("SHA256:gern", 987654)

	if len(c.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1 (only the matching-ref node dropped): %+v", len(c.Nodes), c.Nodes)
	}
	if c.Nodes[0].TargetCraftID != 555 {
		t.Errorf("wrong node survived: %+v", c.Nodes[0])
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancellation notice not stamped: %+v", w.LastNodeTargetRefusal)
	}
}

// TestCancelGhostNodeRefsAbortsActiveBurn — #294 review round 3 finding
// D: a matching ActiveBurn used to have only its ref stripped, keeping
// the burn "alive" so its committed Δv wasn't discarded — but with the
// ref gone (craftID==0), nodeTargetRelState refuses unconditionally, so
// activeBurnTargetReady never returns true again: the burn holds
// forever, its EndTime pushed out every tick, burnExhausted never
// fires, and the zombie burn wedges canKeplerStep's per-craft gate
// (warp stays clamped ≤10× for the rest of the session). The give-up
// countdown already ran its full grace window by the time this is
// called — there is no later resolve to wait for — so the burn is
// aborted outright instead: torn down cleanly, remaining Δv forfeit,
// same as any other "this can never fire" case.
func TestCancelGhostNodeRefsAbortsActiveBurn(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode: spacecraft.BurnTarget, DVRemaining: 100,
		EndTime: w.Clock.SimTime.Add(time.Minute), PrimaryID: c.Primary.ID,
		Throttle: 1, TargetGhostOwner: "SHA256:gern", TargetCraftID: 987654,
	}

	w.CancelGhostNodeRefs("SHA256:gern", 987654)

	if c.ActiveBurn != nil {
		t.Fatalf("ActiveBurn kept alive with a stripped ref — want it aborted outright: %+v", c.ActiveBurn)
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancellation notice not stamped for the active-burn abort: %+v", w.LastNodeTargetRefusal)
	}
}

// TestCancelGhostNodeRefsIgnoresOtherOwners — a give-up for one ref must
// never touch a node/burn bound to a DIFFERENT ref.
func TestCancelGhostNodeRefsIgnoresOtherOwners(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime
	c.Nodes = []spacecraft.ManeuverNode{{
		Mode: spacecraft.BurnTarget, DV: 50, TriggerTime: now.Add(time.Hour),
		TargetGhostOwner: "SHA256:someone-else", TargetCraftID: 987654,
	}}

	w.CancelGhostNodeRefs("SHA256:gern", 987654)

	if len(c.Nodes) != 1 {
		t.Errorf("unrelated node touched: %+v", c.Nodes)
	}
	if w.LastNodeTargetRefusal != nil {
		t.Errorf("notice stamped despite no matching ref: %+v", w.LastNodeTargetRefusal)
	}
}

// TestCancelAllGhostRefsDropsEveryGhostBoundNode — #294 second-round
// review finding 3(ii): called by reportingModel.stopHosting the moment
// a hosting session ends — every ghost ref becomes unresolvable at
// once (no roster left, no watchdog left running), not just whichever
// one ref reconcileTargetLock happened to be tracking. Every ghost-
// bound node, for ANY owner, must be dropped — a local-ref node must
// survive untouched.
func TestCancelAllGhostRefsDropsEveryGhostBoundNode(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime
	c.Nodes = []spacecraft.ManeuverNode{
		{Mode: spacecraft.BurnTarget, DV: 50, TriggerTime: now.Add(time.Hour),
			TargetGhostOwner: "SHA256:gern", TargetCraftID: 987654},
		{Mode: spacecraft.BurnTarget, DV: 60, TriggerTime: now.Add(2 * time.Hour),
			TargetGhostOwner: "SHA256:someone-else", TargetCraftID: 111},
		{Mode: spacecraft.BurnTargetPrograde, DV: 10, TriggerTime: now.Add(3 * time.Hour),
			TargetCraftID: 424242}, // local ref (no TargetGhostOwner) — must survive
	}

	w.CancelAllGhostRefs()

	if len(c.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1 (only the local-ref node survives): %+v", len(c.Nodes), c.Nodes)
	}
	if c.Nodes[0].TargetGhostOwner != "" {
		t.Errorf("a ghost-bound node survived: %+v", c.Nodes[0])
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancellation notice not stamped: %+v", w.LastNodeTargetRefusal)
	}
}

// TestCancelAllGhostRefsAbortsEveryGhostBoundActiveBurn — the ActiveBurn
// mirror of the node case: any craft's in-flight burn bound to ANY
// ghost is aborted outright, same as a single-ref give-up already does
// for its own matching ref.
func TestCancelAllGhostRefsAbortsEveryGhostBoundActiveBurn(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode: spacecraft.BurnTarget, DVRemaining: 100,
		EndTime: w.Clock.SimTime.Add(time.Minute), PrimaryID: c.Primary.ID,
		Throttle: 1, TargetGhostOwner: "SHA256:gern", TargetCraftID: 987654,
	}

	w.CancelAllGhostRefs()

	if c.ActiveBurn != nil {
		t.Fatalf("ghost-bound ActiveBurn kept alive — want it aborted outright: %+v", c.ActiveBurn)
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancellation notice not stamped for the active-burn abort: %+v", w.LastNodeTargetRefusal)
	}
}

// TestCancelAllGhostRefsNoOpWithNothingBound — a no-op call (nothing
// ghost-bound anywhere) must not stamp a spurious cancellation notice.
func TestCancelAllGhostRefsNoOpWithNothingBound(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Nodes = []spacecraft.ManeuverNode{{Mode: spacecraft.BurnPrograde, DV: 10, TriggerTime: w.Clock.SimTime.Add(time.Hour)}}

	w.CancelAllGhostRefs()

	if len(c.Nodes) != 1 {
		t.Errorf("an unrelated local node was touched: %+v", c.Nodes)
	}
	if w.LastNodeTargetRefusal != nil {
		t.Errorf("notice stamped despite nothing ghost-bound: %+v", w.LastNodeTargetRefusal)
	}
}
