package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestWorldSpawnsSpacecraft: NewWorld places the craft in a valid LEO
// around Earth with the expected altitude and speed.
func TestWorldSpawnsSpacecraft(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.Craft == nil {
		t.Fatal("Craft is nil")
	}
	if w.Craft.Primary.EnglishName != "Earth" {
		t.Errorf("expected primary=Earth, got %s", w.Craft.Primary.EnglishName)
	}
	alt := w.Craft.Altitude()
	if math.Abs(alt-200e3) > 1 {
		t.Errorf("initial altitude %.1f m, want 200000", alt)
	}
}

// TestWorldIntegrationStableOverOrbits: the plan's C15 acceptance — 100
// orbits, SMA drift < 1%. Mirrors verlet_test but through the world-tick
// path, which is what the TUI exercises.
func TestWorldIntegrationStableOverOrbits(t *testing.T) {
	w, _ := NewWorld()
	mu := w.Craft.Primary.GravitationalParameter()
	r0 := w.Craft.State.R.Norm()
	a0 := r0 // circular

	period := 2 * math.Pi * math.Sqrt(r0*r0*r0/mu)
	// Advance by 100 orbits of sim-time.
	// BaseStep=50ms, warp=1× means wall-tick ≈ sim-tick. Use a direct
	// test-only Advance path by calling Tick repeatedly with the clock
	// set to warp out enough per tick.
	w.Clock.WarpIdx = 2 // 100× — 5 s sim per tick
	totalSimSecs := 100 * period
	ticksNeeded := int(math.Ceil(totalSimSecs / (w.Clock.BaseStep.Seconds() * w.Clock.Warp())))

	for i := 0; i < ticksNeeded; i++ {
		w.Tick()
	}

	// Re-derive SMA from final state (vis-viva via physics package).
	vf := w.Craft.State.V.Norm()
	rf := w.Craft.State.R.Norm()
	eps := 0.5*vf*vf - mu/rf
	af := -mu / (2 * eps)

	drift := math.Abs(af-a0) / a0
	if drift > 0.01 {
		t.Errorf("SMA drift after 100 orbits via world.Tick: %.4f%% (>1%%)", drift*100)
	}
	t.Logf("world.Tick: 100 LEO orbits at 100× warp, Δa/a = %.3e", drift)
}

// findBody is a test helper — w.System() returns by value, so the
// pointer-receiver FindBody isn't directly callable.
func findBody(w *World, name string) *bodies_CelestialBody {
	sys := w.System()
	return sys.FindBody(name)
}

type bodies_CelestialBody = bodies.CelestialBody

// TestBodyPositionRecursesThroughHierarchy: v0.5.0 added moons with
// ParentID. Luna's BodyPosition must be Earth_position + Luna's
// position relative to Earth, not Luna_elements interpreted directly
// in heliocentric coords. A Moon at 384k km from Earth would otherwise
// render as 384k km from the Sun — closer than Mercury.
func TestBodyPositionRecursesThroughHierarchy(t *testing.T) {
	w, _ := NewWorld()
	moonPtr := findBody(w,"Moon")
	earthPtr := findBody(w,"Earth")
	if moonPtr == nil || earthPtr == nil {
		t.Skip("Moon or Earth missing from Sol")
	}
	rMoon := w.BodyPosition(*moonPtr)
	rEarth := w.BodyPosition(*earthPtr)
	dist := rMoon.Sub(rEarth).Norm()
	// Luna's semimajor is 384399 km. Allow ~7% for current eccentricity
	// (e=0.0549 → swing of ~21k km).
	if dist < 360e6 || dist > 410e6 {
		t.Errorf("Moon-Earth distance = %.1f km, want ~384k (semimajor)", dist/1000)
	}
	// Sanity: Moon's heliocentric distance ≈ Earth's, within Luna's
	// orbit radius. Catches the regression where BodyPosition treated
	// Luna's elements as heliocentric directly.
	if math.Abs(rMoon.Norm()-rEarth.Norm()) > 410e6 {
		t.Errorf("Moon's heliocentric |r| differs from Earth's by %.1f km > Luna's orbit",
			(rMoon.Norm()-rEarth.Norm())/1000)
	}
}

// TestFindPrimaryNestedSOIWalk: a craft inside Luna's SOI must be
// owned by Luna, not Earth. v0.5.0 SOIRadius now uses each body's
// actual parent (Luna→Earth, Earth→Sun) so Luna's SOI sizes
// correctly to ~66 000 km — large enough to dominate at close range.
// Pre-v0.5.0 Luna's SOI was computed against the Sun (~1.5e11 m
// semimajor times mass ratio), giving an absurd value.
func TestFindPrimaryNestedSOIWalk(t *testing.T) {
	w, _ := NewWorld()
	sys := w.System()
	moon := findBody(w,"Moon")
	if moon == nil {
		t.Skip("Moon missing from Sol")
	}
	moonPos := w.BodyPosition(*moon)

	// Place craft 5000 km from Luna (well inside Luna's ~66k km SOI,
	// well inside Earth's ~924k km SOI — Luna should win).
	craftInertial := moonPos.Add(orbital.Vec3{X: 5000e3})

	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}
	prim := physics.FindPrimary(sys, craftInertial, positions)
	if prim.Body.EnglishName != "Moon" {
		t.Errorf("near-Luna craft primary = %s, want Moon", prim.Body.EnglishName)
	}
}

// TestFindPrimaryGalileanMultiMoon: Jupiter has four moons (Io,
// Europa, Ganymede, Callisto). FindPrimary near each must select
// the correct moon, not whichever appears first in iteration. A
// hierarchy-naive implementation that mis-sized Galilean SOIs
// against the Sun would either pick none or pick by iteration order.
func TestFindPrimaryGalileanMultiMoon(t *testing.T) {
	w, _ := NewWorld()
	sys := w.System()
	for _, name := range []string{"Io", "Europa", "Ganymede", "Callisto"} {
		moon := findBody(w,name)
		if moon == nil {
			t.Errorf("%s missing from Sol", name)
			continue
		}
		moonPos := w.BodyPosition(*moon)
		// 100 km from moon center — well inside any Galilean SOI.
		craftInertial := moonPos.Add(orbital.Vec3{X: 100e3})

		positions := make(map[string]orbital.Vec3, len(sys.Bodies))
		for _, b := range sys.Bodies {
			positions[b.ID] = w.BodyPosition(b)
		}
		prim := physics.FindPrimary(sys, craftInertial, positions)
		if prim.Body.EnglishName != name {
			t.Errorf("near-%s craft primary = %s, want %s", name, prim.Body.EnglishName, name)
		}
	}
}

// TestPausedTickDoesNothing: world.Clock.Paused must block both time
// advancement and physics stepping.
func TestPausedTickDoesNothing(t *testing.T) {
	w, _ := NewWorld()
	r0 := w.Craft.State.R
	t0 := w.Clock.SimTime
	w.Clock.Paused = true
	w.Clock.BaseStep = 50 * time.Millisecond
	for i := 0; i < 10; i++ {
		w.Tick()
	}
	if w.Craft.State.R != r0 {
		t.Errorf("paused: craft moved from %v to %v", r0, w.Craft.State.R)
	}
	if !w.Clock.SimTime.Equal(t0) {
		t.Errorf("paused: sim-time advanced from %v to %v", t0, w.Clock.SimTime)
	}
}
