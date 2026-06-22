package sim

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// The checklist chip flashes a just-failed mission for a few wall-clock
// seconds before advancing (ADR 0025 §5 / Slice 5). These pin the window
// helper and the InProgress→Failed transition detection in evaluateMissions.

func TestMissionFailFlashActiveWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	if _, ok := missionFailFlashActive("", now.Add(time.Second), now); ok {
		t.Error("empty message should never be active")
	}
	if msg, ok := missionFailFlashActive("Apollo failed: crashed", now.Add(time.Second), now); !ok || msg != "Apollo failed: crashed" {
		t.Errorf("within window: got %q ok=%v, want the message + true", msg, ok)
	}
	if _, ok := missionFailFlashActive("Apollo failed", now.Add(-time.Second), now); ok {
		t.Error("past the deadline should be inactive")
	}
}

// failOnCrashMission is a one-objective mission that can only end by failing
// on crash: the SOIFlyby names a body the craft is never the primary of, so
// the kind predicate stays InProgress and the opt-in fail_on drives it Failed.
func failOnCrashMission() missions.Mission {
	return missions.Mission{
		ID:   "t",
		Name: "Test Mission",
		Objectives: []missions.Objective{{
			Kind:   missions.KindSOIFlyby,
			Params: missions.Params{PrimaryID: "no-such-body"},
			FailOn: []missions.FailCondition{missions.FailCrashed},
		}},
	}
}

func TestEvaluateMissionsFlagsFailure(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.Crashed = true
	w.Missions = []missions.Mission{failOnCrashMission()}

	w.evaluateMissions()

	if w.Missions[0].Status != missions.Failed {
		t.Fatalf("mission status = %v, want Failed", w.Missions[0].Status)
	}
	msg, ok := w.MissionFailFlash()
	if !ok {
		t.Fatal("expected an active failure flash after the crash")
	}
	if !strings.Contains(msg, "Test Mission") || !strings.Contains(msg, "crashed") {
		t.Errorf("flash = %q, want it to name the mission and the reason", msg)
	}
}

func TestEvaluateMissionsNoFlashWithoutFailure(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Crashed, but the mission opts into no fail condition — it must not flash.
	w.ActiveCraft().Crashed = true
	w.Missions = []missions.Mission{{
		ID:   "safe",
		Name: "Safe",
		Objectives: []missions.Objective{{
			Kind:   missions.KindSOIFlyby,
			Params: missions.Params{PrimaryID: "no-such-body"},
		}},
	}}

	w.evaluateMissions()

	if _, ok := w.MissionFailFlash(); ok {
		t.Error("a no-fail_on mission should not raise a failure flash")
	}
}
