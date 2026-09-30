package save_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestRoundtripRendezvousBurnFields (ADR 0045 S7, #400) — a planted Rendezvous
// Burn node's RendezvousArrivalSec / RendezvousOrbitLabel / RendezvousLaps must
// survive a save/load round-trip so a reloaded save's node still carries
// what RendezvousCommitWithPlan needs to commit to its arrival directly.
// Additive zero-value-omitempty, same precedent as AdvisoryKey
// (TestRoundtripAdvisoryKey) and BurnDirUnit: no schema bump for THIS
// feature — an ordinary node with none of these set round-trips as the
// zero value, and save.SchemaVersion is asserted unchanged from the
// value this test was written against so a bump elsewhere doesn't
// silently make this test's own claim stale. Updated 10 -> 11: ADR 0049
// decision 8 (#453) bumped SchemaVersion for an unrelated reason
// (Craft.HeadingTrim), confirmed here to still hold no bump was needed
// for the rendezvous-burn fields themselves.
func TestRoundtripRendezvousBurnFields(t *testing.T) {
	if save.SchemaVersion != 11 {
		t.Fatalf("save.SchemaVersion = %d, want 11 — ADR 0045 S7 (#400) claims no schema bump was needed for rendezvous-burn fields; if one landed since for THAT reason, update this pin and confirm the claim still holds", save.SchemaVersion)
	}

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	base := w.Clock.SimTime
	w.ActiveCraft().Nodes = []sim.ManeuverNode{
		{
			TriggerTime:       base.Add(time.Minute),
			DV:                10,
			Mode:              spacecraft.BurnVector,
			AdvisoryKey:       "rendezvous-burn",
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
