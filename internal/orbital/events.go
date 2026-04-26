package orbital

import "math"

// Event-time helpers for v0.6.0's burn-at-next scheduler. Compute the
// time-of-flight from a state vector to the next time the orbit hits a
// reference true anomaly (peri / apo) or crosses the equatorial plane
// (ascending / descending node).
//
// All helpers return the elapsed seconds from "now" to the target
// crossing along the orbit's natural prograde direction. Closed
// (elliptical) orbits always have a future crossing; for open
// (hyperbolic) orbits and degenerate inputs the helpers return -1 to
// signal "unreachable from current state". Callers (the lazy-freeze
// resolver in sim) treat -1 as "leave the node unresolved this tick
// and try again next tick" — for instance a craft on an escape
// trajectory through Luna's SOI will have no future periapsis until it
// recaptures.

// TimeToTrueAnomaly returns the elapsed seconds from the given state
// to the next time the orbit reaches targetNu (radians). Uses Kepler's
// equation: convert ν → E → M, take Δ M to the target, divide by mean
// motion. The result is always in (0, T] for elliptical orbits where T
// is the orbital period; if currentNu == targetNu the return is T (one
// full revolution to come back), which matches "next" semantics.
//
// Returns -1 if the orbit is hyperbolic / parabolic (e ≥ 1) or
// degenerate (a ≤ 0).
func TimeToTrueAnomaly(currentNu, targetNu, a, e, mu float64) float64 {
	if a <= 0 || e >= 1 || mu <= 0 {
		return -1
	}
	period := 2 * math.Pi * math.Sqrt(a*a*a/mu)
	currentM := meanFromTrue(currentNu, e)
	targetM := meanFromTrue(targetNu, e)
	dM := targetM - currentM
	// Wrap into (0, 2π] so "next" always lies strictly in the future.
	for dM <= 0 {
		dM += 2 * math.Pi
	}
	for dM > 2*math.Pi {
		dM -= 2 * math.Pi
	}
	n := 2 * math.Pi / period
	return dM / n
}

// TimeToPeriapsis returns elapsed seconds to the next periapsis (ν=0).
// Convenience wrapper around TimeToTrueAnomaly.
func TimeToPeriapsis(state Vec3State, mu float64) float64 {
	el := ElementsFromState(state.R, state.V, mu)
	nu := TrueAnomalyFromState(state.R, state.V, mu, el)
	return TimeToTrueAnomaly(nu, 0, el.A, el.E, mu)
}

// TimeToApoapsis returns elapsed seconds to the next apoapsis (ν=π).
// Convenience wrapper around TimeToTrueAnomaly.
func TimeToApoapsis(state Vec3State, mu float64) float64 {
	el := ElementsFromState(state.R, state.V, mu)
	nu := TrueAnomalyFromState(state.R, state.V, mu, el)
	return TimeToTrueAnomaly(nu, math.Pi, el.A, el.E, mu)
}

// TimeToNodeCrossing returns elapsed seconds to the next ascending or
// descending node crossing (where the orbit pierces the equatorial
// plane of the central body's reference frame).
//
// Geometry: at ν = -ω the craft is on the line of nodes at the
// ascending node; at ν = π - ω it's at the descending node. Both
// resolve to the next future ν via TimeToTrueAnomaly.
//
// For equatorial orbits (i ≈ 0 or i ≈ π) every point is technically
// "on the equatorial plane" — there is no well-defined crossing. The
// helper returns -1 in that case; callers should fall back to
// TimeToPeriapsis or treat the node as unresolvable.
func TimeToNodeCrossing(state Vec3State, mu float64, ascending bool) float64 {
	const equatorialTol = 1e-3 // radians (~0.06°)
	el := ElementsFromState(state.R, state.V, mu)
	if el.I < equatorialTol || math.Abs(el.I-math.Pi) < equatorialTol {
		return -1
	}
	nu := TrueAnomalyFromState(state.R, state.V, mu, el)
	target := -el.Arg
	if !ascending {
		target = math.Pi - el.Arg
	}
	return TimeToTrueAnomaly(nu, target, el.A, el.E, mu)
}

// Vec3State is a thin (R, V) pair used by event helpers without
// pulling in the physics StateVector type (which would create a
// package cycle: physics → orbital → physics).
type Vec3State struct {
	R Vec3
	V Vec3
}

// TrueAnomalyFromState extracts the current true anomaly ν from a
// state vector and its derived elements. Standard formulation: ν is
// the angle (in the orbital plane) from the periapsis direction to
// the current radius vector, with the orbit's natural prograde
// direction picked via r·v.
//
// For circular or near-circular orbits (e ≈ 0) ν is undefined;
// callers receive 0 as a safe fallback (peri/apo events are
// degenerate for circles anyway).
func TrueAnomalyFromState(r, v Vec3, mu float64, el Elements) float64 {
	if el.E < 1e-9 {
		return 0
	}
	// Recompute eccentricity vector inline (cheap; avoids returning it
	// from ElementsFromState and breaking that signature).
	rMag := r.Norm()
	if rMag == 0 || mu == 0 {
		return 0
	}
	vMag := v.Norm()
	rDotV := r.X*v.X + r.Y*v.Y + r.Z*v.Z
	coef1 := vMag*vMag - mu/rMag
	eVec := Vec3{
		X: (coef1*r.X - rDotV*v.X) / mu,
		Y: (coef1*r.Y - rDotV*v.Y) / mu,
		Z: (coef1*r.Z - rDotV*v.Z) / mu,
	}
	eMag := eVec.Norm()
	if eMag == 0 {
		return 0
	}
	// cos ν = (e · r) / (|e|·|r|)
	dot := (eVec.X*r.X + eVec.Y*r.Y + eVec.Z*r.Z) / (eMag * rMag)
	if dot > 1 {
		dot = 1
	} else if dot < -1 {
		dot = -1
	}
	nu := math.Acos(dot)
	// Pick the half-plane via r·v: outbound (positive) → ν in (0, π);
	// inbound (negative) → ν in (-π, 0) which we wrap to (π, 2π).
	if rDotV < 0 {
		nu = 2*math.Pi - nu
	}
	return nu
}

// meanFromTrue converts true anomaly ν to mean anomaly M for a closed
// orbit (e < 1). Helper intentionally unexported — TimeToTrueAnomaly
// is the public entry point.
func meanFromTrue(nu, e float64) float64 {
	// E from ν: tan(E/2) = √((1-e)/(1+e)) · tan(ν/2). Using Atan2 keeps
	// the result in (-π, π) without quadrant ambiguity.
	cosNu := math.Cos(nu)
	sinNu := math.Sin(nu)
	// sin E = √(1-e²) · sin ν / (1 + e·cos ν)
	// cos E = (e + cos ν) / (1 + e·cos ν)
	denom := 1 + e*cosNu
	if denom == 0 {
		return 0
	}
	sinE := math.Sqrt(1-e*e) * sinNu / denom
	cosE := (e + cosNu) / denom
	E := math.Atan2(sinE, cosE)
	return E - e*math.Sin(E)
}
