package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #594 (GS Q5): a finished rung leaves a standing "last finished" fact and a
// short flash at the moment it completes. Both are derived from / driven by
// Mission.Status transitions; nothing new is persisted.

func tutorialWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	return w
}

// finishOrientation drives Orientation to Passed through RecordAction + the
// real World.Tick (which runs the mission evaluator).
func finishOrientation(t *testing.T, w *World) {
	t.Helper()
	w.RecordAction(missions.ActionCycleView)
	w.Tick()
	w.Target = spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: findBodyIdx(t, w, "moon")}
	w.RecordAction(missions.ActionCycleTarget)
	w.Tick()
}

func TestRungCompletionAnnouncesOnce(t *testing.T) {
	w := tutorialWorld(t)
	if w.RungDoneFlash() {
		t.Fatal("flash armed before any rung finished")
	}
	if _, ok := w.LastFinishedRung(); ok {
		t.Fatal("a last-finished rung before any rung finished")
	}
	finishOrientation(t, w)
	if am := w.ActiveMission(); am == nil || am.ID != "tut-plan" {
		t.Fatalf("active = %v, want tut-plan after Orientation", am)
	}
	if !w.RungDoneFlash() {
		t.Error("no flash at the moment Orientation completed")
	}
	if name, ok := w.LastFinishedRung(); !ok || name != "Flight School: Orientation" {
		t.Errorf("LastFinishedRung = %q, %v; want Orientation", name, ok)
	}
	// Flash expires on wall-clock (past the window), standing fact stays.
	w.rungDoneUntil = time.Now().Add(-time.Second)
	if w.RungDoneFlash() {
		t.Error("flash still on past its window")
	}
	// Further ticks must not re-arm it (latch trap, #516).
	for i := 0; i < 50; i++ {
		w.Tick()
	}
	if w.RungDoneFlash() {
		t.Error("flash re-armed by later ticks with no new completion")
	}
	if name, ok := w.LastFinishedRung(); !ok || name != "Flight School: Orientation" {
		t.Errorf("standing rung lost after ticks: %q, %v", name, ok)
	}
}

func TestProgramSendoffStillFiresAfterLastRung(t *testing.T) {
	w := tutorialWorld(t)
	for i := range w.Missions {
		if w.Missions[i].Program == missions.ProgramTutorial {
			w.Missions[i].Status = missions.Passed
		}
	}
	text, _, ok := w.LadderSendoff()
	if !ok || text != "FLIGHT SCHOOL COMPLETE" {
		t.Fatalf("LadderSendoff = %q, %v", text, ok)
	}
}

// Two synthetic event rungs: the second finishing replaces the standing row
// and flashes again, and a mission that fails never counts as finished.
func TestRungCompletionReplacesStandingRow(t *testing.T) {
	w := tutorialWorld(t)
	ev := func(a missions.Action) []missions.Objective {
		return []missions.Objective{{Name: "do it", Kind: missions.KindEvent, Params: missions.Params{Action: a}}}
	}
	w.Missions = []missions.Mission{
		{ID: "a", Name: "Rung A", Program: missions.ProgramTutorial, Objectives: ev(missions.ActionCycleView)},
		{ID: "b", Name: "Rung B", Program: missions.ProgramTutorial, Requires: []string{"a"}, Objectives: ev(missions.ActionOpenManeuver)},
		{ID: "c", Name: "Rung C", Program: missions.ProgramTutorial, Requires: []string{"b"}, Objectives: ev(missions.ActionPlanTransfer)},
	}
	w.RecordAction(missions.ActionCycleView)
	w.Tick()
	if n, _ := w.LastFinishedRung(); n != "Rung A" || !w.RungDoneFlash() {
		t.Fatalf("after A: last=%q flash=%v", n, w.RungDoneFlash())
	}
	w.rungDoneUntil = time.Now().Add(-time.Second)
	w.RecordAction(missions.ActionOpenManeuver)
	w.Tick()
	if n, _ := w.LastFinishedRung(); n != "Rung B" {
		t.Errorf("standing row = %q, want Rung B", n)
	}
	if !w.RungDoneFlash() {
		t.Error("no flash when the second rung completed")
	}
}
