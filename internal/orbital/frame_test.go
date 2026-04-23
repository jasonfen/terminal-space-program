package orbital

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestInertialPlanetCentricRoundTrip(t *testing.T) {
	primary := Vec3{X: 1.5e11, Y: 2.3e10, Z: -4.7e9}
	cases := []Vec3{
		{0, 0, 0},
		{1e9, 1e9, 1e9},
		{-7.4e10, 3.1e9, 5.2e11},
		{1e-3, -1e-3, 0},
	}
	// Absolute tolerance scales with primary magnitude (~AU). Float64 has
	// ~15.7 decimal digits of precision — at 1.5e11 m that's ~3e-5 m per
	// operation, so allow a few ULPs of the primary norm.
	tol := primary.Norm() * 1e-14
	for _, rIn := range cases {
		rLocal := ToPlanetCentric(rIn, primary)
		rBack := FromPlanetCentric(rLocal, primary)
		d := rBack.Sub(rIn).Norm()
		if d > tol {
			t.Errorf("round-trip error %.3e m (tol %.3e) for r=%+v", d, tol, rIn)
		}
	}
}

// TestEarthPositionAtJ2000: Earth sits at a reasonable heliocentric radius
// (~1 AU) given its M₀ at J2000. Sanity check that the whole M → E → ν → r
// chain doesn't produce nonsense; not a NASA-grade ephemeris match.
func TestEarthPositionAtJ2000(t *testing.T) {
	systems, _ := bodies.LoadAll()
	sol := systems[0]
	earth := sol.FindBody("Earth")
	calc := ForSystem(sol, bodies.J2000)
	M := calc.CalculateMeanAnomaly(*earth, bodies.J2000)
	E := SolveKepler(M, earth.Eccentricity)
	nu := TrueAnomaly(E, earth.Eccentricity)
	el := ElementsFromBody(*earth)
	r := PositionAtTrueAnomaly(el, nu)
	dist := r.Norm()
	// Earth is between perihelion (~0.983 AU) and aphelion (~1.017 AU).
	if dist < 0.98*bodies.AU || dist > 1.02*bodies.AU {
		t.Errorf("Earth distance %.3e m at J2000 outside [0.98, 1.02] AU", dist)
	}
}

// TestCircularOrbitVelocity: at a=1 AU, e=0, v should equal √(μ/a).
func TestCircularOrbitVelocity(t *testing.T) {
	el := Elements{A: bodies.AU, E: 0, I: 0, Omega: 0, Arg: 0}
	mu := bodies.G * bodies.SunMassKg
	v := VelocityAtTrueAnomaly(el, 0, mu).Norm()
	want := math.Sqrt(mu / bodies.AU) // ~29.78 km/s
	if d := math.Abs(v-want) / want; d > 1e-6 {
		t.Errorf("circular-orbit v=%.3f m/s, want %.3f m/s (rel err %.2e)", v, want, d)
	}
}
