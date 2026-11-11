package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const moonR = 1737400.0

func TestApollo12ToSurveyor3(t *testing.T) {
	// ADR 0066 decision 9 worked example (LROC coordinates).
	lat1, lon1, lat2, lon2 := -3.01239, -23.42157, -3.0159, -23.4181
	d := HaversineM(lat1, lon1, lat2, lon2, moonR)
	az := ForwardAzimuthDeg(lat1, lon1, lat2, lon2)
	if math.Abs(d-150) > 1 {
		t.Errorf("ground = %.1f m, want about 150", d)
	}
	if math.Abs(az-135) > 1 {
		t.Errorf("bearing = %.1f deg, want about 135", az)
	}
	// the equal point: zero distance (bearing is dashed by LandBesideReading)
	if got := HaversineM(lat1, lon1, lat1, lon1, moonR); got != 0 {
		t.Errorf("equal point distance = %v, want 0", got)
	}
}

func TestImpactOffsetSignFlipsAcrossSite(t *testing.T) {
	// flying due east along the equator, sub-point at lon 0, site at lon 1.
	m := func(impLon, impLat float64) (float64, float64, bool) {
		return ImpactOffsetM(0, 0, impLat, impLon, 0, 1, moonR)
	}
	long, cross, ok := m(1.1, 0)
	if !ok || long <= 0 || math.Abs(cross) > 1 {
		t.Errorf("beyond: long=%v cross=%v ok=%v, want long>0 cross~0", long, cross, ok)
	}
	if long, _, _ := m(0.9, 0); long >= 0 {
		t.Errorf("short of the site: long=%v, want <0", long)
	}
	if long, cross, ok := m(1, 0); !ok || long != 0 || cross != 0 {
		t.Errorf("equal point: long=%v cross=%v ok=%v, want 0 0 true", long, cross, ok)
	}
	// south of an eastbound track is the right-hand side
	if _, cross, _ := m(1, -0.1); cross <= 0 {
		t.Errorf("right of track: cross=%v, want >0", cross)
	}
	if _, cross, _ := m(1, 0.1); cross >= 0 {
		t.Errorf("left of track: cross=%v, want <0", cross)
	}
	if _, _, ok := ImpactOffsetM(0, 0, 0, 0, 0, 1, moonR); ok {
		t.Error("impact at the sub-point has no track, want ok=false")
	}
}

func TestLookDownWorkedExample(t *testing.T) {
	// 4.0 km up, 11.9 km out, nose 10 deg off the zenith: LOS 18.6 below, lpd 8.6 -> 9.
	r := orbital.Vec3{Z: moonR + 4000}
	site := r.Add(orbital.Vec3{X: 11900, Z: -4000})
	ten := 10 * math.Pi / 180
	got, ok := LookDownDeg(r, site, orbital.Vec3{X: math.Sin(ten), Z: math.Cos(ten)})
	if !ok || math.Round(got) != 9 || math.Abs(got-8.6) > 0.1 {
		t.Errorf("lpd = %v ok=%v, want 8.6 (reads 9)", got, ok)
	}
	// equal case: nose on the zenith, lpd is the bare line-of-sight depression
	got, _ = LookDownDeg(r, site, orbital.Vec3{Z: 1})
	if math.Abs(got-math.Atan(4.0/11.9)/rad) > 1e-9 {
		t.Errorf("zenith nose lpd = %v, want atan(4/11.9)", got)
	}
}

// Ground and bearing read from orbit; miss, cross and lpd dash until the
// trajectory meets the ground (decision 10).
func TestLandBesideReadingDashesUntilGroundFound(t *testing.T) {
	w, lander, _ := siteWorld(t)
	lander.Landed = false
	lander.Crashed = false
	// circular, level: never meets the ground inside the horizon
	r := lander.Primary.RadiusMeters() + 100000
	mu := lander.Primary.GravitationalParameter()
	lander.State.R = orbital.Vec3{X: r}
	lander.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	lb, ok := w.LandBesideReading()
	if ok {
		t.Fatalf("no target yet, reading must be absent: %+v", lb)
	}
	w.SetTargetSite(w.Sites[0].ID)
	lb, ok = w.LandBesideReading()
	if !ok || lb.GroundM <= 0 || !lb.BearingOK {
		t.Fatalf("ground/bearing must read from orbit: %+v ok=%v", lb, ok)
	}
	if lb.ImpactOK || lb.LPDOK || lb.MissOK {
		t.Errorf("miss/cross/lpd must be dashes before the ground is found: %+v", lb)
	}
	// drop the speed: the path now meets the ground, and the rows fill in
	lander.State.V = orbital.Vec3{Y: 0.2 * math.Sqrt(mu/r)}
	lander.CurrentAttitudeDir = orbital.Vec3{X: 1}
	lb, _ = w.LandBesideReading()
	if !lb.ImpactOK || !lb.MissOK || !lb.LPDOK {
		t.Errorf("miss/cross/lpd must fill in once PredictImpact finds the ground: %+v", lb)
	}
}

func TestLandBesideEqualPointHasNoBearing(t *testing.T) {
	w, lander, _ := siteWorld(t)
	lander.Landed = false
	w.SetTargetSite(w.Sites[0].ID)
	rs, _ := surfacePointState(lander.Primary, w.Sites[0].LatDeg, w.Sites[0].LonDeg, w.Clock.SimTime)
	lander.State.R = rs.Scale((rs.Norm() + 100000) / rs.Norm()) // straight above the site
	lander.State.V = orbital.Vec3{}
	lb, ok := w.LandBesideReading()
	if !ok || lb.GroundM > 1e-3 || lb.BearingOK {
		t.Errorf("above the site: ground=%v bearingOK=%v, want 0 and a dash", lb.GroundM, lb.BearingOK)
	}
}

// Playtest 2026-11-11: a Dragon's deorbit burn flies on the SuperDracos, so the
// (plan) miss must forecast that burn, not a 17-minute Draco burn (it read
// 2,566 km right after N while the flight landed under a metre from the flag).
func TestLandBesidePlanForecastsTheSuperDracoBurn(t *testing.T) {
	w, _ := dragonFromOrbit(t, "earth", 200e3)
	if _, err := w.PlanDeorbitAndEngage(); err != nil {
		t.Fatal(err)
	}
	lb, ok := w.LandBesideReading()
	if !ok || !lb.Planned || !lb.MissOK || math.Abs(lb.MissM) > 500 || math.Abs(lb.CrossM) > 500 {
		t.Errorf("right after N: %+v, want the plan's stop within 500 m, tagged Planned", lb)
	}
}

// ADR 0066 decision 21 / guard 19: a vessel that can stop reads the stop point,
// tagged as a plan before the burn and live after it, never the free fall; a
// vessel that cannot stop reads the free-fall impact as before.
func TestLandBesideReadsTheStopPointForAVesselThatCanStop(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	lb, ok := w.LandBesideReading()
	if !ok || !lb.Planned || !lb.MissOK || lb.ImpactOK || math.Abs(lb.MissM) > 500 || math.Abs(lb.CrossM) > 500 {
		t.Fatalf("before the burn: %+v, want the plan's stop within 500 m, tagged Planned, no impact yet", lb)
	}
	for i := 0; (len(c.Nodes) > 0 || c.ActiveBurn != nil || i == 0) && i < 80000; i++ {
		w.Tick()
	}
	lb, _ = w.LandBesideReading()
	if lb.Planned || !lb.Stop || !lb.ImpactOK || math.Abs(lb.MissM) > 1000 || math.Abs(lb.CrossM) > 1000 {
		t.Errorf("after the burn: %+v, want the live stop within 1 km of the site", lb)
	}
	// the free fall lands hundreds of km beyond the stop: the rows must not read it
	imp, found := PredictImpact(c, DescentPredictHorizon)
	if !found {
		t.Fatal("no free-fall impact")
	}
	s := w.Sites[0]
	subLat, subLon := render.WorldToBodyFixed(c.Primary, unitV(c.State.R), w.Clock.SimTime)
	iLat, iLon := render.WorldToBodyFixed(c.Primary, unitV(imp.Point), w.Clock.SimTime.Add(imp.TimeToImpact))
	ffMiss, _, _ := ImpactOffsetM(subLat, subLon, iLat, iLon, s.LatDeg, s.LonDeg, c.Primary.RadiusMeters())
	if ffMiss < 100e3 {
		t.Errorf("free fall is %.0f m long, want hundreds of km (the stop is not the crash)", ffMiss)
	}
	// a capsule (no engine to brake with) reads the free fall
	for i := range c.Stages {
		c.Stages[i].Thrust = 0
	}
	c.SyncFields()
	lb, _ = w.LandBesideReading()
	if lb.Stop || math.Abs(lb.MissM-ffMiss) > 1 {
		t.Errorf("capsule: %+v, want the free fall (%.0f m long)", lb, ffMiss)
	}
}

// Guard 20: braking with a heading trim held, cross: reads where you would stop
// if you straightened now, so it closes toward 0 monotonically and does not flip
// sign until it is under 1 km; with no trim it stays put (the equal case).
func TestLandBesideCrossClosesUnderAHeldTrim(t *testing.T) {
	series := func(trimDeg float64) []float64 {
		w, c := deorbitWorld(t, 150, 0)
		if _, err := w.PlanDeorbitOntoSite(); err != nil {
			t.Fatal(err)
		}
		w.Sites[0].LatDeg += 0.5 // crossrange for the trim to take out
		w.Clock.BaseStep = time.Second
		var xs []float64
		n := 0
		flyDeorbitTrim(t, w, c, 0, 0.9, trimDeg*rad, func() bool {
			if n++; n%10 == 1 { // every 10 s of braking
				lb, _ := w.LandBesideReading()
				xs = append(xs, lb.CrossM)
			}
			return len(xs) >= 8
		})
		return xs
	}
	trimmed := series(1)
	if len(trimmed) < 8 || math.Abs(trimmed[0]) < 2000 {
		t.Fatalf("setup: %v, want 8 samples starting over 2 km across", trimmed)
	}
	for i := 1; i < len(trimmed); i++ {
		if math.Abs(trimmed[i]) >= math.Abs(trimmed[i-1])-1 {
			t.Errorf("sample %d: cross %.0f from %.0f, want it to fall by more than 1 m", i, trimmed[i], trimmed[i-1])
		}
		if math.Abs(trimmed[i-1]) >= 1000 && trimmed[i]*trimmed[i-1] < 0 {
			t.Errorf("sample %d: cross flipped sign at %.0f m, before it was under 1 km", i, trimmed[i-1])
		}
	}
	flat := series(0)
	for i := 1; i < len(flat); i++ {
		if math.Abs(flat[i]-flat[i-1]) > 1 {
			t.Errorf("no trim, sample %d: cross moved %.0f -> %.0f, want unchanged within 1 m", i, flat[i-1], flat[i])
		}
	}
}

// Equal case: a partner's site and one of yours share id 1; the aim point is the partner's.
func TestSurfaceTargetLatLonPartnerSiteIDCollidesWithOwn(t *testing.T) {
	w, _, _, _, _, _ := moonSiteWorld(t)
	moon := siteBody(w, "moon")
	sys := w.System().Name
	w.Sites = append(w.Sites, Site{ID: 1, Body: moon.EnglishName, System: sys, LatDeg: 12.5, LonDeg: -33})
	w.PartnerSites = []PartnerSite{{Site: Site{ID: 1, Owner: "p", Body: moon.EnglishName, System: sys, LatDeg: -40, LonDeg: 120}}}
	w.SetTargetPartnerSite("p", 1)
	c := w.ActiveCraft()
	c.Primary = moon
	if lat, lon, ok := w.surfaceTargetLatLon(c); !ok || lat != -40 || lon != 120 {
		t.Errorf("aim = %v %v ok=%v, want -40 120", lat, lon, ok)
	}
}

// F6: TARGET's land-beside reading reruns the 30-minute forecast only when the
// orbit changes, not every frame; a cold read equals the uncached forecast.
func TestLandBesideForecastRunsOnlyWhenTheOrbitChanges(t *testing.T) {
	w, lander, _ := siteWorld(t)
	lander.Landed, lander.Crashed = false, false
	r := lander.Primary.RadiusMeters() + 100000
	mu := lander.Primary.GravitationalParameter()
	lander.State.R = orbital.Vec3{X: r}
	lander.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	w.SetTargetSite(w.Sites[0].ID)
	w.impactSolves = 0
	for i := 0; i < 200; i++ {
		w.Tick()
		w.LandBesideReading()
	}
	if w.impactSolves > 2 {
		t.Fatalf("200 frames on an unchanged orbit ran %d forecasts, want at most 2", w.impactSolves)
	}
	// a burn (new orbit) invalidates
	before := w.impactSolves
	lander.State.V = lander.State.V.Scale(0.5)
	lb, _ := w.LandBesideReading()
	if w.impactSolves != before+1 || !lb.ImpactOK {
		t.Errorf("after a retro burn: solves %d->%d impact=%v, want one new forecast that finds the ground", before, w.impactSolves, lb.ImpactOK)
	}
	before = w.impactSolves
	w.LandBesideReading()
	if w.impactSolves != before {
		t.Errorf("same orbit again re-ran the forecast")
	}
	// mid-burn: never cached
	lander.ActiveBurn = &spacecraft.ActiveBurn{}
	before = w.impactSolves
	w.LandBesideReading()
	w.LandBesideReading()
	if w.impactSolves != before+2 {
		t.Errorf("mid-burn solves %d->%d, want 2 (uncached)", before, w.impactSolves)
	}
}

func TestLandBesideColdReadEqualsUncachedForecast(t *testing.T) {
	w, lander, _ := siteWorld(t)
	lander.Landed, lander.Crashed = false, false
	r := lander.Primary.RadiusMeters() + 100000
	mu := lander.Primary.GravitationalParameter()
	lander.State.R = orbital.Vec3{X: r}
	lander.State.V = orbital.Vec3{Y: 0.2 * math.Sqrt(mu/r)}
	lander.CurrentAttitudeDir = orbital.Vec3{X: 1}
	w.SetTargetSite(w.Sites[0].ID)
	cached, _ := w.LandBesideReading()
	imp, found := PredictImpact(lander, DescentPredictHorizon)
	got, ok := w.cachedImpact(lander)
	if !found || !ok || got.Point != imp.Point || got.TimeToImpact != imp.TimeToImpact {
		t.Fatalf("cold cachedImpact %+v, want PredictImpact %+v", got, imp)
	}
	// a hit one second later reads the same ground contact, one second nearer
	w.Clock.SimTime = w.Clock.SimTime.Add(time.Second)
	hit, _ := w.cachedImpact(lander)
	if hit.Point != imp.Point || hit.TimeToImpact != imp.TimeToImpact-time.Second || !cached.ImpactOK {
		t.Errorf("hit %+v after 1 s, want the same point and TTI-1s", hit)
	}
}

// A found impact is an absolute time, so warp steps of many sim-minutes on an
// unchanged orbit must not re-solve; the entry expires exactly at the impact,
// and a "no impact" answer slides after a minute (R2-1).
func TestImpactCacheSurvivesWarpAndExpires(t *testing.T) {
	w, lander, _ := siteWorld(t)
	lander.Landed, lander.Crashed = false, false
	r := lander.Primary.RadiusMeters() + 100000
	mu := lander.Primary.GravitationalParameter()
	lander.State.R = orbital.Vec3{X: r}
	lander.State.V = orbital.Vec3{Y: 0.2 * math.Sqrt(mu/r)}
	w.impactSolves = 0
	imp, found := w.cachedImpact(lander)
	if !found || imp.TimeToImpact < 4*time.Minute {
		t.Fatalf("setup: found=%v tti=%v, want a falling orbit with 4+ min to impact", found, imp.TimeToImpact)
	}
	t0 := w.Clock.SimTime
	for step := time.Duration(1); t0.Add(step * 61 * time.Second).Before(t0.Add(imp.TimeToImpact)); step++ {
		w.Clock.SimTime = t0.Add(step * 61 * time.Second)
		w.cachedImpact(lander)
	}
	if w.impactSolves != 1 {
		t.Errorf("warp steps of 61 s on a falling orbit ran %d forecasts, want 1", w.impactSolves)
	}
	w.Clock.SimTime = t0.Add(imp.TimeToImpact) // equal case: impact time reached
	w.cachedImpact(lander)
	if w.impactSolves != 2 {
		t.Errorf("at the impact time the entry must expire: %d forecasts, want 2", w.impactSolves)
	}

	// no impact: kept inside a minute, re-solved after it
	r = lander.Primary.RadiusMeters() + 100000
	lander.State.R, lander.State.V = orbital.Vec3{X: r}, orbital.Vec3{Y: math.Sqrt(mu / r)}
	w.impactSolves = 0
	if _, f := w.cachedImpact(lander); f {
		t.Fatal("setup: circular orbit found an impact")
	}
	t0 = w.Clock.SimTime
	w.Clock.SimTime = t0.Add(59 * time.Second)
	w.cachedImpact(lander)
	if w.impactSolves != 1 {
		t.Errorf("no-impact at +59 s: %d forecasts, want 1", w.impactSolves)
	}
	w.Clock.SimTime = t0.Add(60 * time.Second)
	w.cachedImpact(lander)
	if w.impactSolves != 2 {
		t.Errorf("no-impact at +60 s: %d forecasts, want 2 (horizon slid)", w.impactSolves)
	}
}

// Drag moves the answer along the orbit, so an orbit that dips into the
// atmosphere is never cached; one that clears it is.
func TestImpactCacheSkipsOrbitsThatReachTheAtmosphere(t *testing.T) {
	w, lander, _ := siteWorld(t)
	for _, b := range w.System().Bodies {
		if b.Atmosphere != nil && b.ID == "earth" {
			lander.Primary = b
		}
	}
	if lander.Primary.Atmosphere == nil {
		t.Fatal("setup: no atmospheric body")
	}
	lander.Landed, lander.Crashed = false, false
	mu, r := lander.Primary.GravitationalParameter(), lander.Primary.RadiusMeters()+20000
	lander.State.R = orbital.Vec3{X: r}
	lander.State.V = orbital.Vec3{Y: 0.9 * math.Sqrt(mu/r)} // periapsis inside the air
	w.impactSolves = 0
	w.cachedImpact(lander)
	w.cachedImpact(lander)
	if w.impactSolves != 2 {
		t.Errorf("in-atmosphere orbit: %d forecasts for 2 reads, want 2", w.impactSolves)
	}
	r = lander.Primary.RadiusMeters() + lander.Primary.Atmosphere.CutoffAltitude + 200000
	lander.State.R, lander.State.V = orbital.Vec3{X: r}, orbital.Vec3{Y: math.Sqrt(mu / r)}
	w.impactSolves = 0
	w.cachedImpact(lander)
	w.cachedImpact(lander)
	if w.impactSolves != 1 {
		t.Errorf("clear orbit: %d forecasts for 2 reads, want 1", w.impactSolves)
	}
}

func BenchmarkLandBesideInOrbit(b *testing.B) {
	t := &testing.T{}
	w, lander, _ := siteWorld(t)
	lander.Landed, lander.Crashed = false, false
	r := lander.Primary.RadiusMeters() + 100000
	lander.State.R = orbital.Vec3{X: r}
	lander.State.V = orbital.Vec3{Y: math.Sqrt(lander.Primary.GravitationalParameter() / r)}
	w.SetTargetSite(w.Sites[0].ID)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.LandBesideReading()
	}
}

// ADR 0067 review F3: past the coast, miss: and cross: read the autopilot's own
// forecast (the stop the landing view's marker draws); with it off they read
// the plain stop. In final the reading is the position error being closed, so
// it ends within the landing's 50 m (the plain stop read +80 m at 0% throttle).
func TestLandBesideReadsTheAutopilotsForecast(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	var sawBrake, sawOff, signed bool
	var lastMiss, lastCross float64
	flyLanding(t, w, c, 2*3600*4, func(int) {
		ap := c.Landing
		if !ap.Engaged || ap.Phase == spacecraft.PhaseCoast || !ap.ForecastOK {
			return
		}
		lb, ok := w.LandBesideReading()
		if !ok || !lb.MissOK || lb.MissM != ap.ForecastAlongM || lb.CrossM != ap.ForecastCrossM {
			t.Fatalf("%v: miss %v cross %v ok=%v, want the forecast %v / %v", ap.Phase.Word(), lb.MissM, lb.CrossM, ok, ap.ForecastAlongM, ap.ForecastCrossM)
		}
		lastMiss, lastCross = lb.MissM, lb.CrossM
		if f := apFrameOf(c); ap.Phase == spacecraft.PhaseFinal && !signed && f.sh > 0.5 {
			// + is long: the flag lies behind the vessel's ground track
			lat, lon, _, _ := w.apTarget(c)
			toSite := siteVec(c, lat, lon, w.Clock.SimTime).Scale(c.Primary.RadiusMeters()).Sub(c.State.R)
			if d := toSite.Dot(f.vh.Unit()); math.Abs(d) > 3 && math.Abs(lb.MissM) > 3 {
				signed = true
				if (d < 0) != (lb.MissM > 0) {
					t.Errorf("final: flag %.0f m ahead of the vessel but miss reads %+.0f m", d, lb.MissM)
				}
			}
		}
		if ap.Phase == spacecraft.PhaseBrake && !sawOff {
			sawBrake, sawOff = true, true
			ap.Engaged = false // autopilot off: the plain stop, a different number
			plain, _ := w.LandBesideReading()
			ap.Engaged = true
			if plain.MissM == lb.MissM {
				t.Errorf("autopilot off still reads the forecast %v", plain.MissM)
			}
		}
	})
	if !sawBrake {
		t.Fatal("setup: no brake tick read")
	}
	if !signed {
		t.Log("final-phase sign not exercised: no tick with the flag >3 m off")
	}
	if math.Abs(lastMiss) > 50 || math.Abs(lastCross) > 50 {
		t.Errorf("last reading before contact miss %.1f cross %.1f m, want both within 50", lastMiss, lastCross)
	}
}

// Review-2 L4: with the tank dry the autopilot returns before it refreshes its
// forecast, so miss:/cross: kept serving the last one. Past that point they go
// back to the plain stop (or a dash), never a number the autopilot stopped
// updating.
func TestLandBesideDropsTheForecastOnceTheTankIsDry(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	var dried bool
	flyLanding(t, w, c, 2*3600*4, func(int) {
		ap := c.Landing
		if dried || !ap.Engaged || ap.Phase != spacecraft.PhaseBrake || !ap.ForecastOK {
			return
		}
		dried = true
		for _, s := range c.Stages {
			s.FuelMass = 0
		}
		c.Thrust = 0
		w.Tick()
		if ap.ForecastOK {
			t.Errorf("ForecastOK still set after a tick with no fuel")
		}
		if lb, _ := w.LandBesideReading(); lb.MissOK && lb.MissM == ap.ForecastAlongM {
			t.Errorf("miss: still serves the frozen forecast %v", lb.MissM)
		}
	})
	if !dried {
		t.Fatal("setup: never reached a brake tick with a forecast")
	}
}
