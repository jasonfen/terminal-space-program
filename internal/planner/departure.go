package planner

import (
	"errors"
	"math"
)

// EscapeBurnDeltaV returns the prograde Δv that, applied at periapsis
// of a circular parking orbit of radius rPark around a primary with
// gravitational parameter muPlanet, yields a hyperbolic escape
// trajectory whose excess speed at infinity is vInfinity.
//
// Patched-conic identity (vis-viva at hyperbolic periapsis):
//
//	v_peri² = v∞² + 2·µ/r_peri
//	Δv      = v_peri − v_circ
//
// The result is in m/s (matching the SI used everywhere else in this
// repo). vInfinity is taken as a magnitude — direction is the caller's
// concern (typically aligned with the outbound asymptote, which the
// transfer-plan layer handles via Lambert).
func EscapeBurnDeltaV(vInfinity, muPlanet, rPark float64) (float64, error) {
	if muPlanet <= 0 {
		return 0, errors.New("departure: muPlanet must be > 0")
	}
	if rPark <= 0 {
		return 0, errors.New("departure: rPark must be > 0")
	}
	if vInfinity < 0 {
		return 0, errors.New("departure: vInfinity must be ≥ 0")
	}
	vCirc := math.Sqrt(muPlanet / rPark)
	vPeri := math.Sqrt(vInfinity*vInfinity + 2*muPlanet/rPark)
	return vPeri - vCirc, nil
}

// CaptureBurnDeltaV mirrors EscapeBurnDeltaV for arrival: Δv to drop
// from a hyperbolic approach (excess speed vInfinity) into a circular
// orbit of radius rCapture around the destination primary. By
// symmetry the magnitude equals EscapeBurnDeltaV; provided as a named
// helper so the transfer-plan layer reads naturally.
func CaptureBurnDeltaV(vInfinity, muPlanet, rCapture float64) (float64, error) {
	return EscapeBurnDeltaV(vInfinity, muPlanet, rCapture)
}
