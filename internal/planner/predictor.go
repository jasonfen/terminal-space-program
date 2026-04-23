// Package planner implements trajectory prediction (predictor) and stubs
// for Phase 3 maneuver-library work (hohmann, lambert) that slip past v0.1.
package planner

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// Predict forward-integrates a shadow StateVector using Verlet, returning
// a slice of inertial (primary-relative) positions sampled at regular
// intervals. Used by the maneuver screen for its live preview line.
//
// - start: initial state (post-burn).
// - mu: gravitational parameter of the primary.
// - totalSeconds: total sim-time horizon.
// - samples: number of points to return (inclusive of start).
func Predict(start physics.StateVector, mu, totalSeconds float64, samples int) []orbital.Vec3 {
	if samples < 2 {
		samples = 2
	}
	out := make([]orbital.Vec3, 0, samples)
	out = append(out, start.R)

	// Sub-step so each dt is < period/100 per the same guard world.Tick uses.
	period := orbitalPeriod(start, mu)
	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}

	stepSecs := totalSeconds / float64(samples-1)
	s := start
	for i := 1; i < samples; i++ {
		// Advance by stepSecs in as many sub-steps as period-safety needs.
		nSub := int(math.Ceil(stepSecs / maxStep))
		if nSub < 1 {
			nSub = 1
		}
		if nSub > 256 {
			nSub = 256
		}
		dt := stepSecs / float64(nSub)
		for j := 0; j < nSub; j++ {
			s = physics.StepVerlet(s, mu, dt)
		}
		out = append(out, s.R)
	}
	return out
}

func orbitalPeriod(s physics.StateVector, mu float64) float64 {
	a := physics.SemimajorAxis(s, mu)
	if a <= 0 || math.IsNaN(a) {
		return math.Inf(1)
	}
	return 2 * math.Pi * math.Sqrt(a*a*a/mu)
}
