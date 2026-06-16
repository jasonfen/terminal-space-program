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

// predictStep advances one predictor sub-step. Ballistic coast legs use
// analytic Kepler propagation (physics.KeplerStep) — exact for bound
// two-body arcs, with none of the dt² truncation that the fixed-step
// Verlet path drifts on an eccentric ellipse (GH #66: ~46 000 km by
// apogee on the e≈0.96 LEO→Luna transfer, so the dashed line sailed past
// Luna's SOI). It falls back to drag-aware Verlet exactly where Kepler
// can't apply — inside an atmosphere, on hyperbolic/degenerate arcs, or
// on an impactor whose periapsis dips below the surface — gated by the
// same canKeplerStepState guard the live integrator uses, so the
// predicted line agrees with the craft's actual flight. SOI transitions
// stay the caller's job: the sub-step loop's FindPrimary/Rebase runs
// against each step's output regardless of which propagator produced it.
func predictStep(state physics.StateVector, mu, dt float64, primary bodies.CelestialBody, bc float64) physics.StateVector {
	if canKeplerStepState(state, mu, primary) {
		if next, ok := physics.KeplerStep(state, mu, dt); ok {
			return next
		}
	}
	return physics.StepVerletWithAccel(state, mu, dt, func(r, v orbital.Vec3) orbital.Vec3 {
		return physics.DragAccel(r, v, primary, bc)
	})
}

// SOISegment is a contiguous run of predicted-trajectory samples that
// share the same owning SOI primary. PrimaryID == craft's home primary
// means "still in the home SOI"; a different ID means the segment has
// crossed into another body's sphere of influence.
//
// Each sample is recorded twice: at its inertial (system-primary-
// centered) position, and as the body-relative offset from the owning
// primary's center at that sample's clock. The offsets are the
// Local-to-Body Arc's raw material (ADR 0021 B): a foreign-SOI segment
// draws them anchored at the body's CURRENT position (SegmentDrawPoints),
// because at the inertial sample positions the body's own motion smears
// the in-SOI hyperbola across many times the SOI (~24×, measured
// Kern→Cursor) and the encounter reads as a straight line.
type SOISegment struct {
	PrimaryID string
	Points    []orbital.Vec3 // inertial, system-primary-centered, at each sample's clock
	RelPoints []orbital.Vec3 // offset from the owning primary's center at each sample's clock
}

// SegmentDrawPoints returns the canvas plot positions for a predicted
// segment under the Local-to-Body Arc rule (ADR 0021 B): a segment in the
// craft's home SOI draws at its inertial sample positions, unchanged; a
// foreign-SOI segment draws each body-relative sample anchored at the
// owning body's CURRENT position, so the hyperbola wraps the body's drawn
// disk and converges to truth as arrival nears. Every foreign-SOI consumer
// — the live SOI Pass arc, the dim counterfactual, and the planted-node
// legs — routes through this one helper, so the pictures at one body can
// never disagree (the #66 two-site lesson, applied to drawing).
func (w *World) SegmentDrawPoints(seg SOISegment, homeID string) []orbital.Vec3 {
	if seg.PrimaryID == homeID || len(seg.RelPoints) != len(seg.Points) {
		return seg.Points
	}
	var anchor orbital.Vec3
	found := false
	for _, b := range w.System().Bodies {
		if b.ID == seg.PrimaryID {
			anchor = w.BodyPosition(b)
			found = true
			break
		}
	}
	// Unknown primary, or one anchored at the origin (the system root —
	// its rel offsets ARE the inertial positions): nothing to rebase.
	if !found || anchor == (orbital.Vec3{}) {
		return seg.Points
	}
	pts := make([]orbital.Vec3, len(seg.RelPoints))
	for i, r := range seg.RelPoints {
		pts[i] = anchor.Add(r)
	}
	return pts
}

// predictTuning selects fidelity variants of the SOI-aware propagation
// loops (predictedSegmentsFromTuned and propagateStateWithPrimaryTuned).
// The zero value reproduces the PRE-v0.17.2 ("legacy") behavior exactly;
// production callers pass defaultPredictTuning() (see below). The
// SOI-entry prediction eval harness flips individual knobs to attribute
// moon-transfer Projected Orbit error to each numeric artifact (vault:
// warp-orbit-accuracy.md §"2026-06-09 — SOI-entry prediction fidelity").
// The knobs that won the attribution (BodyInterp + RefineCrossing +
// CoastSubStepCap=120 — the harness's "fix" variant) are the new
// production default; the zero value stays as the harness's "legacy"
// A/B baseline.
type predictTuning struct {
	// BodyPerSubStep refreshes body positions (and the SOI-test clock)
	// every integrator sub-step instead of once per output sample.
	// Targets the predictedSegmentsFromTuned staleness artifact: the
	// per-sub-step FindPrimary test otherwise runs against a positions
	// snapshot up to one sample interval old — hours on a multi-day
	// transfer horizon, thousands of km of moon motion.
	BodyPerSubStep bool

	// RefineCrossing bisects the SOI crossing time inside the sub-step
	// that detected the flip (~40 iterations of the same propagator),
	// then rebases with body positions and velocities evaluated at the
	// refined crossing time. Without it the crossing is quantized to
	// the sub-step that noticed it and the rebase velocities come from
	// an even staler clock. Same load-bearing trick as
	// NextClosestApproach's parabolic refinement (CONTEXT.md "Closest
	// Approach").
	RefineCrossing bool

	// BodyInterp linearly interpolates body positions between the
	// sample-boundary ephemeris evaluations for every sub-step's SOI
	// test — the cheap alternative to BodyPerSubStep (two Kepler
	// ephemeris evals per sample instead of one, plus vector lerps).
	// Within one sample a body's arc is <1°, so linearization error is
	// tens of meters (warp-orbit-accuracy.md §"Analytic SOI crossing").
	// Ignored when BodyPerSubStep is set.
	BodyInterp bool

	// CoastSubStepCap, when >0, applies an absolute cap (seconds) to
	// the coast sub-step of propagateStateWithPrimaryTuned — which
	// today uses bare period/100 (hours on a transfer ellipse, so SOI
	// detection quantizes to hours) — and re-resolves the sub-step
	// after a rebase into a tighter orbit (the #91 treatment, which
	// predictedSegmentsFromTuned already has). predictMaxSubStepCap
	// (120 s) is the natural value.
	CoastSubStepCap float64

	// HyperbolicDtCap, when >0, caps the Verlet dt (seconds) on arcs
	// where analytic Kepler is ineligible (hyperbolic flyby inside the
	// target moon's SOI, atmosphere, sub-surface periapsis) by
	// splitting one sub-step into smaller Verlet steps. Kepler-eligible
	// arcs are dt-independent and skip the split.
	HyperbolicDtCap float64

	// MaxSubSteps, when >0, overrides the per-sample / per-call
	// sub-step perf clamps (256 in predictedSegmentsFromTuned, 1024 in
	// propagateStateWithPrimaryTuned). The clamps silently re-coarsen
	// dt on long horizons — the #91 re-resolution asks for ~1 s steps
	// across a lunar flyby but the 256 clamp hands back ~30 s.
	MaxSubSteps int

	// PeriapsisDense distributes the output samples uniformly in ECCENTRIC
	// anomaly instead of uniform time for a bound starting orbit, evening the
	// arc-length between samples — densest at both apsides (ADR 0023 C
	// playtest follow-up). Equal-time bunches points at apoapsis and leaves
	// the departure periapsis coarse, which the gap-fill then draws as a
	// straight chord shooting out of the orbit. Set only on the rendered-leg
	// path (PredictedSegmentsFrom); the SOI-pass scans keep uniform time so
	// the encounter arc's own dense sampling is undisturbed.
	PeriapsisDense bool
}

// eccentricAnomalyStepSecs returns the per-output-sample durations (length
// samples-1, summing to totalSeconds) that walk a bound leg uniformly in
// ECCENTRIC anomaly. Its arc-length per step is shortest at both apsides and
// longest at quadrature, so it densifies the departure periapsis AND the
// apoapsis approach (sparser only through the gentle middle) — even visual
// dot spacing without a straight chord at either end (ADR 0023 C). Equal time
// instead bunches at apoapsis and starves periapsis; uniform true anomaly
// over-corrects, starving apoapsis. ok=false for near-circular (already
// even), non-elliptical, or degenerate orbits, where the caller keeps time.
func eccentricAnomalyStepSecs(state physics.StateVector, mu, totalSeconds float64, samples int) ([]float64, bool) {
	if samples < 3 || totalSeconds <= 0 || mu <= 0 {
		return nil, false
	}
	el := orbital.ElementsFromState(state.R, state.V, mu)
	e := el.E
	if el.A <= 0 || e < 0.05 || e >= 1 || math.IsNaN(el.A) || math.IsInf(el.A, 0) {
		return nil, false
	}
	period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)
	if period <= 0 || math.IsNaN(period) || math.IsInf(period, 0) {
		return nil, false
	}
	mean := 2 * math.Pi / period
	nu0 := orbital.TrueAnomalyFromState(state.R, state.V, mu, el)
	// Eccentric anomaly from true anomaly (half-angle), then mean anomaly.
	E0 := 2 * math.Atan2(math.Sqrt(1-e)*math.Sin(nu0/2), math.Sqrt(1+e)*math.Cos(nu0/2))
	M0 := E0 - e*math.Sin(E0)
	// Unwrapped eccentric anomaly at the horizon end — SolveKepler normalises
	// M into [-π,π], so solve here without the mod (E ≈ M seeds the Newton).
	Mend := M0 + mean*totalSeconds
	Eend := E0 + mean*totalSeconds
	for iter := 0; iter < 40; iter++ {
		f := Eend - e*math.Sin(Eend) - Mend
		fp := 1 - e*math.Cos(Eend)
		if fp == 0 {
			break
		}
		Eend -= f / fp
		if math.Abs(f) < 1e-10 {
			break
		}
	}
	if Eend <= E0 {
		return nil, false
	}
	times := make([]float64, samples)
	for i := range times {
		Ei := E0 + (Eend-E0)*float64(i)/float64(samples-1)
		times[i] = (Ei - e*math.Sin(Ei) - M0) / mean
	}
	times[0], times[samples-1] = 0, totalSeconds
	steps := make([]float64, samples-1)
	for i := range steps {
		steps[i] = times[i+1] - times[i]
		if steps[i] <= 0 {
			return nil, false // non-monotone (numerical) — fall back to uniform time
		}
	}
	return steps, true
}

// defaultPredictTuning is the production fidelity profile for both
// SOI-aware propagators (v0.17.2, ADR 0017). It enables the three knobs
// the SOI-entry prediction eval harness identified as the fix — the
// harness's "fix" variant — and which it confirmed converged (its
// "fix" column matched the converged "ref"/"ref/2" columns to within
// live-chunk quantization across all three scenarios):
//
//   - BodyInterp:      interpolate body positions across each output
//     sample so the per-sub-step SOI test runs against where the moon
//     actually is, not a stale start-of-sample snapshot (the cheap
//     alternative to BodyPerSubStep — ~10 ms vs ~29 ms per Earth→Moon
//     site-A call).
//   - RefineCrossing:  bisect the SOI crossing time inside the detecting
//     sub-step and rebase at the refined instant, so the predicted
//     entry clock stops quantizing to the sub-step boundary.
//   - CoastSubStepCap: cap the node-chain coast sub-step at 120 s (site
//     B's missing #91 treatment — bare period/100 is hours on a
//     transfer ellipse, which is what grew its entry-time bias to
//     +6720 s near arrival).
//
// TestDefaultPredictTuningIsFixVariant pins this equal to the harness's
// "fix" variant so the two can't drift. HyperbolicDtCap / MaxSubSteps
// are deliberately left off (measured immaterial once positions are
// fresh — a few km of post-entry drawing only) and stay harness-only.
func defaultPredictTuning() predictTuning {
	return predictTuning{
		BodyInterp:      true,
		RefineCrossing:  true,
		CoastSubStepCap: 120,
	}
}

// segSubStepClamp returns the sub-step perf clamp for the segment
// predictor (legacy 256 unless overridden).
func (tu predictTuning) segSubStepClamp() int {
	if tu.MaxSubSteps > 0 {
		return tu.MaxSubSteps
	}
	return 256
}

// chainSubStepClamp returns the sub-step perf clamp for the node-chain
// propagator (legacy 1024 unless overridden).
func (tu predictTuning) chainSubStepClamp() int {
	if tu.MaxSubSteps > 0 {
		return tu.MaxSubSteps
	}
	return 1024
}

// soiEntry records one predicted SOI transition: the body whose sphere
// was entered (or the parent re-entered on exit), the crossing
// wall-clock — bisection-refined when predictTuning.RefineCrossing is
// set, otherwise the end of the sub-step that detected the flip — and
// the craft state in the new primary's frame immediately after rebase.
// Diagnostic surface for the SOI-entry prediction eval harness;
// production rendering ignores it.
type soiEntry struct {
	BodyID string
	Clock  time.Time
	State  physics.StateVector
}

// predictStepTuned is predictStep with the HyperbolicDtCap knob: when
// the state is Kepler-ineligible (Verlet fallback territory) and the
// requested dt exceeds the cap, the step is split into equal Verlet
// sub-steps no longer than the cap. Kepler-eligible states pass
// through — the analytic step is exact at any dt.
func predictStepTuned(state physics.StateVector, mu, dt float64, primary bodies.CelestialBody, bc float64, tu predictTuning) physics.StateVector {
	if tu.HyperbolicDtCap > 0 && dt > tu.HyperbolicDtCap && !canKeplerStepState(state, mu, primary) {
		n := int(math.Ceil(dt / tu.HyperbolicDtCap))
		sub := dt / float64(n)
		for i := 0; i < n; i++ {
			state = predictStep(state, mu, sub, primary, bc)
		}
		return state
	}
	return predictStep(state, mu, dt, primary, bc)
}

// refineCrossingTime bisects the SOI crossing inside (t0, t0+dt]: preState
// is the craft state (current-primary frame) at wall-clock t0, known to be
// inside `current`'s SOI; propagating it by dt flips FindPrimary to a
// different primary. Returns tau ∈ (0, dt] — the offset of the earliest
// detected flip — the state at t0+tau (still in the OLD primary's frame),
// and the crossing candidate at that time. Body positions are evaluated
// fresh at each probe time, so the sphere is tested where the moon
// actually is at the candidate crossing instant.
func (w *World) refineCrossingTime(sys bodies.System, preState physics.StateVector, current bodies.CelestialBody, mu float64, t0 time.Time, dt float64, bc float64, tu predictTuning) (float64, physics.StateVector, physics.Primary) {
	probe := func(tau float64) (physics.StateVector, physics.Primary, bool) {
		s := predictStepTuned(preState, mu, tau, current, bc, tu)
		tc := t0.Add(time.Duration(tau * float64(time.Second)))
		pos := make(map[string]orbital.Vec3, len(sys.Bodies))
		for _, b := range sys.Bodies {
			pos[b.ID] = w.BodyPositionAt(b, tc)
		}
		cand := physics.FindPrimary(sys, pos[current.ID].Add(s.R), pos)
		return s, cand, cand.Body.ID != current.ID
	}

	lo, hi := 0.0, dt
	state, cand, flipped := probe(hi)
	if !flipped {
		// The flip seen by the caller's (possibly stale-positions) test
		// does not reproduce against fresh body positions — treat the
		// sub-step end as the crossing and let the caller's rebase
		// proceed; the next sub-step re-tests.
		return dt, state, cand
	}
	const iters = 40 // ~dt/2^40 — sub-millisecond on any sane dt
	for i := 0; i < iters && hi-lo > 1e-3; i++ {
		mid := (lo + hi) / 2
		s, c, f := probe(mid)
		if f {
			hi, state, cand = mid, s, c
		} else {
			lo = mid
		}
	}
	return hi, state, cand
}

// PredictedSegmentsFrom forward-integrates a post-burn state by
// totalSeconds and partitions the trajectory into SOISegments,
// parameterised on the starting primary and clock. Pre-v0.3.0 the
// predictor locked to the home primary's μ throughout, which made
// post-escape segments geometrically wrong even though their coloring
// was correct. v0.3.0: when a sub-step crosses a sphere-of-influence
// boundary, rebase the state vector to the new primary's frame and
// switch μ for subsequent steps. v0.6.1: used by the multi-leg
// colored preview, where each leg starts in its own node-planted
// frame (e.g. Hohmann departure leg in Earth, arrival leg in Mars).
// v0.8.4: takes a startClock so body positions track real time across
// the leg (per-sample refresh — sub-step refresh would cost 60 % of a
// render frame on long horizons), and folds atmospheric drag into the
// integrator via the active craft's EffectiveBallisticCoefficient.
// Output shape (a slice of SOISegments) is unchanged so the renderer
// keeps working.
func (w *World) PredictedSegmentsFrom(post physics.StateVector, startPrimary bodies.CelestialBody, startClock time.Time, totalSeconds float64, samples int) []SOISegment {
	tu := defaultPredictTuning()
	tu.PeriapsisDense = true // even arc-length spacing for the rendered leg (ADR 0023 C)
	segs, _ := w.predictedSegmentsFromTuned(post, startPrimary, startClock, totalSeconds, samples, tu)
	return segs
}

// predictedSegmentsFromTuned is PredictedSegmentsFrom parameterised on
// predictTuning (zero value = pre-v0.17.2 "legacy" behavior; the
// production wrapper above passes defaultPredictTuning()). It
// additionally reports each SOI transition
// as a soiEntry so the eval harness can score the predicted entry state
// without re-deriving it from sampled points.
func (w *World) predictedSegmentsFromTuned(post physics.StateVector, startPrimary bodies.CelestialBody, startClock time.Time, totalSeconds float64, samples int, tu predictTuning) ([]SOISegment, []soiEntry) {
	if w.ActiveCraft() == nil || samples < 2 {
		return nil, nil
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
	// Periapsis-dense legs walk a non-uniform per-sample schedule (uniform
	// true anomaly); equal-time otherwise. Sub-step sizing (nSub below) keeps
	// dt ≤ maxStep regardless, so integration accuracy and SOI detection are
	// unaffected — only the output cadence changes (ADR 0023 C).
	var stepSchedule []float64
	if tu.PeriapsisDense {
		if s, ok := eccentricAnomalyStepSecs(state, muNow, totalSeconds, samples); ok {
			stepSchedule = s
		}
	}

	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPositionAt(b, clock)
	}

	segments := []SOISegment{{
		PrimaryID: current.ID,
		Points:    []orbital.Vec3{positions[current.ID].Add(state.R)},
		RelPoints: []orbital.Vec3{state.R},
	}}
	var entries []soiEntry

predict:
	for i := 1; i < samples; i++ {
		if stepSchedule != nil {
			stepSecs = stepSchedule[i-1]
		}
		nSub := int(math.Ceil(stepSecs / maxStep))
		if nSub < 1 {
			nSub = 1
		}
		if nSub > tu.segSubStepClamp() {
			nSub = tu.segSubStepClamp()
		}
		dt := stepSecs / float64(nSub)
		stepDur := time.Duration(stepSecs * float64(time.Second))
		var posStart, posEnd map[string]orbital.Vec3
		if tu.BodyInterp && !tu.BodyPerSubStep {
			posStart = make(map[string]orbital.Vec3, len(sys.Bodies))
			posEnd = make(map[string]orbital.Vec3, len(sys.Bodies))
			endClock := clock.Add(stepDur)
			for _, b := range sys.Bodies {
				posStart[b.ID] = positions[b.ID]
				posEnd[b.ID] = w.BodyPositionAt(b, endClock)
			}
		}
		elapsed := 0.0 // seconds integrated into this sample so far
		for j := 0; j < nSub; j++ {
			preState := state
			state = predictStepTuned(state, muNow, dt, current, bc, tu)
			elapsed += dt

			if tu.BodyPerSubStep {
				subClock := clock.Add(time.Duration(elapsed * float64(time.Second)))
				for _, b := range sys.Bodies {
					positions[b.ID] = w.BodyPositionAt(b, subClock)
				}
			} else if tu.BodyInterp {
				frac := elapsed / stepSecs
				if frac > 1 {
					frac = 1
				}
				for _, b := range sys.Bodies {
					positions[b.ID] = posStart[b.ID].Scale(1 - frac).Add(posEnd[b.ID].Scale(frac))
				}
			}

			// v0.8.5: stop the predicted line at surface contact so
			// the dashed trajectory terminates on the body instead of
			// drawing the gravity-singularity slingshot loop.
			if clamped, hit := physics.ClampToSurface(state, current); hit {
				state = clamped
				impact := positions[current.ID].Add(state.R)
				seg := &segments[len(segments)-1]
				seg.Points = append(seg.Points, impact)
				seg.RelPoints = append(seg.RelPoints, state.R)
				break predict
			}

			crossingInertial := positions[current.ID].Add(state.R)
			cand := physics.FindPrimary(sys, crossingInertial, positions)
			if cand.Body.ID != current.ID {
				// Legacy rebase clock: the positions snapshot the flip was
				// detected against — the start of this output sample.
				rebaseClock := clock
				carry := 0.0 // crossing-refined leftover of this sub-step
				if tu.RefineCrossing {
					t0 := clock.Add(time.Duration((elapsed - dt) * float64(time.Second)))
					tau, refined, refCand := w.refineCrossingTime(sys, preState, current, muNow, t0, dt, bc, tu)
					if refCand.Body.ID == current.ID {
						// Phantom crossing: the flip came from the stale
						// positions snapshot and does not reproduce against
						// fresh ephemerides (the moving moon's SOI sphere
						// "lagged" onto/off the craft). Adopt the fresh
						// snapshot and keep integrating in-frame.
						state = refined
						subClock := clock.Add(time.Duration(elapsed * float64(time.Second)))
						for _, b := range sys.Bodies {
							positions[b.ID] = w.BodyPositionAt(b, subClock)
						}
						continue
					}
					state = refined
					cand = refCand
					rebaseClock = t0.Add(time.Duration(tau * float64(time.Second)))
					carry = dt - tau
					elapsed += tau - dt
					for _, b := range sys.Bodies {
						positions[b.ID] = w.BodyPositionAt(b, rebaseClock)
					}
					crossingInertial = positions[current.ID].Add(state.R)
				} else if tu.BodyPerSubStep || tu.BodyInterp {
					rebaseClock = clock.Add(time.Duration(elapsed * float64(time.Second)))
				}

				// Close the outgoing segment at the crossing so it
				// terminates where the new segment begins (no time gap
				// between the previous output sample and the rebase).
				outgoing := &segments[len(segments)-1]
				outgoing.Points = append(outgoing.Points, crossingInertial)
				outgoing.RelPoints = append(outgoing.RelPoints, state.R)

				vOld := w.bodyInertialVelocityAt(current, rebaseClock)
				vNew := w.bodyInertialVelocityAt(cand.Body, rebaseClock)
				state = physics.Rebase(state, positions[current.ID], cand.Inertial, vOld.Sub(vNew))
				current = cand.Body
				muNow = current.GravitationalParameter()
				entries = append(entries, soiEntry{BodyID: current.ID, Clock: rebaseClock, State: state})

				period = orbitalPeriod(state, muNow)
				newMaxStep := predictMaxSubStep(period)
				// Re-resolve the sub-step for the REST of this sample after
				// the rebase. The new SOI's orbit can be far tighter than
				// the one we entered with (e.g. an Earth-transfer leg with
				// dt≈120 s crossing into a fast lunar hyperbolic flyby whose
				// cap is ~1 s) — and only the Verlet path is affected, since
				// analytic Kepler is dt-independent. Leaving the coarse
				// pre-crossing dt in place integrated ~37 remaining 120 s
				// steps across the encounter, grossly aliasing its geometry.
				// Re-divide the remaining time into sub-steps sized for the
				// new orbit (keeping the 256 perf clamp). (#91)
				if tu.RefineCrossing {
					// The refined crossing leaves carry seconds of the
					// detecting sub-step un-integrated — always re-divide
					// the remainder, even when the new orbit is no tighter.
					remainingTime := float64(nSub-j-1)*dt + carry
					if remainingTime > 0 {
						effMax := newMaxStep
						if maxStep < effMax {
							effMax = maxStep
						}
						newNSub := int(math.Ceil(remainingTime / effMax))
						if newNSub < 1 {
							newNSub = 1
						}
						if newNSub > tu.segSubStepClamp() {
							newNSub = tu.segSubStepClamp()
						}
						nSub = j + 1 + newNSub
						dt = remainingTime / float64(newNSub)
					}
				} else if newMaxStep < maxStep {
					remainingTime := float64(nSub-j-1) * dt
					if remainingTime > 0 {
						newNSub := int(math.Ceil(remainingTime / newMaxStep))
						if newNSub < 1 {
							newNSub = 1
						}
						if newNSub > tu.segSubStepClamp() {
							newNSub = tu.segSubStepClamp()
						}
						nSub = j + 1 + newNSub
						dt = remainingTime / float64(newNSub)
					}
				}
				maxStep = newMaxStep

				segments = append(segments, SOISegment{
					PrimaryID: current.ID,
					Points:    []orbital.Vec3{positions[current.ID].Add(state.R)},
					RelPoints: []orbital.Vec3{state.R},
				})
			}
		}
		// Refresh body positions once per sample — bodies move slowly
		// relative to one Verlet sub-step (typically minutes), so the
		// per-sub-step SOI rebase above keeps using the previous-sample
		// snapshot accurately enough; doing this only at the sample
		// boundary keeps the refresh count at one per sample (`samples`
		// is the adaptive budget, capped at predictSamplesMax). With
		// tu.BodyPerSubStep the snapshot is already at the sample end;
		// the refresh below recomputes the same values.
		clock = clock.Add(stepDur)
		for _, b := range sys.Bodies {
			positions[b.ID] = w.BodyPositionAt(b, clock)
		}

		seg := &segments[len(segments)-1]
		seg.Points = append(seg.Points, positions[current.ID].Add(state.R))
		seg.RelPoints = append(seg.RelPoints, state.R)
	}
	return segments, entries
}
