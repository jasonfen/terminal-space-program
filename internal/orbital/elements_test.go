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

// TestNearCircularOrbitOmegaIsStable: with a near-zero eccentricity
// (state perturbed by float noise), ω is mathematically undefined.
// Pre-v0.8.6 ElementsFromState returned atan2(noise, noise) for ω,
// which flipped each frame and made the orbit-flat canvas basis jerk
// around the orbit normal. The fix snaps ω to 0 when eMag falls below
// circularTol; this test verifies two close-to-circular states with
// different noise patterns both report ω = 0.
func TestNearCircularOrbitOmegaIsStable(t *testing.T) {
	mu := 3.986e14
	r0 := 7e6
	v0 := math.Sqrt(mu / r0)
	// Two states that are circular up to float noise but perturbed
	// in different directions. Both have eMag well below circularTol
	// (1e-6); ω should snap to 0.
	stateA := struct{ R, V Vec3 }{
		R: Vec3{X: r0},
		V: Vec3{Y: v0 * (1 + 1e-15), Z: v0 * 1e-15},
	}
	stateB := struct{ R, V Vec3 }{
		R: Vec3{X: r0 * (1 + 1e-15)},
		V: Vec3{Y: v0},
	}
	elA := ElementsFromState(stateA.R, stateA.V, mu)
	elB := ElementsFromState(stateB.R, stateB.V, mu)
	if elA.Arg != 0 {
		t.Errorf("near-circular state A: ω = %.6e rad, want 0 (snap)", elA.Arg)
	}
	if elB.Arg != 0 {
		t.Errorf("near-circular state B: ω = %.6e rad, want 0 (snap)", elB.Arg)
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

// TestElementsFromStateDegenerateAngles pins the angle conventions on the
// exact edges where a quantity is undefined or sits on a branch cut: the
// guard must pick "undefined means 0" (no NaN), and an angle that is exactly
// 0 must stay 0 rather than wrap to 2π. Each row is one boundary comparison
// in ElementsFromState.
func TestElementsFromStateDegenerateAngles(t *testing.T) {
	const mu = 3.986e14
	const rp = 7e6
	vPeri := 1.2 * math.Sqrt(mu/rp) // e ≈ 0.44, periapsis at the start point
	cases := []struct {
		name                string
		r, v                Vec3
		wantI, wantO, wantW float64
	}{
		// h = 0: no orbit plane, so inclination is 0, not acos(0/0).
		{"radial: h=0 gives i=0", Vec3{X: rp}, Vec3{X: 1e3}, 0, 0, math.NaN()},
		// n = ẑ×h = 0: no node, so Ω is 0, not acos(0/0).
		{"equatorial: n=0 gives Ω=0", Vec3{X: rp}, Vec3{Y: vPeri}, 0, 0, 0},
		// Polar, h along −Y: node vector on +X with n.Y == 0 exactly, so Ω
		// is 0 and must not wrap to 2π; periapsis on that node line with
		// e_z == 0 exactly, so ω is 0 and must not wrap to 2π.
		{"polar, periapsis at the ascending node", Vec3{X: rp}, Vec3{Z: vPeri}, math.Pi / 2, 0, 0},
	}
	for _, c := range cases {
		el := ElementsFromState(c.r, c.v, mu)
		if math.IsNaN(el.I) || math.IsNaN(el.Omega) || math.IsNaN(el.Arg) {
			t.Errorf("%s: NaN angle in %+v", c.name, el)
			continue
		}
		if math.Abs(el.I-c.wantI) > 1e-9 || el.Omega != c.wantO {
			t.Errorf("%s: i=%v Ω=%v, want i=%v Ω=%v", c.name, el.I, el.Omega, c.wantI, c.wantO)
		}
		if !math.IsNaN(c.wantW) && el.Arg != c.wantW {
			t.Errorf("%s: ω=%v, want exactly %v", c.name, el.Arg, c.wantW)
		}
	}
}
