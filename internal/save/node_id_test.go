package save_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestRoundtripNodeIDsAndCounter: a planted node's stable ID and the
// World.NextNodeID counter round-trip through save/load (v0.16 / ADR
// 0016). Additive, no schema bump — the field is omitempty and
// EnsureNodeIDs re-primes the counter on load.
func TestRoundtripNodeIDsAndCounter(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	base := w.Clock.SimTime
	w.PlanNode(sim.ManeuverNode{TriggerTime: base.Add(time.Minute), DV: 10, Mode: spacecraft.BurnPrograde})
	w.PlanNode(sim.ManeuverNode{TriggerTime: base.Add(2 * time.Minute), DV: 20, Mode: spacecraft.BurnPrograde})

	wantIDs := []uint64{w.ActiveCraft().Nodes[0].ID, w.ActiveCraft().Nodes[1].ID}
	wantNext := w.NextNodeID
	if wantIDs[0] == 0 || wantIDs[1] == 0 || wantIDs[0] == wantIDs[1] {
		t.Fatalf("setup: node IDs not distinct/nonzero: %v", wantIDs)
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	nodes := got.ActiveCraft().Nodes
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].ID != wantIDs[0] || nodes[1].ID != wantIDs[1] {
		t.Errorf("node IDs lost in round-trip: got [%d %d], want %v", nodes[0].ID, nodes[1].ID, wantIDs)
	}
	if got.NextNodeID != wantNext {
		t.Errorf("NextNodeID round-trip: got %d, want %d", got.NextNodeID, wantNext)
	}

	// A post-load plant must not collide with a restored ID.
	got.PlanNode(sim.ManeuverNode{TriggerTime: base.Add(3 * time.Minute), DV: 30, Mode: spacecraft.BurnPrograde})
	newID := got.ActiveCraft().Nodes[2].ID
	for _, id := range wantIDs {
		if newID == id {
			t.Errorf("post-load plant minted a colliding node ID %d", newID)
		}
	}
}

// TestLegacySaveBackfillsNodeIDs: a save with nodes carrying ID 0 (older
// than the ID field) loads with every node back-filled to a unique,
// nonzero stable ID via EnsureNodeIDs.
func TestLegacySaveBackfillsNodeIDs(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	base := w.Clock.SimTime
	// Append nodes directly with no ID (the legacy shape) and zero the
	// counter, bypassing PlanNode's stamp.
	w.ActiveCraft().Nodes = []sim.ManeuverNode{
		{TriggerTime: base.Add(time.Minute), DV: 10, Mode: spacecraft.BurnPrograde},
		{TriggerTime: base.Add(2 * time.Minute), DV: 20, Mode: spacecraft.BurnPrograde},
	}
	w.NextNodeID = 0

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	nodes := got.ActiveCraft().Nodes
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].ID == 0 || nodes[1].ID == 0 || nodes[0].ID == nodes[1].ID {
		t.Errorf("EnsureNodeIDs failed to back-fill unique IDs: [%d %d]", nodes[0].ID, nodes[1].ID)
	}
}
