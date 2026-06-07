package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestPlanNodeStampsUniqueNodeIDs — every planted node gets a distinct,
// nonzero stable ID, and NextNodeID stays ahead of every live ID.
// Mirrors TestStableIDsAreUniqueAcrossSpawnAndStage for nodes (ADR 0016).
func TestPlanNodeStampsUniqueNodeIDs(t *testing.T) {
	w := mustWorld(t)
	base := w.Clock.SimTime
	for i := 0; i < 4; i++ {
		w.PlanNode(ManeuverNode{
			TriggerTime: base.Add(time.Duration(60*(i+1)) * time.Second),
			DV:          10,
			Mode:        spacecraft.BurnPrograde,
		})
	}
	seen := map[uint64]bool{}
	for i, n := range w.ActiveCraft().Nodes {
		if n.ID == 0 {
			t.Errorf("node %d has zero ID", i)
		}
		if seen[n.ID] {
			t.Errorf("duplicate node ID %d at slice idx %d", n.ID, i)
		}
		seen[n.ID] = true
		if n.ID >= w.NextNodeID {
			t.Errorf("NextNodeID %d not ahead of live node ID %d", w.NextNodeID, n.ID)
		}
	}
}

// TestNodeIDSurvivesResort — a node's stable ID must follow the value
// through the sortNodes reorder that runs on every plant, so a frozen
// Auto-Warp target keeps resolving after the player plants an earlier
// burn. Slice index would go stale here; the ID can't (ADR 0016).
func TestNodeIDSurvivesResort(t *testing.T) {
	w := mustWorld(t)
	base := w.Clock.SimTime

	// Plant a node 120 s out and capture its ID.
	w.PlanNode(ManeuverNode{TriggerTime: base.Add(120 * time.Second), DV: 10, Mode: spacecraft.BurnPrograde})
	if len(w.ActiveCraft().Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(w.ActiveCraft().Nodes))
	}
	targetID := w.ActiveCraft().Nodes[0].ID
	if targetID == 0 {
		t.Fatal("first planted node has zero ID")
	}

	// Plant an earlier node — sortNodes moves the original to slice idx 1.
	w.PlanNode(ManeuverNode{TriggerTime: base.Add(30 * time.Second), DV: 20, Mode: spacecraft.BurnPrograde})
	if w.ActiveCraft().Nodes[0].ID == targetID {
		t.Fatal("setup: original node did not shift after the earlier plant")
	}

	craftID := w.ActiveCraft().ID
	n, ok := w.nodeByID(craftID, targetID)
	if !ok {
		t.Fatal("nodeByID lost the original node across the resort")
	}
	if got := n.TriggerTime.Sub(base); got != 120*time.Second {
		t.Errorf("resolved the wrong node: TriggerTime +%v, want +120s", got)
	}
}

// TestNodeByIDDegradesOnDelete — a deleted node (and a stale craft ID)
// must resolve to ok=false, the cue Auto-Warp uses to disengage.
func TestNodeByIDDegradesOnDelete(t *testing.T) {
	w := mustWorld(t)
	base := w.Clock.SimTime
	w.PlanNode(ManeuverNode{TriggerTime: base.Add(60 * time.Second), DV: 10, Mode: spacecraft.BurnPrograde})
	craftID := w.ActiveCraft().ID
	nodeID := w.ActiveCraft().Nodes[0].ID

	if _, ok := w.nodeByID(craftID, nodeID); !ok {
		t.Fatal("setup: node should resolve before deletion")
	}
	// Delete it.
	w.ActiveCraft().Nodes = nil
	if _, ok := w.nodeByID(craftID, nodeID); ok {
		t.Error("deleted node still resolves by ID")
	}
	// Stale craft ID.
	if _, ok := w.nodeByID(craftID+999, nodeID); ok {
		t.Error("node resolves under a nonexistent craft ID")
	}
	// Zero IDs never resolve.
	if _, ok := w.nodeByID(0, nodeID); ok {
		t.Error("nodeByID resolved with craftID==0")
	}
	if _, ok := w.nodeByID(craftID, 0); ok {
		t.Error("nodeByID resolved with nodeID==0")
	}
}

// TestEnsureNodeIDsBackfills — a save loaded before the ID field existed
// (or a pre-v5 migration) lands with nodes carrying ID 0. EnsureNodeIDs
// must stamp every unstamped node and prime NextNodeID past any ID
// already present, so a post-load plant can't mint a colliding value.
// Mirrors EnsureCraftIDs (ADR 0012/0016).
func TestEnsureNodeIDsBackfills(t *testing.T) {
	w := mustWorld(t)
	base := w.Clock.SimTime
	c := w.ActiveCraft()
	// Two unstamped (legacy) nodes plus one already carrying a high ID.
	c.Nodes = []ManeuverNode{
		{TriggerTime: base.Add(30 * time.Second), DV: 1, Mode: spacecraft.BurnPrograde},
		{TriggerTime: base.Add(60 * time.Second), DV: 2, Mode: spacecraft.BurnPrograde, ID: 42},
		{TriggerTime: base.Add(90 * time.Second), DV: 3, Mode: spacecraft.BurnPrograde},
	}
	w.NextNodeID = 0 // simulate a counter that hasn't been primed

	w.EnsureNodeIDs()

	if w.NextNodeID <= 42 {
		t.Errorf("NextNodeID %d not primed past the live ID 42", w.NextNodeID)
	}
	seen := map[uint64]bool{}
	for i := range c.Nodes {
		id := c.Nodes[i].ID
		if id == 0 {
			t.Errorf("node %d still unstamped after EnsureNodeIDs", i)
		}
		if seen[id] {
			t.Errorf("EnsureNodeIDs minted a duplicate ID %d", id)
		}
		seen[id] = true
	}
	if c.Nodes[1].ID != 42 {
		t.Errorf("EnsureNodeIDs clobbered an already-stamped node: ID %d, want 42", c.Nodes[1].ID)
	}
}
