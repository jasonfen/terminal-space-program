package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSpaceRelightsDryOrderThroughKeys (#466): drives the real key path.
// `b` lights the descent engine, the tiny tank runs dry on its own, the
// throttle order stands (DRY, not thrusting), and ONE `space` drops the
// stage and lights the next engine at the same throttle.
func TestSpaceRelightsDryOrderThroughKeys(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "Descent", DryMass: 500, FuelMass: 20, FuelCapacity: 20, Thrust: 200000, Isp: 250, CommandSource: spacecraft.CommandCrewed},
		{Name: "Ascent", DryMass: 800, FuelMass: 4000, FuelCapacity: 4000, Thrust: 100000, Isp: 300, CommandSource: spacecraft.CommandCrewed},
	}
	c.SyncFields()
	c.State.M = c.TotalMass()
	c.Throttle = 1

	pressKey(a, 'b')
	if c.ManualBurn == nil {
		t.Fatal("setup: `b` did not light the engine")
	}
	for i := 0; i < 400 && c.ActiveStageFuel() > 0; i++ {
		a.world.Tick()
	}
	a.world.Tick()
	if c.ManualBurn != nil || !sim.StackDryArmed(c) {
		t.Fatalf("setup: want dry-armed, burn=%v armed=%v", c.ManualBurn != nil, sim.StackDryArmed(c))
	}
	if a.world.AnyCraftThrusting() {
		t.Fatal("dry-armed counted as thrusting")
	}

	pressKey(a, ' ')
	if c.ManualBurn == nil {
		t.Fatalf("space did not relight (flash %q)", a.statusMsg)
	}
	if c.Name != "Ascent" || c.EffectiveThrottle() != 1 {
		t.Errorf("after space: stage %q throttle %v, want Ascent at 1", c.Name, c.EffectiveThrottle())
	}
}
