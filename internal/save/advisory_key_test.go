package save_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestRoundtripAdvisoryKey — #293: a node's AdvisoryKey (which
// single-keystroke advisory planter — K/C — planted it) must survive a
// save/load round-trip, so a reloaded save still replaces its own
// advisory node on the next press instead of stacking a duplicate
// alongside it. Additive omitempty, no schema bump: an ordinary node
// with no AdvisoryKey round-trips as "".
func TestRoundtripAdvisoryKey(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	base := w.Clock.SimTime
	w.ActiveCraft().Nodes = []sim.ManeuverNode{
		{TriggerTime: base.Add(time.Minute), DV: 10, Mode: spacecraft.BurnPrograde, AdvisoryKey: "rendezvous-nudge"},
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
	if nodes[0].AdvisoryKey != "rendezvous-nudge" {
		t.Errorf("AdvisoryKey lost in round-trip: got %q, want %q", nodes[0].AdvisoryKey, "rendezvous-nudge")
	}
	if nodes[1].AdvisoryKey != "" {
		t.Errorf("ordinary node picked up a non-empty AdvisoryKey after round-trip: %q", nodes[1].AdvisoryKey)
	}
}
