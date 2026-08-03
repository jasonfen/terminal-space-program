package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestLocalReArmLatchHoldsDriftedPairApart is the #343 decision-2 regression
// test: the local Undock split now arms a same-World re-arm latch
// (mirroring ADR 0038 §5's cross-player DockCooldown) for every pair it just
// released. This covers the case geometry alone cannot: two of the
// restored craft drifting back inside BOTH docking gates for reasons that
// have nothing to do with the separation push (station-keeping error, a
// third body's perturbation, or simply flying them back together) must
// still not silently re-fuse the instant they touch.
func TestLocalReArmLatchHoldsDriftedPairApart(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 2-component composite")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("expected 2 craft after undock, got %d", len(w.Crafts))
	}

	// Simulate the pair drifting back together: park them well inside both
	// gates, matched velocity — exactly the state checkDocking looks for.
	a, b := w.Crafts[0], w.Crafts[1]
	b.State.R = a.State.R.Add(orbital.Vec3{X: 5})
	b.State.V = a.State.V

	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("checkDocking fused a latched pair that drifted back inside both gates — the local re-arm latch did not hold")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("slate count = %d after a latched drift-together, want 2 (no fuse)", len(w.Crafts))
	}

	// Once they separate past ReArmDistM, the latch clears and an ordinary
	// rendezvous (same position/velocity gates, no history) may fuse again.
	b.State.R = a.State.R.Add(orbital.Vec3{X: ReArmDistM + 10})
	b.State.V = a.State.V
	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("checkDocking fused while the pair is still beyond the proximity gate — unexpected match on distance alone")
	}

	// Prune runs at the top of checkDocking; separating past ReArmDistM
	// should have dropped the latch even though the pair is currently too
	// far apart to dock. Bring them back together now that the latch is
	// gone and confirm an ordinary re-dock succeeds.
	b.State.R = a.State.R.Add(orbital.Vec3{X: 5})
	b.State.V = a.State.V
	if _, _, ok := w.checkDocking(); !ok {
		t.Fatal("checkDocking refused to re-fuse a pair that had legitimately cleared the re-arm latch by separating past ReArmDistM")
	}
	if len(w.Crafts) != 1 {
		t.Errorf("slate count = %d after the cleared-latch re-dock, want 1", len(w.Crafts))
	}
}

// TestLocalReArmLatchClearsAtCeiling: a latch that can never observe the
// pair separating (in this test, because they never actually move apart)
// must still release once ReArmCeiling of SIM-time has passed — mirroring
// the cross-player ceiling's #326 rationale, so a latch can never hold a
// locally-undocked pair un-dockable indefinitely.
func TestLocalReArmLatchClearsAtCeiling(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 2-component composite")
	}
	a, b := w.Crafts[0], w.Crafts[1]
	b.State.R = a.State.R.Add(orbital.Vec3{X: 5})
	b.State.V = a.State.V

	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("checkDocking fused a freshly-latched pair — latch should hold immediately after undock")
	}

	w.Clock.SimTime = w.Clock.SimTime.Add(ReArmCeiling + time.Minute)
	if _, _, ok := w.checkDocking(); !ok {
		t.Fatal("checkDocking still refused to fuse after ReArmCeiling of sim-time elapsed — latch never expires")
	}
	if len(w.Crafts) != 1 {
		t.Errorf("slate count = %d after the ceiling-expired re-dock, want 1", len(w.Crafts))
	}
}
