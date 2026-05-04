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

// TestIdentityFrameRoundTrip: vectors round-trip through the identity
// frame unchanged.
func TestIdentityFrameRoundTrip(t *testing.T) {
	f := IdentityFrame()
	v := Vec3{X: 1, Y: 2, Z: 3}
	if got := f.FromWorld(v); got != v {
		t.Errorf("FromWorld(identity, %+v) = %+v, want %+v", v, got, v)
	}
	if got := f.ToWorld(v); got != v {
		t.Errorf("ToWorld(identity, %+v) = %+v, want %+v", v, got, v)
	}
}

// TestBodyFrameRoundTrip: ToWorld and FromWorld are inverses.
func TestBodyFrameRoundTrip(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", AxialTilt: 23.44}
	f := BodyEquatorialFrame(earth)
	v := Vec3{X: 1.5e7, Y: -2.3e7, Z: 4.1e6}
	got := f.ToWorld(f.FromWorld(v))
	if d := got.Sub(v).Norm() / v.Norm(); d > 1e-14 {
		t.Errorf("round-trip rel err %.3e for %+v → %+v", d, v, got)
	}
}

// TestBodyEquatorialFrameEarthSpinAxis: Earth's frame Ez should match
// the spin axis (sin 23.44°, 0, cos 23.44°).
func TestBodyEquatorialFrameEarthSpinAxis(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", AxialTilt: 23.44}
	f := BodyEquatorialFrame(earth)
	tilt := 23.44 * math.Pi / 180
	want := Vec3{X: math.Sin(tilt), Y: 0, Z: math.Cos(tilt)}
	if d := f.Ez.Sub(want).Norm(); d > 1e-14 {
		t.Errorf("Earth Ez = %+v, want %+v", f.Ez, want)
	}
}

// TestBodyEquatorialFrameEcliptic_z_in_body_frame: a point on the
// world ecliptic (Z=0) maps to body-frame Z = -|r|·sin(tilt) when
// r points along world +Y (perpendicular to the spin axis's azimuth).
// Confirms the projection direction matches the rotation convention.
func TestEarthEquatorialFrameProjectsEclipticPoint(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", AxialTilt: 23.44}
	f := BodyEquatorialFrame(earth)
	// World +Y on the ecliptic plane: orthogonal to Earth's spin axis
	// (which lies in the world X-Z plane), so its body-frame Z
	// component should be exactly 0.
	p := Vec3{Y: 1}
	got := f.FromWorld(p)
	if math.Abs(got.Z) > 1e-14 {
		t.Errorf("ecliptic +Y has body-frame Z = %.3e, want 0", got.Z)
	}
	// World +X on the ecliptic plane: lies in the same X-Z plane as
	// the spin axis, so it projects partly along the equator and
	// partly along Ez. Specifically, body-frame Z = sin(tilt).
	q := Vec3{X: 1}
	gotQ := f.FromWorld(q)
	wantZ := math.Sin(23.44 * math.Pi / 180)
	if math.Abs(gotQ.Z-wantZ) > 1e-14 {
		t.Errorf("ecliptic +X has body-frame Z = %.3e, want %.3e", gotQ.Z, wantZ)
	}
}

// TestReferenceFrameForPrimarySunIsIdentity: heliocentric orbits use
// the ecliptic by convention — ReferenceFrameForPrimary(sun) must
// return the identity frame.
func TestReferenceFrameForPrimarySunIsIdentity(t *testing.T) {
	sun := bodies.CelestialBody{ID: "sun", AxialTilt: 7.25}
	f := ReferenceFrameForPrimary(sun)
	id := IdentityFrame()
	if f.Ex != id.Ex || f.Ey != id.Ey || f.Ez != id.Ez {
		t.Errorf("ReferenceFrameForPrimary(sun) = %+v, want identity", f)
	}
}

// TestEquatorialOrbitInBodyFrameHasZeroInclination: a circular orbit
// in Earth's equatorial plane (constructed in world coords by rotating
// a world-XY orbit by Earth's tilt) must report i=0 when extracted in
// the body-equatorial frame, even though it has i=23.44° in the world
// frame. This is the central correctness check for the v0.8.6 fix.
func TestEquatorialOrbitInBodyFrameHasZeroInclination(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", AxialTilt: 23.44}
	f := BodyEquatorialFrame(earth)

	// Build a circular orbit in Earth's equatorial plane: start
	// in body-frame coords (r along Ex, v along Ey for prograde),
	// then rotate to world.
	rMag := 7e6
	mu := 3.986e14
	vMag := math.Sqrt(mu / rMag)
	rBody := Vec3{X: rMag}
	vBody := Vec3{Y: vMag}
	rWorld := f.ToWorld(rBody)
	vWorld := f.ToWorld(vBody)

	// World-frame elements: i should be ~tilt (23.44°).
	elWorld := ElementsFromState(rWorld, vWorld, mu)
	tiltRad := 23.44 * math.Pi / 180
	if d := math.Abs(elWorld.I - tiltRad); d > 1e-9 {
		t.Errorf("world-frame i = %.6f, want %.6f", elWorld.I, tiltRad)
	}

	// Body-frame elements: i should be ~0.
	elBody := ElementsFromStateInFrame(rWorld, vWorld, mu, f)
	if elBody.I > 1e-9 {
		t.Errorf("body-frame i = %.6e, want ~0", elBody.I)
	}
}

// TestEclipticOrbitInEarthFrameHasInclinationEqualToTilt: a 0°
// inclination orbit in the world (ecliptic) frame must read as
// inclination = 23.44° in Earth's body-equatorial frame. This is the
// exact symptom the user reported (a "0°" orbit going over Guatemala
// instead of Ecuador).
func TestEclipticOrbitInEarthFrameHasInclinationEqualToTilt(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", AxialTilt: 23.44}
	f := BodyEquatorialFrame(earth)
	rMag := 7e6
	mu := 3.986e14
	vMag := math.Sqrt(mu / rMag)
	rWorld := Vec3{Y: rMag}
	vWorld := Vec3{X: -vMag}

	elBody := ElementsFromStateInFrame(rWorld, vWorld, mu, f)
	tiltRad := 23.44 * math.Pi / 180
	if d := math.Abs(elBody.I - tiltRad); d > 1e-9 {
		t.Errorf("ecliptic-orbit body-frame i = %.6f, want %.6f", elBody.I, tiltRad)
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
