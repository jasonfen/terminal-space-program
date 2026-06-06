package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// systemIdxByName returns the slate index of the loaded System with the
// given name, failing the test if absent. Systems are name-sorted Sol-first,
// so the index isn't hardcoded — Lumen's position shifts as systems are
// added (ADR 0015).
func systemIdxByName(t *testing.T, w *World, name string) int {
	t.Helper()
	for i := range w.Systems {
		if w.Systems[i].Name == name {
			return i
		}
	}
	t.Fatalf("system %q not loaded (have %d systems)", name, len(w.Systems))
	return -1
}

// cycleToSystem advances the browse view until it lands on the named system.
func cycleToSystem(t *testing.T, w *World, name string) int {
	t.Helper()
	want := systemIdxByName(t, w, name)
	for i := 0; i < len(w.Systems); i++ {
		if w.SystemIdx == want {
			return want
		}
		w.CycleSystem()
	}
	if w.SystemIdx != want {
		t.Fatalf("could not browse to %q (stuck at idx %d)", name, w.SystemIdx)
	}
	return want
}

// TestCraftVisibleHereFollowsBinding — ADR 0015: the flight HUD shows iff
// the Active Vessel is bound to the viewed System, not iff the view is Sol.
func TestCraftVisibleHereFollowsBinding(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if !w.CraftVisibleHere() {
		t.Fatal("seed Sol craft should be visible in the Sol view")
	}
	// Browse to a System the Vessel is NOT bound to: HUD hides.
	w.CycleSystem()
	if w.CraftVisibleHere() {
		t.Fatal("craft should be hidden while browsing away from its System")
	}
	// Browse back to Sol: HUD returns.
	cycleToSystem(t, w, "Sol")
	if !w.CraftVisibleHere() {
		t.Fatal("craft should be visible again after browsing back to Sol")
	}
}

// TestSpawnBindsToViewedSystem — ADR 0015: an orbital or launchpad spawn
// binds the new Vessel to the *viewed* System at spawn time.
func TestSpawnBindsToViewedSystem(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lumenIdx := cycleToSystem(t, w, "Lumen")

	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutKernStackID, ParentBodyID: "kern", AltitudeM: 200e3})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if c.SystemIdx != lumenIdx {
		t.Errorf("orbital spawn SystemIdx = %d, want %d (Lumen)", c.SystemIdx, lumenIdx)
	}
	if c.Primary.ID != "kern" {
		t.Errorf("orbital spawn Primary = %q, want kern", c.Primary.ID)
	}
	// View-follows-active: spawning made the Lumen craft active, so the
	// HUD is visible right away.
	if !w.CraftVisibleHere() {
		t.Error("freshly-spawned Lumen craft should be visible (view follows active)")
	}

	// Launchpad spawn in Lumen binds to Lumen too.
	pad, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutKernStackID, ParentBodyID: "cursor", Launchpad: true})
	if err != nil {
		t.Fatalf("SpawnCraft launchpad: %v", err)
	}
	if pad.SystemIdx != lumenIdx {
		t.Errorf("launchpad spawn SystemIdx = %d, want %d (Lumen)", pad.SystemIdx, lumenIdx)
	}
}

// TestAlongsideSpawnInheritsActiveSystem — ADR 0015: an Alongside spawn
// clones the active Vessel's state, so it inherits the *active* Vessel's
// System, not the viewed one.
func TestAlongsideSpawnInheritsActiveSystem(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lumenIdx := cycleToSystem(t, w, "Lumen")
	active, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutKernStackID, ParentBodyID: "kern", AltitudeM: 200e3})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if active.SystemIdx != lumenIdx {
		t.Fatalf("precondition: active craft not bound to Lumen (got %d)", active.SystemIdx)
	}

	buddy, err := w.SpawnCraft(SpawnSpec{Alongside: true})
	if err != nil {
		t.Fatalf("SpawnCraft alongside: %v", err)
	}
	if buddy.SystemIdx != lumenIdx {
		t.Errorf("Alongside spawn SystemIdx = %d, want %d (inherit active)", buddy.SystemIdx, lumenIdx)
	}
}

// TestSetActiveCraftIdxSnapsView — ADR 0015: switching the Active Vessel
// snaps the camera to its System, recreating the Calculator.
func TestSetActiveCraftIdxSnapsView(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	cycleToSystem(t, w, "Lumen")
	lumenIdx := w.SystemIdx
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutKernStackID, ParentBodyID: "kern", AltitudeM: 200e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	// Spawn made the Lumen craft active (idx 1); the Sol seed is idx 0.
	if w.SystemIdx != lumenIdx {
		t.Fatalf("precondition: view not on Lumen after spawn (got %d)", w.SystemIdx)
	}

	// Switch to the Sol seed: view snaps to Sol, Calculator becomes Solar.
	w.SetActiveCraftIdx(0)
	if w.SystemIdx != 0 {
		t.Errorf("after switching to Sol craft, SystemIdx = %d, want 0", w.SystemIdx)
	}
	if got := w.Calculator.GetSystemType(); got != orbital.SystemTypeSolar {
		t.Errorf("Calculator type = %v, want Solar after snap to Sol", got)
	}

	// Switch back to the Lumen craft: view snaps to Lumen.
	w.SetActiveCraftIdx(1)
	if w.SystemIdx != lumenIdx {
		t.Errorf("after switching to Lumen craft, SystemIdx = %d, want %d", w.SystemIdx, lumenIdx)
	}
}

// TestIntegrateResolvesCraftOwnSystem — ADR 0015: the integrator and the
// SOI backstop resolve bodies against the Vessel's own System. A Vessel
// orbiting a Lumen body must stay on a Lumen body across ticks, never get
// yanked onto a Sol body by a Sol-hardcoded SOI check.
func TestIntegrateResolvesCraftOwnSystem(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lumenIdx := cycleToSystem(t, w, "Lumen")
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutKernStackID, ParentBodyID: "kern", AltitudeM: 200e3})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}

	// Tick well past the 20-tick SOI backstop so both the per-sub-step
	// re-eval and maybeSwitchPrimaryFor run.
	for i := 0; i < 40; i++ {
		w.Tick()
	}

	if c.SystemIdx != lumenIdx {
		t.Errorf("SystemIdx drifted to %d, want %d", c.SystemIdx, lumenIdx)
	}
	// The Primary must still be a body that exists in Lumen — never a Sol
	// body the old Systems[0]-hardcoded SOI check would have switched to.
	if w.Systems[lumenIdx].FindBody(c.Primary.ID) == nil {
		t.Errorf("craft Primary %q is not a Lumen body after ticking — yanked out of its System", c.Primary.ID)
	}
}

// TestStagingInheritsSystemIdx — ADR 0015: a staging-popped passive stage
// stays in its parent's System.
func TestStagingInheritsSystemIdx(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lumenIdx := cycleToSystem(t, w, "Lumen")
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: spacecraft.LoadoutKernStackID, ParentBodyID: "kern", AltitudeM: 200e3})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(c.Stages) < 2 {
		t.Skipf("Kern Stack has %d stage(s); need ≥2 to stage", len(c.Stages))
	}
	activeIdx := w.ActiveCraftIdx
	_, jettisonedIdx, err := w.StageActive(activeIdx)
	if err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	jettisoned := w.Crafts[jettisonedIdx]
	if jettisoned.SystemIdx != lumenIdx {
		t.Errorf("jettisoned stage SystemIdx = %d, want %d (inherit parent)", jettisoned.SystemIdx, lumenIdx)
	}
}
