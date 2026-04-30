package planner

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// rLEO is a 500 km circular LEO around Earth (the v0.6.1 default
// spawn altitude). muEarth is shared with transfer_test.go.
const rLEO = 6.378137e6 + 500e3

// circularInclinedState synthesises a state vector for a circular
// orbit at radius rPark inclined to the equatorial plane by inc
// (radians), with longitude of ascending node Ω = 0 and the craft
// sitting on +x at the ascending node moment (so v has a +z component
// — rising through equator).
func circularInclinedState(rPark, inc, mu float64) orbital.Vec3State {
	v := math.Sqrt(mu / rPark)
	return orbital.Vec3State{
		R: orbital.Vec3{X: rPark},
		V: orbital.Vec3{Y: v * math.Cos(inc), Z: v * math.Sin(inc)},
	}
}

// TestPlanInclinationDeltaVMatchesCircularFormula: for a circular
// orbit, the load-bearing identity is Δv = 2v·sin(|Δi|/2). Test a
// 28.5° → 0° rotation — the canonical "drop LEO inclination to
// equatorial" maneuver.
func TestPlanInclinationDeltaVMatchesCircularFormula(t *testing.T) {
	const inc = 28.5 * math.Pi / 180
	state := circularInclinedState(rLEO, inc, muEarth)

	plan, err := PlanInclinationChange(state, muEarth, 0, "earth")
	if err != nil {
		t.Fatalf("PlanInclinationChange: %v", err)
	}

	v := math.Sqrt(muEarth / rLEO)
	wantDv := 2 * v * math.Sin(inc/2)
	if math.Abs(plan.DV-wantDv) > 1e-6 {
		t.Errorf("Δv = %.6f m/s, want %.6f m/s (Δ %.3e)",
			plan.DV, wantDv, plan.DV-wantDv)
	}
}

// TestPlanInclinationPicksDescendingNodeFromAN: with the craft
// currently at the ascending node (rising through equator), the next
// equator crossing is the descending node — half a period away. The
// planner picks DN and reports AtAN=false. This is the load-bearing
// case for the v0.Z-based physical AN/DN identification: circular
// orbits have a degenerate ω that flips the events-helper labels, so
// trusting the labels would mis-identify the burn direction.
func TestPlanInclinationPicksDescendingNodeFromAN(t *testing.T) {
	const inc = 28.5 * math.Pi / 180
	state := circularInclinedState(rLEO, inc, muEarth)

	plan, err := PlanInclinationChange(state, muEarth, 0, "earth")
	if err != nil {
		t.Fatalf("PlanInclinationChange: %v", err)
	}
	if plan.AtAN {
		t.Errorf("expected DN (state at AN, rising through equator), got AtAN=true")
	}

	period := 2 * math.Pi * math.Sqrt(rLEO*rLEO*rLEO/muEarth)
	wantHalfPeriod := period / 2
	gotSecs := plan.OffsetTime.Seconds()
	if math.Abs(gotSecs-wantHalfPeriod) > 1.0 {
		t.Errorf("OffsetTime = %.1f s, want %.1f s (half-period)",
			gotSecs, wantHalfPeriod)
	}
}

// TestPlanInclinationDecreaseAtDNUsesNormalPlus: at DN, decreasing
// inclination requires +Normal (the rule's geometric sign).
func TestPlanInclinationDecreaseAtDNUsesNormalPlus(t *testing.T) {
	const inc = 28.5 * math.Pi / 180
	state := circularInclinedState(rLEO, inc, muEarth)

	plan, err := PlanInclinationChange(state, muEarth, 0, "earth")
	if err != nil {
		t.Fatalf("PlanInclinationChange: %v", err)
	}
	// Planner picks DN; target 0 < current 28.5° → decrease → +1.
	if plan.NormalSign != +1 {
		t.Errorf("NormalSign = %d at DN decreasing inclination, want +1", plan.NormalSign)
	}
}

// TestPlanInclinationIncreaseAtDNUsesNormalMinus: at DN, increasing
// inclination requires -Normal.
func TestPlanInclinationIncreaseAtDNUsesNormalMinus(t *testing.T) {
	const incFrom = 28.5 * math.Pi / 180
	const incTo = 45 * math.Pi / 180
	state := circularInclinedState(rLEO, incFrom, muEarth)

	plan, err := PlanInclinationChange(state, muEarth, incTo, "earth")
	if err != nil {
		t.Fatalf("PlanInclinationChange: %v", err)
	}
	if plan.AtAN {
		t.Errorf("expected DN (state is at AN), got AtAN=true")
	}
	if plan.NormalSign != -1 {
		t.Errorf("NormalSign = %d at DN increasing inclination, want -1", plan.NormalSign)
	}

	v := math.Sqrt(muEarth / rLEO)
	wantDv := 2 * v * math.Sin(math.Abs(incTo-incFrom)/2)
	if math.Abs(plan.DV-wantDv) > 1e-6 {
		t.Errorf("Δv = %.6f m/s, want %.6f m/s", plan.DV, wantDv)
	}
}

// TestPlanInclinationFromEquatorialFiresAtCurrentState: v0.8.2.x
// equatorial sources have no defined node line, but every point on
// the orbit is in the equatorial plane, so the planner can pick
// the current state as the burn point and rotate the plane there.
// Replaces the pre-v0.8.2.x rejection that returned
// ErrEquatorialOrbit.
func TestPlanInclinationFromEquatorialFiresAtCurrentState(t *testing.T) {
	v := math.Sqrt(muEarth / rLEO)
	state := orbital.Vec3State{
		R: orbital.Vec3{X: rLEO},
		V: orbital.Vec3{Y: v}, // pure equatorial, no z velocity
	}
	const targetIncl = 28.5 * math.Pi / 180
	plan, err := PlanInclinationChange(state, muEarth, targetIncl, "earth")
	if err != nil {
		t.Fatalf("equatorial source: unexpected err %v", err)
	}
	if plan.OffsetTime != 0 {
		t.Errorf("OffsetTime = %v, want 0 — burn fires at current state", plan.OffsetTime)
	}
	// Δv = 2·v·sin(Δi/2) for an equatorial circular source.
	want := 2 * v * math.Sin(targetIncl/2)
	if math.Abs(plan.DV-want) > 1 {
		t.Errorf("Δv = %.1f m/s, want %.1f m/s", plan.DV, want)
	}
	if plan.NormalSign != +1 {
		t.Errorf("NormalSign = %d, want +1 (raise inclination from prograde-equatorial)", plan.NormalSign)
	}
}

// TestPlanInclinationRejectsHyperbolic: e ≥ 1 → no closed orbit, no
// repeating node crossings → ErrHyperbolicOrbit.
func TestPlanInclinationRejectsHyperbolic(t *testing.T) {
	// Synthesize a hyperbolic state: position at LEO, velocity above
	// escape speed (~1.5× circular). Tilted out of plane so
	// inclination is non-zero and we exercise the e≥1 check, not the
	// equatorial check.
	const inc = 28.5 * math.Pi / 180
	vCirc := math.Sqrt(muEarth / rLEO)
	v := 1.5 * vCirc
	state := orbital.Vec3State{
		R: orbital.Vec3{X: rLEO},
		V: orbital.Vec3{Y: v * math.Cos(inc), Z: v * math.Sin(inc)},
	}
	_, err := PlanInclinationChange(state, muEarth, 0, "earth")
	if !errors.Is(err, ErrHyperbolicOrbit) {
		t.Errorf("hyperbolic source: err = %v, want ErrHyperbolicOrbit", err)
	}
}

// TestPlanInclinationRejectsBadTarget: targetIncl outside [0, π] is
// nonsense — surface as ErrInclinationRange.
func TestPlanInclinationRejectsBadTarget(t *testing.T) {
	const inc = 28.5 * math.Pi / 180
	state := circularInclinedState(rLEO, inc, muEarth)

	for _, tgt := range []float64{-0.1, math.Pi + 0.1, 4} {
		_, err := PlanInclinationChange(state, muEarth, tgt, "earth")
		if !errors.Is(err, ErrInclinationRange) {
			t.Errorf("target %.3f: err = %v, want ErrInclinationRange", tgt, err)
		}
	}
}

// TestPlanInclinationNoOpWhenAlreadyAtTarget: |Δi| < 1 µrad is the
// rounding-floor for "already there" — return ErrInclinationNoOp.
func TestPlanInclinationNoOpWhenAlreadyAtTarget(t *testing.T) {
	const inc = 28.5 * math.Pi / 180
	state := circularInclinedState(rLEO, inc, muEarth)

	_, err := PlanInclinationChange(state, muEarth, inc, "earth")
	if !errors.Is(err, ErrInclinationNoOp) {
		t.Errorf("Δi=0: err = %v, want ErrInclinationNoOp", err)
	}
}

// TestPlanInclinationPrimaryIDPropagates: the burn fires in the
// craft's home frame; the planner just passes through the ID so the
// HUD can render it correctly.
func TestPlanInclinationPrimaryIDPropagates(t *testing.T) {
	const inc = 28.5 * math.Pi / 180
	state := circularInclinedState(rLEO, inc, muEarth)

	plan, err := PlanInclinationChange(state, muEarth, 0, "earth")
	if err != nil {
		t.Fatalf("PlanInclinationChange: %v", err)
	}
	if plan.PrimaryID != "earth" {
		t.Errorf("PrimaryID = %q, want %q", plan.PrimaryID, "earth")
	}
}
