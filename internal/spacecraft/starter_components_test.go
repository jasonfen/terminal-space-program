package spacecraft

import (
	"math"
	"reflect"
	"testing"
)

// TestStarterComponentsAggregateCleanly — every embedded component is
// individually valid (composes into a one-component stage with no warning),
// carries the fields its kind needs, and each of the five kinds ships a
// selection (engines/tanks have several so clustering + tank-sizing are real
// choices). Guards the v0.24 starter content against a malformed addition.
func TestStarterComponentsAggregateCleanly(t *testing.T) {
	comps, _, _, err := loadEmbeddedCatalog()
	if err != nil {
		t.Fatalf("loadEmbeddedCatalog: %v", err)
	}
	byKind := map[string]int{}
	for id, c := range comps {
		byKind[c.Kind]++
		if _, warn := ComposeStage([]string{id}, comps); warn != "" {
			t.Errorf("component %q does not aggregate cleanly: %s", id, warn)
		}
		if c.DryMassKg <= 0 {
			t.Errorf("component %q has non-positive dry mass", id)
		}
		switch c.Kind {
		case ComponentEngine:
			if c.ThrustN <= 0 || c.IspS <= 0 || c.FuelType == "" {
				t.Errorf("engine %q missing thrust/Isp/fuel: %+v", id, c)
			}
		case ComponentTank:
			if c.FuelCapacityKg <= 0 || c.FuelType == "" {
				t.Errorf("tank %q missing capacity/fuel: %+v", id, c)
			}
		case ComponentCommandCore:
			if !IsCommandSource(c.CommandSource) {
				t.Errorf("command-core %q has no command source: %+v", id, c)
			}
		case ComponentAntenna:
			if c.AntennaKind == AntennaNone || c.RangeM <= 0 {
				t.Errorf("antenna %q missing kind/range: %+v", id, c)
			}
		case ComponentStructure:
			// dry mass only — already checked above.
		default:
			t.Errorf("component %q has unknown kind %q", id, c.Kind)
		}
	}
	for _, k := range []string{ComponentEngine, ComponentTank, ComponentCommandCore, ComponentAntenna, ComponentStructure} {
		if byKind[k] == 0 {
			t.Errorf("no components of kind %q ship", k)
		}
	}
	if byKind[ComponentEngine] < 3 || byKind[ComponentTank] < 3 {
		t.Errorf("want a real selection, got %d engines / %d tanks", byKind[ComponentEngine], byKind[ComponentTank])
	}
}

// TestStarterEngineClusterThrustWeighted — two different same-chemistry
// engines from the catalog cluster honestly: thrust adds and the effective
// Isp is the thrust-weighted blend (the property the spread of engines makes
// meaningful).
func TestStarterEngineClusterThrustWeighted(t *testing.T) {
	comps, _, _, err := loadEmbeddedCatalog()
	if err != nil {
		t.Fatalf("loadEmbeddedCatalog: %v", err)
	}
	a, b := comps["rd180-booster"], comps["merlin-sustainer"] // both kerolox, different Isp
	st, warn := ComposeStage([]string{"rd180-booster", "merlin-sustainer"}, comps)
	if warn != "" {
		t.Fatalf("kerolox cluster should aggregate: %s", warn)
	}
	if st.Thrust != a.ThrustN+b.ThrustN {
		t.Errorf("cluster thrust = %g, want %g", st.Thrust, a.ThrustN+b.ThrustN)
	}
	wantIsp := (a.ThrustN + b.ThrustN) / (a.ThrustN/a.IspS + b.ThrustN/b.IspS)
	if math.Abs(st.Isp-wantIsp) > 1e-6 {
		t.Errorf("cluster Isp_eff = %g, want %g (thrust-weighted)", st.Isp, wantIsp)
	}
}

// TestProbeSatComposedFromComponents — the v0.24 starter dogfood (ADR 0029
// S4): the embedded "Probe-Sat" loadout is built from a composed part
// (probe-sat-stage → probe-engine + probe-tank-1k + probe-core +
// probe-antenna) and must fly INDISTINGUISHABLY from an equivalent atomic
// part with the same summed stats. Proves the components→part→loadout→spawn
// chain end-to-end, one level below the ADR 0026 modding proof.
func TestProbeSatComposedFromComponents(t *testing.T) {
	c := NewFromLoadout("Probe-Sat")
	if c == nil || len(c.Stages) != 1 {
		t.Fatalf("Probe-Sat is not a flyable single-stage craft: %+v", c)
	}
	got := c.Stages[0]

	// Hand-summed aggregate of the four starter components:
	//   dry  = 200 + 100 + 80 + 20 = 400
	//   fuel = 1000 (the one tank, full)
	//   thrust 50 kN, Isp 320 (single engine → passthrough)
	//   probe command source + a basic direct antenna (1e7 m).
	if got.DryMass != 400 {
		t.Errorf("dry mass = %g, want 400 (Σ component dry)", got.DryMass)
	}
	if got.FuelMass != 1000 || got.FuelCapacity != 1000 {
		t.Errorf("fuel = %g/%g, want 1000/1000 (full tank)", got.FuelMass, got.FuelCapacity)
	}
	if got.Thrust != 50000 || got.Isp != 320 {
		t.Errorf("engine = %g N / %g s, want 50000/320", got.Thrust, got.Isp)
	}
	if got.CommandSource != CommandProbe {
		t.Errorf("command source = %q, want probe", got.CommandSource)
	}
	if got.AntennaKind != AntennaDirect || got.AntennaRangeM != 1e7 {
		t.Errorf("antenna = %q @ %g, want direct @ 1e7", got.AntennaKind, got.AntennaRangeM)
	}

	// Build the atomic equivalent and compare the resolved runtime Stage —
	// they must be byte-identical (RCS derived from the same dry mass).
	atomic := Part{
		ID:             "probe-sat-atomic",
		Name:           "Probe Sat",
		Glyph:          VesselGlyph,
		Color:          "#5BB3FF",
		DryMassKg:      400,
		FuelMassKg:     1000,
		FuelCapacityKg: 1000,
		ThrustN:        50000,
		IspS:           320,
		FuelType:       FuelTypeKerolox, // derived up from the engine/tank chemistry
		CommandSource:  CommandProbe,
		Antenna:        &Antenna{Kind: AntennaDirect, RangeM: 1e7},
		// Visual fields the composed part also carries.
		LaunchSpriteRowsPx:  6,
		LaunchSpriteWidthPx: 2,
		LaunchSpriteColor:   "#5BB3FF",
	}
	def := LoadoutDef{ID: "Probe-Sat-Atomic", Name: "Probe Sat", Role: "probe", Glyph: VesselGlyph, Color: "#5BB3FF", Parts: []PartRef{{PartID: "probe-sat-atomic"}}}
	atomicL := resolveLoadout(def, map[string]Part{"probe-sat-atomic": atomic})

	// Compare the physics-relevant fields (LoadoutID differs by construction).
	want := atomicL.Stages[0]
	want.LoadoutID = got.LoadoutID
	if !reflect.DeepEqual(got, want) {
		t.Errorf("composed stage differs from atomic equivalent:\n composed %+v\n  atomic  %+v", got, want)
	}
}
