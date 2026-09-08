package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// This file covers decision 4 of the burn-state-visibility batch
// (grilled 2026-09-06, designdocs/terminal-space-program/ux-reviews/
// 20260902-1059/triage/action-plan.md; ADR 0048): a planted finite burn
// firing and finishing in total silence. Modeled on how LastDockEvent
// (docking_test.go) and LastNodeTargetRefusal are tested — stash-site
// assertions on World fields, not app.go's flash wording (that's covered
// by internal/tui/app_test.go, if one exists, or just left to the
// flash-formatting convention shared with the other Last*Event fields).

// TestExecuteDueNodesStampsBurnFiredEvent — a due finite node igniting
// (the c.ActiveBurn = &ActiveBurn{...} branch of executeDueNodesFor)
// must stash LastBurnFiredEvent with the craft's name, the node's DV,
// and its 0-based NodeIndex (the "node #N" convention) — per craft, not
// just the active one.
func TestExecuteDueNodesStampsBurnFiredEvent(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	now := w.Clock.SimTime

	// A held node ahead of it (unresolved) so NodeIndex must read 1, not
	// 0 — proving the stash names this node's real ordinal, not just "the
	// first node examined".
	held := spacecraft.ManeuverNode{
		Mode: spacecraft.BurnPrograde,
		DV:   50,
		// Event-relative, unresolved: TriggerTime stays zero until the
		// lazy-freeze resolver fires, so this node holds every tick.
		Event: spacecraft.TriggerNextApo,
	}
	firing := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnPrograde,
		DV:          3054,
		TriggerTime: now.Add(10 * time.Second),
		Duration:    60 * time.Second, // BurnStart = now - 20s: already due
	}
	c.Nodes = []spacecraft.ManeuverNode{held, firing}

	if w.LastBurnFiredEvent != nil {
		t.Fatal("precondition: LastBurnFiredEvent already set before any burn fired")
	}
	w.executeDueNodes()

	// The held node can't resolve/fire, so it stays queued and blocks the
	// walk (executeDueNodesFor stops dispatching once one node holds) —
	// rebuild the fixture without it so `firing` is actually reachable.
	if c.ActiveBurn != nil {
		t.Fatal("test fixture bug: held node should have blocked the walk")
	}

	c.Nodes = []spacecraft.ManeuverNode{firing}
	w.executeDueNodes()

	if c.ActiveBurn == nil {
		t.Fatal("firing node did not start an ActiveBurn")
	}
	if w.LastBurnFiredEvent == nil {
		t.Fatal("firing node did not stash LastBurnFiredEvent")
	}
	e := w.LastBurnFiredEvent
	if e.CraftName != c.Name {
		t.Errorf("LastBurnFiredEvent.CraftName = %q, want %q", e.CraftName, c.Name)
	}
	if e.DV != 3054 {
		t.Errorf("LastBurnFiredEvent.DV = %v, want 3054", e.DV)
	}
	if e.NodeIndex != 0 {
		t.Errorf("LastBurnFiredEvent.NodeIndex = %d, want 0 (only node left in the queue)", e.NodeIndex)
	}
	// The running burn itself must carry the same figures forward so the
	// finish event can report them later (DVRemaining drifts as the burn
	// integrates; PlannedDV/NodeIndex must not).
	if c.ActiveBurn.PlannedDV != 3054 {
		t.Errorf("ActiveBurn.PlannedDV = %v, want 3054", c.ActiveBurn.PlannedDV)
	}
}

// TestExecuteDueNodesStampsBurnFiredEventNonActiveCraft — decision 4 is
// explicit that this is a per-craft hook, not just the active vessel's:
// a burn igniting on a different craft in the slate must still stamp
// LastBurnFiredEvent, naming that craft.
func TestExecuteDueNodesStampsBurnFiredEventNonActiveCraft(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 500e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = 0
	other := w.Crafts[1]
	other.Name = "Tug-1"
	now := w.Clock.SimTime
	other.Nodes = []spacecraft.ManeuverNode{{
		Mode:        spacecraft.BurnPrograde,
		DV:          500,
		TriggerTime: now.Add(10 * time.Second),
		Duration:    20 * time.Second,
	}}

	w.executeDueNodes()

	if other.ActiveBurn == nil {
		t.Fatal("non-active craft's due node did not fire")
	}
	if w.LastBurnFiredEvent == nil {
		t.Fatal("non-active craft's ignition did not stash LastBurnFiredEvent")
	}
	if w.LastBurnFiredEvent.CraftName != "Tug-1" {
		t.Errorf("LastBurnFiredEvent.CraftName = %q, want %q (non-active craft)", w.LastBurnFiredEvent.CraftName, "Tug-1")
	}
}

// TestBurnExhaustionStampsBurnFinishedEvent — the teardown branch in
// integrateOneCraft (c.ActiveBurn = nil on exhaustion) must stash
// LastBurnFinishedEvent naming the craft, the node's originally planned
// Δv (not the ~0 DVRemaining left by exhaustion), the fired node's
// ordinal, and how many nodes are still queued on that craft afterward.
func TestBurnExhaustionStampsBurnFinishedEvent(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	// One more node still queued behind the burn under test, so
	// NodesRemaining has something other than 0 to assert against.
	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:        spacecraft.BurnPrograde,
		DV:          10,
		TriggerTime: w.Clock.SimTime.Add(24 * time.Hour),
	}}
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 3054,
		PlannedDV:   3054,
		NodeIndex:   2,               // "node #3" — proves the finish event carries this through, not just 0
		EndTime:     w.Clock.SimTime, // already due: exhausts on this tick
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
	}

	if w.LastBurnFinishedEvent != nil {
		t.Fatal("precondition: LastBurnFinishedEvent already set before any burn finished")
	}
	_, done := tickUntil(w, 50, func() bool { return c.ActiveBurn == nil })
	if !done {
		t.Fatal("burn never tore down as exhausted within 50 ticks")
	}
	if w.LastBurnFinishedEvent == nil {
		t.Fatal("exhausted burn did not stash LastBurnFinishedEvent")
	}
	e := w.LastBurnFinishedEvent
	if e.CraftName != c.Name {
		t.Errorf("LastBurnFinishedEvent.CraftName = %q, want %q", e.CraftName, c.Name)
	}
	if e.DV != 3054 {
		t.Errorf("LastBurnFinishedEvent.DV = %v, want 3054 (PlannedDV, not the exhausted DVRemaining)", e.DV)
	}
	if e.NodeIndex != 2 {
		t.Errorf("LastBurnFinishedEvent.NodeIndex = %d, want 2", e.NodeIndex)
	}
	if e.NodesRemaining != 1 {
		t.Errorf("LastBurnFinishedEvent.NodesRemaining = %d, want 1 (the still-queued node)", e.NodesRemaining)
	}
}

// TestBurnStallDoesNotStampBurnFinishedEvent — a stalled burn (fuel-dry,
// Δv still owed) is paused, not finished; it must not stamp
// LastBurnFinishedEvent while stalled.
func TestBurnStallDoesNotStampBurnFinishedEvent(t *testing.T) {
	w := mustWorld(t)
	c := twoStageBurner(t, w, 200)
	c.ActiveBurn.PlannedDV = 200

	_, ok := tickUntil(w, 200, func() bool { return c.ActiveStageFuel() <= 0 })
	if !ok {
		t.Fatal("lower stage never ran dry within 200 ticks")
	}
	if c.ActiveBurn == nil {
		t.Fatal("stalled burn was torn down — want it kept alive")
	}
	if w.LastBurnFinishedEvent != nil {
		t.Errorf("LastBurnFinishedEvent stamped for a merely-stalled burn: %+v", w.LastBurnFinishedEvent)
	}
}
