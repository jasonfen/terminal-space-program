package planner

import (
	"errors"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// LambertSolve is the single-revolution (N=0) entry point to the
// Lambert solver. Kept for backward-compat with v0.3.0–v0.3.2 callers;
// new code should prefer LambertSolveRev with an explicit N.
func LambertSolve(r1, r2 orbital.Vec3, dt, mu float64) (v1, v2 orbital.Vec3, err error) {
	return LambertSolveRev(r1, r2, dt, mu, 0)
}

// LambertSolveRev solves Lambert's problem for an N-revolution transfer:
// given two position vectors, a time of flight, and a revolution count,
// find the velocity vectors that connect them on a Keplerian orbit
// completing exactly N full revs before reaching r2.
//
// Algorithm: Curtis "Orbital Mechanics for Engineering Students"
// Algorithm 5.2 — universal-variables formulation, Newton-Raphson on
// z. For N-rev transfers the lower bound on z shifts to (2πN)²
// (each rev contributes (2π)² to the universal-variable domain);
// the bracket sweep starts just past that lower bound.
//
// Single branch only — at N ≥ 1 there are typically two time-of-flight
// solutions per N (a "long" and "short" transfer separated by the
// minimum-energy critical z). This solver returns whichever branch the
// bracket sweep lands in first, which is adequate for the porkchop
// grid's coarse sampling. Multi-branch selection is a v0.4 polish item
// if it comes up.
func LambertSolveRev(r1, r2 orbital.Vec3, dt, mu float64, nRev int) (v1, v2 orbital.Vec3, err error) {
	if dt <= 0 {
		return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: dt must be > 0")
	}
	if mu <= 0 {
		return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: mu must be > 0")
	}
	if nRev < 0 {
		return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: nRev must be ≥ 0")
	}

	r1m := r1.Norm()
	r2m := r2.Norm()
	if r1m == 0 || r2m == 0 {
		return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: position vectors must be non-zero")
	}

	// Transfer angle. The sign of (r1 × r2)·z disambiguates [0, π] from
	// (π, 2π) for prograde motion in the equatorial frame.
	cosDtheta := (r1.X*r2.X + r1.Y*r2.Y + r1.Z*r2.Z) / (r1m * r2m)
	if cosDtheta > 1 {
		cosDtheta = 1
	} else if cosDtheta < -1 {
		cosDtheta = -1
	}
	crossZ := r1.X*r2.Y - r1.Y*r2.X
	dtheta := math.Acos(cosDtheta)
	if crossZ < 0 {
		dtheta = 2*math.Pi - dtheta
	}
	sinDtheta := math.Sin(dtheta)
	if math.Abs(sinDtheta) < 1e-12 {
		return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: degenerate transfer angle (0 or π)")
	}

	// Curtis (5.35).
	A := sinDtheta * math.Sqrt(r1m*r2m/(1-cosDtheta))

	yFn := func(z float64) float64 {
		return r1m + r2m + A*(z*stumpffS(z)-1)/math.Sqrt(stumpffC(z))
	}
	F := func(z float64) float64 {
		y := yFn(z)
		if y < 0 {
			return math.NaN()
		}
		return math.Pow(y/stumpffC(z), 1.5)*stumpffS(z) + A*math.Sqrt(y) - math.Sqrt(mu)*dt
	}
	Fprime := func(z float64) float64 {
		y := yFn(z)
		if math.Abs(z) < 1e-10 {
			return math.Sqrt(2)/40*math.Pow(y, 1.5) + A/8*(math.Sqrt(y)+A*math.Sqrt(1/(2*y)))
		}
		c := stumpffC(z)
		s := stumpffS(z)
		return math.Pow(y/c, 1.5)*(1/(2*z)*(c-3*s/(2*c))+3*s*s/(4*c)) +
			A/8*(3*s/c*math.Sqrt(y)+A*math.Sqrt(c/y))
	}

	// Bracket: walk z upward until F flips to positive. For N-rev
	// transfers the domain is z > (2πN)² — the N-rev branch lives
	// entirely in the elliptic region bounded below by the critical
	// value. Single-rev (N=0) starts from z=0 and may walk into the
	// hyperbolic region (z < 0) if F(0) is already positive.
	zLower := 4 * math.Pi * math.Pi * float64(nRev*nRev)
	zUpper := 4 * math.Pi * math.Pi * float64((nRev+1)*(nRev+1))
	if nRev == 0 {
		zUpper = 4 * math.Pi * math.Pi
	}
	z := zLower
	if nRev > 0 {
		z += 0.01 // nudge past the lower critical value
	}
	if nRev == 0 && !math.IsNaN(F(z)) && F(z) > 0 {
		// N=0 only: F(0) already positive → walk negative into the
		// hyperbolic region to find the bracket.
		for F(z) > 0 && z > -4*math.Pi*math.Pi {
			z -= 0.1
		}
	} else {
		for {
			fv := F(z)
			if !math.IsNaN(fv) && fv >= 0 {
				break
			}
			z += 0.1
			if z >= zUpper {
				return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: bracket search failed — no solution in this rev band")
			}
		}
	}

	// Newton-Raphson. Convergence: stop when the step in z is below
	// tolStep (machine-precision-relative is more reliable than testing
	// |F|, which sits at ~ε·sqrt(mu)·dt scale and bounces on noise).
	const tolStep = 1e-12
	const maxIter = 100
	converged := false
	for i := 0; i < maxIter; i++ {
		fv := F(z)
		if math.IsNaN(fv) {
			return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: y went negative during iteration")
		}
		fp := Fprime(z)
		if fp == 0 {
			return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: Newton derivative vanished")
		}
		step := fv / fp
		zNext := z - step
		for yFn(zNext) < 0 {
			zNext = (z + zNext) / 2
			if math.Abs(zNext-z) < 1e-15 {
				return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: failed to recover from y<0 step")
			}
		}
		if math.Abs(zNext-z) < tolStep {
			z = zNext
			converged = true
			break
		}
		z = zNext
	}
	if !converged {
		return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: failed to converge in maxIter")
	}

	// Lagrange coefficients — Curtis (5.46).
	y := yFn(z)
	f := 1 - y/r1m
	g := A * math.Sqrt(y/mu)
	gdot := 1 - y/r2m
	if g == 0 {
		return orbital.Vec3{}, orbital.Vec3{}, errors.New("lambert: degenerate Lagrange g")
	}

	v1 = orbital.Vec3{
		X: (r2.X - f*r1.X) / g,
		Y: (r2.Y - f*r1.Y) / g,
		Z: (r2.Z - f*r1.Z) / g,
	}
	v2 = orbital.Vec3{
		X: (gdot*r2.X - r1.X) / g,
		Y: (gdot*r2.Y - r1.Y) / g,
		Z: (gdot*r2.Z - r1.Z) / g,
	}
	return v1, v2, nil
}

// stumpffC is the Stumpff C(z) function: C(z) = (1 − cos√z) / z for
// elliptic z>0, (cosh√−z − 1) / −z for hyperbolic z<0, 1/2 at z=0.
func stumpffC(z float64) float64 {
	switch {
	case z > 0:
		sz := math.Sqrt(z)
		return (1 - math.Cos(sz)) / z
	case z < 0:
		sz := math.Sqrt(-z)
		return (math.Cosh(sz) - 1) / (-z)
	default:
		return 0.5
	}
}

// stumpffS is the Stumpff S(z) function: S(z) = (√z − sin√z) / √z³ for
// z>0, (sinh√−z − √−z) / √−z³ for z<0, 1/6 at z=0.
func stumpffS(z float64) float64 {
	switch {
	case z > 0:
		sz := math.Sqrt(z)
		return (sz - math.Sin(sz)) / (sz * sz * sz)
	case z < 0:
		sz := math.Sqrt(-z)
		return (math.Sinh(sz) - sz) / (sz * sz * sz)
	default:
		return 1.0 / 6.0
	}
}
