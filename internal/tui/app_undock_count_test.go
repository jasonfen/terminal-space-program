package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #709, ADR 0063 §4: the undock flash names the one vessel this U released,
// whatever else is on the slate.
func TestUndockFlashNamesTheReleasedVessel(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3}); err != nil {
			t.Fatalf("SpawnCraft: %v", err)
		}
	}
	a.world.DockCrafts(0, 1) // a two-piece stack, plus one bystander on the slate
	a.world.SetActiveCraftIdx(0)
	if len(a.world.Crafts) != 2 {
		t.Fatalf("setup: slate has %d vessels, want stack + bystander", len(a.world.Crafts))
	}

	a, _ = pressUndock(t, a)

	if len(a.world.Crafts) != 3 {
		t.Fatalf("setup: undock left %d vessels, want 3", len(a.world.Crafts))
	}
	if want := "undocked from " + a.world.Crafts[1].Name; a.statusMsg != want {
		t.Errorf("flash = %q, want %q", a.statusMsg, want)
	}
}
