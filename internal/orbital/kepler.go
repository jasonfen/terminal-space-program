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

// ElementsFromState derives Keplerian orbital elements (a, e, i, Ω, ω) from
// a state vector (r, v) and the central body's gravitational parameter μ.
// Standard formulation from Vallado §2.5 / Curtis §4.3. Returns e=0 for
// circular orbits (Ω and ω then undefined — left at zero rather than NaN).
func ElementsFromState(r, v Vec3, mu float64) Elements {
	rMag := r.Norm()
	vMag := v.Norm()
	if rMag == 0 || vMag == 0 || mu == 0 {
		return Elements{}
	}
	// Specific angular momentum h = r × v.
	h := Vec3{
		X: r.Y*v.Z - r.Z*v.Y,
		Y: r.Z*v.X - r.X*v.Z,
		Z: r.X*v.Y - r.Y*v.X,
	}
	hMag := h.Norm()

	// Eccentricity vector e = ((v² − μ/r)·r − (r·v)·v) / μ.
	rDotV := r.X*v.X + r.Y*v.Y + r.Z*v.Z
	coef1 := vMag*vMag - mu/rMag
	eVec := Vec3{
		X: (coef1*r.X - rDotV*v.X) / mu,
		Y: (coef1*r.Y - rDotV*v.Y) / mu,
		Z: (coef1*r.Z - rDotV*v.Z) / mu,
	}
	eMag := eVec.Norm()

	// Semimajor axis a = -μ/(2ε).
	eps := 0.5*vMag*vMag - mu/rMag
	var a float64
	if eps != 0 {
		a = -mu / (2 * eps)
	}

	// Inclination i = acos(h_z / |h|).
	var inc float64
	if hMag > 0 {
		cosI := h.Z / hMag
		if cosI > 1 {
			cosI = 1
		} else if cosI < -1 {
			cosI = -1
		}
		inc = math.Acos(cosI)
	}

	// Node vector n = ẑ × h (for Ω). Degenerate for equatorial orbits.
	n := Vec3{X: -h.Y, Y: h.X, Z: 0}
	nMag := n.Norm()

	// Longitude of ascending node Ω.
	var omega float64
	if nMag > 0 {
		cosO := n.X / nMag
		if cosO > 1 {
			cosO = 1
		} else if cosO < -1 {
			cosO = -1
		}
		omega = math.Acos(cosO)
		if n.Y < 0 {
			omega = 2*math.Pi - omega
		}
	}

	// Argument of periapsis ω.
	var argp float64
	if nMag > 0 && eMag > 0 {
		dot := (n.X*eVec.X + n.Y*eVec.Y + n.Z*eVec.Z) / (nMag * eMag)
		if dot > 1 {
			dot = 1
		} else if dot < -1 {
			dot = -1
		}
		argp = math.Acos(dot)
		if eVec.Z < 0 {
			argp = 2*math.Pi - argp
		}
	}

	return Elements{
		A:     a,
		E:     eMag,
		I:     inc,
		Omega: omega,
		Arg:   argp,
	}
}

// Periapsis returns a(1-e); zero for degenerate (a=0) orbits.
func (el Elements) Periapsis() float64 { return el.A * (1 - el.E) }

// Apoapsis returns a(1+e); zero for degenerate orbits. For e≥1 the
// return value is negative (hyperbolic) — callers should check.
func (el Elements) Apoapsis() float64 { return el.A * (1 + el.E) }

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
