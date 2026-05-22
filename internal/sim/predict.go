package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// Predicted-trajectory sample budgeting. A predicted leg is drawn with
// a roughly constant point density per orbital period, so the dashed
// ellipse stays crisp no matter how many revolutions the leg's horizon
// spans. Before v0.10.3 the budget was a flat 96 samples per leg; a
// long inter-node horizon — routine at high warp, where nodes are
// planted dozens of orbits ahead — then smeared the orbit into a
// sparse scatter of points (the three-cycle "predictor adaptive
// sampling" carry-over).
const (
	predictSamplesPerPeriod = 96  // target point density per revolution
	predictSamplesMin       = 96  // floor — also the legacy single-period budget
	predictSamplesMax       = 720 // ceiling — caps the per-frame body-ephemeris cost
)

// adaptiveSampleCount sizes a predicted leg's sample budget from its
// horizon and orbital period: ~predictSamplesPerPeriod points per
// revolution, clamped to [predictSamplesMin, predictSamplesMax]. A
// non-periodic (hyperbolic or degenerate) period falls back to the
// minimum — a hyperbolic arc does not loop, so a flat budget draws it
// cleanly.
func adaptiveSampleCount(horizonSecs, periodSecs float64) int {
	if periodSecs <= 0 || math.IsNaN(periodSecs) || math.IsInf(periodSecs, 0) ||
		horizonSecs <= 0 || math.IsNaN(horizonSecs) || math.IsInf(horizonSecs, 0) {
		return predictSamplesMin
	}
	n := int(math.Round(predictSamplesPerPeriod * horizonSecs / periodSecs))
	if n < predictSamplesMin {
		return predictSamplesMin
	}
	if n > predictSamplesMax {
		return predictSamplesMax
	}
	return n
}

// predictMaxSubStepCap bounds the predicted-trajectory integrator's
// Verlet sub-step (seconds). The per-leg cap had been period/100 alone,
// which is fine for a parking orbit but far too coarse for a long
// transfer leg: an Earth→Moon transfer ellipse has a ~9-day period, so
// period/100 ≈ 8000 s — a single Verlet step that long steps clean over
// a lunar SOI encounter, and the coarse integration flings the dashed
// trajectory off to a bogus heliocentric escape instead of drawing the
// encounter. An absolute cap keeps the sub-step fine enough to resolve
// an encounter regardless of the orbit's period. Verlet sub-steps don't
// refresh body positions (that stays per output sample), so a tighter
// cap is cheap. v0.10.3+.
const predictMaxSubStepCap = 120.0

// predictMaxSubStep returns the integrator sub-step cap for an orbit of
// the given period: period/100, clamped to predictMaxSubStepCap. A
// degenerate period (hyperbolic / NaN / non-positive) falls back to a
// conservative 1 s, matching the pre-v0.10.3 guard.
func predictMaxSubStep(period float64) float64 {
	if period <= 0 || math.IsNaN(period) || math.IsInf(period, 0) {
		return 1.0
	}
	if s := period / 100.0; s < predictMaxSubStepCap {
		return s
	}
	return predictMaxSubStepCap
}

// SOISegment is a contiguous run of predicted-trajectory samples that
// share the same owning SOI primary. PrimaryID == craft's home primary
// means "still in the home SOI"; a different ID means the segment has
// crossed into another body's sphere of influence.
type SOISegment struct {
	PrimaryID string
	Points    []orbital.Vec3 // inertial, system-primary-centered
}

// PredictedSegments forward-integrates a post-burn state by totalSeconds
// and partitions the trajectory into SOISegments. Pre-v0.3.0 the
// predictor locked to the home primary's μ throughout, which made
// post-escape segments geometrically wrong even though their coloring
// was correct. v0.3.0: when a sub-step crosses a sphere-of-influence
// boundary, rebase the state vector to the new primary's frame and
// switch μ for subsequent steps. Output shape (a slice of SOISegments)
// is unchanged so the renderer keeps working.
func (w *World) PredictedSegments(post physics.StateVector, totalSeconds float64, samples int) []SOISegment {
	return w.PredictedSegmentsFrom(post, w.ActiveCraft().Primary, w.Clock.SimTime, totalSeconds, samples)
}

// PredictedSegmentsFrom is the same trajectory predictor but
// parameterised on the starting primary and clock. v0.6.1: used by
// the multi-leg colored preview, where each leg starts in its own
// node-planted frame (e.g. Hohmann departure leg in Earth, arrival
// leg in Mars). v0.8.4: takes a startClock so body positions track
// real time across the leg (per-sample refresh — sub-step refresh
// would cost 60 % of a render frame on long horizons), and folds
// atmospheric drag into the integrator via the active craft's
// EffectiveBallisticCoefficient. Output shape unchanged.
func (w *World) PredictedSegmentsFrom(post physics.StateVector, startPrimary bodies.CelestialBody, startClock time.Time, totalSeconds float64, samples int) []SOISegment {
	if w.ActiveCraft() == nil || samples < 2 {
		return nil
	}

	sys := w.System()
	bc := w.ActiveCraft().EffectiveBallisticCoefficient()

	current := startPrimary
	muNow := current.GravitationalParameter()
	state := post
	clock := startClock

	period := orbitalPeriod(state, muNow)
	maxStep := predictMaxSubStep(period)
	stepSecs := totalSeconds / float64(samples-1)

	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPositionAt(b, clock)
	}

	segments := []SOISegment{{
		PrimaryID: current.ID,
		Points:    []orbital.Vec3{positions[current.ID].Add(state.R)},
	}}

predict:
	for i := 1; i < samples; i++ {
		nSub := int(math.Ceil(stepSecs / maxStep))
		if nSub < 1 {
			nSub = 1
		}
		if nSub > 256 {
			nSub = 256
		}
		dt := stepSecs / float64(nSub)
		stepDur := time.Duration(stepSecs * float64(time.Second))
		for j := 0; j < nSub; j++ {
			state = physics.StepVerletWithAccel(state, muNow, dt, func(r, v orbital.Vec3) orbital.Vec3 {
				return physics.DragAccel(r, v, current, bc)
			})

			// v0.8.5: stop the predicted line at surface contact so
			// the dashed trajectory terminates on the body instead of
			// drawing the gravity-singularity slingshot loop.
			if clamped, hit := physics.ClampToSurface(state, current); hit {
				state = clamped
				impact := positions[current.ID].Add(state.R)
				segments[len(segments)-1].Points = append(
					segments[len(segments)-1].Points, impact)
				break predict
			}

			crossingInertial := positions[current.ID].Add(state.R)
			cand := physics.FindPrimary(sys, crossingInertial, positions)
			if cand.Body.ID != current.ID {
				// Close the outgoing segment at the crossing so it
				// terminates where the new segment begins (no time gap
				// between the previous output sample and the rebase).
				segments[len(segments)-1].Points = append(
					segments[len(segments)-1].Points, crossingInertial)

				vOld := w.bodyInertialVelocityAt(current, clock)
				vNew := w.bodyInertialVelocityAt(cand.Body, clock)
				state = physics.Rebase(state, positions[current.ID], cand.Inertial, vOld.Sub(vNew))
				current = cand.Body
				muNow = current.GravitationalParameter()

				period = orbitalPeriod(state, muNow)
				maxStep = predictMaxSubStep(period)

				segments = append(segments, SOISegment{
					PrimaryID: current.ID,
					Points:    []orbital.Vec3{positions[current.ID].Add(state.R)},
				})
			}
		}
		// Refresh body positions once per sample — bodies move slowly
		// relative to one Verlet sub-step (typically minutes), so the
		// per-sub-step SOI rebase above keeps using the previous-sample
		// snapshot accurately enough; doing this only at the sample
		// boundary keeps the refresh count at one per sample (`samples`
		// is the adaptive budget, capped at predictSamplesMax).
		clock = clock.Add(stepDur)
		for _, b := range sys.Bodies {
			positions[b.ID] = w.BodyPositionAt(b, clock)
		}

		seg := &segments[len(segments)-1]
		seg.Points = append(seg.Points, positions[current.ID].Add(state.R))
	}
	return segments
}
