package sim

import (
	"fmt"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// countingCalc counts mean-anomaly lookups: one per Kepler solve in
// bodyPositionIn / bodyVelocityIn / bodyPosRelTo, so it is the ephemeris
// evaluation count without touching production code.
type countingCalc struct {
	orbital.Calculator
	n int
}

func (c *countingCalc) CalculateMeanAnomaly(b bodies.CelestialBody, t time.Time) float64 {
	c.n++
	return c.Calculator.CalculateMeanAnomaly(b, t)
}

func moonOf(t *testing.T, w *World) bodies.CelestialBody {
	t.Helper()
	for _, b := range w.System().Bodies {
		if b.ID == "moon" {
			return b
		}
	}
	t.Fatal("no Moon")
	return bodies.CelestialBody{}
}

// hyperbolicEarthLeg puts the active craft on an Earth-escape trajectory at
// 100000x warp (the audit's case): free flight, no burn, Verlet path.
func hyperbolicEarthLeg(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.State.R = orbital.Vec3{X: 7e6}
	c.State.V = orbital.Vec3{Y: 12000}
	w.Clock.WarpIdx = len(WarpFactors) - 1
	return w
}

// TestVerletTickEphemerisCost: a free-flight Verlet tick must not re-solve
// every body each sub-step. Audit 19 measured ~19k Kepler solves per Tick
// here (1024 sub-steps x 19 Sol bodies, parents included). The bound allows
// the Moon's own solve every sub-step (the only body that can change an
// Earth-primary craft's SOI) and nothing like the full catalogue.
func TestVerletTickEphemerisCost(t *testing.T) {
	w := hyperbolicEarthLeg(t)
	cc := &countingCalc{Calculator: w.Calculator}
	w.Calculator = cc
	w.Tick()
	t.Logf("mean-anomaly lookups in one Tick: %d", cc.n)
	if cc.n == 0 {
		t.Fatal("counter saw nothing: the instrument is not wired to the Verlet loop")
	}
	if cc.n > 2048 {
		t.Errorf("one Tick did %d ephemeris lookups, want <= 2048", cc.n)
	}
}

// trajectoryTrace is what a scenario records: the primary at the end of
// every tick plus the final state.
type trajectoryTrace struct {
	primaries []string
	r, v      orbital.Vec3
}

func (a trajectoryTrace) sees(id string) bool {
	for _, p := range a.primaries {
		if p == id {
			return true
		}
	}
	return false
}

func runTrace(w *World, ticks int) trajectoryTrace {
	var tr trajectoryTrace
	c := w.ActiveCraft()
	for i := 0; i < ticks; i++ {
		w.Tick()
		tr.primaries = append(tr.primaries, c.Primary.ID)
	}
	tr.r, tr.v = c.State.R, c.State.V
	return tr
}

// moonFlybyWorld aims the active craft at the Moon from 500,000 km out,
// closing at 1.5 km/s relative, 20,000 km off-centre, at 100000x warp.
func moonFlybyWorld(t *testing.T) *World { return moonFlybyFrom(t, 150e6) }

// moonFlybyFrom starts the flyby startM metres from the Moon; shifting it
// shifts where in a tick the SOI crossings fall.
func moonFlybyFrom(t *testing.T, startM float64) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	moon := moonOf(t, w)
	t0 := w.Clock.SimTime
	earth := bodyByID(t, w.System(), "earth")
	mp := w.bodyPosRelTo(moon, earth, t0)
	mv := w.bodyPosRelTo(moon, earth, t0.Add(time.Second)).Sub(mp)
	e1 := mp.Unit()
	e2 := orbital.Vec3{Z: 1}.Cross(e1).Unit()
	c := w.ActiveCraft()
	c.State.R = mp.Add(e1.Scale(startM)).Add(e2.Scale(8e6))
	c.State.V = mv.Add(e1.Scale(-2000))
	w.Clock.WarpIdx = len(WarpFactors) - 1
	return w
}

// moonExitBurnWorld: a craft 1000 km inside the Moon's SOI edge, outbound,
// burning prograde at the 10x burn clamp.
func moonExitBurnWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	moon := moonOf(t, w)
	earth := bodyByID(t, w.System(), "earth")
	c := w.ActiveCraft()
	soi := physics.SOIRadius(moon, earth)
	c.Primary = moon
	c.State.R = orbital.Vec3{X: soi - 1000e3}
	c.State.V = orbital.Vec3{X: 800}
	w.Clock.WarpIdx = 1
	w.StartManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("manual burn did not start")
	}
	return w
}

// reentryWorld: Earth perigee 60 km, drag active, 10x warp.
func reentryWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.State.R = orbital.Vec3{X: 6371e3 + 120e3}
	c.State.V = orbital.Vec3{Y: 7000, X: -1500}
	w.Clock.WarpIdx = 1
	return w
}

// transitions is the tick index and new primary of every SOI change.
func (a trajectoryTrace) transitions() string {
	out, prev := "", ""
	for i, p := range a.primaries {
		if p != prev {
			out += fmt.Sprintf("%d:%s ", i, p)
			prev = p
		}
	}
	return out
}

// TestVerletShortcutMatchesFullRefresh: the shortcut skips the refresh only
// when FindPrimary would return the current primary anyway, so a trajectory
// flown with it must equal, bit for bit, the one flown with the full
// per-sub-step refresh (same binary, same arch, so no tolerance). Each
// scenario must change primary or drag, or it would prove nothing.
func TestVerletShortcutMatchesFullRefresh(t *testing.T) {
	defer func(v bool) { liveSOIShortcut = v }(liveSOIShortcut)
	for _, s := range []struct {
		name  string
		mk    func(*testing.T) *World
		ticks int
		want  string // SOI transitions the scenario must contain
	}{
		{"lunar flyby at 100000x", moonFlybyWorld, 60, "0:earth 8:moon 21:earth "},
		{"lunar flyby, crossings mid-tick", func(t *testing.T) *World { return moonFlybyFrom(t, 153.1e6) }, 60, "0:earth 8:moon 22:earth "},
		{"burn out of the Moon's SOI at 10x", moonExitBurnWorld, 1500, "0:moon 730:earth "},
		{"reentry drag at 10x", reentryWorld, 400, "0:earth "},
	} {
		liveSOIShortcut = false
		ref := runTrace(s.mk(t), s.ticks)
		liveSOIShortcut = true
		got := runTrace(s.mk(t), s.ticks)
		if ref.transitions() != s.want {
			t.Errorf("%s: scenario drifted, reference transitions %q want %q", s.name, ref.transitions(), s.want)
		}
		if got.transitions() != ref.transitions() || got.r != ref.r || got.v != ref.v {
			t.Errorf("%s: shortcut diverged from full refresh\n  transitions %q vs %q\n  dR=%g m dV=%g m/s",
				s.name, got.transitions(), ref.transitions(), got.r.Sub(ref.r).Norm(), got.v.Sub(ref.v).Norm())
		}
	}
}

// BenchmarkVerletTick is the audit's case: one craft on a hyperbolic Earth
// leg at 100000x. "full" runs the pre-shortcut loop (all-bodies refresh every
// sub-step); "shortcut" is the shipped path. Interleave with -count.
func BenchmarkVerletTick(b *testing.B) {
	defer func(v bool) { liveSOIShortcut = v }(liveSOIShortcut)
	for _, mode := range []struct {
		name string
		on   bool
	}{{"full", false}, {"shortcut", true}} {
		b.Run(mode.name, func(b *testing.B) {
			liveSOIShortcut = mode.on
			w := hyperbolicEarthLeg(&testing.T{})
			c := w.ActiveCraft()
			s := c.State
			for i := 0; i < b.N; i++ {
				c.State = s // same leg every iteration
				w.Tick()
			}
		})
	}
}
