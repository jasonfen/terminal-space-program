package spacecraft

import "testing"

// TestLoadoutsCatalogShape — every entry in LoadoutOrder must map
// to a non-empty Loadouts entry, and each entry must have non-empty
// ID / Name / Glyph / Color so the HUD render path can rely on them.
func TestLoadoutsCatalogShape(t *testing.T) {
	for _, id := range LoadoutOrder {
		l, ok := Loadouts[id]
		if !ok {
			t.Errorf("LoadoutOrder references %q but Loadouts has no entry", id)
			continue
		}
		if l.ID != id {
			t.Errorf("loadout %q: ID field = %q, mismatched", id, l.ID)
		}
		if l.Name == "" || l.Glyph == "" || l.Color == "" {
			t.Errorf("loadout %q: empty visual field — Name=%q Glyph=%q Color=%q",
				id, l.Name, l.Glyph, l.Color)
		}
	}
}

// TestLoadoutsStagesHaveLaunchSprites — v0.11.3 Slice 4 parity: every
// stage of every canonical loadout (S-IVB-1, ICPS, RCS-Tug, Lander,
// Saturn-V, SLS, Falcon-9, Apollo-Stack) must carry a LaunchSprite so
// the chase-cam launch view renders the composed stack, not a single
// fallback glyph. Catalog-side coverage lives in TestStageCatalogShape.
func TestLoadoutsStagesHaveLaunchSprites(t *testing.T) {
	for _, id := range LoadoutOrder {
		l := Loadouts[id]
		for i, s := range l.Stages {
			if s.LaunchSprite == "" {
				t.Errorf("loadout %q stage %d (%q): empty LaunchSprite",
					id, i, s.Name)
			}
		}
	}
}

// TestLookupLoadoutFallback — empty / unknown IDs should fall back
// to the S-IVB-1 default so legacy saves don't break.
func TestLookupLoadoutFallback(t *testing.T) {
	l := LookupLoadout("")
	if l.ID != LoadoutSIVB1ID {
		t.Errorf("empty ID resolved to %q, want %q", l.ID, LoadoutSIVB1ID)
	}
	l = LookupLoadout("not-a-real-loadout")
	if l.ID != LoadoutSIVB1ID {
		t.Errorf("unknown ID resolved to %q, want %q", l.ID, LoadoutSIVB1ID)
	}
}

// TestNewFromLoadoutPopulatesAll — NewFromLoadout must set propulsion
// numbers + visual fields + RCS pool from the catalog entry. Caller
// is still responsible for Primary + State.
func TestNewFromLoadoutPopulatesAll(t *testing.T) {
	c := NewFromLoadout(LoadoutICPSID)
	if c.LoadoutID != LoadoutICPSID {
		t.Errorf("LoadoutID = %q, want %q", c.LoadoutID, LoadoutICPSID)
	}
	if c.Glyph == "" || c.Color == "" {
		t.Error("Glyph / Color not populated from loadout")
	}
	if c.MonopropCapacity <= 0 {
		t.Error("RCS pool not populated")
	}
	if c.Throttle != 1.0 {
		t.Errorf("Throttle = %v, want 1.0 (default full)", c.Throttle)
	}
}

// TestPureRCSTugHasNoMainEngine — the RCS-tug loadout is monoprop-
// only; main Thrust / Isp must be zero so manual-burn paths cleanly
// no-op.
func TestPureRCSTugHasNoMainEngine(t *testing.T) {
	c := NewFromLoadout(LoadoutRCSTugID)
	if c.Thrust != 0 || c.Isp != 0 {
		t.Errorf("RCS-tug has main engine: Thrust=%v Isp=%v", c.Thrust, c.Isp)
	}
	if c.Fuel != 0 {
		t.Errorf("RCS-tug shipped with main fuel: %v kg", c.Fuel)
	}
}

// --- v0.10.1: stage catalog + multi-tier loadout + NewFromStages ---

// TestStageCatalogShape — every StageCatalogOrder id resolves in
// StageCatalog, BuildStage succeeds with full tanks + visual fields,
// and the net-new CSM payload module is present and engine-capable.
func TestStageCatalogShape(t *testing.T) {
	for _, id := range StageCatalogOrder {
		m, ok := StageCatalog[id]
		if !ok {
			t.Errorf("StageCatalogOrder references %q but StageCatalog has none", id)
			continue
		}
		if m.ID != id || m.Name == "" || m.Glyph == "" || m.Color == "" || m.Tier == "" {
			t.Errorf("catalog %q: incomplete meta %+v", id, m)
		}
		st, built := BuildStage(id)
		if !built {
			t.Errorf("BuildStage(%q) failed", id)
			continue
		}
		if st.LoadoutID != "" {
			t.Errorf("catalog stage %q: LoadoutID = %q, want empty (not a loadout)", id, st.LoadoutID)
		}
		if st.FuelMass != st.FuelCapacity {
			t.Errorf("catalog stage %q: built with non-full tank (%.0f/%.0f)",
				id, st.FuelMass, st.FuelCapacity)
		}
		if st.Name == "" || st.Glyph == "" || st.Color == "" {
			t.Errorf("catalog stage %q: empty visual field on built Stage", id)
		}
		// v0.11.3 Slice 4: every catalog part ships a LaunchSprite so
		// the chase-cam launch view renders the rocket as its actual
		// stack rather than a single glyph.
		if st.LaunchSprite == "" {
			t.Errorf("catalog stage %q: empty LaunchSprite on built Stage", id)
		}
	}
	if _, ok := StageCatalog[StageModuleCSMID]; !ok {
		t.Fatal("CSM module missing from catalog")
	}
	csm, _ := BuildStage(StageModuleCSMID)
	if csm.Thrust <= 0 || csm.Isp <= 0 {
		t.Errorf("CSM should have a maneuvering engine: Thrust=%v Isp=%v", csm.Thrust, csm.Isp)
	}
	if _, ok := BuildStage("not-a-real-part"); ok {
		t.Error("BuildStage accepted an unknown id")
	}
}

// TestApolloStackShape — the v0.10.1 multi-tier loadout: 5 stages
// bottom-first [S-IC, S-II, S-IVB, LM, CSM], lift-off TWR > 1 at
// sea-level g with the full payload, CSM is the surviving core.
func TestApolloStackShape(t *testing.T) {
	l, ok := Loadouts[LoadoutApolloStackID]
	if !ok {
		t.Fatal("Apollo-Stack loadout missing")
	}
	wantNames := []string{"S-IC", "S-II", "S-IVB", "LM", "CSM"}
	if len(l.Stages) != len(wantNames) {
		t.Fatalf("Apollo-Stack: %d stages, want %d", len(l.Stages), len(wantNames))
	}
	for i, n := range wantNames {
		if l.Stages[i].Name != n {
			t.Errorf("stage %d: name %q, want %q", i, l.Stages[i].Name, n)
		}
	}
	totalMass := SumDryMass(l.Stages) + SumFuelMass(l.Stages)
	twr := l.Stages[0].Thrust / (totalMass * g0)
	if twr <= 1.0 {
		t.Errorf("Apollo-Stack lift-off TWR = %.3f, want > 1", twr)
	}
	// Catalog parity: shared parts must match the loadout's tuning so
	// a configurator-built S-IVB flies like the Apollo-Stack S-IVB.
	sivb, _ := BuildStage(StageModuleSIVBID)
	if sivb.Thrust != l.Stages[2].Thrust || sivb.Isp != l.Stages[2].Isp ||
		sivb.DryMass != l.Stages[2].DryMass || sivb.FuelCapacity != l.Stages[2].FuelMass {
		t.Errorf("catalog S-IVB diverges from Apollo-Stack S-IVB tuning")
	}
}

// TestNewFromStages — empty slice → nil; a real stack derives the
// craft identity from the top (core) stage, sums mass via
// SyncFields, and carries an empty LoadoutID (custom, not a
// catalog archetype).
func TestNewFromStages(t *testing.T) {
	if NewFromStages(nil) != nil {
		t.Error("NewFromStages(nil) should be nil (empty stack not spawnable)")
	}
	sic, _ := BuildStage(StageModuleSICID)
	csm, _ := BuildStage(StageModuleCSMID)
	c := NewFromStages([]Stage{sic, csm})
	if c == nil {
		t.Fatal("NewFromStages returned nil for a non-empty stack")
	}
	if c.LoadoutID != "" {
		t.Errorf("custom craft LoadoutID = %q, want empty", c.LoadoutID)
	}
	if c.Name != "CSM" || c.Glyph != csm.Glyph {
		t.Errorf("identity should come from core stage: Name=%q Glyph=%q", c.Name, c.Glyph)
	}
	want := SumDryMass(c.Stages) + SumFuelMass(c.Stages) + SumMonopropMass(c.Stages)
	if c.TotalMass() != want {
		t.Errorf("TotalMass = %.0f, want %.0f (summed via SyncFields)", c.TotalMass(), want)
	}
	// Defensive: NewFromStages must copy, not alias, the input slice.
	c.Stages[0].FuelMass = 0
	if sic.FuelMass == 0 {
		t.Error("NewFromStages aliased the caller's stage slice")
	}
}
