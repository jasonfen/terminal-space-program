package spacecraft

import (
	"reflect"
	"testing"
)

// TestBuildStageGoldenByteIdentical pins the FULL Stage that BuildStage
// produces for representative catalog parts, so the ADR 0026 (C1-2)
// migration of the catalog data into data/parts.json — and any future
// edit to that file — stays byte-identical to the pre-migration Go
// literal. s-ivb anchors a fuelled engine stage (dry 11000 ⇒ the RCS pool
// derives to round numbers); capsule anchors an engineless parachute part
// (RCS still scales from dry mass). The RCS pool is derived at build time
// via DefaultRCSLoadout, not stored per part — asserted here against that
// same canonical derivation.
func TestBuildStageGoldenByteIdentical(t *testing.T) {
	sivbMono, sivbCap, sivbThr, sivbIsp := DefaultRCSLoadout(11000)
	wantSIVB := Stage{
		Name:                 "S-IVB",
		Glyph:                VesselGlyph,
		Color:                "#FFD93D",
		DryMass:              11000,
		FuelMass:             109000,
		FuelCapacity:         109000,
		Thrust:               1023000,
		Isp:                  421,
		MonopropMass:         sivbMono,
		MonopropCap:          sivbCap,
		RCSThrust:            sivbThr,
		RCSIsp:               sivbIsp,
		BallisticCoefficient: 6.25e-5,
		LaunchSpriteRowsPx:   12,
		LaunchSpriteWidthPx:  3,
		LaunchSpriteColor:    "#D8D8D8",
		FuelType:             FuelTypeHydrolox,
	}
	if got, ok := BuildStage(StageModuleSIVBID); !ok || !reflect.DeepEqual(got, wantSIVB) {
		t.Errorf("BuildStage(s-ivb) byte-identity drift:\n want %+v\n  got %+v (ok=%v)", wantSIVB, got, ok)
	}

	capMono, capCap, capThr, capIsp := DefaultRCSLoadout(5800)
	wantCapsule := Stage{
		Name:                "Capsule",
		Glyph:               VesselGlyph,
		Color:               "#B8C8E0",
		DryMass:             5800,
		MonopropMass:        capMono,
		MonopropCap:         capCap,
		RCSThrust:           capThr,
		RCSIsp:              capIsp,
		LaunchSpriteRowsPx:  6,
		LaunchSpriteWidthPx: 3,
		LaunchSpriteColor:   "#C8C8D0",
		HasParachute:        true,
		CommandSource:       CommandCrewed, // v0.23/ADR 0027: the capsule is a crewed pod
	}
	if got, ok := BuildStage(StageModuleCapsuleID); !ok || !reflect.DeepEqual(got, wantCapsule) {
		t.Errorf("BuildStage(capsule) byte-identity drift:\n want %+v\n  got %+v (ok=%v)", wantCapsule, got, ok)
	}
}

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

// TestApolloStackSilhouetteIsUnifiedPalette (v0.11.5-followup): the
// Apollo Stack must paint its launch silhouette in cohesive rocket-body
// tones (cream / white / metal), NOT a rainbow of the per-stage
// slate-HUD colours. Pins each stage's LaunchSpriteColor override
// against the unified catalog values so a future catalog edit can't
// accidentally restore the rainbow read. v0.12 / ADR 0009: the fused
// CSM split into SM (silver service module) + CM (pale cone).
func TestApolloStackSilhouetteIsUnifiedPalette(t *testing.T) {
	apollo := Loadouts[LoadoutApolloStackID]
	want := map[string]string{
		"S-IC":    "#F5EFE0",
		"S-II":    "#E8E8E8",
		"S-IVB":   "#D8D8D8",
		"Descent": "#D4C088", // v0.12 Slice 2: LM split — descent keeps the gold foil
		"Ascent":  "#C8C8B0", // pale metal band above the descent
		"SM":      "#C8C8D0", // bare aluminium service module
		"CM":      "#D8D8E0", // pale command-module cone
	}
	for _, s := range apollo.Stages {
		w, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected stage %q in Apollo Stack", s.Name)
			continue
		}
		if s.LaunchSpriteColor != w {
			t.Errorf("Apollo Stack %s LaunchSpriteColor = %q, want %q", s.Name, s.LaunchSpriteColor, w)
		}
		// Per-stage Color (slate HUD identity) must remain its
		// original distinct hue — overriding the silhouette does
		// not touch slate identity.
		if s.Color == s.LaunchSpriteColor {
			t.Errorf("Apollo Stack %s Color and LaunchSpriteColor must differ — silhouette override decouples them", s.Name)
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
	// Apollo: S-IC, S-II, S-IVB, Descent, Ascent, SM, CM (v0.12 / ADR
	// 0009: the fused CSM split into a hypergolic-SPS Service Module +
	// an engineless Command Module — the CM carries no fuelType).
	wantApollo := []string{FuelTypeKerolox, FuelTypeHydrolox, FuelTypeHydrolox, FuelTypeHypergolic, FuelTypeHypergolic, FuelTypeHypergolic, ""}
	for i, s := range apollo.Stages {
		if s.FuelType != wantApollo[i] {
			t.Errorf("Apollo-Stack Stage[%d] (%s) FuelType = %q, want %q", i, s.Name, s.FuelType, wantApollo[i])
		}
	}
}
