package save_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0053 decision 10: no schema bump. nav_mode simply may hold 3 (PORT);
// 0-2 mean what they always meant.
func TestNavModePortRoundTripsAndOldValuesLoadUnchanged(t *testing.T) {
	if save.SchemaVersion < 14 {
		t.Fatalf("SchemaVersion = %d; nav_mode PORT shipped at 14", save.SchemaVersion)
	}
	for _, nav := range []sim.NavMode{sim.NavOrbit, sim.NavSurface, sim.NavTarget, sim.NavPort} {
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatal(err)
		}
		w.NavMode = nav
		path := filepath.Join(t.TempDir(), "save.json")
		if err := save.Save(w, path); err != nil {
			t.Fatalf("Save(%v): %v", nav, err)
		}
		if nav == sim.NavPort {
			b, _ := os.ReadFile(path)
			if !strings.Contains(string(b), `"nav_mode": 3`) && !strings.Contains(string(b), `"nav_mode":3`) {
				t.Errorf("PORT did not persist as nav_mode 3")
			}
		}
		w2, err := save.Load(path)
		if err != nil {
			t.Fatalf("Load(%v): %v", nav, err)
		}
		if w2.NavMode != nav {
			t.Errorf("NavMode %v round-tripped as %v", nav, w2.NavMode)
		}
	}
}

// A save holding PORT with nothing to aim at (the target gone, or without
// a port) loads as saved and drops to ORBIT on the first tick, never a
// frame with no port behind it.
func TestLoadedPortWithoutPortFallsToOrbitOnFirstTick(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.NavMode = sim.NavPort
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	w2, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	w2.Tick()
	if w2.NavMode != sim.NavOrbit {
		t.Errorf("PORT with no ported target after a tick = %v, want ORBIT", w2.NavMode)
	}
}
