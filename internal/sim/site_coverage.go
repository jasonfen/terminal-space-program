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

// siteRelays are the slate's controllable, flying relay-antenna vessels
// orbiting primaryID in the viewed system.
func (w *World) siteRelays(primaryID string) []*spacecraft.Spacecraft {
	var out []*spacecraft.Spacecraft
	for _, c := range w.Crafts {
		if c == nil || c.SystemIdx != w.SystemIdx || c.AntennaKind != spacecraft.AntennaRelay ||
			!c.Controllable || c.Landed || c.Crashed || c.Primary.ID != primaryID {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (w *World) siteCovKey(primaryID string, relays []*spacecraft.Spacecraft) string {
	var parts []string
	for _, c := range relays {
		el := orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
		d := 180 / math.Pi
		parts = append(parts, fmt.Sprintf("%d:%.0f/%.3f/%.1f/%.1f/%.1f", c.ID,
			math.Round(el.A/1000), math.Round(el.E*1000)/1000,
			math.Round(el.I*d*10)/10, math.Round(el.Omega*d*10)/10, math.Round(el.Arg*d*10)/10))
	}
	sort.Strings(parts)
	return fmt.Sprintf("%s|%d|%s", primaryID, w.Clock.SimTime.Unix()/int64(siteCovBucket/time.Second), strings.Join(parts, ","))
}

// SiteCoverage samples whether the surface point (lat, lon) of primaryID is
// linked to a ground station at each of lapS/sampleS+1 times from now,
// through the slate's relays at that body (other bodies' relays and
// landed vessels are left out, a known under-count). ok=false for an
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
			r := c.State.R
			if s, ok := physics.KeplerStep(c.State, mu, float64(i)*sampleS); ok {
				r = s.R
			}
			nodes = append(nodes, commNode{
				pos:      bodyPos.Add(r),
				rangeM:   c.AntennaRangeM,
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
