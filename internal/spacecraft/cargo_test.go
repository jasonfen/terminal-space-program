package spacecraft

import (
	"math"
	"testing"
)

// ADR 0057 decision 5.1: a Cargo Hold component puts the hold flag on the
// stage that holds it; a stage without one is not a hold.
func TestComposeStageCargoHoldComponent(t *testing.T) {
	c, ok := Components["cargo-hold"]
	if !ok {
		t.Fatal("components.json has no cargo-hold")
	}
	if c.Name != "Cargo Hold" || c.Kind != ComponentStructure || c.DryMassKg != 100 || !c.CargoHold {
		t.Errorf("cargo-hold = %+v, want Cargo Hold / structure / 100 kg / cargo_hold", c)
	}
	if c.Description != "Receives cargo while docked." {
		t.Errorf("cargo-hold description = %q", c.Description)
	}
	with, warn := ComposeStage([]string{"probe-core", "cargo-hold"}, Components)
	if warn != "" {
		t.Fatal(warn)
	}
	without, warn := ComposeStage([]string{"probe-core"}, Components)
	if warn != "" {
		t.Fatal(warn)
	}
	if !with.CargoHold || without.CargoHold {
		t.Errorf("hold flag with/without = %v/%v, want true/false", with.CargoHold, without.CargoHold)
	}
	if with.HoldKg != 0 || with.CargoKg != 0 {
		t.Errorf("a fresh hold is empty, got cargo %g hold %g", with.CargoKg, with.HoldKg)
	}
	if d := with.DryMass - without.DryMass; d != 100 {
		t.Errorf("the hold adds %v kg, want 100", d)
	}
}

// The Dragon data: the cargo capsule carries 2,507 kg, the trunk 800 kg on both
// Dragons, the crew capsule none (ADR 0057 decision 1 table).
func TestDragonCargoDataTable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	parts, _, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]float64{"dragon-crew": 0, "dragon-cargo": 2507, "dragon-trunk": 800} {
		if got := parts[id].CargoKg; got != want {
			t.Errorf("%s cargo_kg = %g, want %g", id, got, want)
		}
	}
}

// Guard 5.2: a Cargo Dragon at spawn carries 3,307 kg that is mass but not dry
// mass, and the rocket equation sees it.
func TestCargoDragonCarriesCargoAsMass(t *testing.T) {
	c := NewFromLoadout("Cargo-Dragon")
	if c == nil {
		t.Fatal("no Cargo-Dragon loadout")
	}
	if c.Cargo != 3307 {
		t.Errorf("Cargo = %g, want 3307", c.Cargo)
	}
	if c.DryMass != 9500 {
		t.Errorf("DryMass = %g, want 9500 (cargo is not dry mass)", c.DryMass)
	}
	want := c.DryMass + c.Fuel + c.Monoprop + 3307
	if math.Abs(c.TotalMass()-want) > 1e-6 {
		t.Errorf("TotalMass = %g, want dry+fuel+monoprop+cargo = %g", c.TotalMass(), want)
	}
	if math.Abs(c.TotalMass()-15991) > 1 {
		t.Errorf("TotalMass = %g, want 15991 +-1", c.TotalMass())
	}
	crew := NewFromLoadout("Dragon")
	if crew.Cargo != 800 {
		t.Errorf("crewed Dragon Cargo = %g, want the trunk's 800", crew.Cargo)
	}
	// Δv: the loaded RCS floor is lower than the same pool on an unloaded twin.
	twin := NewFromLoadout("Cargo-Dragon")
	for i := range twin.Stages {
		twin.Stages[i].CargoKg = 0
	}
	twin.SyncFields()
	if twin.Cargo != 0 {
		t.Fatalf("twin still carries %g", twin.Cargo)
	}
	if c.RCSDeltaV() >= twin.RCSDeltaV() {
		t.Errorf("RCSDeltaV loaded %g >= unloaded %g; cargo must count against it", c.RCSDeltaV(), twin.RCSDeltaV())
	}
	// StackStats: the strip's mass includes cargo.
	if got := StackStats(c.Stages).TotalMass; math.Abs(got-(c.TotalMass()-0)) > 1e-6 {
		t.Errorf("StackStats.TotalMass = %g, want %g", got, c.TotalMass())
	}
}

// BuildStage carries the cargo numbers for a catalog part.
func TestBuildStageCarriesCargo(t *testing.T) {
	st, ok := BuildStage("dragon-cargo")
	if !ok {
		t.Fatal("dragon-cargo not in StageCatalog")
	}
	if st.CargoKg != 2507 {
		t.Errorf("BuildStage(dragon-cargo).CargoKg = %g, want 2507", st.CargoKg)
	}
}

// SyncFields sums both CargoKg and HoldKg into the flat mirror.
func TestSyncFieldsSumsCargoAndHold(t *testing.T) {
	c := NewFromStages([]Stage{
		{Name: "a", DryMass: 100, CargoKg: 30, CargoHold: true, HoldKg: 12},
		{Name: "b", DryMass: 100, CargoKg: 5},
	})
	if c.Cargo != 47 {
		t.Errorf("Cargo = %g, want 30+12+5", c.Cargo)
	}
}
