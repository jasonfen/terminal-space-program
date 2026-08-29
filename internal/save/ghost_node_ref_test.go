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

// TestGhostNodeRefSurvivesSave — #294 review finding 5. A K-nudge node
// planted against a remote player's craft (a ghost) carries a session-
// local ghost ref (TargetGhostOwner + a REMOTE TargetCraftID). v0.28 S4
// used to drop the whole ref on save (owner cleared, craft id zeroed),
// mirroring the same normalisation the original #294 fix already
// reversed for Craft.Target — on the same now-overturned theory that the
// owner fingerprint couldn't resolve again and the remote craft id would
// collide with a LOCAL one. Neither holds: ghost refs are only ever
// resolved by the (owner, craftID) PAIR (World.ghostByRef), never by
// craftID alone, so a same-numbered local craft can't collide with a
// remote one. Dropping the ref left a gap: Craft.Target re-latches onto
// the peer after a reconnect, but a node planted at that same target used
// to fire with a stale/zeroed ref — see executeDueNodesFor's refuse-to-
// fire guard for what happens now instead. SchemaVersion stays 9 (no
// schema field bumped — see save_migrate_v9_to_v10.go for the unrelated
// v10 bump that DID land, over Target.Kind's widened vocabulary).
func TestGhostNodeRefSurvivesSave(t *testing.T) {
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

	// Ghost ref preserved.
	if n.TargetGhostOwner != "SHA256:gern" {
		t.Errorf("TargetGhostOwner = %q, want %q (survives save)", n.TargetGhostOwner, "SHA256:gern")
	}
	if n.TargetCraftID != 987654 {
		t.Errorf("remote TargetCraftID = %d, want 987654 (survives save)", n.TargetCraftID)
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

// TestGhostActiveBurnRefSurvivesSave — #294 review finding 5, ActiveBurn
// half. An in-flight finite burn targeted at a ghost used to lose its ref
// the same way a planted node did; it now round-trips identically.
func TestGhostActiveBurnRefSurvivesSave(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.ActiveBurn = &sim.ActiveBurn{
		Mode:             spacecraft.BurnTarget,
		DVRemaining:      15,
		EndTime:          w.Clock.SimTime.Add(30 * time.Second),
		PrimaryID:        c.Primary.ID,
		Throttle:         1,
		TargetCraftID:    987654,
		TargetGhostOwner: "SHA256:gern",
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ab := got.ActiveCraft().ActiveBurn
	if ab == nil {
		t.Fatalf("ActiveBurn lost on save/load")
	}
	if ab.TargetGhostOwner != "SHA256:gern" {
		t.Errorf("ActiveBurn.TargetGhostOwner = %q, want %q (survives save)", ab.TargetGhostOwner, "SHA256:gern")
	}
	if ab.TargetCraftID != 987654 {
		t.Errorf("ActiveBurn.TargetCraftID = %d, want 987654 (survives save)", ab.TargetCraftID)
	}
	if ab.Mode != spacecraft.BurnTarget {
		t.Errorf("ActiveBurn.Mode = %v, want BurnTarget", ab.Mode)
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
