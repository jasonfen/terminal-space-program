package save

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestDryOrderIsTransientAcrossSaveLoad (#466): the dry-armed standing
// order is deliberately not persisted (ManualBurn and Throttle are not
// either), so a reloaded vessel has cold engines and no order, and cannot
// pin warp. Pins that a saved dry-armed vessel does not come back armed.
func TestDryOrderIsTransientAcrossSaveLoad(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "Descent", DryMass: 500, FuelMass: 0, FuelCapacity: 20, Thrust: 200000, Isp: 250},
		{Name: "Ascent", DryMass: 800, FuelMass: 4000, FuelCapacity: 4000, Thrust: 100000, Isp: 300},
	}
	c.SyncFields()
	c.Throttle = 1
	c.DryOrder = true
	if !sim.StackDryArmed(c) {
		t.Fatal("setup: not dry-armed")
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}
	w2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c2 := w2.ActiveCraft()
	if c2.DryOrder || sim.StackDryArmed(c2) {
		t.Error("dry-armed order survived save/load; it is meant to be transient")
	}
	if c2.ManualBurn != nil || w2.AnyCraftThrusting() {
		t.Error("loaded vessel is thrusting")
	}
}
