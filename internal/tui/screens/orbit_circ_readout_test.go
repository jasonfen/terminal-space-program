package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// PROPELLANT's Δv→circ burn time is the time the circularisation burn
// actually takes (Jason 2026-10-10: "delta V -> circ time readout doesn't
// seem to align to actual"). It was Δv·m/F at today's mass, ignoring the
// fuel the burn sheds: on an S-IVB climbing at 6.4 km/s it read 1m11s
// while the C burn planted 1m01s and flew 1m01s engine-on. The planted
// burn already uses the rocket equation (BurnTimeForDV); the readout now
// reads the same.
func TestCircReadoutBurnTimeMatchesTheFlownBurn(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: spacecraft.LoadoutSIVB1ID, ParentBodyID: "earth", AltitudeM: 150e3}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	c := w.ActiveCraft()
	// Sub-orbital and still climbing: 6.4 km/s across, 400 m/s up.
	up := c.State.R.Unit()
	across := c.State.V.Sub(up.Scale(c.State.V.Dot(up))).Unit()
	c.State.V = across.Scale(6400).Add(up.Scale(400))

	v := NewOrbitView(launchThemeForTest())
	cell := v.deltaVToCircLabel(c)
	plan, err := w.PlanCircularizeAtApoapsis()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cell, readout.DeltaV(plan.DV)) {
		t.Fatalf("setup: Δv→circ %q does not lead with the C burn's %s", cell, readout.DeltaV(plan.DV))
	}
	planted := c.BurnTimeForDV(plan.DV)
	if want := "  " + readout.Duration(planted); !strings.HasSuffix(cell, want) {
		t.Errorf("Δv→circ reads %q; the C burn plants %s", cell, readout.Duration(planted))
	}

	// Fly the planted burn through the real tick and time the engine.
	w.EngageAutoWarp()
	var on time.Duration
	started := false
	for i := 0; i < 2000000; i++ {
		before := w.Clock.SimTime
		w.Tick()
		if c.ActiveBurn != nil {
			started = true
			on += w.Clock.SimTime.Sub(before)
		} else if started {
			break
		}
	}
	if !started {
		t.Fatal("the circularisation burn never fired")
	}
	// The readout prints whole seconds; the flown burn lands within one.
	if d := on - planted; d > time.Second || d < -time.Second {
		t.Errorf("Δv→circ reads %q, but the burn flew %v engine-on", cell, on.Round(100*time.Millisecond))
	}
}
