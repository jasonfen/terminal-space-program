package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// addVesselAt appends a vessel on the active craft's orbit, offsetM metres
// along +X from it, and returns it.
func addVesselAt(t *testing.T, w *World, name string, offsetM float64) *spacecraft.Spacecraft {
	t.Helper()
	a := w.Crafts[0]
	b := spacecraft.NewFromLoadout(spacecraft.LoadoutSIVB1ID)
	b.Name = name
	b.Primary = a.Primary
	b.State = physics.StateVector{R: a.State.R.Add(orbital.Vec3{X: offsetM}), V: a.State.V, M: b.TotalMass()}
	w.Crafts = append(w.Crafts, b)
	w.stampCraftID(b)
	return b
}

// pressesTo returns how many `t` presses from no target reach the craft with
// id, or -1 within limit presses.
func pressesTo(w *World, id uint64, limit int) int {
	w.ClearTarget()
	for n := 1; n <= limit; n++ {
		w.CycleTarget(true)
		if w.Target.Kind == TargetCraft && w.Target.CraftID == id {
			return n
		}
	}
	return -1
}

// TestTargetCycleVesselsNearestFirst: with the Moon, a vessel 12 km away
// (earlier in the slate) and a vessel 2 km away (later), `t` must walk Moon,
// the 2 km vessel, the 12 km vessel (triage T2: it walked the far one first).
func TestTargetCycleVesselsNearestFirst(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	moonIdx := bodyIdxForTest(t, w, "moon")
	far := addVesselAt(t, w, "far", 12e3)
	near := addVesselAt(t, w, "near", 2e3)
	w.SetActiveCraftIdx(0)
	w.ClearTarget()

	w.CycleTarget(true)
	if w.Target.Kind != TargetBody || w.Target.BodyIdx != moonIdx {
		t.Fatalf("press 1: %+v, want the Moon", w.Target)
	}
	w.CycleTarget(true)
	if w.Target.Kind != TargetCraft || w.Target.CraftID != near.ID {
		t.Errorf("press 2: %+v, want the 2 km vessel (ID %d)", w.Target, near.ID)
	}
	w.CycleTarget(true)
	if w.Target.Kind != TargetCraft || w.Target.CraftID != far.ID {
		t.Errorf("press 3: %+v, want the 12 km vessel (ID %d)", w.Target, far.ID)
	}
}

// TestTargetCycleReachesUndockedVesselQuickly: after an undock the restored
// vessels are 75 m apart, so the other one must be within two presses (Moon,
// then it), never a walk out to the outer planets (docking grill: 12 presses).
func TestTargetCycleReachesUndockedVesselQuickly(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock refused")
	}
	other := 1 - w.ActiveCraftIdx
	if got := pressesTo(w, w.Crafts[other].ID, 40); got < 1 || got > 2 {
		t.Errorf("presses to reach the undocked vessel = %d, want <= 2", got)
	}
}
