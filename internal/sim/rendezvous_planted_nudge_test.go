package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestRendezvousCommitHonorsPlantedNudge (PR #392 review, finding 1):
// the #276 fix's own prescribed remedy — refuse, then "plant a nudge
// [K] first" — was a dead end. Planting a node doesn't touch
// active.State (only firing does), so a plant-then-Engage sequence hit
// the identical current-course refusal every time. RendezvousCommit
// must honor an ACTUALLY-PLANTED rendezvous-nudge node: once K has
// queued a burn, Engage commits to the encounter that burn leads to,
// not to the (still zero-drift) current course.
func TestRendezvousCommitHonorsPlantedNudge(t *testing.T) {
	w := rendezvousSmallLagWorld(t)

	// Precondition: this geometry has zero relative drift on the
	// current courses — the phantom-commit regression test
	// (rendezvous_matched_orbit_test.go) already pins that
	// RendezvousCommit refuses with nothing planted.
	if _, _, ok := w.RendezvousCommit(); ok {
		t.Fatal("precondition: RendezvousCommit should refuse with no planted node on matched orbits")
	}

	adv, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("small-lag geometry yielded no useful nudge (%v); not exercising the planted-node commit path", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if adv == nil || !adv.Ok {
		t.Fatalf("expected a successful plant, got %+v", adv)
	}
	c := w.ActiveCraft()
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 planted node, got %d", len(c.Nodes))
	}
	node := c.Nodes[0]

	tau, ca, ok := w.RendezvousCommit()
	if !ok {
		t.Fatal("RendezvousCommit refused despite a freshly planted rendezvous nudge — " +
			"the #276 refusal's own \"plant a nudge [K] first\" remedy is a dead end without this")
	}
	if !tau.After(w.Clock.SimTime) {
		t.Errorf("committed τ %v is not in the future (SimTime %v)", tau, w.Clock.SimTime)
	}
	if tau.Before(node.TriggerTime) {
		t.Errorf("committed τ %v precedes the node's own TriggerTime %v — "+
			"the encounter can only exist once the burn has actually fired", tau, node.TriggerTime)
	}
	if ca <= 0 {
		t.Errorf("committed CA = %.3f, want > 0", ca)
	}
	// Sanity bound, not exact equality: the planted node's TriggerTime
	// carries a small lead buffer past "now" (v0.10.0 slew lead-comp),
	// so the honest post-burn CA (searched from TriggerTime) need not
	// exactly match the advisory's own preview (which previews the burn
	// as if fired instantly at t=0) — but on this fixture's small,
	// slowly-evolving lag geometry they should stay in the same
	// ballpark.
	if adv.AchievableCA > 0 && (ca > adv.AchievableCA*5 || ca < adv.AchievableCA/5) {
		t.Errorf("committed CA %.0f m is far from the advisory's own preview %.0f m — "+
			"expected the same ballpark for this slowly-evolving geometry", ca, adv.AchievableCA)
	}
}

// TestRendezvousCommitPlantedNudgeIgnoredWhenPastDue: a planted node
// whose TriggerTime has already elapsed (but hasn't fired yet this
// tick — a same-tick race) must not be treated as a future course to
// search from; RendezvousCommit falls back to the current-course
// search instead of dividing by a non-positive dt.
func TestRendezvousCommitPlantedNudgeIgnoredWhenPastDue(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	adv, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("small-lag geometry yielded no useful nudge (%v); not exercising this path", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if adv == nil || !adv.Ok {
		t.Fatalf("expected a successful plant, got %+v", adv)
	}
	c := w.ActiveCraft()
	c.Nodes[0].TriggerTime = w.Clock.SimTime // due "now" — dt <= 0

	// Must not panic or misbehave; falls through to the (refusing,
	// zero-drift) current-course search.
	if _, _, ok := w.RendezvousCommit(); ok {
		t.Error("RendezvousCommit committed via a past-due planted node instead of falling back")
	}
}

// TestPostBurnStateWithTarget_TargetRelativeAxisAppliesBurn (PR #392
// review, finding 1, root cause): PostBurnState resolves a planted
// node's direction via spacecraft.NodeBurnDirection, which falls
// through to DirectionUnit for every mode it doesn't special-case —
// and DirectionUnit returns the ZERO vector for the four target-
// relative modes (thrust.go ~287-290), since they need target state
// DirectionUnit doesn't have. So a BurnTargetPrograde/Retrograde
// planted node used to silently commit with NO applied Δv. This pins
// the fix at the lowest level, independent of whether a resulting
// encounter converges within the search horizon: the post-burn
// velocity must differ from the plain coast velocity by exactly the
// node's DV.
func TestPostBurnStateWithTarget_TargetRelativeAxisAppliesBurn(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()

	rT, vT, rok := w.TargetStateRelativeToActivePrimary()
	if !rok {
		t.Fatal("TargetStateRelativeToActivePrimary: not ok")
	}
	mu := c.Primary.GravitationalParameter()

	node := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnTargetRetrograde,
		DV:          5.0,
		Event:       spacecraft.TriggerAbsolute,
		TriggerTime: w.Clock.SimTime.Add(60 * time.Second),
		PrimaryID:   c.Primary.ID,
	}
	dt := node.TriggerTime.Sub(w.Clock.SimTime).Seconds()

	// Same propagation rendezvousCommitFromPlantedNode performs: the
	// target's relative state Kepler-propagated forward to the node's
	// TriggerTime, and the craft's own coast (no-burn) state at the
	// same instant as a baseline to diff against.
	targetState, tok := physics.KeplerStep(physics.StateVector{R: rT, V: vT}, mu, dt)
	if !tok {
		t.Fatal("KeplerStep on target state: not ok")
	}
	coastState, _ := w.propagateCraftWithPrimary(dt)

	postState, primaryID, ok := w.postBurnStateWithTarget(node, targetState.R, targetState.V)
	if !ok {
		t.Fatal("postBurnStateWithTarget: expected ok=true — this fixture's rotated " +
			"co-orbital geometry has nonzero relative velocity, so BurnTargetRetrograde's " +
			"direction is not degenerate")
	}
	if primaryID != c.Primary.ID {
		t.Errorf("primaryID = %q, want %q", primaryID, c.Primary.ID)
	}
	delta := postState.V.Sub(coastState.V).Norm()
	if math.Abs(delta-node.DV) > 1e-6 {
		t.Errorf("|post-burn V - coast V| = %.9f, want %.9f (== node.DV) — "+
			"a target-relative axis must resolve a real burn direction, not silently "+
			"apply zero Δv (finding 1)", delta, node.DV)
	}
}

// TestRendezvousCommitHonorsPlantedNudge_BothAxisFamilies (PR #392
// review, finding 1): the existing TestRendezvousCommitHonorsPlantedNudge
// only covers whatever axis PlanRendezvousNudge's own advisory happens
// to pick for the fixture — it could land on a velocity-frame axis
// (prograde/retrograde/normal/radial) or a target-relative one
// (BurnTargetPrograde/Retrograde) depending on the Lambert scan, and
// only the former exercised PostBurnState's working path before this
// fix. This test hand-plants BOTH families directly (bypassing the
// advisory's own axis choice) with the same Δv magnitude, so both a
// velocity-frame axis and a target-relative axis are pinned committing
// successfully via RendezvousCommit's planted-node path.
func TestRendezvousCommitHonorsPlantedNudge_BothAxisFamilies(t *testing.T) {
	const dv = 20.0 // m/s — enough to break the matched-orbit symmetry within the 4h horizon

	plant := func(t *testing.T, w *World, mode spacecraft.BurnMode) {
		t.Helper()
		c := w.ActiveCraft()
		node := ManeuverNode{
			Mode:          mode,
			DV:            dv,
			Event:         spacecraft.TriggerAbsolute,
			TriggerTime:   w.Clock.SimTime.Add(rendezvousBurnLeadMin),
			PrimaryID:     c.Primary.ID,
			Throttle:      1.0,
			TargetCraftID: w.Target.CraftID,
			AdvisoryKey:   AdvisoryKeyRendezvousNudge,
		}
		w.PlanNode(node)
	}

	t.Run("target-relative axis", func(t *testing.T) {
		w := rendezvousSmallLagWorld(t)
		plant(t, w, spacecraft.BurnTargetRetrograde)
		tau, ca, ok := w.RendezvousCommit()
		if !ok {
			t.Fatal("RendezvousCommit refused a planted BurnTargetRetrograde nudge — " +
				"finding 1: target-relative axes must resolve a real post-burn course")
		}
		if !tau.After(w.Clock.SimTime) || ca <= 0 {
			t.Errorf("tau=%v ca=%.3f — expected a future τ with positive CA", tau, ca)
		}
	})

	t.Run("velocity-frame axis", func(t *testing.T) {
		w := rendezvousSmallLagWorld(t)
		plant(t, w, spacecraft.BurnPrograde)
		tau, ca, ok := w.RendezvousCommit()
		if !ok {
			t.Fatal("RendezvousCommit refused a planted BurnPrograde nudge")
		}
		if !tau.After(w.Clock.SimTime) || ca <= 0 {
			t.Errorf("tau=%v ca=%.3f — expected a future τ with positive CA", tau, ca)
		}
	})
}
