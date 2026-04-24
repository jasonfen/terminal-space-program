package planner

import (
	"math"
	"testing"
)

// Reference values for textbook Earth → Mars Hohmann transfer
// (Curtis Example 8.3, slightly reformulated for SI):
//
//   r_earth      = 1 AU                = 1.496e11 m
//   r_mars       = 1.524 AU            = 2.279e11 m
//   r_park (LEO) = 6378 + 200 km       = 6.578e6 m
//   r_capture    = Mars 200 km altitude = 3.39e6 + 200e3 = 3.59e6 m
//
//   Δv_departure ≈ 3.61 km/s (heliocentric v∞ ≈ 2.945 km/s, escape from LEO)
//   Δv_arrival   ≈ 2.09 km/s (heliocentric v∞ ≈ 2.65 km/s, capture at low Mars)
//   total        ≈ 5.7 km/s
//   t_transfer   ≈ 258.8 days
// muSun is shared with hohmann_test.go (var declared there).
const (
	muEarth  = 3.986004418e14
	muMars   = 4.282837e13
	rEarth   = 1.495978707e11
	rMars    = 1.524 * 1.495978707e11
	rPark    = 6.578e6
	rMarsCap = 3.39e6 + 200e3
)

// TestPlanHohmannTransferEarthToMars: top-level Δv and transfer time
// match Curtis Example 8.3 (textbook Earth→Mars Hohmann) within 5%.
// The scope-excluded "phasing not enforced" caveat means absolute
// timing accuracy isn't tested — only the canonical Δv budget.
func TestPlanHohmannTransferEarthToMars(t *testing.T) {
	plan, err := PlanHohmannTransfer(
		muSun, rEarth, rMars,
		muEarth, rPark, "earth",
		muMars, rMarsCap, "mars",
	)
	if err != nil {
		t.Fatalf("PlanHohmannTransfer: %v", err)
	}

	// Δv_departure ~ 3.61 km/s.
	wantDvDep := 3610.0
	if d := math.Abs(plan.Departure.DV-wantDvDep) / wantDvDep; d > 0.05 {
		t.Errorf("departure Δv: got %.0f, want ≈%.0f m/s (rel %.2e)",
			plan.Departure.DV, wantDvDep, d)
	}

	// Δv_arrival ~ 2.09 km/s — that's into a LOW Mars orbit. The
	// 5% tolerance covers small variations in textbook reference
	// (some sources use ~2.6 km/s for higher capture orbits).
	wantDvArr := 2090.0
	if d := math.Abs(plan.Arrival.DV-wantDvArr) / wantDvArr; d > 0.05 {
		t.Errorf("arrival Δv: got %.0f, want ≈%.0f m/s (rel %.2e)",
			plan.Arrival.DV, wantDvArr, d)
	}

	// Frame tagging: departure in Earth frame, arrival in Mars frame.
	if plan.Departure.PrimaryID != "earth" {
		t.Errorf("departure PrimaryID: got %q, want earth", plan.Departure.PrimaryID)
	}
	if plan.Arrival.PrimaryID != "mars" {
		t.Errorf("arrival PrimaryID: got %q, want mars", plan.Arrival.PrimaryID)
	}

	// Transfer time ≈ 258.8 days ≈ 22.36e6 s.
	wantTransfer := 22.36e6
	gotTransfer := plan.TransferDt.Seconds()
	if d := math.Abs(gotTransfer-wantTransfer) / wantTransfer; d > 0.02 {
		t.Errorf("transfer time: got %.3e s, want ≈%.3e s (rel %.2e)",
			gotTransfer, wantTransfer, d)
	}

	// Outbound transfer: departure prograde, arrival retrograde
	// (slow down at apoapsis to drop into Mars orbit).
	if plan.Departure.IsRetrograde {
		t.Errorf("outbound departure should be prograde, got retrograde")
	}
	if !plan.Arrival.IsRetrograde {
		t.Errorf("outbound arrival should be retrograde, got prograde")
	}
}

// TestPlanHohmannTransferInbound: a Mars→Earth transfer flips the
// retrograde flags (slow down at departure to drop perihelion;
// speed up at Earth to circularize at the new lower radius).
func TestPlanHohmannTransferInbound(t *testing.T) {
	plan, err := PlanHohmannTransfer(
		muSun, rMars, rEarth,
		muMars, rMarsCap, "mars",
		muEarth, rPark, "earth",
	)
	if err != nil {
		t.Fatalf("PlanHohmannTransfer: %v", err)
	}
	if !plan.Departure.IsRetrograde {
		t.Errorf("inbound departure should be retrograde, got prograde")
	}
	if plan.Arrival.IsRetrograde {
		t.Errorf("inbound arrival should be prograde, got retrograde")
	}
}

// TestPlanHohmannTransferRejectsDegenerate: equal radii, zero/negative
// inputs all surface as errors rather than panicking or NaN-ing
// silently.
func TestPlanHohmannTransferRejectsDegenerate(t *testing.T) {
	cases := []struct {
		name             string
		muSun, rDep, rArr float64
	}{
		{"equal radii", muSun, rEarth, rEarth},
		{"zero rDep", muSun, 0, rMars},
		{"negative rArr", muSun, rEarth, -1},
		{"zero muSun", 0, rEarth, rMars},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := PlanHohmannTransfer(
				c.muSun, c.rDep, c.rArr,
				muEarth, rPark, "earth",
				muMars, rMarsCap, "mars",
			)
			if err == nil {
				t.Errorf("expected error for %s", c.name)
			}
		})
	}
}
