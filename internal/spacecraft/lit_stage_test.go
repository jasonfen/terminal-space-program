package spacecraft

import (
	"math"
	"testing"
)

// litStack is [inert 1,000 kg, engine 10 kN Isp 300 with 500 kg fuel and a
// 20 kg RCS pool at 100 N / 220 s], the ADR 0057 decision 3 guard 2 fixture.
func litStack() []Stage {
	return []Stage{
		{Name: "inert", DryMass: 1000, HasParachute: true, BallisticCoefficient: 77},
		{Name: "lit", DryMass: 200, FuelMass: 500, FuelCapacity: 500, Thrust: 10_000, Isp: 300,
			MonopropMass: 20, RCSThrust: 100, RCSIsp: 220},
	}
}

func TestLitStageIdx(t *testing.T) {
	cases := []struct {
		name   string
		stages []Stage
		want   int
	}{
		{"empty", nil, 0},
		{"no engine anywhere", []Stage{{DryMass: 1}, {DryMass: 2}}, 0},
		{"bottom has the engine", []Stage{{Thrust: 5}, {Thrust: 9}}, 0},
		{"engine second", []Stage{{}, {Thrust: 5}}, 1},
		{"first engine wins over a higher one", []Stage{{}, {Thrust: 5}, {Thrust: 9}}, 1},
	}
	for _, c := range cases {
		if got := LitStageIdx(c.stages); got != c.want {
			t.Errorf("%s: LitStageIdx = %d, want %d", c.name, got, c.want)
		}
	}
}

// Guard 1: for EVERY shipped loadout the lit stage is the bottom stage, so
// every engine reading is the one the bottom stage always gave.
func TestLitStageIsBottomForEveryShippedLoadout(t *testing.T) {
	if len(LoadoutOrder) == 0 {
		t.Fatal("empty loadout catalog")
	}
	for _, id := range LoadoutOrder {
		l := Loadouts[id]
		if len(l.Stages) == 0 {
			t.Errorf("%s: no stages", id)
			continue
		}
		wantLit := 0
		if id == "Dragon" || id == "Cargo-Dragon" {
			wantLit = 1 // ADR 0057 decision 3: the trunk is the inert bottom stage
		}
		if got := LitStageIdx(l.Stages); got != wantLit {
			t.Errorf("%s: LitStageIdx = %d, want %d", id, got, wantLit)
		}
		if wantLit != 0 || l.Stages[0].LitWithAbove {
			continue // the bottom-stage identities below hold only where lit == bottom and serial (ADR 0058: a marked pair reads as a set, lit_set_test.go)
		}
		b := l.Stages[0]
		if l.Thrust() != b.Thrust || l.Isp() != b.Isp || l.Fuel() != b.FuelMass {
			t.Errorf("%s: Loadout engine read (%g, %g, %g) != bottom stage (%g, %g, %g)",
				id, l.Thrust(), l.Isp(), l.Fuel(), b.Thrust, b.Isp, b.FuelMass)
		}
		c := NewFromLoadout(id)
		if c.Thrust != b.Thrust || c.Isp != b.Isp || c.RCSThrust != b.RCSThrust || c.RCSIsp != b.RCSIsp {
			t.Errorf("%s: synced engine fields differ from the bottom stage", id)
		}
		if c.ActiveStageFuel() != b.FuelMass {
			t.Errorf("%s: ActiveStageFuel = %g, want bottom %g", id, c.ActiveStageFuel(), b.FuelMass)
		}
		// TWR and Δv as the VAB reads them.
		vs := StackStats(l.Stages)
		if vs.TotalMass > 0 {
			want := b.Thrust / (vs.TotalMass * g0)
			if math.Abs(vs.LiftoffTWR-want) > 1e-12 {
				t.Errorf("%s: LiftoffTWR = %g, want %g", id, vs.LiftoffTWR, want)
			}
		}
	}
}

// Guard 2: burns and the synced mirror read the lit stage; the chute and
// ballistic coefficient stay the bottom stage's.
func TestLitStageEngineBurnsAndMirrors(t *testing.T) {
	c := NewFromStages(litStack())
	if c.Thrust != 10_000 || c.Isp != 300 {
		t.Errorf("Thrust/Isp = %g/%g, want 10000/300", c.Thrust, c.Isp)
	}
	if c.RCSThrust != 100 || c.RCSIsp != 220 {
		t.Errorf("RCSThrust/RCSIsp = %g/%g, want 100/220", c.RCSThrust, c.RCSIsp)
	}
	if got := c.ActiveStageFuel(); got != 500 {
		t.Errorf("ActiveStageFuel = %g, want 500", got)
	}
	if got := c.BurnFuel(100); got != 100 {
		t.Errorf("BurnFuel returned %g, want 100", got)
	}
	if c.Stages[1].FuelMass != 400 || c.Stages[0].FuelMass != 0 {
		t.Errorf("fuel after burn: stage1 %g (want 400), stage0 %g (want 0)", c.Stages[1].FuelMass, c.Stages[0].FuelMass)
	}
	if got := c.BurnMonoprop(5); got != 5 {
		t.Errorf("BurnMonoprop returned %g, want 5", got)
	}
	if c.Stages[1].MonopropMass != 15 || c.Stages[0].MonopropMass != 0 {
		t.Errorf("monoprop after burn: stage1 %g (want 15), stage0 %g (want 0)", c.Stages[1].MonopropMass, c.Stages[0].MonopropMass)
	}
	if !c.HasParachute {
		t.Error("HasParachute should keep reading the bottom stage")
	}
	if got := c.EffectiveBallisticCoefficient(); got != 77 {
		t.Errorf("EffectiveBallisticCoefficient = %g, want the bottom stage's 77", got)
	}
}

// The VAB strip reads TWR from the lit stage.
func TestStackStatsLiftoffTWRFromLitStage(t *testing.T) {
	st := litStack()
	vs := StackStats(st)
	mass := 1000.0 + 200 + 500 + 20
	want := 10_000 / (mass * g0)
	if math.Abs(vs.LiftoffTWR-want) > 1e-12 {
		t.Errorf("LiftoffTWR = %g, want %g", vs.LiftoffTWR, want)
	}
}

// Loadout accessors read the lit stage too.
func TestLoadoutAccessorsReadLitStage(t *testing.T) {
	l := Loadout{Stages: litStack()}
	if l.Thrust() != 10_000 || l.Isp() != 300 || l.Fuel() != 500 {
		t.Errorf("Loadout engine read = %g/%g/%g, want 10000/300/500", l.Thrust(), l.Isp(), l.Fuel())
	}
	if l.DryMass() != 1000 {
		t.Errorf("DryMass = %g, want the bottom stage's 1000", l.DryMass())
	}
}
