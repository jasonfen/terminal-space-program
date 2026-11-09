package sim

import (
	"errors"
	"math"
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
