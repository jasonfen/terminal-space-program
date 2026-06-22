package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// v0.21 Slice 4 (ADR 0025 §6/§7) — the full tui→sim→missions path. Pressing a
// bound key records its semantic action downward, and an event objective
// waiting on that action passes on the next mission-eval tick. Proves the
// input layer is actually wired to World.RecordAction (not just that the sink
// works in isolation).
func TestKeypressRecordsActionForEventObjective(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.active = screenOrbit
	a.world.Clock.Paused = false
	a.world.Missions = []missions.Mission{{
		ID:         "press-cycle-view",
		Objectives: []missions.Objective{{Kind: missions.KindEvent, Params: missions.Params{Action: missions.ActionCycleView}}},
	}}

	// Press 'v' (CycleView) — the handler must record cycle_view downward.
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	// Next tick drains the sink into the evaluator; the event objective passes.
	a.world.Tick()
	if a.world.Missions[0].Status != missions.Passed {
		t.Fatalf("event objective after 'v' keypress: got %v, want Passed", a.world.Missions[0].Status)
	}
}
