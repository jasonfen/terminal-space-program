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
	a := Accel(s.R, mu)
	halfDt := 0.5 * dt

	// r' = r + v·dt + ½·a·dt²
	rNew := orbital.Vec3{
		X: s.R.X + s.V.X*dt + a.X*halfDt*dt,
		Y: s.R.Y + s.V.Y*dt + a.Y*halfDt*dt,
		Z: s.R.Z + s.V.Z*dt + a.Z*halfDt*dt,
	}
	aNew := Accel(rNew, mu)
	vNew := orbital.Vec3{
		X: s.V.X + (a.X+aNew.X)*halfDt,
		Y: s.V.Y + (a.Y+aNew.Y)*halfDt,
		Z: s.V.Z + (a.Z+aNew.Z)*halfDt,
	}

	return StateVector{R: rNew, V: vNew, M: s.M}
}
