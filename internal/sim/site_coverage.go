package sim

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Forward site-coverage sampler (ADR 0065 decision 4). It answers "is a
// point on a body's surface linked to a ground station at every sample
// of the coming lap?" by running the production connectivityFull at each
// sample time, never a second link formula.

const (
	// siteCovMinWall caps recomputes at one per this much wall clock; at
	// 100000x a sim hour is under one tick, so this is the cap that binds.
	siteCovMinWall = 2 * time.Second
	siteCovBucket  = time.Hour // sim-time bucket of the memo key
	siteProbeID    = uint64(1) << 62
)

// siteCovWallNow is the wall clock the recompute cap reads (a test seam).
var siteCovWallNow = time.Now

// siteCoverage is the sampled forward lap: Covered[i] is whether the site
// was linked at Epoch + i*StepS.
type siteCoverage struct {
	Epoch      time.Time
	StepS      float64
	Covered    []bool
	RelayCount int
}

// Fraction is the covered share of the samples (0 for an empty memo).
func (s siteCoverage) Fraction() float64 {
	if len(s.Covered) == 0 {
		return 0
	}
	n := 0
	for _, c := range s.Covered {
		if c {
			n++
		}
	}
	return float64(n) / float64(len(s.Covered))
}

// siteCovMemo is World's transient cache of the last sample.
type siteCovMemo struct {
	key      string
	params   string
	at       time.Time // wall clock of the last compute
	memo     siteCoverage
	ok       bool
	computes int // test counter
}

// siteRelay is one relay the sampler places at each sample time: a flying
// vessel (own or a partner's ghost) coasts by Kepler from its state; a
// landed one is a fixed body-fixed point (ADR 0066 decisions 11, 13).
type siteRelay struct {
	key      string // memo-key identity; a ghost's carries its owner so ids never collide
	rangeM   float64
	state    physics.StateVector // primary-relative, at the sim clock (flying)
	landed   bool
	lat, lon float64 // landed only
}

// siteRelays are the controllable relay-antenna vessels orbiting or parked
// on primaryID in the viewed system: the slate's, then partners' ghosts.
func (w *World) siteRelays(primaryID string) []siteRelay {
	var out []siteRelay
	sys := w.System()
	body := sys.FindBody(primaryID)
	for _, c := range w.Crafts {
		if c == nil || c.SystemIdx != w.SystemIdx || c.AntennaKind != spacecraft.AntennaRelay ||
			!c.Controllable || c.Crashed || c.Primary.ID != primaryID {
			continue
		}
		r := siteRelay{key: fmt.Sprint(c.ID), rangeM: c.AntennaRangeM, state: c.State}
		if c.Landed {
			if body == nil {
				continue
			}
			u := c.State.R.Unit()
			v := render.Vec3{X: u.X, Y: u.Y, Z: u.Z}
			r.landed = true
			r.lat, r.lon = render.WorldToBodyFixed(*body, v, w.Clock.SimTime)
		}
		out = append(out, r)
	}
	for _, g := range w.Ghosts {
		if !ghostIsRelay(g) || g.PrimaryID != primaryID {
			continue
		}
		out = append(out, siteRelay{
			key: g.Owner + "/" + fmt.Sprint(g.CraftID), rangeM: g.AntennaRangeM,
			state:  physics.StateVector{R: g.RelPos, V: g.Vel, M: 1},
			landed: g.Landed, lat: g.LatDeg, lon: g.LonDeg,
		})
	}
	return out
}

func (w *World) siteCovKey(primaryID string, relays []siteRelay) string {
	var parts []string
	sys := w.System()
	mu := sys.FindBody(primaryID).GravitationalParameter()
	for _, c := range relays {
		if c.landed {
			parts = append(parts, fmt.Sprintf("%s:L%.2f/%.2f", c.key, c.lat, c.lon))
			continue
		}
		el := orbital.ElementsFromState(c.state.R, c.state.V, mu)
		d := 180 / math.Pi
		parts = append(parts, fmt.Sprintf("%s:%.0f/%.3f/%.1f/%.1f/%.1f", c.key,
			math.Round(el.A/1000), math.Round(el.E*1000)/1000,
			math.Round(el.I*d*10)/10, math.Round(el.Omega*d*10)/10, math.Round(el.Arg*d*10)/10))
	}
	sort.Strings(parts)
	return fmt.Sprintf("%s|%d|%s", primaryID, w.Clock.SimTime.Unix()/int64(siteCovBucket/time.Second), strings.Join(parts, ","))
}

// SiteCoverage samples whether the surface point (lat, lon) of primaryID is
// linked to a ground station at each of lapS/sampleS+1 times from now,
// through the relays at that body: the slate's (landed ones as fixed
// points) and partners' (other bodies' relays are left out, a known
// under-count). ok=false for an
// unknown body or non-positive lap/step. The memo recomputes when the
// relay set, their rounded elements or the sim hour change, and at most
// once per siteCovMinWall of wall clock.
func (w *World) SiteCoverage(primaryID string, latDeg, lonDeg, lapS, sampleS float64) (siteCoverage, bool) {
	sys := w.System()
	body := sys.FindBody(primaryID)
	if body == nil || lapS <= 0 || sampleS <= 0 {
		return siteCoverage{}, false
	}
	relays := w.siteRelays(primaryID)
	params := fmt.Sprintf("%s|%v|%v|%v|%v", primaryID, latDeg, lonDeg, lapS, sampleS)
	key := w.siteCovKey(primaryID, relays)
	m := &w.siteCov
	now := siteCovWallNow()
	if m.ok && m.params == params {
		if m.key == key || now.Sub(m.at) < siteCovMinWall {
			return m.memo, true
		}
	}

	t0 := w.Clock.SimTime
	n := int(lapS/sampleS) + 1
	memo := siteCoverage{Epoch: t0, StepS: sampleS, Covered: make([]bool, n), RelayCount: len(relays)}
	stationBodies := map[string]bool{}
	for _, st := range w.GroundStations {
		if sys.FindBody(st.BodyID) != nil {
			stationBodies[st.BodyID] = true
		}
	}
	mu := body.GravitationalParameter()
	R := body.RadiusMeters()
	nodes := make([]commNode, 0, len(w.GroundStations)+len(relays)+1)
	occ := make([]physics.OccluderBody, len(sys.Bodies))
	for i := 0; i < n; i++ {
		t := t0.Add(time.Duration(float64(i) * sampleS * float64(time.Second)))
		for j := range sys.Bodies {
			occ[j] = physics.OccluderBody{Center: w.BodyPositionAt(sys.Bodies[j], t), Radius: sys.Bodies[j].RadiusMeters()}
		}
		bodyPos := w.BodyPositionAt(*body, t)
		nodes = nodes[:0]
		for _, st := range w.GroundStations {
			sb := sys.FindBody(st.BodyID)
			if sb == nil {
				continue
			}
			nodes = append(nodes, commNode{
				pos:      stationPosFrom(w.BodyPositionAt(*sb, t), st, *sb, t),
				rangeM:   st.AntennaRangeM,
				forwards: true,
				station:  true,
				bodyID:   st.BodyID,
			})
		}
		for _, c := range relays {
			r := c.state.R
			if c.landed {
				r, _ = surfacePointState(*body, c.lat, c.lon, t)
			} else if s, ok := physics.KeplerStep(c.state, mu, float64(i)*sampleS); ok {
				r = s.R
			}
			nodes = append(nodes, commNode{
				pos:      bodyPos.Add(r),
				rangeM:   c.rangeM,
				forwards: true,
				bodyID:   primaryID,
				nearHome: stationBodies[primaryID] && r.Norm() <= nearHomeRadiiFactor*R,
			})
		}
		dir := render.BodyFixedToWorld(*body, latDeg, lonDeg, t)
		nodes = append(nodes, commNode{
			pos:     bodyPos.Add(orbital.Vec3{X: R * dir.X, Y: R * dir.Y, Z: R * dir.Z}),
			rangeM:  spacecraft.AntennaRangeDirectBasic,
			probe:   true,
			craftID: siteProbeID,
			bodyID:  primaryID,
		})
		memo.Covered[i] = connectivityFull(nodes, occ).connected[siteProbeID]
	}
	*m = siteCovMemo{key: key, params: params, at: now, memo: memo, ok: true, computes: m.computes + 1}
	return memo, true
}

// CoverageSubject is the surface point the COVERAGE notice follows when a
// site, landed vessel or landed ghost is targeted (ADR 0066 decision 11).
type CoverageSubject struct {
	BodyID   string
	Label    string
	Lat, Lon float64
	LapS     float64
}

// coverageLapCapS caps the aimed-site lap; the rung keeps its authored lap.
const coverageLapCapS = 48 * 3600

// TargetCoverage resolves the targeted surface point in the viewed system and
// the lap to sample: the longest relay period at that body, capped at 48 h.
// ok=false when the target is not a surface point, or the body has no relay.
func (w *World) TargetCoverage() (CoverageSubject, bool) {
	sys := w.System()
	var s CoverageSubject
	var bodyName string
	switch w.Target.Kind {
	case TargetSite:
		site, found := w.targetSite()
		if !found || site.System != sys.Name {
			return s, false
		}
		bodyName, s.Label, s.Lat, s.Lon = site.Body, site.Name, site.LatDeg, site.LonDeg
	case TargetCraft:
		c, _, found := w.ResolveTargetCraft()
		if !found || !c.Landed || c.SystemIdx != w.SystemIdx {
			return s, false
		}
		bodyName, s.Label = c.Primary.EnglishName, c.Name
		s.Lat, s.Lon = c.SurfaceLatLon()
	case TargetGhost:
		g, p, found := w.ResolveTargetGhost()
		if !found || !g.Landed {
			return s, false
		}
		bodyName, s.Label, s.Lat, s.Lon = p.EnglishName, g.Name, g.LatDeg, g.LonDeg
	default:
		return s, false
	}
	for _, b := range sys.Bodies {
		if b.EnglishName == bodyName {
			s.BodyID = b.ID
		}
	}
	body := sys.FindBody(s.BodyID)
	if body == nil {
		return CoverageSubject{}, false
	}
	relays := w.siteRelays(s.BodyID)
	if len(relays) == 0 {
		return CoverageSubject{}, false
	}
	mu := body.GravitationalParameter()
	lap := 0.0
	for _, r := range relays {
		if r.landed {
			continue
		}
		el := orbital.ElementsFromState(r.state.R, r.state.V, mu)
		if el.A > 0 && el.E < 1 {
			lap = math.Max(lap, 2*math.Pi*math.Sqrt(el.A*el.A*el.A/mu))
		}
	}
	if lap == 0 {
		lap = 24 * 3600 // only landed relays: they do not move, a day shows the body turning
	}
	s.LapS = math.Min(math.Ceil(lap/60)*60, coverageLapCapS)
	return s, true
}

// LinkedSource is one live-network member seen from a body, for the linked-now
// overlay: its body-fixed unit direction from the body's centre, distance from
// that centre and rated range.
type LinkedSource struct {
	Dir           render.Vec3
	DistM, RangeM float64
}

// LinkedNow lists the live-network members as seen from primaryID at the
// rotation clock (the texture's frame), and whether the body sits inside the
// home blanket (it has a ground station, so every surface dot links, as
// commLinked grants a near-home probe). ok=false without a CommGraph or body.
func (w *World) LinkedNow(primaryID string) (srcs []LinkedSource, blanket, ok bool) {
	sys := w.System()
	body := sys.FindBody(primaryID)
	if body == nil || w.CommGraph == nil {
		return nil, false, false
	}
	for _, st := range w.GroundStations {
		if st.BodyID == primaryID {
			blanket = true
		}
	}
	centre := w.BodyPosition(*body)
	for _, n := range w.CommGraph.Net {
		rel := n.Pos.Sub(centre)
		d := rel.Norm()
		if d == 0 || n.RangeM <= 0 {
			continue
		}
		u := rel.Unit()
		lat, lon := render.WorldToBodyFixed(*body, render.Vec3{X: u.X, Y: u.Y, Z: u.Z}, w.Clock.RotationTime)
		la, lo := lat*math.Pi/180, lon*math.Pi/180
		srcs = append(srcs, LinkedSource{
			Dir:    render.Vec3{X: math.Cos(la) * math.Cos(lo), Y: math.Cos(la) * math.Sin(lo), Z: math.Sin(la)},
			DistM:  d,
			RangeM: n.RangeM,
		})
	}
	return srcs, blanket, true
}

// Linked says whether a surface dot at body-fixed unit vector (x, y, z) on a
// body of radius R sees any source: in range of a basic direct antenna (the
// link range is the geometric mean, as commLinkRangeM) and above the dot's
// horizon (the body's own limb is the only occluder considered).
func Linked(srcs []LinkedSource, R, x, y, z float64) bool {
	for _, s := range srcs {
		c := s.DistM * (x*s.Dir.X + y*s.Dir.Y + z*s.Dir.Z) // source height along the dot's vertical
		if c < R {
			continue
		}
		d2 := s.DistM*s.DistM + R*R - 2*R*c
		if r := commLinkRangeM(s.RangeM, spacecraft.AntennaRangeDirectBasic); d2 <= r*r {
			return true
		}
	}
	return false
}
