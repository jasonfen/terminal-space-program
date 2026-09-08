package tui

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// This file pins decision 4's app.go half (grilled 2026-09-06, ADR 0048):
// World.LastBurnFiredEvent / LastBurnFinishedEvent must reach the player
// as an Event Flash, worded in the ADR's own voice, and clear after one
// fire — same contract as LastDockEvent / LastNodeTargetRefusal.

// TestBurnFiredEventFlashesAndClears — the sim.TickMsg case reads
// World.LastBurnFiredEvent, flashes "<craft>: node <N> firing — <DV>
// m/s", and nils the field so a single ignition only flashes once.
func TestBurnFiredEventFlashesAndClears(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.LastBurnFiredEvent = &sim.BurnFiredEvent{
		When:      a.world.Clock.SimTime,
		CraftName: "S-IVB-1",
		NodeIndex: 0,
		DV:        3054,
	}
	tickAt(a, time.Now())

	const want = "S-IVB-1: node 1 firing — 3054 m/s"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
	if a.world.LastBurnFiredEvent != nil {
		t.Error("LastBurnFiredEvent was not cleared after flashing")
	}
}

// TestBurnFinishedEventFlashesAndClears — the ADR's own worked example
// wording verbatim: "node 1 burned — 3054 m/s, 2 remaining".
func TestBurnFinishedEventFlashesAndClears(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.LastBurnFinishedEvent = &sim.BurnFinishedEvent{
		When:           a.world.Clock.SimTime,
		CraftName:      "S-IVB-1",
		NodeIndex:      0,
		DV:             3054,
		NodesRemaining: 2,
	}
	tickAt(a, time.Now())

	const want = "S-IVB-1: node 1 burned — 3054 m/s, 2 remaining"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
	if a.world.LastBurnFinishedEvent != nil {
		t.Error("LastBurnFinishedEvent was not cleared after flashing")
	}
}
