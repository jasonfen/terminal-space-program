package spacecraft

import (
	"reflect"
	"testing"
)

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
