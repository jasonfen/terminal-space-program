package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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

// The [»Burn] title-bar button is G by mouse, so a click records the same
// auto_warp action G does (Jason 2026-10-09: "make the button record the
// auto-warp action too"), on the map and in the launch view.
func TestBurnButtonClickRecordsAutoWarpAction(t *testing.T) {
	for _, view := range []sim.ViewMode{sim.ViewTop, sim.ViewLaunch} {
		a, err := New(nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
		a.active = screenOrbit
		a.world.Clock.Paused = false
		a.world.PlanNode(sim.ManeuverNode{
			DV:          10,
			Mode:        spacecraft.BurnPrograde,
			TriggerTime: a.world.Clock.SimTime.Add(2 * time.Hour),
		})
		a.world.Missions = []missions.Mission{{
			ID:         "click-auto-warp",
			Objectives: []missions.Objective{{Kind: missions.KindEvent, Params: missions.Params{Action: missions.ActionAutoWarp}}},
		}}
		a.world.ViewMode = view
		a.View() // lays out the title bar's hit-test ranges
		hit := a.orbitView.HitBurnButton
		if view == sim.ViewLaunch {
			hit = a.launchView.HitBurnButton
		}
		col, ok := findHitCol(140, hit)
		if !ok {
			t.Fatalf("%v: no [»Burn] hit-test range", view)
		}
		a.Update(tea.MouseMsg{X: col, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if !a.world.AutoWarpEngaged() {
			t.Fatalf("%v: the click did not engage Auto-Warp", view)
		}
		a.world.Tick()
		if a.world.Missions[0].Status != missions.Passed {
			t.Errorf("%v: auto_warp objective after a [»Burn] click: got %v, want Passed", view, a.world.Missions[0].Status)
		}
	}
}
