package spacecraft

import "testing"

// ADR 0060 decision 1, the data table pinned part by part. The numbers are
// spelled out here rather than read back from parts.json so the test can
// disagree with the data.
func TestOrionPartsDataTable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	parts, _, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name, cmd, fuelType, desc string
		dry, fuel, thrust, isp    float64
		antenna, chute, port      bool
	}
	want := map[string]row{
		"orion-esm": {"ESM", "", "hypergolic", "8 R-4D auxiliary thrusters, 490 N each",
			6185, 8600, 26600, 316, false, false, false},
		"orion-cm": {"Orion", "crewed", "", "",
			10400, 0, 0, 0, true, true, true},
	}
	for id, w := range want {
		p, ok := parts[id]
		if !ok {
			t.Errorf("%s missing from parts.json", id)
			continue
		}
		if p.Name != w.name || p.Tier != "payload" || p.CommandSource != w.cmd || p.FuelType != w.fuelType {
			t.Errorf("%s: name/tier/command/fuel = %q/%q/%q/%q, want %q/payload/%q/%q", id,
				p.Name, p.Tier, p.CommandSource, p.FuelType, w.name, w.cmd, w.fuelType)
		}
		if p.DryMassKg != w.dry || p.FuelMassKg != w.fuel || p.FuelCapacityKg != w.fuel || p.ThrustN != w.thrust || p.IspS != w.isp {
			t.Errorf("%s: dry/fuel/cap/thrust/isp = %g/%g/%g/%g/%g, want %g/%g/%g/%g/%g", id,
				p.DryMassKg, p.FuelMassKg, p.FuelCapacityKg, p.ThrustN, p.IspS, w.dry, w.fuel, w.fuel, w.thrust, w.isp)
		}
		if p.Description != w.desc {
			t.Errorf("%s: description = %q, want %q", id, p.Description, w.desc)
		}
		if w.antenna {
			if p.Antenna == nil || p.Antenna.Kind != "direct" || p.Antenna.RangeM != 100_000_000 {
				t.Errorf("%s: antenna = %+v, want direct 1e8 m", id, p.Antenna)
			}
		} else if p.Antenna != nil {
			t.Errorf("%s: antenna = %+v, want none", id, p.Antenna)
		}
		if p.HasParachute != w.chute || p.CanSoftLand || p.BallisticCoefficient != 0 {
			t.Errorf("%s: chute/softland/ballistic = %v/%v/%g, want %v/false/0", id, p.HasParachute, p.CanSoftLand, p.BallisticCoefficient, w.chute)
		}
		if p.LaunchSpriteRowsPx != 6 || p.LaunchSpriteWidthPx != 3 {
			t.Errorf("%s: sprite %dx%d, want 6x3", id, p.LaunchSpriteRowsPx, p.LaunchSpriteWidthPx)
		}
		if p.Glyph != "➤" {
			t.Errorf("%s: glyph = %q", id, p.Glyph)
		}
		if w.port {
			if len(p.Ports) != 1 || p.Ports[0].End != PortEndTop {
				t.Errorf("%s: ports = %+v, want [{top}]", id, p.Ports)
			}
		} else if len(p.Ports) != 0 {
			t.Errorf("%s: ports = %+v, want none", id, p.Ports)
		}
	}
}

// ADR 0060 decision 2: the Orion stage adapter rides with the SLS's own ICPS
// only. The separate ICPS upper-stage loadout (icps-1) and the catalog
// module (icps) keep 3,500 kg.
func TestICPSDryMassOnlySLSCarriesAdapter(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	parts, _, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]float64{"icps-sls": 4316, "icps": 3500, "icps-1": 3500} {
		p, ok := parts[id]
		if !ok {
			t.Errorf("%s missing", id)
			continue
		}
		if p.DryMassKg != want {
			t.Errorf("%s dry mass = %g, want %g", id, p.DryMassKg, want)
		}
	}
}

// ADR 0060 decision 1: the two loadouts, and the bare SLS untouched in shape.
func TestOrionLoadouts(t *testing.T) {
	cases := []struct {
		id, name, role, category string
		stageNames               []string
	}{
		{"SLS-Orion", "SLS + Orion", "mission-stack", "mission-stacks", []string{"SRBs", "Core", "ICPS", "ESM", "Orion"}},
		{"Orion", "Orion", "capsule", "landers-capsules", []string{"ESM", "Orion"}},
		{"SLS-Block1", "SLS Block 1", "launch-vehicle", "launch-vehicles", []string{"SRBs", "Core", "ICPS"}},
	}
	for _, c := range cases {
		l, ok := Loadouts[c.id]
		if !ok {
			t.Errorf("loadout %s missing", c.id)
			continue
		}
		if l.Name != c.name || l.Role != c.role || l.Category != c.category {
			t.Errorf("%s: name/role/category = %q/%q/%q, want %q/%q/%q", c.id, l.Name, l.Role, l.Category, c.name, c.role, c.category)
		}
		if len(l.Stages) != len(c.stageNames) {
			t.Errorf("%s: %d stages, want %d", c.id, len(l.Stages), len(c.stageNames))
			continue
		}
		for i, n := range c.stageNames {
			if l.Stages[i].Name != n {
				t.Errorf("%s: stage %d = %q, want %q", c.id, i, l.Stages[i].Name, n)
			}
			if wantMark := c.id != "Orion" && i == 0; l.Stages[i].LitWithAbove != wantMark {
				t.Errorf("%s: stage %d LitWithAbove = %v, want %v", c.id, i, l.Stages[i].LitWithAbove, wantMark)
			}
		}
		if len(l.DecouplePlan) != 0 || len(l.NosePayloadPlan) != 0 {
			t.Errorf("%s: decouple/nose plans = %v/%v, want none", c.id, l.DecouplePlan, l.NosePayloadPlan)
		}
	}
	// SLS-Orion is listed after the Dragon loadouts, Orion after it.
	idx := map[string]int{}
	for i, id := range LoadoutOrder {
		idx[id] = i
	}
	if !(idx["Falcon-9-Cargo-Dragon"] < idx["SLS-Orion"] && idx["SLS-Orion"] < idx["Orion"]) {
		t.Errorf("order: F9 cargo dragon %d, SLS-Orion %d, Orion %d", idx["Falcon-9-Cargo-Dragon"], idx["SLS-Orion"], idx["Orion"])
	}
	if cr := NewFromLoadout("SLS-Orion"); !cr.Crewed || cr.AntennaKind != AntennaDirect {
		t.Errorf("SLS-Orion crewed/antenna = %v/%v", cr.Crewed, cr.AntennaKind)
	}
}

// ADR 0060 decision 4 / guard 4: the rendered VAB figures, recomputed by hand
// from the part data (rocket equation, boosters and core lit together).
func TestOrionStackStatsPins(t *testing.T) {
	vs := StackStats(Loadouts["SLS-Orion"].Stages)
	for i, w := range []float64{2478, 8258, 2781, 1295, 0} {
		if i >= len(vs.StageDV) {
			t.Fatalf("StageDV = %v", vs.StageDV)
		}
		near(t, "SLS-Orion stage dv", vs.StageDV[i], w, 1)
	}
	near(t, "SLS-Orion Σ Δv", vs.TotalDV, 14812, 2)
	near(t, "SLS-Orion TWR", vs.LiftoffTWR, 1.61, 0.005)
	// Hand sum: 2,587,228 kg dry+fuel plus 19,909.7 kg derived RCS monoprop
	// (stageRCS scales with dry mass, so the ICPS pool is 282.5 kg, not the
	// 229 kg the ADR carried over). The ADR's 2,607,084 is 54 kg low.
	near(t, "SLS-Orion mass", vs.TotalMass, 2_607_138, 1)

	o := StackStats(Loadouts["Orion"].Stages)
	if len(o.StageDV) != 2 {
		t.Fatalf("Orion StageDV = %v", o.StageDV)
	}
	near(t, "Orion ESM dv", o.StageDV[0], 1295, 1)
	near(t, "Orion CM dv", o.StageDV[1], 0, 0)

	// Decision 2's price, settled by Jason 2026-10-23: the bare SLS's ICPS
	// drops from 9,501 to 8,680 m/s.
	b := StackStats(Loadouts["SLS-Block1"].Stages)
	near(t, "SLS Block 1 ICPS dv", b.StageDV[2], 8680, 1)
}
