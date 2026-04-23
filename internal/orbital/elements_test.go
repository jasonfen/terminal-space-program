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
