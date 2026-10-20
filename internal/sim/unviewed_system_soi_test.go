package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// unviewedCursorWorld parks a Lumen vessel (Primary=kern) 0.3 SOI inside
// Cursor's sphere, then points the view at Sol. Audit 2026-10-18 item 7.
func unviewedCursorWorld(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	cycleToSystem(t, w, "Lumen")
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutKernStackID, ParentBodyID: "kern", AltitudeM: 200e3})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	sys := w.System()
	cursor := sys.FindBody("cursor")
	kern := sys.FindBody("kern")
	soi := physics.SOIRadius(*cursor, *kern)
	cursorRel := w.bodyPosRelTo(*cursor, *kern, w.Clock.SimTime) // Lumen still viewed
	r := cursorRel.Add(cursorRel.Unit().Scale(0.3 * soi))
	// Bound, non-degenerate orbit around kern so the Kepler fast path accepts it.
	v := orbital.Vec3{Z: 1}.Cross(r).Unit().Scale(0.5 * math.Sqrt(kern.GravitationalParameter()/r.Norm()))
	c.State = physics.StateVector{R: r, V: v}
	if c.Primary.ID != "kern" {
		t.Fatalf("precondition: primary %q, want kern", c.Primary.ID)
	}
	w.SystemIdx = 0
	w.Calculator = orbital.ForSystem(w.System())
	return w, c
}

func TestUnviewedSystemVesselSeesMoons(t *testing.T) {
	t.Run("backstop", func(t *testing.T) {
		w, c := unviewedCursorWorld(t)
		w.maybeSwitchPrimaryFor(c)
		if c.Primary.ID != "cursor" {
			t.Errorf("primary = %q, want cursor", c.Primary.ID)
		}
	})
	t.Run("kepler", func(t *testing.T) {
		w, c := unviewedCursorWorld(t)
		w.keplerStepWithSOICheck(c, time.Second)
		if c.Primary.ID != "cursor" {
			t.Errorf("primary = %q, want cursor", c.Primary.ID)
		}
	})
	t.Run("verlet", func(t *testing.T) {
		w, c := unviewedCursorWorld(t)
		w.integrateOneCraft(c, time.Second)
		if c.Primary.ID != "cursor" {
			t.Errorf("primary = %q, want cursor", c.Primary.ID)
		}
	})
}
