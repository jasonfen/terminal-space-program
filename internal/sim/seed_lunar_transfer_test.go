package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// seedLunarRun plans the game's own Earth->Moon transfer ([H], target the
// Moon) from the active vessel, flies it with Auto-Warp, and reports the
// numbers. It starts where a fresh game does (the next lunar window).
type seedLunarRun struct {
	strategy            string
	splitDv, combinedDv float64
	nodeDv              []float64
	craftDv             float64
	overBudget          bool
	enteredMoonSOI      bool
	nodesLeft           int
	moonEcc             float64 // eccentricity about the Moon after the flight; <1 is a bound capture
}

func runSeedLunarTransfer(t *testing.T, equatorial bool) seedLunarRun {
	t.Helper()
	w := mustWorld(t)
	if equatorial {
		makeActiveEquatorial(w)
	}
	w.AdjustStartForLunarTransferWindow(DefaultLunarTransferLead)
	moonIdx := -1
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon missing from Sol")
	}
	w.SetTargetBody(moonIdx)
	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	c := w.ActiveCraft()
	r := seedLunarRun{
		strategy: w.LastTransfer.Strategy, splitDv: w.LastTransfer.SplitDv,
		combinedDv: w.LastTransfer.CombinedDv, craftDv: c.RemainingDeltaV(),
		overBudget: activeCraftHasOverBudgetNode(c),
	}
	for _, n := range c.Nodes {
		r.nodeDv = append(r.nodeDv, n.DV)
	}
	for leg := 0; leg < 8 && len(c.Nodes) > 0 && c.Primary.ID == "earth"; leg++ {
		if !w.EngageAutoWarp() {
			break
		}
		if _, ok := tickUntil(w, 5_000_000, func() bool { return w.AutoWarp == nil }); !ok {
			break
		}
		n := len(c.Nodes)
		tickUntil(w, 5_000_000, func() bool { return len(c.Nodes) < n })
		tickUntil(w, 5_000_000, func() bool { return c.ActiveBurn == nil })
	}
	tickUntil(w, 2_000_000, func() bool { return c.Primary.ID == "moon" })
	r.enteredMoonSOI = c.Primary.ID == "moon"
	r.nodesLeft = len(c.Nodes)
	if r.enteredMoonSOI {
		for leg := 0; leg < 4 && len(c.Nodes) > 0 && c.Primary.ID == "moon"; leg++ {
			if !w.EngageAutoWarp() {
				break
			}
			tickUntil(w, 5_000_000, func() bool { return w.AutoWarp == nil })
			n := len(c.Nodes)
			tickUntil(w, 5_000_000, func() bool { return len(c.Nodes) < n })
			tickUntil(w, 5_000_000, func() bool { return c.ActiveBurn == nil })
		}
		r.nodesLeft = len(c.Nodes)
		if c.Primary.ID == "moon" {
			r.moonEcc = orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter()).E
		}
	}
	return r
}

// TestSeedLunarTransferFitsAndFlies pins issue #566's mission check: from
// the 51.6 deg seed the game's own Moon transfer is affordable on the
// S-IVB-1's Δv (no over-budget node: tut-plan's budget gate) and the
// flown plan reaches the Moon's sphere of influence (chal-luna-flyby).
// The equatorial run is logged alongside for the cost of the tilt.
func TestSeedLunarTransferFitsAndFlies(t *testing.T) {
	eq := runSeedLunarTransfer(t, true)
	in := runSeedLunarTransfer(t, false)
	t.Logf("equatorial 0.0 deg: %+v", eq)
	t.Logf("seed %.1f deg:     %+v", spacecraft.SeedInclinationDeg, in)
	if in.overBudget {
		t.Errorf("a node is over budget from the seed (craft Δv %.0f, nodes %v)", in.craftDv, in.nodeDv)
	}
	if in.enteredMoonSOI && (in.moonEcc <= 0 || in.moonEcc >= 1) {
		t.Errorf("flown capture burn left e=%.3f about the Moon, want a bound orbit (0<e<1) (chal-luna-capture)", in.moonEcc)
	}
	if !in.enteredMoonSOI {
		t.Errorf("the planted transfer never reached the Moon's SOI from the seed (nodes left %d)", in.nodesLeft)
	}
}

// TestSeedMarsPlanBudgetProbe logs (no assertion on the equatorial arm) the
// Mars transfer plan's node Δv from both seeds for chal-mars-flyby, and
// asserts the seed's plan has no over-budget node.
func TestSeedMarsPlanBudgetProbe(t *testing.T) {
	for _, equatorial := range []bool{true, false} {
		w := mustWorld(t)
		if equatorial {
			makeActiveEquatorial(w)
		}
		marsIdx := -1
		for i, b := range w.System().Bodies {
			if b.ID == "mars" {
				marsIdx = i
			}
		}
		if marsIdx < 0 {
			t.Skip("Mars missing")
		}
		w.SetTargetBody(marsIdx)
		plan, err := w.PlanTransfer(marsIdx)
		if err != nil {
			t.Fatalf("equatorial=%v PlanTransfer(mars): %v", equatorial, err)
		}
		c := w.ActiveCraft()
		sum := 0.0
		for _, n := range c.Nodes {
			sum += n.DV
		}
		over := activeCraftHasOverBudgetNode(c)
		t.Logf("equatorial=%v mars: nodes=%d sumDv=%.0f craftDv=%.0f overBudget=%v depWait=%v", equatorial, len(c.Nodes), sum, c.RemainingDeltaV(), over, plan.Departure.OffsetTime)
		if !equatorial && over {
			t.Errorf("Mars plan from the seed has an over-budget node")
		}
	}
}
