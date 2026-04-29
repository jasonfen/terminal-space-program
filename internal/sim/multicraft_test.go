package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestMultiCraftBothPropagate: with two craft in the slate, both
// must advance under their own primary's gravity each tick. Active
// status doesn't gate Verlet — only thrust.
func TestMultiCraftBothPropagate(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("Earth")
	if earth == nil {
		t.Skip("Earth missing from Sol")
	}
	// Stand up a second craft 90° around Earth from the first, in the
	// same 500 km circular orbit. Both should advance the same way.
	mu := earth.GravitationalParameter()
	r := earth.RadiusMeters() + 500e3
	v := math.Sqrt(mu / r)
	c2 := spacecraft.NewInLEO(*earth)
	c2.State = physics.StateVector{
		R: orbital.Vec3{Y: r},
		V: orbital.Vec3{X: -v},
		M: c2.TotalMass(),
	}
	w.Crafts = append(w.Crafts, c2)

	r0a := w.Crafts[0].State.R
	r0b := w.Crafts[1].State.R

	// Spin one tick.
	w.Tick()

	// Both craft should have moved (state.R changed).
	if w.Crafts[0].State.R.Sub(r0a).Norm() == 0 {
		t.Error("active craft did not advance")
	}
	if w.Crafts[1].State.R.Sub(r0b).Norm() == 0 {
		t.Error("non-active craft did not advance — multi-craft Tick is not propagating it")
	}
}

// TestNonActiveCraftDoesNotConsumeFuel: a manual burn on the active
// craft must only consume the active craft's fuel.
func TestNonActiveCraftDoesNotConsumeFuel(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	c2 := spacecraft.NewInLEO(*earth)
	w.Crafts = append(w.Crafts, c2)
	f0Other := c2.Fuel

	w.SetThrottle(1.0)
	w.SetAttitudeMode(spacecraft.BurnPrograde)
	w.StartManualBurn()
	// Run a tick to advance the burn.
	w.Tick()

	if c2.Fuel != f0Other {
		t.Errorf("non-active craft burned fuel during manual burn on active: %v → %v", f0Other, c2.Fuel)
	}
	w.StopManualBurn()
}

// TestCycleActiveCraftWraps: [/] cycling must wrap forward and back.
func TestCycleActiveCraftWraps(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	w.Crafts = append(w.Crafts, spacecraft.NewInLEO(*earth), spacecraft.NewInLEO(*earth))
	if len(w.Crafts) != 3 {
		t.Fatalf("expected 3 craft, got %d", len(w.Crafts))
	}

	w.CycleActiveCraft(1)
	if w.ActiveCraftIdx != 1 {
		t.Errorf("after +1 = %d, want 1", w.ActiveCraftIdx)
	}
	w.CycleActiveCraft(1)
	if w.ActiveCraftIdx != 2 {
		t.Errorf("after +2 = %d, want 2", w.ActiveCraftIdx)
	}
	w.CycleActiveCraft(1)
	if w.ActiveCraftIdx != 0 {
		t.Errorf("wrap forward to 0 = %d", w.ActiveCraftIdx)
	}
	w.CycleActiveCraft(-1)
	if w.ActiveCraftIdx != 2 {
		t.Errorf("wrap backward to 2 = %d", w.ActiveCraftIdx)
	}
}

// TestCycleSingleCraftIsNoOp: with only one craft, [/] can't cycle.
func TestCycleSingleCraftIsNoOp(t *testing.T) {
	w, _ := NewWorld()
	if len(w.Crafts) != 1 {
		t.Fatalf("expected 1 craft from NewWorld, got %d", len(w.Crafts))
	}
	w.CycleActiveCraft(1)
	if w.ActiveCraftIdx != 0 {
		t.Errorf("cycling single-craft world changed idx: %d", w.ActiveCraftIdx)
	}
}

// TestActiveCraftAccessor: out-of-range / empty Crafts → nil.
func TestActiveCraftAccessor(t *testing.T) {
	w := &World{}
	if w.ActiveCraft() != nil {
		t.Error("empty Crafts should return nil")
	}
	w, _ = NewWorld()
	w.ActiveCraftIdx = 99
	if w.ActiveCraft() != nil {
		t.Error("out-of-range ActiveCraftIdx should return nil")
	}
}

// TestBurnFiresOnPlantedCraftNotActive (v0.8.1 regression): when a
// burn is planted on craft A, then the player switches to craft B
// before the burn triggers, the burn must still fire on craft A.
// Pre-fix the burn followed the active craft via a shared
// World.Nodes / World.ActiveBurn — switching active would move the
// in-flight burn to the wrong vessel.
func TestBurnFiresOnPlantedCraftNotActive(t *testing.T) {
	w, _ := NewWorld()
	craftA := w.ActiveCraft()
	speedAbefore := craftA.State.V.Norm()

	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	craftB := w.ActiveCraft()
	if craftA == craftB {
		t.Fatal("active craft didn't change after spawn")
	}
	speedBbefore := craftB.State.V.Norm()

	// Switch back to craft A and plant an impulsive prograde burn at
	// now+1 second.
	w.ActiveCraftIdx = 0
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(1 * time.Second),
		Mode:        spacecraft.BurnPrograde,
		DV:          50,
	})

	// Switch to craft B and advance sim until the burn fires.
	w.ActiveCraftIdx = 1
	for i := 0; i < 200; i++ {
		w.Tick()
		if len(craftA.Nodes) == 0 {
			break
		}
	}
	if len(craftA.Nodes) != 0 {
		t.Fatal("planted burn never fired")
	}

	// Compare speeds (|v| magnitude). Free flight conserves speed
	// at any single moment in a circular orbit (kinetic energy is
	// constant); only thrust changes |v|. Craft A's |v| should have
	// jumped by ~50 m/s; craft B's |v| should be ~unchanged.
	dvA := craftA.State.V.Norm() - speedAbefore
	if dvA < 45 || dvA > 55 {
		t.Errorf("craft A's |v| change = %.2f m/s, expected ~50 m/s prograde burn", dvA)
	}
	dvB := math.Abs(craftB.State.V.Norm() - speedBbefore)
	if dvB > 1 {
		t.Errorf("non-active craft B's |v| changed by %.2f m/s — burn leaked", dvB)
	}
}

// TestSpawnSisterCraftPopulatesAndActivates: pressing `n`-equivalent
// spawns a copy of the active craft, increments the slate count, and
// activates the new one.
func TestSpawnSisterCraftPopulatesAndActivates(t *testing.T) {
	w, _ := NewWorld()
	beforeCount := len(w.Crafts)
	beforeIdx := w.ActiveCraftIdx

	c, err := w.SpawnSisterCraft()
	if err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	if len(w.Crafts) != beforeCount+1 {
		t.Errorf("slate count = %d, want %d", len(w.Crafts), beforeCount+1)
	}
	if w.ActiveCraftIdx == beforeIdx {
		t.Errorf("ActiveCraftIdx didn't advance: %d (was %d)", w.ActiveCraftIdx, beforeIdx)
	}
	if c.Primary.ID != w.Crafts[beforeIdx].Primary.ID {
		t.Errorf("sister primary %q != original primary %q", c.Primary.ID, w.Crafts[beforeIdx].Primary.ID)
	}
	if c.Monoprop != c.MonopropCapacity {
		t.Errorf("sister should ship full monoprop, got %v / %v", c.Monoprop, c.MonopropCapacity)
	}
}

// TestMultiCraftIntegrateAdvancesBothByAFullTick: confirm that after
// a Tick of duration D, both craft have moved a distance roughly
// matching their tangential speed × D.
func TestMultiCraftIntegrateAdvancesBothByAFullTick(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	c2 := spacecraft.NewInLEO(*earth)
	w.Crafts = append(w.Crafts, c2)

	c1Before := w.Crafts[0].State.R
	c2Before := w.Crafts[1].State.R

	// Integrate 60 s of sim-time.
	w.Crafts[0].State.M = w.Crafts[0].TotalMass()
	w.Crafts[1].State.M = w.Crafts[1].TotalMass()
	w.Clock.SimTime = w.Clock.SimTime.Add(60 * time.Second)
	w.integrateOneCraft(w.Crafts[0], 60*time.Second)
	w.integrateOneCraft(w.Crafts[1], 60*time.Second)

	d1 := w.Crafts[0].State.R.Sub(c1Before).Norm()
	d2 := w.Crafts[1].State.R.Sub(c2Before).Norm()
	// In 60 s of LEO at ~7600 m/s, expect ~456 km of arc displacement.
	const minMove = 100e3 // 100 km
	if d1 < minMove {
		t.Errorf("active craft moved only %.0f m in 60 s — expected > %.0f m", d1, minMove)
	}
	if d2 < minMove {
		t.Errorf("non-active craft moved only %.0f m in 60 s — expected > %.0f m", d2, minMove)
	}
}
