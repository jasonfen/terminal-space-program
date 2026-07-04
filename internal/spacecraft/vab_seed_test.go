package spacecraft

import "testing"

// S2 / ADR 0032 §6-§7 — the crack-open seed data model: vab_seed is
// seed-only (never aggregated into runtime stats), the two bulk tanks exist,
// and every authored seed references a real component.

// TestVabSeedNeverDrivesStats — a Part carrying a VabSeed but no Components is
// atomic: the aggregation pass (the whole-catalog load path) passes its inline
// scalars through untouched, so a seed can never leak into runtime stats /
// loadouts / budget evals (§6). VabSeed is read only by the VAB crack path.
func TestVabSeedNeverDrivesStats(t *testing.T) {
	p := Part{
		ID:        "seed-probe",
		DryMassKg: 1234,
		ThrustN:   99000,
		IspS:      321,
		FuelType:  FuelTypeKerolox,
		VabSeed:   []string{"f1-booster", "kero-tank-100k"}, // would aggregate very differently
	}
	out, warns := aggregateComponents(map[string]Part{p.ID: p}, Components)
	if len(warns) != 0 {
		t.Fatalf("aggregation warned on a seed-only part: %v", warns)
	}
	got := out[p.ID]
	if got.DryMassKg != 1234 || got.ThrustN != 99000 || got.IspS != 321 {
		t.Errorf("VabSeed leaked into stats: dry=%g thrust=%g isp=%g, want 1234/99000/321",
			got.DryMassKg, got.ThrustN, got.IspS)
	}
}

// TestSeededPartsKeepAuthoredStats — the shipped seeded parts still expose
// their AUTHORED inline stats via BuildStage, not their seed's aggregate
// (which is deliberately close-but-not-equal — the delta the crack flash
// shows). Proves adding vab_seed to parts.json changed no runtime number.
func TestSeededPartsKeepAuthoredStats(t *testing.T) {
	cases := []struct {
		id           string
		thrust, fuel float64
	}{
		{"s-ivb", 1023000, 109000}, // seed aggregates to 1033000 / 100000
		{"s-ic", 35100000, 2160000},
		{"csm", 91000, 18400},
	}
	for _, c := range cases {
		st, ok := BuildStage(c.id)
		if !ok {
			t.Fatalf("BuildStage(%q) not found", c.id)
		}
		if st.Thrust != c.thrust || st.FuelMass != c.fuel {
			t.Errorf("%s: thrust=%g fuel=%g, want authored %g/%g (seed must not drive stats)",
				c.id, st.Thrust, st.FuelMass, c.thrust, c.fuel)
		}
	}
}

// TestBulkTanksExist — the two §7 bulk tanks are in the catalog with the
// right chemistry and capacity so from-scratch heavy lifters are practical.
func TestBulkTanksExist(t *testing.T) {
	cases := []struct {
		id   string
		cap  float64
		fuel string
	}{
		{"kero-tank-100k", 100000, FuelTypeKerolox},
		{"hydro-tank-25k", 25000, FuelTypeHydrolox},
	}
	for _, c := range cases {
		comp, ok := Components[c.id]
		if !ok {
			t.Fatalf("bulk tank %q missing from Components", c.id)
		}
		if comp.Kind != ComponentTank || comp.FuelCapacityKg != c.cap || comp.FuelType != c.fuel {
			t.Errorf("%s = %+v, want tank cap %g fuel %s", c.id, comp, c.cap, c.fuel)
		}
	}
}

// TestSeededPartsResolve — every component ID in every authored seed exists in
// the catalog (catches a typo in a seed list), the seed composes without a
// mixed-chemistry error, and the 10 §7 palette parts all carry a seed.
func TestSeededPartsResolve(t *testing.T) {
	want := []string{"s-ic", "srb", "f9-s1", "core-rs25", "s-ii", "s-ivb", "f9-s2", "icps", "csm", "rcs-tug"}
	for _, id := range want {
		m, ok := StageCatalog[id]
		if !ok {
			t.Errorf("palette part %q missing from StageCatalog", id)
			continue
		}
		if len(m.VabSeed) == 0 {
			t.Errorf("palette part %q has no vab_seed (ADR 0032 §7 wants all 10 seeded)", id)
			continue
		}
		for _, cid := range m.VabSeed {
			if _, ok := Components[cid]; !ok {
				t.Errorf("%s seed references unknown component %q", id, cid)
			}
		}
		if _, warn := ComposeStage(m.VabSeed, Components); warn != "" {
			t.Errorf("%s seed does not compose cleanly: %s", id, warn)
		}
	}
}
