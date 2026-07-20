package save

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The CommNet ground-station catalog is transient world state: it is loaded
// from the embedded catalog (+ user overlay) rather than persisted, because it
// is not player state. NewWorld loads it; a world rehydrated from a save must
// too. When it didn't, a loaded save had zero station sinks, so every unmanned
// craft lost its connection — reading "NO SIGNAL" at any altitude and, worse,
// becoming uncommandable via CanCommandCraft.
func TestLoadRestoresGroundStations(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if len(w.GroundStations) == 0 {
		t.Fatal("fresh world has no ground stations; catalog broken")
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.GroundStations) != len(w.GroundStations) {
		t.Fatalf("ground stations after load = %d, want %d",
			len(got.GroundStations), len(w.GroundStations))
	}
}

// A probe that is connected before a save must still be connected after a
// load — the player-visible symptom of the missing catalog.
func TestLoadPreservesCommConnection(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:    "Relay-Comsat",
		ParentBodyID: "earth",
		AltitudeM:    500 * 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.RecomputeCommGraph()
	if !w.CommGraph.HasConnection(c.ID) {
		t.Fatal("probe not connected before save; test premise broken")
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	var loaded uint64
	for _, cc := range got.Crafts {
		if cc != nil && cc.Name == c.Name {
			loaded = cc.ID
		}
	}
	if loaded == 0 {
		t.Fatalf("craft %q missing after load", c.Name)
	}
	got.RecomputeCommGraph()
	if !got.CommGraph.HasConnection(loaded) {
		t.Error("probe reads NO SIGNAL after load; ground stations were not restored")
	}
	if !got.CanCommandCraft(got.Crafts[got.ActiveCraftIdx]) {
		t.Error("craft is uncommandable after load")
	}
}
