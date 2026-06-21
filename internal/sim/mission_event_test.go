package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// v0.21 Slice 4 (ADR 0025 §6/§7) — the sim event sink. World.RecordAction is
// the downward seam the TUI input layer calls (tui -> sim -> missions); the
// evaluator drains the sink each tick so events only match during the tick
// they fired.

// TestRecordActionWiresRecentActions — RecordAction feeds the EvalContext sink
// the event objective reads.
func TestRecordActionWiresRecentActions(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.RecordAction(missions.ActionStage)
	w.RecordAction(missions.ActionOpenManeuver)
	ctx := w.missionEvalContext()
	if len(ctx.RecentActions) != 2 || ctx.RecentActions[0] != missions.ActionStage || ctx.RecentActions[1] != missions.ActionOpenManeuver {
		t.Fatalf("RecentActions: got %v, want [stage open_maneuver]", ctx.RecentActions)
	}
}

// TestTickDrainsActionSink — evaluateMissions drains the sink each tick, so a
// recorded action does not linger to latch a later-active objective.
func TestTickDrainsActionSink(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.RecordAction(missions.ActionStage)
	w.Tick()
	if len(w.recentActions) != 0 {
		t.Fatalf("sink after tick: got %d actions, want 0 (drained)", len(w.recentActions))
	}
}

// TestActionSinkDrainsWithNoMissions — the sink drains even when no mission is
// loaded, so actions can't accumulate unbounded across a missionless session.
func TestActionSinkDrainsWithNoMissions(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = nil
	w.RecordAction(missions.ActionStage)
	w.Tick()
	if len(w.recentActions) != 0 {
		t.Fatalf("sink after missionless tick: got %d, want 0", len(w.recentActions))
	}
}

// TestActionSinkDrainsWithEmptySlate — the sink drains every tick even with
// NO crafts (empty slate after EndFlight). spawn_craft / auto_warp / cycle_view
// aren't craft-gated, so without an unconditional drain the sink would grow
// unbounded AND stale actions would survive to latch a freshly-active event
// objective on the first post-spawn tick (ADR 0025 §6 since-active guarantee).
func TestActionSinkDrainsWithEmptySlate(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Crafts = nil // empty slate — last craft ended
	w.RecordAction(missions.ActionSpawnCraft)
	w.Tick()
	if len(w.recentActions) != 0 {
		t.Fatalf("sink after empty-slate tick: got %d, want 0 (drained regardless of crafts)", len(w.recentActions))
	}
}

// TestEventObjectivePassesThroughTick — the integration path: an event
// objective passes when its action is recorded before the tick, and a fresh
// one stays InProgress when no action is recorded (proves the drain).
func TestEventObjectivePassesThroughTick(t *testing.T) {
	mk := func() *World {
		w, err := NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		w.Missions = []missions.Mission{{
			ID:         "event-stage",
			Objectives: []missions.Objective{{Kind: missions.KindEvent, Params: missions.Params{Action: missions.ActionStage}}},
		}}
		return w
	}

	// Recorded before the tick -> passes.
	w := mk()
	w.RecordAction(missions.ActionStage)
	w.Tick()
	if w.Missions[0].Status != missions.Passed {
		t.Fatalf("with stage recorded: got %v, want Passed", w.Missions[0].Status)
	}

	// Not recorded -> stays InProgress (the sink was drained, nothing matches).
	w2 := mk()
	w2.Tick()
	if w2.Missions[0].Status != missions.InProgress {
		t.Fatalf("without action: got %v, want InProgress", w2.Missions[0].Status)
	}
}
