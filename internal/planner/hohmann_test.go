package planner

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// muSun is the standard gravitational parameter for the Sun, derived from
// G * M_sun. Canonical Hohmann numerics in textbooks use slightly
// different μ values (1.32712440018e20 is the JPL DE440 figure); the
// internal value here is consistent with internal/bodies so tolerances
// are relative.
var muSun = bodies.G * bodies.SunMassKg

// TestHohmannEarthToMars checks the canonical textbook case: circular
// Earth orbit to circular Mars orbit around the Sun. Expected values from
// Curtis, "Orbital Mechanics for Engineering Students" §6.2 Example 6.1:
//   Δv1 ≈ 2.943 km/s, Δv2 ≈ 2.649 km/s, t ≈ 258.8 days.
// We allow 2% tolerance to absorb the μ_sun difference between our
// G·M_sun and the JPL-published value.
func TestHohmannEarthToMars(t *testing.T) {
	r1 := bodies.AU
	r2 := 1.524 * bodies.AU
	dv1, dv2, tTransfer, err := HohmannTransfer(r1, r2, muSun)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedDV1 := 2943.0 // m/s
	expectedDV2 := 2649.0 // m/s
	expectedT := 258.8 * bodies.SecondsPerDay

	if !within(dv1, expectedDV1, 0.02) {
		t.Errorf("dv1: got %.1f m/s, want ~%.1f m/s (±2%%)", dv1, expectedDV1)
	}
	if !within(dv2, expectedDV2, 0.02) {
		t.Errorf("dv2: got %.1f m/s, want ~%.1f m/s (±2%%)", dv2, expectedDV2)
	}
	if !within(tTransfer, expectedT, 0.02) {
		t.Errorf("tTransfer: got %.1f d, want ~%.1f d (±2%%)",
			tTransfer/bodies.SecondsPerDay, expectedT/bodies.SecondsPerDay)
	}
}

// TestHohmannInboundSymmetry checks that reversing r1/r2 yields the same
// |Δv1|, |Δv2|, and transfer time (with the burns swapping roles).
func TestHohmannInboundSymmetry(t *testing.T) {
	r1 := bodies.AU
	r2 := 1.524 * bodies.AU
	outDV1, outDV2, outT, err := HohmannTransfer(r1, r2, muSun)
	if err != nil {
		t.Fatalf("outbound: %v", err)
	}
	inDV1, inDV2, inT, err := HohmannTransfer(r2, r1, muSun)
	if err != nil {
		t.Fatalf("inbound: %v", err)
	}
	if !within(outDV1, inDV2, 1e-9) {
		t.Errorf("outbound dv1 (%.3f) != inbound dv2 (%.3f)", outDV1, inDV2)
	}
	if !within(outDV2, inDV1, 1e-9) {
		t.Errorf("outbound dv2 (%.3f) != inbound dv1 (%.3f)", outDV2, inDV1)
	}
	if !within(outT, inT, 1e-9) {
		t.Errorf("outbound t (%.3f) != inbound t (%.3f)", outT, inT)
	}
}

// TestHohmannSameOrbitZeroDeltaV checks that r1 == r2 produces dv == 0
// and a transfer time equal to π·√(r³/μ) (half the circular period).
func TestHohmannSameOrbitZeroDeltaV(t *testing.T) {
	r := bodies.AU
	dv1, dv2, tTransfer, err := HohmannTransfer(r, r, muSun)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dv1 > 1e-9 || dv2 > 1e-9 {
		t.Errorf("expected zero Δv, got dv1=%g dv2=%g", dv1, dv2)
	}
	halfPeriod := math.Pi * math.Sqrt(r*r*r/muSun)
	if !within(tTransfer, halfPeriod, 1e-9) {
		t.Errorf("tTransfer: got %g, want %g", tTransfer, halfPeriod)
	}
}

// TestHohmannInvalidOrbit covers the guard clauses.
func TestHohmannInvalidOrbit(t *testing.T) {
	cases := []struct{ r1, r2, mu float64 }{
		{0, bodies.AU, muSun},
		{bodies.AU, 0, muSun},
		{bodies.AU, bodies.AU, 0},
		{-1, bodies.AU, muSun},
	}
	for _, c := range cases {
		if _, _, _, err := HohmannTransfer(c.r1, c.r2, c.mu); !errors.Is(err, ErrInvalidOrbit) {
			t.Errorf("HohmannTransfer(%g,%g,%g): expected ErrInvalidOrbit, got %v",
				c.r1, c.r2, c.mu, err)
		}
	}
}

func within(got, want, tol float64) bool {
	if want == 0 {
		return math.Abs(got) <= tol
	}
	return math.Abs(got-want)/math.Abs(want) <= tol
}
