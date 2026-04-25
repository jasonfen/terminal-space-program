package physics

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// KeplerStep advances a state vector by dt using analytic Kepler
// propagation. Snapshots orbital elements, advances mean anomaly by
// n·dt, and reconstructs (r, v) from the new true anomaly. Exact
// (modulo Newton iteration tolerance in SolveKepler) — no numerical
// drift, regardless of dt size relative to orbital period.
//
// Returns ok=false for hyperbolic / parabolic orbits (e ≥ 1) and
// degenerate inputs; the caller falls back to numerical integration
// (Verlet). Used for the v0.4.3 "warp lock": when warp > 1× and no
// active burn, integrateSpacecraft takes one KeplerStep per tick
// instead of looping Verlet sub-steps that drift eccentricity over
// many orbits.
//
// SOI handling is the caller's responsibility: KeplerStep does not
// detect or rebase across primary boundaries. integrateSpacecraft
// guards against SOI crossings by checking apo < primary's SOI before
// taking the analytic path.
func KeplerStep(s StateVector, mu, dt float64) (StateVector, bool) {
	if mu <= 0 {
		return s, false
	}
	el := orbital.ElementsFromState(s.R, s.V, mu)
	if el.E >= 1 || el.A <= 0 {
		return s, false
	}

	rMag := s.R.Norm()
	if rMag <= 0 {
		return s, false
	}

	// Mean anomaly at t=0 derived from current state.
	var M0 float64
	if el.E > 1e-9 {
		// Elliptic, non-circular. Standard formulas:
		//   r = a(1 − e cos E)  →  cos E = (1 − r/a) / e
		//   r·v = √(µa) · e · sin E
		cosE := (1 - rMag/el.A) / el.E
		if cosE > 1 {
			cosE = 1
		} else if cosE < -1 {
			cosE = -1
		}
		rDotV := s.R.X*s.V.X + s.R.Y*s.V.Y + s.R.Z*s.V.Z
		sinE := rDotV / (el.E * math.Sqrt(mu*el.A))
		E0 := math.Atan2(sinE, cosE)
		M0 = E0 - el.E*math.Sin(E0)
	} else {
		// Circular: ν ≡ M ≡ angle of r in the perifocal frame. Inverse-
		// rotate r into perifocal (p, q) and take atan2(q, p).
		cO, sO := math.Cos(el.Omega), math.Sin(el.Omega)
		cw, sw := math.Cos(el.Arg), math.Sin(el.Arg)
		ci, si := math.Cos(el.I), math.Sin(el.I)
		px := cO*cw - sO*sw*ci
		py := sO*cw + cO*sw*ci
		pz := sw * si
		qx := -cO*sw - sO*cw*ci
		qy := -sO*sw + cO*cw*ci
		qz := cw * si
		p := s.R.X*px + s.R.Y*py + s.R.Z*pz
		q := s.R.X*qx + s.R.Y*qy + s.R.Z*qz
		M0 = math.Atan2(q, p)
	}

	n := math.Sqrt(mu / (el.A * el.A * el.A))
	mNew := M0 + n*dt
	eNew := orbital.SolveKepler(mNew, el.E)
	nuNew := orbital.TrueAnomaly(eNew, el.E)
	rNew := orbital.PositionAtTrueAnomaly(el, nuNew)
	vNew := orbital.VelocityAtTrueAnomaly(el, nuNew, mu)
	return StateVector{R: rNew, V: vNew, M: s.M}, true
}
