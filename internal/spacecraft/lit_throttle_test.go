package spacecraft

import "testing"

// liquidPair is a synthetic marked liquid booster under a liquid core.
func liquidPair() []Stage {
	return []Stage{
		{Name: "LRB", DryMass: 50_000, FuelMass: 400_000, FuelCapacity: 400_000,
			Thrust: 10e6, Isp: 300, FuelType: FuelTypeHydrolox, LitWithAbove: true},
		{Name: "Core", DryMass: 30_000, FuelMass: 300_000, FuelCapacity: 300_000,
			Thrust: 5e6, Isp: 400, FuelType: FuelTypeHydrolox},
	}
}

func fp(v float64) *float64 { return &v }

// ADR 0058 guard 15.[3]: a liquid pair at booster 60% / core 100% debits
// 0.6·ṁ and 1.0·ṁ, each from its own tank. Independent count: ṁ = T/(Isp·g0).
func TestBurnFuelAtFollowsEachGroupsThrottle(t *testing.T) {
	c := &Spacecraft{Stages: liquidPair(), BoosterThrottle: fp(0.6)}
	c.SyncFields()
	got := c.BurnFuelAt(1.0, 2.0)
	mB, mC := 10e6/(300*g0), 5e6/(400*g0) // 3399.0, 1274.6 kg/s
	near(t, "booster debit", 400_000-c.Stages[0].FuelMass, 0.6*mB*2, 1e-6)
	near(t, "core debit", 300_000-c.Stages[1].FuelMass, 1.0*mC*2, 1e-6)
	near(t, "burned", got, 0.6*mB*2+mC*2, 1e-6)
}

// ΣT_i·θ_i over ΣT_i: (0.6·10 + 1·5)/15 = 0.7333.
func TestLitThrottleBlendsGroupThrottles(t *testing.T) {
	c := &Spacecraft{Stages: liquidPair(), BoosterThrottle: fp(0.6)}
	c.SyncFields()
	near(t, "LitThrottle", c.LitThrottle(1.0), 11.0/15.0, 1e-9)
	c.BoosterThrottle = nil // follows the core
	near(t, "follows core", c.LitThrottle(0.5), 0.5, 1e-9)
}

// ADR 0058 guard 15.[3]: solids are pinned. Core cut to 0, SRBs still give
// 32/41.29 of the set's thrust and burn at full flow.
func TestSolidsPinnedWhateverTheCoreThrottle(t *testing.T) {
	c := &Spacecraft{Stages: slsSynth(), BoosterThrottle: fp(0)}
	c.SyncFields()
	near(t, "LitThrottle(0)", c.LitThrottle(0), 32.0/41.29, 1e-9)
	c.BurnFuelAt(0, 1)
	near(t, "SRB debit", 1_270_000-c.Stages[0].FuelMass, 32.0e6/(268*g0), 1e-6)
	if c.Stages[1].FuelMass != 979_452 {
		t.Errorf("core burned at throttle 0: %g", c.Stages[1].FuelMass)
	}
	if !c.SolidLit() {
		t.Error("SolidLit = false with SRBs burning")
	}
	if !c.LiquidCut() {
		t.Error("LiquidCut = false with the core at 0")
	}
	c.Stages[0].FuelMass = 0
	c.SyncFields()
	if c.SolidLit() {
		t.Error("SolidLit = true with the SRB tank dry")
	}
}

// A single-stage vessel keeps today's arithmetic exactly.
func TestLitThrottleSingleStageUnchanged(t *testing.T) {
	c := &Spacecraft{Throttle: 0.37, Stages: []Stage{{DryMass: 100, FuelMass: 1000, Thrust: 1e5, Isp: 300}}}
	c.SyncFields()
	if got := c.LitThrottle(0.37); got != 0.37 {
		t.Errorf("LitThrottle = %v, want exactly 0.37", got)
	}
	if c.SolidLit() || c.LiquidCut() {
		t.Error("single stage at 0.37: SolidLit/LiquidCut should be false")
	}
	c.Throttle = 0
	if !c.LiquidCut() {
		t.Error("single stage at 0: LiquidCut should be true")
	}
}

// Liquid group cut needs both liquid groups at 0.
func TestLiquidCutNeedsEveryLiquidGroupAtZero(t *testing.T) {
	c := &Spacecraft{Stages: liquidPair(), BoosterThrottle: fp(0.5)}
	c.SyncFields()
	if c.LiquidCut() {
		t.Error("core 0 with boosters at 50%: not cut")
	}
	c.Throttle = 0
	if c.LiquidCut() {
		t.Error("core at 0, boosters 50%: not cut")
	}
	c.BoosterThrottle = fp(0)
	if !c.LiquidCut() {
		t.Error("both at 0: cut")
	}
}
