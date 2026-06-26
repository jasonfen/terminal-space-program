package spacecraft

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// S1 / ADR 0029 — the component catalog + aggregation spine. A Part may
// declare an optional `components` list; when present its flat scalar
// stats are DERIVED by aggregation (dry mass + tank capacity additive;
// engines combined with thrust-weighted Isp; command-source / antenna
// attributes carried up). Atomic parts (no components) pass through
// byte-identical so the existing fleet flies unchanged.

// testComps builds an in-memory component catalog for the aggregation unit
// tests: two same-chemistry engines of differing Isp, a tank, a structure
// adapter, a probe core, and a relay antenna.
func testComps() map[string]Component {
	return map[string]Component{
		"eng-a": {ID: "eng-a", Kind: ComponentEngine, DryMassKg: 500, ThrustN: 1_000_000, IspS: 300, FuelType: FuelTypeKerolox},
		"eng-b": {ID: "eng-b", Kind: ComponentEngine, DryMassKg: 500, ThrustN: 1_000_000, IspS: 350, FuelType: FuelTypeKerolox},
		"tank":  {ID: "tank", Kind: ComponentTank, DryMassKg: 100, FuelCapacityKg: 2000, FuelType: FuelTypeKerolox},
		"adapt": {ID: "adapt", Kind: ComponentStructure, DryMassKg: 50},
		"probe": {ID: "probe", Kind: ComponentCommandCore, DryMassKg: 80, CommandSource: CommandProbe},
		"crew":  {ID: "crew", Kind: ComponentCommandCore, DryMassKg: 200, CommandSource: CommandCrewed, HasParachute: true},
		"relay": {ID: "relay", Kind: ComponentAntenna, DryMassKg: 30, AntennaKind: AntennaRelay, RangeM: 1e9},
		"dish":  {ID: "dish", Kind: ComponentAntenna, DryMassKg: 10, AntennaKind: AntennaDirect, RangeM: 1e7},
		"hydro": {ID: "hydro", Kind: ComponentTank, DryMassKg: 100, FuelCapacityKg: 2000, FuelType: FuelTypeHydrolox},
	}
}

// TestAggregateComposedPart — a composed part with two same-chemistry
// engines of differing Isp, three tanks, and a structure adapter
// aggregates to the thrust-weighted Isp_eff, summed thrust, summed dry
// mass and summed tank capacity (full tanks by default).
func TestAggregateComposedPart(t *testing.T) {
	parts := map[string]Part{
		"s1": {ID: "s1", Name: "Custom S1", Components: []string{"eng-a", "eng-b", "tank", "tank", "tank", "adapt"}},
	}
	got, warnings := aggregateComponents(parts, testComps())
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	p := got["s1"]
	if p.DryMassKg != 500+500+100*3+50 {
		t.Errorf("DryMassKg = %g, want 1350", p.DryMassKg)
	}
	if p.ThrustN != 2_000_000 {
		t.Errorf("ThrustN = %g, want 2e6", p.ThrustN)
	}
	wantIsp := 2_000_000.0 / (1_000_000.0/300 + 1_000_000.0/350) // ≈ 323.077
	if math.Abs(p.IspS-wantIsp) > 1e-6 {
		t.Errorf("Isp_eff = %g, want %g (thrust-weighted)", p.IspS, wantIsp)
	}
	if p.FuelCapacityKg != 6000 || p.FuelMassKg != 6000 {
		t.Errorf("fuel = %g/%g, want 6000/6000 (full tanks)", p.FuelMassKg, p.FuelCapacityKg)
	}
	if p.FuelType != FuelTypeKerolox {
		t.Errorf("FuelType = %q, want kerolox", p.FuelType)
	}
	// Identity is preserved from the Part, not derived.
	if p.Name != "Custom S1" {
		t.Errorf("Name = %q, want preserved", p.Name)
	}
	// The components list is retained so the design store can re-serialize it.
	if len(p.Components) != 6 {
		t.Errorf("Components list dropped: %v", p.Components)
	}
}

// TestAggregateSingleEnginePassthrough — one engine ⇒ Isp_eff == Isp
// exactly (the degenerate case of the thrust-weighted formula).
func TestAggregateSingleEnginePassthrough(t *testing.T) {
	parts := map[string]Part{"s": {ID: "s", Components: []string{"eng-a", "tank"}}}
	got, warnings := aggregateComponents(parts, testComps())
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if got["s"].IspS != 300 {
		t.Errorf("single-engine Isp_eff = %g, want 300 (passthrough)", got["s"].IspS)
	}
	if got["s"].ThrustN != 1_000_000 {
		t.Errorf("ThrustN = %g, want 1e6", got["s"].ThrustN)
	}
}

// TestAggregateCommandAndAntenna — a command-core lifts CommandSource (and
// its parachute flag) onto the part; multiple antennas resolve to the
// longest-ranged one; a crewed core wins over a probe core.
func TestAggregateCommandAndAntenna(t *testing.T) {
	parts := map[string]Part{
		"pod": {ID: "pod", Components: []string{"probe", "crew", "dish", "relay"}},
	}
	got, _ := aggregateComponents(parts, testComps())
	p := got["pod"]
	if p.CommandSource != CommandCrewed {
		t.Errorf("CommandSource = %q, want crewed (crewed wins over probe)", p.CommandSource)
	}
	if !p.HasParachute {
		t.Error("HasParachute not carried up from the crew core")
	}
	if p.Antenna == nil || p.Antenna.Kind != AntennaRelay || p.Antenna.RangeM != 1e9 {
		t.Errorf("Antenna = %+v, want relay@1e9 (longest range)", p.Antenna)
	}
}

// TestAggregateMixedFuelError — engines/tanks of differing chemistry in one
// stage is rejected (a catalog warning); single fuel type per stage is the
// invariant that keeps the single-pool runtime exact.
func TestAggregateMixedFuelError(t *testing.T) {
	parts := map[string]Part{
		"bad": {ID: "bad", Components: []string{"eng-a", "hydro"}}, // kerolox engine + hydrolox tank
	}
	_, warnings := aggregateComponents(parts, testComps())
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1 (mixed chemistry)", len(warnings))
	}
}

// TestAggregateUnknownComponent — a composed part referencing a missing
// component is a catalog warning, not a panic (skip-bad-with-warning).
func TestAggregateUnknownComponent(t *testing.T) {
	parts := map[string]Part{"x": {ID: "x", Components: []string{"eng-a", "nope"}}}
	_, warnings := aggregateComponents(parts, testComps())
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1 (unknown component)", len(warnings))
	}
}

// TestAggregateAtomicPartsUnchanged — the byte-identical golden: every
// embedded atomic part (none declare components this cycle) passes through
// aggregation unchanged, so the existing fleet flies identically.
func TestAggregateAtomicPartsUnchanged(t *testing.T) {
	comps, parts, _, err := loadEmbeddedCatalog()
	if err != nil {
		t.Fatalf("loadEmbeddedCatalog: %v", err)
	}
	got, warnings := aggregateComponents(parts, comps)
	if len(warnings) != 0 {
		t.Fatalf("embedded aggregation warnings = %v, want none", warnings)
	}
	for id, p := range parts {
		if len(p.Components) != 0 {
			continue // composed — expected to change
		}
		if !reflect.DeepEqual(p, got[id]) {
			t.Errorf("atomic part %q changed under aggregation:\n raw %+v\n got %+v", id, p, got[id])
		}
	}
}

// TestComponentRoundTrip — a Component (every kind's fields) and a Catalog
// carrying components round-trip through JSON byte-for-byte.
func TestComponentRoundTrip(t *testing.T) {
	want := Catalog{
		Components: []Component{
			{ID: "m-eng", Name: "Merlin", Glyph: "v", Color: "#FF8C42", Kind: ComponentEngine, DryMassKg: 470, ThrustN: 845_000, IspS: 282, FuelType: FuelTypeKerolox},
			{ID: "t-2k", Name: "2t tank", Kind: ComponentTank, DryMassKg: 200, FuelCapacityKg: 2000, FuelType: FuelTypeKerolox},
			{ID: "core", Name: "Probe core", Kind: ComponentCommandCore, DryMassKg: 80, CommandSource: CommandProbe, CanSoftLand: true},
			{ID: "ant", Name: "Relay", Kind: ComponentAntenna, DryMassKg: 30, AntennaKind: AntennaRelay, RangeM: 1e9},
			{ID: "ada", Name: "Adapter", Kind: ComponentStructure, DryMassKg: 40},
		},
		Parts: []Part{{ID: "composed", Name: "Composed", Components: []string{"m-eng", "t-2k", "core"}}},
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

// TestComposedPartViaOverlay — the full modding path one level deeper: a
// user overlay file adds components AND a composed part referencing them,
// then a loadout referencing that composed part. Through LoadCatalogWithWarnings
// (which aggregates) the composed part resolves to flyable stats and
// NewFromStages-equivalent thrust, and its probe core + antenna ride up.
func TestComposedPartViaOverlay(t *testing.T) {
	dir := withUserCatalog(t)
	overlay := `{
		"components": [
			{"id":"c-eng","kind":"engine","dry_mass_kg":500,"thrust_n":1000000,"isp_s":320,"fuel_type":"kerolox"},
			{"id":"c-tank","kind":"tank","dry_mass_kg":100,"fuel_capacity_kg":4000,"fuel_type":"kerolox"},
			{"id":"c-core","kind":"command-core","dry_mass_kg":90,"command_source":"probe"},
			{"id":"c-ant","kind":"antenna","dry_mass_kg":20,"antenna_kind":"direct","range_m":10000000}
		],
		"parts": [
			{"id":"c-stage","name":"Composed Stage","glyph":"➤","color":"#AABBCC","components":["c-eng","c-tank","c-core","c-ant"]}
		],
		"loadouts": [
			{"id":"Composed-Rocket","name":"Composed Rocket","role":"custom","glyph":"➤","color":"#AABBCC","parts":[{"part_id":"c-stage"}]}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "composed.json"), []byte(overlay), 0o644); err != nil {
		t.Fatal(err)
	}
	parts, defs, warnings, err := LoadCatalogWithWarnings()
	if err != nil {
		t.Fatalf("LoadCatalogWithWarnings: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	cs, ok := parts["c-stage"]
	if !ok {
		t.Fatal("composed part not in merged catalog")
	}
	if cs.ThrustN != 1_000_000 || cs.IspS != 320 {
		t.Errorf("composed part engine = %g N / %g s, want 1e6/320", cs.ThrustN, cs.IspS)
	}
	if cs.DryMassKg != 500+100+90+20 {
		t.Errorf("composed dry = %g, want 710", cs.DryMassKg)
	}
	if cs.FuelCapacityKg != 4000 || cs.FuelMassKg != 4000 {
		t.Errorf("composed fuel = %g/%g, want 4000/4000", cs.FuelMassKg, cs.FuelCapacityKg)
	}
	if cs.CommandSource != CommandProbe {
		t.Errorf("composed CommandSource = %q, want probe", cs.CommandSource)
	}
	if cs.Antenna == nil || cs.Antenna.Kind != AntennaDirect {
		t.Errorf("composed antenna = %+v, want direct", cs.Antenna)
	}
	// The composed part resolves into a flyable loadout (one stage,
	// thrust carried through ToStage exactly like an atomic part).
	var def *LoadoutDef
	for i := range defs {
		if defs[i].ID == "Composed-Rocket" {
			def = &defs[i]
		}
	}
	if def == nil {
		t.Fatal("composed loadout not merged")
	}
	l := resolveLoadout(*def, parts)
	if len(l.Stages) != 1 || l.Stages[0].Thrust != 1_000_000 || l.Stages[0].Isp != 320 {
		t.Errorf("resolved stage = %+v, want one stage 1e6 N / 320 s", l.Stages)
	}
	if l.Stages[0].CommandSource != CommandProbe {
		t.Errorf("resolved stage CommandSource = %q, want probe", l.Stages[0].CommandSource)
	}
}

// TestComponentsGlobalLoadsEmbedded — the package Components global resolves
// from the embedded components.json at init (mirrors Loadouts / StageCatalog),
// and the embedded stub loads cleanly.
func TestComponentsGlobalLoadsEmbedded(t *testing.T) {
	if Components == nil {
		t.Fatal("Components global is nil, want non-nil map")
	}
}
