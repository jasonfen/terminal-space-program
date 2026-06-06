package planner

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// muLuna is Luna's gravitational parameter (m³/s²).
const muLuna = 4.9028e12

// rLunaSOI approximates Luna's sphere-of-influence radius around Earth
// (a · (m_luna / m_earth)^0.4 ≈ 6.617e7 m). Tests use a constant so the
// math doesn't drift with body-data changes.
const rLunaSOI = 6.617e7

// rLowLunarOrbit is a 100-km-altitude circular orbit around Luna
// (Luna mean radius ≈ 1737.4 km). Standard test parking orbit.
const rLowLunarOrbit = 1.7374e6 + 100e3

// coplanarReturnSetup builds a coplanar (XY-plane) Moon Return scenario:
// the moon on a circular Earth orbit at aLuna, and the craft on a circular
// prograde parking orbit of radius rPark around the moon. Returns the
// arguments PlanMoonEscape needs. The craft phase is arbitrary — the
// planner phases the burn to the periapsis direction itself, so the
// departure Δv is phase-independent in the coplanar case.
func coplanarReturnSetup(rPark float64) (craftR, craftV, moonR, moonV orbital.Vec3) {
	vLuna := math.Sqrt(muEarth / aLuna)
	moonR = orbital.Vec3{X: aLuna, Y: 0, Z: 0}
	moonV = orbital.Vec3{X: 0, Y: vLuna, Z: 0}
	vCirc := math.Sqrt(muLuna / rPark)
	craftR = orbital.Vec3{X: rPark, Y: 0, Z: 0}
	craftV = orbital.Vec3{X: 0, Y: vCirc, Z: 0}
	return craftR, craftV, moonR, moonV
}

// reconstructReturn replays the planner's departure BurnVector through the
// patched-conic model (coplanar case) and returns the inherited Earth-frame
// perigee and whether the inherited orbit is prograde around Earth.
func reconstructReturn(plan TransferPlan, rPark float64, moonR, moonV orbital.Vec3) (perigee float64, prograde bool) {
	nMoon := moonR.Cross(moonV).Unit()
	// Post-burn periapsis state in the moon's frame: a prograde burn at the
	// periapsis raises the circular speed to vEsc along BurnDir; the
	// periapsis position sits 90° behind the velocity.
	vEsc := math.Sqrt(muLuna/rPark) + plan.Departure.DV
	v := plan.Departure.BurnDir.Scale(vEsc)
	r := orbital.Rotate(plan.Departure.BurnDir, nMoon, -math.Pi/2).Scale(rPark)

	vInf := math.Sqrt(math.Max(0, v.Dot(v)-2*muLuna/rPark))
	eVec := r.Scale(v.Dot(v)/muLuna - 1/r.Norm()).Sub(v.Scale(r.Dot(v) / muLuna))
	ecc := eVec.Norm()
	nuInf := math.Acos(-1 / ecc)
	asym := orbital.Rotate(eVec.Unit(), nMoon, nuInf) // outgoing asymptote direction
	vEarth := moonV.Add(asym.Scale(vInf))
	return parentPerigee(moonR, vEarth, muEarth), moonR.Cross(vEarth).Dot(nMoon) > 0
}

// TestPlanMoonEscapeReachesTargetPerigee: the planted return must inherit
// an Earth-frame orbit whose perigee lands on the target, prograde around
// Earth. This is the load-bearing invariant of the ADR 0013 rewrite.
func TestPlanMoonEscapeReachesTargetPerigee(t *testing.T) {
	rPark := rLowLunarOrbit
	craftR, craftV, moonR, moonV := coplanarReturnSetup(rPark)
	plan, err := PlanMoonEscape(muLuna, muEarth, craftR, craftV, moonR, moonV,
		rLunaSOI, rEarthLEO, 0, "moon", "earth")
	if err != nil {
		t.Fatalf("PlanMoonEscape: %v", err)
	}
	peri, prograde := reconstructReturn(plan, rPark, moonR, moonV)
	if !prograde {
		t.Errorf("inherited Earth orbit is retrograde; want prograde")
	}
	// 1% tolerance: the analytic solve targets perigee directly, but the
	// reconstruction reintroduces the same rounding the solver bisected on.
	if rel := math.Abs(peri-rEarthLEO) / rEarthLEO; rel > 0.01 {
		t.Errorf("inherited perigee = %.0f km, want %.0f km (%.2f%% off)",
			peri/1e3, rEarthLEO/1e3, rel*100)
	}
}

// TestPlanMoonEscapeDepartureIsBurnVector: the departure carries a full 3D
// BurnDir (so the sim plants a BurnVector), and the arrival stays a zero-Δv
// frame marker ordered after it.
func TestPlanMoonEscapeDepartureIsBurnVector(t *testing.T) {
	craftR, craftV, moonR, moonV := coplanarReturnSetup(rLowLunarOrbit)
	plan, err := PlanMoonEscape(muLuna, muEarth, craftR, craftV, moonR, moonV,
		rLunaSOI, rEarthLEO, 0, "moon", "earth")
	if err != nil {
		t.Fatalf("PlanMoonEscape: %v", err)
	}
	if n := plan.Departure.BurnDir.Norm(); math.Abs(n-1) > 1e-9 {
		t.Errorf("departure BurnDir norm = %.6f, want unit vector", n)
	}
	if plan.Departure.PrimaryID != "moon" {
		t.Errorf("departure PrimaryID = %q, want %q", plan.Departure.PrimaryID, "moon")
	}
	if plan.Departure.DV <= 0 {
		t.Errorf("departure Δv = %.1f, want > 0", plan.Departure.DV)
	}
	if plan.Arrival.PrimaryID != "earth" {
		t.Errorf("arrival PrimaryID = %q, want %q", plan.Arrival.PrimaryID, "earth")
	}
	if plan.Arrival.DV != 0 {
		t.Errorf("arrival is a frame marker — Δv should be 0, got %.3f", plan.Arrival.DV)
	}
	if plan.Arrival.OffsetTime <= plan.Departure.OffsetTime {
		t.Errorf("arrival offset %v must follow departure offset %v",
			plan.Arrival.OffsetTime, plan.Departure.OffsetTime)
	}
	if absDuration(plan.Arrival.OffsetTime-plan.Departure.OffsetTime-plan.TransferDt) > time.Second {
		t.Errorf("arrival − departure (%v) should equal TransferDt (%v)",
			plan.Arrival.OffsetTime-plan.Departure.OffsetTime, plan.TransferDt)
	}
}

// TestPlanMoonEscapeMinLeadPads: a non-zero minLead pushes the departure
// offset to at least that lead (padded by whole parking-orbit periods).
func TestPlanMoonEscapeMinLeadPads(t *testing.T) {
	const lead = 3000.0
	craftR, craftV, moonR, moonV := coplanarReturnSetup(rLowLunarOrbit)
	plan, err := PlanMoonEscape(muLuna, muEarth, craftR, craftV, moonR, moonV,
		rLunaSOI, rEarthLEO, lead, "moon", "earth")
	if err != nil {
		t.Fatalf("PlanMoonEscape: %v", err)
	}
	if plan.Departure.OffsetTime.Seconds() < lead {
		t.Errorf("departure offset %.0f s < minLead %.0f s", plan.Departure.OffsetTime.Seconds(), lead)
	}
}

// TestPlanMoonEscapeRejectsBadInputs: degenerate inputs surface as errors
// so the dispatch path falls back instead of planting nonsense.
func TestPlanMoonEscapeRejectsBadInputs(t *testing.T) {
	craftR, craftV, moonR, moonV := coplanarReturnSetup(rLowLunarOrbit)
	cases := []struct {
		name                         string
		muMoon, muParent, rSOI, peri float64
	}{
		{"zero moon mu", 0, muEarth, rLunaSOI, rEarthLEO},
		{"zero parent mu", muLuna, 0, rLunaSOI, rEarthLEO},
		{"SOI ≤ rPark", muLuna, muEarth, rLowLunarOrbit, rEarthLEO},
		{"perigee above moon distance", muLuna, muEarth, rLunaSOI, aLuna * 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := PlanMoonEscape(c.muMoon, c.muParent, craftR, craftV, moonR, moonV,
				c.rSOI, c.peri, 0, "moon", "earth")
			if err == nil {
				t.Errorf("expected error for %s, got nil", c.name)
			}
		})
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
