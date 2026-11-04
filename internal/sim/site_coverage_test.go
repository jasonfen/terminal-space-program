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
