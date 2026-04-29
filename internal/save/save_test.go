package save_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func TestRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Clock.SimTime = w.Clock.SimTime.Add(73 * time.Hour)
	w.Clock.WarpIdx = 3
	w.Clock.Paused = true
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: 4}
	w.ActiveCraft().Fuel = 412.5
	w.ActiveCraft().State.V = w.ActiveCraft().State.V.Add(orbital.Vec3{Y: 25.5})
	w.ActiveCraft().Nodes = append(w.ActiveCraft().Nodes, sim.ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(5 * time.Minute),
		Mode:        spacecraft.BurnPrograde,
		DV:          200,
		Duration:    20 * time.Second,
		PrimaryID:   "earth",
	})
	w.ActiveCraft().ActiveBurn = &sim.ActiveBurn{
		Mode:        spacecraft.BurnRetrograde,
		DVRemaining: 50,
		EndTime:     w.Clock.SimTime.Add(8 * time.Second),
		PrimaryID:   "earth",
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !got.Clock.SimTime.Equal(w.Clock.SimTime) {
		t.Errorf("SimTime: got %v want %v", got.Clock.SimTime, w.Clock.SimTime)
	}
	if got.Clock.WarpIdx != w.Clock.WarpIdx {
		t.Errorf("WarpIdx: got %d want %d", got.Clock.WarpIdx, w.Clock.WarpIdx)
	}
	if got.Clock.Paused != w.Clock.Paused {
		t.Errorf("Paused: got %v want %v", got.Clock.Paused, w.Clock.Paused)
	}
	if got.Clock.BaseStep != w.Clock.BaseStep {
		t.Errorf("BaseStep: got %v want %v", got.Clock.BaseStep, w.Clock.BaseStep)
	}
	if got.SystemIdx != w.SystemIdx {
		t.Errorf("SystemIdx: got %d want %d", got.SystemIdx, w.SystemIdx)
	}
	if got.Focus != w.Focus {
		t.Errorf("Focus: got %+v want %+v", got.Focus, w.Focus)
	}
	if got.ActiveCraft().Name != w.ActiveCraft().Name {
		t.Errorf("Craft.Name: got %q want %q", got.ActiveCraft().Name, w.ActiveCraft().Name)
	}
	if got.ActiveCraft().Fuel != w.ActiveCraft().Fuel {
		t.Errorf("Craft.Fuel: got %v want %v", got.ActiveCraft().Fuel, w.ActiveCraft().Fuel)
	}
	if got.ActiveCraft().Primary.ID != w.ActiveCraft().Primary.ID {
		t.Errorf("Craft.Primary.ID: got %q want %q", got.ActiveCraft().Primary.ID, w.ActiveCraft().Primary.ID)
	}
	if !vecEq(got.ActiveCraft().State.R, w.ActiveCraft().State.R) {
		t.Errorf("Craft.State.R: got %v want %v", got.ActiveCraft().State.R, w.ActiveCraft().State.R)
	}
	if !vecEq(got.ActiveCraft().State.V, w.ActiveCraft().State.V) {
		t.Errorf("Craft.State.V: got %v want %v", got.ActiveCraft().State.V, w.ActiveCraft().State.V)
	}
	if len(got.ActiveCraft().Nodes) != 1 || got.ActiveCraft().Nodes[0].DV != 200 || got.ActiveCraft().Nodes[0].PrimaryID != "earth" {
		t.Errorf("Nodes: got %+v", got.ActiveCraft().Nodes)
	}
	if got.ActiveCraft().ActiveBurn == nil || got.ActiveCraft().ActiveBurn.DVRemaining != 50 {
		t.Errorf("ActiveBurn: got %+v", got.ActiveCraft().ActiveBurn)
	}
	if got.Calculator == nil {
		t.Error("Calculator nil after Load — should be reconstructed via orbital.ForSystem")
	}
}

func TestRoundtripEmptyState(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ActiveCraft() == nil {
		t.Fatal("Craft nil after roundtrip on default world")
	}
	if len(got.ActiveCraft().Nodes) != 0 {
		t.Errorf("Nodes: expected empty, got %d", len(got.ActiveCraft().Nodes))
	}
	if got.ActiveCraft().ActiveBurn != nil {
		t.Errorf("ActiveBurn: expected nil, got %+v", got.ActiveCraft().ActiveBurn)
	}
}

func TestHeaderShape(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if f.Version != save.SchemaVersion {
		t.Errorf("Version: got %d want %d", f.Version, save.SchemaVersion)
	}
	if f.Generator == "" {
		t.Error("Generator empty")
	}
	if f.ClockT0 == 0 {
		t.Error("ClockT0 zero")
	}
	if len(f.BodyCatalogHash) != 64 {
		t.Errorf("BodyCatalogHash: got len %d want 64 hex chars", len(f.BodyCatalogHash))
	}
}

func TestCatalogMismatchRejected(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	f.BodyCatalogHash = "deadbeef" // tamper
	tampered, _ := json.Marshal(f)
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = save.Load(path)
	if !errors.Is(err, save.ErrCatalogMismatch) {
		t.Errorf("expected ErrCatalogMismatch, got %v", err)
	}
}

func TestSchemaMismatchRejected(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	f.Version = 999 // future schema we don't understand
	tampered, _ := json.Marshal(f)
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = save.Load(path)
	if !errors.Is(err, save.ErrSchemaMismatch) {
		t.Errorf("expected ErrSchemaMismatch, got %v", err)
	}
}

func TestUnknownPrimaryRejected(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(f.Payload.Crafts) == 0 {
		t.Fatalf("expected at least one craft on the wire, got 0")
	}
	f.Payload.Crafts[0].PrimaryID = "no-such-body"
	tampered, _ := json.Marshal(f)
	if err := os.WriteFile(path, tampered, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = save.Load(path)
	if !errors.Is(err, save.ErrCraftPrimary) {
		t.Errorf("expected ErrCraftPrimary, got %v", err)
	}
}

func TestDefaultPathHonorsXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg-state")
	got, err := save.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := "/tmp/xdg-state/terminal-space-program/save.json"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestDefaultPathFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/tmp/fakehome")
	got, err := save.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := "/tmp/fakehome/.local/state/terminal-space-program/save.json"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSaveAtomicRename(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp file should be removed by rename, got err=%v", err)
	}
}

func TestStatePreservedAfterRoundtrip(t *testing.T) {
	// Ensures the rehydrated craft state physics matches.
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	wantR := w.ActiveCraft().State.R
	wantV := w.ActiveCraft().State.V
	wantPrimaryID := w.ActiveCraft().Primary.ID

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !vecEq(got.ActiveCraft().State.R, wantR) {
		t.Errorf("R: got %+v want %+v", got.ActiveCraft().State.R, wantR)
	}
	if !vecEq(got.ActiveCraft().State.V, wantV) {
		t.Errorf("V: got %+v want %+v", got.ActiveCraft().State.V, wantV)
	}
	if got.ActiveCraft().Primary.ID != wantPrimaryID {
		t.Errorf("Primary.ID: got %q want %q", got.ActiveCraft().Primary.ID, wantPrimaryID)
	}
	if got.ActiveCraft().Primary.GravitationalParameter() == 0 {
		t.Error("rehydrated primary has zero μ — body lookup didn't preserve mass")
	}
	// Confirm a state vector still propagates: pre/post Verlet step
	// from rehydrated state should not blow up.
	mu := got.ActiveCraft().Primary.GravitationalParameter()
	stepped := physics.StepVerlet(got.ActiveCraft().State, mu, 1.0)
	if stepped.R.Norm() == 0 {
		t.Error("post-load Verlet step produced zero state — primary μ likely wrong")
	}
}

// TestV1SaveLoadsAsV2: a v1 save written before v0.6.0 (no Event
// field on the wire) must load cleanly under SchemaVersion = 2 with
// Event defaulting to TriggerAbsolute.
func TestV1SaveLoadsAsV2(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.PlanNode(sim.ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(60 * time.Second),
		Mode:        spacecraft.BurnPrograde,
		DV:          100,
	})
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// Forge a v1-shaped envelope: drop the version, drop any event
	// field on nodes (omitempty already does that for zero values, so
	// the wire form is identical to a real v1 save when Event=0).
	f.Version = 1
	rewritten, _ := json.Marshal(f)
	if err := os.WriteFile(path, rewritten, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load v1 save: %v", err)
	}
	if len(got.ActiveCraft().Nodes) != 1 {
		t.Fatalf("expected 1 node after load, got %d", len(got.ActiveCraft().Nodes))
	}
	if got.ActiveCraft().Nodes[0].Event != sim.TriggerAbsolute {
		t.Errorf("v1 node loaded with Event=%v, want TriggerAbsolute", got.ActiveCraft().Nodes[0].Event)
	}
}

// TestEventRoundtrip: an event-relative node with a non-zero Event
// survives Save → Load with Event preserved and TriggerTime still
// zero (resolver hasn't fired yet).
func TestEventRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.PlanNode(sim.ManeuverNode{
		Mode:  spacecraft.BurnPrograde,
		DV:    50,
		Event: sim.TriggerNextApo,
	})
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.ActiveCraft().Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(got.ActiveCraft().Nodes))
	}
	if got.ActiveCraft().Nodes[0].Event != sim.TriggerNextApo {
		t.Errorf("Event lost in round-trip: got %v, want TriggerNextApo", got.ActiveCraft().Nodes[0].Event)
	}
	if !got.ActiveCraft().Nodes[0].TriggerTime.IsZero() {
		t.Errorf("expected zero TriggerTime on unresolved node, got %v", got.ActiveCraft().Nodes[0].TriggerTime)
	}
}

// TestMissionsRoundtrip: a save written with progressed mission status
// (e.g. one passed, one in-progress) round-trips with status preserved.
func TestMissionsRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if len(w.Missions) == 0 {
		t.Fatal("NewWorld did not seed default missions")
	}
	// Force the first mission into Passed so we can verify status
	// persists across save/load.
	w.Missions[0].Status = missions.Passed

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Missions) != len(w.Missions) {
		t.Fatalf("missions count: got %d, want %d", len(got.Missions), len(w.Missions))
	}
	if got.Missions[0].Status != missions.Passed {
		t.Errorf("first mission status: got %v, want Passed", got.Missions[0].Status)
	}
	if got.Missions[0].ID != w.Missions[0].ID {
		t.Errorf("first mission ID: got %q, want %q", got.Missions[0].ID, w.Missions[0].ID)
	}
}

// TestV2SaveLoadsAsV3: a pre-v0.6.5 save (no missions field on the
// wire) loads cleanly under SchemaVersion = 3 with the default
// catalog seeded by NewWorld preserved.
func TestV2SaveLoadsAsV3(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Forge a v2 envelope: drop the missions field by zeroing it on
	// the in-memory File and re-marshalling.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	f.Version = 2
	f.Payload.Missions = nil
	rewritten, _ := json.Marshal(f)
	if err := os.WriteFile(path, rewritten, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load v2 save: %v", err)
	}
	if len(got.Missions) == 0 {
		t.Fatal("v2 save loaded with no missions; expected default-catalog seed from NewWorld")
	}
}

// TestRCSFieldsRoundtrip: the v0.8.0 RCS fields (monoprop, capacity,
// thrust, isp) survive Save → Load unchanged.
func TestRCSFieldsRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ActiveCraft().Monoprop = 17.5
	w.ActiveCraft().MonopropCapacity = 50
	w.ActiveCraft().RCSThrust = 440
	w.ActiveCraft().RCSIsp = 220

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ActiveCraft().Monoprop != 17.5 {
		t.Errorf("Monoprop = %v, want 17.5", got.ActiveCraft().Monoprop)
	}
	if got.ActiveCraft().MonopropCapacity != 50 {
		t.Errorf("MonopropCapacity = %v, want 50", got.ActiveCraft().MonopropCapacity)
	}
	if got.ActiveCraft().RCSThrust != 440 {
		t.Errorf("RCSThrust = %v, want 440", got.ActiveCraft().RCSThrust)
	}
	if got.ActiveCraft().RCSIsp != 220 {
		t.Errorf("RCSIsp = %v, want 220", got.ActiveCraft().RCSIsp)
	}
}

// TestLegacySaveBackfillsRCS: a save written before v0.8.0 has zero
// RCS fields on the wire; the loader must backfill DefaultRCSLoadout
// off DryMass so older saves inherit a full RCS budget without a
// schema bump.
func TestLegacySaveBackfillsRCS(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Forge a pre-v0.8.0 envelope: zero out the RCS fields on disk to
	// simulate a v3-era save that never knew about monoprop.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// Forge a v0.7.x-era save: singular Craft field, no Crafts slice,
	// version 4, RCS fields zeroed. The loader must translate the
	// singular form into Crafts and backfill DefaultRCSLoadout.
	if len(f.Payload.Crafts) == 0 {
		t.Fatalf("expected wire payload to carry at least one craft")
	}
	c := f.Payload.Crafts[0]
	c.Monoprop = 0
	c.MonopropCapacity = 0
	c.RCSThrust = 0
	c.RCSIsp = 0
	f.Payload.Craft = &c
	f.Payload.Crafts = nil
	f.Version = 4
	rewritten, _ := json.Marshal(f)
	if err := os.WriteFile(path, rewritten, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ActiveCraft().RCSIsp == 0 || got.ActiveCraft().RCSThrust == 0 || got.ActiveCraft().MonopropCapacity == 0 {
		t.Errorf("legacy save did not backfill RCS: %+v", got.ActiveCraft())
	}
	if got.ActiveCraft().Monoprop != got.ActiveCraft().MonopropCapacity {
		t.Errorf("legacy save should ship full monoprop: %v / %v",
			got.ActiveCraft().Monoprop, got.ActiveCraft().MonopropCapacity)
	}
}

// TestMultiCraftRoundtrip: save → load preserves all craft in the
// slate, the ActiveCraftIdx, and each craft's per-craft state.
func TestMultiCraftRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft 2: %v", err)
	}
	wantCount := len(w.Crafts)
	wantActive := w.ActiveCraftIdx
	// Mutate the second craft so we can check it round-trips.
	w.Crafts[1].Monoprop = 123.45
	wantMono := w.Crafts[1].Monoprop

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Crafts) != wantCount {
		t.Errorf("Crafts count = %d, want %d", len(got.Crafts), wantCount)
	}
	if got.ActiveCraftIdx != wantActive {
		t.Errorf("ActiveCraftIdx = %d, want %d", got.ActiveCraftIdx, wantActive)
	}
	if len(got.Crafts) > 1 && got.Crafts[1].Monoprop != wantMono {
		t.Errorf("Crafts[1].Monoprop = %v, want %v", got.Crafts[1].Monoprop, wantMono)
	}
}

func vecEq(a, b orbital.Vec3) bool {
	return a.X == b.X && a.Y == b.Y && a.Z == b.Z
}
