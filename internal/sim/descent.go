// Package sim — the descent half of the surface view (issue #348 §3,
// ADR 0043). Pure forecasts the launch/surface screen renders: where
// the current trajectory meets the ground (PredictImpact), whether a
// full-throttle stop burn started now would arrest it in time
// (PredictPoweredStop), and the latest moment that burn can still start
// (PredictBurnAt) — DescentCorridorFor bundles the cheap, gate-defining
// half of the block; the two powered forecasts are expensive enough
// (issue #377) that callers are expected to cache them (ADR 0017; see
// screens.LaunchView's descentStopCache).
//
// PredictImpact reuses the predictor's ONE propagator — predictStep,
// analytic Kepler where the arc is Kepler-eligible and drag-aware Verlet
// everywhere else — so the drawn descent arc agrees with the orbit map's
// dashed trajectory AND with the craft's actual flight. PredictPoweredStop
// / PredictBurnAt integrate with physics.StepRK4 instead (thrust is a
// non-conservative force Kepler propagation can't carry), but still
// through exactly one accel closure (poweredStopAccel) shared by every
// termination test inside them — the same #66 two-drift-sites rule this
// file has always followed, just a second single site rather than a
// second propagator layered on the first.
//
// PredictImpact is ballistic-from-now: it propagates the CURRENT state
// without assuming any future thrust, exactly like the orbit map's
// projected orbit. That is what makes the impact marker live under a
// burn — every tick the burn reshapes the state, and the next frame's
// forecast is taken from the reshaped state, so the marker slides along
// the ground as the player thrusts. PredictPoweredStop / PredictBurnAt
// are the deliberate exception: they DO model a hypothetical future
// burn (100% throttle, current stage only) because "can this still be
// stopped" is unanswerable without it.

package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const (
	// DescentPredictHorizon is how far ahead the surface view looks for
	// ground contact. Past it the forecast reports "no impact" and the
	// corridor instruments stand down — a craft whose trajectory clears
	// the surface for the next half hour is not on a descent, whatever
	// its instantaneous radial rate says (the routine case: an eccentric
	// parking orbit falling toward periapsis).
	DescentPredictHorizon = 30 * time.Minute

	// impactSubStepCap bounds the forecast's integrator sub-step in
	// seconds. predictMaxSubStep alone (period/100, capped at 120 s) is
	// far too coarse near the ground: an impacting arc is never
	// Kepler-eligible (canKeplerStepState rejects a sub-surface
	// periapsis), so every step is Verlet, and a 120 s Verlet step at
	// descent speeds walks kilometres between surface tests. 2 s keeps a
	// full-horizon forecast under ~1000 steps — cheap enough to redo
	// every frame, which is what "live under thrust" requires.
	impactSubStepCap = 2.0

	// impactMaxSubSteps caps total integrator work per forecast so a
	// degenerate state (tiny period → tiny sub-step) can't stall the
	// render loop. Hitting the cap coarsens dt rather than truncating
	// the horizon, so the forecast still spans DescentPredictHorizon.
	impactMaxSubSteps = 1024

	// impactPathSamples is the target number of points on the drawn arc.
	// The arc is inked as a dashed polyline (ClassPlanned), which
	// interpolates between points, so this only has to be dense enough
	// that the curve doesn't facet — not one point per pixel.
	impactPathSamples = 180

	// surfaceRefineIters / surfaceRefineEpsilon bisect the contact time
	// inside the sub-step that detected it, the same trick
	// refineCrossingTime uses on an SOI crossing. Without it the impact
	// point quantizes to wherever a 2 s step happened to land — tens to
	// hundreds of metres of marker jitter frame to frame.
	surfaceRefineIters   = 40
	surfaceRefineEpsilon = 1e-3
)

// ImpactPrediction is the ballistic-from-now ground-contact forecast:
// where the current trajectory meets the primary's surface, when, and
// how fast. Positions are PRIMARY-RELATIVE (body at the origin), the
// same frame the surface view's canvas draws in and the same frame
// craft.State.R lives in.
type ImpactPrediction struct {
	// Point is the contact position, projected exactly onto the
	// primary's mean radius so the marker lands on the drawn ground
	// line rather than a metre under it.
	Point orbital.Vec3
	// Path is the sampled trajectory from the craft's current position
	// (Path[0]) to Point (the last element) — the raw material for the
	// dashed arc to ground.
	Path []orbital.Vec3
	// TimeToImpact is the flight time from now to contact.
	TimeToImpact time.Duration
	// SpeedMps is the SURFACE-relative speed at contact (v − ω×r via
	// physics.AirRelativeVelocity — the same quantity the surface-arrival
	// classifier and the chute gate measure), not the inertial speed.
	SpeedMps float64
}

// PredictImpact forward-propagates the craft's current state until it
// meets the primary's surface, returning ok=false when the trajectory
// clears the ground for the whole horizon (or the inputs are
// degenerate). Thrust is deliberately NOT modelled: the forecast answers
// "where do I land if I stop flying now", which is the question a
// descent instrument exists to answer, and it re-derives from the live
// state every frame so an active burn visibly walks the answer.
func PredictImpact(c *spacecraft.Spacecraft, horizon time.Duration) (ImpactPrediction, bool) {
	fc, setupOK := forwardBallisticPath(c, horizon)
	if !setupOK || !fc.ImpactFound {
		return ImpactPrediction{}, false
	}
	return ImpactPrediction{
		Point:        fc.Impact,
		Path:         fc.Path,
		TimeToImpact: fc.TimeToImpact,
		SpeedMps:     fc.SpeedMps,
	}, true
}

// ballisticForecast is forwardBallisticPath's result: the sampled path
// plus, when the horizon actually finds one, ground contact. Shared by
// PredictImpact (descent, ADR 0043 §3 first half) and PredictAscentPath
// (ascent, §3 second half / issue #348, internal/sim/ascent.go) — see
// forwardBallisticPath's doc comment for why the loop itself lives in
// exactly one place.
type ballisticForecast struct {
	Path         []orbital.Vec3
	Impact       orbital.Vec3
	ImpactFound  bool
	TimeToImpact time.Duration
	SpeedMps     float64

	// samples parallels Path at the same cadence but carries the full
	// (state, elapsed-seconds) pair rather than just the plotted
	// position — PredictBurnAt's candidate start states (issue #377 §2).
	// Unexported: PredictImpact never surfaces it, and nothing outside
	// this file needs a burn-at candidate that isn't already resolved
	// into a BurnAtCue. Reusing forwardBallisticPath's own walk means
	// the "latest start" search costs zero extra propagation over what
	// the dashed arc already computes — only extra bookkeeping.
	samples []ballisticSample
}

// ballisticSample is one point on a ballistic-coast forecast, keeping
// the full state (not just position) plus how far into the forecast it
// sits. PredictBurnAt bisects over a slice of these instead of
// re-propagating (issue #377 §2).
type ballisticSample struct {
	state      physics.StateVector
	elapsedSec float64
}

// forwardBallisticPath is the one forward-propagation loop behind both
// the descent and ascent halves of the surface view (ADR 0043 §3 / issue
// #348): it walks the craft's current state forward via predictStep —
// the repo's single propagator — until either ground contact or the
// horizon runs out, and returns the full sampled path either way.
// PredictImpact discards the path when no contact is found inside the
// horizon; PredictAscentPath needs exactly that path (a climbing vessel,
// or one that has already reached a stable-enough orbit, is the common
// case where no contact is ever found) — this is what makes both
// instrument sets agree on where this trajectory goes, instead of a
// second propagator drifting apart from this one (the #66 lesson
// documented at the top of this file).
//
// setupOK is false only for the degenerate-input cases PredictImpact has
// always refused: a nil craft, missing primary radius/mu, a non-positive
// horizon, or a craft state already at/under the surface.
func forwardBallisticPath(c *spacecraft.Spacecraft, horizon time.Duration) (ballisticForecast, bool) {
	if c == nil {
		return ballisticForecast{}, false
	}
	primary := c.Primary
	radius := primary.RadiusMeters()
	mu := primary.GravitationalParameter()
	total := horizon.Seconds()
	if radius <= 0 || mu <= 0 || total <= 0 {
		return ballisticForecast{}, false
	}
	state := c.State
	if r := state.R.Norm(); r <= radius || math.IsNaN(r) {
		return ballisticForecast{}, false
	}
	bc := c.EffectiveBallisticCoefficient()

	dt := predictMaxSubStep(orbitalPeriod(state, mu))
	if dt > impactSubStepCap {
		dt = impactSubStepCap
	}
	steps := int(math.Ceil(total / dt))
	if steps > impactMaxSubSteps {
		steps = impactMaxSubSteps
		dt = total / float64(steps)
	}
	if steps < 1 {
		return ballisticForecast{}, false
	}
	sampleEvery := steps / impactPathSamples
	if sampleEvery < 1 {
		sampleEvery = 1
	}

	path := make([]orbital.Vec3, 0, impactPathSamples+2)
	path = append(path, state.R)
	samples := make([]ballisticSample, 0, impactPathSamples+2)
	samples = append(samples, ballisticSample{state: state, elapsedSec: 0})
	elapsed := 0.0
	for i := 0; i < steps; i++ {
		pre := state
		state = predictStep(state, mu, dt, primary, bc)
		if state.R.Norm() < radius {
			hit, tau := refineSurfaceCrossing(pre, mu, dt, primary, bc, radius)
			elapsed += tau
			point := hit.R
			if n := point.Norm(); n > 0 {
				point = point.Scale(radius / n)
			}
			path = append(path, point)
			return ballisticForecast{
				Path:         path,
				Impact:       point,
				ImpactFound:  true,
				TimeToImpact: time.Duration(elapsed * float64(time.Second)),
				SpeedMps:     physics.AirRelativeVelocity(hit.R, hit.V, primary).Norm(),
				samples:      samples,
			}, true
		}
		elapsed += dt
		if (i+1)%sampleEvery == 0 {
			path = append(path, state.R)
			samples = append(samples, ballisticSample{state: state, elapsedSec: elapsed})
		}
	}
	return ballisticForecast{Path: path, samples: samples}, true
}

// refineSurfaceCrossing bisects surface contact inside (0, dt]: pre is
// the state at the start of the detecting sub-step (known above the
// surface) and propagating it by dt is known to land below. Returns the
// state at the earliest detected contact and the offset tau ∈ (0, dt]
// that produced it. Each probe re-integrates from pre by the trial
// offset — one propagator call, never a partial re-walk — so the refined
// state is on the same curve the sub-step loop drew.
func refineSurfaceCrossing(pre physics.StateVector, mu, dt float64, primary bodies.CelestialBody, bc, radius float64) (physics.StateVector, float64) {
	lo, hi := 0.0, dt
	state := predictStep(pre, mu, hi, primary, bc)
	for i := 0; i < surfaceRefineIters && hi-lo > surfaceRefineEpsilon; i++ {
		mid := (lo + hi) / 2
		s := predictStep(pre, mu, mid, primary, bc)
		if s.R.Norm() < radius {
			hi, state = mid, s
		} else {
			lo = mid
		}
	}
	return state, hi
}

// MarginState is the burn-margin alarm ladder. The surface view renders
// it as a label AND a colour swap on the MARGIN row, never as a subtle
// shade alone — a silent state reads as broken.
type MarginState int

const (
	// MarginNone means the margin is undefined for this state (not
	// descending, on the ground, no mass). Rendered as an em dash.
	MarginNone MarginState = iota
	// MarginOK: comfortably more stopping capability than needed.
	MarginOK
	// MarginTight: enough to stop, but with little to spare — the
	// suicide-burn window is closing.
	MarginTight
	// MarginInsufficient: the stack CANNOT null this descent rate in the
	// altitude left. The alarm state.
	MarginInsufficient
)

// marginTightBelow is the ratio under which a survivable margin still
// warrants a warning. 1.5× is one and a half times the deceleration the
// stop needs — below that, a few seconds of hesitation (or the mass the
// burn itself has yet to shed) eats the whole reserve.
const marginTightBelow = 1.5

// MarginLimiter names which of the two capabilities binds the margin:
// the thrust available to decelerate, or the propellant left to spend on
// it. Reported so the alarm says what to fix, not just that something is
// wrong.
type MarginLimiter int

const (
	LimitNone MarginLimiter = iota
	LimitThrust
	LimitFuel
)

func (l MarginLimiter) String() string {
	switch l {
	case LimitThrust:
		return "thrust"
	case LimitFuel:
		return "fuel"
	}
	return ""
}

// BurnMargin answers the descent question "can the remaining thrust kill
// this velocity in the altitude left", as a ratio of capability to
// requirement (≥ 1 means yes).
//
// Two independent capabilities have to hold, so the reported Ratio is
// the smaller of the two and Limiter says which one bound it:
//
//   - Acceleration. Nulling a descent rate v within altitude h needs a
//     net deceleration of at least v²/2h. The stack's net deceleration
//     at full thrust is a_net = F/m − g_local (gravity keeps pulling
//     through the burn), so the acceleration ratio is a_net / (v²/2h).
//     A stack that can't out-thrust local gravity (a_net ≤ 0) has ratio
//     0 — it cannot stop at any altitude.
//   - Propellant. The stop takes t = v/a_net seconds at full thrust, and
//     the engine spends Δv at F/m per second the whole time, so it costs
//     v·(F/m)/a_net m/s of the budget — strictly more than v, because
//     part of every second is spent holding the vehicle up. The
//     propellant ratio is RemainingDeltaV over that.
//
// Both are the idealised constant-mass, straight-down, frozen-g bound:
// no attitude error, no throttle ramp, no mass shed mid-burn, g_local
// frozen at the instant evaluated, and v the VERTICAL descent rate only
// (issue #377). That is NOT "optimistic only in the mass term" — ignoring
// mass loss is the one place this model is conservative (a real burn
// gets lighter, so a_net actually grows through the burn). The other two
// idealisations run the other way: freezing g at the current altitude
// overstates it by ~10-20% over a real descent (g grows as r shrinks),
// and dropping the horizontal component entirely is the dangerous one —
// a craft carrying serious downrange speed reads as comfortably OK here
// while nowhere near actually being able to stop. DescentCorridorFor no
// longer wires this into the corridor's Margin field for exactly that
// reason: PredictPoweredStop integrates the real burn (mass loss, g(r),
// the horizontal component, and drag) instead of hand-deriving
// corrections for each. ComputeBurnMargin stays as a cheap, pure,
// still-tested piece of arithmetic — just not the corridor's answer to
// "can this still be stopped".
type BurnMargin struct {
	Ratio        float64
	State        MarginState
	Limiter      MarginLimiter
	NetAccelMps2 float64 // F/m − g_local, the deceleration actually available
	ReqAccelMps2 float64 // v²/2h, the deceleration the stop requires
	ReqDVMps     float64 // the stop burn's Δv cost (+Inf when it can't be flown)
	AvailDVMps   float64 // the stack's remaining Δv
}

// ComputeBurnMargin evaluates the margin from SI scalars. Pure, so the
// arithmetic can be pinned exactly by tests without seeding a World.
//
// Superseded by PredictPoweredStop (issue #377) as the corridor's actual
// margin — DescentCorridorFor no longer calls this at all (see its doc
// comment). It is kept only as the frozen-scalar BASELINE
// TestPredictPoweredStopLunaHorizontalRegression pins its "before"
// number against — that regression is the evidence the whole slice
// rests on, so the old arithmetic needs to keep existing and keep being
// exactly this, not because anything should call it live again. Do not
// wire this back into DescentCorridorFor or any other live code path.
func ComputeBurnMargin(thrustN, massKg, gLocalMps2, descentRateMps, altitudeM, availDVMps float64) BurnMargin {
	if massKg <= 0 || altitudeM <= 0 || descentRateMps <= 0 {
		return BurnMargin{AvailDVMps: availDVMps}
	}
	aAvail := thrustN / massKg
	aNet := aAvail - gLocalMps2
	reqAccel := descentRateMps * descentRateMps / (2 * altitudeM)
	m := BurnMargin{
		NetAccelMps2: aNet,
		ReqAccelMps2: reqAccel,
		ReqDVMps:     math.Inf(1),
		AvailDVMps:   availDVMps,
	}
	if aNet <= 0 || reqAccel <= 0 {
		// Can't out-thrust gravity: no altitude and no propellant makes
		// this stop flyable, so the thrust side binds at zero.
		m.Ratio = 0
		m.State = MarginInsufficient
		m.Limiter = LimitThrust
		return m
	}
	accelRatio := aNet / reqAccel
	m.ReqDVMps = descentRateMps * aAvail / aNet
	dvRatio := math.Inf(1)
	if m.ReqDVMps > 0 {
		dvRatio = availDVMps / m.ReqDVMps
	}
	m.Ratio, m.Limiter = accelRatio, LimitThrust
	if dvRatio < accelRatio {
		m.Ratio, m.Limiter = dvRatio, LimitFuel
	}
	switch {
	case m.Ratio < 1:
		m.State = MarginInsufficient
	case m.Ratio < marginTightBelow:
		m.State = MarginTight
	default:
		m.State = MarginOK
	}
	return m
}

// stdGravityMps2 is standard gravity, used to convert Isp (seconds) to
// exhaust velocity / mass-flow (Isp·g0). Duplicated locally rather than
// imported from spacecraft — planner/finiteburn.go's stdGravity const
// does the same, for the same reason (sim already imports spacecraft for
// the *spacecraft.Spacecraft type, but the constant itself isn't
// exported, and duplicating one float64 beats exporting it just for
// this).
const stdGravityMps2 = 9.80665

// PoweredStopOutcome names which of PredictPoweredStop's termination
// conditions the integration actually hit (issue #377 §1) — reported
// instead of just a pass/fail so the corridor's alarm can say what
// stopped the stop, not only that it didn't happen.
type PoweredStopOutcome int

const (
	// StopUndetermined is the zero value: PredictPoweredStop refused
	// (ok=false, the step cap was hit before any of the three real
	// outcomes below resolved) — "don't know", not "can't".
	StopUndetermined PoweredStopOutcome = iota
	// StopStopped: surface-relative speed reached ~0 before the ground.
	// MarginM is the altitude left, ≥ 0.
	StopStopped
	// StopCrashed: r crossed the surface before speed reached ~0.
	// ImpactSpeedMps is the surface-relative speed at that crossing;
	// MarginM is the altitude at which speed FINALLY would have reached
	// ~0 had the ground not been there (found by letting the same
	// integration run on past the crossing — no second propagator, no
	// hand-derived correction, just not stopping early) — so it comes
	// out ≤ 0, reading as "short by |MarginM|" rather than a negative
	// altitude (issue #377 §3).
	StopCrashed
	// StopFuelLimited: the current (bottom, firing) stage's propellant
	// ran out before either of the above. No auto-staging inside the
	// forecast (issue #377 §1) — a stop that needs a decouple is
	// reported as fuel-limited, not silently solved by staging for the
	// pilot. MarginM is the altitude AT the instant fuel ran out
	// (informational — "still this far up when the tank ran dry"), not
	// a stopping-distance shortfall the way StopCrashed's is.
	StopFuelLimited
)

func (o PoweredStopOutcome) String() string {
	switch o {
	case StopStopped:
		return "stopped"
	case StopCrashed:
		return "crashed"
	case StopFuelLimited:
		return "fuel-limited"
	}
	return "undetermined"
}

// PoweredStopPrediction is PredictPoweredStop's result: what happened
// when the current (or a candidate future) state was integrated forward
// under a full-throttle, surface-relative-retrograde stop burn.
type PoweredStopPrediction struct {
	Outcome PoweredStopOutcome
	// MarginM is signed altitude, metres: positive means the stop
	// completed with that much altitude still in hand (StopStopped);
	// non-positive means it didn't (StopCrashed — read as "short by
	// |MarginM|", never as a negative altitude, per issue #377 §3).
	// StopFuelLimited's MarginM is the altitude at the exhaustion
	// instant instead (see StopFuelLimited's doc).
	MarginM float64
	// ImpactSpeedMps is the surface-relative speed at the ground
	// crossing. Zero unless Outcome == StopCrashed.
	ImpactSpeedMps float64
	// ElapsedSec is the flight time from the integration's start to
	// termination.
	ElapsedSec float64
	// DVUsedMps is the Δv the rocket-equation accounting actually spent
	// reaching termination (bounded by the stage's available Δv).
	DVUsedMps float64
}

// stopSubStepSeconds is PredictPoweredStop's RK4 sub-step. Finer than
// forwardBallisticPath's impactSubStepCap (2 s): the accel closure here
// captures mass/fuel at the START of each outer step and holds it fixed
// across the step (the same per-step-snapshot trick
// planner.SimulateFiniteBurn uses), so the step needs to be short enough
// that a stage's mass doesn't change much within one — 1 s keeps that
// error well under the modelling error everywhere else in this forecast.
const stopSubStepSeconds = 1.0

// stopMaxSubSteps bounds PredictPoweredStop's total integrator work, the
// same discipline impactMaxSubSteps applies to the ballistic forecast:
// hitting the cap coarsens dt (spreading it across `horizon`) rather than
// silently truncating the search, and if that's still not enough the
// forecast refuses (ok=false) instead of guessing. A 1.6 km/s stop is
// ~800 s of flight at a typical lander's TWR (issue #377) — 2048 steps at
// 1 s each covers that many times over before the horizon-driven
// coarsening even engages.
const stopMaxSubSteps = 2048

// stopSpeedFloorMps is PredictPoweredStop's "stopped" tolerance. Exactly
// zero is a target the discrete integrator will straddle rather than
// land on, and it's also the direction singularity for the retrograde
// thrust closure (a zero-length vRel has no direction to burn along) —
// this is small enough to read as "stopped" against the speeds this
// forecast deals in (hundreds to low thousands of m/s) while staying
// comfortably clear of that singularity.
const stopSpeedFloorMps = 0.5

// stopAdaptiveShrinkFrac / stopAdaptiveMinDt bound the adaptive step
// shrink documented at its call site in the integration loop: a step
// removes at most this fraction of the current speed (at the craft's
// unthrottled accel ceiling), and never shrinks the step below
// stopAdaptiveMinDt seconds — without a floor, a craft sitting exactly
// on the singularity (vRel == 0, no direction to burn along, aAvail
// effectively infinite relative to a zero speedPre) would compute a
// zero-length step and stall the loop instead of burning through steps
// toward the sub-count cap.
const (
	stopAdaptiveShrinkFrac = 0.5
	stopAdaptiveMinDt      = 0.01
)

// stopStepSizeSeconds picks one outer-loop sub-step for
// predictPoweredStopFrom's integration: no larger than dt, capped
// further so a step never outruns the current stage's remaining fuel
// (fuelTimeLeftSec = fuelKg/mdot — mass bookkeeping already clamps
// `burned` to what's left regardless, but a step WIDER than that would
// still integrate full thrust, via poweredStopAccel, for longer than the
// tank can actually sustain it: an impulse the mass side never paid
// for), and, near the stop-speed floor, capped again so a step can't
// remove more than stopAdaptiveShrinkFrac of the current speed (see
// stopAdaptiveShrinkFrac's doc).
//
// The adaptive cap has its own floor, stopAdaptiveMinDt, so it can't
// shrink to zero and stall the loop at the vRel==0 singularity — but
// that floor must never be allowed to WIDEN the step back out past the
// fuel cap. A prior version applied the floor unconditionally
// (`stepDt = capDt` after raising capDt to the floor), so a step already
// shortened to a few ms by an almost-exhausted tank could get stretched
// back out to stopAdaptiveMinDt — up to 5x longer than the tank
// sustains at the sizes involved. Numerically small (stopAdaptiveMinDt
// is 10 ms) but wrong in kind, and the kind of wrong that stops being
// negligible if the floor is ever raised. The `< stepDt` re-check below
// is what keeps the floor-raise from ever widening the step past
// whatever cap (dt or the fuel cap) was already in force.
func stopStepSizeSeconds(dt, fuelTimeLeftSec, speedPre, aAvail float64) float64 {
	stepDt := dt
	if fuelTimeLeftSec < stepDt {
		stepDt = fuelTimeLeftSec
	}
	if speedPre > 0 && aAvail > 0 {
		if capDt := stopAdaptiveShrinkFrac * speedPre / aAvail; capDt < stepDt {
			if capDt < stopAdaptiveMinDt {
				capDt = stopAdaptiveMinDt
			}
			if capDt < stepDt {
				stepDt = capDt
			}
		}
	}
	return stepDt
}

// poweredStopAccel is the ONE accel closure behind every termination
// test inside PredictPoweredStop / PredictBurnAt (this file's own #66
// rule, restated for the powered half): two-body gravity, drag exactly
// as forwardBallisticPath applies it, plus thrust along surface-relative
// retrograde at full magnitude thrustN/massKg (zero once thrustN or
// massKg is non-positive — no fuel, no thrust). Surface-relative, not
// inertial (issue #377 §1): the goal is zero speed OVER THE GROUND, so
// this uses physics.AirRelativeVelocity, matching descentKinematics.
func poweredStopAccel(r, v orbital.Vec3, mu float64, primary bodies.CelestialBody, bc, thrustN, massKg float64) orbital.Vec3 {
	accel := physics.Accel(r, mu).Add(physics.DragAccel(r, v, primary, bc))
	if thrustN <= 0 || massKg <= 0 {
		return accel
	}
	vRel := physics.AirRelativeVelocity(r, v, primary)
	n := vRel.Norm()
	if n == 0 {
		return accel
	}
	dir := vRel.Scale(-1 / n)
	return accel.Add(dir.Scale(thrustN / massKg))
}

// bisectPoweredCrossing refines exactly when, within (0, dt], a powered
// sub-step first satisfies `hit` — the same bisection trick
// refineSurfaceCrossing uses on the ballistic forecast, adapted to an
// arbitrary predicate so PredictPoweredStop can share it between the
// ground-crossing test and the stop-speed test. pre is the state at the
// start of the detecting step (known NOT to satisfy hit); propagating it
// by dt is known TO satisfy hit. Every probe re-integrates from pre by a
// trial offset via the SAME accelFn the outer loop used for this step —
// one propagator, never a partial re-walk.
func bisectPoweredCrossing(pre physics.StateVector, dt float64, accelFn func(r, v orbital.Vec3, t float64) orbital.Vec3, hit func(physics.StateVector) bool) (physics.StateVector, float64) {
	lo, hi := 0.0, dt
	state := physics.StepRK4(pre, hi, accelFn, 0)
	for i := 0; i < surfaceRefineIters && hi-lo > surfaceRefineEpsilon; i++ {
		mid := (lo + hi) / 2
		s := physics.StepRK4(pre, mid, accelFn, 0)
		if hit(s) {
			hi, state = mid, s
		} else {
			lo = mid
		}
	}
	return state, hi
}

// PredictPoweredStop integrates a full-throttle, current-stage-only,
// surface-relative-retrograde stop burn forward from the craft's CURRENT
// state (issue #377 §1) — physics.StepRK4 through poweredStopAccel, the
// planner.SimulateFiniteBurn precedent for iterating a powered burn as a
// forecast, applied here to "when do I stop" instead of "hit this target
// apoapsis". ok=false only on the same degenerate inputs
// forwardBallisticPath has always refused, or on the step-cap refusal
// (StopUndetermined) documented on that constant.
func PredictPoweredStop(c *spacecraft.Spacecraft, horizon time.Duration) (PoweredStopPrediction, bool) {
	if c == nil {
		return PoweredStopPrediction{}, false
	}
	return predictPoweredStopFrom(c.State, c, horizon)
}

// predictPoweredStopFrom is PredictPoweredStop generalised to an
// arbitrary starting state so PredictBurnAt can ask "if I start the stop
// burn from THIS point on the ballistic coast instead of right now, does
// it still work" without a second implementation.
func predictPoweredStopFrom(state physics.StateVector, c *spacecraft.Spacecraft, horizon time.Duration) (PoweredStopPrediction, bool) {
	if c == nil {
		return PoweredStopPrediction{}, false
	}
	primary := c.Primary
	radius := primary.RadiusMeters()
	mu := primary.GravitationalParameter()
	total := horizon.Seconds()
	if radius <= 0 || mu <= 0 || total <= 0 {
		return PoweredStopPrediction{}, false
	}
	if r := state.R.Norm(); r <= radius || math.IsNaN(r) {
		return PoweredStopPrediction{}, false
	}
	bc := c.EffectiveBallisticCoefficient()
	thrustFull := c.Thrust
	ispSec := c.Isp
	massKg := c.TotalMass()
	fuelKg := c.ActiveStageFuel()
	if massKg <= 0 {
		return PoweredStopPrediction{}, false
	}
	// No engine, no Isp, or the firing stage is already dry: the stop
	// can't even begin. Fuel-limited from the first instant, current
	// altitude reported as "where that happened" (issue #377 §1).
	if thrustFull <= 0 || ispSec <= 0 || fuelKg <= 0 {
		return PoweredStopPrediction{
			Outcome: StopFuelLimited,
			MarginM: state.R.Norm() - radius,
		}, true
	}
	mdot := thrustFull / (ispSec * stdGravityMps2)

	dt := stopSubStepSeconds
	steps := int(math.Ceil(total / dt))
	if steps > stopMaxSubSteps {
		steps = stopMaxSubSteps
		dt = total / float64(steps)
	}
	if steps < 1 {
		return PoweredStopPrediction{}, false
	}

	var crossedGround bool
	var impactSpeed float64
	elapsed := 0.0
	dvUsed := 0.0

	for i := 0; i < steps; i++ {
		speedPre := physics.AirRelativeVelocity(state.R, state.V, primary).Norm()
		aAvail := thrustFull / massKg
		stepDt := stopStepSizeSeconds(dt, fuelKg/mdot, speedPre, aAvail)
		massSnap := massKg
		accelFn := func(r, v orbital.Vec3, _ float64) orbital.Vec3 {
			return poweredStopAccel(r, v, mu, primary, bc, thrustFull, massSnap)
		}
		pre := state
		state = physics.StepRK4(state, stepDt, accelFn, 0)
		elapsed += stepDt
		burned := mdot * stepDt
		if burned > fuelKg {
			burned = fuelKg
		}
		fuelKg -= burned
		massKg -= burned
		dvUsed += (thrustFull / massSnap) * stepDt

		if !crossedGround && state.R.Norm() < radius {
			crossedGround = true
			hit, _ := bisectPoweredCrossing(pre, stepDt, accelFn, func(s physics.StateVector) bool {
				return s.R.Norm() < radius
			})
			impactSpeed = physics.AirRelativeVelocity(hit.R, hit.V, primary).Norm()
		}

		vRel := physics.AirRelativeVelocity(state.R, state.V, primary)
		if vRel.Norm() <= stopSpeedFloorMps {
			stopState, tau := bisectPoweredCrossing(pre, stepDt, accelFn, func(s physics.StateVector) bool {
				return physics.AirRelativeVelocity(s.R, s.V, primary).Norm() <= stopSpeedFloorMps
			})
			outcome := StopStopped
			if crossedGround {
				outcome = StopCrashed
			}
			return PoweredStopPrediction{
				Outcome:        outcome,
				MarginM:        stopState.R.Norm() - radius,
				ImpactSpeedMps: impactSpeed,
				ElapsedSec:     elapsed - stepDt + tau,
				DVUsedMps:      dvUsed,
			}, true
		}

		if fuelKg <= 1e-9 {
			outcome := StopFuelLimited
			if crossedGround {
				// Ground already breached — that already dominates the
				// verdict. What we can no longer do, having spent every
				// last kilogram, is keep searching for the natural
				// (below-ground) zero-speed point StopCrashed's MarginM
				// documents; best effort is wherever the integration
				// stands at this instant.
				outcome = StopCrashed
			}
			return PoweredStopPrediction{
				Outcome:        outcome,
				MarginM:        state.R.Norm() - radius,
				ImpactSpeedMps: impactSpeed,
				ElapsedSec:     elapsed,
				DVUsedMps:      dvUsed,
			}, true
		}
	}
	return PoweredStopPrediction{}, false
}

// BurnAtCue is the "burn at" row (issue #377 §2): the latest point on
// the current ballistic coast from which starting a PredictPoweredStop
// burn still lands with non-negative margin — the standard suicide-burn
// boundary. AltitudeM / InSec describe that point.
type BurnAtCue struct {
	AltitudeM float64
	InSec     float64
}

// burnAtRefineIters bounds PredictBurnAt's continuous refinement once
// the coarse sample-index bisection has bracketed the crossing between
// two adjacent forwardBallisticPath samples. Each iteration LERPs a
// candidate state between the bracket's two ALREADY-COMPUTED samples —
// "use those samples as candidate start states instead of
// re-propagating" (issue #377 §2) — and spends one more
// predictPoweredStopFrom call testing it.
const burnAtRefineIters = 3

// lerpBallisticSample linearly interpolates between two adjacent
// ballistic samples. Not a re-propagation: over one sample interval
// (a small fraction of the coast, per forwardBallisticPath's sampling)
// the ballistic R/V curve is close enough to straight that a lerp is a
// good candidate state to test, and PredictBurnAt's bisection only ever
// needs a candidate to TEST — the final answer's precision comes from
// how many bisection rounds ran, not from this interpolation being
// exact.
func lerpBallisticSample(a, b ballisticSample, frac float64) ballisticSample {
	return ballisticSample{
		state: physics.StateVector{
			R: a.state.R.Add(b.state.R.Sub(a.state.R).Scale(frac)),
			V: a.state.V.Add(b.state.V.Sub(a.state.V).Scale(frac)),
			M: a.state.M,
		},
		elapsedSec: a.elapsedSec + (b.elapsedSec-a.elapsedSec)*frac,
	}
}

// PredictBurnAt finds the latest safe stop-burn start on the craft's
// current ballistic coast (issue #377 §2). Stop margin is monotone in
// start time (start later, stop lower), so this bisects rather than
// scanning: forwardBallisticPath already samples the whole coast to draw
// the dashed arc, and those samples are the candidate start states here
// too — no second propagator, no re-walking the coast from scratch.
//
// ok=false when there is no future safe start to cue: either the coast
// never finds ground contact within horizon (forwardBallisticPath itself
// refuses), or the CURRENT instant is already unsafe — the corridor's own
// Margin.State (CAN'T STOP) already carries that alarm, and a "burn at:
// Xs ago" row would only muddy it.
func PredictBurnAt(c *spacecraft.Spacecraft, horizon time.Duration) (BurnAtCue, bool) {
	if c == nil {
		return BurnAtCue{}, false
	}
	fc, ok := forwardBallisticPath(c, horizon)
	if !ok || len(fc.samples) < 2 {
		return BurnAtCue{}, false
	}
	radius := c.Primary.RadiusMeters()

	safeAt := func(s ballisticSample) bool {
		remaining := horizon - time.Duration(s.elapsedSec*float64(time.Second))
		if remaining <= 0 {
			return false
		}
		pred, ok := predictPoweredStopFrom(s.state, c, remaining)
		return ok && pred.Outcome == StopStopped && pred.MarginM >= 0
	}

	if !safeAt(fc.samples[0]) {
		return BurnAtCue{}, false
	}

	loIdx, hiIdx := 0, len(fc.samples)-1
	if safeAt(fc.samples[hiIdx]) {
		// Even the very end of the sampled coast is still stoppable (a
		// shallow / slow descent) — best effort: the latest candidate we
		// actually have.
		last := fc.samples[hiIdx]
		return BurnAtCue{AltitudeM: last.state.R.Norm() - radius, InSec: last.elapsedSec}, true
	}
	for hiIdx-loIdx > 1 {
		mid := (loIdx + hiIdx) / 2
		if safeAt(fc.samples[mid]) {
			loIdx = mid
		} else {
			hiIdx = mid
		}
	}

	lo, hi := fc.samples[loIdx], fc.samples[hiIdx]
	loFrac, hiFrac := 0.0, 1.0
	for i := 0; i < burnAtRefineIters; i++ {
		mid := (loFrac + hiFrac) / 2
		if safeAt(lerpBallisticSample(lo, hi, mid)) {
			loFrac = mid
		} else {
			hiFrac = mid
		}
	}
	best := lerpBallisticSample(lo, hi, loFrac)
	return BurnAtCue{AltitudeM: best.state.R.Norm() - radius, InSec: best.elapsedSec}, true
}

// marginTightAltitudeFrac is the fraction of CURRENT altitude below
// which a StopStopped margin still reads TIGHT rather than OK — issue
// #377 §4's suggested "~10% of current altitude".
const marginTightAltitudeFrac = 0.10

// burnAtImminentSec is how soon the "burn at" cue can be before a
// StopStopped margin promotes to TIGHT on that grounds alone — issue
// #377 §4's "or the start cue is inside a few seconds".
const burnAtImminentSec = 5.0

// DeriveMarginState maps a PredictPoweredStop result (plus, when
// available, the PredictBurnAt cue) onto the OK / TIGHT / CAN'T STOP
// alarm ladder the surface view's arc colour and alarm
// (screens/launch.go) key off (issue #377 §4). Pure and cheap — no
// integration here, so callers can call this every frame straight off a
// CACHED Stop/BurnAt pair without re-triggering the expensive forecasts.
//
//   - StopUndetermined (stopOK false): the integration couldn't resolve
//     within its step budget. Read as CAN'T STOP, not "unknown" — an
//     instrument that flatters the pilot on "I don't know" is worse than
//     none (BurnMargin's own doc comment makes the same call for its
//     degenerate case).
//   - StopCrashed / StopFuelLimited: CAN'T STOP from THIS stage, full
//     stop. Limiter names which one bound it — thrust (ran into the
//     ground before killing the speed) or fuel (ran dry first) — the
//     terminal condition the integration actually hit, better evidence
//     than a two-ratio comparison ever was.
//   - StopStopped: OK, unless the margin is thin (< ~10% of current
//     altitude) or the latest safe start is only seconds away, either of
//     which promotes to TIGHT.
func DeriveMarginState(stop PoweredStopPrediction, stopOK bool, currentAltitudeM float64, burnAt BurnAtCue, hasBurnAt bool) BurnMargin {
	if !stopOK {
		return BurnMargin{State: MarginInsufficient, Limiter: LimitThrust}
	}
	switch stop.Outcome {
	case StopCrashed:
		return BurnMargin{State: MarginInsufficient, Limiter: LimitThrust, ReqDVMps: stop.DVUsedMps}
	case StopFuelLimited:
		return BurnMargin{State: MarginInsufficient, Limiter: LimitFuel, ReqDVMps: stop.DVUsedMps}
	case StopStopped:
		tight := hasBurnAt && burnAt.InSec >= 0 && burnAt.InSec < burnAtImminentSec
		if currentAltitudeM > 0 && stop.MarginM < currentAltitudeM*marginTightAltitudeFrac {
			tight = true
		}
		state := MarginOK
		if tight {
			state = MarginTight
		}
		return BurnMargin{State: state, Limiter: LimitNone, ReqDVMps: stop.DVUsedMps}
	}
	return BurnMargin{State: MarginNone}
}

// descentRateFloorMps is the surface-relative descent rate below which
// the corridor stands down. Above zero so a craft parked on a pad, or
// one the pilot has already brought to a hover, doesn't flicker the
// instruments on numerical dust.
const descentRateFloorMps = 0.1

// DescentCorridor is the surface view's descent instrument block:
// altitude, descent rate, time to impact, plus the impact forecast the
// arc and the ground marker are drawn from.
//
// HorizontalRateMps / FlightPathAngleDeg are the two readings the older
// airless-body DESCENT chip carried that the corridor's own four numbers
// don't imply. They live here so the surface view can show ONE block
// during a descent instead of two that disagree about sign conventions —
// the corridor says `descent: 40 m/s` where DESCENT said `v_vert: -40.0
// m/s`, and having both on screen at once was the duplication this
// resolves. FlightPathAngleDeg / HasFPA still render as their own `fpa`
// row alongside `burn at` / `stop margin` — issue #377's pinned mock
// sketched only the two new rows, not the whole block, so dropping
// `fpa` was read too literally the first time round.
//
// Stop / StopOK and BurnAt / HasBurnAt are DescentCorridorFor's OWN
// fields but are NOT populated by it — they are the expensive half
// (PredictPoweredStop / PredictBurnAt, issue #377) that callers are
// expected to cache (ADR 0017) and merge in themselves. See
// screens.LaunchView's descentStopCache / cachedDescentStop: it calls
// DescentCorridorFor for the cheap gate + numbers, then separately (and
// cached) fills these two fields plus Margin before handing the struct to
// the renderer. Margin is still String()able / colour-drivable exactly as
// before (MarginState / MarginLimiter survive unchanged) — only ITS
// source changed, from ComputeBurnMargin's frozen scalars to
// DeriveMarginState reading the integrated Stop/BurnAt result.
type DescentCorridor struct {
	AltitudeM      float64
	DescentRateMps float64 // surface-relative, positive = falling
	// HorizontalRateMps is the surface-relative speed ACROSS the ground —
	// the component the descent rate says nothing about, and the one that
	// decides a touchdown from a smear when the vertical rate is already
	// nulled.
	HorizontalRateMps float64
	// FlightPathAngleDeg is the surface-relative velocity's angle out of
	// the local horizontal: 0 = flying level, −90 = straight down.
	// HasFPA is false below the speed floor where the angle is numerical
	// dust rather than a heading.
	FlightPathAngleDeg float64
	HasFPA             bool
	Impact             ImpactPrediction

	// Stop / StopOK: PredictPoweredStop's result for the CURRENT state —
	// not populated by DescentCorridorFor (see the type doc). StopOK
	// false means the integration hit its step cap without resolving
	// (refused, not a definite answer).
	Stop   PoweredStopPrediction
	StopOK bool
	// BurnAt / HasBurnAt: PredictBurnAt's "latest safe start" cue — not
	// populated by DescentCorridorFor (see the type doc). HasBurnAt is
	// false both when no future start is safe (already past the point of
	// no return — StopOK's own Margin.State already carries that alarm)
	// and, by convention, once the burn is under way (issue #377: "burn
	// at hides once the burn is under way").
	BurnAt    BurnAtCue
	HasBurnAt bool
	Margin    BurnMargin
}

// fpaSpeedFloorMps is the surface-relative speed below which the flight
// path angle stops meaning anything — at a metre per second of total
// motion the ratio of two near-zero components is noise, not a heading.
// Carried over verbatim from the DESCENT chip's own threshold so the
// folded rows read identically to the ones they replace.
const fpaSpeedFloorMps = 1.0

// DescentCorridorFor builds the CHEAP half of the corridor for a craft,
// or reports ok=false when the craft isn't in a descent worth
// instrumenting. The gate is deliberately the FORECAST, not the phase:
// instruments appear exactly when the current trajectory reaches the
// ground inside the horizon while the craft is falling. That keeps them
// off an ascent (climbing, so no descent rate) and off a stable parking
// orbit (falling toward periapsis, but never reaching the surface), with
// no separate phase predicate to drift out of step with the picture on
// the canvas.
//
// This does NOT compute Stop / BurnAt / Margin (issue #377): those need
// PredictPoweredStop and PredictBurnAt, each up to ~1000 RK4 sub-steps
// (and the burn-at search multiplies that ~8-10x), so running them here
// would make this function exactly the kind of per-frame cost ADR 0017 /
// #363 exist to keep off the render path. Callers that need the full
// block (screens.LaunchView) call this for the cheap gate + numbers, then
// separately — and CACHED — call PredictPoweredStop / PredictBurnAt /
// DeriveMarginState and merge the result in. Callers that only need the
// gate (sim.updateLaunchHint's `descending` check) pay nothing extra at
// all: the returned Stop/BurnAt/Margin fields are simply left zero-valued.
func DescentCorridorFor(c *spacecraft.Spacecraft, horizon time.Duration) (DescentCorridor, bool) {
	k, ok := descentKinematics(c)
	if !ok {
		return DescentCorridor{}, false
	}
	impact, ok := PredictImpact(c, horizon)
	if !ok {
		return DescentCorridor{}, false
	}
	return DescentCorridor{
		AltitudeM:          k.altM,
		DescentRateMps:     k.descentRate,
		HorizontalRateMps:  k.horizRate,
		FlightPathAngleDeg: k.fpaDeg,
		HasFPA:             k.hasFPA,
		Impact:             impact,
	}, true
}

// descentKinematics is DescentCorridorFor's CHEAP half: everything
// derivable from the craft's current state alone, with no forward
// propagation. Split out so callers that only need "is this thing
// falling?" — the DESCENDING hint's crossing state machine — can ask
// without paying for an impact forecast they'd throw away. ok=false on
// exactly the states DescentCorridorFor has always refused before it
// reaches PredictImpact.
type descentKinematicsResult struct {
	altM        float64
	descentRate float64
	horizRate   float64
	fpaDeg      float64
	hasFPA      bool
	gLocal      float64
}

func descentKinematics(c *spacecraft.Spacecraft) (descentKinematicsResult, bool) {
	if c == nil || c.Landed || c.Crashed {
		return descentKinematicsResult{}, false
	}
	mu := c.Primary.GravitationalParameter()
	if mu <= 0 || c.Primary.RadiusMeters() <= 0 {
		return descentKinematicsResult{}, false
	}
	rNorm := c.State.R.Norm()
	alt := c.Altitude()
	if rNorm <= 0 || alt <= 0 {
		return descentKinematicsResult{}, false
	}
	rHat := c.State.R.Scale(1 / rNorm)
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	vVert := vRel.Dot(rHat)
	descentRate := -vVert
	if !(descentRate >= descentRateFloorMps) {
		return descentKinematicsResult{}, false
	}
	horiz := vRel.Sub(rHat.Scale(vVert)).Norm()
	res := descentKinematicsResult{
		altM:        alt,
		descentRate: descentRate,
		horizRate:   horiz,
		gLocal:      mu / (rNorm * rNorm),
	}
	if vRel.Norm() > fpaSpeedFloorMps {
		res.fpaDeg = math.Atan2(vVert, horiz) * 180 / math.Pi
		res.hasFPA = true
	}
	return res, true
}
