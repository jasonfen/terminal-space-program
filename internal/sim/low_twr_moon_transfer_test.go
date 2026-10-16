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
	w = mustWorld(t)
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID: "Comsat-Carrier-3", ParentBodyID: "earth",
		AltitudeM: 500e3, Inclination: spacecraft.SeedInclinationDeg,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	for i, cc := range w.Crafts {
		if cc == c {
			w.ActiveCraftIdx = i
		}
	}
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
	if len(c.Nodes) == 0 || c.Nodes[0].Mode != spacecraft.BurnPrograde {
		t.Fatalf("expected a prograde departure node first, got %+v", c.Nodes)
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

// TestMissedTransferHUDDoesNotClaimMoonEncounter: after a departure that
// misses the Moon's SOI, the HUD sources that drive NAVIGATION's plan:
// row ("Moon encounter"), the FRAME TRANSITION box ("Earth -> Moon") and
// CAPTURE PREVIEW must not name the Moon. Today PredictedFinalOrbit,
// NextFrameTransition and ArrivalCapturePreview take the capture node's
// PrimaryID as proof of arrival and rebase into the Moon's frame from
// wherever the craft is (130 Mm away in the screenshot).
func TestMissedTransferHUDDoesNotClaimMoonEncounter(t *testing.T) {
	// Fly the departure 40 m/s short so the miss does not depend on the
	// planner defect above: once that is fixed this test still sees a
	// missed transfer and still checks what the HUD claims about it.
	w, moon, closest, rSOI := comsatMoonDeparture(t, 40)
	if closest <= rSOI {
		t.Skipf("this departure reaches the Moon's SOI (closest %.0f km); the HUD claim is true here", closest/1e3)
	}
	if _, primary, ok := w.PredictedFinalOrbit(); ok && primary.ID == moon.ID {
		t.Errorf("plan: reads \"Moon encounter\" (PredictedFinalOrbit ends at the Moon) but the coast passes the Moon at %.0f km, outside its %.0f km SOI",
			closest/1e3, rSOI/1e3)
	}
	if ft, ok := w.NextFrameTransition(); ok && ft.To == moon.ID {
		t.Errorf("FRAME TRANSITION shows %s -> %s at node #%d, but the trajectory never enters the Moon's SOI (closest %.0f km)",
			ft.From, ft.To, ft.NodeIndex+1, closest/1e3)
	}
	if cp, ok := w.ArrivalCapturePreview(); ok && cp.Primary.ID == moon.ID {
		t.Errorf("CAPTURE PREVIEW shows a Moon capture (hyperbolic=%v), but the trajectory never enters the Moon's SOI (closest %.0f km)",
			cp.Hyperbolic, closest/1e3)
	}
}
