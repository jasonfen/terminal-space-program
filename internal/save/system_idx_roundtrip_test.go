package save_test

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// lumenIdx returns the loaded Lumen System's slate index.
func lumenIdx(t *testing.T, w *sim.World) int {
	t.Helper()
	for i := range w.Systems {
		if w.Systems[i].Name == "Lumen" {
			return i
		}
	}
	t.Fatal("Lumen system not loaded")
	return -1
}

// TestSystemIdxRoundtrip — ADR 0015 / schema v8: a Vessel's per-System
// binding survives save → load. Spawn a Kern Stack in Lumen, persist, and
// reload; the reloaded Vessel must still be bound to Lumen with its Primary
// rehydrated from the Lumen System (not a stray same-ID body elsewhere).
func TestSystemIdxRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	want := lumenIdx(t, w)
	// Browse to Lumen, then spawn there.
	for i := 0; i < len(w.Systems) && w.SystemIdx != want; i++ {
		w.CycleSystem()
	}
	if w.SystemIdx != want {
		t.Fatalf("could not browse to Lumen (at %d)", w.SystemIdx)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: spacecraft.LoadoutKernStackID, ParentBodyID: "kern", AltitudeM: 200e3})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if c.SystemIdx != want {
		t.Fatalf("precondition: spawned craft not bound to Lumen (got %d)", c.SystemIdx)
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Find the reloaded Lumen craft (the one whose Primary is kern).
	var reloaded *spacecraft.Spacecraft
	for _, rc := range got.Crafts {
		if rc != nil && rc.Primary.ID == "kern" {
			reloaded = rc
			break
		}
	}
	if reloaded == nil {
		t.Fatal("reloaded world has no kern-orbiting craft")
	}
	if reloaded.SystemIdx != want {
		t.Errorf("reloaded SystemIdx = %d, want %d (Lumen)", reloaded.SystemIdx, want)
	}
	if got.Systems[want].FindBody(reloaded.Primary.ID) == nil {
		t.Errorf("reloaded Primary %q not found in Lumen System", reloaded.Primary.ID)
	}
}
