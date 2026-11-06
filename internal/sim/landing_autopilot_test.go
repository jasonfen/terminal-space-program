package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// flyLanding plants N on the deorbit world, engages Site+ and ticks until the
// vessel lands or crashes. It returns the true distance from the real site and
// the speed over the ground / inertial speed on the last tick before contact.
func flyLanding(t *testing.T, w *World, c *spacecraft.Spacecraft, maxTicks int, each func(i int)) (distM, lastSpeed float64, landed bool) {
	t.Helper()
	if _, err := w.PlanDeorbitOntoSite(); err != nil {
		t.Fatal(err)
	}
	if err := w.EngageLandingAutopilot(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxTicks; i++ {
		lastSpeed = c.State.V.Norm()
		w.Tick()
		if each != nil {
			each(i)
		}
		if c.Landed || c.Crashed {
			break
		}
	}
	s := w.Sites[0]
	return HaversineM(c.LandedLatDeg, c.LandedLonDeg, s.LatDeg, s.LonDeg, c.Primary.RadiusMeters()), lastSpeed, c.Landed
}

func TestLandingAutopilotLandsTheLanderOnTheFlag(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	dist, v, landed := flyLanding(t, w, c, 2*3600*4, nil)
	t.Logf("landed=%v crashed=%v dist=%.1f m lastspeed=%.2f phase=%v thr=%.2f alt=%.1f", landed, c.Crashed, dist, v, c.Landing.Phase, c.Throttle, c.Altitude())
	if !landed || dist > 50 || v >= CrashVCritMps {
		t.Errorf("landed=%v dist=%.1f m speed=%.2f m/s, want landed within 50 m under %v m/s", landed, dist, v, CrashVCritMps)
	}
}

func TestLandingTrace(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	flyLanding(t, w, c, 2*3600*4, func(i int) {
		if c.Landing.Phase == spacecraft.PhaseCoast && !c.Landed && !c.Crashed {
			return
		}
		if i%40 == 0 || c.Landed || c.Crashed {
			f := apFrameOf(c)
			t.Logf("t=%s ph=%s alt=%.0f vz=%.1f sh=%.1f thr=%.3f fuel=%.0f res=%v oor=%v", w.Clock.SimTime.Format("15:04:05"), c.Landing.Phase.Word(), f.alt, f.vz, f.sh, c.Throttle, c.ActiveStageFuel(), c.Landing.Reserve, c.Landing.OutOfReach)
		}
	})
}
