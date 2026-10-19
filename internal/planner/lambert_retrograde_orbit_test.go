package planner

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// circularAt returns the state on a circular orbit of the given world-frame
// inclination at true anomaly nu.
func circularAt(a, incDeg, nuDeg, mu float64) orbital.Vec3State {
	el := orbital.Elements{A: a, I: incDeg * math.Pi / 180, Omega: 0.7}
	nu := nuDeg * math.Pi / 180
	return orbital.Vec3State{
		R: orbital.PositionAtTrueAnomaly(el, nu),
		V: orbital.VelocityAtTrueAnomaly(el, nu, mu),
	}
}

// Audit 2026-10-18 #4: Lambert's short/long-way rule reads the world-frame Z
// of r1 x r2, so any orbit above 90 degrees in the world frame (retrograde,
// or an Earth-polar orbit on the tilted-axis side) was solved the long way
// round. Callers must pass the orbit's own sense.
func TestLambertCallersFollowOrbitSense(t *testing.T) {
	const mu = earthMu
	a := 6378e3 + 500e3
	period := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	tof := 0.15 * period

	for _, inc := range []float64{28.5, 80, 91, 100, 120, 180} {
		dep := circularAt(a, inc, 0, mu)
		arr := circularAt(a, inc, 54, mu) // 0.15 period of mean motion = 54 deg

		plan, err := PlanIntraPrimaryFused(mu, dep.R, dep.V, arr.R, arr.V, tof,
			0, "earth", 4.9e12, 1.9e6, "moon")
		if err != nil {
			t.Errorf("i=%v fused: %v", inc, err)
		} else if plan.Departure.DV > 1 {
			t.Errorf("i=%v fused departure DV = %.1f m/s on a same-orbit transfer, want ~0", inc, plan.Departure.DV)
		}

		chaser := circularAt(a, inc, 0, mu)
		target := circularAt(a, inc, 1, mu)
		adv := RecommendRendezvousNudge(chaser, target, bodies.CelestialBody{}, mu, 3*period, 1e5)
		if adv.LambertIdealDV > 100 {
			t.Errorf("i=%v nudge LambertIdealDV = %.1f m/s for a 1 deg phasing gap, want small (%q)", inc, adv.LambertIdealDV, adv.Reason)
		}
	}
}
