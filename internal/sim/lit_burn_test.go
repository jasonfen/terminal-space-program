package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const testG0 = 9.80665

// slsLit is the ADR 0058 SLS lit set on the active craft (LEO), burning by hand.
func slsLit(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "SRBs", DryMass: 198_000, FuelMass: 1_270_000, FuelCapacity: 1_270_000,
			Thrust: 32.0e6, Isp: 268, FuelType: spacecraft.FuelTypeSolid, LitWithAbove: true},
		{Name: "Core", DryMass: 85_275, FuelMass: 979_452, FuelCapacity: 979_452,
			Thrust: 9.29e6, Isp: 452, FuelType: spacecraft.FuelTypeHydrolox},
		{Name: "ICPS", DryMass: 3_500, FuelMass: 25_000, FuelCapacity: 25_000,
			Thrust: 110e3, Isp: 462, FuelType: spacecraft.FuelTypeHydrolox},
	}
	crewTend(c)
	c.SyncFields()
	c.State.M = c.TotalMass()
	c.Throttle = 1
	c.EngineMode = spacecraft.EngineMain
	w.StartManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("setup: manual burn did not start")
	}
	return w, c
}

func litNear(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f (±%g)", what, got, want, tol)
	}
}

// ADR 0058 guard 15.[3]: x with a solid lit keeps the burn alive, the solids
// keep thrusting at 32 MN worth of flow, and the core does not burn.
func TestCutWithSolidLitKeepsTheBurn(t *testing.T) {
	w, c := slsLit(t)
	w.SetThrottle(0)
	if c.ManualBurn == nil {
		t.Fatal("x ended the burn while the SRBs burn")
	}
	t0 := w.Clock.SimTime
	w.Tick()
	dt := w.Clock.SimTime.Sub(t0).Seconds()
	litNear(t, "SRB debit", 1_270_000-c.Stages[0].FuelMass, 32.0e6/(268*testG0)*dt, 1)
	litNear(t, "core debit", 979_452-c.Stages[1].FuelMass, 0, 1e-9)
}

// The core adds exactly its own 9.29 MN at θ=1 and nothing at θ=0: one
// tick's along-nose Δv differs by 9.29 MN / m · dt between the two.
func TestSolidsPinnedThrustSplit(t *testing.T) {
	dv := func(core float64) float64 {
		w, c := slsLit(t)
		w.SetThrottle(core)
		v0, t0 := c.State.V, w.Clock.SimTime
		w.Tick()
		dt := w.Clock.SimTime.Sub(t0).Seconds()
		_ = dt
		return c.State.V.Sub(v0).Dot(c.CurrentAttitudeDir.Unit())
	}
	w, c := slsLit(t)
	dt := w.Clock.BaseStep.Seconds()
	want := 9.29e6 / c.TotalMass() * dt
	litNear(t, "core Δv contribution", dv(1)-dv(0), want, want*0.02)
}

// b with a solid lit is x on all: throttle 0, burn object alive.
func TestBWithSolidLitCutsLiquidOnly(t *testing.T) {
	w, c := slsLit(t)
	w.ToggleManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("b ended the burn while the SRBs burn")
	}
	if c.Throttle != 0 {
		t.Errorf("core throttle after b = %v, want 0", c.Throttle)
	}
}

// With the SRBs spent and the core at 0, nothing is firing: the burn ends.
func TestBurnEndsWhenLastSolidBurnsOutWithCoreCut(t *testing.T) {
	w, c := slsLit(t)
	w.SetThrottle(0)
	c.Stages[0].FuelMass = 1000 // a few seconds of SRB left
	c.SyncFields()
	if _, ok := tickUntil(w, 100, func() bool { return c.ManualBurn == nil }); !ok {
		t.Fatal("burn outlived the last solid with the core at 0")
	}
	if c.Stages[1].FuelMass != 979_452 {
		t.Errorf("core burned while cut: %v", c.Stages[1].FuelMass)
	}
}

// Without a solid, x ends the burn as before.
func TestCutWithoutSolidEndsTheBurn(t *testing.T) {
	w, c := slsLit(t)
	c.Stages[0].FuelMass = 0
	c.SyncFields()
	w.SetThrottle(0)
	if c.ManualBurn != nil {
		t.Error("x left the burn running with no solid lit")
	}
}

// padSLS sits an SLS on Earth's pad with total weight pinned to weightN.
func padSLS(t *testing.T, weightN float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID: spacecraft.LoadoutSLSBlock1ID, ParentBodyID: "earth",
		Launchpad: true, Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	c.Stages[0].LitWithAbove = true // the S9 retune marks Raster-LS; SLS-Block1 already is
	r := c.State.R.Norm()
	g := c.Primary.GravitationalParameter() / (r * r)
	c.Stages[len(c.Stages)-1].DryMass += weightN/g - c.TotalMass()
	crewTend(c)
	c.SyncFields()
	c.State.M = c.TotalMass()
	w.SetAttitudeMode(spacecraft.BurnRadialOut)
	return w, c
}

// #716 reconcile: with the SRBs lit and the core throttled, lift is
// ΣT_i·θ_i, not (ΣT)·θ_core. Weight 37 MN: core 60% gives 32+5.57 = 37.57 MN
// and lifts; core 50% gives 36.65 MN and stays seated. The old formula read
// 41.29·0.6 = 24.8 MN and kept both seated.
func TestPadLiftUsesPerGroupThrust(t *testing.T) {
	for _, tc := range []struct {
		core float64
		lift bool
	}{{0.6, true}, {0.5, false}} {
		w, c := padSLS(t, 37e6)
		w.SetThrottle(tc.core)
		w.StartManualBurn()
		if c.ManualBurn == nil {
			t.Fatalf("core %v: engine did not light", tc.core)
		}
		w.Tick()
		if c.Crashed {
			t.Fatalf("core %v: crashed on the pad", tc.core)
		}
		if lifted := !c.Landed; lifted != tc.lift {
			t.Errorf("core %v: lifted=%v, want %v", tc.core, lifted, tc.lift)
		}
	}
}

// Seated fuel burn follows the same per-group flow as flight: SRBs full, core at θ.
func TestSeatedBurnDebitsPerGroup(t *testing.T) {
	w, c := padSLS(t, 60e6) // far too heavy to lift
	w.SetThrottle(0.5)
	w.StartManualBurn()
	t0 := w.Clock.SimTime
	w.Tick()
	if !c.Landed {
		t.Fatal("setup: should stay seated")
	}
	dt := w.Clock.SimTime.Sub(t0).Seconds()
	litNear(t, "SRB debit", c.Stages[0].FuelCapacity-c.Stages[0].FuelMass, 32.0e6/(268*testG0)*dt, 1)
	litNear(t, "core debit", c.Stages[1].FuelCapacity-c.Stages[1].FuelMass, 0.5*9.29e6/(452*testG0)*dt, 1)
}

// ADR 0058 guard 15.[3] through the integrator: a liquid pair with the
// booster group at 60% and the core at 100% debits 0.6·ṁ and 1.0·ṁ.
func TestLiquidPairDebitsPerGroupThroughStepThrust(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "LRB", DryMass: 50_000, FuelMass: 400_000, FuelCapacity: 400_000,
			Thrust: 10e6, Isp: 300, FuelType: spacecraft.FuelTypeHydrolox, LitWithAbove: true},
		{Name: "Core", DryMass: 30_000, FuelMass: 300_000, FuelCapacity: 300_000,
			Thrust: 5e6, Isp: 400, FuelType: spacecraft.FuelTypeHydrolox},
	}
	crewTend(c)
	c.SyncFields()
	c.State.M = c.TotalMass()
	c.Throttle = 1
	c.EngineMode = spacecraft.EngineMain
	w.StartManualBurn()
	w.SetThrottleFor(ThrottleBoosters, 0.6)
	if c.Throttle != 1 {
		t.Fatalf("boosters write moved the core: %v", c.Throttle)
	}
	t0 := w.Clock.SimTime
	w.Tick()
	dt := w.Clock.SimTime.Sub(t0).Seconds()
	litNear(t, "booster debit", 400_000-c.Stages[0].FuelMass, 0.6*10e6/(300*testG0)*dt, 0.01)
	litNear(t, "core debit", 300_000-c.Stages[1].FuelMass, 1.0*5e6/(400*testG0)*dt, 0.01)
	// Core to 0 with the boosters still at 60%: the burn lives.
	w.SetThrottleFor(ThrottleCore, 0)
	if c.ManualBurn == nil {
		t.Error("core at 0 ended the burn with the boosters at 60%")
	}
	w.SetThrottleFor(ThrottleBoosters, 0)
	if c.ManualBurn != nil {
		t.Error("both liquid groups at 0, no solid: burn should end")
	}
}
