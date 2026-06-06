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
	calc := ForSystem(sol)
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

// TestPlaneMatchInclinationEarthFromSunIsZero: Earth's plane viewed
// from heliocentric orbit (sun-frame = identity) is the ecliptic, so
// matching it from the Sun's frame requires i ≈ Earth's ecliptic-
// relative inclination (~0.034°).
func TestPlaneMatchInclinationEarthFromSunIsZero(t *testing.T) {
	systems, _ := bodies.LoadAll()
	sol := systems[0]
	earth := sol.FindBody("Earth")
	sunFrame := IdentityFrame()
	got := PlaneMatchInclination(*earth, sunFrame)
	// Earth's ecliptic-relative inclination is tiny (~0.034°). Allow
	// 0.1° margin.
	if got > 0.1*math.Pi/180 {
		t.Errorf("PlaneMatch(Earth, sun) = %.4f rad (%.3f°), want ~0", got, got*180/math.Pi)
	}
}

// TestPlaneMatchInclinationMarsFromEarth: matching Mars's heliocentric
// orbit plane from a LEO orbit requires inclining LEO by Earth's
// axial tilt (Mars's ecliptic-relative i is small, so the dominant
// term is Earth's 23.44° tilt away from the ecliptic).
func TestPlaneMatchInclinationMarsFromEarth(t *testing.T) {
	systems, _ := bodies.LoadAll()
	sol := systems[0]
	earth := sol.FindBody("Earth")
	mars := sol.FindBody("Mars")
	earthFrame := BodyEquatorialFrame(*earth)
	got := PlaneMatchInclination(*mars, earthFrame)
	// Earth's tilt (23.44°) ± Mars's small ecliptic inclination (1.85°)
	// — depending on the relative orientation of nodes. Loose bounds:
	// somewhere in [21°, 26°].
	gotDeg := got * 180 / math.Pi
	if gotDeg < 21 || gotDeg > 26 {
		t.Errorf("PlaneMatch(Mars, Earth-frame) = %.2f°, want 21–26°", gotDeg)
	}
}

// TestPlaneMatchInclinationSunReturnsZero: defensive — passing the
// Sun returns 0 (no orbit to match).
func TestPlaneMatchInclinationSunReturnsZero(t *testing.T) {
	systems, _ := bodies.LoadAll()
	sol := systems[0]
	sun := sol.FindBody("Sun")
	if sun == nil {
		t.Skip("Sol system has no body named Sun")
	}
	if got := PlaneMatchInclination(*sun, IdentityFrame()); got != 0 {
		t.Errorf("PlaneMatch(Sun, *) = %.6f, want 0", got)
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

// --- v0.10.0 rate-limited attitude: Unit + Rodrigues Rotate ---

func vecClose(a, b Vec3, tol float64) bool { return a.Sub(b).Norm() <= tol }

func TestUnitNormalizesAndZeroSafe(t *testing.T) {
	if got := (Vec3{3, 0, 4}).Unit(); !vecClose(got, Vec3{0.6, 0, 0.8}, 1e-12) {
		t.Errorf("Unit() = %+v, want {0.6 0 0.8}", got)
	}
	if got := (Vec3{}).Unit(); got != (Vec3{}) {
		t.Errorf("Unit() of zero = %+v, want zero", got)
	}
}

func TestRotateIdentityAtZeroTheta(t *testing.T) {
	v := Vec3{1, 2, 3}
	if got := Rotate(v, Vec3{0, 0, 1}, 0); !vecClose(got, v, 1e-12) {
		t.Errorf("Rotate(θ=0) = %+v, want %+v", got, v)
	}
}

func TestRotate90AboutZMapsXToY(t *testing.T) {
	got := Rotate(Vec3{1, 0, 0}, Vec3{0, 0, 1}, math.Pi/2)
	if !vecClose(got, Vec3{0, 1, 0}, 1e-12) {
		t.Errorf("Rotate(X, +Z, 90°) = %+v, want {0 1 0}", got)
	}
}

func TestRotate180Antiparallel(t *testing.T) {
	got := Rotate(Vec3{1, 0, 0}, Vec3{0, 1, 0}, math.Pi)
	if !vecClose(got, Vec3{-1, 0, 0}, 1e-12) {
		t.Errorf("Rotate(X, +Y, 180°) = %+v, want {-1 0 0}", got)
	}
}

func TestRotateAboutOwnAxisIsNoop(t *testing.T) {
	axis := (Vec3{1, 1, 1}).Unit()
	if got := Rotate(axis, axis, 1.234); !vecClose(got, axis, 1e-12) {
		t.Errorf("rotating axis about itself changed it: %+v != %+v", got, axis)
	}
}

func TestRotatePreservesNorm(t *testing.T) {
	v := Vec3{2, -3, 5}
	axis := (Vec3{0, 1, 2}).Unit()
	got := Rotate(v, axis, 0.7)
	if d := math.Abs(got.Norm()-v.Norm()) / v.Norm(); d > 1e-12 {
		t.Errorf("Rotate changed norm: |got|=%.9f |v|=%.9f (rel %.2e)", got.Norm(), v.Norm(), d)
	}
}

// TestOrbitNormalWorldWithOrbitalElementsOverride — OrbitNormalWorld must
// guard the *computed* semimajor axis (which honors the OrbitalElements
// override), not the top-level SemimajorAxis field. A body with
// top-level SemimajorAxis=0 but a populated override has a real orbit;
// guarding the top-level field wrongly returned a zero normal, which
// makes PlaneMatchInclination collapse to 0 and breaks transfer
// planning. (#90)
func TestOrbitNormalWorldWithOrbitalElementsOverride(t *testing.T) {
	b := bodies.CelestialBody{
		SemimajorAxis: 0, // top-level zero — the override carries the real orbit
		OrbitalElements: &bodies.OrbitalElement{
			SemimajorAxis:            1e8, // km
			Inclination:              30,
			LongitudeOfAscendingNode: 90,
		},
	}
	n := OrbitNormalWorld(b)
	if n.Norm() == 0 {
		t.Fatal("OrbitNormalWorld returned zero for a body whose orbit lives in the OrbitalElements override")
	}
	// i=30°, Ω=90° → normal = {sinΩ·sinI, −cosΩ·sinI, cosI} = {0.5, 0, cos30°}.
	want := Vec3{X: 0.5, Y: 0, Z: math.Cos(30 * math.Pi / 180)}
	if math.Abs(n.X-want.X) > 1e-12 || math.Abs(n.Y-want.Y) > 1e-12 || math.Abs(n.Z-want.Z) > 1e-12 {
		t.Errorf("normal = %+v, want %+v", n, want)
	}
}

// TestPositionAtTrueAnomalyHyperbolic — for a hyperbolic orbit (e>1) the
// semi-latus rectum p = a(1−e²) is negative, so the old `if p == 0`
// guard let a negative radius through. Guarding `p <= 0` returns the
// zero vector instead, matching VelocityAtTrueAnomaly. (#90)
func TestPositionAtTrueAnomalyHyperbolic(t *testing.T) {
	el := Elements{A: 1e7, E: 1.5}
	for _, nu := range []float64{0, math.Pi} {
		if got := PositionAtTrueAnomaly(el, nu); got != (Vec3{}) {
			t.Errorf("PositionAtTrueAnomaly(e=1.5, ν=%.3f) = %+v, want zero vector", nu, got)
		}
	}
}

// TestPositionAtTrueAnomalyParabolic — parabolic (e=1) gives p=0; the
// `p <= 0` guard covers it as before. (#90)
func TestPositionAtTrueAnomalyParabolic(t *testing.T) {
	el := Elements{A: 1e7, E: 1.0}
	if got := PositionAtTrueAnomaly(el, 0); got != (Vec3{}) {
		t.Errorf("PositionAtTrueAnomaly(e=1) = %+v, want zero vector", got)
	}
}
