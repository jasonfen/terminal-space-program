package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// padSaturn spawns a crewed Saturn V on Earth's pad, radial-out, at the given throttle.
func padSaturn(t *testing.T, throttle float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth",
		Launchpad: true, Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
	})
	if err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	c.Stages[len(c.Stages)-1].CommandSource = spacecraft.CommandCrewed
	c.SyncFields()
	w.SetAttitudeMode(spacecraft.BurnRadialOut)
	w.SetThrottle(throttle)
	return w, c
}

// #716: igniting on the pad with thrust < weight must not destroy the vessel.
func TestPadIgnitionBelowTWR1KeepsVesselSeated(t *testing.T) {
	for _, th := range []float64{0.05, 0.37} {
		w, c := padSaturn(t, th)
		w.StartManualBurn()
		if c.ManualBurn == nil {
			t.Fatalf("throttle %v: engine did not light", th)
		}
		fuel0 := c.ActiveStageFuel()
		for i := 0; i < 50; i++ {
			w.Tick()
		}
		if c.Crashed {
			t.Fatalf("throttle %v: vessel Crashed on the pad", th)
		}
		if c.ManualBurn == nil {
			t.Errorf("throttle %v: burn ended, engine should keep running", th)
		}
		if c.ActiveStageFuel() >= fuel0 {
			t.Errorf("throttle %v: no propellant burned", th)
		}
	}
}

func alt(c *spacecraft.Spacecraft) float64 { return c.State.R.Norm() - c.Primary.RadiusMeters() }

// Throttling up past TWR 1 releases the seated, burning vessel.
func TestPadIgnitionThrottleUpLiftsOff(t *testing.T) {
	w, c := padSaturn(t, 0.05)
	w.StartManualBurn()
	for i := 0; i < 20; i++ {
		w.Tick()
	}
	if !c.Landed || !c.OnPad {
		t.Fatalf("setup: want seated on the pad, Landed=%v OnPad=%v", c.Landed, c.OnPad)
	}
	w.SetThrottle(1)
	for i := 0; i < 100; i++ {
		w.Tick()
	}
	if c.Crashed || c.Landed || c.OnPad || alt(c) < 5 {
		t.Errorf("want lifted off, got Crashed=%v Landed=%v OnPad=%v alt=%.2f", c.Crashed, c.Landed, c.OnPad, alt(c))
	}
}

// TWR just under 1: propellant burned off lightens the stack until it lifts.
func TestPadIgnitionLiftsOffWhenMassBurnsOff(t *testing.T) {
	w, c := padSaturn(t, 0.8) // TWR ~0.99
	w.StartManualBurn()
	w.Tick()
	if !c.Landed {
		t.Fatalf("setup: TWR<1 should stay seated, Landed=%v", c.Landed)
	}
	for i := 0; i < 2000 && c.Landed && !c.Crashed; i++ {
		w.Tick()
	}
	if c.Crashed || c.Landed || alt(c) < 0 {
		t.Errorf("want lifted off as mass burned, got Crashed=%v Landed=%v alt=%.2f", c.Crashed, c.Landed, alt(c))
	}
}

// Seating a burning pad vessel must not make hard impacts survivable.
func TestBurningVesselStillCrashesOnHardImpact(t *testing.T) {
	w, c := padSaturn(t, 0.05)
	c.Landed, c.OnPad = false, false
	setImpactState(c, 200, orbitalUp())
	w.StartManualBurn()
	w.integrateOneCraft(c, w.Clock.BaseStep)
	if !c.Crashed {
		t.Errorf("200 m/s impact under thrust: Crashed=false, Landed=%v", c.Landed)
	}
}

func orbitalUp() orbital.Vec3 { return orbital.Vec3{X: 1} }
