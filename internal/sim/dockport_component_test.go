package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0056 decision 7: a vessel whose top stage was composed with the Docking
// Port component has a port to dock to; without the component it has none.
func TestComposedStageWithDockingPortHasAPortGeometry(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	spawn := func(name string, ids ...string) *spacecraft.Spacecraft {
		st, warn := spacecraft.ComposeStage(ids, spacecraft.Components)
		if warn != "" {
			t.Fatal(warn)
		}
		st.Name = name
		c, err := w.SpawnCraft(SpawnSpec{CustomStages: []spacecraft.Stage{st}, AltitudeM: 400e3})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if g := VesselPortGeometry(spawn("Station Core", "probe-core", "docking-port")); len(g.Ports) != 1 {
		t.Errorf("composed vessel with Docking Port: %d ports, want 1", len(g.Ports))
	}
	if g := VesselPortGeometry(spawn("Plain Core", "probe-core")); len(g.Ports) != 0 {
		t.Errorf("composed vessel without it: %d ports, want 0", len(g.Ports))
	}
}
