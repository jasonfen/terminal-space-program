package missions

import "testing"

// v0.21 Slice 3 (ADR 0025 §5) — opt-in failure semantics. An Objective may
// declare fail_on conditions (crashed, out_of_fuel); declaring nothing means
// it never fails (retry forever). This is the first code path that produces
// Failed. Evaluation is against the Active Vessel (per-craft binding deferred
// to v0.22), so these predicates read the same ctx the live evaluator builds.

// TestObjectiveNoFailOnNeverFails — the default contract: an objective with
// no fail_on never transitions to Failed, even when both crash and
// out-of-fuel conditions are present in the context. (Half of the slice's
// "done when".)
func TestObjectiveNoFailOnNeverFails(t *testing.T) {
	o := Objective{Kind: KindReachAltitude, Params: Params{PrimaryID: "earth", MinAltitudeM: 1e9}}
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 100e3), // nowhere near 1e9 floor
		Crashed:        true,
		TotalFuelKg:    0,
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("no fail_on with crash + empty tanks: got %v, want InProgress", got)
	}
}

// TestObjectiveFailOnCrashed — a fail_on: [crashed] objective Fails the
// moment the active craft is Crashed, and stays InProgress otherwise.
// (The other half of the slice's "done when".)
func TestObjectiveFailOnCrashed(t *testing.T) {
	o := Objective{
		Kind:   KindReachAltitude,
		Params: Params{PrimaryID: "earth", MinAltitudeM: 1e9},
		FailOn: []FailCondition{FailCrashed},
	}
	base := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 100e3),
	}
	notCrashed := base
	if got := o.Evaluate(notCrashed); got != InProgress {
		t.Fatalf("intact craft below floor: got %v, want InProgress", got)
	}
	crashed := base
	crashed.Crashed = true
	if got := o.Evaluate(crashed); got != Failed {
		t.Fatalf("crashed craft: got %v, want Failed", got)
	}
}

// TestObjectiveOnlyDeclaredConditionsFire — fail conditions are opt-in per
// condition: a craft that is Crashed does NOT fail an objective whose fail_on
// only lists out_of_fuel.
func TestObjectiveOnlyDeclaredConditionsFire(t *testing.T) {
	o := Objective{
		Kind:   KindReachAltitude,
		Params: Params{PrimaryID: "earth", MinAltitudeM: 1e9},
		FailOn: []FailCondition{FailOutOfFuel},
	}
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 100e3),
		Crashed:        true, // undeclared for this objective
		TotalFuelKg:    500,  // still has propellant
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("crash with only out_of_fuel declared: got %v, want InProgress", got)
	}
}

// TestObjectiveFailOnOutOfFuel — a fail_on: [out_of_fuel] objective Fails when
// the craft has no propellant left, and stays InProgress while it has fuel.
func TestObjectiveFailOnOutOfFuel(t *testing.T) {
	o := Objective{
		Kind:   KindReachAltitude,
		Params: Params{PrimaryID: "earth", MinAltitudeM: 1e9},
		FailOn: []FailCondition{FailOutOfFuel},
	}
	base := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 100e3),
	}
	withFuel := base
	withFuel.TotalFuelKg = 1200
	if got := o.Evaluate(withFuel); got != InProgress {
		t.Fatalf("with fuel below floor: got %v, want InProgress", got)
	}
	dry := base
	dry.TotalFuelKg = 0
	if got := o.Evaluate(dry); got != Failed {
		t.Fatalf("dry tanks: got %v, want Failed", got)
	}
}

// TestObjectiveOutOfFuelUsesTotalNotActiveStage — the multi-stage footgun:
// out_of_fuel must read TOTAL propellant across all stages, not the active
// (bottom) stage. A Saturn-V-style craft whose bottom stage is spent
// (FuelKg == 0) but with full upper stages (TotalFuelKg > 0) is NOT out of
// fuel — staging would continue the flight.
func TestObjectiveOutOfFuelUsesTotalNotActiveStage(t *testing.T) {
	o := Objective{
		Kind:   KindReachAltitude,
		Params: Params{PrimaryID: "earth", MinAltitudeM: 1e9},
		FailOn: []FailCondition{FailOutOfFuel},
	}
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 100e3),
		FuelKg:         0,       // bottom stage spent...
		TotalFuelKg:    549_000, // ...but upper stages full
	}
	if got := o.Evaluate(ctx); got != InProgress {
		t.Fatalf("bottom stage dry, upper stages full: got %v, want InProgress", got)
	}
}

// TestObjectivePassBeatsFailSameTick — pass takes precedence over fail on the
// same tick: achieving the goal wins even when a fail condition is also met.
// A craft that lands on the target body with empty tanks PASSES land_at_body
// (it accomplished the landing) rather than failing out_of_fuel.
func TestObjectivePassBeatsFailSameTick(t *testing.T) {
	o := Objective{
		Kind:   KindLandAtBody,
		Params: Params{PrimaryID: "earth"},
		FailOn: []FailCondition{FailOutOfFuel},
	}
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		Landed:         true,
		TotalFuelKg:    0, // out of fuel, but already landed
	}
	if got := o.Evaluate(ctx); got != Passed {
		t.Fatalf("landed with empty tanks: got %v, want Passed (pass beats fail)", got)
	}
}

// TestObjectiveFailOnStickyTerminal — once Failed, an objective stays Failed
// regardless of later context (mirrors Passed idempotency), so the per-tick
// caller can blindly re-evaluate.
func TestObjectiveFailOnStickyTerminal(t *testing.T) {
	o := Objective{
		Kind:   KindReachAltitude,
		Params: Params{PrimaryID: "earth", MinAltitudeM: 1e9},
		FailOn: []FailCondition{FailCrashed},
		Status: Failed,
	}
	// Context with no crash and even a satisfied goal must not un-fail it.
	ctx := EvalContext{
		PrimaryID:      "earth",
		PrimaryRadiusM: earthRadius,
		PrimaryMu:      earthMu,
		State:          circularState(earthRadius, earthMu, 2e9), // above the floor
	}
	if got := o.Evaluate(ctx); got != Failed {
		t.Fatalf("Failed objective should stay Failed, got %v", got)
	}
}

// TestCloneDeepCopiesFailOn — Clone must give each cloned objective its own
// FailOn backing array, so mutating a seeded mission's fail conditions can't
// bleed back into the shared embedded catalog (Clone's deep-copy contract).
func TestCloneDeepCopiesFailOn(t *testing.T) {
	src := []Mission{{
		ID: "m",
		Objectives: []Objective{{
			Kind:   KindReachAltitude,
			FailOn: []FailCondition{FailCrashed},
		}},
	}}
	dup := Clone(src)
	if len(dup[0].Objectives[0].FailOn) == 0 {
		t.Fatal("test setup: cloned objective lost its FailOn")
	}
	dup[0].Objectives[0].FailOn[0] = FailOutOfFuel
	if src[0].Objectives[0].FailOn[0] != FailCrashed {
		t.Fatal("Clone shares FailOn backing memory with source")
	}
}

// TestMissionFailsWhenActiveObjectiveFailsOn — the rollup: a fail_on condition
// on the in-progress objective fails the whole Mission (the existing terminal
// rollup), and the failure is sticky.
func TestMissionFailsWhenActiveObjectiveFailsOn(t *testing.T) {
	m := Mission{
		ID: "crash-fails",
		Objectives: []Objective{
			{Kind: KindSOIFlyby, Params: Params{PrimaryID: "moon"}}, // step 1
			{ // step 2 — fails on crash
				Kind:   KindReachAltitude,
				Params: Params{PrimaryID: "moon", MinAltitudeM: 1e9},
				FailOn: []FailCondition{FailCrashed},
			},
		},
	}
	// Tick 1 at the Moon, intact: step 1 passes, step 2 is now active but
	// below its floor — mission still in progress.
	if got := m.Evaluate(EvalContext{PrimaryID: "moon", PrimaryRadiusM: moonRadius, PrimaryMu: moonMu, State: circularState(moonRadius, moonMu, 100e3)}); got != InProgress {
		t.Fatalf("after moon flyby, intact: got %v, want InProgress", got)
	}
	if m.Objectives[0].Status != Passed {
		t.Fatalf("step 1: got %v, want Passed", m.Objectives[0].Status)
	}
	// Tick 2: crash while step 2 is active — the mission fails.
	crash := EvalContext{PrimaryID: "moon", PrimaryRadiusM: moonRadius, PrimaryMu: moonMu, State: circularState(moonRadius, moonMu, 100e3), Crashed: true}
	if got := m.Evaluate(crash); got != Failed {
		t.Fatalf("crash on active fail_on objective: got %v, want Failed", got)
	}
	// Sticky: a later clean tick keeps the mission Failed.
	if got := m.Evaluate(EvalContext{PrimaryID: "moon"}); got != Failed {
		t.Fatalf("mission should stay Failed, got %v", got)
	}
}
