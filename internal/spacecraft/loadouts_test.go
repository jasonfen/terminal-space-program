package spacecraft

import (
	"math"
	"testing"
)

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
// Saturn-V, SLS, Falcon-9, Apollo-Stack) must carry a non-zero
// LaunchSpriteRowsPx so the chase-cam launch view renders the
// composed braille silhouette, not a single fallback glyph. Catalog-
// side coverage lives in TestStageCatalogShape.
func TestLoadoutsStagesHaveLaunchSprites(t *testing.T) {
	for _, id := range LoadoutOrder {
		l := Loadouts[id]
		for i, s := range l.Stages {
			if s.LaunchSpriteRowsPx <= 0 {
				t.Errorf("loadout %q stage %d (%q): LaunchSpriteRowsPx = %d, want > 0",
					id, i, s.Name, s.LaunchSpriteRowsPx)
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
		// v0.11.3 Slice 4: every catalog part ships a non-zero
		// LaunchSpriteRowsPx so the chase-cam launch view renders
		// the rocket as a per-stage braille silhouette rather than
		// the single-glyph fallback. (Pivoted from ASCII content
		// after the v0.11.3 playtest — braille pixels don't smear
		// at gravity-turn angles.)
		if st.LaunchSpriteRowsPx <= 0 {
			t.Errorf("catalog stage %q: LaunchSpriteRowsPx = %d, want > 0", id, st.LaunchSpriteRowsPx)
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

// TestLanderModuleIsTwoStages — v0.13: the configurator's single "lander"
// pick expands (via BuildModule) to the 2-stage Apollo-LM as one vessel:
// Descent (bottom, legs + soft-land) + Ascent (top, its own engine). The
// descent/ascent are NOT separate picker entries — the player adds the LM
// in one pick. Every other catalog id stays a single-stage module.
func TestLanderModuleIsTwoStages(t *testing.T) {
	// The descent/ascent split is intentionally absent from the picker.
	for _, id := range StageCatalogOrder {
		if id == StageModuleLanderDescentID || id == StageModuleLanderAscentID {
			t.Errorf("StageCatalogOrder exposes %q as a separate pick; the lander module should bundle it", id)
		}
	}

	stages, ok := BuildModule(StageModuleLanderID)
	if !ok || len(stages) != 2 {
		t.Fatalf("BuildModule(lander) = (%d stages, ok=%v), want 2", len(stages), ok)
	}
	d, a := stages[0], stages[1] // bottom → top
	if d.Name != "Descent" || !d.CanSoftLand || !d.LaunchSpriteHasLegs {
		t.Errorf("module bottom = %q (soft-land=%v legs=%v), want Descent with legs+soft-land", d.Name, d.CanSoftLand, d.LaunchSpriteHasLegs)
	}
	if a.Name != "Ascent" || a.Thrust <= 0 {
		t.Errorf("module top = %q (thrust=%.0f), want Ascent with its own engine", a.Name, a.Thrust)
	}

	// A normal part stays a single-stage module.
	one, ok := BuildModule(StageModuleSICID)
	if !ok || len(one) != 1 || one[0].Name != "S-IC" {
		t.Errorf("BuildModule(s-ic) = %d stages, want 1 (S-IC)", len(one))
	}
	if _, ok := BuildModule("not-a-real-part"); ok {
		t.Error("BuildModule accepted an unknown id")
	}
}

// TestApolloCSMLMCompositeModule — v0.14 / ADR 0011. The "csm-lm" pick
// expands to the post-transposition stack [SM, CM, Descent, Ascent]
// bottom-first (SM the firing core), and ModuleNosePayloadTop reports 2
// so the spawn path docks the LM (top 2) as a nose payload. The module
// is offered in the configurator; its split sub-stages are not.
func TestApolloCSMLMCompositeModule(t *testing.T) {
	stages, ok := BuildModule(StageModuleApolloCSMLMID)
	if !ok || len(stages) != 4 {
		t.Fatalf("BuildModule(csm-lm) = (%d stages, ok=%v), want 4", len(stages), ok)
	}
	wantNames := []string{"SM", "CM", "Descent", "Ascent"}
	for i, want := range wantNames {
		if stages[i].Name != want {
			t.Errorf("stage[%d] = %q, want %q (bottom-first SM core + LM on top)", i, stages[i].Name, want)
		}
	}
	if stages[0].Thrust <= 0 {
		t.Errorf("SM (Stages[0]) thrust = %.0f, want > 0 (it is the firing core)", stages[0].Thrust)
	}
	if top := ModuleNosePayloadTop(StageModuleApolloCSMLMID); top != 2 {
		t.Errorf("ModuleNosePayloadTop(csm-lm) = %d, want 2 (the LM rides on the nose)", top)
	}
	// Non-composite modules carry no nose payload.
	if top := ModuleNosePayloadTop(StageModuleLanderID); top != 0 {
		t.Errorf("ModuleNosePayloadTop(lander) = %d, want 0 (linear pick)", top)
	}

	// The composite is offered as a single pick; its split halves are not
	// separate configurator entries.
	offered := false
	for _, id := range StageCatalogOrder {
		switch id {
		case StageModuleApolloCSMLMID:
			offered = true
		case StageModuleServiceModuleID, StageModuleCommandModuleID:
			t.Errorf("StageCatalogOrder exposes %q separately; the csm-lm module should bundle it", id)
		}
	}
	if !offered {
		t.Error("StageCatalogOrder does not offer the csm-lm composite pick")
	}
}

// TestApolloStackShape — the multi-tier loadout: 7 stages bottom-first
// [S-IC, S-II, S-IVB, Descent, Ascent, SM, CM] (v0.12 ADR 0009 split the
// fused CSM into a propulsive Service Module + a passive Command Module),
// lift-off TWR > 1 at sea-level g with the full payload. SM+CM dry mass
// equals the pre-split CSM dry, so the split is mass-neutral and TWR is
// unchanged. The DecouplePlan [1,1,1,2] drops the three Saturn stages one
// at a time, then releases the LM (Descent + Ascent) as a single 2-stage
// craft — so the canonical manual flip (drop LM, slew, dock) keeps the
// lander intact; the one-shot transpose key (D) is the alternative that
// reorders the LM to a docked nose payload (ADR 0009).
func TestApolloStackShape(t *testing.T) {
	l, ok := Loadouts[LoadoutApolloStackID]
	if !ok {
		t.Fatal("Apollo-Stack loadout missing")
	}
	wantNames := []string{"S-IC", "S-II", "S-IVB", "Descent", "Ascent", "SM", "CM"}
	if len(l.Stages) != len(wantNames) {
		t.Fatalf("Apollo-Stack: %d stages, want %d", len(l.Stages), len(wantNames))
	}
	for i, n := range wantNames {
		if l.Stages[i].Name != n {
			t.Errorf("stage %d: name %q, want %q", i, l.Stages[i].Name, n)
		}
	}
	wantPlan := []int{1, 1, 1, 2}
	if len(l.DecouplePlan) != len(wantPlan) {
		t.Fatalf("Apollo-Stack DecouplePlan = %v, want %v", l.DecouplePlan, wantPlan)
	}
	for i, g := range wantPlan {
		if l.DecouplePlan[i] != g {
			t.Errorf("DecouplePlan[%d] = %d, want %d", i, l.DecouplePlan[i], g)
		}
	}
	// Stage-by-name lookups for the property assertions below.
	byName := map[string]Stage{}
	for _, s := range l.Stages {
		byName[s.Name] = s
	}
	// SM: the propulsive Service Module — SPS engine, trimmed SPS fuel,
	// no parachute (the CM carries the chute, not the SM).
	sm := byName["SM"]
	if sm.Thrust != 91000 || sm.Isp != 314 {
		t.Errorf("SM engine: Thrust=%.0f Isp=%.0f, want 91000/314", sm.Thrust, sm.Isp)
	}
	if sm.FuelMass != 16000 {
		t.Errorf("SM SPS fuel = %.0f, want 16000 (ADR 0009 trim)", sm.FuelMass)
	}
	if sm.HasParachute {
		t.Error("SM must not carry a parachute (only the CM does)")
	}
	if sm.LaunchSpriteWidthPx < 2 {
		t.Errorf("SM LaunchSpriteWidthPx = %d, want >= 2 (engine-bell render)", sm.LaunchSpriteWidthPx)
	}
	// CM: the passive Command Module — engineless, parachute recovery.
	cm := byName["CM"]
	if cm.Thrust != 0 || cm.FuelMass != 0 {
		t.Errorf("CM should be engineless: Thrust=%.0f Fuel=%.0f", cm.Thrust, cm.FuelMass)
	}
	if !cm.HasParachute {
		t.Error("CM must carry a parachute (ADR 0008 recovery model)")
	}
	if cm.CanSoftLand {
		t.Error("CM has no engine landing route — CanSoftLand must be false")
	}
	// Locked LM trim (ADR 0009 table).
	if d := byName["Descent"]; d.FuelMass != 6310 {
		t.Errorf("Descent fuel = %.0f, want 6310 (ADR 0009 trim)", d.FuelMass)
	}
	if a := byName["Ascent"]; a.FuelMass != 1269 {
		t.Errorf("Ascent fuel = %.0f, want 1269 (ADR 0009 trim)", a.FuelMass)
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

// TestLanderStageDeltaVBudgets — v0.12 Slice 2 regression. The 2-stage
// Lander must carry enough propellant to actually land and return: the
// descent stage fires the full powered descent hauling the ascent as
// payload, so its descent-burn Δv (rocket equation over the whole
// stack) must clear a lunar descent; the ascent stage's Δv must clear
// a lunar-orbit return. The original split (descent fuel 6000) gave
// only ~2.1 km/s and ran dry mid-landing — this pins the rebalance so
// a future tweak can't silently starve the descent again.
func TestLanderStageDeltaVBudgets(t *testing.T) {
	l := LookupLoadout(LoadoutLanderID)
	if len(l.Stages) != 2 {
		t.Fatalf("Lander should be 2-stage, got %d", len(l.Stages))
	}
	descent, ascent := l.Stages[0], l.Stages[1]

	// Full-stack mass before/after the descent burn.
	m0 := SumDryMass(l.Stages) + SumFuelMass(l.Stages)
	m1 := m0 - descent.FuelMass // descent fuel spent
	descentDV := descent.Isp * g0 * math.Log(m0/m1)
	// Ascent stage alone, after the descent stage is shed.
	ascentDV := ascent.Isp * g0 * math.Log((ascent.DryMass+ascent.FuelMass)/ascent.DryMass)

	// Lunar descent from low orbit needs ~2.0 km/s + gravity/hover
	// losses; require comfortable margin. Lunar ascent-to-orbit ~1.9 km/s.
	const minDescentDV = 2600.0
	const minAscentDV = 2000.0
	if descentDV < minDescentDV {
		t.Errorf("descent-burn Δv = %.0f m/s, want ≥ %.0f (would run dry mid-landing)", descentDV, minDescentDV)
	}
	if ascentDV < minAscentDV {
		t.Errorf("ascent Δv = %.0f m/s, want ≥ %.0f (can't reach lunar orbit)", ascentDV, minAscentDV)
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
