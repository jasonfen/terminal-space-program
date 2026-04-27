package planner

import (
	"math"
	"testing"
	"time"
)

// muLuna is Luna's gravitational parameter (m³/s²).
const muLuna = 4.9028e12

// rLunaSOI approximates Luna's sphere-of-influence radius around Earth
// (a · (m_luna / m_earth)^0.4 ≈ 6.617e7 m). Tests use a constant so the
// math doesn't drift with body-data changes — what we care about is
// the planner's algebra against a fixed apolune target.
const rLunaSOI = 6.617e7

// rLowLunarOrbit is a 100-km-altitude circular orbit around Luna
// (Luna mean radius ≈ 1737.4 km). Standard test parking orbit.
const rLowLunarOrbit = 1.7374e6 + 100e3

// TestPlanMoonEscapeApoluneMatchesSOI: the bound-ellipse departure Δv
// should produce a transfer ellipse whose apolune equals the SOI
// radius to floating-point precision. This is the load-bearing
// invariant — if the algebra is wrong, every downstream HUD prediction
// is wrong.
func TestPlanMoonEscapeApoluneMatchesSOI(t *testing.T) {
	plan, err := PlanMoonEscape(muLuna, rLowLunarOrbit, rLunaSOI, 0, "moon", "earth")
	if err != nil {
		t.Fatalf("PlanMoonEscape: %v", err)
	}

	// Reconstruct post-burn velocity at periapsis: v_circ + Δv.
	vCirc := math.Sqrt(muLuna / rLowLunarOrbit)
	vPeri := vCirc + plan.Departure.DV

	// vis-viva: v² = µ · (2/r − 1/a) → a = 1 / (2/r − v²/µ)
	a := 1 / (2/rLowLunarOrbit - vPeri*vPeri/muLuna)
	apoFromBurn := 2*a - rLowLunarOrbit

	if math.Abs(apoFromBurn-rLunaSOI) > 1.0 {
		t.Errorf("apolune from burn = %.0f m, want %.0f m (SOI radius)",
			apoFromBurn, rLunaSOI)
	}
}

// TestPlanMoonEscapeTransferTimeMatchesHalfPeriod: the planner's
// TransferDt must equal half the transfer ellipse's period — that's
// the time between periapsis and the SOI-boundary apolune.
func TestPlanMoonEscapeTransferTimeMatchesHalfPeriod(t *testing.T) {
	plan, err := PlanMoonEscape(muLuna, rLowLunarOrbit, rLunaSOI, 0, "moon", "earth")
	if err != nil {
		t.Fatalf("PlanMoonEscape: %v", err)
	}
	aT := (rLowLunarOrbit + rLunaSOI) / 2
	wantHalf := math.Pi * math.Sqrt(aT*aT*aT/muLuna)
	gotSecs := plan.TransferDt.Seconds()
	if math.Abs(gotSecs-wantHalf) > 1.0 {
		t.Errorf("TransferDt = %.1f s, want %.1f s", gotSecs, wantHalf)
	}
}

// TestPlanMoonEscapePrimaryIDsRouteFrames: departure node carries the
// moon's ID (so the HUD renders the burn in lunar frame), arrival
// marker carries the parent's ID (so the HUD shows it crossing into
// Earth frame at SOI exit).
func TestPlanMoonEscapePrimaryIDsRouteFrames(t *testing.T) {
	plan, err := PlanMoonEscape(muLuna, rLowLunarOrbit, rLunaSOI, 0, "moon", "earth")
	if err != nil {
		t.Fatalf("PlanMoonEscape: %v", err)
	}
	if plan.Departure.PrimaryID != "moon" {
		t.Errorf("departure PrimaryID = %q, want %q", plan.Departure.PrimaryID, "moon")
	}
	if plan.Arrival.PrimaryID != "earth" {
		t.Errorf("arrival PrimaryID = %q, want %q", plan.Arrival.PrimaryID, "earth")
	}
	if plan.Arrival.DV != 0 {
		t.Errorf("arrival is a frame-marker — Δv should be 0, got %.3f", plan.Arrival.DV)
	}
	if plan.Departure.IsRetrograde {
		t.Errorf("escape burn should be prograde, got retrograde")
	}
}

// TestPlanMoonEscapeMinLeadPadsBoth: when minLeadSeconds is non-zero,
// the departure offset advances by that amount and the arrival offset
// rides along (keeping arrival = departure + half-period).
func TestPlanMoonEscapeMinLeadPadsBoth(t *testing.T) {
	const lead = 90.0
	plan, err := PlanMoonEscape(muLuna, rLowLunarOrbit, rLunaSOI, lead, "moon", "earth")
	if err != nil {
		t.Fatalf("PlanMoonEscape: %v", err)
	}
	wantDep := time.Duration(lead * float64(time.Second))
	if plan.Departure.OffsetTime != wantDep {
		t.Errorf("departure OffsetTime = %v, want %v", plan.Departure.OffsetTime, wantDep)
	}
	gap := plan.Arrival.OffsetTime - plan.Departure.OffsetTime
	if absDuration(gap-plan.TransferDt) > time.Second {
		t.Errorf("arrival − departure = %v, want TransferDt = %v", gap, plan.TransferDt)
	}
}

// TestPlanMoonEscapeRejectsBadInputs: degenerate inputs (non-positive
// mu / radii, or SOI inside parking orbit) must surface as errors so
// the dispatch path falls back instead of planting nonsense.
func TestPlanMoonEscapeRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name                string
		muMoon, rPark, rSOI float64
	}{
		{"zero mu", 0, rLowLunarOrbit, rLunaSOI},
		{"negative rPark", muLuna, -1, rLunaSOI},
		{"zero rSOI", muLuna, rLowLunarOrbit, 0},
		{"SOI ≤ rPark", muLuna, rLunaSOI, rLowLunarOrbit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := PlanMoonEscape(c.muMoon, c.rPark, c.rSOI, 0, "moon", "earth"); err == nil {
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
