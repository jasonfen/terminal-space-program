// Package sim - land-beside readouts (ADR 0066 decisions 9 and 10): the surface
// picture TARGET carries while you fly toward a site, a landed vessel or a
// landed ghost on your primary. One-line reason for a new file: a new
// production concept (surface geometry against a target), not a slice of an
// existing file.

package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const rad = math.Pi / 180 // degrees to radians

// HaversineM is the great-circle distance in metres between two lat/lon
// points (degrees) on a sphere of radiusM.
func HaversineM(lat1, lon1, lat2, lon2, radiusM float64) float64 {
	dLat, dLon := (lat2-lat1)*rad, (lon2-lon1)*rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * radiusM * math.Asin(math.Min(1, math.Sqrt(a)))
}

// ForwardAzimuthDeg is the true initial bearing from point 1 to point 2, in
// [0, 360), clockwise from north.
func ForwardAzimuthDeg(lat1, lon1, lat2, lon2 float64) float64 {
	dLon := (lon2 - lon1) * rad
	y := math.Sin(dLon) * math.Cos(lat2*rad)
	x := math.Cos(lat1*rad)*math.Sin(lat2*rad) - math.Sin(lat1*rad)*math.Cos(lat2*rad)*math.Cos(dLon)
	return math.Mod(math.Atan2(y, x)/rad+360, 360)
}

// ImpactOffsetM decomposes where the impact point lands against the site along
// the ground track (the bearing from the sub-point to the impact point):
// longM is positive beyond the site, crossM positive to the right of the track.
// ok=false when the track is undefined (the impact is the sub-point). shortcut:
// the track bearing is taken at the sub-point, not re-measured at the site;
// upgrade if a descent crosses thousands of km of arc.
func ImpactOffsetM(subLat, subLon, impLat, impLon, siteLat, siteLon, radiusM float64) (longM, crossM float64, ok bool) {
	if HaversineM(subLat, subLon, impLat, impLon, radiusM) == 0 {
		return 0, 0, false
	}
	d := HaversineM(siteLat, siteLon, impLat, impLon, radiusM)
	if d == 0 {
		return 0, 0, true
	}
	track := ForwardAzimuthDeg(subLat, subLon, impLat, impLon)
	rel := (ForwardAzimuthDeg(siteLat, siteLon, impLat, impLon) - track) * rad
	return d * math.Cos(rel), d * math.Sin(rel), true
}

// LookDownDeg is the LPD angle (P64): the look-down angle of the line of sight
// to the site from the vessel's forward line, the perpendicular to the nose in
// the vertical plane through the site, positive downward. rVessel and rSite are
// primary-relative; nose is the unit nose direction.
func LookDownDeg(rVessel, rSite, nose orbital.Vec3) (float64, bool) {
	up := rVessel.Unit()
	d := rSite.Sub(rVessel)
	toward := d.Sub(up.Scale(d.Dot(up)))
	if toward.Norm() == 0 || up.Norm() == 0 {
		return 0, false
	}
	toward = toward.Unit()
	los := math.Atan2(-d.Dot(up), d.Dot(toward))
	lean := math.Atan2(nose.Dot(toward), nose.Dot(up)) // nose off the zenith, toward the site
	return math.Remainder(los-lean, 2*math.Pi) / rad, true
}

// LandBeside is TARGET's surface picture for a surface target.
type LandBeside struct {
	GroundM    float64
	BearingDeg float64
	BearingOK  bool // false at the equal point: no bearing to nothing
	ImpactOK   bool // PredictImpact found the ground
	MissM      float64
	CrossM     float64
	LPDDeg     float64
	LPDOK      bool
	MissOK     bool
	// Planned: miss/cross read the planted deorbit node's stop point, not yet
	// flown (decision 21: tagged (plan)). Stop: they read the 90 % brake's stop
	// point of a vessel that can stop, not the free fall.
	Planned bool
	Stop    bool
}

// surfaceTargetLatLon is the aimed surface point on the active vessel's primary:
// a site, a landed vessel or a landed ghost. ok=false otherwise.
func (w *World) surfaceTargetLatLon(c *spacecraft.Spacecraft) (lat, lon float64, ok bool) {
	switch w.Target.Kind {
	case TargetSite:
		s, found := w.targetSite()
		if !found || s.Body != c.Primary.EnglishName || s.System != w.System().Name {
			return 0, 0, false
		}
		return s.LatDeg, s.LonDeg, true
	case TargetCraft:
		t, _, found := w.ResolveTargetCraft()
		if !found || !t.Landed || t.Primary.ID != c.Primary.ID {
			return 0, 0, false
		}
		lat, lon = t.SurfaceLatLon()
		return lat, lon, true
	case TargetGhost:
		g, p, found := w.ResolveTargetGhost()
		if !found || !g.Landed || p.ID != c.Primary.ID {
			return 0, 0, false
		}
		return g.LatDeg, g.LonDeg, true
	}
	return 0, 0, false
}

// LandBesideReading reads the surface picture for the active vessel while it
// flies (ADR 0066 decision 10: no phase gate). ground and bearing read from
// orbit; miss, cross and lpd fill in when PredictImpact finds the ground.
func (w *World) LandBesideReading() (LandBeside, bool) {
	c := w.ActiveCraft()
	if c == nil || c.Landed || c.Crashed || c.State.R.Norm() == 0 {
		return LandBeside{}, false
	}
	siteLat, siteLon, ok := w.surfaceTargetLatLon(c)
	if !ok {
		return LandBeside{}, false
	}
	radius := c.Primary.RadiusMeters()
	subLat, subLon := render.WorldToBodyFixed(c.Primary, unitV(c.State.R), w.Clock.SimTime)
	lb := LandBeside{GroundM: HaversineM(subLat, subLon, siteLat, siteLon, radius)}
	// Under half a metre there is no direction worth reading; exact zero is
	// not reachable on every platform (CI's amd64 left 1e-10 m directly above).
	if lb.GroundM >= 0.5 {
		lb.BearingDeg, lb.BearingOK = ForwardAzimuthDeg(subLat, subLon, siteLat, siteLon), true
	}
	// Decision 21: a vessel that can stop reads where it would STOP, not where
	// it would crash; before the planted burn that is the plan's stop point.
	canStop := canBrakeAtSite(c)
	if n, ok := deorbitNode(c); ok && canStop && c.ActiveBurn == nil {
		if pt, ok := w.plannedAim(c, n); ok {
			oLat, oLon := render.WorldToBodyFixed(c.Primary, unitV(pt.originR), pt.origin)
			aLat, aLon := render.WorldToBodyFixed(c.Primary, unitV(pt.R), pt.at)
			lb.MissM, lb.CrossM, lb.MissOK = ImpactOffsetM(oLat, oLon, aLat, aLon, siteLat, siteLon, radius)
			lb.Planned, lb.Stop = true, true
			return lb, true
		}
	}
	// Past the coast the autopilot's own forecast is the one truthful stop (the
	// landing view's marker draws it); off, miss: stays the hand-flyer's plain stop.
	if ap := c.Landing; ap != nil && ap.Engaged && ap.Phase != spacecraft.PhaseCoast && ap.ForecastOK {
		lb.MissM, lb.CrossM, lb.MissOK, lb.ImpactOK, lb.Stop = ap.ForecastAlongM, ap.ForecastCrossM, true, true, true
		rSite, _ := surfacePointState(c.Primary, siteLat, siteLon, w.Clock.SimTime)
		lb.LPDDeg, lb.LPDOK = LookDownDeg(c.State.R, rSite, c.CurrentAttitudeDir.Unit())
		return lb, true
	}
	imp, found := w.cachedImpact(c)
	if !found {
		return lb, true
	}
	lb.ImpactOK = true
	// the body turns while the vessel falls: read the impact point at the contact time
	aim, at := imp.Point, w.Clock.SimTime.Add(imp.TimeToImpact)
	if canStop {
		if pt, ok := w.liveAim(c); ok {
			aim, at, lb.Stop = pt.R, pt.at, true
		}
	}
	impLat, impLon := render.WorldToBodyFixed(c.Primary, unitV(aim), at)
	lb.MissM, lb.CrossM, lb.MissOK = ImpactOffsetM(subLat, subLon, impLat, impLon, siteLat, siteLon, radius)
	rSite, _ := surfacePointState(c.Primary, siteLat, siteLon, w.Clock.SimTime)
	lb.LPDDeg, lb.LPDOK = LookDownDeg(c.State.R, rSite, c.CurrentAttitudeDir.Unit())
	return lb, true
}

func unitV(v orbital.Vec3) render.Vec3 {
	u := v.Unit()
	return render.Vec3{X: u.X, Y: u.Y, Z: u.Z}
}

type impactCacheKey struct {
	craftID, bc, energy int64
	h                   [3]int64
	primary             string
}

type impactCache struct {
	has   bool
	key   impactCacheKey
	found bool
	point orbital.Vec3
	at    time.Time // impact time (found) or when the horizon has slid (not found)
}

// impactCacheTrusted: the forecast is a pure function of the orbit only when
// nothing is thrusting and drag cannot reach it (airless body, or periapsis
// above the atmosphere cutoff). Otherwise recompute every frame.
func impactCacheTrusted(c *spacecraft.Spacecraft) bool {
	if StackMidBurn(c) {
		return false
	}
	atm := c.Primary.Atmosphere
	if atm == nil {
		return true
	}
	mu := c.Primary.GravitationalParameter()
	h := c.State.R.Cross(c.State.V).Norm()
	e := math.Sqrt(math.Max(0, 1+2*(c.State.V.Dot(c.State.V)/2-mu/c.State.R.Norm())*h*h/(mu*mu)))
	return h*h/(mu*(1+e)) > c.Primary.RadiusMeters()+atm.CutoffAltitude
}

// cachedImpact is PredictImpact keyed on the orbit's constants (energy, h
// vector, ballistic coefficient) so TARGET's every-frame reading does not rerun
// a 30-minute forecast while the orbit holds still. A found impact is stored as an
// absolute time and lives until then, however far warp jumps; a "no impact"
// answer expires after a minute of sim time so the 30-minute horizon slides.
func (w *World) cachedImpact(c *spacecraft.Spacecraft) (ImpactPrediction, bool) {
	if !impactCacheTrusted(c) {
		return w.solveImpact(c)
	}
	mu := c.Primary.GravitationalParameter()
	h := c.State.R.Cross(c.State.V)
	k := impactCacheKey{
		craftID: int64(c.ID), primary: c.Primary.ID, bc: qz(c.EffectiveBallisticCoefficient(), 1e-3),
		energy: qz(c.State.V.Dot(c.State.V)/2-mu/c.State.R.Norm(), 20),
		h:      [3]int64{qz(h.X, 200), qz(h.Y, 200), qz(h.Z, 200)},
	}
	now := w.Clock.SimTime
	if cc := &w.impactCache; cc.has && cc.key == k && cc.at.After(now) {
		if !cc.found {
			return ImpactPrediction{}, false
		}
		return ImpactPrediction{Point: cc.point, TimeToImpact: cc.at.Sub(now)}, true
	}
	imp, found := w.solveImpact(c)
	at := now.Add(imp.TimeToImpact)
	if !found {
		at = now.Add(time.Minute)
	}
	w.impactCache = impactCache{has: true, key: k, found: found, point: imp.Point, at: at}
	return imp, found
}

func (w *World) solveImpact(c *spacecraft.Spacecraft) (ImpactPrediction, bool) {
	w.impactSolves++
	return PredictImpact(c, DescentPredictHorizon)
}
