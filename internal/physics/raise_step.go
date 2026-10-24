package physics

import "math"

// RaiseStep advances s by dt under constant tangential thrust aT (m/s²) that
// spirals a near-round orbit toward semi-major axis targetA (ADR 0059
// decision 4). The speed falls (raise) or rises (lower) at aT, v = sqrt(mu/a),
// and the angle swept while it does so is closed form,
//
//	θ = ∫ v³/μ dt = |v0⁴ − v1⁴| / (4 aT μ),
//
// so a 5,000 s tick is as exact as a 0.05 s one. E, I, Omega and Arg are held,
// so the vessel keeps its own slightly eccentric orbit (exact for e = 0).
// If the target is reached inside the tick the remainder is one KeplerStep.
// used is the thrust time spent (≤ dt, the argon debit); done reports the
// target reached. ok=false for an unbound orbit or aT ≤ 0.
func RaiseStep(s StateVector, mu, aT, dt, targetA float64) (next StateVector, used float64, done, ok bool) {
	if aT <= 0 || dt < 0 || targetA <= 0 {
		return s, 0, false, false
	}
	el, M0, ok := keplerElementsAndAnomaly(s, mu)
	if !ok {
		return s, 0, false, false
	}
	v0 := math.Sqrt(mu / el.A)
	vT := math.Sqrt(mu / targetA)
	need := math.Abs(v0-vT) / aT // thrust time that reaches the target
	used = dt
	v1 := v0 - aT*dt
	if targetA < el.A {
		v1 = v0 + aT*dt
	}
	if need <= dt {
		used, v1, done = need, vT, true
	}
	el.A = mu / (v1 * v1)
	theta := math.Abs(v0*v0*v0*v0-v1*v1*v1*v1) / (4 * aT * mu)
	next = keplerRebuild(el, M0+theta, mu, s.M)
	if done && dt > used {
		if k, kok := KeplerStep(next, mu, dt-used); kok {
			next = k
		}
	}
	return next, used, done, true
}
