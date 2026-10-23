package sim

import (
	"errors"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// plantAndFly plants a prograde node of dv a minute ahead and ticks until it ignites.
func plantAndFly(t *testing.T, w *World, c *spacecraft.Spacecraft, dv float64) {
	t.Helper()
	w.PlanNode(spacecraft.ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(time.Minute),
		Mode:        spacecraft.BurnPrograde, DV: dv, Duration: c.BurnTimeForDV(dv), PrimaryID: c.Primary.ID,
	})
	if _, ok := tickUntil(w, 100_000, func() bool { return c.ActiveBurn != nil }); !ok {
		t.Fatal("the node never ignited")
	}
}

// ADR 0058 review finding 1: a node that finishes its Δv while the solids
// burn cuts the core and leaves a manual burn carrying the solids to empty,
// exactly like b with a solid lit. space stays refused until burnout.
func TestPlantedBurnLeavesSolidsBurning(t *testing.T) {
	w, c := slsWorld(t)
	plantAndFly(t, w, c, 200)
	if _, ok := tickUntil(w, 100_000, func() bool { return c.ActiveBurn == nil }); !ok {
		t.Fatal("the node never finished")
	}
	if c.Stages[0].FuelMass <= 0 {
		t.Fatal("setup: solids already spent when the node ended")
	}
	if !c.SolidLit() || c.ManualBurn == nil {
		t.Fatalf("node ended with solids lit: SolidLit=%v ManualBurn=%v, want a manual burn alive", c.SolidLit(), c.ManualBurn != nil)
	}
	if c.Throttle != 0 {
		t.Errorf("core throttle = %v, want 0", c.Throttle)
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); !errors.Is(err, ErrStageSolidsLit) {
		t.Errorf("space after the node: err = %v, want ErrStageSolidsLit", err)
	}
	w.ToggleManualBurn() // b must not relight anything
	if c.Throttle != 0 || c.ManualBurn == nil {
		t.Errorf("b after the node: throttle %v, burn alive %v", c.Throttle, c.ManualBurn != nil)
	}
	srb, core := c.Stages[0].FuelMass, c.Stages[1].FuelMass
	w.Tick()
	if c.Stages[0].FuelMass >= srb {
		t.Errorf("SRB fuel %v -> %v, want dropping", srb, c.Stages[0].FuelMass)
	}
	if c.Stages[1].FuelMass != core {
		t.Errorf("core burned while cut: %v -> %v", core, c.Stages[1].FuelMass)
	}
	if _, ok := tickUntil(w, 100_000, func() bool { return c.ManualBurn == nil }); !ok {
		t.Fatal("the solids never burned out")
	}
	if c.Stages[0].FuelMass != 0 {
		t.Errorf("burn ended with %v kg of solid left", c.Stages[0].FuelMass)
	}
}
