package save_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestRoundtripMeetingBurnFields (ADR 0045 S7, #400) — a planted Meeting
// Burn node's MeetingArrivalSec / MeetingPlaceLabel / MeetingLaps must
// survive a save/load round-trip so a reloaded save's node still carries
// what RendezvousCommitWithPlan needs to commit to its arrival directly.
// Additive zero-value-omitempty, same precedent as AdvisoryKey
// (TestRoundtripAdvisoryKey) and BurnDirUnit: no schema bump — an
// ordinary node with none of these set round-trips as the zero value,
// and save.SchemaVersion is asserted unchanged from the value this test
// was written against (10) so a bump elsewhere doesn't silently make
// this test's own claim stale.
func TestRoundtripMeetingBurnFields(t *testing.T) {
	if save.SchemaVersion != 10 {
		t.Fatalf("save.SchemaVersion = %d, want 10 — ADR 0045 S7 (#400) claims no schema bump was needed; if one landed since, update this pin and confirm the claim still holds", save.SchemaVersion)
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
			AdvisoryKey:       "meeting-burn",
			MeetingArrivalSec: 8 * 3600,
			MeetingPlaceLabel: "their orbit",
			MeetingLaps:       5,
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
	if nodes[0].MeetingArrivalSec != 8*3600 {
		t.Errorf("MeetingArrivalSec lost in round-trip: got %v, want %v", nodes[0].MeetingArrivalSec, 8*3600)
	}
	if nodes[0].MeetingPlaceLabel != "their orbit" {
		t.Errorf("MeetingPlaceLabel lost in round-trip: got %q, want %q", nodes[0].MeetingPlaceLabel, "their orbit")
	}
	if nodes[0].MeetingLaps != 5 {
		t.Errorf("MeetingLaps lost in round-trip: got %d, want %d", nodes[0].MeetingLaps, 5)
	}
	if nodes[1].MeetingArrivalSec != 0 || nodes[1].MeetingPlaceLabel != "" || nodes[1].MeetingLaps != 0 {
		t.Errorf("ordinary node picked up non-zero Meeting Burn fields after round-trip: %+v", nodes[1])
	}
}
