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
// then **parabolically refine** the minimum from its two bracketing
// samples and re-propagate to that sub-grid time for the reported
// distance + relative velocity.
//
// The refinement is not cosmetic. The HUD recomputes this every
// frame from live, slightly-noisy integrated state. Without
// refinement the answer is snapped to the ~period/50 grid (~111 s
// for LEO), so as the true minimum drifts across a grid boundary
// between frames the reported time jumps by a whole grid step and
// the distance pops to a different sample — the readout looks
// erratic even though the physics is smooth, making it impossible
// to judge whether the approach needs adjusting. The parabolic
// vertex is continuous in the inputs, so the readout is now stable
// frame-to-frame and reports the true sub-grid minimum, not the
// nearest sample.
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

	// Distance at every grid sample, dists[0] = separation now.
	dists := make([]float64, nSamples+1)
	dists[0] = sA.R.Sub(sB.R).Norm()
	minIdx := 0
	for i := 1; i <= nSamples; i++ {
		for j := 0; j < nSub; j++ {
			sA = physics.StepVerlet(sA, mu, dt)
			sB = physics.StepVerlet(sB, mu, dt)
		}
		d := sA.R.Sub(sB.R).Norm()
		dists[i] = d
		if d < dists[minIdx] {
			minIdx = i
		}
	}

	// Parabolic sub-grid refinement. Fit a parabola through the
	// minimum sample and its two neighbours; its vertex is the
	// continuous minimum. δ ∈ [-0.5, 0.5] is the fractional sample
	// offset. Skipped at the endpoints (no bracket) and when the
	// three points are near-collinear (flat / degenerate co-orbital
	// minimum — the exact time barely matters there and δ=0 keeps it
	// stable rather than blowing up on a ~0 denominator).
	delta := 0.0
	if minIdx > 0 && minIdx < nSamples {
		dL, dC, dR := dists[minIdx-1], dists[minIdx], dists[minIdx+1]
		denom := dL - 2*dC + dR
		if math.Abs(denom) > 1e-9 {
			delta = 0.5 * (dL - dR) / denom
			if delta > 0.5 {
				delta = 0.5
			} else if delta < -0.5 {
				delta = -0.5
			}
		}
	}
	tStar := (float64(minIdx) + delta) * dtSample
	if tStar < 0 {
		tStar = 0
	} else if tStar > horizon {
		tStar = horizon
	}

	// Re-propagate fresh state to the refined time for an accurate,
	// continuous distance + relative velocity (the parabola locates
	// the time; the true geometry at that time is what the HUD
	// reports). Same sub-step size, with a partial final step.
	rA := physics.StateVector{R: stateA.R, V: stateA.V}
	rB := physics.StateVector{R: stateB.R, V: stateB.V}
	remaining := tStar
	for remaining > 1e-9 {
		step := dt
		if step > remaining {
			step = remaining
		}
		rA = physics.StepVerlet(rA, mu, step)
		rB = physics.StepVerlet(rB, mu, step)
		remaining -= step
	}
	return tStar, rA.R.Sub(rB.R).Norm(), rA.V.Sub(rB.V), nil
}
