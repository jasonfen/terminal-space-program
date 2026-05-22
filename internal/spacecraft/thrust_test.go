package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
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

// TestRemainingDeltaV: v0.5.13+ default fuel 40000 kg / dry 11000 kg,
// Isp 421s → Δv = 421 * 9.80665 * ln(51050/(11000+50)) ≈ 6314 m/s.
// v0.8.0+: monoprop is dead weight to the main engine, so the
// floor is dry+monoprop rather than just dry — slightly lower Δv
// budget vs the pre-RCS 6326 m/s.
func TestRemainingDeltaV(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	got := sc.RemainingDeltaV()
	want := 421.0 * 9.80665 * math.Log(sc.TotalMass()/(sc.DryMass+sc.Monoprop))
	if math.Abs(got-want) > 1 {
		t.Errorf("Δv_remaining = %.2f m/s, want %.2f m/s", got, want)
	}
}

// TestMassFlowRate: v0.5.13+ default Thrust 1 023 000 N, Isp 421 s
// (J-2) → ṁ = 1 023 000 / (421 · 9.80665) ≈ 247.8 kg/s.
func TestMassFlowRate(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	got := sc.MassFlowRate()
	want := 1023000.0 / (421.0 * 9.80665)
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("MassFlowRate = %.6f, want %.6f", got, want)
	}
}

// TestMassFlowRateZeroWhenNoThrust: if engine has no thrust, ṁ must be 0
// even when Isp is positive.
func TestMassFlowRateZeroWhenNoThrust(t *testing.T) {
	sc := &Spacecraft{Thrust: 0, Isp: 300}
	if got := sc.MassFlowRate(); got != 0 {
		t.Errorf("MassFlowRate with zero thrust = %v, want 0", got)
	}
}

// TestBurnTimeForDVRocketEquation: at S-IVB-1 specs (51050 kg total
// with v0.8.0+ monoprop, 1023 kN thrust, Isp 421 s) a 100 m/s Δv
// requires
//
//	t = (m0/ṁ) · (1 - exp(-Δv/(Isp·g0)))
//	  ≈ 4.93 s
//
// Validates the rocket-equation form vs a constant-mass approximation.
func TestBurnTimeForDVRocketEquation(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	got := sc.BurnTimeForDV(100).Seconds()
	mDot := 1023000.0 / (421.0 * 9.80665)
	want := (sc.TotalMass() / mDot) * (1 - math.Exp(-100.0/(421.0*9.80665)))
	if math.Abs(got-want) > 1e-3 {
		t.Errorf("BurnTimeForDV(100) = %.3f s, want %.3f s", got, want)
	}
}

// TestBurnTimeForDVZeroDV: zero Δv input must yield zero duration so
// the form's enter-on-empty / planner zero-Δv paths fall through to
// the impulsive branch.
func TestBurnTimeForDVZeroDV(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	if got := sc.BurnTimeForDV(0); got != 0 {
		t.Errorf("BurnTimeForDV(0) = %v, want 0", got)
	}
}

// TestBurnTimeForDVZeroThrust: a thrust-less craft must return 0 so
// the App's switch falls into the impulsive branch (legacy path
// preserved through the API even though the form no longer surfaces
// it).
func TestBurnTimeForDVZeroThrust(t *testing.T) {
	sc := &Spacecraft{Thrust: 0, Isp: 421, Fuel: 100, DryMass: 1000}
	if got := sc.BurnTimeForDV(50); got != 0 {
		t.Errorf("BurnTimeForDV with zero thrust = %v, want 0", got)
	}
}

// TestThrustAccelFnAddsThrustOnTopOfGravity: at LEO with prograde mode,
// the thrust component should equal Thrust/mass along the velocity unit
// vector. Gravity component should match physics.Accel.
func TestThrustAccelFnAddsThrustOnTopOfGravity(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	mu := earth.GravitationalParameter()
	r := sc.State.R
	v := sc.State.V
	mass := sc.TotalMass()

	accelFn := sc.ThrustAccelFn(BurnPrograde, mu)
	got := accelFn(r, v, 0)

	// Expected = gravity + (Thrust/mass) along v_hat.
	gravity := orbital.Vec3{}
	rMag := r.Norm()
	gFactor := -mu / (rMag * rMag * rMag)
	gravity.X = r.X * gFactor
	gravity.Y = r.Y * gFactor
	gravity.Z = r.Z * gFactor
	vHat := v.Scale(1 / v.Norm())
	want := gravity.Add(vHat.Scale(sc.Thrust / mass))

	if got.Sub(want).Norm()/want.Norm() > 1e-9 {
		t.Errorf("ThrustAccelFn: got %+v, want %+v", got, want)
	}
}

// TestThrustAccelFnNoThrustWhenFuelEmpty: with Fuel=0, the closure must
// return pure gravity even though Thrust is configured. v0.9.4+:
// drains Stages[0] (the firing stage) instead of zeroing the summed
// shadow field, since the cutoff check now reads ActiveStageFuel.
func TestThrustAccelFnNoThrustWhenFuelEmpty(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)
	sc.BurnFuel(sc.Stages[0].FuelMass) // drain the firing stage
	mu := earth.GravitationalParameter()

	accelFn := sc.ThrustAccelFn(BurnPrograde, mu)
	got := accelFn(sc.State.R, sc.State.V, 0)

	rMag := sc.State.R.Norm()
	gFactor := -mu / (rMag * rMag * rMag)
	want := orbital.Vec3{X: sc.State.R.X * gFactor, Y: sc.State.R.Y * gFactor, Z: sc.State.R.Z * gFactor}

	if got.Sub(want).Norm() > 1e-9 {
		t.Errorf("with empty fuel, got %+v, want pure gravity %+v", got, want)
	}
}

// TestThrustAccelFnNoThrustWhenBottomStageEmpty: regression for the
// v0.9.4 multi-stage cutoff bug. A Saturn V whose S-IC has burned dry
// must not continue thrusting just because S-II + S-IVB still hold
// propellant — the firing engine has nothing to burn. Pre-fix the
// summed-fuel check let the engine apply free thrust through an
// empty bottom stage.
func TestThrustAccelFnNoThrustWhenBottomStageEmpty(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewFromLoadout(LoadoutSaturnVID)
	sc.Primary = *earth
	sc.State.R = orbital.Vec3{X: earth.RadiusMeters() + 70e3}
	sc.State.V = orbital.Vec3{Y: 2500}
	mu := earth.GravitationalParameter()

	// Drain S-IC fully but leave S-II + S-IVB with their full tanks.
	sc.BurnFuel(sc.Stages[0].FuelMass)
	if sc.Stages[0].FuelMass != 0 {
		t.Fatalf("setup: S-IC fuel = %.0f, want 0", sc.Stages[0].FuelMass)
	}
	if sc.Fuel <= 0 {
		t.Fatalf("setup: summed fuel = %.0f, want > 0 (S-II + S-IVB still full)", sc.Fuel)
	}

	accelFn := sc.ThrustAccelFn(BurnPrograde, mu)
	got := accelFn(sc.State.R, sc.State.V, 0)

	rMag := sc.State.R.Norm()
	gFactor := -mu / (rMag * rMag * rMag)
	want := orbital.Vec3{X: sc.State.R.X * gFactor, Y: sc.State.R.Y * gFactor, Z: sc.State.R.Z * gFactor}

	if got.Sub(want).Norm() > 1e-6 {
		t.Errorf("dry S-IC with full upper stages: got %+v, want pure gravity %+v (no free thrust)", got, want)
	}
}

// TestPlaneChangeDirectionRotatesPlanePreservingSpeed: the v0.10.4
// plane-change burn must rotate the orbital plane by |θ| while keeping
// |v| unchanged. The pre-v0.10.4 bug applied the plane-change Δv along
// pure orbit-normal, which always grows |v| (|v_new| = √(v²+Δv²)) and
// under-rotates the plane — leaving the orbit eccentric and off-plane.
func TestPlaneChangeDirectionRotatesPlanePreservingSpeed(t *testing.T) {
	// Circular orbit: position +X, velocity +Y → all velocity is
	// horizontal, so the plane-change Δv magnitude is 2·v·sin(|θ|/2).
	r := orbital.Vec3{X: 7.0e6}
	speed := 7.5e3
	v := orbital.Vec3{Y: speed}
	for _, theta := range []float64{0.1, -0.1, 0.5, -0.5, 1.0} {
		dir := planeChangeDirection(r, v, theta)
		dv := 2 * speed * math.Sin(math.Abs(theta)/2)
		vNew := v.Add(dir.Scale(dv))

		if rel := math.Abs(vNew.Norm()-speed) / speed; rel > 1e-9 {
			t.Errorf("theta=%.2f: |v| changed by %.2e (rel) — not a pure rotation", theta, rel)
		}

		hOld := r.Cross(v)
		hNew := r.Cross(vNew)
		cosA := hOld.Dot(hNew) / (hOld.Norm() * hNew.Norm())
		if cosA > 1 {
			cosA = 1
		} else if cosA < -1 {
			cosA = -1
		}
		if ang := math.Acos(cosA); math.Abs(ang-math.Abs(theta)) > 1e-9 {
			t.Errorf("theta=%.2f: plane rotated %.5f rad, want %.5f", theta, ang, math.Abs(theta))
		}
	}

	// Degenerate inputs return the zero vector (burn no-ops).
	if d := planeChangeDirection(r, v, 0); d.Norm() != 0 {
		t.Errorf("theta=0: got %+v, want zero vector", d)
	}
	if d := planeChangeDirection(orbital.Vec3{}, v, 0.5); d.Norm() != 0 {
		t.Errorf("zero r: got %+v, want zero vector", d)
	}
}
