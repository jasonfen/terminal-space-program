package physics

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

func earthWithAtm() bodies.CelestialBody {
	return bodies.CelestialBody{
		ID:              "earth",
		EnglishName:     "Earth",
		MeanRadius:      6371.0, // km
		Mass:            bodies.Mass{Value: 5.9722, Exponent: 24},
		SideralRotation: 23.9345, // hours
		Atmosphere: &bodies.Atmosphere{
			ScaleHeight:    8500,
			SurfaceDensity: 1.225,
			CutoffAltitude: 150000,
		},
	}
}

func luna() bodies.CelestialBody {
	// No atmosphere — drag must be zero everywhere.
	return bodies.CelestialBody{
		ID:          "moon",
		EnglishName: "Moon",
		MeanRadius:  1737.4,
		Mass:        bodies.Mass{Value: 7.342, Exponent: 22},
	}
}

// TestAtmosphericDensitySurface: surface density equals SurfaceDensity.
func TestAtmosphericDensitySurface(t *testing.T) {
	earth := earthWithAtm()
	got := AtmosphericDensity(earth, 0)
	if math.Abs(got-1.225) > 1e-9 {
		t.Errorf("surface density: got %g, want 1.225", got)
	}
}

// TestAtmosphericDensityScaleHeight: at h = ScaleHeight density falls
// to ρ₀/e — the defining feature of an exponential atmosphere.
func TestAtmosphericDensityScaleHeight(t *testing.T) {
	earth := earthWithAtm()
	got := AtmosphericDensity(earth, 8500)
	want := 1.225 / math.E
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("density at scale height: got %g, want %g", got, want)
	}
}

// TestAtmosphericDensityCutoff: at and above CutoffAltitude density
// hard-zeros (so the integrator can drop drag entirely above the line).
func TestAtmosphericDensityCutoff(t *testing.T) {
	earth := earthWithAtm()
	if got := AtmosphericDensity(earth, 150000); got != 0 {
		t.Errorf("density at cutoff: got %g, want 0", got)
	}
	if got := AtmosphericDensity(earth, 200000); got != 0 {
		t.Errorf("density above cutoff: got %g, want 0", got)
	}
}

// TestAtmosphericDensityNoAtmosphere: bodies without an Atmosphere
// declaration produce zero density — no haze, no drag.
func TestAtmosphericDensityNoAtmosphere(t *testing.T) {
	if got := AtmosphericDensity(luna(), 0); got != 0 {
		t.Errorf("airless body density at surface: got %g, want 0", got)
	}
}

// TestDragAccelOpposesRelativeVelocity: drag must point opposite to
// v_rel — basic sanity check on the sign convention.
func TestDragAccelOpposesRelativeVelocity(t *testing.T) {
	earth := earthWithAtm()
	r := orbital.Vec3{X: earth.RadiusMeters() + 50000} // 50 km up
	v := orbital.Vec3{Y: 7800}
	a := DragAccel(r, v, earth, 0.01)
	// Drag opposes v_rel; v_rel includes -ω×r so it's mostly Y but
	// slightly less than 7800 (Earth surface co-rotation eastward
	// reduces relative airflow). Acceleration's Y component must be
	// strictly negative.
	if a.Y >= 0 {
		t.Errorf("drag accel Y = %g, want < 0 (opposing prograde motion)", a.Y)
	}
}

// TestDragAccelAboveCutoff: zero above CutoffAltitude regardless of
// other inputs.
func TestDragAccelAboveCutoff(t *testing.T) {
	earth := earthWithAtm()
	r := orbital.Vec3{X: earth.RadiusMeters() + 200000} // 200 km — above 150 km cutoff
	v := orbital.Vec3{Y: 7800}
	a := DragAccel(r, v, earth, 0.01)
	if a.Norm() != 0 {
		t.Errorf("drag above cutoff: got |a|=%g, want 0", a.Norm())
	}
}

// TestDragAccelAirless: bodies without an Atmosphere produce zero drag.
func TestDragAccelAirless(t *testing.T) {
	moon := luna()
	r := orbital.Vec3{X: moon.RadiusMeters() + 1000}
	v := orbital.Vec3{Y: 1500}
	a := DragAccel(r, v, moon, 0.01)
	if a.Norm() != 0 {
		t.Errorf("airless drag: got |a|=%g, want 0", a.Norm())
	}
}

// TestDragAccelMagnitudeAtKnownConditions: at sea-level with a craft
// moving 100 m/s relative to the local atmosphere, drag magnitude
// should be 0.5 · ρ · v² · BC = 0.5 · 1.225 · 100² · 0.01 = 61.25 m/s²,
// directed opposite to v_rel. Pin the formula by setting v =
// (ω × r) + v_rel_test so the relative velocity is exactly the test
// vector, free of co-rotation residual.
func TestDragAccelMagnitudeAtKnownConditions(t *testing.T) {
	earth := earthWithAtm()
	r := orbital.Vec3{X: earth.RadiusMeters()} // exactly at surface
	vRelTest := orbital.Vec3{Z: 100}           // straight up relative to atmosphere
	v := AtmosphereOmega(earth).Cross(r).Add(vRelTest)
	a := DragAccel(r, v, earth, 0.01)
	want := -0.5 * 1.225 * 100 * 100 * 0.01
	if math.Abs(a.Z-want) > 1e-6 {
		t.Errorf("drag accel Z = %g, want %g (formula: -0.5·ρ·v²·BC opposing v_rel)", a.Z, want)
	}
	// Other components must be ≈ 0 since v_rel is purely +Z.
	if math.Abs(a.X) > 1e-9 || math.Abs(a.Y) > 1e-9 {
		t.Errorf("drag accel non-Z components nonzero: %+v", a)
	}
}

// TestDynamicPressureAtKnownConditions — q = 0.5·ρ·|v_rel|² (Pa),
// using the same air-relative velocity and exponential-density model
// as DragAccel. At the surface (ρ = 1.225) with a 100 m/s air-relative
// velocity, q = 0.5·1.225·100² = 6125 Pa.
func TestDynamicPressureAtKnownConditions(t *testing.T) {
	earth := earthWithAtm()
	r := orbital.Vec3{X: earth.RadiusMeters()}
	vRelTest := orbital.Vec3{Z: 100}
	v := AtmosphereOmega(earth).Cross(r).Add(vRelTest)
	q := DynamicPressure(r, v, earth)
	want := 0.5 * 1.225 * 100 * 100
	if math.Abs(q-want) > 1e-6 {
		t.Errorf("q = %g, want %g (0.5·ρ·v²)", q, want)
	}
}

// TestDynamicPressureZeroOutsideAtmosphere — zero above the cutoff and
// for airless bodies (the chute can never deploy there).
func TestDynamicPressureZeroOutsideAtmosphere(t *testing.T) {
	earth := earthWithAtm()
	// Above cutoff.
	rHigh := orbital.Vec3{X: earth.RadiusMeters() + earth.Atmosphere.CutoffAltitude + 1000}
	if q := DynamicPressure(rHigh, orbital.Vec3{Z: 5000}, earth); q != 0 {
		t.Errorf("above cutoff: q = %g, want 0", q)
	}
	// Airless body.
	moon := luna()
	rMoon := orbital.Vec3{X: moon.RadiusMeters()}
	if q := DynamicPressure(rMoon, orbital.Vec3{Z: 100}, moon); q != 0 {
		t.Errorf("airless body: q = %g, want 0", q)
	}
}

// TestDragAccelZeroBC: zero ballistic coefficient ⇒ zero drag. (Useful
// shortcut for craft that want to bypass the drag path, though we
// always pass effective-BC ≥ 0.01 today.)
func TestDragAccelZeroBC(t *testing.T) {
	earth := earthWithAtm()
	r := orbital.Vec3{X: earth.RadiusMeters() + 50000}
	v := orbital.Vec3{Y: 7800}
	a := DragAccel(r, v, earth, 0)
	if a.Norm() != 0 {
		t.Errorf("BC=0 drag: got |a|=%g, want 0", a.Norm())
	}
}

// TestAtmosphereOmegaEarth: untilted Earth's spin vector is along
// +Z with ω ≈ 7.292e-5 rad/s. earthWithAtm() leaves AxialTilt = 0
// so the tilted formula collapses to the legacy Z-aligned result.
func TestAtmosphereOmegaEarth(t *testing.T) {
	earth := earthWithAtm()
	w := AtmosphereOmega(earth)
	if w.X != 0 || w.Y != 0 {
		t.Errorf("ω should be along Z only on untilted body, got %+v", w)
	}
	want := 2 * math.Pi / (23.9345 * 3600)
	if math.Abs(w.Z-want) > 1e-12 {
		t.Errorf("ω.Z = %g, want %g", w.Z, want)
	}
}

// TestAtmosphereOmegaTiltedEarth — v0.11.2+ (ADR 0003) pins the
// unification: a tilted Earth (AxialTilt = 23.44°) produces a spin
// vector along the physical spin axis n = (sin tilt, 0, cos tilt),
// not the pre-v0.11.2 world +Z. Magnitude is unchanged.
func TestAtmosphereOmegaTiltedEarth(t *testing.T) {
	earth := earthWithAtm()
	earth.AxialTilt = 23.44 // matches sol.json catalog value
	w := AtmosphereOmega(earth)
	tiltRad := 23.44 * math.Pi / 180.0
	mag := 2 * math.Pi / (23.9345 * 3600)
	wantX := mag * math.Sin(tiltRad)
	wantZ := mag * math.Cos(tiltRad)
	if math.Abs(w.X-wantX) > 1e-12 {
		t.Errorf("tilted Earth ω.X = %g, want %g (= |ω| sin 23.44°)", w.X, wantX)
	}
	if math.Abs(w.Y) > 1e-12 {
		t.Errorf("tilted Earth (azimuth 0) ω.Y = %g, want 0", w.Y)
	}
	if math.Abs(w.Z-wantZ) > 1e-12 {
		t.Errorf("tilted Earth ω.Z = %g, want %g (= |ω| cos 23.44°)", w.Z, wantZ)
	}
	// Magnitude check — the unification doesn't change |ω|.
	got := math.Sqrt(w.X*w.X + w.Y*w.Y + w.Z*w.Z)
	if math.Abs(got-mag) > 1e-12 {
		t.Errorf("|ω| = %g, want %g (unchanged by tilt)", got, mag)
	}
}

// TestAtmosphereOmegaTidallyLockedUsesOrbit — tidally-locked bodies
// rotate at their orbital period, not their (unused/zero/ignored)
// sidereal-rotation period. Mirrors render.rotationPeriodSeconds so
// the renderer and physics agree.
func TestAtmosphereOmegaTidallyLockedUsesOrbit(t *testing.T) {
	luna := bodies.CelestialBody{
		ID:              "moon",
		SideralRotation: 9999.0, // junk — must be ignored when TidallyLocked
		SideralOrbit:    27.321661,
		TidallyLocked:   true,
	}
	w := AtmosphereOmega(luna)
	want := 2 * math.Pi / (27.321661 * 86400)
	got := math.Sqrt(w.X*w.X + w.Y*w.Y + w.Z*w.Z)
	if math.Abs(got-want) > 1e-12 {
		t.Errorf("|ω| = %g, want %g (orbital period for tidally-locked)", got, want)
	}
}
