package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Playtest 2026-10-16 (v0.56.1): "spawned a comsat carrier in 500km orbit,
// targeted moon, hohmann transferred, 6 minute+ transfer burn misses moon
// encounter". These tests pin the measured cause and the HUD that hid it.
// They are RED on purpose until the fix lands; see the GitHub issues.

// TestRefineFiniteDepartureHitsAimAtLowTWR: the [H] split departure is
// "refined" for the finite burn by refineFiniteDeparture, which Newton-
// iterates the Δv until a simulated finite burn reaches the far apsis.
// Its 8-iteration / 1 km tolerance never converges for a Moon aim from a
// 500 km parking orbit (Ap moves ~3-4 Mm per m/s there), so it returns
// on the "diverged" error and the plan keeps the IMPULSIVE Δv. At the
// Comsat Carrier x3's TWR (0.42 at ignition, a 622 s burn) that is ~34
// m/s of uncorrected gravity loss and an apoapsis ~100 Mm short of the
// Moon.
func TestRefineFiniteDepartureHitsAimAtLowTWR(t *testing.T) {
	const (
		mu     = 3.986004418e14
		rPark  = 6378137.0 + 500e3
		mass   = 16114.0 // Comsat Carrier x3 fully fuelled
		thrust = 67000.0 // NTR Tug
		isp    = 900.0
	)
	prograde := func(r, v orbital.Vec3) orbital.Vec3 {
		return spacecraft.DirectionUnit(spacecraft.BurnPrograde, r, v)
	}
	park := physics.StateVector{
		R: orbital.Vec3{X: rPark},
		V: orbital.Vec3{Y: math.Sqrt(mu / rPark)},
		M: mass,
	}
	// Aims the split's capture trim actually produced in the repro
	// (361.8, 377.5, 391.8, 406.8 Mm) across four epochs and two planes.
	for _, rFar := range []float64{361.764e6, 377.534e6, 391.757e6, 406.846e6} {
		var plan planner.TransferPlan
		plan.Departure.DV = math.Abs(splitRaiseDv(mu, rPark, rFar))
		impulsive := plan.Departure.DV
		refineFiniteDeparture(&plan, mu, rPark, mass, thrust, isp, rFar)
		post := planner.SimulateFiniteBurn(park, mu, thrust, isp, plan.Departure.DV, prograde)
		ap := orbital.ElementsFromState(post.R, post.V, mu).Apoapsis()
		t.Logf("aim %.1f Mm: impulsive Δv %.2f, refined Δv %.2f, finite-burn Ap %.1f Mm",
			rFar/1e6, impulsive, plan.Departure.DV, ap/1e6)
		if math.Abs(ap-rFar) > 0.01*rFar {
			t.Errorf("aim %.1f Mm: the refined departure (%.2f m/s, impulsive %.2f) flies to Ap %.1f Mm, want within 1%% of the aim",
				rFar/1e6, plan.Departure.DV, impulsive, ap/1e6)
		}
	}
}

// comsatMoonDeparture spawns the Comsat Carrier x3 in a 500 km orbit at the
// spawn form's default inclination, targets the Moon, plants [H], and
// flies the departure burn through the real sim path (Auto-Warp to the
// node, the planted finite burn). It returns the world after the burn and
// the closest the coasting trajectory comes to the Moon in the next 12
// days (analytic Kepler coast, sampled every 60 s, Moon from the
// ephemeris), plus the Moon's SOI radius. dvShortMps trims the planted
// departure before it fires (0 flies the plan as planted).
func comsatMoonDeparture(t *testing.T, dvShortMps float64) (w *World, moon bodies.CelestialBody, closest, rSOI float64) {
	t.Helper()
	return comsatMoonDepartureAt(t, spacecraft.SeedInclinationDeg, 0, dvShortMps)
}

// comsatMoonDepartureAt is comsatMoonDeparture from a chosen spawn
// inclination (deg) with the clock advanced by epoch before the spawn,
// so a test can reach geometries where [H] plants the combined strategy.
func comsatMoonDepartureAt(t *testing.T, inclDeg float64, epoch time.Duration, dvShortMps float64) (w *World, moon bodies.CelestialBody, closest, rSOI float64) {
	t.Helper()
	w = mustWorld(t)
	w.Clock.SimTime = w.Clock.SimTime.Add(epoch)
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID: "Comsat-Carrier-3", ParentBodyID: "earth",
		AltitudeM: 500e3, Inclination: inclDeg,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	return flyMoonDeparture(t, w, dvShortMps)
}

// flyMoonDeparture targets the Moon with the active vessel, plants [H],
// flies the departure burn through the real sim path and measures the
// coast to the Moon (see comsatMoonDeparture).
func flyMoonDeparture(t *testing.T, w *World, dvShortMps float64) (_ *World, moon bodies.CelestialBody, closest, rSOI float64) {
	t.Helper()
	c := w.ActiveCraft()
	moonIdx := -1
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			moonIdx = i
		}
	}
	if moonIdx < 0 {
		t.Skip("Moon missing from Sol")
	}
	moon = w.System().Bodies[moonIdx]
	w.SetTargetBody(moonIdx)
	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	if len(c.Nodes) == 0 || (c.Nodes[0].Mode != spacecraft.BurnPrograde && c.Nodes[0].Mode != spacecraft.BurnVector) {
		t.Fatalf("expected a prograde (split) or vector (combined) departure node first, got %+v", c.Nodes)
	}
	// dvShortMps > 0 deliberately under-flies the departure so a HUD test
	// sees a missed transfer whatever the planner delivers.
	c.Nodes[0].DV -= dvShortMps
	planted := c.Nodes[0]
	nodesBefore := len(c.Nodes)
	if !w.EngageAutoWarp() {
		t.Fatal("Auto-Warp refused to engage on the planted departure")
	}
	tickUntil(w, 5_000_000, func() bool { return w.AutoWarp == nil })
	tickUntil(w, 5_000_000, func() bool { return len(c.Nodes) < nodesBefore })
	if _, ok := tickUntil(w, 5_000_000, func() bool { return c.ActiveBurn == nil }); !ok {
		t.Fatal("departure burn never finished")
	}
	if c.Primary.ID != "earth" {
		t.Fatalf("after the departure the vessel orbits %s, want earth", c.Primary.ID)
	}
	mu := c.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	t.Logf("departure planted %.1f m/s over %v; post-burn Ap %.1f Mm, Pe alt %.0f km",
		planted.DV, planted.Duration, el.Apoapsis()/1e6, (el.Periapsis()-c.Primary.RadiusMeters())/1e3)

	closest = math.Inf(1)
	t0 := w.Clock.SimTime
	for s := 0.0; s <= 12*86400; s += 60 {
		st, ok := physics.KeplerStep(c.State, mu, s)
		if !ok {
			break
		}
		mr, _ := w.bodyParentRelativeState(moon, t0.Add(time.Duration(s*float64(time.Second))))
		if d := st.R.Sub(mr).Norm(); d < closest {
			closest = d
		}
	}
	sys := w.System()
	rSOI = physics.SOIRadius(moon, *sys.ParentOf(moon))
	return w, moon, closest, rSOI
}

// TestComsatCarrierHohmannDepartureReachesMoonSOI: the flown [H] departure
// of a low-TWR vessel must put it on a path into the Moon's SOI. Measured
// at v0.56.1 / main cce5aab: post-burn Ap 286.9 Mm, closest approach
// 152,591 km against a 66,169 km SOI. The same plan flown at 20x thrust
// reaches the SOI.
func TestComsatCarrierHohmannDepartureReachesMoonSOI(t *testing.T) {
	_, _, closest, rSOI := comsatMoonDeparture(t, 0)
	t.Logf("closest Moon approach after the departure: %.0f km (SOI %.0f km)", closest/1e3, rSOI/1e3)
	if closest > rSOI {
		t.Errorf("the flown [H] departure passes the Moon at %.0f km, outside its %.0f km SOI: the transfer misses",
			closest/1e3, rSOI/1e3)
	}
}

// finiteDepartureLegState returns the state the planted departure node
// leaves the vessel in when it is FLOWN as the centred finite burn the
// executor runs (rocket-equation mass loss, thrust following the node's
// mode), together with the clock at burn end. PredictedLegs applies a
// node's delta-v as an impulse, which is only a faithful picture of the
// flight when the planted delta-v is the impulsive one; since #666 the
// planted delta-v is the finite-burn-corrected one, so a test that
// integrates onward from the impulsive leg state overshoots by the
// gravity loss.
func finiteDepartureLegState(t *testing.T, w *World, nodeIdx int) (physics.StateVector, time.Time) {
	t.Helper()
	c := w.ActiveCraft()
	n := c.Nodes[nodeIdx]
	mu := c.Primary.GravitationalParameter()
	pre, _ := w.propagateStateWithPrimary(c.State, c.Primary, w.Clock.SimTime, n.TriggerTime.Sub(w.Clock.SimTime).Seconds())
	half := n.Duration.Seconds() / 2
	start, ok := physics.KeplerStep(pre, mu, -half)
	if !ok {
		t.Fatal("KeplerStep back to burn start failed")
	}
	mode := n.Mode
	fin := planner.SimulateFiniteBurn(start, mu, c.Thrust, c.Isp, n.DV, func(r, v orbital.Vec3) orbital.Vec3 {
		return spacecraft.DirectionUnit(mode, r, v)
	})
	return fin, n.TriggerTime.Add(time.Duration(half * float64(time.Second)))
}

// finiteBurnTWRs are the three thrust levels of the #666 sweep on the
// 16.1 t Comsat Carrier (TWR 0.42, 1.27, 8.48 at ignition).
var finiteBurnTWRs = []float64{67000, 200000, 1.34e6}

// TestRefineFiniteDepartureConvergesInFewSteps: 71 lunar aims (350 to 420
// Mm from 500 km) at three TWRs all converge to the 1 km tolerance in at
// most 6 Newton steps (measured 2 to 4; the old 1 %-of-dv finite
// difference took 8 to 10 and ran out at 8). The step count is the
// guard: a slope estimate that is wrong enough to converge only linearly
// is what made 71 of 71 aims fail.
func TestRefineFiniteDepartureConvergesInFewSteps(t *testing.T) {
	const mu = 3.986004418e14
	rPark := 6378137.0 + 500e3
	prograde := func(r, v orbital.Vec3) orbital.Vec3 {
		return spacecraft.DirectionUnit(spacecraft.BurnPrograde, r, v)
	}
	park := physics.StateVector{
		R: orbital.Vec3{X: rPark}, V: orbital.Vec3{Y: math.Sqrt(mu / rPark)}, M: 16114,
	}
	for _, thrust := range finiteBurnTWRs {
		worst, fails := 0, 0
		for aim := 350e6; aim <= 420e6; aim += 1e6 {
			dv := math.Abs(splitRaiseDv(mu, rPark, aim))
			sol, err := planner.SolveFiniteBurn(park, mu, thrust, 900, dv, prograde,
				planner.TargetApoapsis(aim), refineTolMeters, refineMaxIter)
			if err != nil || !sol.Converged {
				fails++
			}
			if sol.Iterations > worst {
				worst = sol.Iterations
			}
		}
		t.Logf("thrust %.0f N: %d of 71 aims failed, worst %d Newton steps", thrust, fails, worst)
		if fails != 0 || worst > 6 {
			t.Errorf("thrust %.0f N: %d of 71 aims failed to converge, worst case %d Newton steps (want 0 and <= 6)",
				thrust, fails, worst)
		}
	}
}

// TestRefineFiniteDepartureRefusesWhatItCannotReach: a burn that cannot
// reach the aim is refused, never planted at the impulsive delta-v as if
// it had been refined (the pre-#666 silent fallback).
func TestRefineFiniteDepartureRefusesWhatItCannotReach(t *testing.T) {
	const mu = 3.986004418e14
	rPark := 6378137.0 + 500e3
	for _, c := range []struct {
		name        string
		thrust, aim float64
	}{
		{"aim beyond escape", 67000, 1e12},
		{"1 N-per-tonne burn arc", 500, 391e6},
	} {
		var plan planner.TransferPlan
		plan.Departure.DV = math.Abs(splitRaiseDv(mu, rPark, c.aim))
		err := refineFiniteDeparture(&plan, mu, rPark, 16114, c.thrust, 900, c.aim)
		if err != errDepartureBurnShort {
			t.Errorf("%s: err = %v, want errDepartureBurnShort", c.name, err)
		}
	}
}

// TestPlanTransferRefusesWhenDepartureBurnCannotReachAim: the refusal
// reaches the planner's caller, with no node planted.
func TestPlanTransferRefusesWhenDepartureBurnCannotReachAim(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Comsat-Carrier-3", ParentBodyID: "earth",
		AltitudeM: 500e3, Inclination: 0}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	c := w.ActiveCraft()
	c.Thrust = 500
	n0 := len(c.Nodes)
	if _, err := w.PlanTransfer(moonIndex(w)); err != errDepartureBurnShort {
		t.Fatalf("PlanTransfer at 500 N: err = %v, want errDepartureBurnShort", err)
	}
	if len(c.Nodes) != n0 {
		t.Errorf("a refused plan planted %d nodes", len(c.Nodes)-n0)
	}
}

// TestComsatCarrierDepartureReachesMoonSOIAcrossEpochs: the flown [H]
// departure enters the Moon's SOI at every epoch and both parking
// planes, not at the one sample the issue measured.
func TestComsatCarrierDepartureReachesMoonSOIAcrossEpochs(t *testing.T) {
	for _, incl := range []float64{0, spacecraft.SeedInclinationDeg} {
		for _, days := range []int{0, 3, 7, 11, 15, 19, 23} {
			_, _, closest, rSOI := comsatMoonDepartureAt(t, incl, time.Duration(days)*24*time.Hour, 0)
			t.Logf("incl %.1f +%dd: closest %.0f km (SOI %.0f km)", incl, days, closest/1e3, rSOI/1e3)
			if closest > rSOI {
				t.Errorf("incl %.1f +%dd: the flown departure passes the Moon at %.0f km, outside its %.0f km SOI",
					incl, days, closest/1e3, rSOI/1e3)
			}
		}
	}
}

// TestComsatCarrierCombinedDepartureReachesMoonSOI: when [H] plants the
// COMBINED strategy (a fixed-direction BurnVector fused-Lambert
// departure) a low-TWR vessel still reaches the Moon. Before #666 the
// combined departure got no finite-burn correction at all and the
// Comsat Carrier x3 flew to Ap 219 to 232 Mm. The cases are the epochs
// found by sweeping 6 h steps over a lunar month where the combined
// total beats the split; combined wins only a few times in 300.
func TestComsatCarrierCombinedDepartureReachesMoonSOI(t *testing.T) {
	combined := 0
	for _, c := range []struct {
		incl  float64
		hours int
	}{{0, 90}, {0, 414}, {23.4, 12}, {23.4, 666}} {
		w, _, closest, rSOI := comsatMoonDepartureAt(t, c.incl, time.Duration(c.hours)*time.Hour, 0)
		if w.LastTransfer.Strategy != "combined" {
			continue
		}
		combined++
		t.Logf("incl %.1f +%dh: combined, closest %.0f km (SOI %.0f km)", c.incl, c.hours, closest/1e3, rSOI/1e3)
		if closest > rSOI {
			t.Errorf("incl %.1f +%dh: combined departure passes the Moon at %.0f km, outside its %.0f km SOI",
				c.incl, c.hours, closest/1e3, rSOI/1e3)
		}
	}
	if combined == 0 {
		t.Fatal("no case planted the combined strategy: the epochs need re-finding")
	}
}

// TestPlannedDepartureLegMatchesFlownBurn: the predicted leg after a
// planted finite departure is the orbit the burn really flies. A
// predictor that applies the (finite-corrected) delta-v as an impulse
// puts the Comsat Carrier x3 at Ap 597 Mm for a burn that flies to 392.
func TestPlannedDepartureLegMatchesFlownBurn(t *testing.T) {
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Comsat-Carrier-3", ParentBodyID: "earth",
		AltitudeM: 500e3, Inclination: spacecraft.SeedInclinationDeg}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	mi := moonIndex(w)
	w.SetTargetBody(mi)
	if _, err := w.PlanTransfer(mi); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs")
	}
	predicted := orbital.ElementsFromState(legs[0].State.R, legs[0].State.V, mu).Apoapsis()
	flown, _, _, _ := flyMoonDeparture(t, w, 0)
	got := orbital.ElementsFromState(flown.ActiveCraft().State.R, flown.ActiveCraft().State.V, mu).Apoapsis()
	t.Logf("predicted leg Ap %.1f Mm, flown Ap %.1f Mm", predicted/1e6, got/1e6)
	if math.Abs(predicted-got) > 0.01*got {
		t.Errorf("predicted post-burn Ap %.1f Mm, flown %.1f Mm (want within 1%%)", predicted/1e6, got/1e6)
	}
}

// TestPlanChainMemoCoversTheFiniteBurnsInputs: the chain walk flies finite
// nodes (nodeBurnState), so the engine it flies them with is part of the
// memo key. Swapping the active stage's thrust or Isp inside a frame must
// re-walk, and the walk must come out different, not replay the old one.
func TestPlanChainMemoCoversTheFiniteBurnsInputs(t *testing.T) {
	w, _ := plannedMoonTransfer(t, 0)
	c := w.ActiveCraft()
	frame := func() float64 {
		st, _, ok := w.PredictedFinalOrbit()
		if !ok {
			t.Fatal("no predicted final orbit")
		}
		return st.V.Norm()
	}
	v0 := frame()
	frame()
	base := w.PlanChainWalks()
	c.Thrust *= 0.1
	v1 := frame()
	if w.PlanChainWalks() != base+1 {
		t.Errorf("thrust change did not re-walk (walks %d, want %d): stale memo", w.PlanChainWalks(), base+1)
	}
	if v1 == v0 {
		t.Errorf("a tenth of the thrust predicts the same final speed %.3f: the walk is not flying the finite burn", v0)
	}
	base = w.PlanChainWalks()
	c.Isp *= 0.9
	frame()
	if w.PlanChainWalks() != base+1 {
		t.Errorf("Isp change did not re-walk: stale memo")
	}
}
