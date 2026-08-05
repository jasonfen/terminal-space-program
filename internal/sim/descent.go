// Package sim — the descent half of the surface view (issue #348 §3,
// ADR 0043). Two pure forecasts the launch/surface screen renders:
// where the current trajectory meets the ground (PredictImpact) and
// whether the stack can still stop before it gets there
// (DescentCorridorFor / ComputeBurnMargin).
//
// The forecast reuses the predictor's ONE propagator — predictStep,
// analytic Kepler where the arc is Kepler-eligible and drag-aware Verlet
// everywhere else — so the drawn descent arc agrees with the orbit map's
// dashed trajectory AND with the craft's actual flight. No second
// propagator lives here (the #66 two-drift-sites lesson: when the same
// curve is integrated at two sites they drift apart and only one of them
// gets fixed).
//
// Both forecasts are ballistic-from-now: they propagate the CURRENT
// state without assuming any future thrust, exactly like the orbit map's
// projected orbit. That is what makes the impact marker live under a
// burn — every tick the burn reshapes the state, and the next frame's
// forecast is taken from the reshaped state, so the marker slides along
// the ground as the player thrusts.

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
	if c == nil {
		return ImpactPrediction{}, false
	}
	primary := c.Primary
	radius := primary.RadiusMeters()
	mu := primary.GravitationalParameter()
	total := horizon.Seconds()
	if radius <= 0 || mu <= 0 || total <= 0 {
		return ImpactPrediction{}, false
	}
	state := c.State
	if r := state.R.Norm(); r <= radius || math.IsNaN(r) {
		return ImpactPrediction{}, false
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
		return ImpactPrediction{}, false
	}
	sampleEvery := steps / impactPathSamples
	if sampleEvery < 1 {
		sampleEvery = 1
	}

	path := make([]orbital.Vec3, 0, impactPathSamples+2)
	path = append(path, state.R)
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
			return ImpactPrediction{
				Point:        point,
				Path:         path,
				TimeToImpact: time.Duration(elapsed * float64(time.Second)),
				SpeedMps:     physics.AirRelativeVelocity(hit.R, hit.V, primary).Norm(),
			}, true
		}
		elapsed += dt
		if (i+1)%sampleEvery == 0 {
			path = append(path, state.R)
		}
	}
	return ImpactPrediction{}, false
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
// Both are the idealised constant-mass, straight-down bound: no attitude
// error, no throttle ramp, no mass shed mid-burn. That is deliberate —
// an instrument that flatters the pilot is worse than none, and this one
// errs optimistic only in the mass term (a real burn gets lighter, so a
// little easier), which the TIGHT band exists to cover.
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

// descentRateFloorMps is the surface-relative descent rate below which
// the corridor stands down. Above zero so a craft parked on a pad, or
// one the pilot has already brought to a hover, doesn't flicker the
// instruments on numerical dust.
const descentRateFloorMps = 0.1

// DescentCorridor is the surface view's descent instrument block:
// altitude, descent rate, time to impact, and the burn margin, plus the
// impact forecast the arc and the ground marker are drawn from.
type DescentCorridor struct {
	AltitudeM      float64
	DescentRateMps float64 // surface-relative, positive = falling
	Impact         ImpactPrediction
	Margin         BurnMargin
}

// DescentCorridorFor builds the corridor for a craft, or reports
// ok=false when the craft isn't in a descent worth instrumenting. The
// gate is deliberately the FORECAST, not the phase: instruments appear
// exactly when the current trajectory reaches the ground inside the
// horizon while the craft is falling. That keeps them off an ascent
// (climbing, so no descent rate) and off a stable parking orbit (falling
// toward periapsis, but never reaching the surface), with no separate
// phase predicate to drift out of step with the picture on the canvas.
func DescentCorridorFor(c *spacecraft.Spacecraft, horizon time.Duration) (DescentCorridor, bool) {
	if c == nil || c.Landed || c.Crashed {
		return DescentCorridor{}, false
	}
	mu := c.Primary.GravitationalParameter()
	if mu <= 0 || c.Primary.RadiusMeters() <= 0 {
		return DescentCorridor{}, false
	}
	rNorm := c.State.R.Norm()
	alt := c.Altitude()
	if rNorm <= 0 || alt <= 0 {
		return DescentCorridor{}, false
	}
	rHat := c.State.R.Scale(1 / rNorm)
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	descentRate := -vRel.Dot(rHat)
	if !(descentRate >= descentRateFloorMps) {
		return DescentCorridor{}, false
	}
	impact, ok := PredictImpact(c, horizon)
	if !ok {
		return DescentCorridor{}, false
	}
	return DescentCorridor{
		AltitudeM:      alt,
		DescentRateMps: descentRate,
		Impact:         impact,
		Margin: ComputeBurnMargin(
			c.Thrust,
			c.TotalMass(),
			mu/(rNorm*rNorm),
			descentRate,
			alt,
			c.RemainingDeltaV(),
		),
	}, true
}
