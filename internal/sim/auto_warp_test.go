package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// maxWarpFactor is the top discrete warp factor — the value Auto-Warp
// seeds clampedWarp's "selected" baseline to while engaged.
var maxWarpFactor = WarpFactors[len(WarpFactors)-1]

// plantOn plants an impulsive prograde node on the craft at slate idx,
// dt out from now, and returns its stable ID.
func plantOn(t *testing.T, w *World, idx int, dt time.Duration) uint64 {
	t.Helper()
	w.SetActiveCraftIdx(idx)
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(dt),
		DV:          10,
		Mode:        spacecraft.BurnPrograde,
	})
	nodes := w.Crafts[idx].Nodes
	return nodes[len(nodes)-1].ID
}

// TestEngageAutoWarpPicksGloballySoonest — engage aims at the earliest
// BurnStart across ALL vessels, freezing T = BurnStart − leadTime.
func TestEngageAutoWarpPicksGloballySoonest(t *testing.T) {
	w, a, b, _ := threeCraftSlate(t)

	// Active craft A: a burn 5 days out. Craft B: a sooner burn 2 days out.
	plantOn(t, w, 0, 5*24*time.Hour)
	bID := plantOn(t, w, 1, 2*24*time.Hour)
	w.SetActiveCraftIdx(0) // fly A; Auto-Warp must still aim at B's sooner burn

	if !w.EngageAutoWarp() {
		t.Fatal("EngageAutoWarp returned false with two eligible burns")
	}
	if w.AutoWarp == nil {
		t.Fatal("AutoWarp nil after a successful engage")
	}
	if w.AutoWarp.CraftID != b.ID || w.AutoWarp.NodeID != bID {
		t.Errorf("Auto-Warp aimed at craft %d node %d, want B (craft %d node %d)",
			w.AutoWarp.CraftID, w.AutoWarp.NodeID, b.ID, bID)
	}
	wantT := w.Crafts[1].Nodes[0].BurnStart().Add(-autoWarpLeadTime)
	if !w.AutoWarp.T.Equal(wantT) {
		t.Errorf("AutoWarp.T = %v, want BurnStart−lead = %v", w.AutoWarp.T, wantT)
	}
	_ = a
}

// TestEngageAutoWarpNoEligibleBurn — no nodes, or only burns inside the
// lead window, makes engage a no-op returning false.
func TestEngageAutoWarpNoEligibleBurn(t *testing.T) {
	w := mustWorld(t)
	if w.EngageAutoWarp() {
		t.Error("engage returned true with no nodes planted")
	}
	if w.AutoWarp != nil {
		t.Error("AutoWarp set despite no eligible burn")
	}
	// A burn inside the lead window is not eligible.
	plantOn(t, w, 0, autoWarpLeadTime/2)
	if w.EngageAutoWarp() {
		t.Error("engage returned true for a burn inside the lead window")
	}
}

// TestEngageAutoWarpAutoUnpauses — engaging while paused resumes time.
func TestEngageAutoWarpAutoUnpauses(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.Clock.Paused = true
	if !w.EngageAutoWarp() {
		t.Fatal("engage failed")
	}
	if w.Clock.Paused {
		t.Error("engaging Auto-Warp did not auto-unpause")
	}
}

// TestAutoWarpRaisesEffectiveWarp — with Selected Warp at 1× and a
// far-off target, the engaged driver max-seeds the effective rate well
// above 1×, without touching WarpIdx.
func TestAutoWarpRaisesEffectiveWarp(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 10*24*time.Hour)
	w.Clock.WarpIdx = 0 // Selected Warp 1×

	before := w.EffectiveWarp()
	if before != 1 {
		t.Fatalf("pre-engage effective warp = %v, want 1", before)
	}
	if !w.EngageAutoWarp() {
		t.Fatal("engage failed")
	}
	if got := w.EffectiveWarp(); got <= 1 {
		t.Errorf("engaged effective warp = %v, want > 1 (max-seeded)", got)
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("Auto-Warp mutated WarpIdx to %d; Selected Warp must stay untouched", w.Clock.WarpIdx)
	}
}

// TestAutoWarpPausedDoesNotAdvance — pausing while engaged keeps
// Auto-Warp on but freezes time (effective warp 0); it resumes on unpause.
func TestAutoWarpPausedDoesNotAdvance(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 10*24*time.Hour)
	w.EngageAutoWarp()
	w.Clock.Paused = true
	if got := w.EffectiveWarp(); got != 0 {
		t.Errorf("paused effective warp = %v, want 0", got)
	}
	if w.AutoWarp == nil {
		t.Error("pause disengaged Auto-Warp; it should stay engaged")
	}
	w.Clock.Paused = false
	if got := w.EffectiveWarp(); got <= 1 {
		t.Errorf("post-unpause effective warp = %v, want > 1", got)
	}
}

// TestAutoWarpRampsDownNearTarget — as SimTime nears T the effective
// rate ramps toward the 1× floor (the new approach term anchored at T).
func TestAutoWarpRampsDownNearTarget(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 10*24*time.Hour)
	w.Clock.WarpIdx = len(WarpFactors) - 1 // Selected at max so the term, not WarpIdx, governs
	w.EngageAutoWarp()

	far := w.EffectiveWarp()
	// Jump SimTime to a few seconds before T — well inside the ramp.
	w.Clock.SimTime = w.AutoWarp.T.Add(-3 * time.Second)
	near := w.EffectiveWarp()
	if near >= far {
		t.Errorf("effective warp did not ramp down near target: far=%v near=%v", far, near)
	}
	if near < 1 {
		t.Errorf("ramp undershot the 1× floor: %v", near)
	}
}

// TestAutoWarpReachingTargetDropsToOneXAndDisengages — once SimTime ≥ T
// the per-tick resolve forces Selected Warp to 1× and releases the driver.
func TestAutoWarpReachingTargetDropsToOneXAndDisengages(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.Clock.WarpIdx = 3
	w.EngageAutoWarp()

	w.Clock.SimTime = w.AutoWarp.T.Add(time.Second) // past the lead point
	w.resolveAutoWarp()

	if w.AutoWarp != nil {
		t.Error("Auto-Warp still engaged after reaching the target")
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("reaching the target left WarpIdx at %d, want 0 (1×)", w.Clock.WarpIdx)
	}
}

// TestAutoWarpDisengagesWhenNodeDeleted — deleting the target node makes
// the next resolve disengage, leaving Selected Warp untouched (a node-
// invalidated cancel, not a reached-target stop).
func TestAutoWarpDisengagesWhenNodeDeleted(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.Clock.WarpIdx = 4
	w.EngageAutoWarp()

	w.Crafts[0].Nodes = nil // delete the target
	w.resolveAutoWarp()

	if w.AutoWarp != nil {
		t.Error("Auto-Warp stayed engaged after its target was deleted")
	}
	if w.Clock.WarpIdx != 4 {
		t.Errorf("node-invalidated disengage clobbered WarpIdx to %d, want 4 (untouched)", w.Clock.WarpIdx)
	}
}

// TestAutoWarpRefreezesOnNodeMove — editing the target node's BurnStart
// re-freezes T to track it.
func TestAutoWarpRefreezesOnNodeMove(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.EngageAutoWarp()
	origT := w.AutoWarp.T

	// Push the node 1 day later (edit in place; ID is preserved).
	w.Crafts[0].Nodes[0].TriggerTime = w.Crafts[0].Nodes[0].TriggerTime.Add(24 * time.Hour)
	w.resolveAutoWarp()

	wantT := w.Crafts[0].Nodes[0].BurnStart().Add(-autoWarpLeadTime)
	if !w.AutoWarp.T.Equal(wantT) {
		t.Errorf("T not re-frozen after node move: T=%v want=%v", w.AutoWarp.T, wantT)
	}
	if w.AutoWarp.T.Equal(origT) {
		t.Error("T unchanged after the node moved 1 day later")
	}
}

// TestDisengageAutoWarpLeavesWarpIdx — a manual cancel drops the driver
// without touching Selected Warp, so the player falls back to their rate.
func TestDisengageAutoWarpLeavesWarpIdx(t *testing.T) {
	w := mustWorld(t)
	plantOn(t, w, 0, 2*24*time.Hour)
	w.Clock.WarpIdx = 5
	w.EngageAutoWarp()
	w.DisengageAutoWarp()
	if w.AutoWarp != nil {
		t.Error("DisengageAutoWarp left AutoWarp set")
	}
	if w.Clock.WarpIdx != 5 {
		t.Errorf("manual disengage changed WarpIdx to %d, want 5", w.Clock.WarpIdx)
	}
}

// TestAutoWarpEndToEndThroughTick — engage, run real ticks, and confirm
// time advances fast then hands off to 1× near the burn with the driver
// released. Exercises the resolveAutoWarp + clampedWarp wiring in Tick.
func TestAutoWarpEndToEndThroughTick(t *testing.T) {
	w := mustWorld(t)
	burnDt := 6 * time.Hour
	plantOn(t, w, 0, burnDt)
	w.Clock.WarpIdx = 0
	start := w.Clock.SimTime
	if !w.EngageAutoWarp() {
		t.Fatal("engage failed")
	}
	target := w.AutoWarp.T

	for i := 0; i < 2_000_000 && w.AutoWarp != nil; i++ {
		w.Tick()
	}
	if w.AutoWarp != nil {
		t.Fatal("Auto-Warp never disengaged across the tick budget")
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("ended at WarpIdx %d, want 0 (1×)", w.Clock.WarpIdx)
	}
	// Landed at or just past T, and comfortably before the burn itself.
	if w.Clock.SimTime.Before(target) {
		t.Errorf("stopped before the lead point: SimTime=%v T=%v", w.Clock.SimTime, target)
	}
	if got := w.Clock.SimTime.Sub(start); got >= burnDt {
		t.Errorf("overshot the burn: advanced %v, burn was %v out", got, burnDt)
	}
}
