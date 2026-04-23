// Package physics implements the spacecraft propagation layer: state
// vectors, integrators (Verlet + RK4), and patched-conic SOI handling.
package physics

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// StateVector is the spacecraft's kinematic state relative to the current
// primary body (see patched-conic model in plan §Phase 1). All SI:
// position in m, velocity in m/s, mass in kg.
type StateVector struct {
	R orbital.Vec3 // position relative to primary
	V orbital.Vec3 // velocity relative to primary
	M float64     // current total mass (wet)
}

// Accel returns the two-body gravitational acceleration on the spacecraft
// due to the primary with gravitational parameter mu. Direction is toward
// the primary (origin in the relative frame).
func Accel(r orbital.Vec3, mu float64) orbital.Vec3 {
	rMag := r.Norm()
	if rMag == 0 {
		return orbital.Vec3{}
	}
	f := -mu / (rMag * rMag * rMag)
	return orbital.Vec3{X: r.X * f, Y: r.Y * f, Z: r.Z * f}
}

// SurfaceGravity returns the magnitude of gravitational acceleration
// at the body's mean radius (m/s²). Diagnostic helper for tests.
func SurfaceGravity(b bodies.CelestialBody) float64 {
	r := b.RadiusMeters()
	if r == 0 {
		return 0
	}
	return b.GravitationalParameter() / (r * r)
}

// SpecificEnergy returns the two-body orbital specific energy
// ε = v²/2 − μ/r (J/kg). Used as an invariant by energy-conservation tests.
func SpecificEnergy(s StateVector, mu float64) float64 {
	rMag := s.R.Norm()
	vMag := s.V.Norm()
	if rMag == 0 {
		return math.Inf(-1)
	}
	return 0.5*vMag*vMag - mu/rMag
}

// SemimajorAxis returns the orbit's semimajor axis a = -μ/(2ε) for bound
// orbits; NaN for parabolic (ε≈0) and negative for hyperbolic trajectories.
func SemimajorAxis(s StateVector, mu float64) float64 {
	eps := SpecificEnergy(s, mu)
	if eps == 0 {
		return math.NaN()
	}
	return -mu / (2 * eps)
}
