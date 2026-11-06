package sim

// New file: site_coverage.go is a new prod file (ADR 0065 S3 sampler).

import (
	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const siteTestLap, siteTestStep = 60.0, 60.0 // two samples: fast, geometry barely moves

// moonSiteWorld returns a world whose slate has no relays, plus the unit
// vector Moon->Earth and the Moon's (lat, lon) of the sub-Earth point and
// its antipode at SimTime.
func moonSiteWorld(t *testing.T) (w *World, dirE orbital.Vec3, nearLat, nearLon, farLat, farLon float64) {
	t.Helper()
	w = mustWorld(t)
	moon := siteBody(w, "moon")
	d := w.BodyPosition(siteBody(w, "earth")).Sub(w.BodyPosition(moon))
	dirE = d.Scale(1 / d.Norm())
	v := render.Vec3{X: dirE.X, Y: dirE.Y, Z: dirE.Z}
	nearLat, nearLon = render.WorldToBodyFixed(moon, v, w.Clock.SimTime)
	farLat, farLon = render.WorldToBodyFixed(moon, render.Vec3{X: -v.X, Y: -v.Y, Z: -v.Z}, w.Clock.SimTime)
	return
}

// addMoonCraft adds a controllable craft on a circular lunar orbit of radius
// rM along dir (velocity perpendicular, in the ecliptic plane).
func addMoonCraft(w *World, kind string, rangeM float64, dir orbital.Vec3, rM float64) *spacecraft.Spacecraft {
	moon := siteBody(w, "moon")
	c := spacecraft.NewFromLoadout("Relay-Tug")
	c.Primary = moon
	c.AntennaKind, c.AntennaRangeM, c.Controllable = kind, rangeM, true
	perp := orbital.Vec3{X: -dir.Y, Y: dir.X}
	perp = perp.Scale(1 / perp.Norm())
	c.State.R = dir.Scale(rM)
	c.State.V = perp.Scale(sqrtMuOverR(moon.GravitationalParameter(), rM))
	w.Crafts = append(w.Crafts, c)
	w.EnsureCraftIDs()
	return c
}

func sqrtMuOverR(mu, r float64) float64 { return math.Sqrt(mu / r) }

func siteBody(w *World, id string) bodies.CelestialBody {
	sys := w.System()
	return *sys.FindBody(id)
}

// Guard 7: no relays reads 0; a relay between site and Earth reads covered;
// the Moon between site and relay reads 0 (needs occlusion).
func TestSiteCoverageTruth(t *testing.T) {
	w, dirE, nLat, nLon, _, _ := moonSiteWorld(t)
	wall := time.Unix(1000, 0)
	siteCovWallNow = func() time.Time { wall = wall.Add(time.Hour); return wall }
	defer func() { siteCovWallNow = time.Now }()
	cov, ok := w.SiteCoverage("moon", nLat, nLon, siteTestLap, siteTestStep)
	if !ok || cov.Fraction() != 0 || cov.RelayCount != 0 {
		t.Fatalf("no relays: ok=%v frac=%v relays=%d, want 0", ok, cov.Fraction(), cov.RelayCount)
	}
	moonB := siteBody(w, "moon")
	rM := moonB.RadiusMeters() + 3e6
	addMoonCraft(w, spacecraft.AntennaRelay, spacecraft.AntennaRangeRelayCislunar, dirE, rM)
	cov, _ = w.SiteCoverage("moon", nLat, nLon, siteTestLap, siteTestStep)
	if cov.RelayCount != 1 || cov.Fraction() != 1 {
		t.Errorf("relay over the sub-Earth site: frac=%v relays=%d, want 1", cov.Fraction(), cov.RelayCount)
	}
	// Antipodal site: the Moon sits between it and the relay (and Earth).
	_, _, _, _, fLat, fLon := moonSiteWorld(t)
	cov, _ = w.SiteCoverage("moon", fLat, fLon, siteTestLap, siteTestStep)
	if cov.Fraction() != 0 {
		t.Errorf("Moon between site and relay: frac=%v, want 0", cov.Fraction())
	}
}

// Guard 8: for one sample time the sampler equals RecomputeCommGraph's
// HasConnection for a real direct-basic probe parked at the site.
func TestSiteCoverageMatchesSolver(t *testing.T) {
	w, dirE, nLat, nLon, fLat, fLon := moonSiteWorld(t)
	moon := siteBody(w, "moon")
	rM := moon.RadiusMeters() + 3e6
	addMoonCraft(w, spacecraft.AntennaRelay, spacecraft.AntennaRangeRelayCislunar, dirE, rM)
	for _, c := range []struct {
		name     string
		lat, lon float64
		wantAny  bool
	}{{"near", nLat, nLon, true}, {"far", fLat, fLon, false}} {
		cov, _ := w.SiteCoverage("moon", c.lat, c.lon, siteTestLap, siteTestStep)
		dir := render.BodyFixedToWorld(moon, c.lat, c.lon, w.Clock.SimTime)
		probe := addMoonCraft(w, spacecraft.AntennaDirect, spacecraft.AntennaRangeDirectBasic,
			orbital.Vec3{X: dir.X, Y: dir.Y, Z: dir.Z}, moon.RadiusMeters())
		w.RecomputeCommGraph()
		got := w.CommGraph.HasConnection(probe.ID)
		w.Crafts = w.Crafts[:len(w.Crafts)-1]
		if cov.Covered[0] != got || got != c.wantAny {
			t.Errorf("%s site: sampler=%v solver=%v want=%v", c.name, cov.Covered[0], got, c.wantAny)
		}
	}
}

// Guard 9 (key half): a still world recomputes once; a relay moved 10 km
// recomputes once more; inside the 2 s wall cap a changed key keeps the
// stored memo, and at exactly 2 s (the equal case) it recomputes.
func TestSiteCoverageMemo(t *testing.T) {
	w, dirE, nLat, nLon, _, _ := moonSiteWorld(t)
	wall := time.Unix(1000, 0)
	siteCovWallNow = func() time.Time { return wall }
	defer func() { siteCovWallNow = time.Now }()
	relay := addMoonCraft(w, spacecraft.AntennaRelay, spacecraft.AntennaRangeRelayCislunar, dirE, 4.7e6)
	call := func() { w.SiteCoverage("moon", nLat, nLon, siteTestLap, siteTestStep) }
	call()
	wall = wall.Add(10 * time.Second) // past the cap: only the key can hold the memo
	call()
	if n := w.siteCov.computes; n != 1 {
		t.Fatalf("two calls with nothing moving: %d computes, want 1", n)
	}
	move := func(dr float64) {
		relay.State.R = relay.State.R.Scale((relay.State.R.Norm() + dr) / relay.State.R.Norm())
	}
	move(1e4)
	call()
	if n := w.siteCov.computes; n != 2 {
		t.Fatalf("relay moved 10 km: %d computes, want 2", n)
	}
	move(1e4)
	wall = wall.Add(siteCovMinWall - time.Nanosecond)
	call()
	if n := w.siteCov.computes; n != 2 {
		t.Errorf("changed key inside the wall cap: %d computes, want 2", n)
	}
	wall = wall.Add(time.Nanosecond)
	call()
	if n := w.siteCov.computes; n != 3 {
		t.Errorf("changed key at exactly the wall cap: %d computes, want 3", n)
	}
}

// Decisions 11, 13: a partner's relay (flying or landed) and an own landed
// relay count in the sampler, and the sampler equals the solver for each, the
// solver seeing the ghost through RecomputeCommGraph.
func TestSiteCoverageSharedAndLandedRelays(t *testing.T) {
	w, dirE, nLat, nLon, fLat, fLon := moonSiteWorld(t)
	moon := siteBody(w, "moon")
	wall := time.Unix(1000, 0)
	siteCovWallNow = func() time.Time { wall = wall.Add(time.Hour); return wall }
	defer func() { siteCovWallNow = time.Now }()
	cov := func(lat, lon float64) siteCoverage {
		c, _ := w.SiteCoverage("moon", lat, lon, siteTestLap, siteTestStep)
		return c
	}
	surf := func(lat, lon float64) (r, v orbital.Vec3) { return surfacePointState(moon, lat, lon, w.Clock.SimTime) }

	// A partner's flying relay over the near site.
	rM := moon.RadiusMeters() + 3e6
	rel := dirE.Scale(rM)
	vel := orbital.Vec3{X: -dirE.Y, Y: dirE.X}.Scale(sqrtMuOverR(moon.GravitationalParameter(), rM))
	w.Ghosts = []Ghost{{Owner: "fp-b", CraftID: 1, PrimaryID: "moon", RelPos: rel, Vel: vel,
		AntennaKind: spacecraft.AntennaRelay, AntennaRangeM: spacecraft.AntennaRangeRelayCislunar, Controllable: true}}
	if c := cov(nLat, nLon); c.Fraction() != 1 || c.RelayCount != 1 {
		t.Errorf("partner relay over the site: frac=%v relays=%d, want 1, 1", c.Fraction(), c.RelayCount)
	}
	if c := cov(fLat, fLon); c.Fraction() != 0 {
		t.Errorf("Moon between far site and partner relay: frac=%v, want 0", c.Fraction())
	}
	// A partner's landed relay on the near side links a site at its own point.
	// (A site 1 degree away is not linked: the chord between two surface
	// points dips under the sphere, in the live CommNet as in the sampler.)
	r, v := surf(nLat, nLon)
	w.Ghosts = []Ghost{{Owner: "fp-b", CraftID: 1, PrimaryID: "moon", Landed: true, LatDeg: nLat, LonDeg: nLon,
		RelPos: r, Vel: v, AntennaKind: spacecraft.AntennaRelay, AntennaRangeM: spacecraft.AntennaRangeRelayCislunar, Controllable: true}}
	if c := cov(nLat, nLon); c.Fraction() != 1 || c.RelayCount != 1 {
		t.Errorf("landed partner relay at the site: frac=%v relays=%d, want 1, 1", c.Fraction(), c.RelayCount)
	}
	// Sampler equals solver for the ghost: a probe parked at the far side reads what sample 0 says.
	w.Ghosts = []Ghost{{Owner: "fp-b", CraftID: 1, PrimaryID: "moon", RelPos: rel, Vel: vel,
		AntennaKind: spacecraft.AntennaRelay, AntennaRangeM: spacecraft.AntennaRangeRelayCislunar, Controllable: true}}
	w.Ghosts[0].Pos = w.BodyPosition(moon).Add(rel)
	for _, c := range []struct{ lat, lon float64 }{{nLat, nLon}, {fLat, fLon}} {
		dir := render.BodyFixedToWorld(moon, c.lat, c.lon, w.Clock.SimTime)
		probe := addMoonCraft(w, spacecraft.AntennaDirect, spacecraft.AntennaRangeDirectBasic,
			orbital.Vec3{X: dir.X, Y: dir.Y, Z: dir.Z}, moon.RadiusMeters())
		w.RecomputeCommGraph()
		got := w.CommGraph.HasConnection(probe.ID)
		w.Crafts = w.Crafts[:len(w.Crafts)-1]
		if s := cov(c.lat, c.lon).Covered[0]; s != got {
			t.Errorf("site (%v,%v): sampler=%v solver=%v", c.lat, c.lon, s, got)
		}
	}
	// An own landed relay is a fixed node: it covers the site where it sits.
	w.Ghosts = nil
	lander := addMoonCraft(w, spacecraft.AntennaRelay, spacecraft.AntennaRangeRelayCislunar, dirE, moon.RadiusMeters())
	lander.Landed = true
	if c := cov(nLat, nLon); c.Fraction() != 1 || c.RelayCount != 1 {
		t.Errorf("own landed relay at the site: frac=%v relays=%d, want 1, 1", c.Fraction(), c.RelayCount)
	}
}

// Two relays with the same CraftID from different owners (ids are per player)
// are both counted, and a ghost's rounded elements are in the memo key.
func TestSiteCoverageKeyHasGhosts(t *testing.T) {
	w, dirE, _, _, _, _ := moonSiteWorld(t)
	moon := siteBody(w, "moon")
	rM := moon.RadiusMeters() + 3e6
	g := func(owner string, dr float64) Ghost {
		return Ghost{Owner: owner, CraftID: 1, PrimaryID: "moon", RelPos: dirE.Scale(rM + dr),
			Vel:         orbital.Vec3{X: -dirE.Y, Y: dirE.X}.Scale(sqrtMuOverR(moon.GravitationalParameter(), rM)),
			AntennaKind: spacecraft.AntennaRelay, AntennaRangeM: spacecraft.AntennaRangeRelayCislunar, Controllable: true}
	}
	addMoonCraft(w, spacecraft.AntennaRelay, spacecraft.AntennaRangeRelayCislunar, dirE, rM).ID = 1
	key := func() string { return w.siteCovKey("moon", w.siteRelays("moon")) }
	w.Ghosts = []Ghost{g("a", 0), g("b", 0)}
	if n := len(w.siteRelays("moon")); n != 3 {
		t.Fatalf("own relay + two same-id ghosts: %d relays, want 3", n)
	}
	k0 := key()
	w.Ghosts = []Ghost{g("a", 0), g("b", 1e4)}
	if key() == k0 {
		t.Error("a partner's relay burned 10 km higher left the memo key unchanged")
	}
}

// ADR 0066 decision 11: the aimed surface point resolves to its body, with a
// lap of the longest relay period (rounded up to the minute) capped at 48 h;
// no relay at the body, or no surface target, reads false.
func TestTargetCoverage(t *testing.T) {
	w, dirE, _, _, _, _ := moonSiteWorld(t)
	moon := siteBody(w, "moon")
	w.Sites = append(w.Sites, Site{ID: 1, Body: moon.EnglishName, System: w.System().Name, LatDeg: -90, Name: "Pole site"})
	w.SetTargetSite(1)
	if _, ok := w.TargetCoverage(); ok {
		t.Fatal("a site with no relay at its body must read false")
	}
	addMoonCraft(w, spacecraft.AntennaRelay, 1e9, dirE, 2.0e6)
	sub, ok := w.TargetCoverage()
	if !ok || sub.BodyID != "moon" || sub.Label != "Pole site" || sub.Lat != -90 {
		t.Fatalf("site subject = %+v ok=%v", sub, ok)
	}
	period := 2 * math.Pi * math.Sqrt(2.0e6*2.0e6*2.0e6/moon.GravitationalParameter())
	if want := math.Ceil(period/60) * 60; math.Abs(sub.LapS-want) > 1 {
		t.Errorf("lap = %v s, want %v s (relay period rounded up to the minute)", sub.LapS, want)
	}
	addMoonCraft(w, spacecraft.AntennaRelay, 1e9, dirE, 3.0e7) // period about 128 h
	if sub, _ = w.TargetCoverage(); sub.LapS != coverageLapCapS {
		t.Errorf("long-period relay: lap = %v, want the 48 h cap %v", sub.LapS, float64(coverageLapCapS))
	}
	w.ClearTarget()
	if _, ok := w.TargetCoverage(); ok {
		t.Error("no target must read false")
	}
	// a landed ghost is a surface point too
	w.Ghosts = []Ghost{{Owner: "p", CraftID: 9, Name: "Ghost-1", PrimaryID: "moon", Landed: true, LatDeg: 10, LonDeg: 20}}
	w.Target = Target{Kind: TargetGhost, GhostOwner: "p", CraftID: 9}
	if sub, ok = w.TargetCoverage(); !ok || sub.Label != "Ghost-1" || sub.Lon != 20 {
		t.Errorf("ghost subject = %+v ok=%v", sub, ok)
	}
}

// Linked: a source sees a dot only above its horizon and inside the geometric
// mean of the two ranges; a metre either side of each edge (floats, no exact edge).
func TestLinked(t *testing.T) {
	const R = 1737400.0
	basic := spacecraft.AntennaRangeDirectBasic
	up := render.Vec3{Z: 1}
	// source straight above the dot at height h: always above the horizon
	src := func(h, rng float64) []LinkedSource {
		return []LinkedSource{{Dir: up, DistM: R + h, RangeM: rng}}
	}
	rng := 4 * basic // link range = sqrt(4*basic*basic) = 2*basic
	if !Linked(src(2*basic-1, rng), R, 0, 0, 1) {
		t.Error("1 m inside the link range must link")
	}
	if Linked(src(2*basic+1, rng), R, 0, 0, 1) {
		t.Error("1 m outside the link range must not link")
	}
	// horizon: a dot 90 deg round from a source at 3R is behind the limb
	if Linked(src(2*R, 1e15), R, 1, 0, 0) {
		t.Error("a dot a quarter turn away must be behind the limb")
	}
	// the tangent dot of a source at distance d is at cos(a) = R/d
	d := 3 * R
	a := math.Acos(R / d)
	if !Linked(src(d-R, 1e15), R, math.Sin(a-1e-6), 0, math.Cos(a-1e-6)) {
		t.Error("just above the horizon must link")
	}
	if Linked(src(d-R, 1e15), R, math.Sin(a+1e-6), 0, math.Cos(a+1e-6)) {
		t.Error("just below the horizon must not link")
	}
}

// CommGraph.Net lists the stations and the forwarders with a chain to one, and
// not a relay with no chain.
func TestCommGraphNetIsTheLiveNetwork(t *testing.T) {
	w, dirE, _, _, _, _ := moonSiteWorld(t)
	w.RecomputeCommGraph()
	base := len(w.CommGraph.Net)
	if base == 0 {
		t.Fatal("no relays: Net must hold the stations")
	}
	addMoonCraft(w, spacecraft.AntennaRelay, 1e9, dirE, 2.0e6) // between the Moon and Earth: sees a station
	w.RecomputeCommGraph()
	if got := len(w.CommGraph.Net); got != base+1 {
		t.Errorf("connected relay: Net has %d, want %d", got, base+1)
	}
	w.Crafts[len(w.Crafts)-1].AntennaRangeM = 1 // out of everyone's reach
	w.RecomputeCommGraph()
	if got := len(w.CommGraph.Net); got != base {
		t.Errorf("isolated relay: Net has %d, want %d", got, base)
	}
}

// A partner's site shares its id with one of yours (both players' first flag is
// id 1): the coverage subject must be the partner's point, not yours.
func TestTargetCoveragePartnerSiteIDCollidesWithOwn(t *testing.T) {
	w, dirE, _, _, _, _ := moonSiteWorld(t)
	moon := siteBody(w, "moon")
	sys := w.System().Name
	w.Sites = append(w.Sites, Site{ID: 1, Body: moon.EnglishName, System: sys, LatDeg: 12.5, LonDeg: -33, Name: "Own site"})
	w.PartnerSites = []PartnerSite{{Site: Site{ID: 1, Owner: "p", Body: moon.EnglishName, System: sys, LatDeg: -40, LonDeg: 120, Name: "Their site"}}}
	addMoonCraft(w, spacecraft.AntennaRelay, 1e9, dirE, 2.0e6)
	w.SetTargetPartnerSite("p", 1)
	if sub, ok := w.TargetCoverage(); !ok || sub.Label != "Their site" || sub.Lat != -40 || sub.Lon != 120 {
		t.Errorf("partner subject = %+v ok=%v", sub, ok)
	}
}
