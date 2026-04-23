package physics

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestVerletCircularLEO1000Orbits is the Phase 1 exit criterion from
// docs/plan.md: uncontrolled spacecraft in stable circular LEO, after
// 1000 simulated orbits, semi-major-axis drift must be under 1%. We
// also assert specific-energy drift under 1% (same invariant).
func TestVerletCircularLEO1000Orbits(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	mu := earth.GravitationalParameter()
	r0 := earth.RadiusMeters() + 200e3 // 200 km circular parking orbit
	v0 := math.Sqrt(mu / r0)

	s := StateVector{
		R: orbital.Vec3{X: r0},
		V: orbital.Vec3{Y: v0},
		M: 1000,
	}
	eps0 := SpecificEnergy(s, mu)
	a0 := SemimajorAxis(s, mu)

	period := 2 * math.Pi * math.Sqrt(r0*r0*r0/mu) // ~88.5 min at LEO
	stepsPerOrbit := 256
	dt := period / float64(stepsPerOrbit)
	totalSteps := 1000 * stepsPerOrbit

	for i := 0; i < totalSteps; i++ {
		s = StepVerlet(s, mu, dt)
	}

	eps1 := SpecificEnergy(s, mu)
	a1 := SemimajorAxis(s, mu)

	energyDrift := math.Abs(eps1-eps0) / math.Abs(eps0)
	smaDrift := math.Abs(a1-a0) / a0

	if energyDrift > 0.01 {
		t.Errorf("energy drift after 1000 orbits: %.4f%% (>1%%)", energyDrift*100)
	}
	if smaDrift > 0.01 {
		t.Errorf("SMA drift after 1000 orbits: %.4f%% (>1%%)", smaDrift*100)
	}
	t.Logf("Verlet: 1000 LEO orbits, Δε=%.3e%% Δa=%.3e%%", energyDrift*100, smaDrift*100)
}

// TestVerletReversibility: stepping forward then stepping with -dt should
// not return to the exact starting state (Verlet is reversible for the
// (r, v) trajectory only with symmetric half-kicks). Instead, we validate
// that a short forward integration over a known circular orbit keeps |r|
// within numerical noise of the starting radius.
func TestVerletCircularPreservesRadius(t *testing.T) {
	mu := 3.986e14
	r0 := 7e6
	v0 := math.Sqrt(mu / r0)
	s := StateVector{R: orbital.Vec3{X: r0}, V: orbital.Vec3{Y: v0}, M: 1}

	period := 2 * math.Pi * math.Sqrt(r0*r0*r0/mu)
	steps := 1000
	dt := period / float64(steps)

	maxRadDiff := 0.0
	for i := 0; i < steps; i++ {
		s = StepVerlet(s, mu, dt)
		d := math.Abs(s.R.Norm() - r0)
		if d > maxRadDiff {
			maxRadDiff = d
		}
	}
	// Max radial excursion over one orbit at 1000 steps should be <0.1% of r0.
	if maxRadDiff/r0 > 1e-3 {
		t.Errorf("radial excursion over one orbit: %.3e m (%.4f%% of r0)",
			maxRadDiff, maxRadDiff/r0*100)
	}
}
