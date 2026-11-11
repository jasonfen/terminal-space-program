package spacecraft

import "testing"

// ADR 0057 decision 1, the data table pinned part by part. The numbers are
// spelled out here rather than read back from parts.json so the test can
// disagree with the data.
func TestDragonPartsDataTable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	parts, _, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name, tier, cmd, fuelType, desc string
		dry, fuel, thrust, isp          float64
		antenna                         bool
		chute                           bool
		spriteRows, spriteW             int
	}
	want := map[string]row{
		"dragon-crew": {"Dragon", "payload", "crewed", "hypergolic", "8 SuperDraco abort engines, 71 kN each",
			7700, 2562, 1600, 300, true, true, 6, 3},
		"dragon-cargo": {"Cargo Dragon", "payload", "probe", "hypergolic", "",
			7700, 2562, 1600, 300, true, true, 6, 3},
		"dragon-trunk": {"Dragon Trunk", "payload", "", "", "",
			1800, 0, 0, 0, false, false, 5, 3},
	}
	for id, w := range want {
		p, ok := parts[id]
		if !ok {
			t.Errorf("%s missing from parts.json", id)
			continue
		}
		if p.Name != w.name || p.Tier != w.tier || p.CommandSource != w.cmd || p.FuelType != w.fuelType {
			t.Errorf("%s: name/tier/command/fuel = %q/%q/%q/%q, want %q/%q/%q/%q", id,
				p.Name, p.Tier, p.CommandSource, p.FuelType, w.name, w.tier, w.cmd, w.fuelType)
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
		// the capsules have legs and SuperDracos (ADR 0069 decision 6), the trunk neither
		if p.HasParachute != w.chute || p.CanSoftLand != w.chute {
			t.Errorf("%s: chute/softland = %v/%v, want %v/%v", id, p.HasParachute, p.CanSoftLand, w.chute, w.chute)
		}
		if wantSuper := map[bool]float64{true: 568000, false: 0}[w.chute]; p.SuperThrustN != wantSuper || (w.chute && p.SuperIspS != 235) {
			t.Errorf("%s: super thrust/isp = %g/%g, want %g/235 (8 x 71 kN)", id, p.SuperThrustN, p.SuperIspS, wantSuper)
		}
		if p.BallisticCoefficient != 0 {
			t.Errorf("%s: ballistic_coeff = %g, want absent", id, p.BallisticCoefficient)
		}
		if p.LaunchSpriteRowsPx != w.spriteRows || p.LaunchSpriteWidthPx != w.spriteW {
			t.Errorf("%s: sprite %dx%d, want %dx%d", id, p.LaunchSpriteRowsPx, p.LaunchSpriteWidthPx, w.spriteRows, w.spriteW)
		}
		if p.Glyph != "➤" {
			t.Errorf("%s: glyph = %q", id, p.Glyph)
		}
		// Ports: the capsules carry the one top port; the trunk none.
		if id == "dragon-trunk" {
			if len(p.Ports) != 0 {
				t.Errorf("%s: ports = %+v, want none", id, p.Ports)
			}
		} else if len(p.Ports) != 1 || p.Ports[0].End != PortEndTop {
			t.Errorf("%s: ports = %+v, want [{top}]", id, p.Ports)
		}
	}
}

// ADR 0057 decision 1, the four loadouts: id, name, role, category, parts.
func TestDragonLoadouts(t *testing.T) {
	cases := []struct {
		id, name, role, category string
		stageNames               []string
		crewed                   bool
	}{
		{"Dragon", "Dragon", "capsule", "landers-capsules", []string{"Dragon Trunk", "Dragon"}, true},
		{"Cargo-Dragon", "Cargo Dragon", "capsule", "landers-capsules", []string{"Dragon Trunk", "Cargo Dragon"}, false},
		{"Falcon-9-Dragon", "Falcon 9 + Dragon", "mission-stack", "mission-stacks", []string{"F9-S1", "F9-S2", "Dragon Trunk", "Dragon"}, true},
		{"Falcon-9-Cargo-Dragon", "F9 + Cargo Dragon", "mission-stack", "mission-stacks", []string{"F9-S1", "F9-S2", "Dragon Trunk", "Cargo Dragon"}, false},
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
		}
		if len(l.DecouplePlan) != 0 {
			t.Errorf("%s: decouple_plan = %v, want none (every press drops one stage)", c.id, l.DecouplePlan)
		}
		cr := NewFromLoadout(c.id)
		if cr.Crewed != c.crewed {
			t.Errorf("%s: Crewed = %v, want %v", c.id, cr.Crewed, c.crewed)
		}
		if cr.AntennaKind != AntennaDirect || cr.AntennaRangeM != 100_000_000 {
			t.Errorf("%s: antenna = %v %g, want direct 1e8", c.id, cr.AntennaKind, cr.AntennaRangeM)
		}
	}
	// A lone Dragon flies on the capsule's Dracos with the trunk on.
	d := NewFromLoadout("Dragon")
	if d.Thrust != 1600 || d.Isp != 300 || d.ActiveStageFuel() != 2562 {
		t.Errorf("Dragon thrust/isp/fuel = %g/%g/%g, want 1600/300/2562", d.Thrust, d.Isp, d.ActiveStageFuel())
	}
}

// ADR 0064 decision 2 (guard 1): fh-boosters is exactly two f9-s1, spelled out
// here rather than read back so the test can disagree with the data.
func TestFHBoostersPartDataTable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	parts, _, err := LoadCatalog()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := parts["fh-boosters"]
	if !ok {
		t.Fatal("fh-boosters missing from parts.json")
	}
	if p.Name != "Side Boosters" || p.Tier != "booster" || p.Glyph != "➤" || p.Color != "#E8E8E8" ||
		p.FuelType != "kerolox" || p.CommandSource != "probe" || p.Antenna == nil || p.Antenna.Kind != "direct" || p.Antenna.RangeM != 1e7 {
		t.Errorf("identity fields = %+v", p)
	}
	if p.DryMassKg != 51200 || p.FuelMassKg != 822000 || p.FuelCapacityKg != 822000 || p.ThrustN != 15214000 || p.IspS != 282 ||
		p.BallisticCoefficient != 0.0000074 || p.DiameterM != 3.7 {
		t.Errorf("figures = dry %g fuel %g cap %g thrust %g isp %g bc %g d %g", p.DryMassKg, p.FuelMassKg, p.FuelCapacityKg, p.ThrustN, p.IspS, p.BallisticCoefficient, p.DiameterM)
	}
	if p.LaunchSpriteRowsPx != 20 || p.LaunchSpriteWidthPx != 6 || !p.CanSoftLand || len(p.VabSeed) != 26 {
		t.Errorf("sprite %dx%d soft-land %v seed %d, want 20x6 true 26", p.LaunchSpriteRowsPx, p.LaunchSpriteWidthPx, p.CanSoftLand, len(p.VabSeed))
	}
	if c := parts["f9-s1"]; p.DryMassKg != 2*c.DryMassKg || p.FuelMassKg != 2*c.FuelMassKg || p.ThrustN != 2*c.ThrustN {
		t.Errorf("fh-boosters is not two f9-s1: %g/%g/%g vs %g/%g/%g", p.DryMassKg, p.FuelMassKg, p.ThrustN, c.DryMassKg, c.FuelMassKg, c.ThrustN)
	}
}
