package physics

import "github.com/jasonfen/terminal-space-program/internal/orbital"

// StepVerlet advances a StateVector by dt using the symplectic velocity-
// Verlet integrator. Single force evaluation per step (cheap under warp),
// conserves specific orbital energy over long horizons — see plan §Phase 1
// rationale. Returns the new state; input is untouched.
//
// The algorithm (for a = a(r) only, true for pure two-body gravity):
//   r' = r + v·dt + ½·a·dt²
//   a' = Accel(r')
//   v' = v + ½·(a + a')·dt
func StepVerlet(s StateVector, mu, dt float64) StateVector {
	return StepVerletWithAccel(s, mu, dt, nil)
}

// StepVerletWithAccel is StepVerlet with an extra non-conservative
// acceleration term a_extra(r, v) added to the gravitational accel at
// both half-kicks. Used by v0.8.4+ to fold atmospheric drag into the
// integrator without splitting it across separate Verlet + drag steps
// (which would lose the symplectic property's stability margin).
//
// Velocity-dependent forces aren't strictly symplectic in this naive
// fold-in (the canonical fix is operator splitting), but for drag
// magnitudes typical of LEO and below the energy drift over a single
// tick is negligible compared with the actual deorbit physics we want
// to capture.
//
// extraAccel = nil collapses to plain two-body Verlet — same numerical
// path as the original StepVerlet.
func StepVerletWithAccel(s StateVector, mu, dt float64, extraAccel func(r, v orbital.Vec3) orbital.Vec3) StateVector {
	a := Accel(s.R, mu)
	if extraAccel != nil {
		a = a.Add(extraAccel(s.R, s.V))
	}
	halfDt := 0.5 * dt

	// r' = r + v·dt + ½·a·dt²
	rNew := orbital.Vec3{
		X: s.R.X + s.V.X*dt + a.X*halfDt*dt,
		Y: s.R.Y + s.V.Y*dt + a.Y*halfDt*dt,
		Z: s.R.Z + s.V.Z*dt + a.Z*halfDt*dt,
	}
	// Estimate v at the half-step for the velocity-dependent term in
	// a'. v_half ≈ v + ½·a·dt (kick from the first half-step).
	vHalf := orbital.Vec3{
		X: s.V.X + a.X*halfDt,
		Y: s.V.Y + a.Y*halfDt,
		Z: s.V.Z + a.Z*halfDt,
	}
	aNew := Accel(rNew, mu)
	if extraAccel != nil {
		aNew = aNew.Add(extraAccel(rNew, vHalf))
	}
	vNew := orbital.Vec3{
		X: s.V.X + (a.X+aNew.X)*halfDt,
		Y: s.V.Y + (a.Y+aNew.Y)*halfDt,
		Z: s.V.Z + (a.Z+aNew.Z)*halfDt,
	}

	return StateVector{R: rNew, V: vNew, M: s.M}
}
