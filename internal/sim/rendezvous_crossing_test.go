package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// rendezvousCrossingWorld: the target sits 10 degrees behind on a same-energy
// orbit whose velocity is rotated 8 degrees out of tangential (flight path
// angle), so the two orbits cross transversally at two points while their
// periods match (no natural phasing, and not tangent, which would be
// float-noise sensitive).
func rendezvousCrossingWorld(t *testing.T) *World {
	t.Helper()
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	target.State.R = rotateAboutAxis(active.State.R, axis, -10*math.Pi/180)
	target.State.V = rotateAboutAxis(active.State.V, axis, -10*math.Pi/180+8*math.Pi/180)
	target.Primary = active.Primary
	return w
}

// #416 acceptance (G4 Q3): plant a crossing row through the production
// planter and fly it through the SAME commit path Engage uses
// (RendezvousCommitWithPlan -> rendezvousCommitFromPlantedBurnNode); the
// flown closest approach must match what the row advertised. PR #412 shipped
// the crossing advertising 16 km and flying 15,000 km.
func TestPlanRendezvousBurn_Crossing_FlownCAMatchesAdvertised(t *testing.T) {
	w := rendezvousCrossingWorld(t)
	c := w.ActiveCraft()

	ladder, err := w.RecommendRendezvousLadder(planner.RendezvousCrossing)
	if err != nil {
		t.Fatalf("crossing ladder refused: %v", err)
	}
	checked := 0
	for _, row := range ladder.Rows {
		if !row.Ok {
			continue
		}
		plan, err := w.PlanRendezvousBurn(planner.RendezvousCrossing, row.Laps)
		if err != nil {
			t.Fatalf("laps=%d plant: %v", row.Laps, err)
		}
		cp, ok := w.RendezvousCommitWithPlan()
		if !ok || cp.Tau.IsZero() {
			t.Fatalf("laps=%d: no committed plan (ok=%v)", row.Laps, ok)
		}
		const tolM = 7_000.0 // 0.1% of the orbit radius, the planner's own Ok gate
		if cp.CommittedCA > tolM {
			t.Errorf("laps=%d: flown CA %.0f m through the commit path, advertised %.0f m, want < %.0f m",
				row.Laps, cp.CommittedCA, plan.AchievableCA, tolM)
		}
		if math.Abs(cp.CommittedCA-plan.AchievableCA) > tolM {
			t.Errorf("laps=%d: flown CA %.0f m disagrees with advertised %.0f m", row.Laps, cp.CommittedCA, plan.AchievableCA)
		}
		checked++
	}
	if checked == 0 {
		t.Fatalf("no Ok crossing rows to check: %+v", ladder.Rows)
	}
	if len(c.Nodes) != 1 {
		t.Errorf("replace-not-stack: %d nodes, want 1", len(c.Nodes))
	}
}
