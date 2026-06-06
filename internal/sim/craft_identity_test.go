package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// threeCraftSlate spawns two siblings so the slate is [A, B, C] with
// distinct stable IDs, and returns the three craft pointers. Active is
// left at idx 0 (A).
func threeCraftSlate(t *testing.T) (*World, *spacecraft.Spacecraft, *spacecraft.Spacecraft, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft B: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 700e3}); err != nil {
		t.Fatalf("SpawnCraft C: %v", err)
	}
	if len(w.Crafts) != 3 {
		t.Fatalf("want 3 crafts, got %d", len(w.Crafts))
	}
	a, b, c := w.Crafts[0], w.Crafts[1], w.Crafts[2]
	// Distinct, nonzero IDs.
	if a.ID == 0 || b.ID == 0 || c.ID == 0 || a.ID == b.ID || b.ID == c.ID || a.ID == c.ID {
		t.Fatalf("craft IDs not distinct/nonzero: A=%d B=%d C=%d", a.ID, b.ID, c.ID)
	}
	w.SetActiveCraftIdx(0)
	return w, a, b, c
}

// TestEndFlightKeepsSurvivingTargetBinding — GH #87 (#2, HIGH). A
// surviving craft's target must keep pointing at the SAME vessel after a
// crashed craft is removed from a lower slot. Pre-fix the stored slate
// index went stale on the splice and silently dropped or mistargeted;
// with ID binding (ADR 0012) the index shift is irrelevant.
func TestEndFlightKeepsSurvivingTargetBinding(t *testing.T) {
	w, a, b, c := threeCraftSlate(t)

	// Fly B, target C. Then crash A (idx 0) and end its flight.
	w.SetActiveCraftIdx(1)
	w.SetTargetCraft(2)
	if b.Target.Kind != TargetCraft || b.Target.CraftID != c.ID {
		t.Fatalf("setup: B.Target = %+v, want craft C (ID %d)", b.Target, c.ID)
	}

	w.SetActiveCraftIdx(0) // fly A
	a.Crashed = true
	if !w.EndFlightActive() {
		t.Fatal("EndFlightActive returned false for a crashed active craft")
	}

	// Slate is now [B, C]; B's binding must still resolve to C.
	if len(w.Crafts) != 2 {
		t.Fatalf("after end-flight: %d crafts, want 2", len(w.Crafts))
	}
	tc, idx, ok := w.craftByID(b.Target.CraftID)
	if !ok {
		t.Fatal("B's target binding was lost when A was removed")
	}
	if tc != c {
		t.Errorf("B's target resolves to a different vessel after removal (idx %d) — mistarget", idx)
	}
}

// TestEndFlightDoesNotClobberSuccessorTarget — GH #87 (#3, MEDIUM /
// defect 2). Removing the active (crashed) craft must not write its live
// target onto the successor that takes its slot. Pre-fix SetActiveCraftIdx
// ran with a stale ActiveCraftIdx and checkpointed the removed craft's
// target onto the successor, destroying the successor's own binding.
func TestEndFlightDoesNotClobberSuccessorTarget(t *testing.T) {
	w, a, b, c := threeCraftSlate(t)

	// B (the successor that will take A's slot) targets C.
	w.SetActiveCraftIdx(1)
	w.SetTargetCraft(2)

	// A (active, crashed) targets a body (Luna-ish: any body idx).
	w.SetActiveCraftIdx(0)
	w.SetTargetBody(1)
	a.Crashed = true

	if !w.EndFlightActive() {
		t.Fatal("EndFlightActive returned false")
	}

	// B is now active at idx 0; its own C-binding must be intact, NOT
	// overwritten with A's body target.
	if b.Target.Kind != TargetCraft || b.Target.CraftID != c.ID {
		t.Errorf("successor B's target was clobbered: got %+v, want craft C (ID %d)", b.Target, c.ID)
	}
	if w.Target.Kind != TargetCraft || w.Target.CraftID != c.ID {
		t.Errorf("world cursor after end-flight = %+v, want B's craft-C binding", w.Target)
	}
}

// TestDockDropPartnerTargetDoesNotAlias — GH #87 (docking.go:308/376).
// When the lead docks with the craft it was targeting, the fused-away
// partner's binding must resolve to nothing, never alias a different
// vessel that shifted into the freed slot. Pre-fix the stale index could
// resolve to whatever craft now occupied that slot.
func TestDockDropPartnerTargetDoesNotAlias(t *testing.T) {
	w, a, b, c := threeCraftSlate(t)
	_, _ = a, c

	// Fly A (idx 0), target B (idx 1, the dock partner).
	w.SetActiveCraftIdx(0)
	w.SetTargetCraft(1)
	if w.Target.CraftID != b.ID {
		t.Fatalf("setup: world target = %+v, want B (ID %d)", w.Target, b.ID)
	}

	// Dock A with B — B is consumed into the composite, C shifts left.
	w.DockCrafts(0, 1)

	// The composite (A's identity) inherits A's stale target (B's ID).
	// B no longer exists, so it must resolve to nothing — not to C, which
	// took the slot B's old index pointed at.
	if _, _, ok := w.craftByID(b.ID); ok {
		t.Error("dropped partner B still resolves by ID after the dock")
	}
	if tc, _, ok := w.ResolveTargetCraft(); ok {
		t.Errorf("world target aliased to a surviving vessel %q after the partner fused away", tc.Name)
	}
}

// TestUndockTailTargetSurvivesShift — GH #87 (docking.go:152). A target
// aimed at a craft in the tail must keep resolving to that craft after an
// undock inserts extra component slots ahead of it. Pre-fix the insert
// shift left the stored index pointing one (or more) slots too low.
func TestUndockTailTargetSurvivesShift(t *testing.T) {
	w := mustWorld(t)
	// Build a composite at idx 0 by docking two craft, then add a tail
	// craft Z that something targets.
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("spawn partner: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 700e3}); err != nil {
		t.Fatalf("spawn Z: %v", err)
	}
	z := w.Crafts[2]
	// Dock idx0 + idx1 → composite at idx0, Z shifts to idx1.
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 2 {
		t.Fatalf("after dock: %d crafts, want 2 (composite + Z)", len(w.Crafts))
	}
	zIdx := -1
	for i, cc := range w.Crafts {
		if cc == z {
			zIdx = i
		}
	}
	if zIdx < 0 {
		t.Fatal("Z missing after dock")
	}

	// Fly the composite (idx 0), target Z.
	w.SetActiveCraftIdx(0)
	w.SetTargetCraft(zIdx)
	if w.Target.CraftID != z.ID {
		t.Fatalf("setup: target = %+v, want Z (ID %d)", w.Target, z.ID)
	}

	// Undock the composite — it splits into ≥2 components, shifting Z right.
	if !w.Undock(0) {
		t.Fatal("Undock returned false")
	}
	if len(w.Crafts) < 3 {
		t.Fatalf("after undock: %d crafts, want ≥3", len(w.Crafts))
	}

	// Z must still resolve by its stable ID despite the index shift.
	tc, _, ok := w.craftByID(z.ID)
	if !ok || tc != z {
		t.Errorf("Z's binding broke across the undock insert shift (ok=%v)", ok)
	}
}

// TestStableIDsAreUniqueAcrossSpawnAndStage — every craft entering the
// slate gets a distinct, nonzero ID and NextCraftID never hands out a
// value already in use.
func TestStableIDsAreUniqueAcrossSpawnAndStage(t *testing.T) {
	w := mustWorld(t)
	for i := 0; i < 4; i++ {
		if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: float64(500+100*i) * 1e3}); err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}
	}
	seen := map[uint64]bool{}
	for i, c := range w.Crafts {
		if c.ID == 0 {
			t.Errorf("craft %d (%q) has zero ID", i, c.Name)
		}
		if seen[c.ID] {
			t.Errorf("duplicate craft ID %d at slate idx %d", c.ID, i)
		}
		seen[c.ID] = true
		if c.ID >= w.NextCraftID {
			t.Errorf("NextCraftID %d not ahead of live ID %d", w.NextCraftID, c.ID)
		}
	}
}
