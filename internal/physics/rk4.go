package physics

import "github.com/jasonfen/terminal-space-program/internal/orbital"

// StepRK4 advances a StateVector by dt using classical 4th-order Runge-Kutta.
// Not symplectic — energy drifts over long horizons — but handles non-
// conservative forces (thrust during a finite burn, drag) cleanly, which
// Verlet does not. Parked for Phase 2 burns per plan §Phase 1 rationale;
// until then, Verlet is the default integrator.
//
// accelFn(r, v, t) lets callers inject an applied thrust on top of two-body
// gravity. For pure gravity, pass a closure that computes Accel(r, μ).
func StepRK4(
	s StateVector,
	dt float64,
	accelFn func(r, v orbital.Vec3, t float64) orbital.Vec3,
	t float64,
) StateVector {
	r0, v0 := s.R, s.V

	k1r := v0
	k1v := accelFn(r0, v0, t)

	r2 := r0.Add(k1r.Scale(dt / 2))
	v2 := v0.Add(k1v.Scale(dt / 2))
	k2r := v2
	k2v := accelFn(r2, v2, t+dt/2)

	r3 := r0.Add(k2r.Scale(dt / 2))
	v3 := v0.Add(k2v.Scale(dt / 2))
	k3r := v3
	k3v := accelFn(r3, v3, t+dt/2)

	r4 := r0.Add(k3r.Scale(dt))
	v4 := v0.Add(k3v.Scale(dt))
	k4r := v4
	k4v := accelFn(r4, v4, t+dt)

	dr := k1r.Add(k2r.Scale(2)).Add(k3r.Scale(2)).Add(k4r).Scale(dt / 6)
	dv := k1v.Add(k2v.Scale(2)).Add(k3v.Scale(2)).Add(k4v).Scale(dt / 6)

	return StateVector{
		R: r0.Add(dr),
		V: v0.Add(dv),
		M: s.M,
	}
}

// GravityOnly wraps Accel as an RK4-compatible accelFn closure.
func GravityOnly(mu float64) func(r, v orbital.Vec3, t float64) orbital.Vec3 {
	return func(r, _ orbital.Vec3, _ float64) orbital.Vec3 {
		return Accel(r, mu)
	}
}
