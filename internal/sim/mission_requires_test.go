package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// requires gates the evaluator, not just the ladder display (ADR 0025 §8,
// v0.21 Slice 6): a mission whose prerequisites haven't Passed must not have
// its objectives latch — even when the objective condition is already true.

func TestEvaluateMissionsGatesOnRequires(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	primary := w.ActiveCraft().Primary.ID // the craft's real primary (earth)

	// B is gated behind A. B's soi_flyby of the craft's own primary would
	// pass on tick 1; A's names a body the craft is never the primary of, so
	// A stays InProgress and B must stay locked.
	w.Missions = []missions.Mission{
		{ID: "A", Name: "A", Objectives: []missions.Objective{
			{Kind: missions.KindSOIFlyby, Params: missions.Params{PrimaryID: "no-such-body"}},
		}},
		{ID: "B", Name: "B", Requires: []string{"A"}, Objectives: []missions.Objective{
			{Kind: missions.KindSOIFlyby, Params: missions.Params{PrimaryID: primary}},
		}},
	}

	w.evaluateMissions()
	if w.Missions[1].Status == missions.Passed {
		t.Fatal("gated mission B passed before requirement A — requires must gate evaluation, not just display")
	}

	// Unblock A, then B should be free to pass on a subsequent tick.
	w.Missions[0].Objectives[0].Params.PrimaryID = primary
	w.evaluateMissions() // A passes (B still gated against the pre-tick snapshot)
	w.evaluateMissions() // A now in the passed set → B evaluated → passes
	if w.Missions[0].Status != missions.Passed {
		t.Fatalf("A status = %v, want Passed", w.Missions[0].Status)
	}
	if w.Missions[1].Status != missions.Passed {
		t.Fatalf("B status = %v, want Passed once A passed", w.Missions[1].Status)
	}
}
