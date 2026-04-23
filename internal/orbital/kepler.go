package orbital

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// SolveKepler returns the eccentric anomaly E that satisfies M = E − e·sin(E).
// Newton-Raphson iteration; converges to |f(E)| < tol in ≤ 8 steps for e < 0.95.
// Falls back after maxIter even if tolerance not met (returns best estimate).
func SolveKepler(M, e float64) float64 {
	const (
		tol     = 1e-12
		maxIter = 32
	)
	// Normalize M to [-π, π] for faster convergence.
	M = math.Mod(M, 2*math.Pi)
	if M > math.Pi {
		M -= 2 * math.Pi
	} else if M < -math.Pi {
		M += 2 * math.Pi
	}
	// Initial guess: E₀ = M + e·sin(M) (good for e < ~0.8).
	E := M + e*math.Sin(M)
	for i := 0; i < maxIter; i++ {
		f := E - e*math.Sin(E) - M
		fp := 1 - e*math.Cos(E)
		if fp == 0 {
			break
		}
		d := f / fp
		E -= d
		if math.Abs(d) < tol {
			return E
		}
	}
	return E
}

// TrueAnomaly returns ν (true anomaly, radians) from eccentric anomaly E
// and eccentricity e. Uses the half-angle formulation — numerically stable
// across the full range including near e→1.
func TrueAnomaly(E, e float64) float64 {
	return 2 * math.Atan2(
		math.Sqrt(1+e)*math.Sin(E/2),
		math.Sqrt(1-e)*math.Cos(E/2),
	)
}

// Elements groups the orbital elements (in SI / radians) needed to place a
// body in inertial space at a given true anomaly.
type Elements struct {
	A     float64 // semimajor axis (m)
	E     float64 // eccentricity
	I     float64 // inclination (rad)
	Omega float64 // longitude of ascending node Ω (rad)
	Arg   float64 // argument of periapsis ω (rad)
}

// ElementsFromBody pulls Keplerian elements from a bodies.CelestialBody,
// converting stored km→m and deg→rad. Precise OrbitalElements overrides
// the top-level fields when present.
func ElementsFromBody(b bodies.CelestialBody) Elements {
	deg2rad := math.Pi / 180.0
	el := Elements{
		A:     b.SemimajorAxisMeters(),
		E:     b.Eccentricity,
		I:     b.Inclination * deg2rad,
		Omega: b.LongitudeOfAscendingNode * deg2rad,
		Arg:   b.ArgumentOfPeriapsis * deg2rad,
	}
	if b.OrbitalElements != nil {
		oe := b.OrbitalElements
		el.A = oe.SemimajorAxis * 1000.0
		el.E = oe.Eccentricity
		el.I = oe.Inclination * deg2rad
		el.Omega = oe.LongitudeOfAscendingNode * deg2rad
		el.Arg = oe.ArgumentOfPeriapsis * deg2rad
	}
	return el
}
