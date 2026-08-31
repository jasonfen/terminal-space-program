package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// rendezvousBeyondTwoHourWorld builds a two-craft LEO pair (500 km
// active, 650 km target, 25° phase offset) whose true first closest
// approach sits beyond the OLD K horizon (min(2×period, 2 h), which
// always capped at 2 h = 7200 s for LEO periods) but inside the 4 h
// window every other rendezvous surface already searches. Calibrated
// against internal/planner.NextClosestApproach directly (see #394):
// tCA ≈ 12,358 s (~3.4 h), CA ≈ 149.4 km — while a 7200 s search on
// the identical states snaps to its window edge and reports CA ≈
// 1,272 km, a wildly different (and wrong) encounter.
//
// States are raw world-frame XY (matching docking_test.go's
// convention) rather than run through the equatorial tilt frame —
// StepVerlet's two-body gravity is tilt-independent, so this is a
// faithful LEO scenario for closest-approach search purposes.
func rendezvousBeyondTwoHourWorld(t *testing.T) (*World, orbital.Vec3State, orbital.Vec3State) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("Earth")
	if earth == nil {
		t.Fatal("setup: Earth missing from Sol")
	}
	mu := earth.GravitationalParameter()

	rA := earth.RadiusMeters() + 500e3
	vA := math.Sqrt(mu / rA)
	a := w.Crafts[0]
	a.Primary = *earth
	a.State.R = orbital.Vec3{X: rA}
	a.State.V = orbital.Vec3{Y: vA}

	const phaseDeg = 25.0
	th := phaseDeg * math.Pi / 180
	rB := earth.RadiusMeters() + 650e3
	vB := math.Sqrt(mu / rB)
	b := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	b.Primary = *earth
	b.State = physics.StateVector{
		R: orbital.Vec3{X: rB * math.Cos(th), Y: rB * math.Sin(th)},
		V: orbital.Vec3{X: -vB * math.Sin(th), Y: vB * math.Cos(th)},
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)

	stateA := orbital.Vec3State{R: a.State.R, V: a.State.V}
	stateB := orbital.Vec3State{R: b.State.R, V: b.State.V}
	return w, stateA, stateB
}

// TestRendezvousAdvisoryMatchesTargetChipHorizon pins ADR 0045 S1
// (#394): K's advisory and the TARGET chip's TCA/CA must resolve the
// SAME encounter. Before this fix K searched its own period-scaled,
// 2h-capped window (rendezvousHorizonSeconds) while the TARGET chip
// (orbit_target_markers.go's closestApproachHorizonSec) and the
// Rendezvous Warp commit (rendezvousCommitHorizonSec) already searched
// a flat 4 h — so in LEO, where 2× period always exceeded the 2h cap,
// K silently scored a different, nearer encounter than the one the
// pilot could see on the HUD.
//
// The scenario is calibrated so the true first closest approach sits
// at ~3.4 h — past the old 2h cap, inside the new 4h window — so a
// regression back to the period-scaled horizon would make this test
// fail (not pass vacuously): rendezvousBeyondTwoHourWorldOldHorizon
// below confirms the old-style 2h search lands on a materially
// different distance/time on this exact scenario.
func TestRendezvousAdvisoryMatchesTargetChipHorizon(t *testing.T) {
	w, stateA, stateB := rendezvousBeyondTwoHourWorld(t)
	earth := w.Crafts[0].Primary
	mu := earth.GravitationalParameter()

	// The TARGET chip / map marker computation
	// (orbit_target_markers.go / orbit_chip_builders.go) — reproduced
	// here verbatim since it lives in the tui/screens package: same
	// inputs (active + target state, shared primary), same 4h flat
	// horizon.
	const chipHorizonSec = 4 * 3600.0
	chipTCA, chipCA, _, err := planner.NextClosestApproach(stateA, stateB, earth, mu, chipHorizonSec)
	if err != nil {
		t.Fatalf("chip-style NextClosestApproach: %v", err)
	}

	// Non-vacuous guard: the true encounter must sit beyond the OLD
	// 2h cap. If it doesn't, this test can't distinguish the fixed
	// behavior from the bug.
	if chipTCA <= 7200 || chipTCA >= 14400 {
		t.Fatalf("setup: chip TCA = %.0f s, want strictly between 7200 and 14400 s (test wouldn't stress the fix)", chipTCA)
	}

	adv, ok := w.RecommendedRendezvousBurn()
	if !ok {
		t.Fatalf("RecommendedRendezvousBurn: ok=false, want true (advisory computed)")
	}
	if math.Abs(adv.CurrentCA-chipCA) > 1e-6 {
		t.Errorf("K's CurrentCA = %.3f m, chip CA = %.3f m — same encounter search should agree exactly", adv.CurrentCA, chipCA)
	}

	// Prove the fix actually fires: the OLD K horizon
	// (min(2×period, 7200s) floor 600s) always capped at 7200s for
	// these LEO periods, so it would have searched a strictly
	// narrower window and landed on a different CA than the shared
	// 4h search above.
	oldTCA, oldCA, _, err := planner.NextClosestApproach(stateA, stateB, earth, mu, 7200)
	if err != nil {
		t.Fatalf("old-horizon NextClosestApproach: %v", err)
	}
	if math.Abs(oldCA-chipCA) < 1000 {
		t.Fatalf("setup: old 2h-capped search (CA=%.0f m) too close to the 4h result (CA=%.0f m) to prove the fix fires — pick a scenario with more separation", oldCA, chipCA)
	}
	_ = oldTCA
}

// TestRendezvousAdvisoryAndCommitShareHorizon pins that K's advisory
// (computeRendezvousAdvisory) and Engage/Rendezvous Warp's commit
// search (RendezvousCommit's current-course fallback) both resolve off
// the SAME rendezvousCommitHorizonSec constant (#394) — sabotaging
// that constant to a shorter value would move both surfaces together,
// since both call planner.NextClosestApproach with it directly on
// identical (active.State, TargetStateRelativeToActivePrimary) inputs.
//
// Verified non-vacuous the same way as the sibling test: the scenario
// puts the real encounter beyond the OLD 2h K cap, so a regression to
// the period-scaled horizon would desync K's CurrentCA from
// RendezvousCommit's ca (which never used the deleted function).
func TestRendezvousAdvisoryAndCommitShareHorizon(t *testing.T) {
	w, _, _ := rendezvousBeyondTwoHourWorld(t)

	adv, ok := w.RecommendedRendezvousBurn()
	if !ok {
		t.Fatalf("RecommendedRendezvousBurn: ok=false, want true")
	}

	tau, ca, cok := w.RendezvousCommit()
	if !cok {
		t.Fatalf("RendezvousCommit: ok=false, want true")
	}

	if math.Abs(adv.CurrentCA-ca) > 1e-6 {
		t.Errorf("K's CurrentCA = %.3f m, RendezvousCommit ca = %.3f m — both should resolve the same encounter", adv.CurrentCA, ca)
	}

	tCA := tau.Sub(w.Clock.SimTime).Seconds()
	if tCA <= 7200 || tCA >= 14400 {
		t.Fatalf("setup: committed tCA = %.0f s, want strictly between 7200 and 14400 s (test wouldn't stress the fix)", tCA)
	}
}
