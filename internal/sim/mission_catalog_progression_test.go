package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestEmbeddedTutorialProgression is the automated proxy for playtesting the
// v0.21 Slice 6 catalog: it drives the seeded tutorial through its intended
// semantic actions + the requires gating and asserts each rung advances in
// order, ending with the first challenge surfaced as active. This pins the
// Slice-6 done-criterion ("a new player can complete the tutorial") so a
// future catalog edit that strands a rung fails loudly.
func TestEmbeddedTutorialProgression(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	statusByID := func(id string) missions.Status {
		for i := range w.Missions {
			if w.Missions[i].ID == id {
				return w.Missions[i].Status
			}
		}
		t.Fatalf("mission %q not in seeded catalog", id)
		return missions.InProgress
	}
	// step records a semantic action and runs one mission-eval tick — the
	// downward tui→sim→missions path the input handler drives in flight.
	step := func(a missions.Action) {
		w.RecordAction(a)
		w.evaluateMissions()
	}

	// The opening active rung is the first tutorial step.
	if am := w.ActiveMission(); am == nil || am.ID != "tut-orient" {
		t.Fatalf("opening active mission = %v, want tut-orient", am)
	}

	// tut-orient: change view, then target.
	step(missions.ActionCycleView)
	step(missions.ActionCycleTarget)
	if got := statusByID("tut-orient"); got != missions.Passed {
		t.Fatalf("tut-orient = %v after view+target, want Passed", got)
	}

	// tut-plan unlocks: open the planner, then plant a transfer.
	step(missions.ActionOpenManeuver)
	step(missions.ActionPlanTransfer)
	if got := statusByID("tut-plan"); got != missions.Passed {
		t.Fatalf("tut-plan = %v after open+transfer, want Passed", got)
	}

	// tut-fly unlocks: a state objective — climb above 700 km. Pin the craft
	// above the floor and evaluate (no physics tick needed).
	c := w.ActiveCraft()
	r := c.Primary.RadiusMeters() + 800_000 // comfortably above the 700 km floor
	mu := c.Primary.GravitationalParameter()
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: math.Sqrt(mu / r)},
		M: c.TotalMass(),
	}
	w.evaluateMissions()
	if got := statusByID("tut-fly"); got != missions.Passed {
		t.Fatalf("tut-fly = %v after climbing above 700 km, want Passed", got)
	}

	// With the tutorial done, the first challenge is the active rung — and it
	// stays InProgress (the craft is at 800 km, not a 1000 km circular), so
	// the gating didn't let a later rung latch out of order.
	if am := w.ActiveMission(); am == nil || am.ID != "chal-high-orbit" {
		t.Fatalf("post-tutorial active mission = %v, want chal-high-orbit", am)
	}
	if got := statusByID("chal-high-orbit"); got != missions.InProgress {
		t.Fatalf("chal-high-orbit = %v, want InProgress (not yet flown)", got)
	}
}
