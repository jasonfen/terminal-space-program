// Package sim - the Site+ landing autopilot (ADR 0067 decisions 1, 5, 10). One-
// line reason for a new file: a new guidance law over the vessel, not a slice
// of maneuver.go. It is a per-vessel state (spacecraft.LandingAutopilot)
// ticked from executeDueNodes' loop for every vessel, active or not: it lights
// the reserve-throttle brake at the burn-at cue, steers the stop point onto the
// aim, nulls the drift and flies the descent rate down to contact.

package sim

import (
	"errors"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Law internals, not feel numbers: the gains of the loops below. A playtest
// that wants a different landing changes landing_tuning.go; these only keep the
// loops stable.
const (
	apStopHorizon     = 20 * time.Minute // brake forecast horizon (the Lander's brake is ~9 min)
	apNewtonGain      = 0.5              // damped Newton step on the downrange error
	apCrossGain       = 0.8              // fraction of the crossrange error tilted away per tick
	apMinStopLenM     = 1000.0           // floor on the stopping distance the errors are scaled by
	apMarginTargetM   = 120.0            // stop altitude the brake holds: the final phase's schedule (top at 100 m) takes over from here
	apProbeTheta      = 0.02             // finite-difference probes for the 2x2 Newton step
	apProbePitch      = 0.02
	apStepDamp        = 0.5
	apMaxStepTheta    = 0.05 // per tick
	apMaxStepPitchRad = 0.05
	apMaxPitchUpRad   = 60 * math.Pi / 180
	apMaxCrossTiltRad = 0.25 // ~14 degrees: reaches ~10% of the stopping distance across (measured)
	apRateGain        = 0.5  // 1/s, descent-rate error to acceleration
	apDriftGain       = 0.4  // 1/s, drift to acceleration
	apCloseGain       = 0.05 // 1/s, position error to closing speed in final
	apMaxTiltRad      = 25 * math.Pi / 180
	apTouchdownCutM   = 0.5  // under this altitude the engine is cut
	apDecelDerate     = 0.5  // share of the net deceleration the descent profile asks for
	apRodMinCosine    = 0.3  // floor on thrust-direction . up when holding a descent rate under ROD
	apCueNearSec      = 5.0  // cue closer than this is re-solved every tick
	apCueFarMaxSec    = 60.0 // and a far one at most this long apart
	apNoPathRecheck   = 30 * time.Second
	apThrottleEpsilon = 0.005
)

// Refusals for q in [SITE] (decision 10), shown as `site hold: <reason>`. N's own
// lines (ErrDeorbit*) are reused verbatim; these are the autopilot's.
var (
	ErrSiteHoldUnderWeight = errors.New("thrust under weight")
	ErrSiteHoldCannotStop  = errors.New("cannot stop at reserve throttle")
)

// SiteHoldReachError is the "site out of reach" refusal: the reserve-throttle
// stop lands further off the aim than the brake's steering reaches. AlongM is
// signed (+ long, - short), CrossM signed (+ right).
type SiteHoldReachError struct{ AlongM, CrossM float64 }

func (e *SiteHoldReachError) Error() string { return "out of reach" }

// LandingEvent is the Event Flash for a landing the pilot may not have watched
// (decision 5): touchdown, out-of-reach landing, crash, with the true distance.
type LandingEvent struct {
	CraftName  string
	SiteName   string
	DistanceM  float64
	Crashed    bool
	OutOfReach bool
	Update     bool // P20: the ground moved the computer's site; DistanceM is how far
	Background bool // the vessel was not the active one when it happened
}

// apRuntime is the autopilot's transient cue cache for one vessel.
type apRuntime struct {
	checked    bool
	burning    bool
	ground     bool  // the coast meets the ground in the horizon
	late       bool  // ...and the cue has already passed
	energy     int64 // the orbit the no-path answer was solved for (qz of specific energy)
	cueAt      time.Time
	nextCheck  time.Time
	outOfReach bool    // the last brake tick saturated: remembered for the event
	cueErrM    float64 // a chute worked to the light: how far the cue's stop lands from the aim
	cueAltM    float64 // the height of the cue, and the speed down there, for the aim allowance
	cueDownMps float64
	// navSite is the site key the computer's-site error was drawn for (transient:
	// a reload keeps the saved offset).
	navSite  uint64
	navKnown bool
}

func (w *World) apRuntimeFor(c *spacecraft.Spacecraft) *apRuntime {
	if w.apRT == nil {
		w.apRT = map[uint64]*apRuntime{}
	}
	rt := w.apRT[c.ID]
	if rt == nil {
		rt = &apRuntime{}
		w.apRT[c.ID] = rt
	}
	return rt
}

// apTarget resolves the vessel's own aim to a surface point: a site through
// its own Target, so a vessel nobody is flying keeps its aim; a landed vessel
// or ghost aim only for the active vessel (ADR 0067 later slices).
func (w *World) apTarget(c *spacecraft.Spacecraft) (lat, lon float64, name string, ok bool) {
	t := c.Target
	active := w.ActiveCraft() == c
	if active {
		t = w.Target
	}
	if t.Kind == TargetSite {
		s, found := w.landingSiteOf(t, c)
		if !found || s.Body != c.Primary.EnglishName {
			return 0, 0, "", false
		}
		sys := w.systemForCraft(c)
		if s.System != sys.Name {
			return 0, 0, "", false
		}
		return s.LatDeg, s.LonDeg, s.Name, true
	}
	if active {
		lat, lon, ok = w.surfaceTargetLatLon(c)
		return lat, lon, w.TargetName(), ok
	}
	return 0, 0, "", false
}

// apFrame is the local geometry every phase reads.
type apFrame struct {
	up            orbital.Vec3
	vRel, vh      orbital.Vec3 // ground-relative velocity and its horizontal part
	vz, sh        float64      // vertical rate (+ up) and ground speed
	alt, g        float64
	mass, aMax    float64 // aMax: full-throttle acceleration
	hoverThrottle float64
}

func apFrameOf(c *spacecraft.Spacecraft) apFrame {
	r := c.State.R
	f := apFrame{up: r.Unit(), alt: r.Norm() - c.Primary.RadiusMeters()}
	f.vRel = physics.AirRelativeVelocity(r, c.State.V, c.Primary)
	f.vz = f.vRel.Dot(f.up)
	f.vh = f.vRel.Sub(f.up.Scale(f.vz))
	f.sh = f.vh.Norm()
	f.g = c.Primary.GravitationalParameter() / (r.Norm() * r.Norm())
	f.mass = c.TotalMass()
	if f.mass > 0 {
		f.aMax = c.Thrust / f.mass
		if f.aMax > 0 {
			f.hoverThrottle = f.g / f.aMax
		}
	}
	return f
}

// retro is the surface-retrograde unit direction (the brake's nose), or local
// up when the vessel is not moving over the ground.
func (f apFrame) retro() orbital.Vec3 {
	if n := f.vRel.Norm(); n > 1e-6 {
		return f.vRel.Scale(-1 / n)
	}
	return f.up
}

// apStopErr is where a stop at stopR lands against the aim at tStop: signed
// metres along the track (+ long) and across it (+ right), with the track the
// ground-relative orbit plane through the start state. The aim is the real
// site plus the computer's navigation error and the pilot's aim offsets.
func (w *World) apStopErr(c *spacecraft.Spacecraft, lat, lon float64, start physics.StateVector, stopR orbital.Vec3, tStop time.Time) (along, cross float64) {
	n := apTrackNormal(c, start)
	if n.Norm() == 0 {
		return 0, 0
	}
	aimHat := siteVec(c, lat, lon, tStop)
	tHat := n.Cross(aimHat).Unit()
	rHat := tHat.Cross(aimHat)
	d := stopR.Sub(aimHat.Scale(c.Primary.RadiusMeters()))
	ap := c.Landing
	off := tHat.Scale(ap.NavAlongM + ap.AimAlongM).Add(rHat.Scale(ap.NavAcrossM + ap.AimAcrossM))
	d = d.Sub(off)
	return d.Dot(tHat), d.Dot(rHat)
}

// apTrackNormal is the unit normal of the ground-relative orbit plane the
// aim's axes hang on: the one fixed at the brake's light once flying, else the
// plane of the given state.
func apTrackNormal(c *spacecraft.Spacecraft, start physics.StateVector) orbital.Vec3 {
	if ap := c.Landing; ap != nil && ap.TrackN.Norm() != 0 {
		return ap.TrackN
	}
	vRel := physics.AirRelativeVelocity(start.R, start.V, c.Primary)
	if n := start.R.Cross(vRel); n.Norm() != 0 {
		return n.Unit()
	}
	return orbital.Vec3{}
}

// landingCueIn is the sim time until the brake's cue (the latest instant a
// reserve-throttle brake still stops above the ground), cached: a far cue is
// re-solved rarely, a near one every tick. ground=false when the coast never
// meets the ground; late=true once the cue has passed.
func (w *World) landingCueIn(c *spacecraft.Spacecraft) (in time.Duration, ground, late bool) {
	c.OnLandingBrake(func() { in, ground, late = w.landingCueInOn(c) })
	return
}

func (w *World) landingCueInOn(c *spacecraft.Spacecraft) (in time.Duration, ground, late bool) {
	rt := w.apRuntimeFor(c)
	now := w.Clock.SimTime
	burning := c.ActiveBurn != nil
	// a burn nobody read the cue through (a planted one owns the engine) changes
	// the orbit: a cached no-path answer is stale the moment the energy moves
	energy := qz(c.State.V.Dot(c.State.V)/2-c.Primary.GravitationalParameter()/c.State.R.Norm(), 200)
	if !rt.checked || rt.burning != burning || !now.Before(rt.nextCheck) || (!rt.ground && rt.energy != energy) {
		rt.checked, rt.burning, rt.energy = true, burning, energy
		cue, g, ok := burnAtCue(c, deorbitHorizon, brakeThrottle(c), deorbitRefineIters)
		rt.ground, rt.late = g, g && !ok
		switch {
		case !g:
			rt.nextCheck = now.Add(apNoPathRecheck)
		case !ok:
			rt.cueAt, rt.nextCheck = now, now
		default:
			rt.cueAt = now.Add(time.Duration(cue.InSec * float64(time.Second)))
			if oneBurn(c) {
				rt.cueErrM = w.cueStopErrM(c, cue)
			}
			rt.cueAltM = cue.AltitudeM
			rt.cueDownMps = -physics.AirRelativeVelocity(cue.State.R, cue.State.V, c.Primary).Dot(cue.State.R.Unit())
			wait := cue.InSec / 4
			switch {
			case cue.InSec < apCueNearSec:
				wait = 0
			case wait > apCueFarMaxSec:
				wait = apCueFarMaxSec
			}
			rt.nextCheck = now.Add(time.Duration(wait * float64(time.Second)))
		}
	}
	return rt.cueAt.Sub(now), rt.ground, rt.late
}

// cueStopErrM is how far the brake lit at cue stops from the aim, metres.
func (w *World) cueStopErrM(c *spacecraft.Spacecraft, cue BurnAtCue) float64 {
	lat, lon, _, ok := w.apTarget(c)
	if !ok {
		return 0
	}
	pred, ok := predictPoweredStopFromAt(cue.State, c, deorbitHorizon-time.Duration(cue.InSec*float64(time.Second)), brakeThrottle(c))
	if !ok {
		return 0
	}
	tStop := w.Clock.SimTime.Add(time.Duration((cue.InSec + pred.ElapsedSec) * float64(time.Second)))
	return math.Hypot(w.apStopErr(c, lat, lon, cue.State, pred.StopR, tStop))
}

// chuteAimCut is the cut rule (ADR 0069 decision 8), the chute's half of "as
// late as possible": under an open canopy on a one-burn vessel the cut waits
// for the cue (the latest instant the brake still stops, which the caller
// tests), but not past the point the burn needs to put the touchdown on the
// aim. Under a canopy the vessel cannot steer, so the cue's stop error e stands
// until the cut; the one-burn law pulls e in with 6 e / tgo^2 sideways against
// a tilt limit of g tan(apMaxTiltRad), so it wants a burn of at least
// sqrt(6 e / (g tan(apMaxTiltRad))) seconds, lit that long before the cue.
func (w *World) chuteAimCut(c *spacecraft.Spacecraft, f apFrame, in time.Duration) bool {
	if (c.WorksChute() && c.ChuteState != spacecraft.ChuteDeployed) || !oneBurn(c) || f.g <= 0 || c.Isp <= 0 {
		return false
	}
	// seconds the burn needs to null the stop's error, and the most the tank
	// can hover for (a share of it) so the landing itself keeps its fuel
	need := math.Sqrt(6 * w.apRuntimeFor(c).cueErrM / (f.g * math.Tan(apMaxTiltRad)))
	if !c.WorksChute() {
		// no canopy: the brake lit at the cue has its own time to steer, tgo
		// = 2 h / (v + 2) at the cue (the aim is dropped under 3 s). A booster
		// lights kilometres up and has it; a Dragon falling at its terminal
		// speed lights 120 m up with 2 s, so it lights `need` early instead
		// (the cue's own height and speed: a booster falling from 30 km is faster
		// now than at the cue, and the current speed read 20 s early and 1.4 t of
		// propellant over the plan)
		rt := w.apRuntimeFor(c)
		if 2*rt.cueAltM/(math.Max(rt.cueDownMps, OneBurnContactMps)+OneBurnContactMps) >= need {
			return false
		}
	}
	// the law slows to the contact speed, so the burn runs mean speed is (v+2)/2, so the
	// burn lasts 2v/(v+2) times the lead
	v := math.Max(-f.vz, OneBurnContactMps)
	afford := ChuteAimFuelFrac * c.Isp * stdGravityMps2 * c.ActiveStageFuel() / (c.TotalMass() * f.g) * (v + OneBurnContactMps) / (2 * v)
	return need <= afford && in <= time.Duration(need*float64(time.Second))
}

// landingAutopilotWarpLimit is the warp ceiling the engaged autopilots impose
// (decision 5): AutopilotWarpCap once one is burning, and a glide down toward
// the cue while one coasts to it. ok=false when none constrains warp.
func (w *World) landingAutopilotWarpLimit() (float64, bool) {
	lim, ok := 0.0, false
	take := func(v float64) {
		if !ok || v < lim {
			lim, ok = v, true
		}
	}
	for _, c := range w.Crafts {
		if c == nil || !c.Landing.Owns() || c.Landed || c.Crashed {
			continue
		}
		if c.Landing.Phase != spacecraft.PhaseCoast {
			take(AutopilotWarpCap)
			continue
		}
		if c.ActiveBurn != nil || landingNodePending(c) {
			continue // the planted burn's own cap governs, and a planted one ramps warp down to itself
		}
		in, ground, late := w.landingCueIn(c)
		if !ground {
			continue
		}
		if late {
			take(1)
			continue
		}
		maxWarp := in.Seconds() / (approachClampSubsteps * w.Clock.BaseStep.Seconds())
		if maxWarp < 1 {
			maxWarp = 1
		}
		take(maxWarp)
	}
	return lim, ok
}

// EngageLandingAutopilot is q in [SITE] on the active vessel (decision 5).
// The refusals are facts (decision 10): N's lines, thrust under weight, and the
// signed shortfall when the reserve-throttle stop is further off the aim than
// the brake can steer.
func (w *World) EngageLandingAutopilot() (err error) {
	c := w.ActiveCraft()
	if c == nil {
		return errors.New("no vessel")
	}
	c.OnLandingBrake(func() { err = w.engageLandingAutopilot(c) })
	return err
}

func (w *World) engageLandingAutopilot(c *spacecraft.Spacecraft) error {
	if c.Landed {
		return ErrDeorbitLanded
	}
	if _, err := w.claimLZ(c); err != nil {
		return err
	}
	if ap := c.Landing; ap.Owns() {
		// already flying: take the throttle back and keep the phase, whatever the
		// link or the target is doing
		ap.ROD, ap.ThrottleHeld = false, false
		return nil
	}
	lat, lon, _, ok := w.apTarget(c)
	if !ok {
		if w.Target.Kind == TargetSite {
			return ErrDeorbitOtherBody
		}
		return ErrDeorbitNoSite
	}
	if !w.canCommand(c) {
		return ErrDeorbitNoSignal
	}
	if c.Thrust <= 0 || c.Isp <= 0 {
		return ErrDeorbitNoEngine
	}
	// a planted boostback was solved on the lighter, post-burn stage, and judged
	// the fall it leaves; the reach test and the weight test read the fall before it
	boostback := boostbackPending(c)
	if !boostback && !canBrakeAtSite(c) {
		return ErrSiteHoldUnderWeight
	}
	w.drawNavError(c, false) // a vessel that never pressed N draws at q
	ap := c.Landing
	if ap == nil {
		ap = &spacecraft.LandingAutopilot{}
	}
	wasNose := ap.Engaged && ap.Nose
	ap.Engaged, ap.Nose, ap.ROD, ap.ThrottleHeld = true, false, false, false
	f := apFrameOf(c)
	switch {
	case wasNose:
		// N over the nose hold keeps the phase it reached
	case boostback:
		ap.Phase = spacecraft.PhaseCoast // the boostback flies first; a dropped stage's idle manual burn is not a brake
	case f.sh < FinalPhaseGroundSpeedMps && !(oneBurn(c) && c.ManualBurn == nil):
		ap.Phase = spacecraft.PhaseFinal // a hover: nothing to brake (a one-burn vessel falling straight down waits for its cue)
	case c.ManualBurn != nil:
		ap.Phase = spacecraft.PhaseBrake // an engage mid-brake takes the controls where it stands
	default:
		ap.Phase = spacecraft.PhaseCoast
	}
	c.Landing = ap
	if ap.Phase == spacecraft.PhaseFinal && ap.TrackN.Norm() == 0 {
		ap.TrackN = apTrackNormal(c, c.State) // a hover has no brake light to fix the aim's plane; zero drift leaves it unset
	}
	if ap.Phase != spacecraft.PhaseFinal && !boostback {
		if err := w.apReachTest(c, lat, lon); err != nil {
			ap.Engaged, ap.Nose = wasNose, wasNose
			return err
		}
	}
	c.AttitudeMode = spacecraft.BurnSite
	c.SiteDir = apFrameOf(c).retro()
	w.apRuntimeFor(c).checked = false
	return nil
}

// EngageLandingNoseHold is q in [SITE] (fix E, P7): the guidance nose hold.
// The law aims the nose for the phase; the engine, throttle and warp stay the
// pilot's. Refusals are N's target / link lines only.
func (w *World) EngageLandingNoseHold() error {
	c := w.ActiveCraft()
	if c == nil {
		return errors.New("no vessel")
	}
	if c.Landed {
		return ErrDeorbitLanded
	}
	if _, err := w.claimLZ(c); err != nil {
		return err
	}
	if ap := c.Landing; ap != nil && ap.Engaged {
		if !ap.Nose {
			ap.ROD, ap.ThrottleHeld = false, false // under the autopilot q is the old re-assert
		}
		return nil
	}
	if _, _, _, ok := w.apTarget(c); !ok {
		if w.Target.Kind == TargetSite {
			return ErrDeorbitOtherBody
		}
		return ErrDeorbitNoSite
	}
	if !w.canCommand(c) {
		return ErrDeorbitNoSignal
	}
	w.drawNavError(c, false)
	ap := &spacecraft.LandingAutopilot{Engaged: true, Nose: true}
	if old := c.Landing; old != nil { // keep the aim and the computer's-site error
		ap.AimAlongM, ap.AimAcrossM = old.AimAlongM, old.AimAcrossM
		ap.NavAlongM, ap.NavAcrossM, ap.NavSeeded, ap.NavFixed = old.NavAlongM, old.NavAcrossM, old.NavSeeded, old.NavFixed
	}
	c.Landing = ap
	c.AttitudeMode = spacecraft.BurnSite
	c.SiteDir = apFrameOf(c).retro()
	return nil
}

// PlanDeorbitAndEngage is N on a site (playtest P3, fix E): in orbit, plant the
// deorbit burn, engage Site+ and put the navball on SITE. Already descending
// onto the site (the plan refuses as falling) it engages from here with no new
// burn and returns a nil plan and nil error; an engage refusal then comes back
// as the error with a nil plan. The clock stays the player's. When the plan
// itself is refused nothing engages (nil plan); when only the engage is
// refused the plan stays planted and is returned with the error.
func (w *World) PlanDeorbitAndEngage() (*DeorbitPlan, error) {
	c := w.ActiveCraft()
	fresh, err := w.claimLZ(c) // before the plan, so the burn aims at the claimed LZ
	if err != nil {
		return nil, err
	}
	// ADR 0069 decision 8: on a chute vessel with landing engines N arms the chute
	// before the plan, so the plan and the cue fly the canopy. A capsule that
	// cannot brake (no landing engine) keeps the plain deorbit burn.
	armed := armLandingChute(c)
	plan, err := w.planDeorbitAndEngage()
	if armed && !c.Landing.Owns() {
		c.ChuteState = spacecraft.ChuteStowed // N did not engage: the chute is as it was
	}
	if plan == nil && err != nil && fresh {
		c.LZClaim = spacecraft.LZClaim{} // nothing planted, nothing engaged
	}
	return plan, err
}

func (w *World) planDeorbitAndEngage() (plan *DeorbitPlan, err error) {
	// the deorbit burn is N's own and flies on the landing engines (see UsesSuperDraco)
	w.ActiveCraft().OnLandingBrake(func() { plan, err = w.PlanDeorbitOntoSite() })
	if errors.Is(err, ErrDeorbitFalling) {
		if err := w.EngageLandingAutopilot(); err != nil {
			return nil, err
		}
		w.setNavMode(NavSite)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := w.EngageLandingAutopilot(); err != nil {
		return plan, err
	}
	w.setNavMode(NavSite)
	return plan, nil
}

// apReachTest is the pre-engage test (decision 10): the reserve-throttle stop,
// lit at the cue (or from here once braking), must land within the brake's
// steering authority of the aim. No descent yet (a circular orbit awaiting N)
// is not a refusal: the autopilot waits.
func (w *World) apReachTest(c *spacecraft.Spacecraft, lat, lon float64) error {
	start, lead := c.State, 0.0
	if c.ManualBurn == nil {
		cue, ground, ok := burnAtCue(c, deorbitHorizon, brakeThrottle(c), deorbitRefineIters)
		if !ground {
			return nil
		}
		if !ok {
			return ErrSiteHoldCannotStop
		}
		start, lead = cue.State, cue.InSec
	}
	pred, ok := predictPoweredStopFromAt(start, c, apStopHorizon, brakeThrottle(c))
	if !ok || pred.Outcome != StopStopped || pred.MarginM < 0 {
		return ErrSiteHoldCannotStop
	}
	tStop := w.Clock.SimTime.Add(time.Duration((lead + pred.ElapsedSec) * float64(time.Second)))
	along, cross := w.apStopErr(c, lat, lon, start, pred.StopR, tStop)
	span := math.Max(pred.StopR.Sub(start.R).Norm(), apMinStopLenM)
	if math.Abs(along) > OutOfReachAuthorityFrac*span || math.Abs(cross) > OutOfReachAuthorityFrac*span {
		return &SiteHoldReachError{AlongM: along, CrossM: cross}
	}
	return nil
}

// DisengageLandingAutopilot hands the vessel back (a hold key took the nose).
func (w *World) DisengageLandingAutopilot(c *spacecraft.Spacecraft) {
	if c != nil && c.Landing != nil {
		if c.Landing.Engaged {
			c.LZClaim = spacecraft.LZClaim{} // Site+ off frees the LZ (decision 5)
		}
		c.Landing.Engaged, c.Landing.ROD, c.Landing.ThrottleHeld = false, false, false
	}
}

// LandingTakeEngine is b under an engaged Site+ (playtest P1): the pilot flies
// the engine, so the autopilot goes off and the nose goes to surface retrograde,
// as a hold key does. Reports whether it was engaged.
func (w *World) LandingTakeEngine() bool {
	c := w.ActiveCraft()
	if c == nil || !c.Landing.Owns() {
		return false
	}
	w.DisengageLandingAutopilot(c)
	w.SetAttitudeMode(spacecraft.BurnSurfaceRetrograde)
	return true
}

// LandingROD is e/q under the autopilot (decision 5, P66): step the descent
// rate the throttle holds, starting at the rate the vessel is falling. delta is
// +/- RODStepMps (or RODFineStepMps); + slows the descent. Reports whether the
// key was consumed (an engaged autopilot on the active vessel).
func (w *World) LandingROD(delta float64) bool {
	c := w.ActiveCraft()
	if c == nil || !c.Landing.Owns() || c.Landed {
		return false
	}
	ap := c.Landing
	if oneBurn(c) {
		return false // ADR 0069 decision 10: a one-burn landing has no rate to click
	}
	if ap.Phase != spacecraft.PhaseApproach && ap.Phase != spacecraft.PhaseFinal {
		return false // P66 is a final-phase mode: in coast and brake the key is the raw throttle's
	}
	if !ap.ROD {
		ap.ROD, ap.ThrottleHeld = true, false
		ap.RODRateMps = apFrameOf(c).vz
	}
	ap.RODRateMps += delta
	if ap.RODRateMps > 0 {
		ap.RODRateMps = 0 // a descent rate: it never commands a climb
	}
	return true
}

// LandingHandOverThrottle is z/x under the autopilot: the raw throttle comes
// back, steering is kept. Reports whether the autopilot was engaged.
func (w *World) LandingHandOverThrottle() bool {
	c := w.ActiveCraft()
	if c == nil || !c.Landing.Owns() {
		return false
	}
	if c.Landing.Phase == spacecraft.PhaseCoast {
		return true // no throttle to hand over yet: latching it would freeze the brake at its light
	}
	c.Landing.ROD, c.Landing.ThrottleHeld = false, true
	return true
}

// tickLandingAutopilot runs one tick of c's autopilot, called for every vessel.
func (w *World) tickLandingAutopilot(c *spacecraft.Spacecraft) {
	w.dropForLanding(c) // outside the engine swap: staging re-indexes the stage it restores
	c.OnLandingBrake(func() { w.tickLandingAutopilotOn(c) })
}

// landingDrop: c carries stages below a landing stage that cannot land with
// them (a Dragon's Trunk). N flies its deorbit burn with them on and drops them
// once it is done (Jason 2026-11-12: the Crew Dragon order).
func landingDrop(c *spacecraft.Spacecraft) bool {
	n := len(c.Stages)
	return n > 1 && !c.CanSoftLand && c.Stages[n-1].CanSoftLand
}

// armLandingChute arms the chute N arms on a chute vessel with landing engines
// (ADR 0069 decision 8).
func armLandingChute(c *spacecraft.Spacecraft) bool {
	return c.HasParachute && c.CanSoftLand && c.Primary.Atmosphere != nil && !c.Landed && canBrakeAtSite(c) && c.ArmParachute()
}

// dropForLanding stages off what landingDrop names once the engaged
// autopilot's deorbit burn is flown, then arms the chute.
func (w *World) dropForLanding(c *spacecraft.Spacecraft) {
	ap := c.Landing
	if ap == nil || !ap.Engaged || ap.Phase != spacecraft.PhaseCoast || c.ActiveBurn != nil || !landingDrop(c) {
		return
	}
	if _, pending := deorbitNode(c); pending {
		return
	}
	for landingDrop(c) {
		if _, _, err := w.StageActive(w.craftIndex(c)); err != nil {
			return // no signal: try again next tick
		}
	}
	armLandingChute(c)
	w.apRuntimeFor(c).checked = false // a new stack: a cue the HUD read with the trunk on is stale
}

func (w *World) tickLandingAutopilotOn(c *spacecraft.Spacecraft) {
	ap := c.Landing
	if ap == nil {
		return
	}
	if c.Landed || c.Crashed {
		if !ap.Engaged {
			ap.ClearAttempt() // a hand landing ends the attempt too: no stale aim for the next N
			return
		}
		w.landingEnded(c)
		return
	}
	if !ap.Engaged {
		return
	}
	lat, lon, _, ok := w.apTarget(c)
	if !ok || c.ActiveStageFuel() <= 0 || c.Thrust <= 0 {
		ap.ForecastOK = false // a forecast the autopilot no longer refreshes must not keep reading as miss:/cross:
		return                // nothing to fly to, or nothing to fly with: keep the rest of the state, do nothing
	}
	ap.Reserve, ap.OutOfReach, ap.ForecastOK = false, false, false
	w.updateNavOnLink(c)
	f := apFrameOf(c)
	if ap.Phase == spacecraft.PhaseCoast {
		if c.ActiveBurn != nil {
			return // a planted burn owns the engine
		}
		c.AttitudeMode = spacecraft.BurnSite
		c.SiteDir = f.retro()
		if ap.Nose {
			// the nose hold waits for the pilot's engine (or a hover's slow ground speed)
			if c.ManualBurn == nil && f.sh >= FinalPhaseGroundSpeedMps {
				return
			}
			ap.Phase = spacecraft.PhaseBrake
			ap.TrackN, ap.StopSpanM, ap.PitchUpRad = orbital.Vec3{}, 0, 0
			ap.TrackN = apTrackNormal(c, c.State)
		} else {
			if landingNodePending(c) {
				return // N's burns come first: the fall to brake is the one after them (a boostback's pre-burn cue is not it)
			}
			in, ground, late := w.landingCueIn(c)
			if !(ground && w.chuteAimCut(c, f, in)) && (!ground || (!late && in.Seconds() > w.lastSimDelta.Seconds())) {
				return
			}
			if c.WorksChute() {
				c.ChuteState = spacecraft.ChuteCut // ADR 0069 decision 8: the canopy goes at the light
			}
			ap.Phase = spacecraft.PhaseBrake
			ap.TrackN, ap.StopSpanM, ap.PitchUpRad = orbital.Vec3{}, 0, 0
			ap.TrackN = apTrackNormal(c, c.State)
			w.apSetThrottle(c, brakeThrottle(c))
			c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
			c.DryOrder = false
		}
	}
	if !ap.Nose && c.ManualBurn == nil && c.EffectiveThrottle() > 0 {
		c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
	}
	c.AttitudeMode = spacecraft.BurnSite
	// phases only advance: brake, approach (path steeper than 45 degrees), final
	if ap.Phase == spacecraft.PhaseBrake && math.Abs(f.vz) > f.sh {
		ap.Phase = spacecraft.PhaseApproach
	}
	if ap.Phase != spacecraft.PhaseFinal && f.sh < FinalPhaseGroundSpeedMps {
		ap.Phase = spacecraft.PhaseFinal
	}
	var dir orbital.Vec3
	var theta float64
	if oneBurn(c) && !ap.Nose {
		dir, theta = w.apOneBurn(c, ap, f, lat, lon) // ADR 0069 decision 10: no schedule, no clicks
	} else if ap.Phase == spacecraft.PhaseFinal && ap.Nose {
		dir = f.up // P9: the hand nose holds straight up in final; no lean, no forecast
	} else if ap.Phase == spacecraft.PhaseFinal {
		dir, theta = w.apFinal(c, ap, f, lat, lon)
	} else {
		dir, theta = w.apBrake(c, ap, f, lat, lon)
	}
	c.SiteDir = dir
	if ap.Nose {
		return // the nose hold steers only: the engine is the pilot's
	}
	if ap.ROD && ap.Phase != spacecraft.PhaseFinal && !oneBurn(c) {
		theta = apRodThrottle(c, ap, f, dir)
	}
	if !ap.ThrottleHeld {
		w.apSetThrottle(c, theta)
	}
}

// apSetThrottle writes the core throttle without stamping the pilot's
// throttle-change clock (the autopilot changes it every tick; warp is capped
// separately while it burns).
func (w *World) apSetThrottle(c *spacecraft.Spacecraft, t float64) {
	switch {
	case t < throttleZeroEpsilon:
		t = 0
	case t > 1:
		t = 1
	}
	writeThrottle(c, ThrottleAll, t)
}

// apForecast is one stop forecast at (theta, pitchUp) from here: the stop
// point's signed offset from the aim along and across the track, the altitude
// margin, and the distance the stop is ahead.
type apForecast struct {
	along, cross, margin, span float64
	ok                         bool
}

func (w *World) apForecast(c *spacecraft.Spacecraft, lat, lon, theta, pitchUp float64) apForecast {
	pred, ok := predictPoweredStopTraced(c.State, c, apStopHorizon, theta, pitchUp, landingStopTrim(c), nil)
	if !ok || pred.StopR.Norm() == 0 || (pred.Outcome != StopStopped && pred.Outcome != StopCrashed) {
		return apForecast{}
	}
	tStop := w.Clock.SimTime.Add(time.Duration(pred.ElapsedSec * float64(time.Second)))
	along, cross := w.apStopErr(c, lat, lon, c.State, pred.StopR, tStop)
	return apForecast{along, cross, pred.MarginM, math.Max(pred.StopR.Sub(c.State.R).Norm(), apMinStopLenM), true}
}

// apBrake is the brake and approach law: thrust along surface-retrograde with
// the stop point steered onto the aim. Two knobs on the stop forecast: the
// throttle moves the stop along the track (and a little in altitude), the
// pitch-up leans the thrust toward up and holds the stop above the ground. A
// finite-difference 2x2 Newton step per tick solves both (aim, margin);
// when the throttle saturates the margin keeps its priority and the aim gives.
// The crossrange error tilts the thrust sideways (1 degree is ~7 km across
// over the Lander's brake), within the steering authority.
func (w *World) apBrake(c *spacecraft.Spacecraft, ap *spacecraft.LandingAutopilot, f apFrame, lat, lon float64) (orbital.Vec3, float64) {
	theta := c.Throttle
	if theta <= 0 {
		theta = DeorbitBrakeThrottle
	}
	psi := ap.PitchUpRad
	dir := f.retro()
	if ap.TrackN.Norm() == 0 { // a reload mid-brake: the plane is not saved
		ap.TrackN = apTrackNormal(c, c.State)
	}
	if ap.StopSpanM == 0 {
		ap.StopSpanM = math.Max(w.apForecast(c, lat, lon, theta, psi).span, apMinStopLenM)
	}
	lo := math.Max(f.hoverThrottle, 0.05)
	f0 := w.apForecast(c, lat, lon, theta, psi)
	if !f0.ok {
		// no stop to forecast (the tank runs dry first, or the step budget is
		// spent): this setting cannot be flown, so shorten the stop
		ap.OutOfReach = true
		return apPitched(dir, f.up, psi), math.Min(theta+apMaxStepTheta, 1)
	}
	ap.ForecastAlongM, ap.ForecastCrossM, ap.ForecastOK = f0.along, f0.cross, true
	f1 := w.apForecast(c, lat, lon, math.Min(theta+apProbeTheta, 1), psi)
	f2 := w.apForecast(c, lat, lon, theta, psi+apProbePitch)
	newTheta, newPsi := theta, psi
	if f1.ok && f2.ok {
		dth := math.Min(theta+apProbeTheta, 1) - theta
		if dth < apProbeTheta/4 {
			dth = -apProbeTheta // at the ceiling: probe downward instead
			f1 = w.apForecast(c, lat, lon, theta+dth, psi)
		}
		if f1.ok {
			j00, j01 := (f1.along-f0.along)/dth, (f2.along-f0.along)/apProbePitch
			j10, j11 := (f1.margin-f0.margin)/dth, (f2.margin-f0.margin)/apProbePitch
			// beyond the steering authority the aim stops pulling: the vessel
			// flies to the nearest point the brake reaches and lands soft
			reach := OutOfReachAuthorityFrac * ap.StopSpanM
			r0, r1 := -clampF(f0.along, -reach, reach), apMarginTargetM-f0.margin
			if det := j00*j11 - j01*j10; math.Abs(det) > 1e-9 && j11 != 0 {
				dTheta := (r0*j11 - j01*r1) / det * apStepDamp
				newTheta = clampF(theta+clampF(dTheta, -apMaxStepTheta, apMaxStepTheta), lo, 1)
				// the margin keeps its priority after the throttle is clamped
				newPsi = psi + (r1-j10*(newTheta-theta))/j11*apStepDamp
				if newPsi < 0 && j00 != 0 {
					// no pitch to give back: the margin is surplus, so the throttle
					// alone closes the aim
					newTheta = clampF(theta+clampF(r0/j00*apStepDamp, -apMaxStepTheta, apMaxStepTheta), lo, 1)
				}
				if newPsi > apMaxPitchUpRad && j10 > 0 {
					// pitch is spent: the margin takes throttle from the aim
					need := theta + (r1-j11*(apMaxPitchUpRad-psi))/j10*apStepDamp
					newTheta = clampF(math.Max(newTheta, need), lo, 1)
				}
			}
		}
	}
	newPsi = clampF(newPsi, 0, apMaxPitchUpRad)
	if math.Abs(newPsi-psi) > apMaxStepPitchRad {
		newPsi = psi + math.Copysign(apMaxStepPitchRad, newPsi-psi)
	}
	ap.PitchUpRad = newPsi
	ap.OutOfReach = apSaturated(newTheta, lo, f0.along, newPsi >= apMaxPitchUpRad-apThrottleEpsilon)
	ap.Reserve = newTheta > DeorbitBrakeThrottle+apThrottleEpsilon
	w.apRuntimeFor(c).outOfReach = ap.OutOfReach // the last brake tick's reading is the landing's
	dir = apPitched(dir, f.up, newPsi)
	// crossrange: tilt the thrust away from the error, within the steering authority
	if f.sh > 1 && !(ap.Nose && c.HeadingTrim != 0) { // P33: the first up/down arrow takes the cross axis until |
		phi := clampF(-apCrossGain*f0.cross/f0.span, -apMaxCrossTiltRad, apMaxCrossTiltRad)
		rHat := f.vh.Unit().Cross(f.up)
		lateral := rHat.Sub(dir.Scale(rHat.Dot(dir))).Unit()
		dir = dir.Scale(math.Cos(phi)).Add(lateral.Scale(math.Sin(phi))).Unit()
	}
	return dir, newTheta
}

// apSaturated: the law is out of reach when the throttle is pinned at a limit
// with the stop still off the aim by more than the near rung (decision 10): at
// full throttle with the stop long, or at the hover floor (or with the pitch-up
// spent holding the margin) with it short.
func apSaturated(theta, floor, along float64, pitchSpent bool) bool {
	return (theta >= 1-apThrottleEpsilon && along > SiteShiftRungNearM) ||
		((theta <= floor+apThrottleEpsilon || pitchSpent) && along < -SiteShiftRungNearM)
}

// apPitched leans dir toward up by psi radians, in the plane they span.
func apPitched(dir, up orbital.Vec3, psi float64) orbital.Vec3 {
	if perp := up.Sub(dir.Scale(up.Dot(dir))); psi != 0 && perp.Norm() > 1e-12 {
		return dir.Scale(math.Cos(psi)).Add(perp.Unit().Scale(math.Sin(psi))).Unit()
	}
	return dir
}

func clampF(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }

// apFinalRate is the descent rate the final phase commands at altitude h (m/s,
// negative = down): the schedule of decision 5 under 100 m, a derated
// stopping profile above it that meets the schedule at 100 m.
func apFinalRate(h, netDecel float64) float64 {
	switch {
	case h <= 30:
		return -FinalRateUnder30mMps
	case h <= 100:
		return -(FinalRateUnder30mMps + (FinalRateAt100mMps-FinalRateUnder30mMps)*(h-30)/70)
	}
	return -math.Sqrt(FinalRateAt100mMps*FinalRateAt100mMps + 2*math.Max(apDecelDerate*netDecel, 0.1)*(h-100))
}

// apCloseVelocity is the ground-relative velocity the final phase asks for:
// zero, plus a slow closing speed toward the aim point (half the final entry
// speed at most) so the brake's last tens of metres do not stand as the miss.
func (w *World) apCloseVelocity(c *spacecraft.Spacecraft, f apFrame, lat, lon float64) (vel orbital.Vec3, along, cross float64, ok bool) {
	e, along, cross, ok := w.apAimError(c, f, lat, lon)
	d := e.Norm()
	if d == 0 {
		return orbital.Vec3{}, along, cross, ok
	}
	return e.Unit().Scale(math.Min(d*apCloseGain, FinalPhaseGroundSpeedMps/2)), along, cross, ok
}

// apAimError is the horizontal vector from the vessel to the aim (the real
// site plus the computer's navigation error and the pilot's offsets), and the
// same error as miss:/cross: sign it (+ long, + right).
func (w *World) apAimError(c *spacecraft.Spacecraft, f apFrame, lat, lon float64) (e orbital.Vec3, along, cross float64, ok bool) {
	aimHat := siteVec(c, lat, lon, w.Clock.SimTime)
	aim := aimHat.Scale(c.Primary.RadiusMeters())
	var tHat, rHat orbital.Vec3
	if n := apTrackNormal(c, c.State); n.Norm() != 0 {
		tHat = n.Cross(aimHat).Unit()
		rHat = tHat.Cross(aimHat)
		ap := c.Landing
		aim = aim.Add(tHat.Scale(ap.NavAlongM + ap.AimAlongM)).Add(rHat.Scale(ap.NavAcrossM + ap.AimAcrossM))
		ok = true
	}
	e = aim.Sub(c.State.R)
	e = e.Sub(f.up.Scale(e.Dot(f.up)))
	if ok {
		along, cross = -e.Dot(tHat), -e.Dot(rHat)
	}
	return e, along, cross, ok
}

// apFinal nulls the drift and flies the descent rate: thrust = m * (g - a_cmd)
// toward the commanded rate, tilted against the horizontal velocity.
func (w *World) apFinal(c *spacecraft.Spacecraft, ap *spacecraft.LandingAutopilot, f apFrame, lat, lon float64) (orbital.Vec3, float64) {
	if f.aMax <= 0 {
		return f.up, 0
	}
	rCmd := apFinalRate(f.alt, f.aMax-f.g)
	if ap.ROD {
		rCmd = ap.RODRateMps
	}
	aUp := f.g + apRateGain*(rCmd-f.vz)
	if !ap.ROD && f.alt > 100 && rCmd < 0 {
		// feed-forward: the profile itself slows at the derated net deceleration,
		// so ask for it instead of waiting for the rate error to build
		aUp += math.Max(apDecelDerate*(f.aMax-f.g), 0.1) * f.vz / rCmd
	}
	if aUp < 0 {
		aUp = 0
	}
	vClose, along, cross, ok := w.apCloseVelocity(c, f, lat, lon)
	ap.ForecastAlongM, ap.ForecastCrossM, ap.ForecastOK = along, cross, ok
	aH := vClose.Sub(f.vh).Scale(apDriftGain)
	if lim := aUp * math.Tan(apMaxTiltRad); aH.Norm() > lim {
		aH = aH.Unit().Scale(lim)
	}
	tv := f.up.Scale(aUp).Add(aH)
	theta := tv.Norm() / f.aMax
	if f.alt < apTouchdownCutM && !ap.ROD {
		theta = 0
	}
	if tv.Norm() == 0 {
		return f.up, 0
	}
	return tv.Unit(), theta
}

// apOneBurn is the one-burn landing law (ADR 0069 decision 10): a brake timed
// to end at the ground at OneBurnContactMps. The thrust is gravity's share
// straight up plus the deceleration that spends the path to the ground exactly,
// a = (v^2 - v_td^2) / 2s, along surface-retrograde (a vertical fall is
// a = g + (v^2 - v_td^2) / 2h), with a zero-effort-miss pull onto the aim added
// sideways. The thrust is capped at the landing throttle, the aim giving way
// first; the cue lit the brake with the 10 % reserve left, which is what absorbs
// a late start or the pull. No rate schedule, no engine cut above the ground.
func (w *World) apOneBurn(c *spacecraft.Spacecraft, ap *spacecraft.LandingAutopilot, f apFrame, lat, lon float64) (orbital.Vec3, float64) {
	if f.aMax <= 0 {
		return f.up, 0
	}
	h, v := math.Max(f.alt, 0.05), f.vRel.Norm()
	e, along, cross, ok := w.apAimError(c, f, lat, lon)
	ap.ForecastAlongM, ap.ForecastCrossM, ap.ForecastOK = along, cross, ok
	tv := f.up.Scale(f.g)
	tgo := 1.0
	if v > OneBurnContactMps && f.vz < 0 {
		s := h * v / -f.vz // the path to the ground at the present angle
		tv = tv.Add(f.retro().Scale((v*v - OneBurnContactMps*OneBurnContactMps) / (2 * s)))
		tgo = math.Max(s/((v+OneBurnContactMps)/2), 1)
	} else if f.vz > -OneBurnContactMps {
		// slower than the contact speed (a steered burn that has shed its fall): let
		// down to it, never hover on a tank that has a ground to reach
		tv = f.up.Scale(math.Max(f.g-OneBurnLetDownGain*(OneBurnContactMps+f.vz), 0))
	}
	// the retrograde share already damps the drift by vh/tgo; the pull sideways
	// makes that the 4 vh/tgo of the zero-effort-velocity law
	aim := f.vh.Scale(-3 / tgo)
	// the last seconds only null the drift (no late aim move), and so does a tank
	// that no longer holds a hover to the ground with a half again to spare
	if ok && tgo > 3 && c.Isp > 0 && c.ActiveStageFuel() >= 1.5*tgo*c.TotalMass()*f.g/(c.Isp*stdGravityMps2) {
		aim = aim.Add(e.Scale(6 / (tgo * tgo)))
	}
	if lim := tv.Norm() * math.Tan(apMaxTiltRad); aim.Norm() > lim {
		aim = aim.Unit().Scale(lim)
	}
	tv = tv.Add(aim)
	if cap := c.LandingThrottle() * f.aMax; tv.Norm() > cap {
		tv = tv.Unit().Scale(cap)
	}
	if tv.Norm() == 0 {
		return f.up, 0
	}
	return tv.Unit(), tv.Norm() / f.aMax
}

// apRodThrottle is the throttle that holds the commanded descent rate along
// the autopilot's own thrust direction (P66: the computer on the throttle).
func apRodThrottle(c *spacecraft.Spacecraft, ap *spacecraft.LandingAutopilot, f apFrame, dir orbital.Vec3) float64 {
	if f.aMax <= 0 {
		return 0
	}
	aUp := f.g + apRateGain*(ap.RODRateMps-f.vz)
	if aUp < 0 {
		aUp = 0
	}
	return aUp / (f.aMax * math.Max(dir.Dot(f.up), apRodMinCosine))
}

// landingEnded closes the autopilot at contact: the Event Flash with the true
// distance from the real site (decision 5), then the state goes quiet.
func (w *World) landingEnded(c *spacecraft.Spacecraft) {
	ap := c.Landing
	rt := w.apRuntimeFor(c)
	defer ap.ClearAttempt()                             // one offset per landing attempt
	defer func() { c.LZClaim = spacecraft.LZClaim{} }() // spent at contact, after the flash below reads it (ADR 0068 decision 5)

	if ap.Nose { // no flash: the pilot flew it
		ap.Engaged, ap.Nose, ap.Phase = false, false, spacecraft.PhaseCoast
		return
	}
	ev := LandingEvent{CraftName: c.Name, Crashed: c.Crashed, OutOfReach: rt.outOfReach}
	if lat, lon, name, ok := w.apTarget(c); ok {
		ev.SiteName = name
		ev.DistanceM = HaversineM(c.LandedLatDeg, c.LandedLonDeg, lat, lon, c.Primary.RadiusMeters())
	}
	if ev.SiteName != "" {
		w.PendingLandingEvents = append(w.PendingLandingEvents, ev)
	}
	ap.Engaged, ap.Nose, ap.ROD, ap.ThrottleHeld = false, false, false, false
	ap.Phase = spacecraft.PhaseCoast
	rt.outOfReach = false
	rt.checked = false
}
