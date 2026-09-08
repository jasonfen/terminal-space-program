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
// by internal/tui/app_burn_flash_test.go).
//
// PendingBurnFiredEvents / PendingBurnFinishedEvents are slices, not
// single pointers (code-review findings 1 and 2 on the first version of
// this PR): a single scalar silently clobbered an earlier craft's stash
// when two crafts ignited or exhausted in the same tick, and the
// NodeIndex math (len(kept)) undercounted whenever an earlier node in
// the same call fired impulsively (also never appended to kept).

// TestExecuteDueNodesStampsBurnFiredEvent — a due finite node igniting
// (the c.ActiveBurn = &ActiveBurn{...} branch of executeDueNodesFor)
// must append a BurnFiredEvent to PendingBurnFiredEvents with the
// craft's name, the node's DV, and its 0-based NodeIndex (the "node #N"
// convention) — per craft, not just the active one.
func TestExecuteDueNodesStampsBurnFiredEvent(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	now := w.Clock.SimTime

	firing := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnPrograde,
		DV:          3054,
		TriggerTime: now.Add(10 * time.Second),
		Duration:    60 * time.Second, // BurnStart = now - 20s: already due
	}
	c.Nodes = []spacecraft.ManeuverNode{firing}

	if len(w.PendingBurnFiredEvents) != 0 {
		t.Fatal("precondition: PendingBurnFiredEvents already non-empty before any burn fired")
	}
	w.executeDueNodes()

	if c.ActiveBurn == nil {
		t.Fatal("firing node did not start an ActiveBurn")
	}
	if len(w.PendingBurnFiredEvents) != 1 {
		t.Fatalf("PendingBurnFiredEvents has %d entries, want 1", len(w.PendingBurnFiredEvents))
	}
	e := w.PendingBurnFiredEvents[0]
	if e.CraftName != c.Name {
		t.Errorf("CraftName = %q, want %q", e.CraftName, c.Name)
	}
	if e.DV != 3054 {
		t.Errorf("DV = %v, want 3054", e.DV)
	}
	if e.NodeIndex != 0 {
		t.Errorf("NodeIndex = %d, want 0 (only node in the queue)", e.NodeIndex)
	}
	// The running burn itself must carry the same figures forward so the
	// finish event can report them later (DVRemaining drifts as the burn
	// integrates; PlannedDV/NodeIndex must not).
	if c.ActiveBurn.PlannedDV != 3054 {
		t.Errorf("ActiveBurn.PlannedDV = %v, want 3054", c.ActiveBurn.PlannedDV)
	}
}

// TestExecuteDueNodesFiredEventNodeIndexAfterImpulsiveFire — review
// finding 2's exact repro: an impulsive (Duration==0) plane-change node
// followed by a finite burn node, both due the same tick. The impulsive
// node fires and is dropped from kept without ever being appended (same
// as a finite fire), so a len(kept)-derived NodeIndex undercounted the
// finite node that follows it as "node #1" when it's really "node #2"
// (original ordinal 1). NodeIndex must read 1, not 0.
func TestExecuteDueNodesFiredEventNodeIndexAfterImpulsiveFire(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	now := w.Clock.SimTime

	impulsive := spacecraft.ManeuverNode{
		Mode: spacecraft.BurnPlaneChange,
		DV:   10,
		// Duration is zero, so BurnStart() == TriggerTime — already due,
		// so this fires impulsively (ApplyImpulsiveDir), not via
		// ActiveBurn, and is never appended to kept — same as a finite
		// fire.
		TriggerTime: now.Add(-5 * time.Second),
	}
	finite := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnPrograde,
		DV:          3054,
		TriggerTime: now.Add(10 * time.Second),
		Duration:    60 * time.Second, // BurnStart = now - 20s: already due
	}
	c.Nodes = []spacecraft.ManeuverNode{impulsive, finite}

	w.executeDueNodes()

	if c.ActiveBurn == nil {
		t.Fatal("finite node behind the impulsive one did not fire")
	}
	if len(w.PendingBurnFiredEvents) != 1 {
		t.Fatalf("PendingBurnFiredEvents has %d entries, want 1 (only the finite node stamps a fired event)", len(w.PendingBurnFiredEvents))
	}
	e := w.PendingBurnFiredEvents[0]
	if e.NodeIndex != 1 {
		t.Errorf("NodeIndex = %d, want 1 (this was c.Nodes[1] — the impulsive node ahead of it must still count)", e.NodeIndex)
	}
	if c.ActiveBurn.NodeIndex != 1 {
		t.Errorf("ActiveBurn.NodeIndex = %d, want 1", c.ActiveBurn.NodeIndex)
	}
}

// TestExecuteDueNodesStampsBurnFiredEventNonActiveCraft — decision 4 is
// explicit that this is a per-craft hook, not just the active vessel's:
// a burn igniting on a different craft in the slate must still append a
// BurnFiredEvent, naming that craft.
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
	if len(w.PendingBurnFiredEvents) != 1 {
		t.Fatalf("PendingBurnFiredEvents has %d entries, want 1", len(w.PendingBurnFiredEvents))
	}
	if w.PendingBurnFiredEvents[0].CraftName != "Tug-1" {
		t.Errorf("CraftName = %q, want %q (non-active craft)", w.PendingBurnFiredEvents[0].CraftName, "Tug-1")
	}
}

// TestExecuteDueNodesStampsBothCraftsFiredEventsSameTick — code-review
// finding 1's exact repro: two different crafts each have a due finite
// node. Firing both in the same executeDueNodes() call must not let the
// second craft's ignition clobber the first's — both must land in
// PendingBurnFiredEvents, in the order they fired.
func TestExecuteDueNodesStampsBothCraftsFiredEventsSameTick(t *testing.T) {
	w := mustWorld(t)
	first := w.ActiveCraft()
	if first == nil {
		t.Fatal("no active craft")
	}
	first.Name = "Craft-A"
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 500e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	second := w.Crafts[1]
	second.Name = "Craft-B"
	now := w.Clock.SimTime

	first.Nodes = []spacecraft.ManeuverNode{{
		Mode:        spacecraft.BurnPrograde,
		DV:          111,
		TriggerTime: now.Add(10 * time.Second),
		Duration:    20 * time.Second,
	}}
	second.Nodes = []spacecraft.ManeuverNode{{
		Mode:        spacecraft.BurnPrograde,
		DV:          222,
		TriggerTime: now.Add(10 * time.Second),
		Duration:    20 * time.Second,
	}}

	w.executeDueNodes()

	if first.ActiveBurn == nil || second.ActiveBurn == nil {
		t.Fatal("both crafts' due nodes should have fired")
	}
	if len(w.PendingBurnFiredEvents) != 2 {
		t.Fatalf("PendingBurnFiredEvents has %d entries, want 2 (one per craft) — got %+v",
			len(w.PendingBurnFiredEvents), w.PendingBurnFiredEvents)
	}
	names := map[string]bool{}
	for _, e := range w.PendingBurnFiredEvents {
		names[e.CraftName] = true
	}
	if !names["Craft-A"] || !names["Craft-B"] {
		t.Errorf("PendingBurnFiredEvents = %+v, want one entry each for Craft-A and Craft-B", w.PendingBurnFiredEvents)
	}
}

// TestBurnExhaustionStampsBurnFinishedEvent — the teardown branch in
// integrateOneCraft (c.ActiveBurn = nil on exhaustion) must append a
// BurnFinishedEvent naming the craft, the node's originally planned Δv
// (not the ~0 DVRemaining left by exhaustion), the fired node's ordinal,
// and how many nodes are still queued on that craft afterward.
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

	if len(w.PendingBurnFinishedEvents) != 0 {
		t.Fatal("precondition: PendingBurnFinishedEvents already non-empty before any burn finished")
	}
	_, done := tickUntil(w, 50, func() bool { return c.ActiveBurn == nil })
	if !done {
		t.Fatal("burn never tore down as exhausted within 50 ticks")
	}
	if len(w.PendingBurnFinishedEvents) != 1 {
		t.Fatalf("PendingBurnFinishedEvents has %d entries, want 1", len(w.PendingBurnFinishedEvents))
	}
	e := w.PendingBurnFinishedEvents[0]
	if e.CraftName != c.Name {
		t.Errorf("CraftName = %q, want %q", e.CraftName, c.Name)
	}
	if e.DV != 3054 {
		t.Errorf("DV = %v, want 3054 (PlannedDV, not the exhausted DVRemaining)", e.DV)
	}
	if e.NodeIndex != 2 {
		t.Errorf("NodeIndex = %d, want 2", e.NodeIndex)
	}
	if e.NodesRemaining != 1 {
		t.Errorf("NodesRemaining = %d, want 1 (the still-queued node)", e.NodesRemaining)
	}
}

// TestExecuteOneCraftStampsBothCraftsFinishedEventsSameTick — the
// finish-side twin of TestExecuteDueNodesStampsBothCraftsFiredEventsSameTick:
// two crafts' burns both exhaust on the same tick must both land in
// PendingBurnFinishedEvents.
func TestExecuteOneCraftStampsBothCraftsFinishedEventsSameTick(t *testing.T) {
	w := mustWorld(t)
	first := w.ActiveCraft()
	if first == nil {
		t.Fatal("no active craft")
	}
	first.Name = "Craft-A"
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 500e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	second := w.Crafts[1]
	second.Name = "Craft-B"

	mk := func(dv float64) *spacecraft.ActiveBurn {
		return &spacecraft.ActiveBurn{
			Mode:        spacecraft.BurnPrograde,
			DVRemaining: dv,
			PlannedDV:   dv,
			EndTime:     w.Clock.SimTime, // already due: exhausts on this tick
			Throttle:    1,
		}
	}
	first.ActiveBurn = mk(111)
	first.ActiveBurn.PrimaryID = first.Primary.ID
	second.ActiveBurn = mk(222)
	second.ActiveBurn.PrimaryID = second.Primary.ID

	_, done := tickUntil(w, 50, func() bool { return first.ActiveBurn == nil && second.ActiveBurn == nil })
	if !done {
		t.Fatal("both burns never tore down as exhausted within 50 ticks")
	}
	if len(w.PendingBurnFinishedEvents) != 2 {
		t.Fatalf("PendingBurnFinishedEvents has %d entries, want 2 — got %+v",
			len(w.PendingBurnFinishedEvents), w.PendingBurnFinishedEvents)
	}
	names := map[string]bool{}
	for _, e := range w.PendingBurnFinishedEvents {
		names[e.CraftName] = true
	}
	if !names["Craft-A"] || !names["Craft-B"] {
		t.Errorf("PendingBurnFinishedEvents = %+v, want one entry each for Craft-A and Craft-B", w.PendingBurnFinishedEvents)
	}
}

// TestBurnStallDoesNotStampBurnFinishedEvent — a stalled burn (fuel-dry,
// Δv still owed) is paused, not finished; it must not append a
// BurnFinishedEvent while stalled.
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
	if len(w.PendingBurnFinishedEvents) != 0 {
		t.Errorf("PendingBurnFinishedEvents non-empty for a merely-stalled burn: %+v", w.PendingBurnFinishedEvents)
	}
}
