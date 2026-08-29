package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestIntegrateOneCraftBurnReadyHoistedAcrossSubSteps is the
// correctness-preservation test for the performance finding on
// integrateOneCraft's RK4/Verlet sub-step loop: activeBurnTargetReady
// used to be re-resolved inside thrustingAt on EVERY sub-step (nSteps
// capped at 1024) plus twice more post-loop (burnExhausted and the
// EndTime-pause check) — a ghost-slate scan plus nodeTargetRelState →
// BodyPositionAt (SolveKepler) at the CONSTANT w.Clock.SimTime, which
// cannot change within one integrateOneCraft call. The fix hoists that
// resolve to once per craft per tick, the way `bc` is already hoisted,
// and threads the result through thrustingAt / burnExhausted instead.
//
// This must behave IDENTICALLY to the old per-call-site resolution,
// because nothing that resolve depends on (w.Ghosts, the local slate,
// w.Clock.SimTime) changes mid-call. Exercised with enough sim-time in
// one call to force MULTIPLE sub-steps (maxStep = period/100), so a
// per-sub-step-vs-once divergence would show up as partial thrust or a
// mid-call flip, not just a first-sub-step accident.
func TestIntegrateOneCraftBurnReadyHoistedAcrossSubSteps(t *testing.T) {
	t.Run("held: unresolved ghost target never thrusts across any sub-step", func(t *testing.T) {
		w := mustWorld(t)
		c := w.ActiveCraft()
		mu := c.Primary.GravitationalParameter()
		period := orbitalPeriod(c.State, mu)
		simDelta := time.Duration(period / 10 * float64(time.Second)) // ~10 sub-steps

		c.ActiveBurn = &spacecraft.ActiveBurn{
			Mode:             spacecraft.BurnTarget,
			DVRemaining:      100,
			EndTime:          w.Clock.SimTime.Add(time.Hour),
			PrimaryID:        c.Primary.ID,
			Throttle:         1,
			TargetGhostOwner: "SHA256:gern",
			TargetCraftID:    987654,
		}
		w.Ghosts = nil // never resolves — the burn must HOLD the whole call
		fuelBefore := c.ActiveStageFuel()
		endBefore := c.ActiveBurn.EndTime

		w.integrateOneCraft(c, simDelta)

		if c.ActiveBurn == nil {
			t.Fatal("held burn was torn down — want it kept alive, paused")
		}
		if c.ActiveBurn.DVRemaining != 100 {
			t.Errorf("DVRemaining = %v, want unchanged 100 (never thrust across any sub-step)", c.ActiveBurn.DVRemaining)
		}
		if got := c.ActiveStageFuel(); got != fuelBefore {
			t.Errorf("fuel = %v, want unchanged %v (never thrust across any sub-step)", got, fuelBefore)
		}
		if !c.ActiveBurn.EndTime.After(endBefore) {
			t.Errorf("EndTime = %v, want pushed out past %v (held, not exhausted)", c.ActiveBurn.EndTime, endBefore)
		}
	})

	t.Run("thrusting: resolved local target thrusts across every sub-step", func(t *testing.T) {
		w := mustWorld(t)
		c := w.ActiveCraft()

		sibling := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
		sibling.Primary = c.Primary
		sibling.State = c.State
		sibling.State.R = sibling.State.R.Add(orbital.Vec3{X: 1e6})
		w.AdoptCraft(sibling, false)

		mu := c.Primary.GravitationalParameter()
		period := orbitalPeriod(c.State, mu)
		simDelta := time.Duration(period / 10 * float64(time.Second)) // ~10 sub-steps

		c.ActiveBurn = &spacecraft.ActiveBurn{
			Mode: spacecraft.BurnTarget,
			// Far beyond what one call can deliver — isolates "did it
			// thrust on every sub-step" from "did the burn finish".
			DVRemaining:   1e6,
			EndTime:       w.Clock.SimTime.Add(time.Hour),
			PrimaryID:     c.Primary.ID,
			Throttle:      1,
			TargetCraftID: sibling.ID, // local ref, no ghost owner
		}
		fuelBefore := c.ActiveStageFuel()
		dvBefore := c.ActiveBurn.DVRemaining

		w.integrateOneCraft(c, simDelta)

		if c.ActiveBurn == nil {
			t.Fatal("resolved target-relative burn was torn down unexpectedly")
		}
		if c.ActiveBurn.DVRemaining >= dvBefore {
			t.Errorf("DVRemaining = %v, want it to have DECREASED across the multi-sub-step call (target resolves the whole time)", c.ActiveBurn.DVRemaining)
		}
		if got := c.ActiveStageFuel(); got >= fuelBefore {
			t.Errorf("fuel = %v, want it to have decreased — target resolves the whole time, so every sub-step should thrust", got)
		}
	})
}
