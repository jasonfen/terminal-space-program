package missions

import "testing"

// #426 (Flight School content): extends the event objective family with
// optional gates checked at the moment the Action fired, so a control-
// teaching step can require more than "the key was pressed at all" — the
// UX review's finding that pressing [t] once, landing on Mercury, still
// credited "aim for the Moon".

// TestEventRequireTargetBodyIDGatesOnBoundTarget — cycle_target only credits
// when the bound Target is the named body, not the first press.
func TestEventRequireTargetBodyIDGatesOnBoundTarget(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{Action: ActionCycleTarget, RequireTargetBodyID: "moon"}}

	// First press lands on Mercury: action fired, but the bound target isn't
	// the Moon — must NOT credit.
	got := o.Evaluate(EvalContext{RecentActions: []Action{ActionCycleTarget}, TargetBodyID: "mercury"})
	if got != InProgress {
		t.Fatalf("cycle_target to Mercury: got %v, want InProgress", got)
	}

	// A later press lands on the Moon: now it credits.
	got = o.Evaluate(EvalContext{RecentActions: []Action{ActionCycleTarget}, TargetBodyID: "moon"})
	if got != Passed {
		t.Fatalf("cycle_target to Moon: got %v, want Passed", got)
	}
}

// TestEventRequireTargetBodyIDNoActionStillGated — the action must ALSO have
// fired; simply having the Moon bound already (with no press this tick)
// must not credit.
func TestEventRequireTargetBodyIDNoActionStillGated(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{Action: ActionCycleTarget, RequireTargetBodyID: "moon"}}
	got := o.Evaluate(EvalContext{TargetBodyID: "moon"})
	if got != InProgress {
		t.Fatalf("Moon bound but no cycle_target action fired: got %v, want InProgress", got)
	}
}

// TestEventRequireBudgetOKGatesOnOverBudgetNode — plan_transfer only credits
// when no planted node is over budget (ADR 0047 §2). An over-budget plan
// still plants (warn-and-allow); it just doesn't teach "you can afford
// this."
func TestEventRequireBudgetOKGatesOnOverBudgetNode(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{
		Action:              ActionPlanTransfer,
		RequireTargetBodyID: "moon",
		RequireBudgetOK:     true,
	}}

	// Planted a 7431 m/s transfer against a 5910 m/s budget (the issue's own
	// numbers) — over budget, must NOT credit even though it's aimed at the Moon.
	got := o.Evaluate(EvalContext{
		RecentActions:     []Action{ActionPlanTransfer},
		TargetBodyID:      "moon",
		HasOverBudgetNode: true,
	})
	if got != InProgress {
		t.Fatalf("over-budget Moon transfer: got %v, want InProgress", got)
	}

	// An affordable Moon transfer credits.
	got = o.Evaluate(EvalContext{
		RecentActions:     []Action{ActionPlanTransfer},
		TargetBodyID:      "moon",
		HasOverBudgetNode: false,
	})
	if got != Passed {
		t.Fatalf("affordable Moon transfer: got %v, want Passed", got)
	}
}

// TestEventRequireTargetBodyIDRejectsNonMoonEvenAffordable — a transfer
// planted at a body other than the Moon (even a perfectly affordable one)
// must not credit "plant a transfer to the Moon."
func TestEventRequireTargetBodyIDRejectsNonMoonEvenAffordable(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{
		Action:              ActionPlanTransfer,
		RequireTargetBodyID: "moon",
		RequireBudgetOK:     true,
	}}
	got := o.Evaluate(EvalContext{
		RecentActions:     []Action{ActionPlanTransfer},
		TargetBodyID:      "mercury",
		HasOverBudgetNode: false,
	})
	if got != InProgress {
		t.Fatalf("affordable Mercury transfer: got %v, want InProgress (not the Moon)", got)
	}
}

// TestEventRequireSpawnLocationPad — spawn_craft only credits "spawn on the
// pad" when the newly-active (spawned) craft's OnPad flag is true.
func TestEventRequireSpawnLocationPad(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{Action: ActionSpawnCraft, RequireSpawnLocation: "pad"}}

	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionSpawnCraft}, OnPad: false}); got != InProgress {
		t.Fatalf("spawned in orbit: got %v, want InProgress", got)
	}
	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionSpawnCraft}, OnPad: true}); got != Passed {
		t.Fatalf("spawned on pad: got %v, want Passed", got)
	}
}

// TestEventRequireSpawnLocationOrbit — the inverse gate for tut-dock's
// "spawn a partner IN ORBIT" step.
func TestEventRequireSpawnLocationOrbit(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{Action: ActionSpawnCraft, RequireSpawnLocation: "orbit"}}

	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionSpawnCraft}, OnPad: true}); got != InProgress {
		t.Fatalf("spawned on pad: got %v, want InProgress", got)
	}
	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionSpawnCraft}, OnPad: false}); got != Passed {
		t.Fatalf("spawned in orbit: got %v, want Passed", got)
	}
}

// TestEventRequireTargetCraftGatesOnCraftTarget — tut-dock's "target it"
// step only credits when the bound Target is a craft, not a body.
func TestEventRequireTargetCraftGatesOnCraftTarget(t *testing.T) {
	o := Objective{Kind: KindEvent, Params: Params{Action: ActionCycleTarget, RequireTargetCraft: true}}

	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionCycleTarget}, TargetIsCraft: false}); got != InProgress {
		t.Fatalf("target is a body: got %v, want InProgress", got)
	}
	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionCycleTarget}, TargetIsCraft: true}); got != Passed {
		t.Fatalf("target is a craft: got %v, want Passed", got)
	}
}
