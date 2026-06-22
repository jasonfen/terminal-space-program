package missions

import "testing"

// v0.21 Slice 4 (ADR 0025 §6/§7) — the event objective family. An event
// objective matches a semantic gameplay Action that fired *while it was
// active*. The sink (ctx.RecentActions) holds actions recorded since the last
// tick and is drained each tick by the sim, so an action fired before the
// objective became active was already drained and cannot latch it.

// TestEventObjectivePassesOnMatchingAction — the matching action in the
// since-last-tick sink passes the objective.
func TestEventObjectivePassesOnMatchingAction(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{Action: ActionStage}}
	ctx := EvalContext{RecentActions: []Action{ActionThrottleFull, ActionStage}}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("stage in sink: got %v, want Passed", got)
	}
}

// TestEventObjectiveInProgressWithoutAction — no action, or only non-matching
// actions, leaves it InProgress.
func TestEventObjectiveInProgressWithoutAction(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{Action: ActionStage}}
	if got := o.Evaluate(EvalContext{}); got != InProgress {
		t.Fatalf("empty sink: got %v, want InProgress", got)
	}
	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionThrottleFull, ActionCycleView}}); got != InProgress {
		t.Fatalf("non-matching actions: got %v, want InProgress", got)
	}
}

// TestEventObjectiveEmptyActionInert — an event objective with no Action param
// never passes (a misconfigured catalog entry sits inert, never matching the
// empty string against recorded actions).
func TestEventObjectiveEmptyActionInert(t *testing.T) {
	o := Objective{Kind: KindEvent}
	if got := o.Evaluate(EvalContext{RecentActions: []Action{"", ActionStage}}); got != InProgress {
		t.Fatalf("empty Action param: got %v, want InProgress", got)
	}
}

// TestEventMissionSinceActiveWindow — the defining contract: an event
// objective passes ONLY when its action fires during its active window. A
// two-step event mission (stage -> open_maneuver) must not let an
// open_maneuver fired BEFORE step 1 passed satisfy step 2; step 2 needs its
// own open_maneuver after it becomes active. This mirrors the per-tick drain
// the sim performs (each Evaluate call here gets a fresh "tick" of actions).
func TestEventMissionSinceActiveWindow(t *testing.T) {
	m := Mission{
		ID: "stage-then-maneuver",
		Objectives: []Objective{
			{Kind: KindEvent, Params: Params{Action: ActionStage}},
			{Kind: KindEvent, Params: Params{Action: ActionOpenManeuver}},
		},
	}
	// Tick 1: player opens the maneuver planner before staging. Step 1
	// (stage) doesn't match; step 2 is locked and never sees this action.
	if got := m.Evaluate(EvalContext{RecentActions: []Action{ActionOpenManeuver}}); got != InProgress {
		t.Fatalf("tick 1 (open before stage): got %v, want InProgress", got)
	}
	if m.Objectives[1].Status == Passed {
		t.Fatal("step 2 latched on an open_maneuver fired before it was active")
	}
	// Tick 2: player stages. Step 1 passes. Step 2 becomes active this tick,
	// but the only action present is stage (open_maneuver was drained with
	// tick 1), so step 2 must stay InProgress.
	if got := m.Evaluate(EvalContext{RecentActions: []Action{ActionStage}}); got != InProgress {
		t.Fatalf("tick 2 (stage): got %v, want InProgress", got)
	}
	if m.Objectives[0].Status != Passed {
		t.Fatalf("step 1 after stage: got %v, want Passed", m.Objectives[0].Status)
	}
	if m.Objectives[1].Status != InProgress {
		t.Fatalf("step 2 after stage: got %v, want InProgress (needs its own open_maneuver)", m.Objectives[1].Status)
	}
	// Tick 3: player opens the maneuver planner again, now while step 2 is
	// active. Step 2 passes, completing the mission.
	if got := m.Evaluate(EvalContext{RecentActions: []Action{ActionOpenManeuver}}); got != Passed {
		t.Fatalf("tick 3 (open while step 2 active): got %v, want Passed", got)
	}
}

// TestEventObjectiveSameTickPassThrough — when step 1's action and step 2's
// action both fire in the same tick, the ordered pass-through still lets both
// pass that tick (step 1 passes, step 2 is evaluated the same tick against the
// same action set). Pins the same-tick behaviour explicitly.
func TestEventObjectiveSameTickPassThrough(t *testing.T) {
	m := Mission{
		ID: "two-events",
		Objectives: []Objective{
			{Kind: KindEvent, Params: Params{Action: ActionStage}},
			{Kind: KindEvent, Params: Params{Action: ActionToggleBurn}},
		},
	}
	if got := m.Evaluate(EvalContext{RecentActions: []Action{ActionStage, ActionToggleBurn}}); got != Passed {
		t.Fatalf("both actions same tick: got %v, want Passed", got)
	}
}
