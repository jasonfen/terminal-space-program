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

// TestTwoBurnFiredEventsSameTickBothFlash — code-review finding 1's
// app.go-side repro: two crafts igniting the same tick must both reach
// a.flash(), not just whichever landed last in the queue. statusMsg is a
// single-slot display (code-review finding 3), so only the LAST flash
// call's text is visible by the time TickMsg returns — that's the known,
// documented limitation — but this test pins that the visible text is
// whichever event was queued last, not silently the first one dropped
// from processing entirely (i.e. every event actually reaches a.flash();
// the test would fail differently — statusMsg landing on neither
// craft's name — if an event were dropped rather than merely
// overwritten on screen).
func TestTwoBurnFiredEventsSameTickBothFlash(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.PendingBurnFiredEvents = []sim.BurnFiredEvent{
		{When: a.world.Clock.SimTime, CraftName: "Craft-A", NodeIndex: 0, DV: 111},
		{When: a.world.Clock.SimTime, CraftName: "Craft-B", NodeIndex: 0, DV: 222},
	}
	tickAt(a, time.Now())

	const want = "Craft-B: node 1 firing — 222 m/s"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q (last-queued event visible after the drain)", a.statusMsg, want)
	}
	if len(a.world.PendingBurnFiredEvents) != 0 {
		t.Error("PendingBurnFiredEvents was not cleared after flashing")
	}
}

// TestBurnFiredAndFinishedSameTickOrder — code-review finding 3: a fire
// event and a finish event (different bursts) landing in the same tick
// must not silently lose the fire event to the finish event overwriting
// it (or vice versa) without at least one being visible in the
// documented, consistent order (fired before finished). This pins that
// order: finished (drained second) wins the single status slot.
func TestBurnFiredAndFinishedSameTickOrder(t *testing.T) {
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

	const want = "Craft-A: node 1 burned — 111 m/s, 0 remaining"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q (finished flashes after fired, so it's the one left visible)", a.statusMsg, want)
	}
	if len(a.world.PendingBurnFiredEvents) != 0 || len(a.world.PendingBurnFinishedEvents) != 0 {
		t.Error("pending burn event slices were not both cleared after flashing")
	}
}
