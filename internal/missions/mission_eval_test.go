package missions

import "testing"

// Mission.Evaluate is the v0.21 container behaviour: it rolls an ordered
// list of Objectives up into a single Mission status. These tests pin the
// sequencing contract (ADR 0025 §2) using soi_flyby objectives, whose
// pass condition is just "current primary == target" — so a single
// EvalContext can satisfy or withhold each step deterministically.

// flybyMission returns a fresh two-step mission: flyby A, then flyby B.
func flybyMission(a, b string) Mission {
	return Mission{
		ID: "test-flyby",
		Objectives: []Objective{
			{Kind: KindSOIFlyby, Params: Params{PrimaryID: a}},
			{Kind: KindSOIFlyby, Params: Params{PrimaryID: b}},
		},
	}
}

// TestMissionOrderedSecondDoesNotLatchEarly — the defining ordered
// behaviour: even when the SECOND objective's condition is already
// satisfied, it must not pass while the FIRST is still in progress.
func TestMissionOrderedSecondDoesNotLatchEarly(t *testing.T) {
	m := flybyMission("moon", "mars")
	// At Mars: objective 2 (mars) would pass on its own, but objective 1
	// (moon) hasn't, so ordering must hold both back.
	if got := m.Evaluate(EvalContext{PrimaryID: "mars"}); got != InProgress {
		t.Fatalf("mission status: got %v, want InProgress", got)
	}
	if m.Objectives[1].Status == Passed {
		t.Fatal("second objective latched before the first passed")
	}
}

// TestMissionPassesAfterObjectivesPassInOrder — the mission passes only
// once every objective has passed, in sequence across ticks.
func TestMissionPassesAfterObjectivesPassInOrder(t *testing.T) {
	m := flybyMission("moon", "mars")

	// Tick 1 at the Moon: objective 1 passes; objective 2 is evaluated the
	// same tick (its predecessor passed) but the craft isn't at Mars yet.
	if got := m.Evaluate(EvalContext{PrimaryID: "moon"}); got != InProgress {
		t.Fatalf("after moon flyby: got %v, want InProgress", got)
	}
	if m.Objectives[0].Status != Passed {
		t.Fatalf("objective 1 after moon flyby: got %v, want Passed", m.Objectives[0].Status)
	}
	if m.Objectives[1].Status != InProgress {
		t.Fatalf("objective 2 after moon flyby: got %v, want InProgress", m.Objectives[1].Status)
	}

	// Tick 2 at Mars: objective 2 passes, completing the mission.
	if got := m.Evaluate(EvalContext{PrimaryID: "mars"}); got != Passed {
		t.Fatalf("after mars flyby: got %v, want Passed", got)
	}
	if m.Objectives[1].Status != Passed {
		t.Fatalf("objective 2 after mars flyby: got %v, want Passed", m.Objectives[1].Status)
	}
}

// TestMissionStickyOnTerminal — a Passed mission stays Passed regardless
// of later context (mirrors Objective idempotency).
func TestMissionStickyOnTerminal(t *testing.T) {
	m := flybyMission("moon", "mars")
	m.Status = Passed
	if got := m.Evaluate(EvalContext{PrimaryID: "earth"}); got != Passed {
		t.Fatalf("sticky terminal: got %v, want Passed", got)
	}
}

// TestMissionEmptyNeverPasses — a mission with no objectives is never
// complete; it can't accidentally roll up to Passed on an empty loop.
func TestMissionEmptyNeverPasses(t *testing.T) {
	m := Mission{ID: "empty"}
	if got := m.Evaluate(EvalContext{PrimaryID: "mars"}); got != InProgress {
		t.Fatalf("empty mission: got %v, want InProgress", got)
	}
}

// TestMissionSingleObjectiveBehavesLikePredicate — the embedded starter
// catalog wraps each legacy predicate in a one-objective mission; that
// shape must behave exactly like the old single predicate.
func TestMissionSingleObjectiveBehavesLikePredicate(t *testing.T) {
	m := Mission{
		ID:         "single",
		Objectives: []Objective{{Kind: KindSOIFlyby, Params: Params{PrimaryID: "mars"}}},
	}
	if got := m.Evaluate(EvalContext{PrimaryID: "earth"}); got != InProgress {
		t.Fatalf("not at mars: got %v, want InProgress", got)
	}
	if got := m.Evaluate(EvalContext{PrimaryID: "mars"}); got != Passed {
		t.Fatalf("at mars: got %v, want Passed", got)
	}
}
