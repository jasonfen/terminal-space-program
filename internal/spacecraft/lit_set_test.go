package spacecraft

import (
	"math"
	"testing"
)

// slsSynth is the ADR 0058 SLS: [SRBs marked, Core, ICPS], numbers from
// data/parts.json (monoprop zero so the mass convention is unambiguous).
func slsSynth() []Stage {
	return []Stage{
		{Name: "SRBs", DryMass: 198_000, FuelMass: 1_270_000, FuelCapacity: 1_270_000,
			Thrust: 32.0e6, Isp: 268, FuelType: FuelTypeSolid, LitWithAbove: true},
		{Name: "Core", DryMass: 85_275, FuelMass: 979_452, FuelCapacity: 979_452,
			Thrust: 9.29e6, Isp: 452, FuelType: FuelTypeHydrolox},
		{Name: "ICPS", DryMass: 3_500, FuelMass: 25_000, FuelCapacity: 25_000,
			Thrust: 110e3, Isp: 462, FuelType: FuelTypeHydrolox},
	}
}

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f (±%g)", what, got, want, tol)
	}
}

func TestLitStages(t *testing.T) {
	eq := func(got, want []int) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	if got := LitStages(slsSynth()); !eq(got, []int{0, 1}) {
		t.Errorf("marked SLS lit set = %v, want [0 1]", got)
	}
	serial := slsSynth()
	serial[0].LitWithAbove = false
	if got := LitStages(serial); !eq(got, []int{0}) {
		t.Errorf("unmarked SLS lit set = %v, want [0]", got)
	}
	if got := LitStages(nil); len(got) != 0 {
		t.Errorf("empty stack lit set = %v, want empty", got)
	}
	// A mark on the top stage has nothing above it to light.
	top := []Stage{{Thrust: 1, Isp: 1, LitWithAbove: true}}
	if got := LitStages(top); !eq(got, []int{0}) {
		t.Errorf("top-stage mark lit set = %v, want [0]", got)
	}
	// Inert stage under the booster: the set starts at the lit stage.
	inert := append([]Stage{{DryMass: 10}}, slsSynth()...)
	if got := LitStages(inert); !eq(got, []int{1, 2}) {
		t.Errorf("inert-under-SLS lit set = %v, want [1 2]", got)
	}
}

// ADR 0058 guard 2: SyncFields mirrors the set's summed thrust and the
// thrust-weighted blend Isp_eff = ΣT / Σ(T/Isp).
func TestSyncFieldsBlendsTheLitSet(t *testing.T) {
	c := &Spacecraft{Stages: slsSynth()}
	c.SyncFields()
	near(t, "Thrust", c.Thrust, 41.29e6, 1)
	near(t, "Isp", c.Isp, 295.02, 0.01)
}

// Per-tank debit (ADR 0058 guard 2): one BurnFuel of the combined mass flow
// for dt = 1 s takes 12,176 kg from the SRB tank and 2,096 from the core.
func TestBurnFuelDebitsEachLitTank(t *testing.T) {
	c := &Spacecraft{Stages: slsSynth()}
	c.SyncFields()
	dt := 1.0
	got := c.BurnFuel(c.MassFlowRateAt(1) * dt)
	wantSRB := 32.0e6 / (268 * g0)
	wantCore := 9.29e6 / (452 * g0)
	near(t, "burned", got, wantSRB+wantCore, 1e-6)
	near(t, "SRB tank debit", 1_270_000-c.Stages[0].FuelMass, wantSRB, 1e-6)
	near(t, "core tank debit", 979_452-c.Stages[1].FuelMass, wantCore, 1e-6)
	if c.Stages[2].FuelMass != 25_000 {
		t.Errorf("ICPS tank touched: %g", c.Stages[2].FuelMass)
	}
}

// A dry booster stops contributing thrust; the core carries on alone.
func TestDryBoosterDropsOutOfTheBlend(t *testing.T) {
	st := slsSynth()
	st[0].FuelMass = 0
	c := &Spacecraft{Stages: st}
	c.SyncFields()
	near(t, "Thrust", c.Thrust, 9.29e6, 1)
	near(t, "Isp", c.Isp, 452, 1e-9)
	near(t, "ActiveStageFuel", c.ActiveStageFuel(), 979_452, 1e-9)
}

// ADR 0058 decision 7: RemainingDeltaV at SLS T+42 s is the blended-Isp Δv
// until the first lit tank (the SRBs) empties. The state is the ADR's own
// (flows rounded to 12,176 / 2,096 kg/s, so tanks 758,608 / 891,420). The
// exact arithmetic (slice-S1.md) gives 1,746.8; the ADR's 1,745 rounds the
// 62.3 s left to 62 s, so the pin is ±3 around the ADR figure and ±0.5
// around the hand-worked exact one.
func TestRemainingDeltaVAtT42(t *testing.T) {
	st := slsSynth()
	st[0].FuelMass = 1_270_000 - 12_176*42
	st[1].FuelMass = 979_452 - 2_096*42
	c := &Spacecraft{Stages: st}
	c.SyncFields()
	near(t, "mass", c.TotalMass(), 1_961_803, 1)
	got := c.RemainingDeltaV()
	near(t, "RemainingDeltaV vs ADR 1745", got, 1745, 3)
	near(t, "RemainingDeltaV exact", got, 1746.8, 0.5)
}

// An unmarked stack keeps the single-tank answer.
func TestRemainingDeltaVSerialUnchanged(t *testing.T) {
	st := slsSynth()
	st[0].LitWithAbove = false
	c := &Spacecraft{Stages: st}
	c.SyncFields()
	want := 268 * g0 * math.Log(c.TotalMass()/(c.TotalMass()-1_270_000))
	near(t, "serial RemainingDeltaV", c.RemainingDeltaV(), want, 1e-9)
}

// ADR 0058 decision 9 / guard 10 numbers via StackStats: S1 2,518, S2 9,041,
// ICPS 9,502, Σ 21,060 (ADR 21,061 ±1), liftoff TWR 1.64.
func TestStackStatsHandWorkedLitSet(t *testing.T) {
	vs := StackStats(slsSynth())
	near(t, "S1 Δv", vs.StageDV[0], 2518, 1)
	near(t, "S2 Δv", vs.StageDV[1], 9041, 1)
	near(t, "S3 Δv", vs.StageDV[2], 9502, 1)
	near(t, "Σ Δv", vs.TotalDV, 21061, 1.5)
	near(t, "TWR", vs.LiftoffTWR, 1.64, 0.005)
	near(t, "mass", vs.TotalMass, 2_561_227, 1)
}

// ADR 0058 decision 1: SLS-Block1 ships marked on its bottom part; every
// other loadout resolves serial. Raster-LS is deliberately NOT marked in S1:
// lit together its core (51 s) dies before its solids (72 s) and the Lumen
// Kern-orbit gate (TestLumenLaunchersReachKernOrbit) reads 3,188 < 3,400 m/s.
// S9 marks it in the same change as the core-fuel retune.
func TestOnlyBoosterLoadoutsAreMarked(t *testing.T) {
	for _, id := range LoadoutOrder {
		want := id == "SLS-Block1"
		for i, st := range Loadouts[id].Stages {
			if got := st.LitWithAbove; got != (want && i == 0) {
				t.Errorf("%s stage %d (%s) LitWithAbove = %v, want %v", id, i, st.Name, got, want && i == 0)
			}
		}
	}
	// The shipped SLS is the ADR's: lit set {SRBs, Core}, TWR 1.64 on the pad.
	l := Loadouts["SLS-Block1"]
	if got := LitStages(l.Stages); len(got) != 2 {
		t.Errorf("SLS lit set = %v, want 2 stages", got)
	}
	near(t, "SLS pad TWR", StackStats(l.Stages).LiftoffTWR, 1.64, 0.01)
}

// ADR 0058 decision 1: a second mark directly above a mark is dropped with a
// warning (skip-bad, ADR 0026 §3); the first mark and other loadouts stand.
func TestDropDoubleMarks(t *testing.T) {
	defs := []LoadoutDef{
		{ID: "ok", Parts: []PartRef{{PartID: "a", LitWithAbove: true}, {PartID: "b"}}},
		{ID: "twice", Parts: []PartRef{{PartID: "a", LitWithAbove: true}, {PartID: "b", LitWithAbove: true}, {PartID: "c"}}},
	}
	got, warns := dropDoubleMarks(defs)
	if !got[0].Parts[0].LitWithAbove || !got[1].Parts[0].LitWithAbove {
		t.Error("first marks must stand")
	}
	if got[1].Parts[1].LitWithAbove {
		t.Error("second mark must be dropped")
	}
	if len(warns) != 1 || warns[0].Path != "twice" {
		t.Errorf("warnings = %v, want one for loadout twice", warns)
	}
	if !defs[1].Parts[1].LitWithAbove {
		t.Error("input slice mutated")
	}
}

// The thrust canLiftOff reads (c.Thrust over TotalMass, internal/sim/landed.go:
// 94-99) is the lit set's sum, and gives the same TWR as the VAB's LiftoffTWR.
func TestMarkedSLSLiftoffThrustAgreesWithStats(t *testing.T) {
	c := NewFromLoadout("SLS-Block1")
	near(t, "c.Thrust", c.Thrust, 41.29e6, 1)
	got := c.Thrust / (c.TotalMass() * g0)
	near(t, "canLiftOff-basis TWR", got, StackStats(c.Stages).LiftoffTWR, 1e-9)
	near(t, "TWR vs ADR 1.64", got, 1.64, 0.01)
}

// Loadout.Thrust/Isp/Fuel read the lit set, so a marked SLS is 41.29 MN /
// 295 s / SRB + core fuel, and an empty loadout reads zero.
func TestLoadoutReadsTheLitSet(t *testing.T) {
	l := Loadouts["SLS-Block1"]
	near(t, "Thrust", l.Thrust(), 41.29e6, 1)
	near(t, "Isp", l.Isp(), 295.02, 0.01)
	near(t, "Fuel", l.Fuel(), 1_270_000+979_452, 1e-6)
	if th, isp := LitEngine(nil); th != 0 || isp != 0 {
		t.Errorf("LitEngine(nil) = %g, %g, want 0, 0", th, isp)
	}
}
