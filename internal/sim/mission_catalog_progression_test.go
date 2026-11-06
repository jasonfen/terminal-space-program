package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestEmbeddedTutorialProgression is the automated proxy for playtesting the
// Flight School catalog (#426, five rungs: tut-orient, tut-plan, tut-fly,
// tut-launch, tut-dock): it drives the seeded tutorial through its intended
// semantic actions + world state via the real evaluator and asserts each
// rung advances in order, ending with the ladder complete. Along the way it
// pins the two refusals the UX review's own evidence flagged: a target-cycle
// press that lands on Mercury does NOT credit "aim for the Moon", and an
// over-budget or non-Moon transfer does NOT credit "plant a transfer".
func TestEmbeddedTutorialProgression(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Realistic player config for this check: tutorial on, challenges off
	// (both default off; the player opted into the tutorial). This also keeps
	// the independent challenge ladder from interfering with the assertions.
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})

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
	moonIdx := findBodyIdx(t, w, "moon")
	mercuryIdx := findBodyIdx(t, w, "mercury")

	// The opening active rung is the first tutorial step.
	if am := w.ActiveMission(); am == nil || am.ID != "tut-orient" {
		t.Fatalf("opening active mission = %v, want tut-orient", am)
	}

	// tut-orient objective 1: change view.
	step(missions.ActionCycleView)

	// REFUSAL 1 (issue #426 evidence): the first target-cycle press lands on
	// Mercury — must NOT credit "aim for the Moon".
	w.Target = spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: mercuryIdx}
	step(missions.ActionCycleTarget)
	if got := statusByID("tut-orient"); got == missions.Passed {
		t.Fatalf("tut-orient passed after targeting Mercury, want still InProgress")
	}

	// A later press lands on the Moon: now it credits.
	w.Target = spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: moonIdx}
	step(missions.ActionCycleTarget)
	if got := statusByID("tut-orient"); got != missions.Passed {
		t.Fatalf("tut-orient = %v after view+Moon target, want Passed", got)
	}

	// tut-plan unlocks: open the planner.
	step(missions.ActionOpenManeuver)
	c := w.ActiveCraft()

	// REFUSAL 2a (issue #426 evidence): an over-budget Moon transfer plants
	// (ADR 0047 warn-and-allow) but must NOT credit "plant a transfer".
	c.Nodes = []spacecraft.ManeuverNode{{DV: c.RemainingDeltaV() + 1521}}
	step(missions.ActionPlanTransfer)
	if got := statusByID("tut-plan"); got == missions.Passed {
		t.Fatalf("tut-plan passed on an over-budget Moon transfer, want still InProgress")
	}

	// REFUSAL 2b: an affordable transfer at the WRONG body (Mercury) must
	// also not credit.
	c.Nodes = []spacecraft.ManeuverNode{{DV: 10}}
	w.Target = spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: mercuryIdx}
	step(missions.ActionPlanTransfer)
	if got := statusByID("tut-plan"); got == missions.Passed {
		t.Fatalf("tut-plan passed on an affordable Mercury transfer, want still InProgress")
	}

	// An affordable Moon transfer credits.
	c.Nodes = []spacecraft.ManeuverNode{{DV: 10}}
	w.Target = spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: moonIdx}
	step(missions.ActionPlanTransfer)
	if got := statusByID("tut-plan"); got != missions.Passed {
		t.Fatalf("tut-plan = %v after an affordable Moon transfer, want Passed", got)
	}
	c.Nodes = nil

	// tut-fly unlocks: a state objective — climb above 700 km. Pin the craft
	// above the floor and evaluate (no physics tick needed).
	setCircularAltitude(c, 800_000)
	w.evaluateMissions()
	if got := statusByID("tut-fly"); got != missions.Passed {
		t.Fatalf("tut-fly = %v after climbing above 700 km, want Passed", got)
	}

	// tut-launch unlocks: spawn on the pad, throttle up, ignite (liftoff),
	// pitch east above 10 km, stage, plan the circularising burn, then make orbit.
	c.OnPad = true
	step(missions.ActionSpawnCraft)
	step(missions.ActionThrottleFull)
	step(missions.ActionIgnite)
	c.OnPad = false // liftoff clears OnPad in real play (maneuver.go)

	setCircularAltitude(c, 15_000) // above the 10 km floor
	w.evaluateMissions()
	step(missions.ActionStage) // first stage runs dry
	if got := statusByID("tut-launch"); got == missions.Passed {
		t.Fatalf("tut-launch passed before the circularising burn, want still InProgress")
	}

	step(missions.ActionPlanCircularize)

	setCircularAltitude(c, 250_000) // periapsis above the 200 km floor, e≈0
	w.evaluateMissions()
	if got := statusByID("tut-launch"); got != missions.Passed {
		t.Fatalf("tut-launch = %v after making orbit, want Passed", got)
	}

	// tut-dock unlocks: spawn a partner IN ORBIT (not on the pad), target it,
	// plant the rendezvous burn, and dock.
	twin := *c
	twin.ID = c.ID + 1000
	w.Crafts = append(w.Crafts, &twin)
	step(missions.ActionSpawnCraft) // c.OnPad is already false (in orbit)
	if got := statusByID("tut-dock"); got == missions.Passed {
		t.Fatalf("tut-dock passed after the spawn step alone, want still InProgress")
	}

	w.Target = spacecraft.Target{Kind: spacecraft.TargetCraft, CraftID: twin.ID}
	step(missions.ActionCycleTarget)
	step(missions.ActionPlanRendezvous)

	c.DockedComponents = append(c.DockedComponents, spacecraft.DockedComponent{})
	w.evaluateMissions()
	if got := statusByID("tut-dock"); got != missions.Passed {
		t.Fatalf("tut-dock = %v after docking, want Passed", got)
	}

	// Flight School complete and challenges disabled → no active mission
	// remains (the challenge ladder is a separate, opted-out program here).
	if am := w.ActiveMission(); am != nil {
		t.Fatalf("active mission after Flight School = %v, want nil (challenges disabled)", am)
	}
}

// setCircularAltitude pins c's state to a circular orbit at the given
// altitude (m) above its primary — enough for the reach_altitude and
// circularize_from_pad kinds to evaluate without a real physics tick.
func setCircularAltitude(c *spacecraft.Spacecraft, altitudeM float64) {
	r := c.Primary.RadiusMeters() + altitudeM
	mu := c.Primary.GravitationalParameter()
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: math.Sqrt(mu / r)},
		M: c.TotalMass(),
	}
}

// partnerRelays turns the slate's relay-antenna craft into a partner's ghosts,
// so the world holds relays that are not its own.
func partnerRelays(w *World) {
	var keep []*spacecraft.Spacecraft
	for _, c := range w.Crafts {
		if c.AntennaKind != spacecraft.AntennaRelay {
			keep = append(keep, c)
			continue
		}
		w.Ghosts = append(w.Ghosts, Ghost{Owner: "fp-partner", CraftID: c.ID, PrimaryID: "moon",
			RelPos: c.State.R, Vel: c.State.V, Pos: w.BodyPosition(c.Primary).Add(c.State.R),
			AntennaKind: c.AntennaKind, AntennaRangeM: c.AntennaRangeM, Controllable: c.Controllable})
	}
	w.Crafts = keep
}

func catalogObjective(t *testing.T, mission string, kind missions.Kind) missions.Objective {
	t.Helper()
	cat, err := missions.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range cat.Missions {
		if m.ID == mission {
			for _, o := range m.Objectives {
				if o.Kind == kind {
					return o
				}
			}
		}
	}
	t.Fatalf("%s has no %s objective", mission, kind)
	return missions.Objective{}
}

// ADR 0066 guard 15 (decision 15): Far Side Relay's coverage step passes on the
// shared network, with two partner relays half a lap apart and none of the
// player's own; Relay Constellation counts only the player's own relays, so
// three partner relays do not pass it, and one partner relay short of the
// two does not pass the coverage step.
func TestPartnerRelaysCountForCoverageNotForConstellation(t *testing.T) {
	wall := time.Unix(1000, 0)
	siteCovWallNow = func() time.Time { wall = wall.Add(time.Hour); return wall }
	defer func() { siteCovWallNow = time.Now }()
	cover := catalogObjective(t, "chal-far-side-relay", missions.KindLunarCoverage)
	constellation := catalogObjective(t, "chal-relay-constellation", missions.KindRelayCoverage)

	w := mustWorld(t)
	ellipseRelay(w, 0, 180)
	ellipseRelay(w, 0, 0)
	partnerRelays(w)
	if got := cover.Evaluate(w.missionEvalContext()); got != missions.Passed {
		t.Errorf("two partner relays, none own: coverage step %v, want Passed", got)
	}
	w = mustWorld(t)
	ellipseRelay(w, 0, 180)
	partnerRelays(w)
	if got := cover.Evaluate(w.missionEvalContext()); got != missions.InProgress {
		t.Errorf("one partner relay: coverage step %v, want InProgress", got)
	}

	w = mustWorld(t)
	for _, m := range []float64{0, 120, 240} {
		ellipseRelay(w, 0, m)
	}
	partnerRelays(w)
	w.RecomputeCommGraph()
	if got := constellation.Evaluate(w.missionEvalContext()); got == missions.Passed {
		t.Error("three partner relays passed Relay Constellation")
	}
	// The equal case: the same three relays, owned, do pass.
	w = mustWorld(t)
	for _, m := range []float64{0, 120, 240} {
		ellipseRelay(w, 0, m)
	}
	w.RecomputeCommGraph()
	if n := w.ConnectedRelayCount(); n < 3 {
		t.Fatalf("setup: only %d of 3 own relays connected", n)
	}
	if got := constellation.Evaluate(w.missionEvalContext()); got != missions.Passed {
		t.Errorf("three own connected relays: %v, want Passed", got)
	}
}
