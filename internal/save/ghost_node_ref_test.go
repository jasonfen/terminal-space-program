package save_test

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestGhostNodeRefDropsOnSave — v0.28 S4. A K-nudge node planted against
// a remote player's craft (a ghost) carries a session-local ghost ref
// (TargetGhostOwner + a REMOTE TargetCraftID). Ghost refs never persist:
// the owner fingerprint isn't saved, and the remote craft id would
// collide with a LOCAL craft id on load. So save drops the whole ref —
// owner cleared AND craft id zeroed — while keeping the burn geometry
// (mode / Δv / primary / direction). Mirrors the v0.27 ghost-target
// normalisation. SchemaVersion stays 8 (no schema field added).
func TestGhostNodeRefDropsOnSave(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes, sim.ManeuverNode{
		TriggerTime:      w.Clock.SimTime.Add(time.Minute),
		Mode:             spacecraft.BurnPrograde,
		DV:               42,
		PrimaryID:        c.Primary.ID,
		TargetCraftID:    987654, // a REMOTE craft id
		TargetGhostOwner: "SHA256:gern",
	})

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	nodes := got.ActiveCraft().Nodes
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]

	// Ghost ref dropped.
	if n.TargetGhostOwner != "" {
		t.Errorf("TargetGhostOwner survived save: %q (want dropped)", n.TargetGhostOwner)
	}
	if n.TargetCraftID != 0 {
		t.Errorf("remote TargetCraftID survived save: %d (want dropped — would collide with a local id)", n.TargetCraftID)
	}

	// Burn geometry kept.
	if n.Mode != spacecraft.BurnPrograde {
		t.Errorf("Mode = %v, want BurnPrograde", n.Mode)
	}
	if math.Abs(n.DV-42) > 1e-9 {
		t.Errorf("DV = %v, want 42", n.DV)
	}
	if n.PrimaryID != c.Primary.ID {
		t.Errorf("PrimaryID = %q, want %q", n.PrimaryID, c.Primary.ID)
	}
}

// TestLocalNodeTargetRefSurvivesSave — regression guard: a node bound to
// a LOCAL craft (no ghost owner) keeps its TargetCraftID across a save
// round-trip. The ghost-drop must not touch local refs.
func TestLocalNodeTargetRefSurvivesSave(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes, sim.ManeuverNode{
		TriggerTime:   w.Clock.SimTime.Add(time.Minute),
		Mode:          spacecraft.BurnTargetPrograde,
		DV:            10,
		PrimaryID:     c.Primary.ID,
		TargetCraftID: 12345, // a local craft id, no ghost owner
	})

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	n := got.ActiveCraft().Nodes[0]
	if n.TargetCraftID != 12345 {
		t.Errorf("local TargetCraftID lost: got %d want 12345", n.TargetCraftID)
	}
	if n.TargetGhostOwner != "" {
		t.Errorf("local node grew a ghost owner: %q", n.TargetGhostOwner)
	}
}
