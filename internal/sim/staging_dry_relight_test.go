package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// dryLanderStack gives the active craft a tiny lower stage over a full
// upper stage and holds a manual burn at full throttle, the 57-6/57-7
// shape (#466).
func dryLanderStack(t *testing.T, w *World) *spacecraft.Spacecraft {
	t.Helper()
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "Descent", DryMass: 500, FuelMass: 20, FuelCapacity: 20, Thrust: 200000, Isp: 250},
		{Name: "Ascent", DryMass: 800, FuelMass: 4000, FuelCapacity: 4000, Thrust: 100000, Isp: 300},
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
	return c
}

// TestStagingDuringManualBurnRelights: the dry tank ends the burn, the
// throttle order stands (DRY), and ONE stage press relights the next
// engine at that throttle with no `b`.
func TestStagingDuringManualBurnRelights(t *testing.T) {
	w := mustWorld(t)
	c := dryLanderStack(t, w)
	if _, ok := tickUntil(w, 400, func() bool { return c.ActiveStageFuel() <= 0 }); !ok {
		t.Fatal("descent stage never ran dry")
	}
	w.Tick() // the fuel-out clear runs inside the tick
	if c.ManualBurn != nil {
		t.Fatal("manual burn survived a dry tank")
	}
	if !StackDryArmed(c) {
		t.Fatal("dry tank with throttle 100% must read dry-armed")
	}
	if w.AnyCraftThrusting() {
		t.Fatal("dry-armed counted as thrusting")
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if c.ManualBurn == nil {
		t.Fatal("stage press did not relight at the standing throttle")
	}
	if c.EffectiveThrottle() != 1 {
		t.Errorf("throttle = %v, want 1", c.EffectiveThrottle())
	}
	if StackDryArmed(c) {
		t.Error("still dry-armed after relight")
	}
	before := c.ActiveStageFuel()
	for i := 0; i < 20; i++ {
		w.Tick()
	}
	if c.ActiveStageFuel() >= before {
		t.Errorf("Ascent fuel %v -> %v, engine not burning", before, c.ActiveStageFuel())
	}
}

// TestStagingMidBurnWithFuelLeftCarriesBurn: the other half of
// contradiction 1: a stage pressed mid-burn while the lit tank still has
// fuel keeps the burn going on the next engine.
func TestStagingMidBurnWithFuelLeftCarriesBurn(t *testing.T) {
	w := mustWorld(t)
	c := dryLanderStack(t, w)
	w.Tick()
	if c.ActiveStageFuel() <= 0 {
		t.Fatal("setup: stage already dry")
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if c.ManualBurn == nil || c.Name != "Ascent" {
		t.Fatalf("burn lost or wrong stage after mid-burn stage: burn=%v name=%q", c.ManualBurn != nil, c.Name)
	}
	if StackDryArmed(c) {
		t.Error("dry-armed set though the lit stage had fuel")
	}
}

// TestDryArmedDoesNotClampWarp: a dry vessel with a standing order must
// not pin warp at the 10x burn cap nor light the rendezvous burning flag.
func TestDryArmedDoesNotClampWarp(t *testing.T) {
	w := mustWorld(t)
	c := dryLanderStack(t, w)
	if _, ok := tickUntil(w, 400, func() bool { return c.ActiveStageFuel() <= 0 }); !ok {
		t.Fatal("never ran dry")
	}
	w.Tick()
	if !StackDryArmed(c) {
		t.Fatal("setup: not dry-armed")
	}
	w.Clock.WarpIdx = len(WarpFactors) - 1
	if w.AnyCraftThrusting() {
		t.Error("AnyCraftThrusting true while dry-armed")
	}
	if got, cap := w.EffectiveWarp(), burnWarpCap; got <= cap {
		t.Errorf("EffectiveWarp %v clamped to burn cap %v while only dry-armed", got, cap)
	}
}

// TestCutClearsDryOrder: `x` (throttle 0) cancels the standing order, so
// the next stage press does not light anything.
func TestCutClearsDryOrder(t *testing.T) {
	w := mustWorld(t)
	c := dryLanderStack(t, w)
	if _, ok := tickUntil(w, 400, func() bool { return c.ActiveStageFuel() <= 0 }); !ok {
		t.Fatal("never ran dry")
	}
	w.Tick()
	w.SetThrottle(0)
	if StackDryArmed(c) {
		t.Error("dry-armed survived throttle 0")
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	if c.ManualBurn != nil {
		t.Error("stage press lit an engine after the order was cut")
	}
}

// Review LOW 59: a standing dry order belongs to the vessel you are flying.
// Switching away with [ / ] (or a slot key) drops it, so coming back and
// pressing space later cannot relight an engine at a throttle nothing on
// screen showed the vessel was still holding.
func TestSwitchingVesselDropsDryOrder(t *testing.T) {
	for _, name := range []string{"cycle", "slot"} {
		t.Run(name, func(t *testing.T) {
			w := mustWorld(t)
			c := dryLanderStack(t, w)
			if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 600e3}); err != nil {
				t.Fatalf("SpawnCraft: %v", err)
			}
			w.SetActiveCraftIdx(0) // back on the lander
			if _, ok := tickUntil(w, 400, func() bool { return c.ActiveStageFuel() <= 0 }); !ok {
				t.Fatal("descent stage never ran dry")
			}
			w.Tick()
			if !StackDryArmed(c) {
				t.Fatal("setup: lander must be dry-armed")
			}
			if name == "cycle" {
				w.CycleActiveCraft(1)
			} else if !w.SwitchToCraftIdx(1) {
				t.Fatal("slot switch refused")
			}
			if c.DryOrder {
				t.Error("DryOrder survived switching away from the vessel")
			}
			// And back: a stage press must not relight.
			w.SetActiveCraftIdx(0)
			if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
				t.Fatalf("StageActive: %v", err)
			}
			if c.ManualBurn != nil {
				t.Error("stage press relit an engine from an order the player walked away from")
			}
		})
	}
}
