package missions

import "testing"

// These cover the v0.21 Slice 5 player-surface helpers: the pure
// progress / current-objective / requirement-gating / fail-reason
// accessors the ladder screen and checklist chip read. They never touch
// the tui or sim — same dependency boundary as the rest of the package.

func threeStep() Mission {
	return Mission{
		ID: "three",
		Objectives: []Objective{
			{Kind: KindSOIFlyby, Name: "step 1", Params: Params{PrimaryID: "a"}},
			{Kind: KindSOIFlyby, Name: "step 2", Params: Params{PrimaryID: "b"}},
			{Kind: KindSOIFlyby, Name: "step 3", Params: Params{PrimaryID: "c"}},
		},
	}
}

func TestMissionProgress(t *testing.T) {
	m := threeStep()
	if p, total := m.Progress(); p != 0 || total != 3 {
		t.Fatalf("fresh mission progress = %d/%d, want 0/3", p, total)
	}
	m.Objectives[0].Status = Passed
	if p, total := m.Progress(); p != 1 || total != 3 {
		t.Fatalf("one-passed progress = %d/%d, want 1/3", p, total)
	}
	m.Objectives[1].Status = Passed
	m.Objectives[2].Status = Passed
	if p, total := m.Progress(); p != 3 || total != 3 {
		t.Fatalf("all-passed progress = %d/%d, want 3/3", p, total)
	}
	// A Failed objective is not Passed, so it does not count toward progress.
	m.Objectives[2].Status = Failed
	if p, total := m.Progress(); p != 2 || total != 3 {
		t.Fatalf("with-failed progress = %d/%d, want 2/3", p, total)
	}
}

func TestMissionCurrentObjective(t *testing.T) {
	m := threeStep()
	// Fresh: the first objective is current.
	if o, ok := m.CurrentObjective(); !ok || o.Name != "step 1" {
		t.Fatalf("fresh current = %q ok=%v, want step 1 true", o.Name, ok)
	}
	// Pass the first: the second becomes current.
	m.Objectives[0].Status = Passed
	if o, ok := m.CurrentObjective(); !ok || o.Name != "step 2" {
		t.Fatalf("after one passed, current = %q ok=%v, want step 2 true", o.Name, ok)
	}
	// All passed: no current objective.
	m.Objectives[1].Status = Passed
	m.Objectives[2].Status = Passed
	if o, ok := m.CurrentObjective(); ok {
		t.Fatalf("all passed current = %q ok=%v, want !ok", o.Name, ok)
	}
	// Empty mission: no current objective.
	if _, ok := (Mission{ID: "empty"}).CurrentObjective(); ok {
		t.Fatal("empty mission reported a current objective")
	}
}

func TestMissionRequirementsMet(t *testing.T) {
	// No requirements → always met (ungated rung).
	if !(Mission{ID: "free"}).RequirementsMet(nil) {
		t.Fatal("no-requirements mission should be unlocked")
	}
	m := Mission{ID: "gated", Requires: []string{"a", "b"}}
	if m.RequirementsMet(map[string]bool{"a": true}) {
		t.Fatal("missing requirement b should keep the rung locked")
	}
	if !m.RequirementsMet(map[string]bool{"a": true, "b": true}) {
		t.Fatal("all requirements present should unlock the rung")
	}
	// A required ID present-but-false (shouldn't happen — the set holds only
	// Passed IDs — but be defensive) counts as unmet.
	if m.RequirementsMet(map[string]bool{"a": true, "b": false}) {
		t.Fatal("present-but-false requirement should not unlock")
	}
}

func TestObjectiveFailReason(t *testing.T) {
	crash := Objective{Kind: KindCircularize, FailOn: []FailCondition{FailCrashed}}
	if fc, ok := crash.FailReason(EvalContext{Crashed: true}); !ok || fc != FailCrashed {
		t.Fatalf("crashed reason = %q ok=%v, want crashed true", fc, ok)
	}
	if _, ok := crash.FailReason(EvalContext{Crashed: false}); ok {
		t.Fatal("not crashed should report no fail reason")
	}
	fuel := Objective{Kind: KindCircularize, FailOn: []FailCondition{FailOutOfFuel}}
	if fc, ok := fuel.FailReason(EvalContext{TotalFuelKg: 0}); !ok || fc != FailOutOfFuel {
		t.Fatalf("dry reason = %q ok=%v, want out_of_fuel true", fc, ok)
	}
	if _, ok := fuel.FailReason(EvalContext{TotalFuelKg: 10}); ok {
		t.Fatal("fuel remaining should report no fail reason")
	}
	// No opt-in conditions → never a reason, even when crashed and dry.
	none := Objective{Kind: KindCircularize}
	if _, ok := none.FailReason(EvalContext{Crashed: true, TotalFuelKg: 0}); ok {
		t.Fatal("no fail_on objective should never report a fail reason")
	}
}

func TestMissionFailedObjective(t *testing.T) {
	m := threeStep()
	if _, ok := m.FailedObjective(); ok {
		t.Fatal("no objective failed yet")
	}
	m.Objectives[1].Status = Failed
	o, ok := m.FailedObjective()
	if !ok || o.Name != "step 2" {
		t.Fatalf("failed objective = %q ok=%v, want step 2 true", o.Name, ok)
	}
}

func TestObjectiveLabel(t *testing.T) {
	if got := (Objective{Name: "n", Description: "d", Kind: KindDock}).Label(); got != "n" {
		t.Errorf("name-priority label = %q, want n", got)
	}
	if got := (Objective{Description: "d", Kind: KindDock}).Label(); got != "d" {
		t.Errorf("description-fallback label = %q, want d", got)
	}
	if got := (Objective{Kind: KindDock}).Label(); got != "dock" {
		t.Errorf("kind-fallback label = %q, want dock", got)
	}
}

func TestFailConditionLabel(t *testing.T) {
	if got := FailCrashed.Label(); got != "crashed" {
		t.Errorf("FailCrashed label = %q, want crashed", got)
	}
	if got := FailOutOfFuel.Label(); got != "out of fuel" {
		t.Errorf("FailOutOfFuel label = %q, want 'out of fuel'", got)
	}
}
