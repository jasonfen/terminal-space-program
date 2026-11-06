package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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
	return w, c
}

// flyDeorbit flies a planted deorbit through the real integrator: waits for the
// burn, then lights a 90 % surface-retrograde brake at the cue and returns the
// stop point's offset from the site.
func flyDeorbit(t *testing.T, w *World, c *spacecraft.Spacecraft, lightDelaySec, brakeThrottle float64) (long, cross, stopAlt float64) {
	t.Helper()
	w.Clock.WarpIdx = 0
	w.Clock.BaseStep = 250 * time.Millisecond
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
				c.ManualBurn = &spacecraft.ManualBurn{}
				lit = true
			}
		}
		if lit {
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
	t.Logf("nodes=%d burn=%v v=%.1f t=%v", len(c.Nodes), c.ActiveBurn != nil, c.State.V.Norm(), w.Clock.SimTime)
	lb, ok := w.LandBesideReading()
	if !ok || !lb.ImpactOK || lb.Stop || math.Abs(lb.MissM) > 1000 || math.Abs(lb.CrossM) > 1000 {
		t.Errorf("after the burn: %+v, want the free-fall within 1 km of the site", lb)
	}
}
