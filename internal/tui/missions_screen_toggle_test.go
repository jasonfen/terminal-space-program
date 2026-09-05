package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// #426 item F, decision 5, plus the opt-out Jason asked for on
// 2026-09-04: the missions ladder screen carries a one-key row per program
// ("[1] turn on Flight School" while off, "[1] turn off Flight School"
// while on; same for "[2]" and the Challenge ladder) wired through the SAME
// toggleMissionProgram Settings uses, not a new path. These pin that the
// digit flips the persisted Settings toggle in BOTH directions and re-pushes
// the enabled-program set to World, so M, 1, esc is a real opt-out.

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

func TestMissionsScreenDigitOneTogglesTutorialBothWays(t *testing.T) {
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

	// The opt-out: a second press switches it off, and World follows.
	pressKey(a, '1')
	if a.orbitView.Settings().TutorialOn() {
		t.Fatalf("pressing 1 while Flight School is on did not switch it off; M, 1, esc must be a real opt-out")
	}
	if a.world.MissionProgramEnabled(missions.ProgramTutorial) {
		t.Errorf("World still runs the tutorial after the persisted toggle went off")
	}
	if a.orbitView.Settings().TutorialEnabled == nil {
		t.Errorf("opt-out must be recorded as an explicit false, not reset to absent (absent means on)")
	}
}

func TestMissionsScreenDigitTwoTogglesChallengesBothWays(t *testing.T) {
	a := withProgramsOff(t)

	pressKey(a, '2')
	if !a.orbitView.Settings().ChallengesEnabled {
		t.Fatalf("ChallengesEnabled still false after pressing 2 on the missions screen")
	}
	if !a.world.MissionProgramEnabled(missions.ProgramChallenge) {
		t.Errorf("World's enabled-program set does not carry challenges after the persisted toggle went on")
	}

	pressKey(a, '2')
	if a.orbitView.Settings().ChallengesEnabled {
		t.Fatalf("pressing 2 while the Challenge ladder is on did not switch it off")
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
