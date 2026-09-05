package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// #426 item F, decision 5: the missions ladder screen offers one-key
// enables ("[1] turn on Flight School" / "[2] turn on the Challenge
// ladder") wired through the SAME toggleMissionProgram Settings uses, not a
// new path. The keys are enable-only: the offer row is shown only while a
// program is off, so a digit pressed while it is already on must not switch
// it off (the F1 row promises "turn on"). These pin that pressing 1/2 on
// screenMissions flips the persisted Settings toggle on, re-pushes the
// enabled-program set to World, and is a no-op once on.

func withProgramsOff(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := a.orbitView.Settings()
	s.SetTutorialEnabled(false)
	s.ChallengesEnabled = false
	a.orbitView.SetSettings(s)
	a.world.SetEnabledMissionPrograms(enabledProgramsFromSettings(s))
	a.active = screenMissions
	return a
}

func TestMissionsScreenDigitOneTurnsOnTutorial(t *testing.T) {
	a := withProgramsOff(t)

	pressKey(a, '1')

	if !a.orbitView.Settings().TutorialOn() {
		t.Fatalf("TutorialOn still false after pressing 1 on the missions screen")
	}
	if !a.world.MissionProgramEnabled(missions.ProgramTutorial) {
		t.Errorf("World's enabled-program set does not carry the tutorial after the persisted toggle went on")
	}
	if a.active != screenMissions {
		t.Errorf("active screen changed to %v, want to stay on screenMissions", a.active)
	}

	// Enable-only: a second press must not switch it back off.
	pressKey(a, '1')
	if !a.orbitView.Settings().TutorialOn() {
		t.Fatalf("pressing 1 while Flight School is on switched it off; the key is enable-only")
	}
}

func TestMissionsScreenDigitTwoTurnsOnChallenges(t *testing.T) {
	a := withProgramsOff(t)

	pressKey(a, '2')

	if !a.orbitView.Settings().ChallengesEnabled {
		t.Fatalf("ChallengesEnabled still false after pressing 2 on the missions screen")
	}
	if !a.world.MissionProgramEnabled(missions.ProgramChallenge) {
		t.Errorf("World's enabled-program set does not carry challenges after the persisted toggle went on")
	}

	pressKey(a, '2')
	if !a.orbitView.Settings().ChallengesEnabled {
		t.Fatalf("pressing 2 while the Challenge ladder is on switched it off; the key is enable-only")
	}
}

// TestMissionsScreenDigitsInertElsewhere — the digit intercept is scoped to
// screenMissions; from the orbit map, '1' must still do whatever it always
// did (jump to craft slot 1), not touch a mission program.
func TestMissionsScreenDigitsInertElsewhere(t *testing.T) {
	a := withProgramsOff(t)
	a.active = screenOrbit

	pressKey(a, '1')

	if a.orbitView.Settings().TutorialOn() {
		t.Errorf("pressing 1 on the orbit screen turned Flight School on; the intercept must be missions-screen-only")
	}
}
