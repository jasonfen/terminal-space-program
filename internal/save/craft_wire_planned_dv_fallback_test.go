package save

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestCraftFromWireFallsBackPlannedDVWhenAbsent — #447 review finding 8.
// A save written before ActiveBurn.PlannedDV existed decodes it as the
// zero value (additive omitempty field). Without a fallback, the
// burn-finished Event Flash would print "burned — 0 m/s" for a burn
// that really delivered real Δv — a false statement, not merely a less
// precise one. CraftFromWire falls back to DVRemaining (the wire form's
// own remaining-Δv estimate at save time) whenever PlannedDV comes back
// 0, so the finish flash reports the closest available real figure
// instead of a fabricated zero.
func TestCraftFromWireFallsBackPlannedDVWhenAbsent(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	wc := CraftToWire(c)
	wc.ActiveBurn = &ActiveBurn{
		Mode:        int(spacecraft.BurnPrograde),
		DVRemaining: 1800,
		EndTimeNano: w.Clock.SimTime.Add(time.Minute).UnixNano(),
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
		// PlannedDV / NodeIndex both zero — a pre-#447 save's shape.
	}

	loaded, err := CraftFromWire(wc, systems)
	if err != nil {
		t.Fatalf("CraftFromWire: %v", err)
	}
	if loaded.ActiveBurn == nil {
		t.Fatal("ActiveBurn did not survive load")
	}
	if loaded.ActiveBurn.PlannedDV != 1800 {
		t.Errorf("PlannedDV = %v, want fallback to DVRemaining (1800)", loaded.ActiveBurn.PlannedDV)
	}
}

// TestCraftFromWireKeepsExplicitPlannedDV — the fallback above must be
// surgical: a save written AFTER #447, carrying a real (non-zero)
// PlannedDV, loads it unchanged rather than being overridden by
// DVRemaining (which can legitimately differ once a burn has partially
// delivered its Δv).
func TestCraftFromWireKeepsExplicitPlannedDV(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	wc := CraftToWire(c)
	wc.ActiveBurn = &ActiveBurn{
		Mode:        int(spacecraft.BurnPrograde),
		DVRemaining: 900, // partially delivered — differs from PlannedDV
		EndTimeNano: w.Clock.SimTime.Add(time.Minute).UnixNano(),
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
		PlannedDV:   3054,
		NodeIndex:   2,
	}

	loaded, err := CraftFromWire(wc, systems)
	if err != nil {
		t.Fatalf("CraftFromWire: %v", err)
	}
	if loaded.ActiveBurn == nil {
		t.Fatal("ActiveBurn did not survive load")
	}
	if loaded.ActiveBurn.PlannedDV != 3054 {
		t.Errorf("PlannedDV = %v, want the explicit wire value 3054 (not overridden by DVRemaining)", loaded.ActiveBurn.PlannedDV)
	}
	if loaded.ActiveBurn.NodeIndex != 2 {
		t.Errorf("NodeIndex = %v, want 2", loaded.ActiveBurn.NodeIndex)
	}
}
