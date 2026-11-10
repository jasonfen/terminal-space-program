// Package sim - landing zones (ADR 0068): the numbered LZs of a site, the
// shipped zones, and the per-vessel claim `N` and `B` make. One-line reason
// for a new file: a new production concept (LZ model) beside sites.go.

package sim

import (
	"fmt"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Playtest-tuned constants (ADR 0068, "Playtest-tuned constants").
const (
	SiteLZSpacingM = 100.0 // honeycomb centre to centre
	SiteLZRadiusM  = 43.0  // an LZ is a circle 86 m across
	SiteMaxLZs     = 9     // every LZ is one digit in the picture
	// LZMapMinCells: the map draws LZ circles once 86 m spans this many cells
	// (21.5 m a cell or finer); further out a site is its one glyph.
	LZMapMinCells = 4.0

	lc13OffsetM    = 310.0 // LZ-2 from LZ-1 at the Cape, as the real pair
	lc13BearingDeg = 315.0 // northwest
)

// LZLayout says where a site's LZs sit around its LZ-1.
type LZLayout int

const (
	LayoutHoneycomb LZLayout = iota // decision 3: hex packing, 100 m
	LayoutLC13                      // decision 9: the Cape's real pair
)

// LZs is the site's LZ count; a site that never set one has one (a flag is
// its own LZ-1).
func (s Site) LZs() int {
	if s.LZCount < 1 {
		return 1
	}
	return min(s.LZCount, SiteMaxLZs)
}

// lzOffsetM is LZ n's offset from LZ-1 in metres, east and north (n from 1).
func (s Site) lzOffsetM(n int) (east, north float64) {
	pol := func(r, bearingDeg float64) (float64, float64) {
		return r * math.Sin(bearingDeg*rad), r * math.Cos(bearingDeg*rad)
	}
	if s.Layout == LayoutLC13 {
		if n == 2 {
			return pol(lc13OffsetM, lc13BearingDeg)
		}
		return 0, 0
	}
	switch {
	case n >= 2 && n <= 7: // the ring: LZ-2 due north, then clockwise every 60 degrees
		return pol(SiteLZSpacingM, float64(n-2)*60)
	case n == 8: // second ring: a corner due north
		return pol(2*SiteLZSpacingM, 0)
	case n == 9: // ...and the midpoint clockwise of it, 173 m out at 30 degrees
		return pol(math.Sqrt(3)*SiteLZSpacingM, 30)
	}
	return 0, 0
}

// lzLatLon is LZ n's position: LZ-1's point moved the offset along a great
// circle on a body of radiusM. Within 1 degree of a pole "north" is the 0
// degree meridian.
func (s Site) lzLatLon(n int, radiusM float64) (lat, lon float64) {
	east, north := s.lzOffsetM(n)
	d := math.Hypot(east, north)
	if d == 0 || radiusM <= 0 {
		return s.LatDeg, s.LonDeg
	}
	la, lo := s.LatDeg*rad, s.LonDeg*rad
	p := [3]float64{math.Cos(la) * math.Cos(lo), math.Cos(la) * math.Sin(lo), math.Sin(la)}
	e := [3]float64{-math.Sin(lo), math.Cos(lo), 0}
	nn := [3]float64{-math.Sin(la) * math.Cos(lo), -math.Sin(la) * math.Sin(lo), math.Cos(la)}
	if math.Abs(s.LatDeg) >= 89 {
		// the 0 degree meridian's direction, made exactly tangent
		d := p[0]
		nn = [3]float64{1 - d*p[0], -d * p[1], -d * p[2]}
		m := math.Sqrt(nn[0]*nn[0] + nn[1]*nn[1] + nn[2]*nn[2])
		nn = [3]float64{nn[0] / m, nn[1] / m, nn[2] / m}
		e = [3]float64{p[1]*nn[2] - p[2]*nn[1], p[2]*nn[0] - p[0]*nn[2], p[0]*nn[1] - p[1]*nn[0]}
	}
	ang := d / radiusM
	var q [3]float64
	for i := range q {
		t := (east*e[i] + north*nn[i]) / d
		q[i] = p[i]*math.Cos(ang) + t*math.Sin(ang)
	}
	return math.Asin(math.Max(-1, math.Min(1, q[2]))) / rad, math.Atan2(q[1], q[0]) / rad
}

// lzLabel is the on-screen name of LZ n (decision 4). A one-LZ flag keeps its
// bare name; a site with several says which.
func (s Site) lzLabel(n int) string {
	if s.LZs() == 1 {
		return s.Name
	}
	return fmt.Sprintf("%s LZ-%d", s.Name, n)
}

// shippedZoneIDBase keeps the reserved ids clear of the NextSiteID counter.
const shippedZoneIDBase = uint64(1) << 62

// shippedZones are the sites every game has (decisions 9, 9b): a Go-side
// table, not the body catalog (body_catalog_hash stays put), never saved and
// never in the site ledger. LZ-1 of each is the site's point. Sources are in
// the S2 log; Kern's place is 1 km due east of the default pad (0N 0E).
var shippedZones = []Site{
	{ID: shippedZoneIDBase + 1, Body: "Earth", System: "Sol", Kind: SiteZone, Name: "LC-13",
		LatDeg: 28.4858, LonDeg: -80.5444, LZCount: 2, Layout: LayoutLC13},
	{ID: shippedZoneIDBase + 2, Body: "Kern", System: "Lumen", Kind: SiteZone, Name: "Kern LZ",
		LatDeg: 0, LonDeg: 0.0955, LZCount: 2},
	{ID: shippedZoneIDBase + 3, Body: "Moon", System: "Sol", Kind: SiteZone, Name: "Tranquility Base",
		LatDeg: 0.67416, LonDeg: 23.47314, LZCount: 2},
	{ID: shippedZoneIDBase + 4, Body: "Moon", System: "Sol", Kind: SiteZone, Name: "Sinus Medii",
		LatDeg: 0.4743, LonDeg: -1.4275, LZCount: 2},
}

// shippedZoneByID finds a shipped zone.
func shippedZoneByID(id uint64) (Site, bool) {
	for _, s := range shippedZones {
		if s.ID == id {
			return s, true
		}
	}
	return Site{}, false
}

// ShippedZones are the zones every game has, for drawing.
func (w *World) ShippedZones() []Site { return append([]Site(nil), shippedZones...) }

// AllSites is your sites plus the shipped zones: what a lookup or a menu sees.
// The save and the ledger read w.Sites alone.
func (w *World) AllSites() []Site {
	return append(append([]Site(nil), w.Sites...), shippedZones...)
}

// siteRadiusM is the radius of the body a site sits on, 0 if unknown.
func (w *World) siteRadiusM(s Site) float64 {
	for _, sys := range w.Systems {
		if sys.Name != s.System {
			continue
		}
		for _, b := range sys.Bodies {
			if b.EnglishName == s.Body {
				return b.RadiusMeters()
			}
		}
	}
	return 0
}

// onSiteBody reports whether v is on the body s sits on.
func (w *World) onSiteBody(v *spacecraft.Spacecraft, s Site) bool {
	return v != nil && v.Primary.EnglishName == s.Body && w.systemForCraft(v).Name == s.System
}

// claimMatches reports whether v holds a claim on site s.
func claimMatches(v *spacecraft.Spacecraft, s Site) bool {
	return v.LZClaim.N > 0 && v.LZClaim.SiteID == s.ID && v.LZClaim.Owner == s.Owner
}

// lzTaken is decision 5's test: a vessel (landed, or crashed and not cleared)
// inside LZ n's circle, or a vessel other than self holding a claim on it.
func (w *World) lzTaken(s Site, n int, self *spacecraft.Spacecraft) bool {
	r := w.siteRadiusM(s)
	lat, lon := s.lzLatLon(n, r)
	for _, v := range w.Crafts {
		if v == nil || v == self || !w.onSiteBody(v, s) {
			continue
		}
		if claimMatches(v, s) && v.LZClaim.N == n {
			return true
		}
		if v.Landed || v.Crashed {
			vl, vo := v.SurfaceLatLon()
			if insideLZ(HaversineM(vl, vo, lat, lon, r)) {
				return true
			}
		}
	}
	return false
}

// insideLZ is the circle test: exactly the radius is inside (decision 7).
func insideLZ(distM float64) bool { return distM <= SiteLZRadiusM }

// lowestFreeLZ is the lowest LZ number of s nobody holds, 0 if all are taken.
func (w *World) lowestFreeLZ(s Site, self *spacecraft.Spacecraft) int {
	for n := 1; n <= s.LZs(); n++ {
		if !w.lzTaken(s, n, self) {
			return n
		}
	}
	return 0
}

// landingSiteOf is the LZ-resolving lookup every surface-point reader calls
// (ADR 0068 decision 1, guard 8): siteOfTarget with the position and name of
// the LZ the vessel holds, else the lowest free one (LZ-1 if all are taken).
func (w *World) landingSiteOf(t Target, self *spacecraft.Spacecraft) (Site, bool) {
	s, n, ok := w.landingLZOf(t, self)
	if !ok {
		return s, false
	}
	s.LatDeg, s.LonDeg = s.lzLatLon(n, w.siteRadiusM(s))
	s.Name = s.lzLabel(n)
	return s, true
}

// landingLZOf is landingSiteOf's choice: the site and the LZ number n.
func (w *World) landingLZOf(t Target, self *spacecraft.Spacecraft) (s Site, n int, ok bool) {
	s, ok = w.siteOfTarget(t)
	if !ok {
		return s, 0, false
	}
	if self != nil && claimMatches(self, s) {
		n = min(self.LZClaim.N, s.LZs())
	} else if n = w.lowestFreeLZ(s, self); n == 0 {
		n = 1
	}
	return s, n, true
}

// LZPlace is one LZ of a site in the system's world frame: its centre and the
// unit vector straight up from the ground there.
type LZPlace struct {
	N   int
	Pos orbital.Vec3
	Up  orbital.Vec3
}

// LZPlaces places every LZ of s (decision 8's circles), nil if the site's
// body is not in the current system.
func (w *World) LZPlaces(s Site) []LZPlace {
	sys := w.System()
	if s.System != sys.Name {
		return nil
	}
	for _, b := range sys.Bodies {
		if b.EnglishName != s.Body {
			continue
		}
		bp := w.BodyPosition(b)
		out := make([]LZPlace, 0, s.LZs())
		for n := 1; n <= s.LZs(); n++ {
			lat, lon := s.lzLatLon(n, b.RadiusMeters())
			r, _ := surfacePointState(b, lat, lon, w.Clock.SimTime)
			out = append(out, LZPlace{N: n, Pos: bp.Add(r), Up: r.Unit()})
		}
		return out
	}
	return nil
}

// ActiveLandingLZ is the site the active vessel is aimed at and the LZ number
// it is flying to; ok is false when the aim is no site.
func (w *World) ActiveLandingLZ() (s Site, n int, ok bool) {
	if w.Target.Kind != TargetSite {
		return Site{}, 0, false
	}
	return w.landingLZOf(w.Target, w.ActiveCraft())
}

// NoFreeLZError is the `N`/`B` refusal when every LZ of the aimed site is taken.
type NoFreeLZError struct{ Site string }

func (e *NoFreeLZError) Error() string { return e.Site + ": no free LZ" }

// claimLZ makes c's claim on the lowest free LZ of its aimed site, once per
// press (decision 5). fresh is false when c already held one or aims at no
// site on its body (the caller's own refusals follow).
func (w *World) claimLZ(c *spacecraft.Spacecraft) (fresh bool, err error) {
	if c == nil {
		return false, nil
	}
	t := c.Target
	if w.ActiveCraft() == c {
		t = w.Target
	}
	if t.Kind != TargetSite {
		return false, nil
	}
	s, ok := w.siteOfTarget(t)
	if !ok || !w.onSiteBody(c, s) || claimMatches(c, s) {
		return false, nil
	}
	n := w.lowestFreeLZ(s, c)
	if n == 0 {
		return false, &NoFreeLZError{Site: s.Name}
	}
	c.LZClaim = spacecraft.LZClaim{Owner: s.Owner, SiteID: s.ID, N: n}
	w.recordRowFor(c, "lz claim: "+s.lzLabel(n))
	return true, nil
}

// PlanDeorbitOnly is `B`: PlanDeorbitOntoSite with the LZ claimed first
// (decision 5) and let go again if no burn was planted.
func (w *World) PlanDeorbitOnly() (*DeorbitPlan, error) {
	c := w.ActiveCraft()
	fresh, err := w.claimLZ(c)
	if err != nil {
		return nil, err
	}
	plan, err := w.PlanDeorbitOntoSite()
	if err != nil && fresh {
		c.LZClaim = spacecraft.LZClaim{}
	}
	return plan, err
}

// lzAt finds the LZ of any site on c's body that (lat, lon) lies inside
// (decision 7): yours, shipped and partners'. dist is the distance to its centre.
func (w *World) lzAt(c *spacecraft.Spacecraft, lat, lon float64) (s Site, n int, dist float64, ok bool) {
	all := w.AllSites()
	for _, p := range w.PartnerSites {
		all = append(all, p.Site)
	}
	dist = math.Inf(1)
	for _, cand := range all {
		if cand.Kind == SiteWreck || !w.onSiteBody(c, cand) {
			continue
		}
		r := w.siteRadiusM(cand)
		for i := 1; i <= cand.LZs(); i++ {
			ll, lo := cand.lzLatLon(i, r)
			if d := HaversineM(lat, lon, ll, lo, r); insideLZ(d) && d < dist {
				s, n, dist, ok = cand, i, d, true
			}
		}
	}
	return
}

// StandingVessel is a landed vessel of yours inside an LZ of a site: where it
// stands (world frame) and its colour, for decision 8's picture.
type StandingVessel struct {
	Pos   orbital.Vec3
	Color string
	LZ    int
}

// StandingOn lists your landed vessels, the active one apart, that stand
// inside an LZ circle of s.
func (w *World) StandingOn(s Site) []StandingVessel {
	var out []StandingVessel
	r := w.siteRadiusM(s)
	for _, v := range w.Crafts {
		if v == nil || v == w.ActiveCraft() || !v.Landed || !w.onSiteBody(v, s) {
			continue
		}
		vl, vo := v.SurfaceLatLon()
		for n := 1; n <= s.LZs(); n++ {
			lat, lon := s.lzLatLon(n, r)
			if insideLZ(HaversineM(vl, vo, lat, lon, r)) {
				out = append(out, StandingVessel{Pos: w.BodyPosition(v.Primary).Add(v.State.R), Color: v.Color, LZ: n})
				break
			}
		}
	}
	return out
}

// LZCirclesOnMap says the map is zoomed far enough (scale in canvas dots per
// metre, two dots to a cell) for an LZ circle to be at least LZMapMinCells
// across; the equal case draws (decision 8).
func LZCirclesOnMap(scale float64) bool {
	return 2*SiteLZRadiusM*scale/2 >= LZMapMinCells
}
