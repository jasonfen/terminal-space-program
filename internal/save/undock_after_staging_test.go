package save_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func stagedStarlink(t *testing.T) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	cfg := t.TempDir()
	dir := filepath.Join(cfg, "terminal-space-program", "designs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "sim", "testdata", "designs", "starlink.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "starlink.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{DesignID: "starlink", ParentBodyID: "moon", AltitudeM: 100e3})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	return w, c
}

func tiles(c *spacecraft.Spacecraft) bool {
	sum := 0
	for _, dc := range c.DockedComponents {
		sum += len(dc.Stages)
	}
	return sum == len(c.Stages)
}

func undockAndCheck(t *testing.T, w *sim.World) {
	t.Helper()
	if !w.Undock(w.ActiveCraftIdx) {
		t.Fatal("Undock refused")
	}
	comsats := 0
	for _, k := range w.Crafts {
		if len(k.Stages) == 1 && k.Stages[0].Name == "Relay Comsat" {
			comsats++
			if k.AntennaKind != spacecraft.AntennaRelay || k.AntennaRangeM != spacecraft.AntennaRangeRelayCislunar {
				t.Errorf("released %q: antenna %s/%.0e m, want relay/%.0e m", k.Name, k.AntennaKind, k.AntennaRangeM, spacecraft.AntennaRangeRelayCislunar)
			}
		}
	}
	if comsats != 3 {
		t.Errorf("released %d comsats, want 3", comsats)
	}
}

// Stage the F9-S1, save, load, then U: the reloaded composite still tiles and
// every comsat keeps its relay antenna.
func TestUndockAfterStagingSurvivesSaveLoad(t *testing.T) {
	w, _ := stagedStarlink(t)
	path := filepath.Join(t.TempDir(), "s.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	back, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c := back.ActiveCraft(); !tiles(c) {
		t.Errorf("reloaded composite does not tile: %d stages", len(c.Stages))
	}
	undockAndCheck(t, back)
}

// A save written by a build before the fix records the dropped stage in the
// carrier core still. Loading it repairs the breakdown, so U on the reloaded
// vessel keeps the antennas too.
func TestSaveWrittenWithStaleBreakdownLoadsRepaired(t *testing.T) {
	w, c := stagedStarlink(t)
	// Reinstate the stale record the old StageActive left behind.
	core := &c.DockedComponents[0]
	core.Stages = append([]spacecraft.Stage{{Name: "F9-S1", LoadoutID: "f9-s1"}}, core.Stages...)
	if tiles(c) {
		t.Fatal("fixture should be stale")
	}
	path := filepath.Join(t.TempDir(), "s.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	back, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c := back.ActiveCraft(); !tiles(c) {
		t.Errorf("stale save was not repaired on load: components claim more than the %d live stages", len(c.Stages))
	}
	undockAndCheck(t, back)
}
