package spacecraft

import "testing"

// TestCommandSourceDerivation (C2-1, ADR 0027): SyncFields derives the
// vessel-level Crewed / Controllable mirrors from the per-stage command
// sources, and construction defaults a command-less vessel so it stays
// controllable.
func TestCommandSourceDerivation(t *testing.T) {
	cases := []struct {
		loadout         string
		wantCrewed      bool
		wantControllabe bool
	}{
		{LoadoutApolloStackID, true, true}, // CM is a crewed command source
		{LoadoutCapsuleID, true, true},     // crewed re-entry capsule
		{LoadoutKernStackID, true, true},   // crewed Pod core
		{LoadoutSaturnVID, false, true},    // no crew part → defaulted probe core
		{LoadoutSIVB1ID, false, true},      // single transfer stage → defaulted probe
		{"Relay-Tug", false, true},         // explicit probe (data-only loadout)
		{"Station-Keeper", false, true},    // explicit probe
	}
	for _, c := range cases {
		v := NewFromLoadout(c.loadout)
		if v.Crewed != c.wantCrewed {
			t.Errorf("%s Crewed = %v, want %v", c.loadout, v.Crewed, c.wantCrewed)
		}
		if v.Controllable != c.wantControllabe {
			t.Errorf("%s Controllable = %v, want %v", c.loadout, v.Controllable, c.wantControllabe)
		}
	}
}

// TestAntennaDerivation (C2-1): the vessel antenna mirror is the
// longest-ranged antenna across the stack; a vessel with none reads none.
func TestAntennaDerivation(t *testing.T) {
	tug := NewFromLoadout("Relay-Tug") // ntr-tug carries the relay-cislunar antenna
	if tug.AntennaKind != AntennaRelay || tug.AntennaRangeM != AntennaRangeRelayCislunar {
		t.Errorf("Relay-Tug antenna = %q/%g, want relay/%g", tug.AntennaKind, tug.AntennaRangeM, AntennaRangeRelayCislunar)
	}
	probe := NewFromLoadout("Station-Keeper") // ion-keeper: direct-basic
	if probe.AntennaKind != AntennaDirect || probe.AntennaRangeM != AntennaRangeDirectBasic {
		t.Errorf("Station-Keeper antenna = %q/%g, want direct/%g", probe.AntennaKind, probe.AntennaRangeM, AntennaRangeDirectBasic)
	}
	// Saturn-V has no authored antenna, but as a defaulted probe it gets a
	// basic telemetry antenna so it can be reached (else uncommandable).
	sat := NewFromLoadout(LoadoutSaturnVID)
	if sat.AntennaKind != AntennaDirect || sat.AntennaRangeM != DefaultProbeAntennaRangeM {
		t.Errorf("Saturn-V antenna = %q/%g, want direct/%g (defaulted probe telemetry)", sat.AntennaKind, sat.AntennaRangeM, DefaultProbeAntennaRangeM)
	}
	// v0.24: every vessel carries a basic antenna so it appears on the
	// CommNet — including crewed pods (cosmetic for them, since crew are
	// never comms-gated, but the network model stays uniform).
	cap := NewFromLoadout(LoadoutCapsuleID)
	if cap.AntennaKind != AntennaDirect || cap.AntennaRangeM != DefaultProbeAntennaRangeM {
		t.Errorf("crewed Capsule antenna = %q/%g, want direct/%g (all vessels get a basic antenna)", cap.AntennaKind, cap.AntennaRangeM, DefaultProbeAntennaRangeM)
	}
}

// TestAllVesselsCarryAntenna (v0.24): every catalog loadout — crewed pods
// included — comes out with an antenna, so it shows on the CommNet and a
// probe can reach a ground station. Crewed pods used to ship antenna-less
// (they declare their own command_source, so the old EnsureCommandSource
// short-circuited before the antenna backfill).
func TestAllVesselsCarryAntenna(t *testing.T) {
	for _, id := range LoadoutOrder {
		v := NewFromLoadout(id)
		if v == nil {
			t.Fatalf("NewFromLoadout(%q) = nil", id)
		}
		if v.AntennaKind == AntennaNone || v.AntennaRangeM <= 0 {
			t.Errorf("loadout %q has no antenna (%q/%g); every vessel should carry one", id, v.AntennaKind, v.AntennaRangeM)
		}
	}
}

// TestEnsureCommandSourceDefaulting (C2-1): EnsureCommandSource stamps the
// surviving core of a command-less vessel — crewed for a crewed-pod role,
// else probe — and is a no-op once any stage is a command source.
func TestEnsureCommandSourceDefaulting(t *testing.T) {
	// Command-less stack, generic role → top stage defaults to probe.
	c := &Spacecraft{Role: "launch-vehicle", Stages: []Stage{
		{Name: "booster", DryMass: 1000},
		{Name: "core", DryMass: 500},
	}}
	EnsureCommandSource(c)
	if c.Stages[1].CommandSource != CommandProbe {
		t.Errorf("top stage command source = %q, want probe", c.Stages[1].CommandSource)
	}
	if c.Stages[0].CommandSource != CommandNone {
		t.Errorf("bottom stage should stay none, got %q", c.Stages[0].CommandSource)
	}

	// Crewed-pod role → defaults to crewed, and (v0.24) also gets a basic
	// antenna so it appears on the network.
	crew := &Spacecraft{Role: "capsule", Stages: []Stage{{Name: "pod", DryMass: 500}}}
	EnsureCommandSource(crew)
	if crew.Stages[0].CommandSource != CommandCrewed {
		t.Errorf("crewed-pod default = %q, want crewed", crew.Stages[0].CommandSource)
	}
	if crew.Stages[0].AntennaKind != AntennaDirect {
		t.Errorf("crewed-pod antenna = %q, want a defaulted direct antenna", crew.Stages[0].AntennaKind)
	}

	// Already has a command source → command-source defaulting is a no-op
	// (does not overwrite or add a second). The antenna backfill still runs,
	// independently: this probe declares a command source but no antenna, so
	// it would be uncommandable without one — the core gets a basic antenna.
	existing := &Spacecraft{Role: "custom", Stages: []Stage{
		{Name: "probe", DryMass: 100, CommandSource: CommandProbe},
		{Name: "tank", DryMass: 200},
	}}
	EnsureCommandSource(existing)
	if existing.Stages[1].CommandSource != CommandNone {
		t.Errorf("command-source no-op expected, but top stage got %q", existing.Stages[1].CommandSource)
	}
	if existing.Stages[len(existing.Stages)-1].AntennaKind != AntennaDirect {
		t.Errorf("a command-source craft with no antenna should get a basic one backfilled, got %q", existing.Stages[len(existing.Stages)-1].AntennaKind)
	}
}

// TestJettisonedStageIsDebris (C2-1): a craft built directly from
// command-less stages WITHOUT the construction defaulting (the
// buildJettisonedCraft path) derives Controllable=false — a spent booster
// is passive debris, not a vessel.
func TestJettisonedStageIsDebris(t *testing.T) {
	debris := &Spacecraft{Role: "launch-vehicle", Stages: []Stage{
		{Name: "S-IC", DryMass: 130000},
	}}
	debris.SyncFields() // no EnsureCommandSource — mirrors buildJettisonedCraft
	if debris.Controllable {
		t.Error("a jettisoned command-less booster should be debris (Controllable=false)")
	}
	if debris.Crewed {
		t.Error("debris should not read as crewed")
	}
}
