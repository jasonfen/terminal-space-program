package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// v0.21 Slice 3 (ADR 0025 §5) — the sim→missions seam for failure. These
// prove the fail-condition inputs are wired from real World state (the
// Active Vessel) and that a fail_on objective fails through the Tick path.

// TestMissionEvalContextWiresTotalFuel — out_of_fuel reads TotalFuelKg, the
// summed propellant across every stage (c.Fuel), NOT the active-stage fuel.
func TestMissionEvalContextWiresTotalFuel(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	ctx := w.missionEvalContext()
	if ctx.TotalFuelKg != c.Fuel {
		t.Errorf("TotalFuelKg: got %v want %v (summed all-stage fuel)", ctx.TotalFuelKg, c.Fuel)
	}
}

// TestTickFailsOnCrash — the integration path: a fail_on: [crashed] mission
// transitions to Failed through Tick→evaluateMissions once the active craft
// is Crashed.
func TestTickFailsOnCrash(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.Missions = []missions.Mission{{
		ID: "dont-crash",
		Objectives: []missions.Objective{{
			Kind:   missions.KindReachAltitude,
			Params: missions.Params{PrimaryID: c.Primary.ID, MinAltitudeM: 1e9},
			FailOn: []missions.FailCondition{missions.FailCrashed},
		}},
	}}
	w.Tick()
	if w.Missions[0].Status != missions.InProgress {
		t.Fatalf("intact craft below floor: got %v, want InProgress", w.Missions[0].Status)
	}
	c.Crashed = true
	w.Tick()
	if w.Missions[0].Status != missions.Failed {
		t.Fatalf("after crash: got %v, want Failed", w.Missions[0].Status)
	}
}

// TestTickNoFailOnSurvivesCrash — a mission with no fail_on never fails, even
// when the active craft is Crashed (the no-fail default flown end to end).
func TestTickNoFailOnSurvivesCrash(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.Missions = []missions.Mission{{
		ID: "no-fail",
		Objectives: []missions.Objective{{
			Kind:   missions.KindReachAltitude,
			Params: missions.Params{PrimaryID: c.Primary.ID, MinAltitudeM: 1e9},
		}},
	}}
	c.Crashed = true
	w.Tick()
	if w.Missions[0].Status != missions.InProgress {
		t.Fatalf("no fail_on after crash: got %v, want InProgress", w.Missions[0].Status)
	}
}
