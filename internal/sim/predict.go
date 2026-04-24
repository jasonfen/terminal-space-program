package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
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
// (sampled into `samples` points), converts each sample into inertial
// coordinates, and partitions the trajectory into SOISegments whenever
// the dominant SOI changes.
//
// Body positions are taken as a snapshot at Clock.SimTime — this ignores
// planet motion during the preview and is accurate for short horizons
// relative to the target body's orbital period. For a 1-period LEO
// preview (≈ 90 min) this is trivially true. For interplanetary horizons
// it's an approximation, flagged in the commit body.
func (w *World) PredictedSegments(post physics.StateVector, totalSeconds float64, samples int) []SOISegment {
	if w.Craft == nil || samples < 2 {
		return nil
	}

	sys := w.System()
	primary := sys.Bodies[0]
	primaryPos := w.BodyPosition(primary) // should be zero — system primary is at origin

	// Snapshot every other body's inertial position once.
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}

	// Step the predictor around the craft's current primary (where the
	// burn is applied). We track two frames: state is primary-relative;
	// inertial = state + home-primary-inertial.
	homePrimary := w.Craft.Primary
	homePrimaryPos := positions[homePrimary.ID]
	if homePrimary.ID == primary.ID {
		homePrimaryPos = primaryPos
	}

	mu := homePrimary.GravitationalParameter()
	period := orbitalPeriod(post, mu)
	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}
	stepSecs := totalSeconds / float64(samples-1)
	s := post

	segments := []SOISegment{{PrimaryID: homePrimary.ID, Points: []orbital.Vec3{homePrimaryPos.Add(s.R)}}}
	current := homePrimary.ID

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
			s = physics.StepVerlet(s, mu, dt)
		}

		inertial := homePrimaryPos.Add(s.R)
		prim := dominantSOI(sys, positions, inertial, homePrimary)

		if prim.ID != current {
			segments = append(segments, SOISegment{PrimaryID: prim.ID})
			current = prim.ID
		}
		seg := &segments[len(segments)-1]
		seg.Points = append(seg.Points, inertial)
	}
	return segments
}

// dominantSOI returns the body whose SOI contains the given inertial
// position, preferring the smallest containing SOI. Falls back to the
// home primary when no other SOI contains the point.
func dominantSOI(
	sys bodies.System,
	positions map[string]orbital.Vec3,
	r orbital.Vec3,
	homePrimary bodies.CelestialBody,
) bodies.CelestialBody {
	primary := sys.Bodies[0]
	best := homePrimary
	bestSOI := math.Inf(1)
	for i := 1; i < len(sys.Bodies); i++ {
		b := sys.Bodies[i]
		bPos, ok := positions[b.ID]
		if !ok {
			continue
		}
		soi := physics.SOIRadius(b, primary)
		if soi == 0 {
			continue
		}
		d := r.Sub(bPos).Norm()
		if d <= soi && soi < bestSOI {
			best = b
			bestSOI = soi
		}
	}
	return best
}
