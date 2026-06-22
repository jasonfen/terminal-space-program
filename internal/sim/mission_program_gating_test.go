package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// Per-program gating (ADR 0025 §2 / v0.21 Slice 7): a mission in a disabled
// program is inert — not evaluated, not surfaced as active. The tui drives
// this from the persisted Tutorial/Challenges toggles (both default off).

func twoProgramMissions(primary string) []missions.Mission {
	// Both objectives would pass immediately (soi_flyby of the craft's own
	// primary), so any non-pass is the gate doing its job.
	return []missions.Mission{
		{ID: "t1", Program: missions.ProgramTutorial, Objectives: []missions.Objective{
			{Kind: missions.KindSOIFlyby, Params: missions.Params{PrimaryID: primary}},
		}},
		{ID: "c1", Program: missions.ProgramChallenge, Objectives: []missions.Objective{
			{Kind: missions.KindSOIFlyby, Params: missions.Params{PrimaryID: primary}},
		}},
	}
}

func TestMissionProgramGatingSkipsDisabled(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	primary := w.ActiveCraft().Primary.ID
	w.Missions = twoProgramMissions(primary)
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true}) // challenge off

	w.evaluateMissions()

	if w.Missions[0].Status != missions.Passed {
		t.Errorf("enabled tutorial mission = %v, want Passed", w.Missions[0].Status)
	}
	if w.Missions[1].Status == missions.Passed {
		t.Error("disabled challenge mission must not evaluate (it passed)")
	}
	// ActiveMission ignores disabled programs too: t1 passed, c1 disabled → nil.
	if am := w.ActiveMission(); am != nil {
		t.Errorf("ActiveMission = %v, want nil (only mission left is disabled)", am)
	}
}

func TestMissionProgramGatingBothOff(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = twoProgramMissions(w.ActiveCraft().Primary.ID)
	w.SetEnabledMissionPrograms(map[string]bool{}) // empty non-nil = everything off

	w.evaluateMissions()

	for i := range w.Missions {
		if w.Missions[i].Status != missions.InProgress {
			t.Errorf("mission %q = %v with both programs off, want InProgress", w.Missions[i].ID, w.Missions[i].Status)
		}
	}
	if am := w.ActiveMission(); am != nil {
		t.Errorf("ActiveMission = %v, want nil with all programs off", am)
	}
}

func TestMissionProgramNilEnablesAll(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = twoProgramMissions(w.ActiveCraft().Primary.ID)
	// No SetEnabledMissionPrograms call → nil set → all programs active
	// (back-compat default; this is why every existing mission test still runs).
	w.evaluateMissions()
	if w.Missions[0].Status != missions.Passed || w.Missions[1].Status != missions.Passed {
		t.Errorf("nil enabled-set should evaluate all programs, got %v / %v",
			w.Missions[0].Status, w.Missions[1].Status)
	}
}

func TestMissionUntaggedAlwaysActive(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// An untagged (custom/user) mission is never gated, even with all known
	// programs off.
	w.Missions = []missions.Mission{{
		ID: "custom",
		Objectives: []missions.Objective{
			{Kind: missions.KindSOIFlyby, Params: missions.Params{PrimaryID: w.ActiveCraft().Primary.ID}},
		},
	}}
	w.SetEnabledMissionPrograms(map[string]bool{})
	w.evaluateMissions()
	if w.Missions[0].Status != missions.Passed {
		t.Errorf("untagged mission = %v, want Passed (never gated)", w.Missions[0].Status)
	}
}
