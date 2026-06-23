package save_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	// v0.9.1+: Fuel writes go through Stages[0]+SyncFields. Pre-v0.9.1
	// the test wrote `w.ActiveCraft().Fuel = 412.5` directly; that
	// would now leave Stages[0].FuelMass at the loadout default and
	// the round-trip would restore that default, not 412.5.
	w.ActiveCraft().Stages[0].FuelMass = 412.5
	w.ActiveCraft().SyncFields()
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

// TestDecouplePlanRoundtripMidStaging — v0.12 / ADR 0009. A mission
// saved mid-staging must restore its remaining Decouple Plan so the
// pending grouping (the trailing LM 2-group) still fires correctly.
// Spawn an Apollo Stack (plan [1,1,1,2]), drop S-IC (plan advances to
// [1,1,2]), save + reload, and assert the reloaded craft carries
// [1,1,2]. Also confirms a craft with no plan round-trips as nil (the
// omitempty single-pop default).
func TestDecouplePlanRoundtripMidStaging(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	if _, _, err := w.StageActive(0); err != nil {
		t.Fatalf("StageActive (drop S-IC): %v", err)
	}
	wantPlan := []int{1, 1, 2}
	if got := w.Crafts[0].DecouplePlan; len(got) != len(wantPlan) {
		t.Fatalf("pre-save plan = %v, want %v", got, wantPlan)
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Active craft (the partially-staged stack) keeps [1,1,2].
	plan := got.Crafts[0].DecouplePlan
	if len(plan) != len(wantPlan) {
		t.Fatalf("reloaded plan = %v, want %v", plan, wantPlan)
	}
	for i := range wantPlan {
		if plan[i] != wantPlan[i] {
			t.Errorf("reloaded plan[%d] = %d, want %d", i, plan[i], wantPlan[i])
		}
	}
	// The jettisoned S-IC craft had no plan — must round-trip as nil
	// (omitempty), not an empty-but-non-nil slice that would read as a
	// distinct value.
	if got.Crafts[1].DecouplePlan != nil {
		t.Errorf("jettisoned craft plan = %v, want nil (no plan ⇒ single-pop)", got.Crafts[1].DecouplePlan)
	}
}

// transposedApolloWorld builds a world whose active craft is a
// transposed Apollo composite ([SM, CM, Descent, Ascent] with the core
// and LM as docked components carrying per-stage breakdowns). Shared by
// the multi-stage DockedComponent save tests.
func transposedApolloWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0
	for i := 0; i < 3; i++ {
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("decouple #%d: %v", i, err)
		}
	}
	if err := w.Transpose(0); err != nil {
		t.Fatalf("Transpose: %v", err)
	}
	return w
}

// TestSaveLoadMultiStageDockedComponent — v0.12 / ADR 0009. A transposed
// Apollo composite carries DockedComponents with a full per-stage
// breakdown (the LM = Descent + Ascent; the core = SM + CM). Save + Load
// must round-trip those breakdowns so Undock post-reload still rebuilds
// the LM as a 2-stage craft.
func TestSaveLoadMultiStageDockedComponent(t *testing.T) {
	w := transposedApolloWorld(t)

	// Pre-save: the LM component carries 2 stages, the core 2 stages.
	pre := w.Crafts[0].DockedComponents
	if len(pre) != 2 {
		t.Fatalf("pre-save: %d docked components, want 2", len(pre))
	}
	foundLM := false
	for _, dc := range pre {
		if len(dc.Stages) == 2 && dc.Stages[0].Name == "Descent" {
			foundLM = true
		}
	}
	if !foundLM {
		t.Fatal("pre-save: no 2-stage [Descent, Ascent] docked component")
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The reloaded composite's docked components keep their breakdowns.
	comps := got.Crafts[0].DockedComponents
	if len(comps) != 2 {
		t.Fatalf("reloaded: %d docked components, want 2", len(comps))
	}
	var lmStages int
	for _, dc := range comps {
		if len(dc.Stages) == 2 && dc.Stages[0].Name == "Descent" && dc.Stages[1].Name == "Ascent" {
			lmStages = len(dc.Stages)
		}
	}
	if lmStages != 2 {
		t.Fatalf("reloaded LM component lost its 2-stage breakdown")
	}

	// Undock post-reload rebuilds the LM as a real 2-stage craft.
	if !got.Undock(0) {
		t.Fatal("Undock after reload returned false")
	}
	hasLM := false
	for _, c := range got.Crafts {
		if len(c.Stages) == 2 && c.Stages[0].Name == "Descent" && c.Stages[1].Name == "Ascent" {
			hasLM = true
		}
	}
	if !hasLM {
		t.Error("post-reload undock did not rebuild a 2-stage LM craft")
	}
}

// TestRoundtripTopLevelStageFields — the persisted spacecraft.Stage
// fields on a craft's top-level Stages must survive Save+Load. Guards
// the shared simStagesToWire path (Craft.Stages and DockedComponent.
// Stages both go through it now): a new persisted field added to the
// helper that a parallel copy path missed would silently drop on save.
// Distinctive per-field values make any dropped field a mismatch. Only
// the persisted fields are compared — the launch-sprite / fuel-type
// cosmetics are intentionally re-derived from the catalog, not stored.
func TestRoundtripTopLevelStageFields(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	want := []spacecraft.Stage{
		{
			LoadoutID: "lower", Name: "Booster", Glyph: "B", Color: "#112233",
			DryMass: 1200, FuelMass: 8000, FuelCapacity: 9000,
			Thrust: 750000, Isp: 282, MonopropMass: 12, MonopropCap: 40,
			RCSThrust: 220, RCSIsp: 210, BallisticCoefficient: 350,
			CanSoftLand: false, HasParachute: true,
		},
		{
			LoadoutID: "upper", Name: "Payload", Glyph: "P", Color: "#445566",
			DryMass: 300, FuelMass: 1500, FuelCapacity: 1600,
			Thrust: 60000, Isp: 345, MonopropMass: 7.5, MonopropCap: 25,
			RCSThrust: 110, RCSIsp: 205, BallisticCoefficient: 120,
			CanSoftLand: true, HasParachute: false,
		},
	}
	w.ActiveCraft().Stages = append([]spacecraft.Stage(nil), want...)
	w.ActiveCraft().SyncFields()

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Project to just the persisted fields so the comparison ignores the
	// catalog-re-derived cosmetics (LaunchSprite*, FuelType).
	persisted := func(s spacecraft.Stage) spacecraft.Stage {
		return spacecraft.Stage{
			LoadoutID: s.LoadoutID, Name: s.Name, Glyph: s.Glyph, Color: s.Color,
			DryMass: s.DryMass, FuelMass: s.FuelMass, FuelCapacity: s.FuelCapacity,
			Thrust: s.Thrust, Isp: s.Isp, MonopropMass: s.MonopropMass,
			MonopropCap: s.MonopropCap, RCSThrust: s.RCSThrust, RCSIsp: s.RCSIsp,
			BallisticCoefficient: s.BallisticCoefficient,
			CanSoftLand:          s.CanSoftLand, HasParachute: s.HasParachute,
		}
	}
	gotStages := got.ActiveCraft().Stages
	if len(gotStages) != len(want) {
		t.Fatalf("Stages count: got %d want %d", len(gotStages), len(want))
	}
	for i := range want {
		if persisted(gotStages[i]) != persisted(want[i]) {
			t.Errorf("Stage[%d]: got %+v want %+v", i, gotStages[i], want[i])
		}
	}
}

// TestJettisonedDebrisStaysDebrisAcrossSave (hardening, ADR 0027): a saved
// spent booster (Role jettisoned-stage, no command source) must reload as
// passive debris. The load-time EnsureCommandSource backfill is meant only
// for pre-comms saves and must not resurrect debris into a commandable probe
// (and a CommNet node).
func TestJettisonedDebrisStaysDebrisAcrossSave(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Swap in a multi-stage, crew-tended Saturn V so StageActive can pop a
	// spent booster as real debris.
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	stack.Stages[len(stack.Stages)-1].CommandSource = spacecraft.CommandCrewed
	stack.SyncFields()
	w.Crafts[0] = stack
	w.EnsureCraftIDs()
	w.ActiveCraftIdx = 0

	_, jettIdx, err := w.StageActive(0)
	if err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if w.Crafts[jettIdx].Controllable {
		t.Fatal("setup: a freshly jettisoned booster should be passive debris")
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var debris *spacecraft.Spacecraft
	for _, c := range got.Crafts {
		if c != nil && c.Role == spacecraft.RoleJettisonedStage {
			debris = c
			break
		}
	}
	if debris == nil {
		t.Fatal("jettisoned debris not found after reload")
	}
	if debris.Controllable {
		t.Error("a saved spent booster must stay debris, not reload as a commandable probe")
	}
}

// TestLoadOldSaveDockedComponentFallsBackSingleStage — a v6 save written
// before ADR 0009 has DockedComponents with no "stages" array. Loading
// it and undocking must fall back to the legacy single-stage rebuild
// (one craft per component), not error. Simulated by stripping the
// "stages" keys from a freshly-saved composite's JSON.
func TestLoadOldSaveDockedComponentFallsBackSingleStage(t *testing.T) {
	w := transposedApolloWorld(t)
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Strip every docked component's "stages" key to mimic a pre-ADR-0009
	// save.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	payload, _ := doc["payload"].(map[string]any)
	crafts, _ := payload["crafts"].([]any)
	stripped := 0
	for _, c := range crafts {
		cm, _ := c.(map[string]any)
		dcs, _ := cm["docked_components"].([]any)
		for _, dc := range dcs {
			if dcm, ok := dc.(map[string]any); ok {
				if _, had := dcm["stages"]; had {
					delete(dcm, "stages")
					stripped++
				}
			}
		}
	}
	if stripped == 0 {
		t.Fatal("test setup: no docked-component stages found to strip")
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Load the stripped (old-shape) save and undock — must fall back to
	// single-stage rebuild without error.
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load stripped save: %v", err)
	}
	for _, dc := range got.Crafts[0].DockedComponents {
		if len(dc.Stages) != 0 {
			t.Errorf("stripped save still carries a stage breakdown: %d stages", len(dc.Stages))
		}
	}
	if !got.Undock(0) {
		t.Fatal("Undock on stripped (old-shape) save returned false")
	}
	// Fallback path → each component rebuilds as a single-stage craft.
	for _, c := range got.Crafts {
		if len(c.DockedComponents) == 0 && len(c.Stages) != 1 {
			t.Errorf("old-save fallback restored a %d-stage craft, want single-stage", len(c.Stages))
		}
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

// TestLoadOutOfBoundsWarpIdxClampsToValid — a forged warp_idx outside
// [0, len(WarpFactors)-1] must clamp to a valid index at load instead of
// surviving into the world, where the first Tick would panic on
// WarpFactors[idx] (clock.go:52). Mirrors the SystemIdx/ActiveCraftIdx
// load-time guards; WarpUp/WarpDown already keep the index in range, so
// this is purely a load-time gap. (#90)
func TestLoadOutOfBoundsWarpIdxClampsToValid(t *testing.T) {
	for _, forged := range []int{6, -1, 999} {
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
		f.Payload.WarpIdx = forged
		tampered, _ := json.Marshal(f)
		if err := os.WriteFile(path, tampered, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got, err := save.Load(path)
		if err != nil {
			t.Fatalf("Load (warp_idx=%d): %v", forged, err)
		}
		if got.Clock.WarpIdx < 0 || got.Clock.WarpIdx >= len(sim.WarpFactors) {
			t.Errorf("warp_idx=%d loaded as %d, want clamped into [0,%d)", forged, got.Clock.WarpIdx, len(sim.WarpFactors))
		}
		// And the clamp must actually prevent the Tick panic.
		got.Clock.Paused = false
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("warp_idx=%d: Tick panicked after load: %v", forged, r)
				}
			}()
			got.Tick()
		}()
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

// TestTargetCraftIdxRoundtrip — target binding survives save/load.
// Plant a target-relative node bound by stable craft ID (ADR 0012) and
// confirm it round-trips with the same value. Also confirms the JSON
// omitempty tag works: a non-target node round-trips with
// TargetCraftID == 0 (no field on disk → zero unmarshals).
func TestTargetCraftIdxRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	wantID := w.Crafts[0].ID
	// Target-relative node bound to the craft by stable ID.
	w.PlanNode(sim.ManeuverNode{
		Mode:          spacecraft.BurnTargetPrograde,
		DV:            10,
		Event:         sim.TriggerNextClosestApproach,
		TargetCraftID: wantID,
	})
	// Non-target node: no binding.
	w.PlanNode(sim.ManeuverNode{
		Mode: spacecraft.BurnPrograde,
		DV:   100,
	})

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
	// Find the target-bound node — sortNodes may reorder.
	var bound, free *sim.ManeuverNode
	for i := range nodes {
		if nodes[i].Mode == spacecraft.BurnTargetPrograde {
			bound = &nodes[i]
		} else {
			free = &nodes[i]
		}
	}
	if bound == nil || free == nil {
		t.Fatal("did not find both nodes after load")
	}
	if bound.TargetCraftID != wantID {
		t.Errorf("bound node TargetCraftID: got %d, want %d", bound.TargetCraftID, wantID)
	}
	if free.TargetCraftID != 0 {
		t.Errorf("free node TargetCraftID: got %d, want 0", free.TargetCraftID)
	}
	if bound.Event != sim.TriggerNextClosestApproach {
		t.Errorf("bound node Event: got %v, want TriggerNextClosestApproach", bound.Event)
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
	// v0.9.1+: Stages is the source of truth. Write the test
	// values into Stages[0] (the bottom = active stage) and let
	// SyncFields refresh the flat shadow fields.
	c := w.ActiveCraft()
	c.Stages[0].MonopropMass = 17.5
	c.Stages[0].MonopropCap = 50
	c.Stages[0].RCSThrust = 440
	c.Stages[0].RCSIsp = 220
	c.SyncFields()

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
	// v0.9.1+: Monoprop writes go through Stages[0]+SyncFields.
	w.Crafts[1].Stages[0].MonopropMass = 123.45
	w.Crafts[1].SyncFields()
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

// v0.9.0+: World.Target is additive on schema v5. Verify round-trip
// for both kinds plus the no-target zero state.
func TestTargetRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetBody(3)

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Target.Kind != sim.TargetBody || got.Target.BodyIdx != 3 {
		t.Errorf("Target after roundtrip: got %+v, want {TargetBody, 3}", got.Target)
	}
}

func TestTargetCraftRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	// Active is now idx 1 (the new sister); target idx 0 (the
	// original LEO craft).
	w.SetTargetCraft(0)
	wantID := w.Crafts[0].ID
	if w.Target.Kind != sim.TargetCraft || w.Target.CraftID != wantID {
		t.Fatalf("precondition: SetTargetCraft(0) → %+v", w.Target)
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Target.Kind != sim.TargetCraft || got.Target.CraftID != wantID {
		t.Errorf("Target after roundtrip: got %+v, want {TargetCraft, ID %d}", got.Target, wantID)
	}
}

// TestPerCraftTargetRoundtrip exercises the v0.9.3 polish: each
// craft persists its own Target through save/load. Bind distinct
// targets on two craft, round-trip, and confirm switching to either
// surfaces that craft's stored target as w.Target.
func TestPerCraftTargetRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	// Active is craft 1 (the sister). Target body idx 5 here.
	w.SetTargetBody(5)
	craft1Want := w.Target

	// Switch to craft 0, target body idx 3.
	w.SetActiveCraftIdx(0)
	w.SetTargetBody(3)
	craft0Want := w.Target

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Active was craft 0 at save time → load surfaces craft 0's
	// target.
	if got.Target != craft0Want {
		t.Errorf("active=0 target after roundtrip: got %+v, want %+v", got.Target, craft0Want)
	}
	got.SetActiveCraftIdx(1)
	if got.Target != craft1Want {
		t.Errorf("after switch to craft 1: got %+v, want %+v", got.Target, craft1Want)
	}
	got.SetActiveCraftIdx(0)
	if got.Target != craft0Want {
		t.Errorf("after switch back to craft 0: got %+v, want %+v", got.Target, craft0Want)
	}
}

// Pre-v0.9 saves predate the unified target slot — the JSON envelope
// has no `target` key. Loading must yield TargetNone with no error.
// We exercise this by saving with no target set (which the wire form
// drops via the *Target nil pointer) and confirming load produces
// TargetNone.
func TestEmptyTargetRoundtripsAsNone(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.Target.Kind != sim.TargetNone {
		t.Fatalf("precondition: NewWorld target kind = %v, want TargetNone", w.Target.Kind)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The on-disk JSON must omit the target field entirely (proves
	// older saves written without the field continue to load
	// cleanly through the same path).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read save: %v", err)
	}
	var envelope struct {
		Payload map[string]json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("parse save: %v", err)
	}
	if _, present := envelope.Payload["target"]; present {
		t.Errorf("on-disk payload contains \"target\" key when target was None — "+
			"omitempty failing. payload keys: %+v", keysOf(envelope.Payload))
	}

	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Target.Kind != sim.TargetNone {
		t.Errorf("Target after empty roundtrip: got %+v, want TargetNone", got.Target)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestV5SaveLoadsAsV6: a v5-shaped save (no Stages on the wire,
// flat propulsion fields populated) must load with Stages
// reconstructed via migrateV5CraftToStages. Mass + propellant +
// engine numbers should match the v5 flat fields.
func TestV5SaveLoadsAsV6(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Forge a v5 envelope: drop Stages from the wire form, set
	// Version=5. The flat fields stay populated so the v5→v6
	// migration has values to wrap.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(f.Payload.Crafts) == 0 {
		t.Fatalf("expected wire payload to carry at least one craft")
	}
	wantDry := f.Payload.Crafts[0].DryMass
	wantFuel := f.Payload.Crafts[0].Fuel
	wantThrust := f.Payload.Crafts[0].Thrust
	wantIsp := f.Payload.Crafts[0].Isp
	for i := range f.Payload.Crafts {
		f.Payload.Crafts[i].Stages = nil
	}
	f.Version = 5
	rewritten, _ := json.Marshal(f)
	if err := os.WriteFile(path, rewritten, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := got.ActiveCraft()
	if len(c.Stages) != 1 {
		t.Errorf("v5→v6 migration should produce 1 stage, got %d", len(c.Stages))
	}
	if c.Stages[0].DryMass != wantDry {
		t.Errorf("Stages[0].DryMass: got %.0f, want %.0f", c.Stages[0].DryMass, wantDry)
	}
	if c.Stages[0].FuelMass != wantFuel {
		t.Errorf("Stages[0].FuelMass: got %.0f, want %.0f", c.Stages[0].FuelMass, wantFuel)
	}
	if c.Stages[0].Thrust != wantThrust {
		t.Errorf("Stages[0].Thrust: got %.0f, want %.0f", c.Stages[0].Thrust, wantThrust)
	}
	if c.Stages[0].Isp != wantIsp {
		t.Errorf("Stages[0].Isp: got %.0f, want %.0f", c.Stages[0].Isp, wantIsp)
	}
	// Flat shadow fields should mirror Stages[0] post-SyncFields.
	if c.DryMass != wantDry || c.Fuel != wantFuel {
		t.Errorf("flat fields mismatch after migration: dry=%v fuel=%v",
			c.DryMass, c.Fuel)
	}
}

// TestSaturnVStagesRoundtrip: a multi-stage Saturn-V loadout must
// serialise + deserialise with all three stages intact, including
// per-stage fuel + thrust + Isp.
func TestSaturnVStagesRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	saturn := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	saturn.Primary = w.ActiveCraft().Primary
	saturn.State = w.ActiveCraft().State
	w.Crafts[0] = saturn

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := got.ActiveCraft()
	if len(c.Stages) != 3 {
		t.Fatalf("Saturn-V should round-trip with 3 stages, got %d", len(c.Stages))
	}
	for i, want := range saturn.Stages {
		if c.Stages[i].DryMass != want.DryMass ||
			c.Stages[i].FuelMass != want.FuelMass ||
			c.Stages[i].Thrust != want.Thrust ||
			c.Stages[i].Isp != want.Isp {
			t.Errorf("stage %d round-trip mismatch: got %+v, want %+v",
				i, c.Stages[i], want)
		}
	}
}

// v0.10.0: the physical nose (CurrentAttitudeDir) must round-trip so a
// craft caught mid-slew doesn't teleport its nose on reload.
func TestCurrentAttitudeDirRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	want := orbital.Vec3{X: 0.36, Y: -0.48, Z: 0.8}.Unit() // non-axis-aligned unit
	w.ActiveCraft().CurrentAttitudeDir = want

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d := got.ActiveCraft().CurrentAttitudeDir.Sub(want).Norm(); d > 1e-12 {
		t.Errorf("CurrentAttitudeDir round-trip off by %.3e: got %+v want %+v",
			d, got.ActiveCraft().CurrentAttitudeDir, want)
	}
}

// A pre-v0.10.0 save has no current_attitude_dir key → decodes to a
// zero Vec3. The craft must NOT teleport its nose; the slew
// integrator's first-tick snap seeds it from the commanded direction.
func TestLegacySaveNoAttitudeDirSnapsNoTeleport(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Strip the field to simulate a pre-v0.10.0 save.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var f save.File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	f.Payload.Crafts[0].CurrentAttitudeDir = save.Vec3{} // legacy: absent → zero
	stripped, _ := json.Marshal(f)
	if err := os.WriteFile(path, stripped, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := got.ActiveCraft()
	if c.CurrentAttitudeDir.Norm() != 0 {
		t.Fatalf("legacy save should load with zero CurrentAttitudeDir, got %+v",
			c.CurrentAttitudeDir)
	}
	// One tick: the first-tick snap guard must seed (not slew-from-
	// garbage, not leave zero) the physical nose.
	got.Tick()
	if n := c.CurrentAttitudeDir.Norm(); n < 0.999 || n > 1.001 {
		t.Errorf("first-tick snap did not seed a unit nose: |dir|=%.6f", n)
	}
}

// TestRoundtripParachute — v0.12 Slice 3 (ADR 0008): the runtime
// ChuteState (craft-level) and the per-Stage HasParachute capability
// both round-trip, and SyncFields re-derives the Spacecraft.HasParachute
// mirror from Stages[0] on load. No SchemaVersion bump (omitempty-additive).
func TestRoundtripParachute(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	// Stamp the active craft as a chute-bearing capsule mid-descent
	// (armed, waiting to inflate).
	c.Stages[0].HasParachute = true
	c.ChuteState = spacecraft.ChuteArmed
	c.SyncFields()
	if !c.HasParachute {
		t.Fatalf("setup: mirror should be true after SyncFields")
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	gc := got.ActiveCraft()
	if gc.ChuteState != spacecraft.ChuteArmed {
		t.Errorf("ChuteState: got %v, want Armed", gc.ChuteState)
	}
	if !gc.Stages[0].HasParachute {
		t.Errorf("per-stage HasParachute did not round-trip")
	}
	if !gc.HasParachute {
		t.Errorf("Spacecraft.HasParachute mirror not re-derived on load")
	}
}

// TestRoundtripPreservesMoonPhase is a regression for the moon-phase bug:
// the Moon's mean anomaly used to be anchored to the calculator's mutable
// seed epoch (re-seeded from SimTime on Load), so a reloaded game snapped the
// Moon back to its m0 base position instead of where it sat at save time.
// Anchoring generic ephemerides to bodies.J2000 makes the position a pure
// function of SimTime, so a save/load round-trip must preserve it.
func TestRoundtripPreservesMoonPhase(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Skip("Moon missing from Sol")
	}
	// Advance well past J2000 so the Moon is nowhere near its m0 base phase.
	w.Clock.SimTime = w.Clock.SimTime.Add(7 * 24 * time.Hour)
	before := w.BodyPositionAt(*moon, w.Clock.SimTime)

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	gsys := got.System()
	gm := gsys.FindBody("Moon")
	after := got.BodyPositionAt(*gm, got.Clock.SimTime)
	if d := before.Sub(after).Norm(); d > 1.0 { // metres
		t.Errorf("Moon moved across save/load: |Δ| = %.3g m (before %v, after %v)",
			d, before, after)
	}
}

// TestPreAmendmentAntennaReRanged (#182, ADR 0027 §2 amendment): a save
// written before the rated-range amendment stored the antenna's legacy power
// in antenna_power_w. On load the new antenna_range_m is absent (0), so the
// loader re-ranges the surviving antenna kind to its tier — a relay antenna
// comes back at the cislunar range, not dead. Simulated by renaming the new
// save's antenna_range_m key back to the legacy antenna_power_w.
func TestPreAmendmentAntennaReRanged(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	tug := spacecraft.NewFromLoadout("Relay-Tug")
	tug.Primary = w.Crafts[0].Primary
	tug.State = w.Crafts[0].State
	w.Crafts[0] = tug

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	old := strings.ReplaceAll(string(raw), "antenna_range_m", "antenna_power_w")
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := got.Crafts[0].Stages[0]
	if s.AntennaKind != spacecraft.AntennaRelay || s.AntennaRangeM != spacecraft.AntennaRangeRelayCislunar {
		t.Errorf("pre-amendment antenna = %q/%g, want relay/%g (re-ranged from kind on load)",
			s.AntennaKind, s.AntennaRangeM, spacecraft.AntennaRangeRelayCislunar)
	}
}

// TestCommsAttributesRoundtrip (C2-1, ADR 0027): per-stage command_source
// + antenna survive a save/load round-trip, and a pre-comms craft (stages
// with no command source) is backfilled to controllable on load.
func TestCommsAttributesRoundtrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// A relay probe (ntr-tug): probe command source + relay antenna.
	tug := spacecraft.NewFromLoadout("Relay-Tug")
	tug.Primary = w.Crafts[0].Primary
	tug.State = w.Crafts[0].State
	w.Crafts[0] = tug

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	c := got.Crafts[0]
	if c.Stages[0].CommandSource != spacecraft.CommandProbe {
		t.Errorf("command source = %q, want probe after round-trip", c.Stages[0].CommandSource)
	}
	if c.Stages[0].AntennaKind != spacecraft.AntennaRelay || c.Stages[0].AntennaRangeM != spacecraft.AntennaRangeRelayCislunar {
		t.Errorf("antenna = %q/%g, want relay/%g after round-trip", c.Stages[0].AntennaKind, c.Stages[0].AntennaRangeM, spacecraft.AntennaRangeRelayCislunar)
	}
	if !c.Controllable || c.Crewed {
		t.Errorf("probe vessel: Controllable=%v Crewed=%v, want true/false", c.Controllable, c.Crewed)
	}

	// Pre-comms craft: strip the command source from every stage (an old
	// save shape), confirm the load-time backfill restores controllability.
	w2, _ := sim.NewWorld()
	for i := range w2.Crafts[0].Stages {
		w2.Crafts[0].Stages[i].CommandSource = ""
	}
	w2.Crafts[0].SyncFields()
	if w2.Crafts[0].Controllable {
		t.Fatal("precondition: stripped craft should be uncontrollable before save")
	}
	path2 := filepath.Join(t.TempDir(), "old.json")
	if err := save.Save(w2, path2); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got2, err := save.Load(path2)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got2.Crafts[0].Controllable {
		t.Error("pre-comms craft should be backfilled to controllable on load")
	}
}
