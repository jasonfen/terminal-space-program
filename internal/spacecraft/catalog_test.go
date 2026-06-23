package spacecraft

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// withUserCatalog creates an empty user-overlay directory under a temp
// XDG_CONFIG_HOME and returns its path. Mirrors bodies' withUserOverlay
// helper but targets the loadout-catalog subdir (ADR 0026 §2 — the
// bodies-pattern overlay, in its own `loadouts/` subdir).
func withUserCatalog(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "terminal-space-program", "loadouts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir overlay dir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", root)
	return dir
}

// C1-1: the embedded catalog (empty stubs this slice) must always load
// without error and produce no warnings when no user overlay exists.
func TestLoadCatalogEmbeddedStubLoads(t *testing.T) {
	withUserCatalog(t) // empty overlay dir
	parts, loadouts, warnings, err := LoadCatalogWithWarnings()
	if err != nil {
		t.Fatalf("LoadCatalogWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	// Stubs are empty in C1-1; the maps/slices must be non-nil and ready.
	if parts == nil {
		t.Error("parts map is nil, want empty non-nil")
	}
	if loadouts == nil {
		// An empty embedded catalog yields a nil slice; that's acceptable,
		// but the call must not error. Nothing to assert beyond no-error.
		_ = loadouts
	}
}

// C1-1: LoadCatalog drops warnings and returns the same data.
func TestLoadCatalogDropsWarnings(t *testing.T) {
	withUserCatalog(t)
	parts, loadouts, err := LoadCatalog()
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if parts == nil {
		t.Error("parts map is nil")
	}
	_ = loadouts
}

// C1-1: the schema round-trips through JSON byte-for-byte (struct →
// JSON → struct preserves every field).
func TestCatalogRoundTrip(t *testing.T) {
	fill := 0.5
	want := Catalog{
		Parts: []Part{
			{
				ID:                   "test-booster",
				Name:                 "Test Booster",
				Glyph:                "^",
				Color:                "#FF8C42",
				Tier:                 "booster",
				DryMassKg:            130000,
				FuelMassKg:           2000000,
				FuelCapacityKg:       2000000,
				ThrustN:              34000000,
				IspS:                 263,
				MonopropMassKg:       100,
				MonopropCapacityKg:   100,
				RCSThrustN:           1000,
				RCSIspS:              220,
				BallisticCoefficient: 8e-6,
				LaunchSpriteRowsPx:   6,
				LaunchSpriteWidthPx:  4,
				LaunchSpriteColor:    "#DDDDDD",
				FuelType:             FuelTypeKerolox,
				LaunchSpriteHasLegs:  false,
				CanSoftLand:          false,
				HasParachute:         false,
				// Forward-compatible (cycle 2 / ADR 0027) — declared now.
				CommandSource: "none",
				Antenna:       &Antenna{Kind: "relay", PowerW: 5000},
			},
			{
				ID:            "test-capsule",
				Name:          "Test Capsule",
				DryMassKg:     5000,
				HasParachute:  true,
				CommandSource: "crewed",
			},
		},
		Loadouts: []LoadoutDef{
			{
				ID:                "test-stack",
				Name:              "Test Stack",
				Role:              "launch",
				Glyph:             "A",
				Color:             "#FFD93D",
				Parts:             []PartRef{{PartID: "test-booster", Override: &PartOverride{FuelFillFraction: &fill, Name: "half tank"}}, {PartID: "test-capsule"}},
				DecouplePlan:      []int{1},
				NosePayloadPlan:   []int{1},
				SlewRateDegPerSec: 12,
				ScaleClass:        "stripped-back",
			},
		},
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Catalog
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("round-trip mismatch:\n want %+v\n  got %+v", want, got)
	}
}

// C1-1: a Part materializes into a runtime Stage with every physical /
// visual field mapped. (The migration in C1-2/C1-3 leans on this.)
func TestPartToStage(t *testing.T) {
	p := Part{
		ID:                   "p",
		Name:                 "S-IC",
		Glyph:                VesselGlyph,
		Color:                "#FF8C42",
		Tier:                 "booster",
		DryMassKg:            130000,
		FuelMassKg:           2000000,
		FuelCapacityKg:       2100000,
		ThrustN:              34000000,
		IspS:                 263,
		MonopropMassKg:       50,
		MonopropCapacityKg:   60,
		RCSThrustN:           1000,
		RCSIspS:              220,
		BallisticCoefficient: 8e-6,
		LaunchSpriteRowsPx:   6,
		LaunchSpriteWidthPx:  4,
		LaunchSpriteColor:    "#DDDDDD",
		FuelType:             FuelTypeKerolox,
		LaunchSpriteHasLegs:  true,
		CanSoftLand:          true,
		HasParachute:         true,
	}
	got := p.ToStage()
	want := Stage{
		DryMass:              130000,
		FuelMass:             2000000,
		FuelCapacity:         2100000,
		Thrust:               34000000,
		Isp:                  263,
		MonopropMass:         50,
		MonopropCap:          60,
		RCSThrust:            1000,
		RCSIsp:               220,
		BallisticCoefficient: 8e-6,
		Name:                 "S-IC",
		Glyph:                VesselGlyph,
		Color:                "#FF8C42",
		LaunchSpriteRowsPx:   6,
		LaunchSpriteWidthPx:  4,
		LaunchSpriteColor:    "#DDDDDD",
		FuelType:             FuelTypeKerolox,
		LaunchSpriteHasLegs:  true,
		CanSoftLand:          true,
		HasParachute:         true,
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("ToStage mismatch:\n want %+v\n  got %+v", want, got)
	}
}

// C1-1: a user overlay adds a brand-new part to the merged catalog.
func TestLoadCatalogUserOverlayAddsPart(t *testing.T) {
	dir := withUserCatalog(t)
	custom := `{"parts": [{"id": "user-part", "name": "User Part", "dry_mass_kg": 1234}]}`
	if err := os.WriteFile(filepath.Join(dir, "mine.json"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	parts, _, warnings, err := LoadCatalogWithWarnings()
	if err != nil {
		t.Fatalf("LoadCatalogWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	got, ok := parts["user-part"]
	if !ok {
		t.Fatal("user-part not in merged catalog")
	}
	if got.DryMassKg != 1234 || got.Name != "User Part" {
		t.Errorf("user-part = %+v, want dry=1234 name=\"User Part\"", got)
	}
}

// C1-1: user wins on ID — for both parts and loadouts. Tested at the
// merge seam since the embedded stub is empty this slice.
func TestMergeUserCatalogWinsOnID(t *testing.T) {
	dir := withUserCatalog(t)
	user := `{
		"parts": [{"id": "shared", "name": "User Version", "dry_mass_kg": 999}],
		"loadouts": [{"id": "rocket", "name": "User Rocket", "parts": [{"part_id": "shared"}]}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "override.json"), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	// Seed an "embedded" catalog the user file should override on ID.
	parts := map[string]Part{"shared": {ID: "shared", Name: "Embedded Version", DryMassKg: 1}}
	loadouts := []LoadoutDef{{ID: "rocket", Name: "Embedded Rocket", Source: "embedded"}}

	parts, loadouts, warnings := mergeUserCatalog(parts, loadouts, dir)
	if len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	if p := parts["shared"]; p.Name != "User Version" || p.DryMassKg != 999 {
		t.Errorf("part not overridden by user: %+v", p)
	}
	if len(loadouts) != 1 {
		t.Fatalf("loadouts = %d, want 1 (replaced, not appended)", len(loadouts))
	}
	if loadouts[0].Name != "User Rocket" || loadouts[0].Source != "user" {
		t.Errorf("loadout not overridden by user: %+v", loadouts[0])
	}
}

// C1-4: a user overlay loadout — referencing both a new user part and an
// embedded catalog part — resolves into a flyable Loadout (Stages stamped
// with the loadout ID, RCS derived from dry mass). Tested through the
// merge + resolve path without mutating package globals.
func TestUserOverlayLoadoutResolves(t *testing.T) {
	dir := withUserCatalog(t)
	user := `{
		"parts": [{"id":"my-tug","name":"My Tug","glyph":"➤","color":"#ABCDEF","dry_mass_kg":300,"fuel_mass_kg":5000,"fuel_capacity_kg":5000,"thrust_n":20000,"isp_s":300}],
		"loadouts": [{"id":"My-Rocket","name":"My Rocket","role":"custom","glyph":"➤","color":"#ABCDEF","parts":[{"part_id":"my-tug"},{"part_id":"s-ivb"}]}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "mine.json"), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	parts, defs, warnings, err := LoadCatalogWithWarnings()
	if err != nil {
		t.Fatalf("LoadCatalogWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	var def *LoadoutDef
	for i := range defs {
		if defs[i].ID == "My-Rocket" {
			def = &defs[i]
		}
	}
	if def == nil {
		t.Fatal("user loadout not merged into the catalog")
	}
	if def.Source != "user" {
		t.Errorf("user loadout Source = %q, want user", def.Source)
	}
	l := resolveLoadout(*def, parts)
	if len(l.Stages) != 2 {
		t.Fatalf("resolved stages = %d, want 2", len(l.Stages))
	}
	if l.Stages[0].Name != "My Tug" || l.Stages[0].LoadoutID != "My-Rocket" {
		t.Errorf("bottom stage = %+v, want My Tug stamped My-Rocket", l.Stages[0])
	}
	mono, _, _, _ := DefaultRCSLoadout(300)
	if l.Stages[0].MonopropMass != mono {
		t.Errorf("RCS not derived from dry mass: mono = %g, want %g", l.Stages[0].MonopropMass, mono)
	}
	if l.Stages[1].Name != "S-IVB" { // resolved against the EMBEDDED catalog part
		t.Errorf("top stage name = %q, want S-IVB (embedded part)", l.Stages[1].Name)
	}
}

// C1-4: LoadCatalogOverlay wires the merged catalog into the package
// globals (Loadouts / LoadoutOrder / StageCatalog) so the spawn form and
// --list-loadouts see user mods, while preserving the embedded set.
func TestLoadCatalogOverlayWiresGlobals(t *testing.T) {
	savedLoadouts, savedOrder, savedCatalog := Loadouts, LoadoutOrder, StageCatalog
	t.Cleanup(func() { Loadouts, LoadoutOrder, StageCatalog = savedLoadouts, savedOrder, savedCatalog })

	dir := withUserCatalog(t)
	user := `{"loadouts":[{"id":"Overlay-Test","name":"Overlay Test","role":"custom","glyph":"➤","color":"#FFFFFF","parts":[{"part_id":"capsule"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "o.json"), []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if warnings := LoadCatalogOverlay(); len(warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(warnings))
	}
	if _, ok := Loadouts["Overlay-Test"]; !ok {
		t.Error("overlay loadout not wired into Loadouts")
	}
	found := false
	for _, id := range LoadoutOrder {
		if id == "Overlay-Test" {
			found = true
		}
	}
	if !found {
		t.Error("overlay loadout not appended to LoadoutOrder")
	}
	if _, ok := Loadouts[LoadoutSaturnVID]; !ok {
		t.Error("embedded Saturn-V dropped after overlay merge")
	}
	if c := NewFromLoadout("Overlay-Test"); c == nil || len(c.Stages) != 1 {
		t.Fatal("overlay loadout is not flyable via NewFromLoadout")
	}
}

// C1-4: a malformed user overlay file is skipped with a warning; the
// embedded catalog still wires through intact.
func TestLoadCatalogOverlaySkipsMalformed(t *testing.T) {
	savedLoadouts, savedOrder, savedCatalog := Loadouts, LoadoutOrder, StageCatalog
	t.Cleanup(func() { Loadouts, LoadoutOrder, StageCatalog = savedLoadouts, savedOrder, savedCatalog })

	dir := withUserCatalog(t)
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	warnings := LoadCatalogOverlay()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warnings))
	}
	if _, ok := Loadouts[LoadoutSaturnVID]; !ok {
		t.Error("embedded catalog lost after a malformed overlay")
	}
}

// C1-5: the data-only modding-proof loadouts (Relay Tug / Station Keeper)
// resolve into flyable craft with NO Go change, and their parts carry the
// forward-compatible command_source + antenna attributes (cycle 2 / ADR
// 0027) authored purely in data.
func TestModdingProofLoadouts(t *testing.T) {
	for _, id := range []string{"Relay-Tug", "Station-Keeper"} {
		c := NewFromLoadout(id)
		if c == nil || len(c.Stages) != 1 {
			t.Errorf("NewFromLoadout(%q) is not a flyable single-stage craft", id)
			continue
		}
		if c.Thrust <= 0 {
			t.Errorf("%s has no main thrust (Thrust=%g)", id, c.Thrust)
		}
	}
	parts, _, _, err := LoadCatalogWithWarnings()
	if err != nil {
		t.Fatalf("LoadCatalogWithWarnings: %v", err)
	}
	ntr, ok := parts["ntr-tug"]
	if !ok {
		t.Fatal("ntr-tug part missing from the embedded catalog")
	}
	if ntr.CommandSource != "probe" {
		t.Errorf("ntr-tug command_source = %q, want probe", ntr.CommandSource)
	}
	if ntr.Antenna == nil || ntr.Antenna.Kind != "relay" {
		t.Errorf("ntr-tug antenna = %+v, want kind=relay", ntr.Antenna)
	}
	if ion := parts["ion-keeper"]; ion.Antenna == nil || ion.Antenna.Kind != "direct" {
		t.Errorf("ion-keeper antenna = %+v, want kind=direct", ion.Antenna)
	}
}

// C1-1: a malformed user file is skipped with a warning; the rest of the
// catalog still loads (ADR 0026 §3 — skip-bad-with-warning).
func TestLoadCatalogMalformedUserFileWarns(t *testing.T) {
	dir := withUserCatalog(t)
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := `{"parts": [{"id": "ok-part", "dry_mass_kg": 10}]}`
	if err := os.WriteFile(filepath.Join(dir, "good.json"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	parts, _, warnings, err := LoadCatalogWithWarnings()
	if err != nil {
		t.Fatalf("malformed user file should not fail load: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(warnings))
	}
	if _, ok := parts["ok-part"]; !ok {
		t.Error("the valid sibling file should still load")
	}
}
