package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// classifyLadder is the pure render-model behind the missions/ladder screen
// (ADR 0025 / Slice 5): it sorts each mission into completed / active /
// available / locked / failed and computes the locked-rung "needs:" hint.
// These pin that classification independent of the layout/box rendering.

func TestClassifyLadder(t *testing.T) {
	ms := []missions.Mission{
		{ID: "a", Name: "First", Status: missions.Passed},
		{ID: "b", Name: "Second", Objectives: []missions.Objective{{Name: "o1"}}},
		{ID: "c", Name: "Third"},
		{ID: "d", Name: "Fourth", Requires: []string{"c"}},
		{ID: "e", Name: "Fifth", Status: missions.Failed},
	}
	rows := classifyLadder(ms)
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	if rows[0].Category != ladderCompleted {
		t.Errorf("row 0 (Passed) = %v, want completed", rows[0].Category)
	}
	if rows[1].Category != ladderActive {
		t.Errorf("row 1 (first unlocked InProgress) = %v, want active", rows[1].Category)
	}
	if len(rows[1].Objectives) != 1 {
		t.Errorf("active row should carry its objective checklist, got %d", len(rows[1].Objectives))
	}
	if rows[2].Category != ladderAvailable {
		t.Errorf("row 2 (second unlocked InProgress) = %v, want available", rows[2].Category)
	}
	if rows[3].Category != ladderLocked {
		t.Errorf("row 3 (unmet requires) = %v, want locked", rows[3].Category)
	}
	if !strings.Contains(rows[3].Hint, "Third") {
		t.Errorf("locked hint = %q, want it to name the unmet requirement 'Third'", rows[3].Hint)
	}
	if rows[4].Category != ladderFailed {
		t.Errorf("row 4 (Failed) = %v, want failed", rows[4].Category)
	}
}

func TestClassifyLadderExactlyOneActive(t *testing.T) {
	// Several unlocked InProgress missions, but only the first is the active
	// one (it gets the card); the rest are available.
	ms := []missions.Mission{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
		{ID: "c", Name: "C"},
	}
	rows := classifyLadder(ms)
	active := 0
	for _, r := range rows {
		if r.Category == ladderActive {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active rows = %d, want exactly 1", active)
	}
}

func TestClassifyLadderUnlockedAfterRequirementPassed(t *testing.T) {
	// Once the prerequisite passes, the gated rung becomes available, not locked.
	ms := []missions.Mission{
		{ID: "c", Name: "Third", Status: missions.Passed},
		{ID: "d", Name: "Fourth", Requires: []string{"c"}},
	}
	rows := classifyLadder(ms)
	if rows[1].Category == ladderLocked {
		t.Errorf("row with a now-passed requirement should not be locked")
	}
}

// TestMissionsRenderSmoke checks the screen produces the active-card header,
// the live objective status, and a locked rung's hint without panicking.
func TestMissionsRenderSmoke(t *testing.T) {
	scr := NewMissions(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{
		{ID: "a", Name: "Reach Orbit", Status: missions.Passed},
		{ID: "b", Name: "Circularize", Objectives: []missions.Objective{
			{Name: "reach 100 km", Status: missions.Passed},
			{Name: "circular orbit", Description: "burn at apoapsis"},
		}},
		{ID: "c", Name: "Luna Flyby", Requires: []string{"b"}},
	}
	out := scr.Render(w, 60)
	for _, want := range []string{"ACTIVE", "Circularize", "circular orbit", "Luna Flyby", "needs:"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered screen missing %q:\n%s", want, out)
		}
	}
}

func TestMissionsRenderProgramsOff(t *testing.T) {
	scr := NewMissions(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{
		{ID: "t", Name: "TutMission", Program: missions.ProgramTutorial,
			Objectives: []missions.Objective{{Kind: missions.KindEvent, Name: "view", Params: missions.Params{Action: missions.ActionCycleView}}}},
		{ID: "c", Name: "ChalMission", Program: missions.ProgramChallenge,
			Objectives: []missions.Objective{{Kind: missions.KindSOIFlyby, Params: missions.Params{PrimaryID: "moon"}}}},
	}

	// Both programs off → the screen shows the enable-in-Settings hint.
	w.SetEnabledMissionPrograms(map[string]bool{})
	out := scr.Render(w, 70)
	if !strings.Contains(out, "enable") {
		t.Errorf("all-programs-off should show the enable-in-Settings hint:\n%s", out)
	}

	// Enabling just the tutorial surfaces it and keeps challenges hidden.
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	out = scr.Render(w, 70)
	if !strings.Contains(out, "TutMission") {
		t.Errorf("tutorial-on should show the tutorial mission:\n%s", out)
	}
	if strings.Contains(out, "ChalMission") {
		t.Errorf("challenges-off should hide challenge missions:\n%s", out)
	}
}

func TestMissionsRenderEmpty(t *testing.T) {
	scr := NewMissions(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = nil
	out := scr.Render(w, 60)
	if !strings.Contains(out, "no missions") {
		t.Errorf("empty catalog should show a placeholder, got:\n%s", out)
	}
}
