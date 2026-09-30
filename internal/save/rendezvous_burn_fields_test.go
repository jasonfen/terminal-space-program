package save_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestRoundtripRendezvousBurnFields (ADR 0045 S7, #400) - a planted
// Rendezvous Burn node's RendezvousArrivalSec / RendezvousOrbitLabel /
// RendezvousLaps must survive a save/load round-trip at the current schema
// so a reloaded save's node still carries what RendezvousCommitWithPlan
// needs to commit to its arrival directly. An ordinary node with none of
// these set round-trips as the zero value. The v11 -> v12 rename of the
// persisted keys is pinned separately in TestMigrateV11ToV12RendezvousBurn.
func TestRoundtripRendezvousBurnFields(t *testing.T) {
	if save.SchemaVersion != 13 {
		t.Fatalf("save.SchemaVersion = %d, want 13 - this test round-trips the v12+ key names (rendezvous_*); a later bump should re-confirm they are unchanged", save.SchemaVersion)
	}

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	base := w.Clock.SimTime
	w.ActiveCraft().Nodes = []sim.ManeuverNode{
		{
			TriggerTime:          base.Add(time.Minute),
			DV:                   10,
			Mode:                 spacecraft.BurnVector,
			AdvisoryKey:          sim.AdvisoryKeyRendezvousBurn,
			RendezvousArrivalSec: 8 * 3600,
			RendezvousOrbitLabel: "their orbit",
			RendezvousLaps:       5,
		},
		{TriggerTime: base.Add(2 * time.Minute), DV: 20, Mode: spacecraft.BurnPrograde},
	}
	w.EnsureNodeIDs()

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
	if nodes[0].RendezvousArrivalSec != 8*3600 {
		t.Errorf("RendezvousArrivalSec lost in round-trip: got %v, want %v", nodes[0].RendezvousArrivalSec, 8*3600)
	}
	if nodes[0].RendezvousOrbitLabel != "their orbit" {
		t.Errorf("RendezvousOrbitLabel lost in round-trip: got %q, want %q", nodes[0].RendezvousOrbitLabel, "their orbit")
	}
	if nodes[0].RendezvousLaps != 5 {
		t.Errorf("RendezvousLaps lost in round-trip: got %d, want %d", nodes[0].RendezvousLaps, 5)
	}
	if nodes[1].RendezvousArrivalSec != 0 || nodes[1].RendezvousOrbitLabel != "" || nodes[1].RendezvousLaps != 0 {
		t.Errorf("ordinary node picked up non-zero Rendezvous Burn fields after round-trip: %+v", nodes[1])
	}
}
