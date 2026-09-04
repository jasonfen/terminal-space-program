package sim

import (
	"testing"
)

// v0.9.0+ tests for the unified World.Target slot.

func TestTargetDefaultsToNone(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.Target.Kind != TargetNone {
		t.Errorf("default target kind: got %v, want TargetNone", w.Target.Kind)
	}
	if name := w.TargetName(); name != "" {
		t.Errorf("TargetName for None: got %q, want empty", name)
	}
}

func TestSetTargetBodyAndClear(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetBody(3) // some non-root body
	if w.Target.Kind != TargetBody || w.Target.BodyIdx != 3 {
		t.Errorf("SetTargetBody(3): got %+v", w.Target)
	}
	w.ClearTarget()
	if w.Target.Kind != TargetNone {
		t.Errorf("after ClearTarget: %+v, want TargetNone", w.Target)
	}
}

func TestSetTargetBodyRejectsRootAndOutOfRange(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetBody(0) // system primary
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetBody(0) should clear: got %+v", w.Target)
	}
	w.SetTargetBody(99999) // out of range
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetBody(99999) should clear: got %+v", w.Target)
	}
}

func TestSetTargetCraftRejectsActiveAndOutOfRange(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetCraft(w.ActiveCraftIdx) // self
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetCraft(active) should clear: got %+v", w.Target)
	}
	w.SetTargetCraft(99999)
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetCraft(99999) should clear: got %+v", w.Target)
	}
	w.SetTargetCraft(-1)
	if w.Target.Kind != TargetNone {
		t.Errorf("SetTargetCraft(-1) should clear: got %+v", w.Target)
	}
}

// bodyIdxForTest resolves a body ID to its index within w's current
// system, failing the test if the catalog doesn't carry it.
func bodyIdxForTest(t *testing.T, w *World, id string) int {
	t.Helper()
	sys := w.System()
	for i, b := range sys.Bodies {
		if b.ID == id {
			return i
		}
	}
	t.Fatalf("body %q not found in current system", id)
	return -1
}

// TestTargetCycleFromLEOMoonFirstThenOtherVessel pins the nearest-first
// order (grilled 2026-09-04, #425; CONTEXT.md §"Target Cycle") from the
// most common starting point: LEO, Earth primary. With one other vessel
// in the slate, the first `t` press must be the Moon (Earth's only moon
// — nearest first) and the second must be the other vessel, since
// moons-of-primary precede other Vessels in the order.
func TestTargetCycleFromLEOMoonFirstThenOtherVessel(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil || c.Primary.ID != "earth" {
		t.Fatalf("expected the starter craft's primary to be earth, got %+v", c)
	}
	moonIdx := bodyIdxForTest(t, w, "moon")

	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	// SpawnCraft makes the new craft active (idx 1); switch back to the
	// original LEO craft (idx 0) so its own primary (Earth) governs the
	// cycle, with the spawned craft as the "other vessel".
	w.SetActiveCraftIdx(0)
	w.ClearTarget()

	w.CycleTarget(true)
	if w.Target.Kind != TargetBody || w.Target.BodyIdx != moonIdx {
		t.Errorf("first `t` from LEO: got %+v, want the Moon (idx %d)", w.Target, moonIdx)
	}
	w.CycleTarget(true)
	if w.Target.Kind != TargetCraft || w.Target.CraftID != w.Crafts[1].ID {
		t.Errorf("second `t` from LEO: got %+v, want the other vessel (ID %d)", w.Target, w.Crafts[1].ID)
	}
}

// TestTargetCycleFromLunarOrbitFirstIsEarth: from lunar orbit (primary =
// the Moon), Earth has no other moons, so the first press skips straight
// to the "remaining bodies" section — and since Earth is the Moon's own
// parent and a top-level body, it's the very first entry there.
func TestTargetCycleFromLunarOrbitFirstIsEarth(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.Primary = bodyForTest(t, w, "moon")
	earthIdx := bodyIdxForTest(t, w, "earth")

	w.ClearTarget()
	w.CycleTarget(true)
	if w.Target.Kind != TargetBody || w.Target.BodyIdx != earthIdx {
		t.Errorf("first `t` from lunar orbit: got %+v, want Earth (idx %d)", w.Target, earthIdx)
	}
}

// TestTargetCycleNeverTargetsOwnPrimary: the active Vessel's own primary
// must never appear anywhere in the cycle — from lunar orbit, the Moon
// itself is skipped entirely.
func TestTargetCycleNeverTargetsOwnPrimary(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.Primary = bodyForTest(t, w, "moon")
	moonIdx := bodyIdxForTest(t, w, "moon")

	w.ClearTarget()
	seen := map[Target]bool{{Kind: TargetNone}: true}
	for i := 0; i < len(w.System().Bodies)+len(w.Crafts)+2; i++ {
		w.CycleTarget(true)
		if seen[w.Target] {
			break // wrapped back to an already-seen entry
		}
		seen[w.Target] = true
		if w.Target.Kind == TargetBody && w.Target.BodyIdx == moonIdx {
			t.Fatalf("own primary (the Moon) appeared in the cycle: %+v", w.Target)
		}
	}
}

// TestTargetCycleLumenSystemMoonsFirst: the rule holds outside Sol too.
// Kern (Lumen system) carries two moons, cursor and glyph; from a craft
// parked in orbit of Kern itself, the first two `t` presses must be
// those moons (nearest first), exactly as the Moon is first from Earth
// orbit in Sol.
func TestTargetCycleLumenSystemMoonsFirst(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	lumenIdx := -1
	for i, sys := range w.Systems {
		if sys.FindBody("kern") != nil {
			lumenIdx = i
		}
	}
	if lumenIdx < 0 {
		t.Skip("no system carries kern (Lumen catalog absent)")
	}
	w.SystemIdx = lumenIdx
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.SystemIdx = lumenIdx
	c.Primary = bodyForTest(t, w, "kern")

	cursorIdx := bodyIdxForTest(t, w, "cursor")
	glyphIdx := bodyIdxForTest(t, w, "glyph")

	w.ClearTarget()
	w.CycleTarget(true)
	if w.Target.Kind != TargetBody || w.Target.BodyIdx != cursorIdx {
		t.Errorf("first `t` from Kern orbit: got %+v, want cursor (idx %d)", w.Target, cursorIdx)
	}
	w.CycleTarget(true)
	if w.Target.Kind != TargetBody || w.Target.BodyIdx != glyphIdx {
		t.Errorf("second `t` from Kern orbit: got %+v, want glyph (idx %d)", w.Target, glyphIdx)
	}
}

// TestTargetCycleSurvivesPrimaryChange: the bound Target itself must
// never jump when the active Vessel's primary changes (an SOI
// transition) — only the CYCLE ORDER re-derives. CycleTarget finds the
// current index by matching the Target value, not a stored index, so a
// bound Target that's still present in the (re-ordered) cycle stays put.
func TestTargetCycleSurvivesPrimaryChange(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	marsIdx := bodyIdxForTest(t, w, "mars")
	w.SetTargetBody(marsIdx)
	before := w.Target

	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.Primary = bodyForTest(t, w, "moon") // simulate an SOI transition

	if w.Target != before {
		t.Errorf("Target changed on primary change alone: got %+v, want unchanged %+v", w.Target, before)
	}
	// A subsequent cycle should still find it by value and step from
	// there, not treat it as vanished.
	w.CycleTarget(true)
	if w.Target == before {
		t.Errorf("CycleTarget did not advance past the pre-existing bound target")
	}
}

func TestTargetStateForBodyMatchesBodyPosition(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Pick the last body in the system — keeps the assertion robust
	// to body-order changes.
	idx := len(w.System().Bodies) - 1
	w.SetTargetBody(idx)
	st, ok := w.TargetState()
	if !ok {
		t.Fatal("TargetState for body: ok=false")
	}
	want := w.BodyPosition(w.System().Bodies[idx])
	if st.R.Sub(want).Norm() > 1e-6 {
		t.Errorf("TargetState.R: got %+v, want %+v", st.R, want)
	}
}

func TestTargetStateForNoneReturnsNotOk(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ClearTarget()
	if _, ok := w.TargetState(); ok {
		t.Error("TargetState for None: ok=true, want false")
	}
}

// TestPerCraftTargetPersistsAcrossSwitch covers the v0.9.3 polish
// that gives each craft its own target binding. Pre-polish, pressing
// `T` to set a target on craft A would also surface that target on
// craft B (single shared World.Target slot). Post-polish, each craft
// has a Target field; the world-level live cursor is synced from the
// active craft on switch via SetActiveCraftIdx.
func TestPerCraftTargetPersistsAcrossSwitch(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	// Spawn lands the player on the new craft (idx 1). Bind a body
	// target on craft 1, then a different body target on craft 0,
	// and verify each survives a switch back.
	w.SetActiveCraftIdx(1)
	w.SetTargetBody(3)
	craft1Target := w.Target

	w.SetActiveCraftIdx(0)
	if w.Target.Kind == TargetBody && w.Target.BodyIdx == 3 {
		t.Errorf("switching to craft 0: world Target leaked from craft 1: %+v", w.Target)
	}
	w.SetTargetBody(5)
	craft0Target := w.Target

	w.SetActiveCraftIdx(1)
	if w.Target != craft1Target {
		t.Errorf("after switch back to craft 1: got %+v, want %+v", w.Target, craft1Target)
	}

	w.SetActiveCraftIdx(0)
	if w.Target != craft0Target {
		t.Errorf("after switch back to craft 0: got %+v, want %+v", w.Target, craft0Target)
	}
}

// TestPerCraftTargetMirroredOnEverySetter confirms the
// world-level cursor and the active craft's stored Target stay in
// lockstep — every mutator (SetTargetBody / SetTargetCraft /
// ClearTarget / CycleTarget) must mirror through to the active
// craft so a subsequent switch checkpoints the right value.
func TestPerCraftTargetMirroredOnEverySetter(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	active := w.ActiveCraft()
	if active == nil {
		t.Fatal("active craft nil after spawn")
	}
	w.SetTargetBody(3)
	if active.Target != w.Target {
		t.Errorf("SetTargetBody mirror: craft.Target=%+v, w.Target=%+v", active.Target, w.Target)
	}
	w.ClearTarget()
	if active.Target != w.Target {
		t.Errorf("ClearTarget mirror: craft.Target=%+v, w.Target=%+v", active.Target, w.Target)
	}
	w.CycleTarget(true)
	if active.Target != w.Target {
		t.Errorf("CycleTarget mirror: craft.Target=%+v, w.Target=%+v", active.Target, w.Target)
	}
}

func TestTargetNameForBody(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	idx := len(w.System().Bodies) - 1
	w.SetTargetBody(idx)
	if got, want := w.TargetName(), w.System().Bodies[idx].EnglishName; got != want {
		t.Errorf("TargetName: got %q, want %q", got, want)
	}
}
