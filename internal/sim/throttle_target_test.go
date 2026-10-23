package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// liquidPairLit is a marked liquid booster under a liquid core, burning by
// hand at 100%, with a bare upper stage so the booster can be dropped.
func liquidPairLit(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
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
	if c.ManualBurn == nil {
		t.Fatal("setup: manual burn did not start")
	}
	return w, c
}

// ADR 0058 guard 15.[6]: c cycles all → core → boosters → all.
func TestCycleThrottleTargetWalksTheThreeTargets(t *testing.T) {
	w, c := liquidPairLit(t)
	for _, want := range []spacecraft.ThrottleTarget{ThrottleCore, ThrottleBoosters, ThrottleAll, ThrottleCore} {
		got, ok := w.CycleThrottleTarget()
		if !ok || got != want || c.ThrottleTarget != want {
			t.Fatalf("cycle = %v ok=%v (stored %v), want %v", got, ok, c.ThrottleTarget, want)
		}
	}
}

// ADR 0058 guard 15.[6]: a vessel with one throttle (SLS: solid boosters)
// answers c with "no", and the target stays All.
func TestCycleThrottleTargetNoOpOnOneThrottle(t *testing.T) {
	w, c := slsLit(t)
	if got, ok := w.CycleThrottleTarget(); ok || got != ThrottleAll || c.ThrottleTarget != ThrottleAll {
		t.Errorf("SLS cycle = %v ok=%v stored %v, want all/false/all", got, ok, c.ThrottleTarget)
	}
	// A stale non-All target on a one-throttle vessel still moves the one throttle.
	c.ThrottleTarget = ThrottleBoosters
	w.SetThrottle(0.5)
	if c.Throttle != 0.5 {
		t.Errorf("stale boosters target on SLS: core throttle = %v, want 0.5", c.Throttle)
	}
}

// ADR 0058 guard 15.[6]: the throttle keys move the selected throttle only.
func TestThrottleKeysRouteThroughTheTarget(t *testing.T) {
	w, c := liquidPairLit(t)

	c.ThrottleTarget = ThrottleCore
	w.SetThrottle(0.4)
	litNear(t, "core after core-only set", c.Throttle, 0.4, 1e-9)
	litNear(t, "boosters after core-only set", c.BoosterThrottleEff(), 1.0, 1e-9)

	c.ThrottleTarget = ThrottleBoosters
	w.AdjustThrottle(-0.3)
	litNear(t, "boosters after boosters-only step", c.BoosterThrottleEff(), 0.7, 1e-9)
	litNear(t, "core after boosters-only step", c.Throttle, 0.4, 1e-9)

	c.ThrottleTarget = ThrottleAll
	w.SetThrottle(0.8)
	litNear(t, "core after all", c.Throttle, 0.8, 1e-9)
	litNear(t, "boosters after all", c.BoosterThrottleEff(), 0.8, 1e-9)
	if c.BoosterThrottle != nil {
		t.Errorf("all must clear BoosterThrottle back to nil, got %v", *c.BoosterThrottle)
	}
}

// ADR 0058 decision 4: x on core stops the core and the boosters keep the
// burn; x on boosters is the reverse; x on all cuts both and ends the burn.
func TestZeroZeroesOnlyTheTarget(t *testing.T) {
	w, c := liquidPairLit(t)
	c.ThrottleTarget = ThrottleCore
	w.SetThrottle(0)
	if c.ManualBurn == nil || c.BoosterThrottleEff() != 1 {
		t.Fatalf("x on core: burn=%v boosters=%v, want the burn alive with boosters at 1", c.ManualBurn != nil, c.BoosterThrottleEff())
	}
	c.ThrottleTarget = ThrottleAll
	w.SetThrottle(0)
	if c.ManualBurn != nil {
		t.Error("x on all with no solid: burn should end")
	}

	w, c = liquidPairLit(t)
	c.ThrottleTarget = ThrottleBoosters
	w.SetThrottle(0)
	if c.ManualBurn == nil || c.Throttle != 1 || c.BoosterThrottleEff() != 0 {
		t.Errorf("x on boosters: burn=%v core=%v boosters=%v", c.ManualBurn != nil, c.Throttle, c.BoosterThrottleEff())
	}
}

// Core-only after an all keeps the boosters where they were (they stop
// following): the pin happens on the first core-only write.
func TestCoreOnlyPinsFollowingBoosters(t *testing.T) {
	w, c := liquidPairLit(t)
	w.SetThrottle(0.9) // all: boosters follow
	c.ThrottleTarget = ThrottleCore
	w.SetThrottle(0.5)
	litNear(t, "boosters", c.BoosterThrottleEff(), 0.9, 1e-9)
}

// Decision 3 / S4 scope: the booster drop clears BoosterThrottle and snaps
// the target back to all.
func TestBoosterDropResetsThrottleTargetAndBoosterThrottle(t *testing.T) {
	w, c := liquidPairLit(t)
	c.ThrottleTarget = ThrottleBoosters
	w.SetThrottle(0.3)
	if c.BoosterThrottle == nil {
		t.Fatal("setup: BoosterThrottle not set")
	}
	idx := w.ActiveCraftIdx
	if _, _, err := w.StageActive(idx); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if c.BoosterThrottle != nil {
		t.Errorf("BoosterThrottle = %v after the drop, want nil", *c.BoosterThrottle)
	}
	if c.ThrottleTarget != ThrottleAll {
		t.Errorf("ThrottleTarget = %v after the drop, want all", c.ThrottleTarget)
	}
}
