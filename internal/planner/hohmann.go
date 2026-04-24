package planner

import (
	"errors"
	"math"
)

// ErrNotImplemented is returned by planner entry points that are still
// stubbed (e.g. Lambert in v0.2).
var ErrNotImplemented = errors.New("planner: not implemented")

// ErrInvalidOrbit is returned when HohmannTransfer is asked to solve for a
// non-physical input (non-positive radius or mu).
var ErrInvalidOrbit = errors.New("planner: invalid orbit (r1, r2, mu must be > 0)")

// HohmannTransfer computes the two impulsive burns and transfer time for a
// circular-to-circular coplanar Hohmann transfer between orbital radii r1
// and r2 around a primary with standard gravitational parameter mu. All
// SI units: r1, r2 in meters, mu in m^3/s^2.
//
// Returned dv1 and dv2 are magnitudes (always ≥ 0). Direction is implicit
// in r1 vs r2: outbound (r2 > r1) → both burns prograde; inbound → both
// retrograde. tTransfer is the half-period of the transfer ellipse (time
// between burn 1 and burn 2).
func HohmannTransfer(r1, r2, mu float64) (dv1, dv2, tTransfer float64, err error) {
	if r1 <= 0 || r2 <= 0 || mu <= 0 {
		return 0, 0, 0, ErrInvalidOrbit
	}
	aT := (r1 + r2) / 2
	vCirc1 := math.Sqrt(mu / r1)
	vCirc2 := math.Sqrt(mu / r2)
	vTrans1 := math.Sqrt(mu * (2/r1 - 1/aT))
	vTrans2 := math.Sqrt(mu * (2/r2 - 1/aT))
	dv1 = math.Abs(vTrans1 - vCirc1)
	dv2 = math.Abs(vCirc2 - vTrans2)
	tTransfer = math.Pi * math.Sqrt(aT*aT*aT/mu)
	return dv1, dv2, tTransfer, nil
}
