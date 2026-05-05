package planner

import (
	"errors"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// NextClosestApproach finds the next time-to-encounter between two
// craft along their predicted segments. Both states must be in the
// SAME frame (typically the active craft's primary-relative frame —
// world.TargetStateRelativeToActivePrimary handles cross-primary
// conversion before this is called). The function only handles
// same-primary rendezvous; cross-SOI tooling is out of scope for
// v0.9.3 (matches the slice's manual-loop scenario in LEO).
//
// Algorithm: forward-propagate both craft via Verlet at intervals of
// roughly period/50 over `horizon` seconds, track minimum |rA - rB|,
// return the time + distance + relative velocity at the minimum-
// distance sample. No parabolic refinement — sample resolution is
// sub-second-class for typical LEO horizons, plenty for the HUD
// countdown which is recomputed each frame anyway.
//
// Returns:
//   - t: seconds from now until closest approach (0 if "now").
//   - dist: distance at closest approach (meters).
//   - vRel: vA − vB at closest approach (m/s vector — magnitude is
//     the |v_rel| HUD readout).
//   - err: non-nil for invalid inputs (non-positive mu / horizon).
//
// v0.9.3+.
func NextClosestApproach(
	stateA, stateB orbital.Vec3State,
	primary bodies.CelestialBody,
	mu, horizon float64,
) (t, dist float64, vRel orbital.Vec3, err error) {
	_ = primary // captured in the signature for future cross-frame work
	if horizon <= 0 {
		return 0, 0, orbital.Vec3{}, errors.New("rendezvous: non-positive horizon")
	}
	if mu <= 0 {
		return 0, 0, orbital.Vec3{}, errors.New("rendezvous: non-positive mu")
	}

	sA := physics.StateVector{R: stateA.R, V: stateA.V}
	sB := physics.StateVector{R: stateB.R, V: stateB.V}

	pA := orbitalPeriod(sA, mu)
	pB := orbitalPeriod(sB, mu)
	minPeriod := math.Min(pA, pB)
	if math.IsInf(minPeriod, 0) || math.IsNaN(minPeriod) || minPeriod <= 0 {
		minPeriod = horizon
	}

	// ~50 samples per orbit period, bounded.
	nSamples := int(math.Ceil(horizon / (minPeriod / 50)))
	if nSamples < 64 {
		nSamples = 64
	}
	if nSamples > 2048 {
		nSamples = 2048
	}
	dtSample := horizon / float64(nSamples)

	// Verlet sub-step per period/200 for stability; clamp under dtSample.
	subStep := minPeriod / 200
	if subStep <= 0 || subStep > dtSample {
		subStep = dtSample
	}
	nSub := int(math.Ceil(dtSample / subStep))
	if nSub < 1 {
		nSub = 1
	}
	dt := dtSample / float64(nSub)

	minDist := sA.R.Sub(sB.R).Norm()
	minT := 0.0
	minVRel := sA.V.Sub(sB.V)

	for i := 1; i <= nSamples; i++ {
		for j := 0; j < nSub; j++ {
			sA = physics.StepVerlet(sA, mu, dt)
			sB = physics.StepVerlet(sB, mu, dt)
		}
		d := sA.R.Sub(sB.R).Norm()
		if d < minDist {
			minDist = d
			minT = float64(i) * dtSample
			minVRel = sA.V.Sub(sB.V)
		}
	}

	return minT, minDist, minVRel, nil
}
