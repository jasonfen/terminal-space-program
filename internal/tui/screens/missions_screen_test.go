package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// classifyLadder is the pure render-model behind one program's rung list on
// the missions/ladder screen (ADR 0025 / Slice 5; split into two lists by
// program #426): it sorts each mission into completed / active / available
// / locked / failed and computes the locked-rung "needs:" hint. The caller
// decides which mission ID (if any) owns the active card — #426 item F
// split the single interleaved ladder into FLIGHT SCHOOL / CHALLENGES
// lists, so "first unlocked InProgress in this slice" is no longer the
// right notion; these tests pass the id explicitly, mirroring what
// Render() derives once from World.ActiveMission() across both programs.

func TestClassifyLadder(t *testing.T) {
	ms := []missions.Mission{
		{ID: "a", Name: "First", Status: missions.Passed},
		{ID: "b", Name: "Second", Objectives: []missions.Objective{{Name: "o1"}}},
		{ID: "c", Name: "Third"},
		{ID: "d", Name: "Fourth", Requires: []string{"c"}},
		{ID: "e", Name: "Fifth", Status: missions.Failed},
	}
	rows := classifyLadder(ms, "b")
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5", len(rows))
	}
	if rows[0].Category != ladderCompleted {
		t.Errorf("row 0 (Passed) = %v, want completed", rows[0].Category)
	}
	if rows[1].Category != ladderActive {
		t.Errorf("row 1 (given activeID) = %v, want active", rows[1].Category)
	}
	if len(rows[1].Objectives) != 1 {
		t.Errorf("active row should carry its objective checklist, got %d", len(rows[1].Objectives))
	}
	if rows[2].Category != ladderAvailable {
		t.Errorf("row 2 (unlocked, not the active id) = %v, want available", rows[2].Category)
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
	// Several unlocked InProgress missions; only the one matching activeID
	// is marked active, the rest are available.
	ms := []missions.Mission{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
		{ID: "c", Name: "C"},
	}
	rows := classifyLadder(ms, "a")
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

func TestClassifyLadderNoActiveIDMarksNoneActive(t *testing.T) {
	// An empty activeID (e.g. the Sendoff case — nothing is active) marks
	// no row active; unlocked InProgress missions read as available.
	ms := []missions.Mission{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	}
	rows := classifyLadder(ms, "")
	for _, r := range rows {
		if r.Category == ladderActive {
			t.Errorf("row %q marked active with no activeID given", r.Name)
		}
		if r.Category != ladderAvailable {
			t.Errorf("row %q = %v, want available", r.Name, r.Category)
		}
	}
}

func TestClassifyLadderUnlockedAfterRequirementPassed(t *testing.T) {
	// Once the prerequisite passes, the gated rung becomes available, not locked.
	ms := []missions.Mission{
		{ID: "c", Name: "Third", Status: missions.Passed},
		{ID: "d", Name: "Fourth", Requires: []string{"c"}},
	}
	rows := classifyLadder(ms, "d")
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
		{ID: "a", Name: "Reach Orbit", Program: missions.ProgramTutorial, Status: missions.Passed},
		{ID: "b", Name: "Circularize", Program: missions.ProgramTutorial, Objectives: []missions.Objective{
			{Name: "reach 100 km", Status: missions.Passed},
			{Name: "circular orbit", Description: "burn at apoapsis"},
		}},
		{ID: "c", Name: "Luna Flyby", Program: missions.ProgramTutorial, Requires: []string{"b"}},
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	out := scr.Render(w, 60)
	for _, want := range []string{"ACTIVE", "Circularize", "circular orbit", "Luna Flyby", "needs:"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered screen missing %q:\n%s", want, out)
		}
	}
}

// TestMissionsRenderTwoHeadedLists — #426 item F: the ladder is two headed
// lists, FLIGHT SCHOOL and CHALLENGES, each with its own N/M complete
// count, replacing the single interleaved list + flat summary line.
func TestMissionsRenderTwoHeadedLists(t *testing.T) {
	scr := NewMissions(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{
		{ID: "t1", Name: "Orientation", Program: missions.ProgramTutorial, Status: missions.Passed},
		{ID: "t2", Name: "Plan a Burn", Program: missions.ProgramTutorial},
		{ID: "c1", Name: "High Orbit", Program: missions.ProgramChallenge},
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true, missions.ProgramChallenge: true})
	out := scr.Render(w, 70)
	if !strings.Contains(out, "FLIGHT SCHOOL  1/2 complete") {
		t.Errorf("missing FLIGHT SCHOOL header with its own count:\n%s", out)
	}
	if !strings.Contains(out, "CHALLENGES  0/1 complete") {
		t.Errorf("missing CHALLENGES header with its own count:\n%s", out)
	}
	if !strings.Contains(out, "High Orbit") {
		t.Errorf("challenge rung missing from the CHALLENGES list:\n%s", out)
	}
}

// TestMissionsRenderOffProgramShowsToggleOffer — #426 item F, decision 5: a
// program that's switched off shows a one-line dim toggle offer under its
// header instead of its mission list, naming the one-key toggle
// (toggleMissionProgram is wired in app.go, not this screen).
func TestMissionsRenderOffProgramShowsToggleOffer(t *testing.T) {
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

	// Both off: both headers show, each with its own offer row — no
	// generic "enable in Settings" placeholder anymore.
	w.SetEnabledMissionPrograms(map[string]bool{})
	out := scr.Render(w, 70)
	if !strings.Contains(out, "[1] turn on Flight School") {
		t.Errorf("missing Flight School toggle offer with both off:\n%s", out)
	}
	if !strings.Contains(out, "[2] turn on the Challenge ladder") {
		t.Errorf("missing Challenge ladder toggle offer with both off:\n%s", out)
	}
	if strings.Contains(out, "TutMission") || strings.Contains(out, "ChalMission") {
		t.Errorf("no mission list should render for an off program:\n%s", out)
	}

	// Enabling just the tutorial surfaces its list and keeps the challenge
	// section as an offer row.
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	out = scr.Render(w, 70)
	if !strings.Contains(out, "TutMission") {
		t.Errorf("tutorial-on should show the tutorial mission:\n%s", out)
	}
	if strings.Contains(out, "ChalMission") {
		t.Errorf("challenges-off should hide the challenge mission list:\n%s", out)
	}
	if !strings.Contains(out, "[2] turn on the Challenge ladder") {
		t.Errorf("challenges-off should still show its toggle offer:\n%s", out)
	}
	// The on program carries the opposite row under its list, so the key
	// always does what the row says (the fast opt-out from Flight School).
	if !strings.Contains(out, "[1] turn off Flight School") {
		t.Errorf("tutorial-on should offer the turn-off row under its list:\n%s", out)
	}
	if strings.Contains(out, "[1] turn on Flight School") {
		t.Errorf("tutorial-on must not also show the turn-on row:\n%s", out)
	}
}

// TestMissionsRenderLockedRungHasLockGlyphAndAvailableIsBright — #426 item
// F: a locked rung carries a lock glyph (dim, with its "needs:" hint); an
// available (unlocked, not-yet-active) rung reads with the ▸ marker.
func TestMissionsRenderLockedRungHasLockGlyphAndAvailableIsBright(t *testing.T) {
	scr := NewMissions(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{
		{ID: "c1", Name: "Active One", Program: missions.ProgramChallenge},
		{ID: "c2", Name: "Available Two", Program: missions.ProgramChallenge},
		{ID: "c3", Name: "Locked Three", Program: missions.ProgramChallenge, Requires: []string{"c2"}},
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramChallenge: true})
	out := scr.Render(w, 70)
	if !strings.Contains(out, lockGlyph+" Locked Three") {
		t.Errorf("locked rung missing the lock glyph:\n%s", out)
	}
	if !strings.Contains(out, "▸ Available Two") {
		t.Errorf("available rung missing the ▸ marker:\n%s", out)
	}
}

// TestMissionsRenderSendoffWhenFlightSchoolComplete — #426 item F, decision
// 9: once every Flight School rung has Passed and nothing else is active,
// the active-card slot shows the Sendoff instead of nothing, offering the
// Challenge ladder when it's off.
func TestMissionsRenderSendoffWhenFlightSchoolComplete(t *testing.T) {
	scr := NewMissions(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{
		{ID: "t1", Name: "Orientation", Program: missions.ProgramTutorial, Status: missions.Passed},
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	out := scr.Render(w, 70)
	if !strings.Contains(out, "FLIGHT SCHOOL COMPLETE") {
		t.Errorf("missing Sendoff text:\n%s", out)
	}
	if !strings.Contains(out, "[2] turn on the Challenge ladder") {
		t.Errorf("Sendoff missing the Challenge-ladder offer (challenges are off):\n%s", out)
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
