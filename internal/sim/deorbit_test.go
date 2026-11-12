package sim

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// deorbitWorld puts a Lander on a circular 100 km lunar orbit whose plane holds
// the site (12.5N 33W) at the moment, the vessel aheadDeg behind it, with the
// plane tilted so the site sits offDeg off the track.
func deorbitWorld(t *testing.T, aheadDeg, offDeg float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, c, _ := siteWorld(t)
	st, ok := spacecraft.BuildModule(spacecraft.StageModuleLanderID)
	if !ok {
		t.Fatal("no lander module")
	}
	c.Stages = st
	c.SyncFields()
	c.Landed, c.Crashed, c.Crewed, c.Controllable = false, false, true, true
	c.Nodes, c.ActiveBurn = nil, nil
	s := w.Sites[0]
	p0 := siteVec(c, s.LatDeg, s.LonDeg, w.Clock.SimTime)
	h0 := p0.Cross(orbital.Vec3{Z: 1}.Add(orbital.Vec3{X: 0.3})).Unit()
	b := offDeg * rad
	h := h0.Scale(math.Cos(b)).Add(p0.Scale(math.Sin(b))).Unit() // p0.h = sin(off)
	proj := p0.Sub(h.Scale(h.Dot(p0))).Unit()
	rHat := rotAbout(proj, h, -aheadDeg)
	r := c.Primary.RadiusMeters() + 100000
	mu := c.Primary.GravitationalParameter()
	c.State.R = rHat.Scale(r)
	c.State.V = h.Cross(rHat).Unit().Scale(math.Sqrt(mu / r))
	c.State.M = c.TotalMass()
	c.CurrentAttitudeDir = c.State.V.Unit()
	w.SetTargetSite(s.ID)
	w.Clock.BaseStep = 250 * time.Millisecond
	return w, c
}

// flyDeorbit flies a planted deorbit through the real integrator: waits for the
// burn, then lights a 90 % surface-retrograde brake at the cue and returns the
// stop point's offset from the site.
func flyDeorbit(t *testing.T, w *World, c *spacecraft.Spacecraft, lightDelaySec, brakeThrottle float64) (long, cross, stopAlt float64) {
	return flyDeorbitTrim(t, w, c, lightDelaySec, brakeThrottle, 0, nil)
}

// flyDeorbitTrim is flyDeorbit holding a heading trim (radians) through the
// brake and calling each after every braking tick.
func flyDeorbitTrim(t *testing.T, w *World, c *spacecraft.Spacecraft, lightDelaySec, brakeThrottle, trim float64, each func() bool) (long, cross, stopAlt float64) {
	t.Helper()
	w.Clock.WarpIdx = 0
	site := w.Sites[0]
	var lit bool
	minSpeed := math.Inf(1)
	var stopR orbital.Vec3
	var stopT time.Time
	var originR orbital.Vec3
	var originT time.Time
	tickSec := w.Clock.BaseStep.Seconds()
	for i := 0; i < int(2*3600/tickSec); i++ {
		if i == 0 && len(c.Nodes) > 0 {
			originT = c.Nodes[0].BurnStart()
		}
		if originR.Norm() == 0 && !originT.IsZero() && !w.Clock.SimTime.Before(originT) {
			originR, originT = c.State.R, w.Clock.SimTime
		}
		w.Tick()
		if c.Landed || c.Crashed {
			break
		}
		if len(c.Nodes) == 0 && c.ActiveBurn == nil && !lit {
			cue, ground, ok := burnAtCue(c, deorbitHorizon, DeorbitBrakeThrottle, deorbitRefineIters)
			if ground && (!ok || cue.InSec+lightDelaySec <= 0 || (ok && cue.InSec <= tickSec)) {
				c.CurrentAttitudeDir = physicsRel(c).Scale(-1).Unit()
				c.AttitudeMode = spacecraft.BurnSurfaceRetrograde
				c.Throttle = brakeThrottle
				if trim != 0 {
					c.SeedHeadingTrim()
					c.HeadingTrim += trim
				}
				c.ManualBurn = &spacecraft.ManualBurn{}
				lit = true
			}
		}
		if lit {
			if each != nil && each() {
				break
			}
			sp := physicsRel(c).Norm()
			if sp < minSpeed {
				minSpeed, stopR, stopT = sp, c.State.R, w.Clock.SimTime
			}
			if sp < 1 {
				break
			}

		}
	}
	if !lit {
		t.Fatalf("never lit; landed=%v crashed=%v t=%v", c.Landed, c.Crashed, w.Clock.SimTime)
	}
	oLat, oLon := w.surfaceLatLon(originR, c, originT)
	aLat, aLon := w.surfaceLatLon(stopR, c, stopT)
	l, x, _ := ImpactOffsetM(oLat, oLon, aLat, aLon, site.LatDeg, site.LonDeg, c.Primary.RadiusMeters())
	return l, x, stopR.Norm() - c.Primary.RadiusMeters()
}

func physicsRel(c *spacecraft.Spacecraft) orbital.Vec3 {
	return physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
}

// aheadAtBurn is how far the site leads the vessel along its plane at the
// planted node's burn start, read from the world.
func aheadAtBurn(w *World, c *spacecraft.Spacecraft, n spacecraft.ManeuverNode) float64 {
	mu := c.Primary.GravitationalParameter()
	st, _ := physics.KeplerStep(c.State, mu, n.BurnStart().Sub(w.Clock.SimTime).Seconds())
	s := w.Sites[0]
	p := siteVec(c, s.LatDeg, s.LonDeg, n.BurnStart())
	h := c.State.R.Cross(c.State.V).Unit()
	rv := st.R.Unit()
	return math.Atan2(p.Dot(h.Cross(rv)), p.Dot(rv)) / rad
}

// ADR 0066 guard 17: N on the Lander at 100 km plants one node with the site 60
// degrees ahead at the burn, and flying it (light at the burn-at cue, brake at
// 90 %) stops within 1 km of the site.
func TestDeorbitStopsOnTheSite(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	plan, err := w.PlanDeorbitOntoSite()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Nodes) != 1 || c.Nodes[0].AdvisoryKey != AdvisoryKeyDeorbit {
		t.Fatalf("nodes = %+v, want one deorbit node", c.Nodes)
	}
	if got := aheadAtBurn(w, c, c.Nodes[0]); math.Abs(got-60) > 0.5 {
		t.Errorf("site %.2f deg ahead at the burn, want 60 +- 0.5", got)
	}
	if plan.Impact || math.Abs(plan.LongM) > 100 || math.Abs(plan.CrossM) > 150 {
		t.Errorf("plan = %+v, want a stop aim within 100 m / 150 m", plan)
	}
	long, cross, alt := flyDeorbit(t, w, c, 0, 0.9)
	t.Logf("flown: dv %.2f m/s, stops %.0f m long %.0f m across at %.0f m up", plan.DV, long, cross, alt)
	if math.Abs(long) > 1000 || math.Abs(cross) > 1000 {
		t.Errorf("flown stop %.0f m long, %.0f m across: want within 1 km of the site", long, cross)
	}
}

// The equal case: a site exactly 60 degrees ahead at the press burns at once;
// a second N leaves one node, not two.
func TestDeorbitEqualCaseBurnsAtOnceAndReplacesItself(t *testing.T) {
	w, c := deorbitWorld(t, 60, 0)
	plan, err := w.PlanDeorbitOntoSite()
	if err != nil {
		t.Fatal(err)
	}
	if plan.In > time.Second {
		t.Errorf("burns in %v, want at once", plan.In)
	}
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	if len(c.Nodes) != 1 {
		t.Errorf("%d nodes after a second N, want 1", len(c.Nodes))
	}
}

// The "at once" window is a hair either side of 60 degrees: 5e-4 deg short of it
// still burns now, 0.01 deg short does not (the site has just passed 60).
func TestDeorbitAtOnceToleranceEdges(t *testing.T) {
	w, _ := deorbitWorld(t, 60-5e-4, 0)
	plan, err := w.PlanDeorbitOntoSite()
	if err != nil {
		t.Fatalf("59.9995 deg ahead: %v", err)
	}
	if plan.In > time.Second {
		t.Errorf("59.9995 deg ahead: in %v, want at once", plan.In)
	}
	w, _ = deorbitWorld(t, 60-0.01, 0)
	plan, err = w.PlanDeorbitOntoSite()
	var off *DeorbitOffTrackError
	// a lap on, the Moon has turned the site just past the 1 deg cap: also a wait
	if !errors.As(err, &off) && (err != nil || plan.In < 90*time.Minute) {
		t.Errorf("59.99 deg ahead: in %v err %v, want a lap away (or off the track by then)", plan, err)
	}
}

// A vessel that cannot stop (thrust under its weight) aims the free-fall impact: after its
// burn the dashed arc ends within 1 km of the site and the plan says impact.
func TestDeorbitCannotStopAimsTheFreeFall(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	for i := range c.Stages {
		c.Stages[i].Thrust *= 0.4 // 18 kN on 15 t: cannot hover on the Moon
	}
	c.SyncFields()
	c.Crewed, c.Controllable = true, true
	plan, err := w.PlanDeorbitOntoSite()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Impact {
		t.Fatal("an engineless vessel must aim the impact")
	}
	t.Logf("crash aim dv %.2f m/s", plan.DV)
	for i := 0; (len(c.Nodes) > 0 || c.ActiveBurn != nil || i == 0) && i < 80000; i++ {
		w.Tick()
	}
	lb, ok := w.LandBesideReading()
	if !ok || !lb.ImpactOK || lb.Stop || math.Abs(lb.MissM) > 1000 || math.Abs(lb.CrossM) > 1000 {
		t.Errorf("after the burn: %+v, want the free-fall within 1 km of the site", lb)
	}
}

// Guard 18: each refusal returns its line verbatim.
func TestDeorbitRefusalsVerbatim(t *testing.T) {
	cases := []struct {
		name string
		want error
		mod  func(w *World, c *spacecraft.Spacecraft)
	}{
		{"no site", ErrDeorbitNoSite, func(w *World, c *spacecraft.Spacecraft) { w.ClearTarget() }},
		{"other body", ErrDeorbitOtherBody, func(w *World, c *spacecraft.Spacecraft) { w.Sites[0].Body = "Earth" }},
		{"on the ground", ErrDeorbitLanded, func(w *World, c *spacecraft.Spacecraft) { c.Landed = true }},
		{"no signal", ErrDeorbitNoSignal, func(w *World, c *spacecraft.Spacecraft) {
			c.Crewed = false
			for i := range c.Stages { // P37: the Lander carries a 1e8 antenna that reaches Earth from the Moon
				c.Stages[i].AntennaKind, c.Stages[i].AntennaRangeM = spacecraft.AntennaNone, 0
			}
			c.SyncFields()
		}},
		{"no orbit", ErrDeorbitNoOrbit, func(w *World, c *spacecraft.Spacecraft) { c.State.V = c.State.V.Scale(1.6) }},
		{"already falling", ErrDeorbitFalling, func(w *World, c *spacecraft.Spacecraft) { c.State.V = c.State.V.Scale(0.8) }},
		{"no engine", ErrDeorbitNoEngine, func(w *World, c *spacecraft.Spacecraft) {
			for i := range c.Stages {
				c.Stages[i].Thrust = 0
			}
			c.SyncFields()
			c.Crewed, c.Controllable = true, true
		}},
		{"no solution", ErrDeorbitNoSolution, func(w *World, c *spacecraft.Spacecraft) {
			c.Stages[0].FuelMass = 5 // a stop that never finishes: dry before it stops
			c.SyncFields()
			c.Crewed, c.Controllable = true, true
		}},
	}
	lines := map[error]string{
		ErrDeorbitNoSite: "no site targeted, press t", ErrDeorbitOtherBody: "site not on this body",
		ErrDeorbitLanded: "on the ground", ErrDeorbitNoSignal: "NO SIGNAL", ErrDeorbitNoOrbit: "no orbit to leave",
		ErrDeorbitFalling: "already falling, fly miss:", ErrDeorbitNoEngine: "no engine", ErrDeorbitNoSolution: "no solution from this orbit",
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, c := deorbitWorld(t, 150, 0)
			tc.mod(w, c)
			_, err := w.PlanDeorbitOntoSite()
			if err != tc.want || err.Error() != lines[tc.want] {
				t.Errorf("err = %v, want %q", err, lines[tc.want])
			}
			if len(c.Nodes) != 0 {
				t.Errorf("a refusal planted %d nodes", len(c.Nodes))
			}
		})
	}
}

// Guard 18: a site 1.0 degree off the track plants (the equal case at the cap)
// and 1.1 refuses with the angle and when the ground brings it under the track.
func TestDeorbitOffTrackCap(t *testing.T) {
	w, c := deorbitWorld(t, 60, DeorbitMaxOffTrackDeg)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatalf("1.0 deg off the track: %v, want a plant", err)
	}
	// within the 1e-6 deg tolerance of the cap still plants
	w, _ = deorbitWorld(t, 60, DeorbitMaxOffTrackDeg+5e-7)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Errorf("cap + 5e-7 deg: %v, want a plant", err)
	}
	w, c = deorbitWorld(t, 60, 1.1)
	_, err := w.PlanDeorbitOntoSite()
	var e *DeorbitOffTrackError
	if !errors.As(err, &e) {
		t.Fatalf("1.1 deg off the track: %v, want an off-track refusal", err)
	}
	if math.Abs(e.OffDeg-1.1) > 1e-6 || !e.Reaches || e.UnderIn <= 0 {
		t.Errorf("refusal = %+v, want 1.1 deg, reachable, under it in > 0", e)
	}
	if len(c.Nodes) != 0 {
		t.Error("a refusal planted a node")
	}
	// the stated time is when the site is on the plane (the instrument, checked)
	s := w.Sites[0]
	h := c.State.R.Cross(c.State.V).Unit()
	if off := math.Abs(siteVec(c, s.LatDeg, s.LonDeg, w.Clock.SimTime.Add(e.UnderIn)).Dot(h)); off > 1e-4 {
		t.Errorf("site is %.2g (sin of the angle) off the plane at the stated time, want ~0", off)
	}
}

// Guard 18: an equatorial orbit never reaches a site at 12.5 N; a site at the
// inclination's own latitude counts as reachable.
func TestDeorbitOffTrackReachability(t *testing.T) {
	w, c := deorbitWorld(t, 60, 0)
	mu := c.Primary.GravitationalParameter()
	r := c.State.R.Norm()
	spin := vec3(render.BodyRotationAxisWorld(c.Primary))
	x := spin.Cross(orbital.Vec3{X: 1}).Unit().Cross(spin).Unit() // in the equator
	c.State.R = x.Scale(r)
	c.State.V = spin.Cross(x).Unit().Scale(math.Sqrt(mu / r))
	_, err := w.PlanDeorbitOntoSite()
	var e *DeorbitOffTrackError
	if !errors.As(err, &e) || e.Reaches || math.Abs(e.ReachDeg) > 1e-6 {
		t.Fatalf("equatorial orbit: %v (%+v), want unreachable, reaches 0", err, e)
	}
	// tilt the orbit to exactly the site's latitude
	lat := w.Sites[0].LatDeg
	h := rotAbout(spin, x, lat)
	c.State.V = h.Cross(x).Unit().Scale(math.Sqrt(mu / r))
	_, err = w.PlanDeorbitOntoSite()
	if !errors.As(err, &e) || !e.Reaches {
		t.Errorf("orbit inclined to the site's latitude: %v (%+v), want reachable", err, e)
	}
}

// atAltitude re-seats the circular orbit of deorbitWorld at altKm.
func atAltitude(c *spacecraft.Spacecraft, altKm float64) {
	r := c.Primary.RadiusMeters() + altKm*1000
	c.State.V = c.State.V.Unit().Scale(math.Sqrt(c.Primary.GravitationalParameter() / r))
	c.State.R = c.State.R.Unit().Scale(r)
	c.CurrentAttitudeDir = c.State.V.Unit()
}

// Fix J: a brake that loses more altitude than the orbit has says so, with the
// altitude it needs computed for this vessel; 100 km still plans; another
// cause of "no solution" keeps the plain line.
func TestDeorbitRefusalSaysBrakeNeedsAltitude(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatalf("100 km Lander must still plan: %v", err)
	}
	w, c = deorbitWorld(t, 150, 0)
	atAltitude(c, 50)
	_, err := w.PlanDeorbitOntoSite()
	var low *DeorbitBrakeTooLowError
	if !errors.As(err, &low) {
		t.Fatalf("50 km err = %v, want brake-too-low", err)
	}
	if math.Abs(low.OrbitM-50000) > 100 || low.NeedM <= 50000 || low.NeedM > 100000 {
		t.Errorf("need %.0f orbit %.0f m", low.NeedM, low.OrbitM)
	}
	// The equal case: the stated altitude really stops, a km below it does not.
	if !brakeStopsFrom(c, low.NeedM) || brakeStopsFrom(c, low.NeedM-1000) {
		t.Errorf("need %.0f m is not the stop threshold", low.NeedM)
	}
	// The planner agrees with the stated altitude to 2 km (measured 65 fails, 66 plans; the
	// probe grazes 5 km under the surface and ignores rotation).
	for _, tc := range []struct {
		km   float64
		plan bool
	}{{low.NeedM / 1000, true}, {low.NeedM/1000 - 2, false}} {
		w2, c2 := deorbitWorld(t, 150, 0)
		atAltitude(c2, tc.km)
		if _, err := w2.PlanDeorbitOntoSite(); (err == nil) != tc.plan {
			t.Errorf("at %.0f km (need %.0f) plans=%v, err = %v", tc.km, low.NeedM/1000, tc.plan, err)
		}
	}
	t.Logf("50 km Lander: %v", err)
	// Another cause (dry before it stops) keeps the old line.
	w, c = deorbitWorld(t, 150, 0)
	atAltitude(c, 50)
	c.Stages[0].FuelMass = 5
	c.SyncFields()
	c.Crewed, c.Controllable = true, true
	if _, err := w.PlanDeorbitOntoSite(); err != ErrDeorbitNoSolution {
		t.Errorf("fuel-starved err = %v, want the plain no solution", err)
	}
}

// Fix J2: bracketRoot's collapse onto a jump (finite on one side, +Inf on the
// other, the live 50 km Lander: -946 km at the feasibility edge) is no root.
// At exactly the tolerance it is one.
func TestBracketRootCollapseNeedsTolerance(t *testing.T) {
	const tol = 50.0
	step := func(edge, miss float64) func(float64) float64 {
		return func(dv float64) float64 {
			if dv < edge {
				return math.Inf(1) // too shallow: no ground
			}
			return -miss
		}
	}
	for _, tc := range []struct {
		name string
		miss float64
		want bool
	}{{"946 km short refuses", 946104, false}, {"just over the tolerance refuses", tol + 0.01, false}, {"exactly the tolerance accepts", tol, true}} {
		// Where the last probe lands relative to the edge decides the old
		// answer, so sweep the edge.
		for edge := 12.0; edge < 13.0; edge += 0.0137 {
			if _, ok := bracketRoot(step(edge, tc.miss), 15, tol); ok != tc.want {
				t.Errorf("%s (edge %.4f): ok = %v, want %v", tc.name, edge, ok, tc.want)
				break
			}
		}
	}
}

// Fix L: the descent readouts look as far ahead as the deorbit planner. After
// the deorbit burn from a 70 km Moon orbit the fall is ~38 min long, past the
// old 30 min readout horizon, so the ENGINE cue (DescentCorridorFor + burn-at)
// read empty while G already had a target. The horizon is one constant, and
// the equal case (impact exactly at the horizon) is found: the forecast loop
// stops only once elapsed time passes the horizon.
func TestDescentReadoutsSeeTheWholeDeorbitFall(t *testing.T) {
	if DescentPredictHorizon != deorbitHorizon {
		t.Errorf("DescentPredictHorizon %v != deorbitHorizon %v", DescentPredictHorizon, deorbitHorizon)
	}
	w, c := deorbitWorld(t, 150, 0)
	atAltitude(c, 70)
	if _, err := w.PlanDeorbitAndEngage(); err != nil {
		t.Fatal(err)
	}
	w.DisengageLandingAutopilot(c)
	burned := false
	for i := 0; i < 200000 && !burned; i++ {
		w.Tick()
		burned = i > 10 && len(c.Nodes) == 0 && c.ActiveBurn == nil
	}
	if !burned {
		t.Fatal("deorbit burn never completed")
	}
	imp, ok := PredictImpact(c, 2*time.Hour)
	if !ok || imp.TimeToImpact < 31*time.Minute {
		t.Fatalf("setup: impact %v ok=%v, want a fall longer than the old 30 min horizon", imp.TimeToImpact, ok)
	}
	if _, ok := DescentCorridorFor(c, DescentPredictHorizon); !ok {
		t.Errorf("no descent corridor %v out (impact %v): ENGINE node: reads empty", DescentPredictHorizon, imp.TimeToImpact)
	}
	if _, ok := PredictBurnAt(c, DescentPredictHorizon); !ok {
		t.Errorf("no burn-at cue %v out (impact %v)", DescentPredictHorizon, imp.TimeToImpact)
	}
	if _, ok := PredictImpact(c, imp.TimeToImpact); !ok {
		t.Errorf("impact exactly at the horizon (%v) not found, want found", imp.TimeToImpact)
	}
	if _, ok := PredictImpact(c, imp.TimeToImpact-5*time.Second); ok {
		t.Errorf("impact 5 s past the horizon found, want not found")
	}
}

// entryWorld puts a loadout on a circular orbit at altM round a body (system ""
// is the default Sol), 150 degrees of arc behind a flag at 5N 20E, targeted.
// air=false strips the atmosphere.
func entryWorld(t *testing.T, system, body, loadout string, altM float64, air bool) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.navErrorOff = true
	if system != "" {
		cycleToSystem(t, w, system)
	}
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: loadout, ParentBodyID: body, AltitudeM: altM})
	if err != nil {
		t.Fatal(err)
	}
	for i, x := range w.Crafts {
		if x == c {
			w.SetActiveCraftIdx(i)
		}
	}
	if !air {
		c.Primary.Atmosphere = nil
	}
	c.Crewed, c.Controllable = true, true
	w.plantSite(c, SiteFlag, "site", 5, 20)
	s := w.Sites[len(w.Sites)-1]
	p0 := siteVec(c, s.LatDeg, s.LonDeg, w.Clock.SimTime)
	ax := render.BodyRotationAxisWorld(c.Primary)
	east := orbital.Vec3{X: ax.X, Y: ax.Y, Z: ax.Z}.Cross(p0).Unit()
	h := p0.Unit().Cross(east).Unit()
	rHat := rotAbout(p0.Unit(), h, -150)
	r := c.Primary.RadiusMeters() + altM
	c.State.R = rHat.Scale(r)
	c.State.V = h.Cross(rHat).Unit().Scale(math.Sqrt(c.Primary.GravitationalParameter() / r))
	c.State.M = c.TotalMass()
	c.CurrentAttitudeDir = c.State.V.Unit()
	w.SetTargetSite(s.ID)
	// the game's own 50 ms tick stands: a coarser one lags a spinning ground by 60 m per tick
	return w, c
}

// grazing drops c's periapsis to 5 km under the ground: the coast a minimal
// deorbit leaves.
func grazing(c *spacecraft.Spacecraft) {
	mu, rp := c.Primary.GravitationalParameter(), c.Primary.RadiusMeters()
	r := c.State.R.Norm()
	c.State.V = c.State.V.Unit().Scale(math.Sqrt(2 * mu * (rp - 5000) / (r * (r + rp - 5000))))
}

// ADR 0069 guard 2: on a body with no air the cue search is what it
// was before the entry plan existed. The expected bits were captured from
// origin/main 1d23710 before the change.
func TestBurnAtCueAirlessIsUnchanged(t *testing.T) {
	type fix struct {
		name    string
		build   func() *spacecraft.Spacecraft
		ok      bool
		altBits uint64
		inBits  uint64
	}
	for _, f := range []fix{
		{"moon 100 km", func() *spacecraft.Spacecraft { _, c := deorbitWorld(t, 150, 0); return c }, true, 0x40ede992f0dd21f4, 0x409667b455000000},
		{"moon 50 km", func() *spacecraft.Spacecraft { _, c := deorbitWorld(t, 150, 0); atAltitude(c, 50); return c }, true, 0x40e83aa997e387d4, 0x4062417258000000},
		// a light at the top cannot stop and nothing later is searched: no cue
		{"mars without air", func() *spacecraft.Spacecraft {
			_, c := entryWorld(t, "", "mars", "Ground-Station-Lander", 150e3, false)
			return c
		}, false, 0, 0},
	} {
		c := f.build()
		grazing(c)
		cue, ground, ok := burnAtCue(c, 2*time.Hour, DeorbitBrakeThrottle, deorbitRefineIters)
		// arm64 and amd64 differ in the last bit (fused multiply-add), so equal to 1e-12 relative
		near := func(got float64, wantBits uint64) bool {
			want := math.Float64frombits(wantBits)
			return math.Abs(got-want) <= 1e-12*math.Max(1, math.Abs(want))
		}
		if !ground || ok != f.ok || !near(cue.AltitudeM, f.altBits) || !near(cue.InSec, f.inBits) {
			t.Errorf("%s: ground=%v ok=%v alt=%v in=%v, want ok=%v alt=%v in=%v", f.name, ground, ok,
				cue.AltitudeM, cue.InSec, f.ok, math.Float64frombits(f.altBits), math.Float64frombits(f.inBits))
		}
	}
}

// ADR 0069 decision 3 / guard 1: on a body with air a light at the top of the
// coast can fail where a later one stops, so the cue is the latest safe light,
// found from the last safe sample. Mars, Ground-Station Lander, 150 km.
func TestBurnAtCueOnAirIsTheLatestSafeLight(t *testing.T) {
	_, c := entryWorld(t, "", "mars", "Ground-Station-Lander", 150e3, true)
	grazing(c)
	cue, ground, ok := burnAtCue(c, 2*time.Hour, DeorbitBrakeThrottle, deorbitRefineIters)
	if !ground || !ok {
		t.Fatalf("ground=%v ok=%v, want a cue", ground, ok)
	}
	if cue.AltitudeM < 5000 || cue.AltitudeM > 40000 || cue.InSec <= 0 {
		t.Fatalf("cue %.0f m in %.0f s", cue.AltitudeM, cue.InSec)
	}
	hz := 2 * time.Hour
	left := hz - time.Duration(cue.InSec*float64(time.Second))
	pred, pok := predictPoweredStopFromAt(cue.State, c, left, DeorbitBrakeThrottle)
	if !pok || pred.Outcome != StopStopped || pred.MarginM < 0 {
		t.Errorf("the cue itself does not stop: ok=%v %v margin %.1f", pok, pred.Outcome, pred.MarginM)
	}
	// latest: 10 s later (ahead on the same coast) no longer stops
	later, lok := physics.KeplerStep(cue.State, c.Primary.GravitationalParameter(), 10)
	if !lok {
		t.Fatal("no kepler step")
	}
	p2, p2ok := predictPoweredStopFromAt(later, c, left-10*time.Second, DeorbitBrakeThrottle)
	if p2ok && p2.Outcome == StopStopped && p2.MarginM >= 0 {
		t.Errorf("a light 10 s after the cue still stops (margin %.0f m): the cue is not the latest", p2.MarginM)
	}
	t.Logf("Mars Ground-Station Lander cue: %.2f km, T-%.0f s, stops %.0f m up after %.0f s", cue.AltitudeM/1000, cue.InSec, pred.MarginM, pred.ElapsedSec)
}

// ADR 0069 guard 1 at every warp: the N entry lands on the flag whatever the
// warp, because the coast to the air and the touchdown point do not depend on
// the tick size. Before the fix 1000x and up missed by 11-12 km and 10x-100x
// by 120 m.
func TestEntryLandsOnFlagAtEveryWarp(t *testing.T) {
	for idx, f := range WarpFactors {
		w, c := entryWorld(t, "", "mars", "Ground-Station-Lander", 150e3, true)
		if _, err := w.PlanDeorbitOntoSite(); err != nil {
			t.Fatal(err)
		}
		if err := w.EngageLandingAutopilot(); err != nil {
			t.Fatal(err)
		}
		w.Clock.WarpIdx = idx
		for i := 0; i < 4*3600*20 && !c.Landed && !c.Crashed; i++ {
			w.Tick()
		}
		if !c.Landed {
			t.Fatalf("%vx: not landed (crashed=%v)", f, c.Crashed)
		}
		ev := w.PendingLandingEvents[len(w.PendingLandingEvents)-1]
		if ev.DistanceM > 50 || ev.OutOfReach { // equal case: 50 m is within
			t.Errorf("%vx: landed %.0f m from the flag (oor=%v), want within 50 m", f, ev.DistanceM, ev.OutOfReach)
		}
	}
}

// flyBoostback plants N on a dropped stage, ticks to contact at warp index idx
// and returns the touchdown distance from LZ-1, the contact speed (ground
// relative, the tick before contact) and whether it landed.
func flyBoostback(t *testing.T, w *World, c *spacecraft.Spacecraft, lc13 Site, idx int) (distM, speed float64, landed bool) {
	t.Helper()
	w.Clock.WarpIdx = idx
	for i := 0; i < 4*3600*20 && !c.Landed && !c.Crashed; i++ {
		speed = physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary).Norm()
		w.Tick()
	}
	la, lo := lc13.lzLatLon(1, c.Primary.RadiusMeters())
	return HaversineM(c.LandedLatDeg, c.LandedLonDeg, la, lo, c.Primary.RadiusMeters()), speed, c.Landed
}

// ADR 0069 decision 14 (T13): N on a dropped Falcon 9 first stage that is not in
// orbit plans a boostback onto LZ-1, engages Site+ and the stage lands inside
// the 43 m LZ circle at every warp. The rows are a stage that could not hover
// before the burn (52 t of propellant: 79 t on 760 kN of landing thrust) and one with the idle
// manual burn a dropped stage inherits from the core.
func TestBoostbackThenLandingBurnAtEveryWarp(t *testing.T) {
	for _, tc := range []struct {
		name string
		fall boosterFall
		idle bool
		fwd  bool // the nose points along the velocity (as the core flew it), so the burn waits for the swing
	}{
		{"24 t kept", boosterFall{altM: 20e3, downMps: -300, offE: 20e3, driftE: 400, fuelKg: 24000}, false, false},
		{"18 t kept, higher and slower", boosterFall{altM: 30e3, downMps: -100, offE: 20e3, driftE: 200, fuelKg: 18000}, false, false},
		{"52 t kept: too heavy to hover before the burn", boosterFall{altM: 20e3, downMps: -300, offE: 20e3, driftE: 400, fuelKg: 52000}, false, false},
		{"idle manual burn from the core", boosterFall{altM: 20e3, downMps: -300, offE: 20e3, driftE: 400, fuelKg: 24000}, true, false},
		{"nose forward: the swing sets the lead", boosterFall{altM: 20e3, downMps: -300, offE: 20e3, driftE: 400, fuelKg: 24000}, false, true},
	} {
		for _, idx := range []int{0, 1, len(WarpFactors) - 1} {
			w, c, lc13 := fallWorld(t, spacecraft.StageModuleF9S1ID, tc.fall)
			if tc.idle {
				c.ManualBurn, c.Throttle = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}, 0 // as dropped: lit state, throttle shut
			}
			if tc.fwd {
				c.CurrentAttitudeDir = physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary).Unit()
			}
			plan, err := w.PlanDeorbitAndEngage()
			if err != nil || plan == nil || !plan.Boostback || len(c.Nodes) != 1 || !c.Landing.Owns() {
				t.Fatalf("%s: N: plan=%+v err=%v nodes=%d owns=%v, want a boostback planted and Site+ engaged", tc.name, plan, err, len(c.Nodes), c.Landing.Owns())
			}
			d, v, landed := flyBoostback(t, w, c, lc13, idx)
			t.Logf("%s, warp %vx: boostback %.0f m/s in %.1f s, cue %.1f km; landed=%v %.1f m from LZ-1 at %.2f m/s", tc.name, WarpFactors[idx], plan.DV, plan.In.Seconds(), plan.CueAltM/1e3, landed, d, v)
			if !landed || d > 43 || v >= CrashVCritMps {
				t.Errorf("%s, warp %vx: landed=%v %.1f m from LZ-1 at %.2f m/s, want inside the 43 m circle under %v m/s", tc.name, WarpFactors[idx], landed, d, v, CrashVCritMps)
			}
		}
	}
}

// B on a dropped stage plants the same boostback and engages nothing.
func TestBoostbackBPlantsOnly(t *testing.T) {
	w, c, _ := fallWorld(t, spacecraft.StageModuleF9S1ID, boosterFall{altM: 20e3, downMps: -300, offE: 20e3, driftE: 400, fuelKg: 24000})
	plan, err := w.PlanDeorbitOnly()
	if err != nil || plan == nil || !plan.Boostback || len(c.Nodes) != 1 {
		t.Fatalf("B: plan=%+v err=%v nodes=%d, want a boostback planted", plan, err, len(c.Nodes))
	}
	if c.Landing.Owns() {
		t.Error("B engaged the autopilot")
	}
}

// planFalcon9Drop launches a Falcon 9, drops its first stage with keep of the
// propellant left, switches to it, targets LC-13 LZ-1 and presses N.
func planFalcon9Drop(t *testing.T, keep float64, before func(*World)) (*World, *spacecraft.Spacecraft, *DeorbitPlan, error) {
	t.Helper()
	w, _, b := falcon9Drop(t, keep)
	w.CycleActiveCraft(1)
	w.SetTargetSiteLZ(lc13ID(t), 1)
	w.Tick()
	if before != nil {
		before(w)
	}
	plan, err := w.PlanDeorbitAndEngage()
	return w, b, plan, err
}

// ADR 0069 decision 14, amended (slice 9b): N brings a real Falcon 9 first stage
// home in three burns, a boostback, an entry burn on three engines of nine on the
// way down, and the one-burn landing. Dropped with 11, 12 and 16 % of its
// propellant left (11 is the least that closes; 16 lights its entry burn lower, at 50 km), at every warp: inside the 43 m
// circle under 10 m/s, with propellant left. The boostback and the entry burn
// both fire; the entry burn is its own row in the recording.
func TestEntryBurnBringsAFalcon9Home(t *testing.T) {
	for _, keep := range []float64{0.11, 0.12, 0.16} {
		for _, idx := range []int{0, 1, len(WarpFactors) - 1} {
			var dir string
			w, b, plan, err := planFalcon9Drop(t, keep, func(w *World) {
				if idx == 0 {
					dir = recAttach(t, w)
				}
			})
			if err != nil || plan == nil || !plan.Boostback || plan.EntryDV <= 0 || len(b.Nodes) != 2 {
				t.Fatalf("%v kept: N: plan=%+v err=%v nodes=%d, want a boostback and an entry burn planted", keep, plan, err, len(b.Nodes))
			}
			if e := b.Nodes[1]; e.AdvisoryKey != AdvisoryKeyEntry || e.Throttle != EntryThrottle || e.Mode != spacecraft.BurnRetrograde {
				t.Errorf("%v kept: second node = key %q throttle %v mode %v, want the entry burn, retrograde on three engines of nine", keep, e.AdvisoryKey, e.Throttle, e.Mode)
			}
			if again, err := w.PlanDeorbitAndEngage(); err != nil || again == nil || len(b.Nodes) != 2 { // N again re-solves, it does not stack
				t.Errorf("%v kept: second N: plan=%+v err=%v nodes=%d, want the two burns replaced", keep, again, err, len(b.Nodes))
			}
			if _, capped := w.landingAutopilotWarpLimit(); capped {
				t.Errorf("%v kept: the autopilot caps warp while N's burns are planted (they ramp it down themselves)", keep)
			}
			w.PendingBurnFiredEvents = nil
			d, v, landed := flyBoostback(t, w, b, lc13ForTest(t), idx)
			var fired []float64
			for _, e := range w.PendingBurnFiredEvents {
				if e.CraftName == b.Name {
					fired = append(fired, e.DV)
				}
			}
			t.Logf("%v kept, warp %vx: boostback %.0f m/s, entry %.0f m/s from %.0f km, cue %.1f km; landed=%v %.1f m from LZ-1 at %.2f m/s, %.0f kg left", keep, WarpFactors[idx], plan.DV, plan.EntryDV, plan.EntryAltM/1e3, plan.CueAltM/1e3, landed, d, v, b.ActiveStageFuel())
			if !landed || d > 43 || v >= CrashVCritMps || b.ActiveStageFuel() <= 0 {
				t.Errorf("%v kept, warp %vx: landed=%v %.1f m from LZ-1 at %.2f m/s, %.0f kg left, want inside the 43 m circle under %v m/s with propellant", keep, WarpFactors[idx], landed, d, v, b.ActiveStageFuel(), CrashVCritMps)
			}
			if len(fired) != 2 || math.Abs(fired[0]-plan.DV) > 0.5 || math.Abs(fired[1]-plan.EntryDV) > 0.5 {
				t.Errorf("%v kept, warp %vx: burns fired %v, want the boostback %.0f then the entry burn %.0f", keep, WarpFactors[idx], fired, plan.DV, plan.EntryDV)
			}
			if dir != "" {
				ev := strings.Join(allEv(t, recFile(t, dir)), "|")
				for _, want := range []string{"boostback: ", "entry burn: ", "entry burn start: node "} {
					if !strings.Contains(ev, want) {
						t.Errorf("%v kept: recording lacks %q: %s", keep, want, ev)
					}
				}
			}
		}
	}
}

// The edge of the window, on a real Falcon 9: 10 % kept is refused (1 m/s
// short: the need/have line) and plants nothing, 11 % closes. 35 % kept reaches the site but its landing burn would
// light 11.8 km up, over the entry burn's 10 km target, and says where.
func TestEntryBurnWindowOnARealFalcon9(t *testing.T) {
	for _, tc := range []struct {
		keep    float64
		wantErr error // nil: a plan
		noLand  bool
	}{{0.10, &BoostbackShortError{}, false}, {0.11, nil, false}, {0.35, nil, true}} {
		_, b, plan, err := planFalcon9Drop(t, tc.keep, nil)
		var nl *BoostbackNoLandingError
		switch {
		case tc.noLand:
			if plan != nil || !errors.As(err, &nl) || nl.CueAltM <= EntryTargetCueAltM {
				t.Errorf("%v kept: plan=%+v err=%v, want a BoostbackNoLandingError lit above %v m", tc.keep, plan, err, EntryTargetCueAltM)
			}
		case tc.wantErr == nil:
			if err != nil || plan == nil {
				t.Errorf("%v kept: plan=%+v err=%v, want a plan", tc.keep, plan, err)
			}
		case tc.keep == 0.10: // 1 m/s short of closing: says so
			var sh *BoostbackShortError
			if plan != nil || !errors.As(err, &sh) || sh.Need <= sh.Have || sh.Need-sh.Have > 20 {
				t.Errorf("%v kept: plan=%+v err=%v, want a BoostbackShortError within 20 m/s", tc.keep, plan, err)
			}
		default:
			if plan != nil || !errors.Is(err, tc.wantErr) {
				t.Errorf("%v kept: plan=%+v err=%v, want %v", tc.keep, plan, err, tc.wantErr)
			}
		}
		if (plan == nil) && (len(b.Nodes) != 0 || b.Landing.Owns() || b.LZClaim != (spacecraft.LZClaim{})) {
			t.Errorf("%v kept: a refused N left nodes=%d owns=%v claim=%+v", tc.keep, len(b.Nodes), b.Landing.Owns(), b.LZClaim)
		}
	}
}

func lc13ForTest(t *testing.T) Site {
	t.Helper()
	for _, z := range shippedZones {
		if z.Name == "LC-13" {
			return z
		}
	}
	t.Fatal("no LC-13")
	return Site{}
}

// falcon9Drop launches a Falcon 9 from the Cape pad (vertical for 8 s, a 0.015 rad
// kick east, then surface prograde) and drops the first stage with `space` when
// keep of its propellant is left. Returns the world, the core and the stage.
func falcon9Drop(t *testing.T, keep float64) (*World, *spacecraft.Spacecraft, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast})
	if err != nil {
		t.Fatal(err)
	}
	w.RecomputeCommGraph()
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	f0, t0 := c.Stages[0].FuelMass, w.Clock.SimTime
	for i := 0; i < 20000 && c.Stages[0].FuelMass > keep*f0; i++ {
		switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
		case el > 8 && el < 14:
			c.PitchTrim = 0.015
		case el >= 14 && c.AttitudeMode == spacecraft.BurnRadialOut:
			c.PitchTrim, c.AttitudeMode = 0, spacecraft.BurnSurfacePrograde
		}
		w.Tick()
	}
	n := len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	return w, c, w.Crafts[n]
}

func lc13ID(t *testing.T) uint64 {
	t.Helper()
	for _, z := range shippedZones {
		if z.Name == "LC-13" {
			return z.ID
		}
	}
	t.Fatal("no LC-13")
	return 0
}

// Jason's playtest drop (2026-11-12, recording): 59.4 km up, 741 m/s down, 1,462
// m/s east of the pad-side LZ, a Falcon 9 first stage with 51.5 t of propellant,
// the LZ offE metres behind it. Used by the 9c guards below.
func jasonDrop(t *testing.T, offE, fuelKg float64) (*World, *spacecraft.Spacecraft, Site) {
	t.Helper()
	return fallWorld(t, spacecraft.StageModuleF9S1ID, boosterFall{altM: 59.4e3, downMps: -741, offE: offE, driftE: 1462, fuelKg: fuelKg})
}

// Slice 9c guard 1: more propellant on a closer drop never loses the plan. The
// bracket used to read an unjudgeable burn at its low edge as "too steep" and
// back away from the root (90 t refused at 60 and 30 km while 107 km landed).
// Within the grid, once a plan exists at some propellant every heavier load plans
// too (up to 120 t: past it the entry burn cap of 1,100 m/s ends the plans (burns that long run into the dense air, which the forecast leaves out, and flew 10 km off)). Two
// of the newly planned cases are flown to touchdown at 1x and at max warp.
func TestBoostbackPlansAtEveryHeavierLoad(t *testing.T) {
	for _, offE := range []float64{107e3, 60e3, 30e3} {
		planned := false
		for _, tons := range []float64{70, 90, 120} {
			w, c, _ := jasonDrop(t, offE, tons*1e3)
			plan, err := w.PlanDeorbitAndEngage()
			if planned && plan == nil {
				t.Errorf("%.0f km out, %.0f t: refused (%v) though a lighter load planned", offE/1e3, tons, err)
			}
			planned = planned || plan != nil
			if plan != nil && plan.DV+plan.EntryDV > stageDV(c) {
				t.Errorf("%.0f km out, %.0f t: plans %.0f + %.0f m/s, more than the stage has", offE/1e3, tons, plan.DV, plan.EntryDV)
			}
		}
	}
	// the stated limit: 160 t is past the entry burn cap, and says so
	if w, _, _ := jasonDrop(t, 30e3, 160e3); true {
		if plan, err := w.PlanDeorbitAndEngage(); plan != nil || err == nil {
			t.Errorf("160 t at 30 km: plan=%+v err=%v, want a refusal past the entry burn cap", plan, err)
		}
	}
	for _, tc := range []struct{ offE, tons float64 }{{60e3, 90}, {30e3, 120}} {
		for _, idx := range []int{0, len(WarpFactors) - 1} {
			w, c, lc13 := jasonDrop(t, tc.offE, tc.tons*1e3)
			plan, err := w.PlanDeorbitAndEngage()
			if err != nil || plan == nil {
				t.Fatalf("%.0f km, %.0f t: N: %v", tc.offE/1e3, tc.tons, err)
			}
			d, v, landed := flyBoostback(t, w, c, lc13, idx)
			t.Logf("%.0f km out, %.0f t, warp %vx: boostback %.0f, entry %.0f from %.0f km; landed=%v %.1f m from LZ-1 at %.2f m/s", tc.offE/1e3, tc.tons, WarpFactors[idx], plan.DV, plan.EntryDV, plan.EntryAltM/1e3, landed, d, v)
			if !landed || d > 43 || v >= CrashVCritMps {
				t.Errorf("%.0f km, %.0f t, warp %vx: landed=%v %.1f m at %.2f m/s, want inside the 43 m circle under %v m/s", tc.offE/1e3, tc.tons, WarpFactors[idx], landed, d, v, CrashVCritMps)
			}
		}
	}
}

// Slice 9c guard 2: Jason's exact drop (51.5 t, 107 km out) is refused with the
// Δv it lacks, and the figure is honest: a tank of the stage with that much Δv
// plans, one 0.5 % short of it does not.
func TestBoostbackShortStageSaysNeedAndHave(t *testing.T) {
	w, c, _ := jasonDrop(t, 107e3, 51536)
	plan, err := w.PlanDeorbitAndEngage()
	var sh *BoostbackShortError
	if plan != nil || !errors.As(err, &sh) {
		t.Fatalf("N: plan=%+v err=%v, want a BoostbackShortError", plan, err)
	}
	have := stageDV(c)
	if math.Abs(sh.Have-have) > 1 || sh.Need <= sh.Have {
		t.Errorf("need %.0f have %.0f, want have %.0f and need above it", sh.Need, sh.Have, have)
	}
	if len(c.Nodes) != 0 || c.Landing.Owns() {
		t.Errorf("a refused N left nodes=%d owns=%v", len(c.Nodes), c.Landing.Owns())
	}
	// the stage with exactly dv m/s: fuel f with Isp g ln((d + f) / d) = dv, d the mass with the tank empty
	d := c.TotalMass() - c.ActiveStageFuel()
	fuelFor := func(dv float64) float64 { return d*math.Exp(dv/(c.Isp*stdGravityMps2)) - d }
	for _, tc := range []struct {
		scale float64
		plans bool
	}{{1.005, true}, {0.995, false}} {
		w2, _, _ := jasonDrop(t, 107e3, fuelFor(sh.Need*tc.scale))
		if p, e := w2.PlanDeorbitAndEngage(); (p != nil) != tc.plans {
			t.Errorf("%.3f x Need (%.0f t): plan=%v err=%v, want plans=%v", tc.scale, fuelFor(sh.Need*tc.scale)/1e3, p != nil, e, tc.plans)
		}
	}
	t.Logf("51.5 t at 107 km: %v", err)
}
