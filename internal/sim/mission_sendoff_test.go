package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// #426 item F, decision 9: LadderSendoff drives both the ladder screen's
// top-card slot and the MISSION chip once a whole Program's rungs are all
// Passed. Pure function of Mission.Status — no persisted state.

func twoTutorialOneChallenge() []missions.Mission {
	return []missions.Mission{
		{ID: "t1", Name: "Orientation", Program: missions.ProgramTutorial, Status: missions.Passed},
		{ID: "t2", Name: "Fly It", Program: missions.ProgramTutorial, Status: missions.Passed},
		{ID: "c1", Name: "High Orbit", Program: missions.ProgramChallenge},
	}
}

func TestLadderSendoffFlightSchoolCompleteOffersChallengesWhenOff(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = twoTutorialOneChallenge()
	// Challenge ladder off: Flight School's sendoff offers it.
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	text, offer, ok := w.LadderSendoff()
	if !ok || text != "FLIGHT SCHOOL COMPLETE" {
		t.Fatalf("LadderSendoff = (%q, %v, %v), want (\"FLIGHT SCHOOL COMPLETE\", _, true)", text, offer, ok)
	}
	if !offer {
		t.Error("should offer the Challenge ladder when it's off")
	}
}

func TestLadderSendoffFlightSchoolCompleteNoOfferWhenChallengesOn(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = twoTutorialOneChallenge()
	// Challenges enabled but its own rung is still InProgress and requires
	// nothing — so it's the active mission, and Sendoff must not fire at
	// all (ActiveMission takes precedence).
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true, missions.ProgramChallenge: true})
	if _, _, ok := w.LadderSendoff(); ok {
		t.Error("LadderSendoff should not fire while a challenge rung is active")
	}
}

func TestLadderSendoffChallengeLadderCompleteNoOffer(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{
		{ID: "t1", Name: "Orientation", Program: missions.ProgramTutorial, Status: missions.Passed},
		{ID: "c1", Name: "High Orbit", Program: missions.ProgramChallenge, Status: missions.Passed},
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true, missions.ProgramChallenge: true})
	text, offer, ok := w.LadderSendoff()
	if !ok || text != "CHALLENGE LADDER COMPLETE" {
		t.Fatalf("LadderSendoff = (%q, %v, %v), want (\"CHALLENGE LADDER COMPLETE\", false, true)", text, offer, ok)
	}
	if offer {
		t.Error("challenge-complete sendoff must not offer anything")
	}
}

func TestLadderSendoffNoneWhenNothingComplete(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{
		{ID: "t1", Name: "Orientation", Program: missions.ProgramTutorial},
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	if _, _, ok := w.LadderSendoff(); ok {
		t.Error("LadderSendoff should not fire with an in-progress mission active")
	}
}

func TestLadderSendoffSurvivesProgramDisabledAfterCompletion(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = twoTutorialOneChallenge()
	// Flight School complete, then the player switches it OFF, challenges
	// also off — Sendoff must still recognise the completed Program by
	// Status, independent of the current enabled toggle.
	w.SetEnabledMissionPrograms(map[string]bool{})
	text, offer, ok := w.LadderSendoff()
	if !ok || text != "FLIGHT SCHOOL COMPLETE" || !offer {
		t.Fatalf("LadderSendoff after disabling = (%q, %v, %v), want (\"FLIGHT SCHOOL COMPLETE\", true, true)", text, offer, ok)
	}
}
