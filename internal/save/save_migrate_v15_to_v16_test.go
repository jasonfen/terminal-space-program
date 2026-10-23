package save_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// slsSaveWorld is the default world with a marked SLS as the active craft.
func slsSaveWorld(t *testing.T) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutSLSBlock1ID)
	c.Primary = w.Crafts[0].Primary
	c.State = w.Crafts[0].State
	c.SyncFields()
	c.State.M = c.TotalMass()
	w.Crafts[0] = c
	w.ActiveCraftIdx = 0
	if !c.Stages[0].LitWithAbove {
		t.Fatal("setup: SLS bottom stage is not marked")
	}
	return w, c
}

func saveLoad(t *testing.T, w *sim.World) *sim.World {
	t.Helper()
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return got
}

// ADR 0058 guard 12: the mark and a set BoosterThrottle of 0 survive a save;
// a nil throttle stays nil.
func TestBoosterMarkAndZeroThrottleRoundTrip(t *testing.T) {
	w, c := slsSaveWorld(t)
	zero := 0.0
	c.BoosterThrottle = &zero
	got := saveLoad(t, w).ActiveCraft()
	if !got.Stages[0].LitWithAbove || got.Stages[1].LitWithAbove {
		t.Errorf("marks after the wire = %v/%v, want true/false", got.Stages[0].LitWithAbove, got.Stages[1].LitWithAbove)
	}
	if got.BoosterThrottle == nil || *got.BoosterThrottle != 0 {
		t.Errorf("BoosterThrottle after the wire = %v, want a set 0", got.BoosterThrottle)
	}

	c.BoosterThrottle = nil
	if got := saveLoad(t, w).ActiveCraft(); got.BoosterThrottle != nil {
		t.Errorf("nil BoosterThrottle came back as %v", *got.BoosterThrottle)
	}
}

// A saved SLS comes back lit as a pair: both tanks drain once the burn starts.
func TestSavedSLSReloadsLitAndBothTanksDrain(t *testing.T) {
	w, c := slsSaveWorld(t)
	c.Throttle = 1
	c.EngineMode = spacecraft.EngineMain
	w.StartManualBurn()
	for i := 0; i < 3; i++ {
		w.Tick()
	}
	got := saveLoad(t, w)
	r := got.ActiveCraft()
	if n := len(spacecraft.LitStages(r.Stages)); n != 2 {
		t.Fatalf("LitStages after load has %d stages, want 2", n)
	}
	r.Throttle = 1
	r.EngineMode = spacecraft.EngineMain
	srb0, core0 := r.Stages[0].FuelMass, r.Stages[1].FuelMass
	got.StartManualBurn()
	if r.ManualBurn == nil {
		t.Fatal("burn did not start after load")
	}
	got.Tick()
	if r.Stages[0].FuelMass >= srb0 || r.Stages[1].FuelMass >= core0 {
		t.Errorf("tanks after one tick: SRB %v->%v core %v->%v, want both draining",
			srb0, r.Stages[0].FuelMass, core0, r.Stages[1].FuelMass)
	}
}

// A v15 envelope (written before the mark existed) loads unmarked with no
// booster throttle, as ADR 0058 decision 13 says: the mark is not re-derived
// from the catalog. The fixture holds an SLS.
func TestV15SaveLoadsUnmarked(t *testing.T) {
	w, err := save.Load("testdata/v15_main_23e28e0.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := craftNamed(t, w, "SLS-Block1-1")
	for i, s := range c.Stages {
		if s.LitWithAbove {
			t.Errorf("stage %d (%s) loaded marked from a v15 save", i, s.Name)
		}
	}
	if c.BoosterThrottle != nil {
		t.Error("v15 save loaded with a BoosterThrottle")
	}
}

// Every fixture in testdata still loads (a migration bug bricks a save).
func TestEveryFixtureStillLoads(t *testing.T) {
	paths, _ := filepath.Glob("testdata/*.json")
	if len(paths) < 2 {
		t.Fatalf("found %d fixtures, want at least the v14 and v15", len(paths))
	}
	for _, p := range paths {
		if _, err := save.Load(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

// The writer stamps 16, so a v15 reader (accepts [1,15]) refuses the file;
// a version past ours is refused here for the same reason.
func TestWriterStampsV16AndNewerIsRefused(t *testing.T) {
	if save.SchemaVersion != 16 {
		t.Fatalf("SchemaVersion = %d, want 16", save.SchemaVersion)
	}
	w, _ := slsSaveWorld(t)
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var f save.File
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != 16 {
		t.Errorf("written version = %d, want 16", f.Version)
	}
	f.Version = 17
	raw, _ = json.Marshal(f)
	_ = os.WriteFile(path, raw, 0o644)
	if _, err := save.Load(path); !errors.Is(err, save.ErrSchemaMismatch) {
		t.Errorf("v17 load = %v, want ErrSchemaMismatch", err)
	}
}

// A handed-over or docked SLS (relay/serve use CraftToWireForTransfer) keeps
// its mark and a set 0 booster throttle.
func TestTransferWireKeepsBoosterMark(t *testing.T) {
	_, c := slsSaveWorld(t)
	zero := 0.0
	c.BoosterThrottle = &zero
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	got, err := save.CraftFromWire(save.CraftToWireForTransfer(c), w.Systems)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stages[0].LitWithAbove || got.BoosterThrottle == nil || *got.BoosterThrottle != 0 {
		t.Errorf("transfer lost the booster state: mark=%v throttle=%v", got.Stages[0].LitWithAbove, got.BoosterThrottle)
	}
}
