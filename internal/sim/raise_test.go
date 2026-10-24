package sim

import (
	"fmt"
	"math"
	"sort"
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

// RaiseRefusal is CommandRaise's checks with no order: the same text for every
// refusal, nothing set on success. RaisePlan is the arithmetic the card prints:
// 290 to 550 km is 146.7 m/s, 5.0 kg and 695,4xx s (8d01h). The ADR's 8d 02h
// divided the rounded 147 m/s; S4's 697,5xx s held the starting mass; the
// mass falls 5 kg of 808 over the climb, so the run takes 0.3% less.
func TestRaiseRefusalAndPlan(t *testing.T) {
	w, c := raiseSat(t, 290e3)
	if err := w.RaiseRefusal(0, 550e3); err != nil || c.Raise != nil {
		t.Fatalf("RaiseRefusal = %v, order %+v: it must refuse nothing and set nothing", err, c.Raise)
	}
	for _, alt := range []float64{50e3, 550e3} {
		w2, c2 := raiseSat(t, 290e3)
		if alt == 550e3 {
			c2.Thrust = 1e6 // chemical
		}
		want := w2.RaiseRefusal(0, alt)
		got := w2.CommandRaise(0, alt)
		if (want == nil) != (got == nil) || (want != nil && want.Error() != got.Error()) {
			t.Errorf("alt %.0f: RaiseRefusal %v, CommandRaise %v", alt, want, got)
		}
		if want != nil && c2.Raise != nil {
			t.Errorf("alt %.0f: a refused order left %+v", alt, c2.Raise)
		}
	}
	dv, kg, dur := RaisePlan(c, 550e3)
	if math.Abs(dv-146.7) > 0.1 || math.Abs(kg-5.04) > 0.05 || dur.Seconds() < 695000 || dur.Seconds() > 696000 {
		t.Errorf("RaisePlan = %.1f m/s, %.2f kg, %.0f s", dv, kg, dur.Seconds())
	}
	// The card must agree with the run: the same argon, and the finish within
	// the one 5,000 s tick the top warp steps by.
	fuel0 := c.ActiveStageFuel()
	if err := w.CommandRaise(0, 550e3); err != nil {
		t.Fatal(err)
	}
	ticks := runRaise(t, w, c)
	if used := fuel0 - c.ActiveStageFuel(); math.Abs(used-kg) > 0.01 {
		t.Errorf("run burned %.3f kg, the plan said %.3f", used, kg)
	}
	if got := float64(ticks) * 5000; math.Abs(got-dur.Seconds()) > 5000 {
		t.Errorf("run took %v s (%d ticks), the plan said %.0f", got, ticks, dur.Seconds())
	}
	if dvL, kgL, durL := RaisePlan(c, 200e3); dvL <= 0 || kgL <= 0 || durL <= 0 {
		t.Errorf("a lowering costs too: %.1f m/s %.2f kg %v", dvL, kgL, durL)
	}
}

// spreadBatch releases a fresh Falcon 9 + Starlink x24 at altM (stack angleDeg
// off the velocity, dry carrier), links every vessel, and returns the world,
// the batch id and the 24 members' slate indices.
func spreadBatch(t *testing.T, altM, angleDeg float64) (*World, uint64, []int) {
	t.Helper()
	w, idx := batchFixtureAt(t, altM, angleDeg, true)
	if !w.ReleaseBatch(idx) {
		t.Fatalf("ReleaseBatch refused at %v degrees", angleDeg)
	}
	w.EnsureCraftIDs()
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{}}
	for _, c := range w.Crafts {
		w.CommGraph.Connected[c.ID] = true
	}
	id := w.Crafts[idx].BatchID
	return w, id, w.SpreadMembers(id)
}

// colocate puts every member on member 0's exact state: the ADR's "co-located
// members", with no fan spread to confuse a hand-computed stagger.
func colocate(w *World, m []int) {
	for _, i := range m[1:] {
		w.Crafts[i].State = w.Crafts[m[0]].State
	}
}

// rotateAbout turns v by ang radians about the unit axis (Rodrigues).
func rotateAbout(v, axis orbital.Vec3, ang float64) orbital.Vec3 {
	c, s := math.Cos(ang), math.Sin(ang)
	return v.Scale(c).Add(axis.Cross(v).Scale(s)).Add(axis.Scale(axis.Dot(v) * (1 - c)))
}

// ringGaps is the sorted in-plane neighbour gaps, in degrees, of the members now.
func ringGaps(w *World, m []int) []float64 {
	c0 := w.Crafts[m[0]]
	n := c0.State.R.Cross(c0.State.V).Unit()
	x := c0.State.R.Unit()
	y := n.Cross(x)
	var angs []float64
	for _, i := range m {
		r := w.Crafts[i].State.R
		a := math.Atan2(r.Dot(y), r.Dot(x)) * 180 / math.Pi
		angs = append(angs, math.Mod(a+360, 360))
	}
	sort.Float64s(angs)
	gaps := make([]float64, len(angs))
	for i := range angs {
		gaps[i] = math.Mod(angs[(i+1)%len(angs)]-angs[i]+360, 360)
	}
	return gaps
}

// flySpread ticks at the top warp until no member has an order. Returns the
// seconds one tick advanced (the claim "5,000 s ticks" is read, not assumed).
func flySpread(t *testing.T, w *World, m []int) float64 {
	t.Helper()
	w.Clock.WarpIdx = len(WarpFactors) - 1
	t0 := w.Clock.SimTime
	w.Tick()
	tick := w.Clock.SimTime.Sub(t0).Seconds()
	for n := 0; n < 400; n++ {
		busy := false
		for _, i := range m {
			busy = busy || w.Crafts[i].Raise != nil
		}
		if !busy {
			return tick
		}
		w.Tick()
	}
	t.Fatal("spread still running after 400 ticks")
	return 0
}

func checkEvenRing(t *testing.T, w *World, m []int) {
	t.Helper()
	for i, g := range ringGaps(w, m) {
		if math.Abs(g-15) > 1 {
			t.Errorf("gap %d = %.3f deg, want 15 +- 1", i, g)
		}
	}
}

func sortedWaits(s SpreadSchedule) []float64 {
	var ws []float64
	for _, sl := range s.Slots {
		ws = append(ws, sl.Wait.Seconds())
	}
	sort.Float64s(ws)
	return ws
}

// Guard 17, waits: co-located members stagger by one slot over the rate at
// which a member outruns the target orbit. The reference is computed here from
// mu and the radii, not copied from the ADR, whose 4,034 s / 21,800 s are
// rounded (the code and this arithmetic give 4,038.4 s / 21,826.2 s; the
// ADR's +-300 s at 500 km is passed by j = 11 and missed by j = 23).
func TestSpreadWaitsAreTheStagger(t *testing.T) {
	for _, alt := range []float64{290e3, 500e3} {
		t.Run(fmt.Sprintf("%.0f km", alt/1000), func(t *testing.T) {
			w, id, m := spreadBatch(t, alt, 10)
			colocate(w, m)
			sch, err := w.SpreadPlan(id, 550e3)
			if err != nil {
				t.Fatal(err)
			}
			c := w.Crafts[m[0]]
			mu, R := c.Primary.GravitationalParameter(), c.Primary.RadiusMeters()
			n := func(a float64) float64 { return math.Sqrt(mu / (a * a * a)) }
			slot := (2 * math.Pi / 24) / (n(orbitalA(c)) - n(R+550e3)) // the members' own a, which the spawn leaves a few hundred m off R+alt
			for j, got := range sortedWaits(sch) {
				if want := float64(j) * slot; math.Abs(got-want) > 1 {
					t.Errorf("wait %d = %.1f s, want %.1f", j, got, want)
				}
			}
			if sch.First() != 0 {
				t.Errorf("first start %v, want now", sch.First())
			}
		})
	}
}

// Guard 17, the ring: release at 10 degrees off the velocity and radial, spread
// and fly at the top warp to completion; every neighbour gap is 15 +- 1.
func TestSpreadEvensTheRing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alt   float64
		angle float64
	}{{"500 km, 10 deg", 500e3, 10}, {"500 km, radial", 500e3, 90}, {"290 km, 10 deg", 290e3, 10}, {"290 km, radial", 290e3, 90}} {
		t.Run(tc.name, func(t *testing.T) {
			w, id, m := spreadBatch(t, tc.alt, tc.angle)
			if err := w.SpreadBatch(id, 550e3); err != nil {
				t.Fatal(err)
			}
			if tick := flySpread(t, w, m); math.Abs(tick-5000) > 1 {
				t.Fatalf("tick = %.1f s, want 5000", tick)
			}
			checkEvenRing(t, w, m)
			for _, i := range m {
				if a := semiMajor(w.Crafts[i]) - w.Crafts[i].Primary.RadiusMeters(); math.Abs(a-550e3) > 1 {
					t.Errorf("%s ended at %.1f m", w.Crafts[i].Name, a)
				}
			}
		})
	}
}

// A member 5 degrees ahead of the rest takes the slot ahead and no wait is
// negative; a member 2% off in semi-major axis (a lowering) lands on its slot.
func TestSpreadMemberOffTheBatch(t *testing.T) {
	t.Run("5 degrees ahead", func(t *testing.T) {
		w, id, m := spreadBatch(t, 500e3, 10)
		colocate(w, m)
		ahead := w.Crafts[m[11]]
		n := ahead.State.R.Cross(ahead.State.V).Unit()
		ahead.State.R = rotateAbout(ahead.State.R, n, 5*math.Pi/180)
		ahead.State.V = rotateAbout(ahead.State.V, n, 5*math.Pi/180)
		sch, err := w.SpreadPlan(id, 550e3)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range sch.Slots {
			if s.Wait < 0 {
				t.Errorf("%s waits %v", w.Crafts[s.Idx].Name, s.Wait)
			}
		}
		// Last in the cyclic order, it takes the last slot 5 degrees early.
		mu, R := ahead.Primary.GravitationalParameter(), ahead.Primary.RadiusMeters()
		rate := math.Sqrt(mu/math.Pow(orbitalA(ahead), 3)) - math.Sqrt(mu/math.Pow(R+550e3, 3))
		var got time.Duration
		for _, s := range sch.Slots {
			if s.Idx == m[11] {
				got = s.Wait
			}
		}
		want := 23*(2*math.Pi/24)/rate - 5*math.Pi/180/rate
		if math.Abs(got.Seconds()-want) > 1 {
			t.Errorf("the member 5 deg ahead waits %.1f s, want %.1f (the last slot less 5 deg)", got.Seconds(), want)
		}
		if err := w.SpreadBatch(id, 550e3); err != nil {
			t.Fatal(err)
		}
		flySpread(t, w, m)
		checkEvenRing(t, w, m)
	})
	t.Run("2 percent off in a", func(t *testing.T) {
		w, id, m := spreadBatch(t, 500e3, 10)
		off := w.Crafts[m[7]]
		s := off.State
		off.State.R = s.R.Scale(1.02)
		off.State.V = s.V.Scale(1 / math.Sqrt(1.02))
		if err := w.SpreadBatch(id, 550e3); err != nil {
			t.Fatal(err)
		}
		flySpread(t, w, m)
		checkEvenRing(t, w, m)
	})
}

// Guard 17, the refusals, each with its equal case at the threshold: 0.5 deg
// off the median plane goes, 0.51 refuses; a member 1 km from the target
// refuses, 1.001 km goes; 60 d goes, 60 d and a second refuses; 500 to 502 km
// is too slow, to 505 km it goes.
func TestSpreadRefusals(t *testing.T) {
	tilt := func(deg float64) func(*World, []int) {
		return func(w *World, m []int) {
			colocate(w, m)
			c := w.Crafts[m[4]]
			c.State.V = rotateAbout(c.State.V, c.State.R.Unit(), deg*math.Pi/180)
		}
	}
	// within puts member 2 on a circle d metres above the 550 km target, in its own plane.
	within := func(d float64) func(*World, []int) {
		return func(w *World, m []int) {
			colocate(w, m)
			c := w.Crafts[m[2]]
			f := (c.Primary.RadiusMeters() + 550e3 + d) / orbitalA(c)
			c.State.R, c.State.V = c.State.R.Scale(f), c.State.V.Scale(1/math.Sqrt(f))
		}
	}
	cases := []struct {
		name   string
		mutate func(*World, []int)
		alt    float64
		want   string // "" = accepted
	}{
		{"even batch", func(w *World, m []int) { colocate(w, m) }, 550e3, ""},
		{"0.5 deg off the plane", tilt(0.5), 550e3, ""},
		{"0.51 deg off the plane", tilt(0.51), 550e3, "spread: not in one plane"},
		{"member 1 km from the target", within(1000), 550e3, "spread: Starlink-3 already at 550 km"},
		{"member 1.001 km from the target", within(1001), 550e3, ""}, // outside the rule; its tiny phase rate is served by starting it first
		{"500 to 502 km", func(w *World, m []int) { colocate(w, m) }, 502e3, "spread: too slow from this orbit"},
		{"500 to 505 km", func(w *World, m []int) { colocate(w, m) }, 505e3, ""},
		{"a member with no engine", func(w *World, m []int) { w.Crafts[m[6]].Stages[0].Thrust = 0; w.Crafts[m[6]].SyncFields() }, 550e3, "spread: Starlink-7: no engine"},
		{"a member with no link", func(w *World, m []int) { w.CommGraph.Connected[w.Crafts[m[9]].ID] = false }, 550e3, "spread: Starlink-10: NO SIGNAL"},
		{"below the atmosphere", func(*World, []int) {}, 100e3, "spread: Starlink-1: below the atmosphere"},
		{"one member only", func(w *World, m []int) {
			for _, i := range m[1:] {
				w.Crafts[i].BatchID = 0
			}
		}, 550e3, "spread: not on one orbit"},
		{"two bodies", func(w *World, m []int) { w.Crafts[m[3]].Primary.ID = "luna" }, 550e3, "spread: not on one orbit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, id, m := spreadBatch(t, 500e3, 10)
			tc.mutate(w, m)
			err := w.SpreadBatch(id, tc.alt)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want accepted, got %v", err)
				}
				return
			}
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("err %v, want prefix %q", err, tc.want)
			}
			for _, i := range m {
				if w.Crafts[i].Raise != nil {
					t.Fatalf("a refused spread left an order on %s", w.Crafts[i].Name)
				}
			}
		})
	}
	t.Run("the days in the refusal", func(t *testing.T) {
		w, id, m := spreadBatch(t, 500e3, 10)
		colocate(w, m)
		err := w.SpreadBatch(id, 502e3)
		if err == nil || !strings.Contains(err.Error(), "(") {
			t.Fatalf("err %v", err)
		}
		t.Log(err)
	})
	t.Run("60 d is the edge", func(t *testing.T) {
		if spreadTooSlow(60 * 24 * time.Hour) {
			t.Error("exactly 60 d refused")
		}
		if !spreadTooSlow(60*24*time.Hour + time.Second) {
			t.Error("60 d and a second went")
		}
	})
}

// Stop the spread drops the waiting orders and keeps the climbing ones; a hand
// raise replaces a member's scheduled order and leaves the schedule; the
// status counts come off the orders and the notice is gone when none is left.
func TestSpreadStopHandRaiseAndStatus(t *testing.T) {
	w, id, m := spreadBatch(t, 500e3, 10)
	if _, ok := w.SpreadStatus(id); ok {
		t.Fatal("status before any spread")
	}
	if err := w.SpreadBatch(id, 550e3); err != nil {
		t.Fatal(err)
	}
	st, ok := w.SpreadStatus(id)
	if !ok || st.Total != 24 || st.Climbing != 1 || st.Waiting != 23 || st.Arrived != 0 || st.Name != "Starlink" || st.TargetAltM != 550e3 {
		t.Fatalf("status at the start: %+v ok=%v", st, ok)
	}
	if st.NextName == "" || st.NextIn <= 0 {
		t.Errorf("next = %q in %v", st.NextName, st.NextIn)
	}

	var waiting int
	for _, i := range m {
		if o := w.Crafts[i].Raise; o != nil && !o.Started {
			waiting = i
			break
		}
	}
	if err := w.CommandRaise(waiting, 600e3); err != nil {
		t.Fatal(err)
	}
	if o := w.Crafts[waiting].Raise; o.BatchID != 0 || !o.Started || o.TargetAltM != 600e3 {
		t.Errorf("hand raise left %+v, want a started order with no batch id", o)
	}
	if st, _ := w.SpreadStatus(id); st.Waiting != 22 || st.Climbing != 1 || st.Stopped != 1 {
		t.Errorf("after a hand raise: %+v", st)
	}

	w.Clock.WarpIdx = len(WarpFactors) - 1
	for n := 0; n < 100; n++ {
		if st, _ := w.SpreadStatus(id); st.Climbing >= 3 {
			break
		}
		w.Tick()
	}
	before, _ := w.SpreadStatus(id)
	if n := w.StopSpread(id); n != before.Waiting {
		t.Errorf("StopSpread dropped %d, want the %d waiting", n, before.Waiting)
	}
	after, ok := w.SpreadStatus(id)
	if !ok || after.Waiting != 0 || after.Climbing != before.Climbing {
		t.Errorf("after stop: %+v (was %+v)", after, before)
	}
	flySpread(t, w, m)
	if _, ok := w.SpreadStatus(id); ok {
		t.Error("status after every order ended")
	}
}
