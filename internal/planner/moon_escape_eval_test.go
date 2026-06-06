package planner

// moon_escape_eval_test.go is the REGRESSION BAR for the ADR 0013 Moon
// Return rewrite (it began life as a diagnostic probe quantifying why the
// old "minimum escape" objective wasted ~590 m/s on the departure side).
//
// The game core is patched-conic, so this analytical patched-conic model
// matches what the integrator actually produces (modulo finite-burn /
// sample noise). It asserts the two headline ADR 0013 acceptance criteria
// for a low-Luna → Earth return:
//
//  1. the planted departure reaches the target Earth-frame perigee
//     (rEarthLEO), prograde around Earth; and
//  2. the departure Δv is within a few percent of the analytic ideal
//     single trans-Earth-injection (~822 m/s), NOT the old ~1414 m/s the
//     min-escape objective spent (escape 645 + a separate 768 perigee
//     drop the planner never planted).
//
// Run: go test ./internal/planner -run TestMoonReturnRegressionBar -v

import (
	"math"
	"testing"
)

// Earth constants (game catalog values, internal/bodies/systems/sol.json).
// muEarth is already a package-level const in transfer_test.go.
const (
	// aLuna is the Moon's orbital semimajor axis around Earth (384 399 km).
	aLuna = 3.84399e8
	// rEarthLEO is a 200-km-altitude parking orbit (R_earth = 6371 km).
	rEarthLEO = 6.371e6 + 200e3
)

// circEarth returns circular-orbit speed at radius r about Earth.
func circEarth(r float64) float64 { return math.Sqrt(muEarth / r) }

// idealReturnDepartureDv is the analytic single-TEI departure Δv: one burn
// at low lunar orbit sized so the inherited Earth-frame perigee is rEarthLEO
// (retrograde exit at the Moon's distance), with no separate perigee-drop.
func idealReturnDepartureDv(rPark float64) float64 {
	vLuna := circEarth(aLuna)
	atLEO := (aLuna + rEarthLEO) / 2
	vApoTarget := math.Sqrt(muEarth * (2/aLuna - 1/atLEO))
	vInf := vLuna - vApoTarget
	vPeriHyper := math.Sqrt(vInf*vInf + 2*muLuna/rPark)
	return vPeriHyper - math.Sqrt(muLuna/rPark)
}

func TestMoonReturnRegressionBar(t *testing.T) {
	rPark := rLowLunarOrbit
	craftR, craftV, moonR, moonV := coplanarReturnSetup(rPark)

	plan, err := PlanMoonEscape(muLuna, muEarth, craftR, craftV, moonR, moonV,
		rLunaSOI, rEarthLEO, 0, "moon", "earth")
	if err != nil {
		t.Fatalf("PlanMoonEscape: %v", err)
	}

	dvDep := plan.Departure.DV
	dvIdeal := idealReturnDepartureDv(rPark)
	peri, prograde := reconstructReturn(plan, rPark, moonR, moonV)

	t.Logf("── Moon Return: low-Luna(%.0f km alt) → Earth (target peri %.0f km) ──",
		(rPark-1.7374e6)/1e3, rEarthLEO/1e3)
	t.Logf("departure Δv : planner %.0f m/s  vs ideal TEI %.0f m/s", dvDep, dvIdeal)
	t.Logf("inherited Earth perigee: %.0f km (target %.0f km), prograde=%v",
		peri/1e3, rEarthLEO/1e3, prograde)

	// (1) Reaches the target perigee, prograde around Earth.
	if !prograde {
		t.Errorf("inherited Earth orbit is retrograde; want prograde (same sense as Earth's rotation)")
	}
	if rel := math.Abs(peri-rEarthLEO) / rEarthLEO; rel > 0.02 {
		t.Errorf("inherited perigee = %.0f km, want %.0f km (%.2f%% off, tol 2%%)",
			peri/1e3, rEarthLEO/1e3, rel*100)
	}

	// (2) Departure Δv within a few percent of the analytic ideal — and
	// nowhere near the old ~1414 m/s min-escape-plus-separate-drop budget.
	if rel := math.Abs(dvDep-dvIdeal) / dvIdeal; rel > 0.05 {
		t.Errorf("departure Δv = %.0f m/s, want ≈ %.0f m/s (%.1f%% off, tol 5%%)",
			dvDep, dvIdeal, rel*100)
	}
	if dvDep > 1000 {
		t.Errorf("departure Δv = %.0f m/s — regressed toward the old min-escape budget (~1414 m/s)", dvDep)
	}
}
