package tui

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// This file pins decision 4's app.go half (grilled 2026-09-06, ADR 0048):
// World.PendingBurnFiredEvents / PendingBurnFinishedEvents must reach the
// player as Event Flashes, worded in the ADR's own voice, and drain to
// empty after one TickMsg — same contract as LastDockEvent /
// LastNodeTargetRefusal, generalized to a queue (code-review finding 1:
// a single scalar silently dropped a same-tick second event).

// TestBurnFiredEventFlashesAndClears — the sim.TickMsg case reads
// World.PendingBurnFiredEvents, flashes "<craft>: node <N> firing — <DV>
// m/s" for each, and empties the slice so a single ignition only flashes
// once.
func TestBurnFiredEventFlashesAndClears(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.PendingBurnFiredEvents = []sim.BurnFiredEvent{{
		When:      a.world.Clock.SimTime,
		CraftName: "S-IVB-1",
		NodeIndex: 0,
		DV:        3054,
	}}
	tickAt(a, time.Now())

	const want = "S-IVB-1: node 1 firing — 3054 m/s"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
	if len(a.world.PendingBurnFiredEvents) != 0 {
		t.Error("PendingBurnFiredEvents was not cleared after flashing")
	}
}

// TestBurnFinishedEventFlashesAndClears — the ADR's own worked example
// wording verbatim: "node 1 burned — 3054 m/s, 2 remaining".
func TestBurnFinishedEventFlashesAndClears(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.PendingBurnFinishedEvents = []sim.BurnFinishedEvent{{
		When:           a.world.Clock.SimTime,
		CraftName:      "S-IVB-1",
		NodeIndex:      0,
		DV:             3054,
		NodesRemaining: 2,
	}}
	tickAt(a, time.Now())

	const want = "S-IVB-1: node 1 burned — 3054 m/s, 2 remaining"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
	if len(a.world.PendingBurnFinishedEvents) != 0 {
		t.Error("PendingBurnFinishedEvents was not cleared after flashing")
	}
}

// TestTwoBurnFiredEventsSameTickBothVisible — code-review finding 1's
// app.go-side repro: two crafts igniting the same tick must both reach
// the player, not just whichever landed last in the queue. Review
// finding 6: rather than separate a.flash() calls overwriting each
// other on the single-slot status line (the original fix's documented
// but avoidable limitation), same-tick messages are joined into one
// flash call — so both events are genuinely visible at once, not a
// last-write-wins coin-flip.
func TestTwoBurnFiredEventsSameTickBothVisible(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.PendingBurnFiredEvents = []sim.BurnFiredEvent{
		{When: a.world.Clock.SimTime, CraftName: "Craft-A", NodeIndex: 0, DV: 111},
		{When: a.world.Clock.SimTime, CraftName: "Craft-B", NodeIndex: 0, DV: 222},
	}
	tickAt(a, time.Now())

	const want = "Craft-A: node 1 firing — 111 m/s · Craft-B: node 1 firing — 222 m/s"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q (both events joined into one visible flash)", a.statusMsg, want)
	}
	if len(a.world.PendingBurnFiredEvents) != 0 {
		t.Error("PendingBurnFiredEvents was not cleared after flashing")
	}
}

// TestBurnFiredAndFinishedSameTickBothVisible — code-review finding 3: a
// fire event and a finish event (different bursts) landing in the same
// tick must not silently lose one to the other overwriting it. Review
// finding 6's join means both are visible, in the drain's fired-before-
// finished order, rather than only the last-drained one surviving.
func TestBurnFiredAndFinishedSameTickBothVisible(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.PendingBurnFiredEvents = []sim.BurnFiredEvent{
		{When: a.world.Clock.SimTime, CraftName: "Craft-B", NodeIndex: 0, DV: 500},
	}
	a.world.PendingBurnFinishedEvents = []sim.BurnFinishedEvent{
		{When: a.world.Clock.SimTime, CraftName: "Craft-A", NodeIndex: 0, DV: 111, NodesRemaining: 0},
	}
	tickAt(a, time.Now())

	const want = "Craft-B: node 1 firing — 500 m/s · Craft-A: node 1 burned — 111 m/s, 0 remaining"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q (fired then finished, both visible)", a.statusMsg, want)
	}
	if len(a.world.PendingBurnFiredEvents) != 0 || len(a.world.PendingBurnFinishedEvents) != 0 {
		t.Error("pending burn event slices were not both cleared after flashing")
	}
}

// TestBurnFinishedZeroDVDropsFigureInsteadOfLying — #447 review finding
// 8: a pre-PR save loaded mid-burn decodes ActiveBurn.PlannedDV as its
// zero value, so a BurnFinishedEvent with DV==0 must not print "burned
// — 0 m/s" (a false statement about a burn that really delivered Δv) —
// drop the whole "— N m/s" clause instead.
func TestBurnFinishedZeroDVDropsFigureInsteadOfLying(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.PendingBurnFinishedEvents = []sim.BurnFinishedEvent{
		{When: a.world.Clock.SimTime, CraftName: "S-IVB-1", NodeIndex: 0, DV: 0, NodesRemaining: 1},
	}
	tickAt(a, time.Now())

	const want = "S-IVB-1: node 1 burned — 1 remaining"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q (no fabricated 0 m/s figure)", a.statusMsg, want)
	}
}
