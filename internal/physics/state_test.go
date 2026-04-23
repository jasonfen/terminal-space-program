package physics

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestEarthSurfaceGravity: standard g at Earth's surface should be 9.80 m/s²
// within a fraction of a percent — Earth's GM and radius are from JPL, so
// anything more than ±0.02 is a data-entry bug.
func TestEarthSurfaceGravity(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	earth := systems[0].FindBody("Earth")
	if earth == nil {
		t.Fatal("Earth not found")
	}
	g := SurfaceGravity(*earth)
	if math.Abs(g-9.81) > 0.02 {
		t.Errorf("Earth surface g = %.4f, expected 9.81 ± 0.02", g)
	}
}

// TestCircularOrbitEnergy: for a circular orbit at radius r around
// gravitational parameter μ, ε = -μ/(2r). Analytic vis-viva result.
func TestCircularOrbitEnergy(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	mu := earth.GravitationalParameter()
	r := earth.RadiusMeters() + 200e3
	v := math.Sqrt(mu / r)

	s := StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: v},
		M: 1000,
	}

	got := SpecificEnergy(s, mu)
	want := -mu / (2 * r)
	if d := math.Abs(got-want) / math.Abs(want); d > 1e-12 {
		t.Errorf("circular energy: got %.6e want %.6e (rel err %.2e)", got, want, d)
	}
	a := SemimajorAxis(s, mu)
	if d := math.Abs(a-r) / r; d > 1e-12 {
		t.Errorf("circular a: got %.3e want %.3e", a, r)
	}
}

// TestAccelInverseSquare: magnitude should drop as 1/r².
func TestAccelInverseSquare(t *testing.T) {
	mu := 3.986e14
	a1 := Accel(orbital.Vec3{X: 1e7}, mu).Norm()
	a2 := Accel(orbital.Vec3{X: 2e7}, mu).Norm()
	ratio := a1 / a2
	if math.Abs(ratio-4.0) > 1e-9 {
		t.Errorf("|a1|/|a2| at r=1e7 vs 2e7: got %.6f, want 4.0", ratio)
	}
}
