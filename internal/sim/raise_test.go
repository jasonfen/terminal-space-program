package sim

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// raiseSat is a lone Starlink (0.17 N argon Hall thruster, 20 kg) on a
// circular equatorial Earth orbit at altM, with a network connection.
func raiseSat(t *testing.T, altM float64) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	c := w.Crafts[0]
	c.Stages = []spacecraft.Stage{spacecraft.NewFromLoadout("Falcon-9-Starlink-24").Stages[2]}
	c.Name, c.DockedComponents, c.Nodes, c.Landed = "Starlink-1", nil, nil, false
	c.SyncFields()
	c.State = circularAt(c, altM)
	w.Crafts = w.Crafts[:1]
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{c.ID: true}}
	return w, c
}

func circularAt(c *spacecraft.Spacecraft, altM float64) physics.StateVector {
	r := c.Primary.RadiusMeters() + altM
	v := math.Sqrt(c.Primary.GravitationalParameter() / r)
	return physics.StateVector{R: orbital.Vec3{X: r}, V: orbital.Vec3{Y: v}, M: c.TotalMass()}
}

// eccentricAt puts c on an equatorial orbit of semi-major axis a (alt aboveR)
// and eccentricity e, at periapsis.
func eccentricAt(c *spacecraft.Spacecraft, altM, e float64) physics.StateVector {
	mu := c.Primary.GravitationalParameter()
	a := c.Primary.RadiusMeters() + altM
	el := orbital.Elements{A: a, E: e}
	return physics.StateVector{R: orbital.PositionAtTrueAnomaly(el, 0), V: orbital.VelocityAtTrueAnomaly(el, 0, mu), M: c.TotalMass()}
}

func semiMajor(c *spacecraft.Spacecraft) float64 { return orbitalA(c) }

func drainRaiseText(w *World) []string {
	var out []string
	for _, e := range w.PendingRaiseEvents {
		out = append(out, e.Text)
	}
	w.PendingRaiseEvents = nil
	return out
}

// runRaise ticks the world at the top warp until the order ends (or max ticks).
func runRaise(t *testing.T, w *World, c *spacecraft.Spacecraft) int {
	t.Helper()
	w.Clock.WarpIdx = len(WarpFactors) - 1
	for i := 1; i <= 2000; i++ {
		w.Tick()
		if c.Raise == nil {
			return i
		}
	}
	t.Fatalf("raise still running after 2000 ticks, a = %.0f", semiMajor(c))
	return 0
}

// Guard 12 (sim side): every refusal string, and a refused order leaves nothing behind.
func TestCommandRaiseRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(w *World, c *spacecraft.Spacecraft)
		alt    float64
		want   string // "" = accepted
	}{
		{"round orbit, in reach", func(*World, *spacecraft.Spacecraft) {}, 550e3, ""},
		{"no engine", func(_ *World, c *spacecraft.Spacecraft) { c.Stages[0].Thrust = 0; c.SyncFields() }, 550e3, "raise: no engine"},
		{"thrust too high", func(_ *World, c *spacecraft.Spacecraft) {
			c.Stages = spacecraft.NewFromLoadout("Station-Keeper").Stages
			c.SyncFields()
			c.State = circularAt(c, 290e3)
		}, 550e3, "raise: thrust too high for a spiral"},
		{"orbit not round", func(_ *World, c *spacecraft.Spacecraft) { c.State = eccentricAt(c, 500e3, 0.06) }, 700e3, "raise: orbit not round"},
		{"eccentricity at the bound", func(_ *World, c *spacecraft.Spacecraft) { c.State = eccentricAt(c, 500e3, 0.05) }, 700e3, ""},
		{"below the atmosphere", func(*World, *spacecraft.Spacecraft) {}, 100e3, "raise: below the atmosphere"},
		{"at the atmosphere cutoff", func(*World, *spacecraft.Spacecraft) {}, 150e3, ""},
		{"outside the body's reach", func(*World, *spacecraft.Spacecraft) {}, 500_000e3, "raise: outside Earth's reach"},
		{"landed", func(_ *World, c *spacecraft.Spacecraft) { c.Landed = true }, 550e3, "raise: not free"},
		{"crashed", func(_ *World, c *spacecraft.Spacecraft) { c.Crashed = true }, 550e3, "raise: not free"},
		{"docked", func(_ *World, c *spacecraft.Spacecraft) {
			c.DockedComponents = []spacecraft.DockedComponent{{}, {}}
		}, 550e3, "raise: not free"},
		{"mid-burn", func(_ *World, c *spacecraft.Spacecraft) { c.ManualBurn = &spacecraft.ManualBurn{} }, 550e3, "raise: not free"},
		{"probe with no link", func(w *World, _ *spacecraft.Spacecraft) { w.CommGraph = &CommGraph{Connected: map[uint64]bool{}} }, 550e3, "NO SIGNAL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, c := raiseSat(t, 290e3)
			tc.mutate(w, c)
			err := w.CommandRaise(0, tc.alt)
			if tc.want == "" {
				if err != nil || c.Raise == nil || !c.Raise.Started {
					t.Fatalf("want accepted, err %v order %+v", err, c.Raise)
				}
				return
			}
			if err == nil || err.Error() != tc.want {
				t.Fatalf("err %v, want %q", err, tc.want)
			}
			if c.Raise != nil {
				t.Errorf("refused order left %+v behind", c.Raise)
			}
		})
	}
}

// 290 -> 550 km under top warp: the order completes, the flash reads
// "raise complete, 550 km", the argon and time match the rocket equation.
func TestRaiseCompletesWithRocketEquationFuelAndTime(t *testing.T) {
	w, c := raiseSat(t, 290e3)
	if err := w.CommandRaise(0, 550e3); err != nil {
		t.Fatal(err)
	}
	m0, fuel0, start := c.TotalMass(), c.ActiveStageFuel(), w.Clock.SimTime
	mu, re := c.Primary.GravitationalParameter(), c.Primary.RadiusMeters()
	dv := math.Sqrt(mu/(re+290e3)) - math.Sqrt(mu/(re+550e3))
	ve := c.Isp * 9.80665
	wantBurn := m0 * (1 - math.Exp(-dv/ve))
	wantSecs := wantBurn / c.MassFlowRateAt(1)

	ticks := runRaise(t, w, c)

	if got := drainRaiseText(w); len(got) != 1 || got[0] != "Starlink-1: raise complete, 550 km" {
		t.Errorf("flashes %q", got)
	}
	if v := c.State.V.Norm(); math.Abs(v-math.Sqrt(mu/(re+550e3))) > 0.5 {
		t.Errorf("end speed %.2f", v)
	}
	if burned := fuel0 - c.ActiveStageFuel(); math.Abs(burned-wantBurn) > 0.02 {
		t.Errorf("burned %.4f kg, rocket equation %.4f", burned, wantBurn)
	}
	if el := w.Clock.SimTime.Sub(start).Seconds(); math.Abs(el-wantSecs) > 5001 {
		t.Errorf("took %.0f s, want %.0f", el, wantSecs)
	}
	if c.State.M != c.TotalMass() {
		t.Errorf("State.M %.3f != TotalMass %.3f", c.State.M, c.TotalMass())
	}
	if ticks > 200 {
		t.Errorf("%d ticks at top warp; ADR says about 140", ticks)
	}
}

// A lowering is the same spiral and reads "lower".
func TestLoweringReadsLower(t *testing.T) {
	w, c := raiseSat(t, 550e3)
	if err := w.CommandRaise(0, 200e3); err != nil {
		t.Fatal(err)
	}
	runRaise(t, w, c)
	if got := drainRaiseText(w); len(got) != 1 || got[0] != "Starlink-1: lower complete, 200 km" {
		t.Errorf("flashes %q", got)
	}
	mu, re := c.Primary.GravitationalParameter(), c.Primary.RadiusMeters()
	if v := c.State.V.Norm(); math.Abs(v-math.Sqrt(mu/(re+200e3))) > 0.5 {
		t.Errorf("end speed %.2f", v)
	}
}

// Guard 4 (tick independence): one 5,000 s tick and 100,000 ticks of 0.05 s
// end within 1 m and 1 mm/s, mass and argon carried through both.
func TestRaiseTickSizeDoesNotMatter(t *testing.T) {
	run := func(d time.Duration, n int) (*spacecraft.Spacecraft, *World) {
		w, c := raiseSat(t, 290e3)
		if err := w.CommandRaise(0, 550e3); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < n; i++ {
			w.Clock.SimTime = w.Clock.SimTime.Add(d)
			if !w.advanceRaise(c, d) {
				t.Fatal("advanceRaise declined a started order")
			}
		}
		return c, w
	}
	check := func(label string, coarse, fine *spacecraft.Spacecraft) {
		if d := coarse.State.R.Sub(fine.State.R).Norm(); d > 1 {
			t.Errorf("%s: positions differ by %.3f m", label, d)
		}
		if d := coarse.State.V.Sub(fine.State.V).Norm(); d > 1e-3 {
			t.Errorf("%s: velocities differ by %.6f m/s", label, d)
		}
		if d := math.Abs(coarse.ActiveStageFuel() - fine.ActiveStageFuel()); d > 1e-6 {
			t.Errorf("%s: argon differs by %.3g kg", label, d)
		}
	}
	// Mass changes accumulate across ticks, so also compare 300,000 s of
	// raise taken as 60 ticks of 5,000 s and as 6,000 ticks of 50 s.
	long5000, _ := run(5000*time.Second, 60)
	long50, _ := run(50*time.Second, 6000)
	check("300,000 s", long5000, long50)
	coarse, _ := run(5000*time.Second, 1)
	fine, _ := run(50*time.Millisecond, 100_000)
	check("5,000 s", coarse, fine)
}

// A 1 kg tank ends the raise with the exhausted flash and a coasting sat.
func TestRaiseArgonExhaustionCoasts(t *testing.T) {
	w, c := raiseSat(t, 290e3)
	c.Stages[0].FuelMass = 1
	c.SyncFields()
	if err := w.CommandRaise(0, 550e3); err != nil {
		t.Fatal(err)
	}
	runRaise(t, w, c)
	a := semiMajor(c)
	alt := (a - c.Primary.RadiusMeters()) / 1000
	got := drainRaiseText(w)
	if want := fmt.Sprintf("Starlink-1: argon exhausted at %.0f km", alt); len(got) != 1 || got[0] != want {
		t.Errorf("flashes %q, want %q", got, want)
	}
	if alt <= 290 || alt >= 550 {
		t.Errorf("exhausted at %.1f km, want between the start and the target", alt)
	}
	if c.ActiveStageFuel() > 1e-9 {
		t.Errorf("tank not dry: %g", c.ActiveStageFuel())
	}
	w.Tick()
	w.Tick()
	if d := math.Abs(semiMajor(c) - a); d > 1 {
		t.Errorf("coasting sat's orbit moved %.3f m", d)
	}
}

// Guard 5: a raise caps nothing; a planted node on the same vessel does.
func TestRaiseDoesNotCapWarp(t *testing.T) {
	w, c := raiseSat(t, 290e3)
	if err := w.CommandRaise(0, 550e3); err != nil {
		t.Fatal(err)
	}
	w.Clock.WarpIdx = len(WarpFactors) - 1
	if got := w.clampedWarp(); got != 100000 {
		t.Errorf("clampedWarp %v with a raise, want 100000", got)
	}
	if w.AnyCraftThrusting() {
		t.Error("a raise reads as thrusting")
	}
	w.PlanNode(ManeuverNode{TriggerTime: w.Clock.SimTime.Add(time.Hour), Duration: 30 * time.Minute, DV: 0.5, Mode: spacecraft.BurnPrograde})
	if len(c.Nodes) == 0 {
		t.Fatal("node not planted")
	}
	for i := 0; i < 5000 && c.ActiveBurn == nil; i++ {
		w.Tick()
	}
	if c.ActiveBurn == nil {
		t.Fatalf("planted node never fired: nodes %d, now %v, start %v, warp %v", len(c.Nodes), w.Clock.SimTime, time.Time{}, w.clampedWarp())
	}
	if got := w.clampedWarp(); got != 10 {
		t.Errorf("clampedWarp %v with the node burning, want 10", got)
	}
	if !w.AnyCraftThrusting() {
		t.Error("a node burn does not read as thrusting")
	}
}

// At 1x the Verlet path cannot carry a 0.17 N thrust; the raise still does.
func TestRaiseRunsAtOneTimes(t *testing.T) {
	w, c := raiseSat(t, 290e3)
	if err := w.CommandRaise(0, 550e3); err != nil {
		t.Fatal(err)
	}
	a0, f0 := semiMajor(c), c.ActiveStageFuel()
	for i := 0; i < 200; i++ {
		w.Tick()
	}
	if semiMajor(c) <= a0 || c.ActiveStageFuel() >= f0 {
		t.Errorf("a %.3f -> %.3f, argon %.9f -> %.9f", a0, semiMajor(c), f0, c.ActiveStageFuel())
	}
}

// Guard 6: an e = 0.05 orbit keeps its eccentricity (within 0.005) and its
// periapsis never drops more than 1 km below the start.
func TestRaiseHoldsEccentricityAndPeriapsis(t *testing.T) {
	w, c := raiseSat(t, 500e3)
	c.State = eccentricAt(c, 500e3, 0.05)
	if err := w.CommandRaise(0, 700e3); err != nil {
		t.Fatal(err)
	}
	mu := c.Primary.GravitationalParameter()
	el0 := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	peri0 := el0.A * (1 - el0.E)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	for i := 0; i < 2000 && c.Raise != nil; i++ {
		w.Tick()
		el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
		if p := el.A * (1 - el.E); p < peri0-1000 {
			t.Fatalf("periapsis %.0f below start %.0f", p, peri0)
		}
		if math.Abs(el.E-el0.E) > 0.005 {
			t.Fatalf("e %.4f drifted from %.4f", el.E, el0.E)
		}
	}
	if c.Raise != nil {
		t.Fatal("raise never completed")
	}
}

// A waiting order coasts until StartAt, thrusts from StartAt (not from the
// next tick), and StopRaise clears it with a flash.
func TestWaitingRaiseStartsAtStartAt(t *testing.T) {
	w, c := raiseSat(t, 290e3)
	start := w.Clock.SimTime.Add(3 * time.Hour)
	c.Raise = &spacecraft.RaiseOrder{TargetAltM: 550e3, StartAt: start}
	a0 := semiMajor(c)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	for w.Clock.SimTime.Before(start) {
		w.Tick()
		if w.Clock.SimTime.Before(start) && (c.Raise.Started || math.Abs(semiMajor(c)-a0) > 1) {
			t.Fatalf("moved before StartAt at %v", w.Clock.SimTime)
		}
	}
	over := w.Clock.SimTime.Sub(start).Seconds() // thrust seconds this tick owes
	if !c.Raise.Started {
		t.Fatal("not started once SimTime >= StartAt")
	}
	want := 20 - over*c.MassFlowRateAt(1)
	if d := math.Abs(c.ActiveStageFuel() - want); d > 1e-6 {
		t.Errorf("argon %.9f, want %.9f (thrust for the %.0f s past StartAt)", c.ActiveStageFuel(), want, over)
	}
	if !w.StopRaise(0) || c.Raise != nil {
		t.Fatal("StopRaise left the order")
	}
	if got := drainRaiseText(w); len(got) != 1 || !strings.HasPrefix(got[0], "Starlink-1: raise stopped at ") {
		t.Errorf("flashes %q", got)
	}
	if w.StopRaise(0) {
		t.Error("StopRaise with no order returned true")
	}
}
