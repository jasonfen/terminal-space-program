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

// TestCancelGhostNodeRefsStripsActiveBurnRef — the ActiveBurn half: the
// ref is stripped, not the burn torn down (the Δv already committed
// shouldn't be discarded) — world.go's activeBurnTargetReady (finding 1)
// then holds it permanently instead of merely pending.
func TestCancelGhostNodeRefsStripsActiveBurnRef(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode: spacecraft.BurnTarget, DVRemaining: 100,
		EndTime: w.Clock.SimTime.Add(time.Minute), PrimaryID: c.Primary.ID,
		Throttle: 1, TargetGhostOwner: "SHA256:gern", TargetCraftID: 987654,
	}

	w.CancelGhostNodeRefs("SHA256:gern", 987654)

	if c.ActiveBurn == nil {
		t.Fatal("ActiveBurn torn down — want it kept, ref stripped instead")
	}
	if c.ActiveBurn.TargetGhostOwner != "" || c.ActiveBurn.TargetCraftID != 0 {
		t.Errorf("ghost ref not stripped: %+v", c.ActiveBurn)
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancellation notice not stamped for the active-burn strip: %+v", w.LastNodeTargetRefusal)
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
