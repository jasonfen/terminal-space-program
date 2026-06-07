package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestCanKeplerStepGatesOnEffectiveWarp is the unit-level guard for the
// same bug: the analytic warp-lock fast path must be chosen by the
// effective warp this tick (the step size simDelta implies), not by the
// player's Selected Warp. Auto-Warp leaves WarpIdx at 0 (Clock.Warp()==1)
// while driving a large simDelta, so a Clock.Warp() gate wrongly forced
// the Verlet slow path.
func TestCanKeplerStepGatesOnEffectiveWarp(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft() // default bound LEO orbit (peri above atmosphere)
	base := w.Clock.BaseStep

	w.Clock.WarpIdx = 0 // the Auto-Warp state: Selected Warp 1×
	if w.Clock.Warp() != 1 {
		t.Fatalf("setup: Clock.Warp()=%v, want 1", w.Clock.Warp())
	}

	if w.canKeplerStep(c, base) {
		t.Error("canKeplerStep true at effective 1× (realtime step); want the Verlet path")
	}
	if w.canKeplerStep(c, 0) {
		t.Error("canKeplerStep true at simDelta=0 (paused); want false")
	}
	big := time.Duration(float64(base) * 100000) // Auto-Warp's max-seeded step
	if !w.canKeplerStep(c, big) {
		t.Error("canKeplerStep false for a high effective-warp step while WarpIdx=0 — the escape bug")
	}
}

// TestAutoWarpLunarTransferStaysBound is the regression for the
// "Auto-Warp to the plane change flings the craft into solar orbit" bug.
//
// Root cause: canKeplerStep gated the analytic warp-lock fast path on the
// player's Selected Warp (Clock.Warp()), but Auto-Warp drives the
// effective warp through clampedWarp's max-seed without touching WarpIdx.
// After a prior Auto-Warp leg set WarpIdx=0, Clock.Warp() read 1×, the
// gate rejected Kepler, and the integrator took a single ~5000 s Verlet
// step (period/100 for a ~10-day transfer orbit) through perigee —
// aliasing the bound trans-lunar ellipse into a hyperbolic Earth escape.
//
// Repro: new game, target Moon, [H] split transfer, Auto-Warp to the TLI
// burn (it fires), then Auto-Warp to the plane-change node. The craft must
// stay bound to Earth on the trans-lunar trajectory.
func TestAutoWarpLunarTransferStaysBound(t *testing.T) {
	w := mustWorld(t)
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
	nodesBefore := len(c.Nodes)
	if nodesBefore < 2 {
		t.Fatalf("expected a multi-node split transfer, got %d nodes", nodesBefore)
	}

	// Leg 1: Auto-Warp to the TLI burn, then let it fire and finish.
	if !w.EngageAutoWarp() {
		t.Fatal("engage 1 (TLI) failed")
	}
	if _, ok := tickUntil(w, 5_000_000, func() bool { return w.AutoWarp == nil }); !ok {
		t.Fatal("Auto-Warp leg 1 never disengaged")
	}
	if _, ok := tickUntil(w, 5_000_000, func() bool { return len(c.Nodes) < nodesBefore }); !ok {
		t.Fatal("TLI node never fired")
	}
	if _, ok := tickUntil(w, 5_000_000, func() bool { return c.ActiveBurn == nil }); !ok {
		t.Fatal("TLI burn never completed")
	}
	if c.Primary.ID != "earth" {
		t.Fatalf("escaped Earth before the plane change (primary=%s)", c.Primary.ID)
	}
	postTLI := orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
	if postTLI.E >= 1 {
		t.Fatalf("TLI left a hyperbolic orbit (e=%.4f); setup invalid", postTLI.E)
	}

	// Leg 2: Auto-Warp to the plane-change node — the buggy leg.
	if !w.EngageAutoWarp() {
		t.Fatal("engage 2 (plane change) failed")
	}
	if _, ok := tickUntil(w, 5_000_000, func() bool { return w.AutoWarp == nil }); !ok {
		t.Fatal("Auto-Warp leg 2 never disengaged")
	}

	if c.Primary.ID != "earth" {
		t.Fatalf("vessel escaped to %s orbit during the warp to the plane change (was bound trans-lunar)", c.Primary.ID)
	}
	el := orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
	if el.E >= 1 {
		t.Fatalf("vessel on a hyperbolic Earth-escape trajectory after the warp (e=%.4f)", el.E)
	}
	// Energy conserved across the analytic coast: semi-major axis should
	// track the post-TLI ellipse, not balloon toward escape.
	if el.A > postTLI.A*1.5 {
		t.Errorf("orbit energy grew during the coast: a %.0f → %.0f km (warp-step aliasing)",
			postTLI.A/1e3, el.A/1e3)
	}
}
