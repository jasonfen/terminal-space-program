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
	if math.Abs(alt-500e3) > 1 {
		t.Errorf("initial altitude %.1f m, want 500000 (v0.6.1+)", alt)
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

// TestCraftTrailGrowsAndCaps: v0.5.2 — sampling at trailIntervalSec
// of sim time, not per tick. Ticks at warp=1 should sparsely add to
// the trail; once trailCap entries are recorded, the buffer stops
// growing and oldest entries get overwritten in FIFO order.
func TestCraftTrailGrowsAndCaps(t *testing.T) {
	w, _ := NewWorld()
	if got := len(w.CraftTrail()); got != 0 {
		t.Fatalf("fresh world trail = %d, want 0", got)
	}

	// Force a known sim-time advance per Tick. BaseStep=50ms × warp=1
	// gives 0.05 s per tick. Need 200 sim seconds per sample at the
	// default interval — i.e., 200 ticks for one sample.
	w.Clock.BaseStep = 1 * time.Second // sim-step 1s/tick at warp=1
	// 11 ticks × 1 s = 11 s ≥ trailIntervalSec (10) → 1 sample.
	for i := 0; i < 11; i++ {
		w.Tick()
	}
	if got := len(w.CraftTrail()); got != 1 {
		t.Errorf("after 11 sim seconds: trail = %d, want 1", got)
	}

	// Push past trailCap to verify the buffer caps and rotates.
	for i := 0; i < trailCap*15; i++ {
		w.Tick()
	}
	if got := len(w.CraftTrail()); got != trailCap {
		t.Errorf("after >>trailCap samples: trail = %d, want %d", got, trailCap)
	}

	// Newest sample (last in returned slice) should equal the live
	// craft inertial position within one sample's worth of motion —
	// the most recent recorded sample was taken at most trailIntervalSec
	// ago, so a small (sub-orbit-fraction) gap is expected for LEO.
	tr := w.CraftTrail()
	last := tr[len(tr)-1]
	live := w.CraftInertial()
	gap := last.Sub(live).Norm()
	// LEO speed ~7.78 km/s; 10 sim seconds covers ~78 km. Allow 2x.
	if gap > 200e3 {
		t.Errorf("newest trail sample too far from live craft: %.1f km", gap/1000)
	}
}

// TestCraftTrailFollowsPrimaryFrame: regression for the v0.5.4 fix.
// Pre-fix the trail stored heliocentric inertial samples; LEO trail
// samples taken minutes apart appeared displaced by Earth's motion
// (~30 km/s × elapsed sim time). Storing primary-relative R + re-
// translating via current BodyPosition keeps the trail aligned with
// the craft's apparent orbit around its primary.
//
// Test: run enough ticks for several samples to land, then verify
// that *every* sample's distance from current Earth position is
// within LEO orbit radius (a few ×10^6 m), not the AU-scale offsets
// produced by the heliocentric-trail bug.
func TestCraftTrailFollowsPrimaryFrame(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.BaseStep = 60 * time.Second // 1 sample per tick (60s ≥ 10s interval)
	for i := 0; i < trailCap; i++ {
		w.Tick()
	}
	tr := w.CraftTrail()
	if len(tr) != trailCap {
		t.Fatalf("expected full trail (%d), got %d", trailCap, len(tr))
	}
	earthPos := w.BodyPosition(w.Craft.Primary)
	craftR := w.Craft.State.R.Norm()
	// Allow 2× LEO radius for trail spread over ticks; bug would
	// produce 1e8+ m displacements.
	maxAllowed := 2 * craftR
	for i, p := range tr {
		dist := p.Sub(earthPos).Norm()
		if dist > maxAllowed {
			t.Errorf("trail sample %d: distance from Earth %.3e m > %.3e m (heliocentric drift bug?)",
				i, dist, maxAllowed)
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
