package physics

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestRK4VerletParity: over a short trajectory (one orbit) both integrators
// should produce the same end state within a generous tolerance. Diverges
// past ~10 orbits because RK4 isn't symplectic; one is enough for parity.
func TestRK4VerletParity(t *testing.T) {
	mu := 3.986e14
	r0 := 7e6
	v0 := math.Sqrt(mu / r0)
	start := StateVector{R: orbital.Vec3{X: r0}, V: orbital.Vec3{Y: v0}, M: 1}

	period := 2 * math.Pi * math.Sqrt(r0*r0*r0/mu)
	steps := 2048
	dt := period / float64(steps)

	sV, sR := start, start
	accel := GravityOnly(mu)
	for i := 0; i < steps; i++ {
		sV = StepVerlet(sV, mu, dt)
		sR = StepRK4(sR, dt, accel, float64(i)*dt)
	}

	// After one full orbit both should return close to the starting point.
	dVerlet := sV.R.Sub(start.R).Norm()
	dRK4 := sR.R.Sub(start.R).Norm()
	// Position mismatch between the two integrators after one orbit.
	dBetween := sV.R.Sub(sR.R).Norm()

	// All three distances should be much smaller than r0.
	if dVerlet/r0 > 1e-4 {
		t.Errorf("Verlet closure error %.3e m (%.4f%% of r0)", dVerlet, dVerlet/r0*100)
	}
	if dRK4/r0 > 1e-4 {
		t.Errorf("RK4 closure error %.3e m (%.4f%% of r0)", dRK4, dRK4/r0*100)
	}
	if dBetween/r0 > 1e-4 {
		t.Errorf("Verlet-vs-RK4 mismatch after 1 orbit: %.3e m (%.4f%% of r0)",
			dBetween, dBetween/r0*100)
	}
}
