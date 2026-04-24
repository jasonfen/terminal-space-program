package orbital

import (
	"math"
	"testing"
)

// TestElementsFromStateCircularLEO: circular orbit at 6578 km (200 km alt
// over Earth) should produce e≈0 and a≈r.
func TestElementsFromStateCircularLEO(t *testing.T) {
	mu := 3.986e14
	r0 := 6.578e6
	v0 := math.Sqrt(mu / r0)
	r := Vec3{X: r0}
	v := Vec3{Y: v0}
	el := ElementsFromState(r, v, mu)
	if math.Abs(el.A-r0)/r0 > 1e-12 {
		t.Errorf("a = %.6e, want %.6e", el.A, r0)
	}
	if el.E > 1e-12 {
		t.Errorf("e = %.3e, want ~0", el.E)
	}
	if el.I > 1e-12 {
		t.Errorf("i = %.3e rad, want ~0", el.I)
	}
}

// TestElementsFromStateEllipticalApoPeri: known elliptical orbit with
// r_peri=7000 km, r_apo=42000 km → a=24500 km, e=0.714.
func TestElementsFromStateEllipticalApoPeri(t *testing.T) {
	mu := 3.986e14
	rPeri := 7e6
	rApo := 4.2e7
	aWant := (rPeri + rApo) / 2
	eWant := (rApo - rPeri) / (rApo + rPeri)

	// Start at periapsis: vis-viva gives v.
	vPeri := math.Sqrt(mu * (2/rPeri - 1/aWant))
	el := ElementsFromState(Vec3{X: rPeri}, Vec3{Y: vPeri}, mu)

	if d := math.Abs(el.A-aWant) / aWant; d > 1e-6 {
		t.Errorf("a = %.6e, want %.6e", el.A, aWant)
	}
	if d := math.Abs(el.E - eWant); d > 1e-6 {
		t.Errorf("e = %.6f, want %.6f", el.E, eWant)
	}
	if math.Abs(el.Apoapsis()-rApo) > 10 {
		t.Errorf("apoapsis %.3e m, want %.3e m", el.Apoapsis(), rApo)
	}
	if math.Abs(el.Periapsis()-rPeri) > 10 {
		t.Errorf("periapsis %.3e m, want %.3e m", el.Periapsis(), rPeri)
	}
}

// TestElementsEquatorialArgPeriapsis: equatorial elliptical orbit with
// periapsis rotated to the +Y direction (ω = π/2 when Ω = 0). Pre-
// v0.3.4 the node vector degenerated at i≈0 and ω stayed at 0,
// freezing the rendered orbit at the initial-state orientation after
// a burn rotated the real periapsis.
func TestElementsEquatorialArgPeriapsis(t *testing.T) {
	mu := 3.986e14
	rPeri := 7e6
	rApo := 4.2e7
	aWant := (rPeri + rApo) / 2
	vPeri := math.Sqrt(mu * (2/rPeri - 1/aWant))

	// Start at +Y periapsis with retrograde-prograde tangent. Velocity
	// at periapsis is perpendicular to r; for +Y periapsis the
	// prograde tangent in the equatorial plane is -X (so h_z > 0).
	el := ElementsFromState(
		Vec3{Y: rPeri},
		Vec3{X: -vPeri},
		mu,
	)

	wantArg := math.Pi / 2
	if d := math.Abs(el.Arg - wantArg); d > 1e-9 {
		t.Errorf("equatorial ω = %.6f rad, want %.6f rad", el.Arg, wantArg)
	}

	// And the rendered periapsis position should land at +Y, not +X.
	peri := PositionAtTrueAnomaly(el, 0)
	if math.Abs(peri.X) > 1 || math.Abs(peri.Y-rPeri)/rPeri > 1e-6 {
		t.Errorf("equatorial periapsis rendered at %+v, want ~(0, %g, 0)", peri, rPeri)
	}
}

// TestInclinedOrbit: 30° inclination, circular, should produce i=30° and e≈0.
func TestInclinedOrbit(t *testing.T) {
	mu := 3.986e14
	r0 := 7e6
	v0 := math.Sqrt(mu / r0)
	incWant := 30.0 * math.Pi / 180.0
	r := Vec3{X: r0}
	v := Vec3{Y: v0 * math.Cos(incWant), Z: v0 * math.Sin(incWant)}
	el := ElementsFromState(r, v, mu)
	if d := math.Abs(el.I - incWant); d > 1e-10 {
		t.Errorf("i = %.6f rad, want %.6f rad", el.I, incWant)
	}
}
