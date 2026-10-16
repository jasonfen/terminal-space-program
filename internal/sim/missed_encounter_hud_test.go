package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #667: a transfer that misses the Moon must not leave the HUD claiming an
// encounter. The departure is flown 40 m/s short on purpose so these stay
// valid HUD tests after the planner fix (#666).

// missedMoonDeparture spawns the Comsat Carrier x3 in a 500 km orbit at the
// spawn form's default inclination, targets the Moon, plants [H], and
// flies the departure burn through the real sim path (Auto-Warp to the
// node, the planted finite burn). It returns the world after the burn and
// the closest the coasting trajectory comes to the Moon in the next 12
// days (analytic Kepler coast, sampled every 60 s, Moon from the
// ephemeris), plus the Moon's SOI radius. dvShortMps trims the planted
// departure before it fires (0 flies the plan as planted).
func missedMoonDeparture(t *testing.T, dvShortMps float64) (w *World, moon bodies.CelestialBody, closest, rSOI float64) {
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
	w, moon, closest, rSOI := missedMoonDeparture(t, 40)
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

// plannedMoonTransfer plants the default vessel's [H] Moon transfer;
// shortMps trims the departure node so the predicted path misses.
func plannedMoonTransfer(t testing.TB, shortMps float64) (*World, bodies.CelestialBody) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for i, b := range w.System().Bodies {
		if b.ID != "moon" {
			continue
		}
		w.SetTargetBody(i)
		if _, err := w.PlanTransfer(i); err != nil {
			t.Fatalf("PlanTransfer: %v", err)
		}
		w.ActiveCraft().Nodes[0].DV -= shortMps
		return w, b
	}
	t.Fatal("no Moon in Sol")
	return nil, bodies.CelestialBody{}
}

// claims reports which of the three encounter sources name the body.
func claims(w *World, id string) (plan, frame, capture bool) {
	if _, p, ok := w.PredictedFinalOrbit(); ok && p.ID == id {
		plan = true
	}
	if ft, ok := w.NextFrameTransition(); ok && ft.To == id {
		frame = true
	}
	if cp, ok := w.ArrivalCapturePreview(); ok && cp.Primary.ID == id {
		capture = true
	}
	return
}

// TestRealMoonEncounterStillClaimed is the positive guard for #667: the
// gate must not pass by deleting the readouts. A planted [H] transfer
// whose path reaches the Moon's SOI keeps all three.
func TestRealMoonEncounterStillClaimed(t *testing.T) {
	w, moon := plannedMoonTransfer(t, 0)
	plan, frame, capture := claims(w, moon.ID)
	if !plan || !frame || !capture {
		t.Errorf("a real Moon transfer lost an encounter readout: plan=%v frame=%v capture=%v", plan, frame, capture)
	}
}

// TestEncounterReadoutsShareOnePredicate: every source answers the same
// question, so they cannot disagree (the 183,642 km miss showed all
// three wrong together; a future gate on one alone would split them).
// Sweeping the departure trim crosses from hit to miss, and at every
// step the three must agree with each other.
func TestEncounterReadoutsShareOnePredicate(t *testing.T) {
	sawHit, sawMiss := false, false
	for short := 0.0; short <= 120; short += 10 {
		w, moon := plannedMoonTransfer(t, short)
		plan, frame, capture := claims(w, moon.ID)
		if plan != frame || frame != capture {
			t.Errorf("trim -%.0f m/s: sources disagree: plan=%v frame=%v capture=%v", short, plan, frame, capture)
		}
		if plan {
			sawHit = true
		} else {
			sawMiss = true
		}
	}
	if !sawHit || !sawMiss {
		t.Errorf("sweep never crossed hit/miss (hit=%v miss=%v); the agreement check proved nothing", sawHit, sawMiss)
	}
}
