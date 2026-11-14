// Package sim - the deorbit burn onto a site (ADR 0066 decision 21): `N` plants
// one retro node so the vessel falls, brakes at 90 % throttle and stops on the
// targeted site. One-line reason for a new file: a new solver, not a slice of
// maneuver.go.

package sim

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

const (
	// DeorbitAheadDeg: the burn starts the instant the site is this far ahead
	// along the orbit plane (decision 21: steep and soon, 12 km per m/s).
	DeorbitAheadDeg = 60.0
	// DeorbitMaxOffTrackDeg: a site further than this from the orbit plane is
	// refused rather than reached with a plane change.
	DeorbitMaxOffTrackDeg = 1.0
	// DeorbitBrakeThrottle: the braking burn the aim assumes, leaving the pilot
	// throttle authority both ways.
	DeorbitBrakeThrottle = 1 - BrakeReserveFrac
	// deorbitHorizon covers the longest fall the solver aims (coast plus
	// brake). It is the readout horizon, one constant (fix L).
	deorbitHorizon = DescentPredictHorizon
	// deorbitRefineIters resolves the burn-at cue to ~0.1 s (it moves the stop
	// point 1.5 km per second).
	deorbitRefineIters = 8

	deorbitLongTolM  = 50.0
	deorbitCrossTolM = 100.0
	// A tilt this small is plain Retrograde (follows velocity); anything more
	// holds the planned inertial direction (BurnVector).
	deorbitMinTiltRad = 1e-4
	deorbitMaxTiltRad = 60 * math.Pi / 180

	// BoostbackMinLeadSec / BoostbackLeadPadSec: the boostback starts after the
	// stage has turned tail-first (the lead the slew needs, as a node's own
	// lead-compensation counts it) plus this pad, never sooner than the minimum.
	BoostbackMinLeadSec = 2.0
	BoostbackLeadPadSec = 1.0
	// BoostbackMaxCueAltM: a one-burn landing lights low (4 to 17 km in the
	// measured cases). A fall whose brake must light higher than this is a
	// gravity-loss hover on the landing throttle that runs the tank dry
	// (measured: lit 76 km up, crash at 100 m/s).
	BoostbackMaxCueAltM = 20e3
	// BoostbackFuelMarginKg: the propellant the stop forecast must leave. The
	// forecast stops at the ground on a constant 0.9 of the landing throttle, the
	// flown law steers and lets down to 2 m/s, and ends 0.9 t to 1.2 t lighter in
	// the tank than the forecast (measured, 11 to 16 % kept); 1.5 t leaves 0.3 t to
	// 0.6 t of spare after that.
	BoostbackFuelMarginKg = 1500.0
	// The entry burn (ADR 0069 decision 14, amended): three engines of nine,
	// lit as the fall passes EntryAltM, sized to the smallest burn that leaves
	// the landing burn lighting under EntryTargetCueAltM.
	EntryThrottle = 3.0 / 9
	entryDVStep   = 100.0
	// entryDVMax is the longest entry burn offered. Longer ones (1,180 m/s and up,
	// a minute or two on three engines) run down into the dense air that the
	// finite-burn forecast leaves out: flown, they landed 1.4 km to 13 km off
	// at several warps; capped, the planner takes a bigger boostback instead.
	entryDVMax = 1100.0
)

// The refusal lines, verbatim (decision 21). "vessel not in this system" and
// "a burn is already planted (...)" are the App's, as for H.
var (
	ErrDeorbitNoSite     = errors.New("no site targeted, press t")
	ErrDeorbitOtherBody  = errors.New("site not on this body")
	ErrDeorbitLanded     = errors.New("on the ground")
	ErrDeorbitNoSignal   = errors.New("NO SIGNAL")
	ErrDeorbitNoOrbit    = errors.New("no orbit to leave")
	ErrDeorbitFalling    = errors.New("already falling, fly miss:")
	ErrDeorbitNoSolution = errors.New("no solution from this orbit")
	ErrDeorbitNoEngine   = errors.New("no engine")
)

// ErrBoostbackNoSolution: no boostback, with any entry burn, lets the stage land
// on the site.
var ErrBoostbackNoSolution = errors.New("no boostback lands on the site")

// noLanding is the refusal for a fall that reaches the site but cannot land:
// where the landing burn would light, or none when no burn gets that far.
func noLanding(cueAlt float64) error {
	if cueAlt <= 0 || math.IsInf(cueAlt, 0) || math.IsNaN(cueAlt) {
		return ErrBoostbackNoSolution
	}
	return &BoostbackNoLandingError{CueAltM: cueAlt}
}

// BoostbackNoLandingError: a boostback would put the fall on the site, but the
// one-burn landing that follows cannot end at the ground (decision 14's refusal):
// its brake has to light CueAltM up and stops in the air, so the stage would
// burn its tank out before it landed.
type BoostbackNoLandingError struct{ CueAltM float64 }

func (e *BoostbackNoLandingError) Error() string {
	return fmt.Sprintf("landing burn lights %s up", readout.Distance(e.CueAltM))
}

// DeorbitBrakeTooLowError is the "no solution" whose cause is the orbit: the
// vessel's own reserve-throttle brake loses more altitude between light and
// stop than the orbit has. NeedM is the lowest whole-km circular altitude it
// stops above the ground from; OrbitM is the vessel's altitude now.
type DeorbitBrakeTooLowError struct{ NeedM, OrbitM float64 }

func (e *DeorbitBrakeTooLowError) Error() string {
	return fmt.Sprintf("brake needs %d km, orbit %d km", int(math.Ceil(e.NeedM/1000)), int(math.Round(e.OrbitM/1000)))
}

// brakeStopsFrom: a minimal deorbit from a circular orbit at altitude h in c's
// plane (retro at that point to a ground-grazing periapsis), then the
// reserve-throttle brake lit as late as it can be. True when it stops above the
// ground at least DeorbitAheadDeg of arc beyond the burn point: the planner
// lights the burn with the site that far ahead, so a shorter reach can never
// land on it. shortcut: periapsis 5 km under the surface and the body's
// rotation ignored, so the threshold is the planner's to within a km or so.
func brakeStopsFrom(c *spacecraft.Spacecraft, h float64) bool {
	mu, rp := c.Primary.GravitationalParameter(), c.Primary.RadiusMeters()
	r := rp + h
	rPeri := rp - 5000
	st := c.State
	st.R = c.State.R.Unit().Scale(r)
	st.V = c.State.R.Cross(c.State.V).Unit().Cross(st.R.Unit()).Scale(math.Sqrt(2 * mu * rPeri / (r * (r + rPeri))))
	st.M = c.TotalMass()
	v := *c
	v.State = st
	sf, _, ok := stopPointFor(&v, 2*time.Hour) // half a lap of the coast ellipse
	return ok && math.Acos(math.Max(-1, math.Min(1, st.R.Unit().Dot(sf.R.Unit()))))/rad >= DeorbitAheadDeg
}

// brakeTooLow returns the typed refusal when the brake cannot stop above the
// ground from c's altitude but can from a higher whole-km circular orbit (below
// one body radius); nil for any other cause.
func brakeTooLow(c *spacecraft.Spacecraft) *DeorbitBrakeTooLowError {
	if c == nil || !canBrakeAtSite(c) {
		return nil
	}
	h0 := c.State.R.Norm() - c.Primary.RadiusMeters()
	if brakeStopsFrom(c, h0) {
		return nil
	}
	hi := math.Max(h0, 1000)
	for ; !brakeStopsFrom(c, hi); hi *= 2 {
		if hi > c.Primary.RadiusMeters() {
			return nil
		}
	}
	loKm, hiKm := int(h0/1000), int(math.Ceil(hi/1000)) // loKm fails (floor of a failing h0 may stop; the bisection only needs hiKm to stop)
	for hiKm-loKm > 1 {
		if mid := (loKm + hiKm) / 2; brakeStopsFrom(c, float64(mid)*1000) {
			hiKm = mid
		} else {
			loKm = mid
		}
	}
	return &DeorbitBrakeTooLowError{NeedM: float64(hiKm) * 1000, OrbitM: h0}
}

// DeorbitOffTrackError is the off-track refusal: the angle, and either when the
// ground brings the site under the track (Reaches) or that it never will
// (the orbit only reaches ReachDeg of latitude).
type DeorbitOffTrackError struct {
	OffDeg   float64
	UnderIn  time.Duration
	Reaches  bool
	LatDeg   float64
	ReachDeg float64
}

func (e *DeorbitOffTrackError) Error() string { return "site off your track" }

// DeorbitPlan summarises a planted deorbit node for the status flash.
type DeorbitPlan struct {
	Site    string
	DV      float64
	In      time.Duration // until the burn starts
	LongM   float64       // where the aimed point lands against the site, + beyond it
	CrossM  float64
	Impact  bool // the vessel cannot stop: the aim is the free-fall impact
	TiltDeg float64
	// Boostback: the burn is a boostback (ADR 0069 decision 14) from a suborbital
	// arc, not a deorbit from a circle.
	Boostback bool
	// EntryDV / EntryIn: the entry burn behind it (0 when the fall needs none) and
	// how long after the boostback ends it starts.
	EntryDV, EntryAltM float64
	EntryIn            time.Duration
	// CueAltM: the height the landing burn after a boostback lights; FuelLeftKg
	// the propellant the forecast leaves at touchdown.
	CueAltM, FuelLeftKg float64
}

// canBrakeAtSite: an engine that, at the brake throttle, out-thrusts gravity at
// the surface, with propellant in the lit stage.
// oneBurn: c's bottom stage is marked for a one-burn landing (ADR 0069
// decision 10): its brake ends at the ground, with no descent-rate schedule.
func oneBurn(c *spacecraft.Spacecraft) bool { return c.LandingThrottle() > 0 }

// brakeThrottle is the throttle the cue, stop:, N and the autopilot plan the
// brake at, as a fraction of full thrust: the reserve throttle, of the landing
// throttle on a marked vessel (a Falcon stage's one engine of nine plans at
// 0.9 / 9). Unmarked vessels get DeorbitBrakeThrottle, bit for bit.
func brakeThrottle(c *spacecraft.Spacecraft) float64 {
	if lt := c.LandingThrottle(); lt > 0 {
		return DeorbitBrakeThrottle * lt
	}
	return DeorbitBrakeThrottle
}

func canBrakeAtSite(c *spacecraft.Spacecraft) (ok bool) {
	c.OnLandingBrake(func() { ok = canBrakeAtSiteOn(c) })
	return ok
}

func canBrakeAtSiteOn(c *spacecraft.Spacecraft) bool {
	if c.Thrust <= 0 || c.Isp <= 0 || c.ActiveStageFuel() <= 0 || c.TotalMass() <= 0 {
		return false
	}
	r := c.Primary.RadiusMeters()
	if r <= 0 {
		return false
	}
	return c.Thrust*brakeThrottle(c)/c.TotalMass() > c.Primary.GravitationalParameter()/(r*r)
}

// deorbitSite resolves the aimed surface point: a site, a landed vessel or a
// landed ghost on the vessel's primary.
func (w *World) deorbitSite(c *spacecraft.Spacecraft) (lat, lon float64, name string, err error) {
	switch w.Target.Kind {
	case TargetSite:
		s, ok := w.targetSite()
		if !ok {
			return 0, 0, "", ErrDeorbitNoSite
		}
		if s.Body != c.Primary.EnglishName || s.System != w.System().Name {
			return 0, 0, "", ErrDeorbitOtherBody
		}
		return s.LatDeg, s.LonDeg, s.Name, nil
	case TargetCraft:
		t, _, ok := w.ResolveTargetCraft()
		if !ok || !t.Landed {
			return 0, 0, "", ErrDeorbitNoSite
		}
		if t.Primary.ID != c.Primary.ID {
			return 0, 0, "", ErrDeorbitOtherBody
		}
		lat, lon = t.SurfaceLatLon()
		return lat, lon, t.Name, nil
	case TargetGhost:
		g, p, ok := w.ResolveTargetGhost()
		if !ok || !g.Landed {
			return 0, 0, "", ErrDeorbitNoSite
		}
		if p.ID != c.Primary.ID {
			return 0, 0, "", ErrDeorbitOtherBody
		}
		return g.LatDeg, g.LonDeg, g.Name, nil
	}
	return 0, 0, "", ErrDeorbitNoSite
}

// DeorbitPreflight is the cheap half of the refusals (no solving), for the
// planner's QUICK PLANS row; nil when N could run.
func (w *World) DeorbitPreflight() error {
	c := w.ActiveCraft()
	if c == nil || c.Crashed {
		return ErrDeorbitNoOrbit
	}
	if c.Landed {
		return ErrDeorbitLanded
	}
	if _, _, _, err := w.deorbitSite(c); err != nil {
		return err
	}
	if !w.CanCommandCraft(c) {
		return ErrDeorbitNoSignal
	}
	if c.Thrust <= 0 {
		return ErrDeorbitNoEngine
	}
	return nil
}

func vec3(v render.Vec3) orbital.Vec3 { return orbital.Vec3{X: v.X, Y: v.Y, Z: v.Z} }

// burnState flies one planned burn (finite for a vessel with an engine,
// impulsive otherwise) from st0, returning the post-burn state and the burn
// duration in seconds.
func burnState(c *spacecraft.Spacecraft, st0 physics.StateVector, mu, dv float64, n spacecraft.ManeuverNode) (physics.StateVector, float64) {
	dir := func(r, v orbital.Vec3) orbital.Vec3 { return spacecraft.NodeBurnDirection(n, r, v) }
	if c.Thrust > 0 && c.Isp > 0 && st0.M > 0 {
		thrust := c.Thrust * n.EffectiveThrottle() // an entry burn flies on three engines of nine
		s := planner.SimulateFiniteBurn(st0, mu, thrust, c.Isp, dv, dir)
		return s, (st0.M - s.M) / (thrust / (c.Isp * stdGravityMps2))
	}
	s := st0
	s.V = st0.V.Add(dir(st0.R, st0.V).Scale(dv))
	return s, 0
}

// afterBurnCraft is c with its state replaced by s and the burned propellant
// taken out of the lit tank, so the stop forecasts see the right mass.
func afterBurnCraft(c *spacecraft.Spacecraft, s physics.StateVector) *spacecraft.Spacecraft {
	v := *c
	v.Stages = append([]spacecraft.Stage(nil), c.Stages...)
	if burned := c.TotalMass() - s.M; burned > 0 {
		v.BurnFuel(burned)
	}
	v.State = s
	if landingDrop(&v) {
		// N drops these once the burn is done: the fall flies the landing stage
		v.Stages = v.Stages[len(v.Stages)-1:]
		v.SyncFields()
		v.State.M = v.TotalMass()
		armLandingChute(&v)
	}
	return &v
}

// stopForecast is where a vessel's 90 % brake ends: the primary-relative stop
// position and the flight time to it.
type stopForecast struct {
	R     orbital.Vec3
	After time.Duration
	// CueAltM is the height the brake lights at; FuelLeftKg the propellant the
	// stop leaves (the tank at the cue less what the stop's Δv burns).
	CueAltM, FuelLeftKg float64
}

// stopPointFor lights a 90 % retrograde brake at the latest instant that still
// stops above the ground (or now, once past it) and reports where it stops.
// ground=false when the coast never meets the ground in the horizon;
// ok=false when it meets it but the brake cannot stop above it.
func stopPointFor(v *spacecraft.Spacecraft, horizon time.Duration) (sf stopForecast, ground, ok bool) {
	v.OnLandingBrake(func() { sf, ground, ok = stopPointForOn(v, horizon) })
	return
}

func stopPointForOn(v *spacecraft.Spacecraft, horizon time.Duration) (sf stopForecast, ground, ok bool) {
	cue, ground, hasCue := burnAtCue(v, horizon, brakeThrottle(v), deorbitRefineIters)
	if !ground {
		return stopForecast{}, false, false
	}
	start, lead := v.State, 0.0
	if hasCue {
		start, lead = cue.State, cue.InSec
	}
	pred, pok := predictPoweredStopFromAt(start, v, horizon-time.Duration(lead*float64(time.Second)), brakeThrottle(v))
	if !pok || pred.Outcome != StopStopped || pred.MarginM < 0 {
		return stopForecast{}, true, false
	}
	sf = stopForecast{R: pred.StopR, After: time.Duration((lead + pred.ElapsedSec) * float64(time.Second))}
	if hasCue {
		sf.CueAltM = cue.AltitudeM
	}
	sf.FuelLeftKg = pred.FuelLeftKg
	if oneBurn(v) {
		// the landing flies a law, not this constant brake: price its propellant
		// (a tank that runs dry first leaves 0: the gate refuses it, the cue stays)
		sf.FuelLeftKg, _ = oneBurnFuelLeft(start, v, pred.StopR)
	}
	return sf, true, true
}

func (w *World) surfaceLatLon(primary orbital.Vec3, c *spacecraft.Spacecraft, t time.Time) (lat, lon float64) {
	return render.WorldToBodyFixed(c.Primary, unitV(primary), t)
}

// boostMode selects the boostback variant of the solve: off (a deorbit), aim
// (the horizontal burn, aimed only) or aim and land (the aim must also be a
// fall the one-burn landing can end at the ground).
type boostMode uint8

const (
	boostOff boostMode = iota
	boostAim
	boostAimLand
)

// deorbitOffset is the aimed point's offset from the site along and across the
// track from the burn point (sign convention of ImpactOffsetM).
type deorbitOffset struct {
	long, cross float64
	valid       bool
	cueAlt      float64
	fuelLeft    float64 // the stop forecast's propellant at the stop
}

// aimOffset flies the burn dv/tilt from st0 (at tS) and reads where the aim
// lands: the 90 % stop point for a vessel that can stop, the free-fall impact
// otherwise. status: 0 fine, +1 too shallow (no ground), -1 too steep to stop, -2 (boostAimLand
// only) a stop that ends with altitude in hand, whose brake lights off.cueAlt up.
func (w *World) aimOffset(c *spacecraft.Spacecraft, st0 physics.StateVector, tS time.Time, dv float64, node spacecraft.ManeuverNode, stop bool, boost boostMode, entry entryBurn, site [2]float64) (off deorbitOffset, status int) {
	w.aimEvals++
	mu := c.Primary.GravitationalParameter()
	s, dur := burnState(c, st0, mu, dv, node)
	tEnd := tS.Add(time.Duration(dur * float64(time.Second)))
	v := afterBurnCraft(c, s)
	if entry.dv > 0 { // the entry burn on the way down, between the boostback and the landing
		te, ok := entryDelay(s, mu, c.Primary.RadiusMeters(), entry.alt)
		se, ok2 := physics.KeplerStep(s, mu, te)
		if !ok || !ok2 {
			return off, 1
		}
		se.M = v.TotalMass()
		s2, dur2 := burnState(v, se, mu, entry.dv, entryNodeFor(v, se, tEnd, entry.dv))
		tEnd = tEnd.Add(time.Duration((te + dur2) * float64(time.Second)))
		v = afterBurnCraft(v, s2)
	}
	var aim orbital.Vec3
	var tAim time.Time
	if stop {
		sf, ground, ok := stopPointFor(v, deorbitHorizon)
		switch {
		case !ground:
			return off, 1
		case !ok:
			return off, -1
		}
		off.cueAlt, off.fuelLeft = sf.CueAltM, sf.FuelLeftKg
		if boost == boostAimLand && (sf.CueAltM > BoostbackMaxCueAltM || sf.FuelLeftKg < BoostbackFuelMarginKg) {
			return off, -2
		}
		aim, tAim = sf.R, tEnd.Add(sf.After)
	} else {
		imp, ok := PredictImpact(v, deorbitHorizon)
		if !ok {
			return off, 1
		}
		aim, tAim = imp.Point, tEnd.Add(imp.TimeToImpact)
	}
	oLat, oLon := w.surfaceLatLon(st0.R, c, tS)
	aLat, aLon := w.surfaceLatLon(aim, c, tAim)
	radius := c.Primary.RadiusMeters()
	if boost != boostOff {
		// a boostback comes back toward the site, so the track is the line to it
		// (site and aim swapped: short of the site reads positive, as more burn
		// brings the aim in)
		l, x, ok := ImpactOffsetM(oLat, oLon, site[0], site[1], aLat, aLon, radius)
		return deorbitOffset{long: l, cross: x, valid: ok, cueAlt: off.cueAlt, fuelLeft: off.fuelLeft}, 0
	}
	l, x, ok := ImpactOffsetM(oLat, oLon, aLat, aLon, site[0], site[1], radius)
	return deorbitOffset{long: l, cross: x, valid: ok}, 0
}

// nodeFor builds the planted node for a burn starting at tS.
// A boostback burns along the local horizontal against the velocity (it keeps
// its climb), not against the whole velocity.
func nodeFor(c *spacecraft.Spacecraft, st0 physics.StateVector, tS time.Time, dv, tilt float64, boost bool) spacecraft.ManeuverNode {
	n := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnRetrograde,
		DV:          dv,
		Event:       spacecraft.TriggerAbsolute,
		PrimaryID:   c.Primary.ID,
		AdvisoryKey: AdvisoryKeyDeorbit,
	}
	if math.Abs(tilt) >= deorbitMinTiltRad || boost {
		hHat := st0.R.Cross(st0.V).Unit()
		back := st0.V.Unit()
		if boost {
			up := st0.R.Unit()
			back = st0.V.Sub(up.Scale(st0.V.Dot(up))).Unit()
		}
		n.Mode = spacecraft.BurnVector
		n.BurnDirUnit = back.Scale(-math.Cos(tilt)).Add(hHat.Scale(math.Sin(tilt))).Unit()
	}
	n.Duration = c.BurnTimeForDV(dv)
	n.TriggerTime = tS.Add(n.Duration / 2)
	return n
}

// siteVec is the unit vector of a body-fixed point at time t.
func siteVec(c *spacecraft.Spacecraft, lat, lon float64, t time.Time) orbital.Vec3 {
	return vec3(render.BodyFixedToWorld(c.Primary, lat, lon, t))
}

func wrapDeg(d float64) float64 { return math.Remainder(d, 360) }

// vGrazeSpeed is the speed at st0's radius of the ellipse that just grazes the
// surface at the opposite side: the speed a deorbit burn is measured down from.
func vGrazeSpeed(c *spacecraft.Spacecraft, st0 physics.StateVector) float64 {
	mu, r0, rp := c.Primary.GravitationalParameter(), st0.R.Norm(), c.Primary.RadiusMeters()
	return math.Sqrt(2 * mu * rp / (r0 * (rp + r0)))
}

// solveRetro finds the size and out-of-plane tilt of the retro burn from st0 at
// tS that puts the aim on site: Newton on the size along the track, a secant on
// the tilt to close the cross-track offset. hint seeds the size.
func (w *World) solveRetro(c *spacecraft.Spacecraft, st0 physics.StateVector, tS time.Time, stop bool, boost boostMode, entry entryBurn, site [2]float64, hint float64, warm bool) (dv, tilt float64, lastOff deorbitOffset, noLandAlt float64, ok bool) {
	maxDV := stageDV(c) // what the stage has
	solveDv := func(tilt, hint float64, warm bool) (float64, bool) {
		f := func(dv float64) float64 {
			n := nodeFor(c, st0, tS, dv, tilt, boost != boostOff)
			off, status := w.aimOffset(c, st0, tS, dv, n, stop, boost, entry, site)
			switch {
			case status > 0:
				return math.Inf(1)
			case status < 0:
				if status == -2 {
					noLandAlt = off.cueAlt
				}
				return math.Inf(-1)
			case !off.valid:
				return 0
			}
			lastOff = off
			return off.long
		}
		// the hint is a root already and this aim has needed the full scan before (a
		// rising aim: a site ahead on the track, a drone ship, which the Illinois
		// bracket below reads as too steep): the next root sits near the hint, so look
		// there first (R5). A falling aim (an LZ behind the stage) brackets in a few
		// flights as before, and N presses never take this path.
		if warm && boost != boostOff && w.boostRising {
			for _, win := range [][3]float64{{0.85, 1.25, 8}, {0.7, 1.6, 12}} {
				if x, ok := scanRoot(f, win[0]*hint, math.Min(win[1]*hint, maxDV), int(win[2]), deorbitLongTolM); ok {
					return x, ok
				}
			}
		}
		if x, ok := bracketRoot(f, hint, deorbitLongTolM); ok || boost == boostOff {
			return x, ok
		}
		// A boostback's feasible burns can sit between two infeasible ones (too
		// small and the fall arrives too fast to land, too big and the tank is
		// gone), which the one-sided bracket reads as "too steep" and backs away
		// from: scan the whole range for the sign change instead.
		w.boostRisingSeen = true
		return scanRoot(f, 0.01*hint, math.Min(3*hint, maxDV), 60, deorbitLongTolM)
	}
	if dv, ok = solveDv(0, hint, warm); !ok {
		return
	}
	if math.Abs(lastOff.cross) > deorbitCrossTolM {
		t0, x0 := 0.0, lastOff.cross
		t1 := 0.5 * rad
		converged := false
		for i := 0; i < 8; i++ {
			d1, ok1 := solveDv(t1, dv, true)
			if !ok1 {
				return 0, 0, lastOff, noLandAlt, false
			}
			dv = d1
			x1 := lastOff.cross
			if math.Abs(x1) <= deorbitCrossTolM {
				converged = true
				break
			}
			if x1 == x0 {
				break
			}
			t0, x0, t1 = t1, x1, t1-x1*(t1-t0)/(x1-x0)
			if math.Abs(t1) > deorbitMaxTiltRad || math.IsNaN(t1) {
				break
			}
		}
		if !converged {
			return 0, 0, lastOff, noLandAlt, false
		}
		tilt = t1
	}
	return dv, tilt, lastOff, noLandAlt, true
}

// PlanDeorbitOntoSite plants one retro node that puts the vessel's aimed point
// (its 90 % stop point, or the free-fall impact for a vessel that cannot stop)
// on the targeted site, replacing its own previous node. Refusals are the
// package's Err* values or a *DeorbitOffTrackError.
func (w *World) PlanDeorbitOntoSite() (*DeorbitPlan, error) {
	plan, err := w.planDeorbitOntoSite()
	if err == ErrDeorbitNoSolution {
		if low := brakeTooLow(w.ActiveCraft()); low != nil {
			return nil, low
		}
	}
	return plan, err
}

func (w *World) planDeorbitOntoSite() (*DeorbitPlan, error) {
	c := w.ActiveCraft()
	if c == nil || c.Crashed {
		return nil, ErrDeorbitNoOrbit
	}
	if c.Landed {
		return nil, ErrDeorbitLanded
	}
	lat, lon, name, err := w.deorbitSite(c)
	if err != nil {
		return nil, err
	}
	if !w.canCommand(c) {
		return nil, ErrDeorbitNoSignal
	}
	if c.Thrust <= 0 { // executeDueNodes drops a node for a vessel with no engine
		return nil, ErrDeorbitNoEngine
	}
	mu := c.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if mu <= 0 || el.E >= 1 || el.A <= 0 || c.State.R.Norm() <= c.Primary.RadiusMeters() {
		return nil, ErrDeorbitNoOrbit
	}
	if _, found := PredictImpact(c, DescentPredictHorizon); found {
		if oneBurn(c) {
			if plan, err := w.planBoostback(c, lat, lon, name); plan != nil || err != nil {
				return plan, err
			}
		}
		return nil, ErrDeorbitFalling
	}
	// ADR 0067 decision 3: the computer draws its own picture of the site here,
	// and the burn is planned onto that picture, not the real flag.
	w.drawNavError(c, true)
	lat, lon = w.computerSiteLatLon(c, lat, lon)
	now := w.Clock.SimTime
	hHat := c.State.R.Cross(c.State.V).Unit()
	period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)
	at := func(s float64) time.Time { return now.Add(time.Duration(s * float64(time.Second))) }
	ahead := func(s float64) float64 { // degrees the site leads the vessel along the plane, minus the aim
		st, ok := physics.KeplerStep(c.State, mu, s)
		if !ok {
			return math.NaN()
		}
		rv := st.R.Unit()
		p := siteVec(c, lat, lon, at(s))
		return wrapDeg(math.Atan2(p.Dot(hHat.Cross(rv)), p.Dot(rv))/rad - DeorbitAheadDeg)
	}

	// The next instant the site is 60 degrees ahead: the first fall of
	// ahead() through zero in one lap; a site within a hair of it now is now.
	sBurn := -1.0
	if g0 := ahead(0); g0 <= 0 && g0 > -1e-3 {
		sBurn = 0
	} else {
		const samples = 360
		prevS, prevG := 0.0, g0
		for i := 1; i <= samples+1 && sBurn < 0; i++ {
			s := period * float64(i) / samples
			g := ahead(s)
			if prevG >= 0 && g < 0 && prevG-g < 90 {
				lo, hi := prevS, s
				for k := 0; k < 40; k++ {
					mid := (lo + hi) / 2
					if ahead(mid) >= 0 {
						lo = mid
					} else {
						hi = mid
					}
				}
				sBurn = (lo + hi) / 2
			}
			prevS, prevG = s, g
		}
	}
	if sBurn < 0 || math.IsNaN(sBurn) {
		return nil, ErrDeorbitNoSolution
	}
	tS := at(sBurn)

	// Off the orbit plane at the burn.
	if offDeg := math.Asin(math.Min(1, math.Abs(siteVec(c, lat, lon, tS).Dot(hHat)))) / rad; offDeg > DeorbitMaxOffTrackDeg+1e-6 {
		return nil, w.deorbitOffTrack(c, lat, lon, hHat, offDeg)
	}

	st0, ok := physics.KeplerStep(c.State, mu, sBurn)
	if !ok {
		return nil, ErrDeorbitNoSolution
	}
	st0.M = c.TotalMass()
	stop := canBrakeAtSite(c)
	dv, tilt, lastOff, _, ok := w.solveRetro(c, st0, tS, stop, boostOff, entryBurn{}, [2]float64{lat, lon}, 2.5*math.Max(st0.V.Norm()-vGrazeSpeed(c, st0), 1), false)
	if !ok {
		return nil, ErrDeorbitNoSolution
	}

	n := nodeFor(c, st0, tS, dv, tilt, false)
	w.replaceAdvisoryNode(c, AdvisoryKeyDeorbit)
	w.replaceAdvisoryNode(c, AdvisoryKeyEntry) // a stale entry burn goes with the boostback it belonged to
	w.PlanNode(n)
	plan := &DeorbitPlan{
		Site: name, DV: dv, In: tS.Sub(now), LongM: lastOff.long, CrossM: lastOff.cross,
		Impact: !stop, TiltDeg: tilt / rad,
	}
	w.recordDeorbit(plan)
	return plan, nil
}

// deorbitOffTrack builds the off-track refusal: when the rotating ground
// brings the site under the orbit plane, or that the orbit never reaches it.
func (w *World) deorbitOffTrack(c *spacecraft.Spacecraft, lat, lon float64, hHat orbital.Vec3, offDeg float64) error {
	spin := vec3(render.BodyRotationAxisWorld(c.Primary))
	inclDeg := math.Acos(math.Min(1, math.Abs(hHat.Dot(spin)))) / rad
	e := &DeorbitOffTrackError{OffDeg: offDeg, LatDeg: lat, ReachDeg: inclDeg}
	if math.Abs(lat) > inclDeg+1e-6 {
		return e
	}
	e.Reaches = true
	om := render.BodySpinOmegaWorld(c.Primary)
	omega := math.Sqrt(om.X*om.X + om.Y*om.Y + om.Z*om.Z)
	if omega == 0 {
		e.Reaches = false
		return e
	}
	period := 2 * math.Pi / omega
	now := w.Clock.SimTime
	f := func(s float64) float64 {
		return siteVec(c, lat, lon, now.Add(time.Duration(s*float64(time.Second)))).Dot(hHat)
	}
	const samples = 720
	step := period / samples
	best, bestAbs := step, math.Inf(1)
	prev := f(0)
	for i := 1; i <= samples+1; i++ {
		s := float64(i) * step
		cur := f(s)
		if (prev < 0) != (cur < 0) {
			lo, hi := s-step, s
			for k := 0; k < 40; k++ {
				mid := (lo + hi) / 2
				if (f(mid) < 0) == (prev < 0) {
					lo = mid
				} else {
					hi = mid
				}
			}
			e.UnderIn = time.Duration((lo + hi) / 2 * float64(time.Second))
			return e
		}
		if a := math.Abs(cur); a < bestAbs {
			best, bestAbs = s, a
		}
		prev = cur
	}
	e.UnderIn = time.Duration(best * float64(time.Second)) // tangent: the closest pass
	return e
}

// bracketRoot finds dv where f (decreasing in dv; +Inf too shallow, -Inf too
// steep) is within tol of zero: expand a bracket around hint, then Illinois.
func bracketRoot(f func(float64) float64, hint, tol float64) (float64, bool) {
	lo, hi := hint*0.9, hint*1.1
	flo, fhi := f(lo), f(hi)
	for i := 0; i < 24 && flo < 0; i++ { // lo too steep: back off
		hi, fhi = lo, flo
		lo *= 0.6
		flo = f(lo)
	}
	for i := 0; i < 24 && fhi > 0; i++ { // hi too shallow: push on
		lo, flo = hi, fhi
		hi *= 1.6
		fhi = f(hi)
	}
	if flo < 0 || fhi > 0 {
		return 0, false
	}
	side := 0
	for i := 0; i < 60; i++ {
		var x float64
		if math.IsInf(flo, 0) || math.IsInf(fhi, 0) {
			x = (lo + hi) / 2
		} else {
			x = hi - fhi*(hi-lo)/(fhi-flo)
			if !(x > lo && x < hi) {
				x = (lo + hi) / 2
			}
		}
		fx := f(x)
		if math.Abs(fx) <= tol {
			return x, true
		}
		if fx > 0 {
			lo, flo = x, fx
			if side == 1 && !math.IsInf(fhi, 0) {
				fhi /= 2
			}
			side = 1
		} else {
			hi, fhi = x, fx
			if side == -1 && !math.IsInf(flo, 0) {
				flo /= 2
			}
			side = -1
		}
		if hi-lo < 1e-4 {
			return x, math.Abs(fx) <= tol // a collapse onto a jump is no root unless it is within tolerance
		}
	}
	return 0, false
}

// scanRoot finds dv in [lo, hi] where f (monotone; +Inf and -Inf mark burns
// that cannot be judged) is within tol of zero: sample n+1 points and bisect
// the first neighbouring pair that straddles zero, falling or rising (a site
// ahead of the stage on its track, a drone ship, is a rising f: more burn
// brings the aim in from beyond it; a site behind it, a falling one). Where a
// judged point meets an unjudged one the edge of the judged range is found first
// (the root can sit in a sliver of it narrower than the sample step).
func scanRoot(f func(float64) float64, lo, hi float64, n int, tol float64) (float64, bool) {
	if hi <= lo {
		return 0, false
	}
	straddle := func(fa, fb float64) bool { return (fa > 0 && fb < 0) || (fa < 0 && fb > 0) }
	bisect := func(a, fa, b, fb float64) (float64, bool) { // fa and fb opposite signs, both judged
		for k := 0; k < 40; k++ {
			m := (a + b) / 2
			fm := f(m)
			if math.Abs(fm) <= tol {
				return m, true
			}
			switch {
			case math.IsInf(fm, 0):
				return 0, false
			case (fm > 0) == (fa > 0):
				a, fa = m, fm
			default:
				b = m
			}
		}
		return 0, false
	}
	// edge walks from a judged point toward an unjudged one to the last judged point.
	edge := func(good, bad float64) (float64, float64) {
		for k := 0; k < 30 && math.Abs(bad-good) > 0.5; k++ {
			m := (good + bad) / 2
			if math.IsInf(f(m), 0) {
				bad = m
			} else {
				good = m
			}
		}
		return good, f(good)
	}
	pa, fa := lo, f(lo)
	if math.Abs(fa) <= tol {
		return lo, true // a root at the low edge is a root (the equal case)
	}
	for i := 1; i <= n; i++ {
		pb := lo + (hi-lo)*float64(i)/float64(n)
		fb := f(pb)
		switch {
		case math.IsInf(fa, 0) && math.IsInf(fb, 0):
		case math.IsInf(fa, 0): // judged range starts between pa and pb
			if q, fq := edge(pb, pa); math.Abs(fq) <= tol {
				return q, true
			} else if straddle(fq, fb) {
				if x, ok := bisect(q, fq, pb, fb); ok {
					return x, true
				}
			}
		case math.IsInf(fb, 0): // and ends between them
			if q, fq := edge(pa, pb); math.Abs(fq) <= tol {
				return q, true
			} else if straddle(fa, fq) {
				if x, ok := bisect(pa, fa, q, fq); ok {
					return x, true
				}
			}
		case math.Abs(fb) <= tol:
			return pb, true
		case straddle(fa, fb):
			if x, ok := bisect(pa, fa, pb, fb); ok {
				return x, true
			}
		}
		pa, fa = pb, fb
	}
	return 0, false
}

// deorbitNode is the queued node N planted, if any.
func deorbitNode(c *spacecraft.Spacecraft) (spacecraft.ManeuverNode, bool) {
	for _, n := range c.Nodes {
		if n.AdvisoryKey == AdvisoryKeyDeorbit {
			return n, true
		}
	}
	return spacecraft.ManeuverNode{}, false
}

// aimPoint is a forecast aim: where it lands (inertial, primary-relative), when,
// and the burn point the track is measured from.
type aimPoint struct {
	R, originR orbital.Vec3
	at, origin time.Time
}

type deorbitCacheKey struct {
	craftID   uint64
	primary   string
	nodeID    uint64
	dv        int64
	dir       [3]int64
	bucket    int64
	midBurn   bool
	r, v      [3]int64
	mass, fue int64
	energy    int64
	h         int64
}

type deorbitCache struct {
	has bool
	key deorbitCacheKey
	pt  aimPoint
	ok  bool
}

func qz(x, step float64) int64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	return int64(math.Round(x / step))
}

func (w *World) deorbitKey(c *spacecraft.Spacecraft, n spacecraft.ManeuverNode, planned bool) deorbitCacheKey {
	k := deorbitCacheKey{craftID: c.ID, primary: c.Primary.ID, nodeID: n.ID, mass: qz(c.TotalMass(), 1), fue: qz(c.ActiveStageFuel(), 1)}
	if planned {
		// the planned stop is the Kepler orbit's, so key on its constants, not
		// on where the vessel is along it
		mu := c.Primary.GravitationalParameter()
		k.energy = qz(c.State.V.Dot(c.State.V)/2-mu/c.State.R.Norm(), 200)
		k.h = qz(c.State.R.Cross(c.State.V).Norm(), 200)
		k.dv = qz(n.DV, 1e-3)
		k.dir = [3]int64{qz(n.BurnDirUnit.X, 1e-6), qz(n.BurnDirUnit.Y, 1e-6), qz(n.BurnDirUnit.Z, 1e-6)}
		return k
	}
	k.bucket = w.Clock.SimTime.UnixNano() / int64(time.Second)
	k.midBurn = StackMidBurn(c)
	k.r = [3]int64{qz(c.State.R.X, 25), qz(c.State.R.Y, 25), qz(c.State.R.Z, 25)}
	k.v = [3]int64{qz(c.State.V.X, 0.5), qz(c.State.V.Y, 0.5), qz(c.State.V.Z, 0.5)}
	return k
}

// plannedAim is where the planted deorbit node's burn would stop the vessel.
func (w *World) plannedAim(c *spacecraft.Spacecraft, n spacecraft.ManeuverNode) (aimPoint, bool) {
	key := w.deorbitKey(c, n, true)
	if cc := &w.deorbitCache; cc.has && cc.key == key {
		return cc.pt, cc.ok
	}
	var pt aimPoint
	var ok bool
	// the node flies on the landing engine (SuperDracos), as the solver planned it
	c.OnLandingBrake(func() { pt, ok = w.computePlannedAim(c, n) })
	w.deorbitCache = deorbitCache{has: true, key: key, pt: pt, ok: ok}
	return pt, ok
}

func (w *World) computePlannedAim(c *spacecraft.Spacecraft, n spacecraft.ManeuverNode) (aimPoint, bool) {
	mu := c.Primary.GravitationalParameter()
	now := w.Clock.SimTime
	tS := n.BurnStart()
	if tS.Before(now) {
		tS = now
	}
	st0, ok := physics.KeplerStep(c.State, mu, tS.Sub(now).Seconds())
	if !ok {
		return aimPoint{}, false
	}
	st0.M = c.TotalMass()
	s, dur := burnState(c, st0, mu, n.DV, n)
	sf, _, ok := stopPointFor(afterBurnCraft(c, s), deorbitHorizon)
	if !ok {
		return aimPoint{}, false
	}
	tEnd := tS.Add(time.Duration(dur * float64(time.Second)))
	return aimPoint{R: sf.R, originR: st0.R, at: tEnd.Add(sf.After), origin: tS}, true
}

// liveAim is where the vessel would stop if it lit its 90 % brake at the
// burn-at cue (or now, once past it): the powered-stop prediction behind
// NAVIGATION's stop:, braking at the aim throttle.
func (w *World) liveAim(c *spacecraft.Spacecraft) (aimPoint, bool) {
	key := w.deorbitKey(c, spacecraft.ManeuverNode{}, false)
	if cc := &w.deorbitCache; cc.has && cc.key == key {
		return cc.pt, cc.ok
	}
	var pt aimPoint
	sf, _, ok := stopPointFor(c, deorbitHorizon)
	if ok {
		now := w.Clock.SimTime
		pt = aimPoint{R: sf.R, originR: c.State.R, at: now.Add(sf.After), origin: now}
	}
	w.deorbitCache = deorbitCache{has: true, key: key, pt: pt, ok: ok}
	return pt, ok
}

// planBoostback is decision 14: N on a marked vessel that is not in orbit and
// cannot land from where it falls plants the retro burn that puts its fall's
// stop onto the site. (nil, nil) when it need not (the landing already reaches
// the site) or cannot (no burn puts it there); the caller then reports the
// fall as it always did. *BoostbackNoLandingError when a burn would put it
// there but the landing burn that follows would light above BoostbackMaxCueAltM.
func (w *World) planBoostback(c *spacecraft.Spacecraft, lat, lon float64, name string) (*DeorbitPlan, error) {
	var reach error
	w.drawNavError(c, false) // the reach test reads the computer's site
	c.OnLandingBrake(func() { reach = w.apReachTest(c, lat, lon) })
	if reach == nil {
		return nil, nil
	}
	mu := c.Primary.GravitationalParameter()
	aim := [2]float64{lat, lon} // the true aim keys the refusal cache: the computer's site is redrawn every tick
	w.drawNavError(c, true)
	lat, lon = w.computerSiteLatLon(c, lat, lon)
	now := w.Clock.SimTime
	lead := boostbackLead(c)
	tS := now.Add(time.Duration(lead * float64(time.Second)))
	st0, ok := physics.KeplerStep(c.State, mu, lead)
	if !ok {
		return nil, nil
	}
	st0.M = c.TotalMass()
	site := [2]float64{lat, lon}
	if out, ok := w.shortfalls[shortfallKey{c.ID, aim, int(c.ActiveStageFuel() / 100), now.Unix()}]; ok {
		return nil, out // the same refused press again: the same answer, free (R6)
	}
	sol, err := w.solveBoostback(c, st0, tS, site)
	if errors.Is(err, errNoAim) {
		return nil, nil // no burn aims the fall at the site at all: the fall is reported as it was
	}
	if err != nil {
		if w.skipShortfall {
			return nil, err // the drop's own N: the figure is one press away (R6)
		}
		out := err
		if sf := w.boostbackShortfall(c, st0, tS, site); sf != nil {
			out = sf
		}
		if len(w.shortfalls) > 16 {
			w.shortfalls = nil
		}
		if w.shortfalls == nil {
			w.shortfalls = map[shortfallKey]error{}
		}
		w.shortfalls[shortfallKey{c.ID, aim, int(c.ActiveStageFuel() / 100), w.Clock.SimTime.Unix()}] = out
		return nil, out
	}
	dv, tilt, entry, off := sol.dv, sol.tilt, sol.entry, sol.off
	n := nodeFor(c, st0, tS, dv, tilt, true)
	w.replaceAdvisoryNode(c, AdvisoryKeyDeorbit)
	w.replaceAdvisoryNode(c, AdvisoryKeyEntry)
	w.PlanNode(n)
	plan := &DeorbitPlan{
		Site: name, DV: dv, In: tS.Sub(now), LongM: off.long, CrossM: off.cross,
		TiltDeg: tilt / rad, Boostback: true, CueAltM: off.cueAlt, FuelLeftKg: off.fuelLeft,
	}
	if entry.dv > 0 {
		s, dur := burnState(c, st0, mu, dv, n)
		te, _ := entryDelay(s, mu, c.Primary.RadiusMeters(), entry.alt)
		v := afterBurnCraft(c, s)
		se, _ := physics.KeplerStep(s, mu, te)
		se.M = v.TotalMass()
		en := entryNodeFor(v, se, tS.Add(time.Duration((dur+te)*float64(time.Second))), entry.dv)
		w.PlanNode(en)
		plan.EntryDV, plan.EntryAltM, plan.EntryIn = entry.dv, entry.alt, time.Duration(te*float64(time.Second))
	}
	w.recordDeorbit(plan)
	return plan, nil
}

const EntryTargetCueAltM = 10e3

// errNoAim: no boostback puts even the impact point on the site.
var errNoAim = errors.New("no aim")

// boostbackSolution is a planned boostback and the entry burn behind it.
type boostbackSolution struct {
	dv, tilt float64
	entry    entryBurn
	off      deorbitOffset
}

// solveBoostback finds the boostback (and the entry burn that lets the fall
// land) for c from st0 at tS: aim the impact point, pick the entry burn, re-aim
// with it in the fall, repeat until it stops moving, then close the exact aim
// on the brake's stop point. It plants nothing.
func (w *World) solveBoostback(c *spacecraft.Spacecraft, st0 physics.StateVector, tS time.Time, site [2]float64) (boostbackSolution, error) {
	var sol boostbackSolution
	hint := 0.6 * st0.V.Norm()
	if w.boostWarmDV > 0 {
		hint = w.boostWarmDV
	}
	// the first aim is the impact point (no brake yet: that is what the entry burn buys)
	dv, tilt, _, _, ok := w.solveRetro(c, st0, tS, false, boostAim, entryBurn{}, site, hint, w.boostWarmDV > 0)
	if !ok {
		return sol, errNoAim
	}
	var entry entryBurn
	var cue float64
	for i := 0; i < entryRounds; i++ {
		entry, cue = w.entryFor(c, st0, tS, dv, tilt)
		if cue > BoostbackMaxCueAltM {
			return sol, noLanding(cue) // not even the longest entry burn lets it land
		}
		// re-aim on the impact point with the entry burn in the fall (a brake
		// stop is infeasible over part of the range, the impact point never is)
		d2, t2, _, _, ok := w.solveRetro(c, st0, tS, false, boostAim, entry, site, dv, true)
		if !ok {
			return sol, ErrBoostbackNoSolution
		}
		moved := math.Abs(d2-dv) < 5 && math.Abs(t2-tilt) < 1e-3
		dv, tilt = d2, t2
		if moved {
			break
		}
	}
	// then the exact aim, on the brake's stop point, from there
	dv, tilt, off, _, ok := w.solveRetro(c, st0, tS, true, boostAimLand, entry, site, dv, true)
	if !ok {
		if cue <= EntryTargetCueAltM {
			return sol, ErrBoostbackNoSolution // an entry burn closes the landing, but not on the aim
		}
		return sol, noLanding(cue)
	}
	return boostbackSolution{dv, tilt, entry, off}, nil
}

// stageDV is the Δv the lit stage's propellant gives it: Isp g ln(m / (m - fuel)).
func stageDV(c *spacecraft.Spacecraft) float64 {
	m, f := c.TotalMass(), c.ActiveStageFuel()
	if f <= 0 || m <= f || c.Isp <= 0 {
		return 0
	}
	return c.Isp * stdGravityMps2 * math.Log(m/(m-f))
}

// BoostbackShortError is the refusal for a stage that has the geometry but not
// the Δv: Need is the Δv of the smallest tank of the same stage that closes a
// plan, Have the Δv of the tank it has.
type BoostbackShortError struct{ Need, Have float64 }

func (e *BoostbackShortError) Error() string {
	return fmt.Sprintf("needs %s, stage has %s", readout.DeltaV(e.Need), readout.DeltaV(e.Have))
}

// shortfallKey is one refused press: the vessel, the aim, the propellant to the
// nearest 100 kg and the sim second. The search costs 0.5 s and a pilot who
// presses N again in the same second is asking the same question.
type shortfallKey struct {
	craft uint64
	site  [2]float64
	fuel  int
	sec   int64
}

// boostbackShortfall says how far short the stage is. Method: re-solve the same
// plan on copies of the stage with more propellant (up to the tank's capacity),
// bracket and bisect the smallest tank that closes, and report that tank's Δv
// (Isp g ln(mass / mass less the lit tank)) against the real one's. nil when no tank closes
// (the fault is not propellant) or the stage has none to add.
func (w *World) boostbackShortfall(c *spacecraft.Spacecraft, st0 physics.StateVector, tS time.Time, site [2]float64) error {
	idx := spacecraft.LitStageIdx(c.Stages)
	if idx >= len(c.Stages) || c.Stages[idx].FuelCapacity <= c.Stages[idx].FuelMass {
		return nil
	}
	// the first plan that closes seeds the rest: a tank a few kg either side has
	// its root a few m/s from it, so the solves after it start there (R6)
	defer func() { w.boostWarmDV = 0 }()
	closes := func(fuel float64) (*spacecraft.Spacecraft, bool) {
		v := *c
		v.Stages = append([]spacecraft.Stage(nil), c.Stages...)
		v.Stages[idx].FuelMass = fuel
		v.SyncFields()
		st := st0
		st.M = v.TotalMass()
		sol, err := w.solveBoostback(&v, st, tS, site)
		if err == nil {
			w.boostWarmDV = sol.dv
		}
		return &v, err == nil
	}
	lo, capacity := c.Stages[idx].FuelMass, c.Stages[idx].FuelCapacity
	hi := lo
	var vHi *spacecraft.Spacecraft
	for step := 0.1; ; step *= 2.2 { // grow the tank until a plan closes
		hi = math.Min(capacity, hi+step*math.Max(lo, 0.01*capacity)) // an empty tank grows too
		v, ok := closes(hi)
		if ok {
			vHi = v
			break
		}
		if hi >= capacity {
			return nil
		}
		lo = hi
	}
	for k := 0; k < 6; k++ {
		mid := (lo + hi) / 2
		if v, ok := closes(mid); ok {
			hi, vHi = mid, v
		} else {
			lo = mid
		}
	}
	return &BoostbackShortError{Need: stageDV(vHi), Have: stageDV(c)}
}

// EntryAltsM are the heights the planner tries lighting the entry burn at,
// highest first (the real one lights about 70 km up; the game has no heat, so a
// lower one that costs less propellant is taken when it is the one that closes).
var EntryAltsM = []float64{70e3, 50e3, 40e3, 30e3, 25e3, 20e3, 15e3}

// entryBurn is the entry burn a boostback solve flies behind it: dv zero is none,
// alt the height the fall lights it at.
type entryBurn struct{ dv, alt float64 }

// entryRounds bounds the boostback / entry-burn alternation.
const entryRounds = 4

// entryNodeFor is the entry burn: retrograde on three engines of nine, starting
// at tS (the node's trigger is its middle, as nodeFor's).
func entryNodeFor(c *spacecraft.Spacecraft, st physics.StateVector, tS time.Time, dv float64) spacecraft.ManeuverNode {
	n := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnRetrograde,
		DV:          dv,
		Throttle:    EntryThrottle,
		Event:       spacecraft.TriggerAbsolute,
		PrimaryID:   c.Primary.ID,
		AdvisoryKey: AdvisoryKeyEntry,
	}
	n.Duration = c.BurnTimeForDVAt(dv, EntryThrottle)
	n.TriggerTime = tS.Add(n.Duration / 2)
	return n
}

// entryDelay is the seconds after s (the state the boostback leaves) until the
// coast first falls down through EntryAltM, on the Kepler arc (the air is
// thin that high). ok=false when it never does inside the horizon.
func entryDelay(s physics.StateVector, mu, radius, entryAlt float64) (float64, bool) {
	alt := func(t float64) float64 {
		st, ok := physics.KeplerStep(s, mu, t)
		if !ok {
			return math.NaN()
		}
		return st.R.Norm() - radius
	}
	const step, horizon = 2.0, 1500.0
	prev := alt(0)
	if math.IsNaN(prev) {
		return 0, false
	}
	for t := step; t <= horizon; t += step {
		cur := alt(t)
		if prev > entryAlt && cur <= entryAlt {
			lo, hi := t-step, t
			for k := 0; k < 30; k++ {
				if mid := (lo + hi) / 2; alt(mid) > entryAlt {
					lo = mid
				} else {
					hi = mid
				}
			}
			return (lo + hi) / 2, true
		}
		prev = cur
	}
	return 0, false
}

// entryFor is the entry burn for the boostback (dv, tilt): at each height in
// EntryAltsM the smallest burn (steps of entryDVStep, then bisected) after
// which the landing burn lights under EntryTargetCueAltM; the smallest of
// those wins (least propellant). cue is the height the landing burn lights at
// with it, or the best height reached when no burn closes.
func (w *World) entryFor(c *spacecraft.Spacecraft, st0 physics.StateVector, tS time.Time, dv, tilt float64) (best entryBurn, cue float64) {
	n := nodeFor(c, st0, tS, dv, tilt, true)
	cueAt := func(e entryBurn) float64 {
		off, status := w.aimOffset(c, st0, tS, dv, n, true, boostAim, e, [2]float64{})
		if status != 0 {
			return math.Inf(1) // no stop: fuel gone or no ground
		}
		return off.cueAlt
	}
	if c0 := cueAt(entryBurn{}); c0 <= BoostbackMaxCueAltM {
		return entryBurn{}, c0 // the landing burn lights low enough as it falls: no entry burn
	}
	cue = math.Inf(1)
	closed := false
	for _, alt := range EntryAltsM {
		prev := 0.0
		for e := entryDVStep; e <= entryDVMax; e += entryDVStep {
			if closed && e >= best.dv {
				break // cannot beat the burn already found
			}
			cc := cueAt(entryBurn{e, alt})
			if cc > EntryTargetCueAltM {
				if !closed && cc < cue {
					best, cue = entryBurn{e, alt}, cc
				}
				prev = e
				continue
			}
			lo, hi := prev, e
			for k := 0; k < 6; k++ {
				if mid := (lo + hi) / 2; cueAt(entryBurn{mid, alt}) <= EntryTargetCueAltM {
					hi = mid
				} else {
					lo = mid
				}
			}
			best, cue, closed = entryBurn{hi, alt}, cueAt(entryBurn{hi, alt}), true
			break
		}
	}
	return best, cue
}

// boostbackLead is the seconds the stage needs to swing its nose from where it
// points to the burn's horizontal-back direction, as nodeLeadActive counts it.
func boostbackLead(c *spacecraft.Spacecraft) float64 {
	cur := c.CurrentAttitudeDir.Unit()
	up := c.State.R.Unit()
	back := c.State.V.Sub(up.Scale(c.State.V.Dot(up))).Unit().Scale(-1)
	if cur.Norm() == 0 || back.Norm() == 0 {
		return BoostbackMinLeadSec
	}
	ang := math.Acos(math.Max(-1, math.Min(1, cur.Dot(back))))
	return math.Max(BoostbackMinLeadSec, nodeLeadSlack*ang/c.SlewRateRad()+BoostbackLeadPadSec)
}

// boostbackPending: c has a planted deorbit burn and a fall already under it,
// so the node is a boostback (an orbit has no ground path).
func boostbackPending(c *spacecraft.Spacecraft) bool {
	if !landingNodePending(c) {
		return false
	}
	_, falling := PredictImpact(c, DescentPredictHorizon)
	return falling
}

// landingNodePending: a burn N planted (the deorbit or boostback, or the entry
// burn behind it) has not fired yet.
func landingNodePending(c *spacecraft.Spacecraft) bool {
	for _, n := range c.Nodes {
		if n.AdvisoryKey == AdvisoryKeyDeorbit || n.AdvisoryKey == AdvisoryKeyEntry {
			return true
		}
	}
	return false
}
