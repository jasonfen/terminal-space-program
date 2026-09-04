package spacecraft

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestManeuverNodeOverBudget — ADR 0047 §2: a node's Δv exceeding the
// craft's currently remaining Δv is the "Over-budget Node" the planner
// marks with a shortfall, and plants anyway (warn-and-allow, not a
// refusal). This is the ONE predicate both the NODES/planner chip rows
// (internal/tui/screens/orbit_chips.go) and the tut-plan Flight School
// objective (#426) share — factored here so neither forks it.
func TestManeuverNodeOverBudget(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	budget := sc.RemainingDeltaV()

	over := ManeuverNode{DV: budget + 1521}
	shortfall, isOver := over.OverBudget(sc)
	if !isOver {
		t.Fatalf("node at budget+1521 should be over budget")
	}
	if shortfall < 1520 || shortfall > 1522 {
		t.Errorf("shortfall = %.2f, want ~1521", shortfall)
	}

	affordable := ManeuverNode{DV: 10}
	if _, isOver := affordable.OverBudget(sc); isOver {
		t.Errorf("a 10 m/s node against a %.0f m/s budget must not be over budget", budget)
	}

	// Exactly at budget is NOT over (only a strictly positive shortfall counts,
	// matching the chip's `> 0` gate).
	exact := ManeuverNode{DV: budget}
	if shortfall, isOver := exact.OverBudget(sc); isOver {
		t.Errorf("a node exactly at budget must not be marked over-budget (shortfall %.2f)", shortfall)
	}
}
