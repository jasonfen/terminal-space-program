package tui

import "testing"

// #426 item F, decision 5: the missions ladder screen offers one-key
// toggles ("[1] turn on Flight School" / "[2] turn on the Challenge
// ladder") wired through the SAME toggleMissionProgram Settings uses, not a
// new path. These pin that pressing 1/2 on screenMissions actually flips
// the Settings toggle and re-pushes the enabled-program set to World.

func TestMissionsScreenDigitOneTogglesTutorial(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.active = screenMissions
	before := a.orbitView.Settings().TutorialEnabled

	pressKey(a, '1')

	if got := a.orbitView.Settings().TutorialEnabled; got == before {
		t.Fatalf("TutorialEnabled unchanged after pressing 1 on the missions screen (was %v)", before)
	}
	if got := a.world.MissionProgramEnabled("tutorial"); got != a.orbitView.Settings().TutorialEnabled {
		t.Errorf("World's enabled-program set (tutorial=%v) disagrees with the persisted Settings toggle (%v)", got, a.orbitView.Settings().TutorialEnabled)
	}
	if a.active != screenMissions {
		t.Errorf("active screen changed to %v, want to stay on screenMissions", a.active)
	}
}

func TestMissionsScreenDigitTwoTogglesChallenges(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.active = screenMissions
	before := a.orbitView.Settings().ChallengesEnabled

	pressKey(a, '2')

	if got := a.orbitView.Settings().ChallengesEnabled; got == before {
		t.Fatalf("ChallengesEnabled unchanged after pressing 2 on the missions screen (was %v)", before)
	}
	if got := a.world.MissionProgramEnabled("challenge"); got != a.orbitView.Settings().ChallengesEnabled {
		t.Errorf("World's enabled-program set (challenge=%v) disagrees with the persisted Settings toggle (%v)", got, a.orbitView.Settings().ChallengesEnabled)
	}
}

// TestMissionsScreenDigitsInertElsewhere — the digit intercept is scoped to
// screenMissions; from the orbit map, '1' must still do whatever it always
// did (jump to craft slot 1), not toggle a mission program.
func TestMissionsScreenDigitsInertElsewhere(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.active = screenOrbit
	before := a.orbitView.Settings().TutorialEnabled

	pressKey(a, '1')

	if got := a.orbitView.Settings().TutorialEnabled; got != before {
		t.Errorf("pressing 1 on the orbit screen toggled TutorialEnabled (%v -> %v); the intercept must be missions-screen-only", before, got)
	}
}
