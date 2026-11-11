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
		s := planner.SimulateFiniteBurn(st0, mu, c.Thrust, c.Isp, dv, dir)
		return s, (st0.M - s.M) / (c.Thrust / (c.Isp * stdGravityMps2))
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
	return &v
}

// stopForecast is where a vessel's 90 % brake ends: the primary-relative stop
// position and the flight time to it.
type stopForecast struct {
	R     orbital.Vec3
	After time.Duration
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
	return stopForecast{R: pred.StopR, After: time.Duration((lead + pred.ElapsedSec) * float64(time.Second))}, true, true
}

func (w *World) surfaceLatLon(primary orbital.Vec3, c *spacecraft.Spacecraft, t time.Time) (lat, lon float64) {
	return render.WorldToBodyFixed(c.Primary, unitV(primary), t)
}

// deorbitOffset is the aimed point's offset from the site along and across the
// track from the burn point (sign convention of ImpactOffsetM).
type deorbitOffset struct {
	long, cross float64
	valid       bool
}

// aimOffset flies the burn dv/tilt from st0 (at tS) and reads where the aim
// lands: the 90 % stop point for a vessel that can stop, the free-fall impact
// otherwise. status: 0 fine, +1 too shallow (no ground), -1 too steep to stop.
func (w *World) aimOffset(c *spacecraft.Spacecraft, st0 physics.StateVector, tS time.Time, dv float64, node spacecraft.ManeuverNode, stop bool, site [2]float64) (off deorbitOffset, status int) {
	mu := c.Primary.GravitationalParameter()
	s, dur := burnState(c, st0, mu, dv, node)
	tEnd := tS.Add(time.Duration(dur * float64(time.Second)))
	v := afterBurnCraft(c, s)
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
	l, x, ok := ImpactOffsetM(oLat, oLon, aLat, aLon, site[0], site[1], radius)
	return deorbitOffset{long: l, cross: x, valid: ok}, 0
}

// nodeFor builds the planted node for a burn starting at tS.
func nodeFor(c *spacecraft.Spacecraft, st0 physics.StateVector, tS time.Time, dv, tilt float64) spacecraft.ManeuverNode {
	n := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnRetrograde,
		DV:          dv,
		Event:       spacecraft.TriggerAbsolute,
		PrimaryID:   c.Primary.ID,
		AdvisoryKey: AdvisoryKeyDeorbit,
	}
	if math.Abs(tilt) >= deorbitMinTiltRad {
		hHat := st0.R.Cross(st0.V).Unit()
		n.Mode = spacecraft.BurnVector
		n.BurnDirUnit = st0.V.Unit().Scale(-math.Cos(tilt)).Add(hHat.Scale(math.Sin(tilt))).Unit()
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
	site := [2]float64{lat, lon}

	// Newton on the burn's size to put the aim on the site along the track, with
	// a secant on the out-of-plane tilt to close the cross-track offset.
	var lastOff deorbitOffset
	solveDv := func(tilt, hint float64) (float64, bool) {
		f := func(dv float64) float64 {
			n := nodeFor(c, st0, tS, dv, tilt)
			off, status := w.aimOffset(c, st0, tS, dv, n, stop, site)
			switch {
			case status > 0:
				return math.Inf(1)
			case status < 0:
				return math.Inf(-1)
			case !off.valid:
				return 0
			}
			lastOff = off
			return off.long
		}
		return bracketRoot(f, hint, deorbitLongTolM)
	}
	v0 := st0.V.Norm()
	r0 := st0.R.Norm()
	rp := c.Primary.RadiusMeters()
	vGraze := math.Sqrt(2 * mu * rp / (r0 * (rp + r0)))
	hint := 2.5 * math.Max(v0-vGraze, 1)
	dv, ok := solveDv(0, hint)
	if !ok {
		return nil, ErrDeorbitNoSolution
	}
	tilt := 0.0
	if math.Abs(lastOff.cross) > deorbitCrossTolM {
		t0, x0 := 0.0, lastOff.cross
		t1 := 0.5 * rad
		converged := false
		for i := 0; i < 8; i++ {
			d1, ok := solveDv(t1, dv)
			if !ok {
				return nil, ErrDeorbitNoSolution
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
			return nil, ErrDeorbitNoSolution
		}
		tilt = t1
	}

	n := nodeFor(c, st0, tS, dv, tilt)
	w.replaceAdvisoryNode(c, AdvisoryKeyDeorbit)
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
	pt, ok := w.computePlannedAim(c, n)
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
