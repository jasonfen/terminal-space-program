package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

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
//
// Body positions are still snapshot at Clock.SimTime — accurate for
// short horizons relative to target body orbital period; an
// approximation flagged in commit history for interplanetary horizons.
func (w *World) PredictedSegments(post physics.StateVector, totalSeconds float64, samples int) []SOISegment {
	if w.Craft == nil || samples < 2 {
		return nil
	}

	sys := w.System()
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}

	current := w.Craft.Primary
	muNow := current.GravitationalParameter()
	state := post

	period := orbitalPeriod(state, muNow)
	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}
	stepSecs := totalSeconds / float64(samples-1)

	segments := []SOISegment{{
		PrimaryID: current.ID,
		Points:    []orbital.Vec3{positions[current.ID].Add(state.R)},
	}}

	for i := 1; i < samples; i++ {
		nSub := int(math.Ceil(stepSecs / maxStep))
		if nSub < 1 {
			nSub = 1
		}
		if nSub > 256 {
			nSub = 256
		}
		dt := stepSecs / float64(nSub)
		for j := 0; j < nSub; j++ {
			state = physics.StepVerlet(state, muNow, dt)

			crossingInertial := positions[current.ID].Add(state.R)
			cand := physics.FindPrimary(sys, crossingInertial, positions)
			if cand.Body.ID != current.ID {
				// Close the outgoing segment at the crossing so it
				// terminates where the new segment begins (no time gap
				// between the previous output sample and the rebase).
				segments[len(segments)-1].Points = append(
					segments[len(segments)-1].Points, crossingInertial)

				vOld := w.bodyInertialVelocity(current)
				vNew := w.bodyInertialVelocity(cand.Body)
				state = physics.Rebase(state, positions[current.ID], cand.Inertial, vOld.Sub(vNew))
				current = cand.Body
				muNow = current.GravitationalParameter()

				period = orbitalPeriod(state, muNow)
				maxStep = period / 100.0
				if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
					maxStep = 1.0
				}

				segments = append(segments, SOISegment{
					PrimaryID: current.ID,
					Points:    []orbital.Vec3{positions[current.ID].Add(state.R)},
				})
			}
		}
		seg := &segments[len(segments)-1]
		seg.Points = append(seg.Points, positions[current.ID].Add(state.R))
	}
	return segments
}
