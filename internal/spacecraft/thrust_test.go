package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestProgradeAtLEORaisesApoapsis: plan §C17 accept criterion. Starting
// circular at LEO, apply a +Δv prograde burn. Resulting orbit's apoapsis
// should equal the two-body transfer ellipse apoapsis to within 0.1%.
func TestProgradeAtLEORaisesApoapsis(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	mu := earth.GravitationalParameter()
	r0 := sc.State.R.Norm()

	dv := 100.0 // 100 m/s prograde
	sc.ApplyImpulsive(BurnPrograde, dv)

	// For a burn at periapsis of a circular orbit, the new periapsis stays at r0
	// (actually becomes periapsis of new orbit) and apoapsis is computed from the
	// new specific energy.
	rMag := sc.State.R.Norm()
	vMag := sc.State.V.Norm()
	eps := 0.5*vMag*vMag - mu/rMag
	a := -mu / (2 * eps)
	// New apoapsis = 2a - r0 (since old r0 becomes new periapsis after prograde boost
	// at a previous periapsis of a circular orbit — circular → elliptical).
	rApo := 2*a - r0

	// Analytic: after Δv at circular periapsis,
	//   v_new = sqrt(μ/r0) + dv
	//   eps = v_new²/2 - μ/r0
	//   a   = -μ/(2 eps)
	//   rApo = 2a - r0
	vNew := math.Sqrt(mu/r0) + dv
	epsWant := 0.5*vNew*vNew - mu/r0
	aWant := -mu / (2 * epsWant)
	rApoWant := 2*aWant - r0

	if d := math.Abs(rApo-rApoWant) / rApoWant; d > 1e-3 {
		t.Errorf("apoapsis after prograde burn: got %.3e m, want %.3e m (rel err %.2e)",
			rApo, rApoWant, d)
	}
}

// TestRetrogradeReducesSpeed: retrograde burn should reduce |v| by exactly dv.
func TestRetrogradeReducesSpeed(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	v0 := sc.OrbitalSpeed()
	sc.ApplyImpulsive(BurnRetrograde, 100)
	if math.Abs(sc.OrbitalSpeed()-(v0-100)) > 1e-6 {
		t.Errorf("retro: |v| went %.3f → %.3f, want %.3f", v0, sc.OrbitalSpeed(), v0-100)
	}
}

// TestBurnConsumesFuel: after a nontrivial burn, fuel should decrease and
// still be ≥ 0.
func TestBurnConsumesFuel(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	f0 := sc.Fuel
	sc.ApplyImpulsive(BurnPrograde, 50)
	if sc.Fuel >= f0 {
		t.Errorf("fuel not consumed: was %.2f, now %.2f", f0, sc.Fuel)
	}
	if sc.Fuel < 0 {
		t.Errorf("fuel went negative: %.2f", sc.Fuel)
	}
}

// TestRemainingDeltaV: starting fuel is half total mass (500 of 1000),
// Isp 300s → Δv = 300 * 9.80665 * ln(2) ≈ 2038 m/s.
func TestRemainingDeltaV(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	got := sc.RemainingDeltaV()
	want := 300.0 * 9.80665 * math.Ln2
	if math.Abs(got-want) > 1 {
		t.Errorf("Δv_remaining = %.2f m/s, want %.2f m/s", got, want)
	}
}
