package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Sync drives Auto-Warp to the target time and releases at 1× — the
// arrival lands within a couple of base steps of the target (the
// approach term has ramped the rate to the floor by then).
func TestSyncWarpArrivalTimeEquality(t *testing.T) {
	w := mustWorld(t)
	target := w.Clock.SimTime.Add(10 * 24 * time.Hour)
	if !w.EngageSyncWarp(target, "alice") {
		t.Fatal("EngageSyncWarp refused a forward target")
	}
	if !w.AutoWarp.Sync {
		t.Fatal("driver not in sync mode")
	}

	for i := 0; i < 500000 && w.AutoWarp != nil; i++ {
		w.Tick()
	}
	if w.AutoWarp != nil {
		t.Fatal("sync never completed")
	}
	overshoot := w.Clock.SimTime.Sub(target)
	if overshoot < 0 {
		t.Fatalf("released %v BEFORE the target", -overshoot)
	}
	if overshoot > 2*w.Clock.BaseStep {
		t.Errorf("arrival overshoot %v exceeds two base steps (%v)", overshoot, w.Clock.BaseStep)
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("arrival left WarpIdx at %d, want 0 (1×)", w.Clock.WarpIdx)
	}
	if w.LastSyncArrival == nil || w.LastSyncArrival.Handle != "alice" {
		t.Errorf("LastSyncArrival = %+v, want alice", w.LastSyncArrival)
	}
}

// Backward (or zero) sync is refused — the laggard always comes
// forward; rewinding forks recorded history (ADR 0034).
func TestSyncWarpForwardOnly(t *testing.T) {
	w := mustWorld(t)
	if w.EngageSyncWarp(w.Clock.SimTime.Add(-time.Hour), "alice") {
		t.Error("backward sync engaged")
	}
	if w.EngageSyncWarp(w.Clock.SimTime, "alice") {
		t.Error("zero-gap sync engaged")
	}
	if w.AutoWarp != nil {
		t.Error("refused sync left a driver engaged")
	}
}

// A planted node en route is lived through, not skipped: the burn-cap
// clamp holds while it fires, the burn actually executes, and the
// sync still completes at the target.
func TestSyncWarpClampsAcrossPlantedNode(t *testing.T) {
	w := mustWorld(t)
	// A finite burn a day out (Duration > 0 → an ActiveBurn window the
	// 10× burn cap governs; an impulsive node would leave no window to
	// observe).
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(24 * time.Hour),
		DV:          10,
		Duration:    2 * time.Minute,
		Mode:        spacecraft.BurnPrograde,
	})
	target := w.Clock.SimTime.Add(2 * 24 * time.Hour)
	if !w.EngageSyncWarp(target, "alice") {
		t.Fatal("EngageSyncWarp refused")
	}

	sawBurn := false
	maxWarpDuringBurn := 0.0
	for i := 0; i < 500000 && w.AutoWarp != nil; i++ {
		w.Tick()
		if c := w.ActiveCraft(); c != nil && c.ActiveBurn != nil {
			sawBurn = true
			if eff := w.EffectiveWarp(); eff > maxWarpDuringBurn {
				maxWarpDuringBurn = eff
			}
		}
	}
	if w.AutoWarp != nil {
		t.Fatal("sync never completed across the node")
	}
	if !sawBurn {
		t.Fatal("the planted burn never fired — sync aliased past it")
	}
	if maxWarpDuringBurn > 10 {
		t.Errorf("burn warp cap violated during sync: %v > 10", maxWarpDuringBurn)
	}
	if got := len(w.ActiveCraft().Nodes); got != 0 {
		t.Errorf("%d nodes left after sync — burn not consumed", got)
	}
	if w.Clock.SimTime.Before(target) {
		t.Error("sync released before the target")
	}
}

// Two-World formation: the laggard syncs to the leader's time; after
// arrival both advance in lockstep at 1×.
func TestSyncWarpTwoWorldFormation(t *testing.T) {
	wA, wB := mustWorld(t), mustWorld(t)
	wA.Clock.SimTime = wA.Clock.SimTime.Add(10 * 24 * time.Hour) // A leads

	if !wB.EngageSyncWarp(wA.Clock.SimTime, "alice") {
		t.Fatal("EngageSyncWarp refused")
	}
	for i := 0; i < 500000 && wB.AutoWarp != nil; i++ {
		wB.Tick()
	}
	if wB.AutoWarp != nil {
		t.Fatal("sync never completed")
	}
	gap := wB.Clock.SimTime.Sub(wA.Clock.SimTime)
	if gap < 0 || gap > time.Minute {
		t.Fatalf("post-sync subspace gap = %v, want tiny and non-negative", gap)
	}

	// Lockstep: both at 1×, ticking together keeps the gap constant.
	for i := 0; i < 200; i++ {
		wA.Tick()
		wB.Tick()
	}
	if after := wB.Clock.SimTime.Sub(wA.Clock.SimTime); after != gap {
		t.Errorf("subspace gap drifted in formation: %v → %v", gap, after)
	}
}
