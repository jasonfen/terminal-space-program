package spacecraft

import "testing"

// TestEveryEngineBearingStageHasFuelType (v0.11.5 sub-scope 4): every
// catalog stage with Thrust > 0 carries a non-empty FuelType so a
// future stage can't ship without its flame colour by accident. The
// pure-monoprop RCS Tug is the only legitimate exception (no main
// engine ⇒ no fuel-type read).
func TestEveryEngineBearingStageHasFuelType(t *testing.T) {
	for id, m := range StageCatalog {
		if m.thrust <= 0 {
			continue
		}
		if m.fuelType == "" {
			t.Errorf("catalog stage %q has Thrust=%v but no FuelType set", id, m.thrust)
		}
	}
}

// TestBuildStageCarriesFuelType: catalog FuelType round-trips through
// BuildStage onto the constructed Stage.
func TestBuildStageCarriesFuelType(t *testing.T) {
	cases := []struct {
		id   string
		want string
	}{
		{StageModuleSICID, FuelTypeKerolox},
		{StageModuleSIIID, FuelTypeHydrolox},
		{StageModuleSRBID, FuelTypeSolid},
		{StageModuleLanderID, FuelTypeHypergolic},
		{StageModuleCSMID, FuelTypeHypergolic},
		{StageModuleRCSTugID, ""}, // monoprop tug — no main engine
	}
	for _, c := range cases {
		s, ok := BuildStage(c.id)
		if !ok {
			t.Errorf("BuildStage(%q) failed", c.id)
			continue
		}
		if s.FuelType != c.want {
			t.Errorf("BuildStage(%q).FuelType = %q, want %q", c.id, s.FuelType, c.want)
		}
	}
}

// TestLoadoutStagesCarryFuelType: stages built via stageWithBC (the
// canonical Saturn-V / SLS / Falcon-9 / Apollo-Stack literals) get
// FuelType populated by name-lookup against the catalog.
func TestLoadoutStagesCarryFuelType(t *testing.T) {
	saturnV := Loadouts[LoadoutSaturnVID]
	want := []string{FuelTypeKerolox, FuelTypeHydrolox, FuelTypeHydrolox}
	for i, s := range saturnV.Stages {
		if s.FuelType != want[i] {
			t.Errorf("Saturn-V Stage[%d] (%s) FuelType = %q, want %q", i, s.Name, s.FuelType, want[i])
		}
	}
	apollo := Loadouts[LoadoutApolloStackID]
	// Apollo: S-IC, S-II, S-IVB, LM, CSM
	wantApollo := []string{FuelTypeKerolox, FuelTypeHydrolox, FuelTypeHydrolox, FuelTypeHypergolic, FuelTypeHypergolic}
	for i, s := range apollo.Stages {
		if s.FuelType != wantApollo[i] {
			t.Errorf("Apollo-Stack Stage[%d] (%s) FuelType = %q, want %q", i, s.Name, s.FuelType, wantApollo[i])
		}
	}
}
