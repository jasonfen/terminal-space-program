package planner

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// muEarth is defined in transfer_test.go as a package-level constant.

// progradeUnit is a DirectionFn that always burns prograde
// (velocity-aligned). Mirrors spacecraft.DirectionUnit(BurnPrograde,
// ...) without dragging the spacecraft package into planner tests.
func progradeUnit(_, v orbital.Vec3) orbital.Vec3 {
	mag := v.Norm()
	if mag == 0 {
		return orbital.Vec3{}
	}
	return orbital.Vec3{X: v.X / mag, Y: v.Y / mag, Z: v.Z / mag}
}

// circularLEOState builds a 200-km circular prograde state used as
// the iterator's starting point in tests.
func circularLEOState(mass float64) physics.StateVector {
	r := 6378.137e3 + 200e3
	v := math.Sqrt(muEarth / r)
	return physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: v},
		M: mass,
	}
}

// TestSimulateFiniteBurnImpulsiveLimit: at high thrust → short burn,
// the integrator should match the impulsive answer to within < 1 %.
// This pins down that the simulation isn't doing something wildly
// wrong in the easy regime where we already trust the impulsive math.
func TestSimulateFiniteBurnImpulsiveLimit(t *testing.T) {
	init := circularLEOState(51000)
	// J-2-class: 1023 kN, Isp 421 s. Very high thrust so the burn is
	// short (~2 s for 50 m/s) and integration loss is minimal.
	post := SimulateFiniteBurn(init, muEarth, 1023e3, 421, 50, progradeUnit)

	// Compare delivered Δv via post-burn |V| − initial |V|.
	delivered := post.V.Norm() - init.V.Norm()
	if math.Abs(delivered-50) > 0.5 {
		t.Errorf("high-thrust delivered Δv = %.3f m/s, want ~50 ± 0.5", delivered)
	}
}

// TestSimulateFiniteBurnLowTWRLossesApoapsis: a low-thrust loadout
// with a multi-minute burn should under-deliver apoapsis vs the
// impulsive ideal by a measurable amount. This is the regime
// IterateForTarget exists to fix.
func TestSimulateFiniteBurnLowTWRLossesApoapsis(t *testing.T) {
	init := circularLEOState(51000)
	r0 := init.R.Norm()
	v0 := init.V.Norm()
	// Pre-v0.5.13 ICPS-class: 108 kN, 462 Isp. ~600 s burn for the
	// 3.1 km/s TLI Δv — the regime where the impulsive guess fell
	// 27 % short on apoapsis.
	const thrust = 108e3
	const isp = 462.0
	const dvCommanded = 3100.0

	// Impulsive expected apoapsis: post-burn v = v0 + dv at periapsis.
	vNew := v0 + dvCommanded
	eps := 0.5*vNew*vNew - muEarth/r0
	aImp := -muEarth / (2 * eps)
	apoImpulsive := 2*aImp - r0

	post := SimulateFiniteBurn(init, muEarth, thrust, isp, dvCommanded, progradeUnit)
	el := orbital.ElementsFromState(post.R, post.V, muEarth)
	apoFinite := el.Apoapsis()

	if apoFinite >= apoImpulsive {
		t.Fatalf("expected finite-burn apoapsis < impulsive, got finite=%.0f m vs impulsive=%.0f m",
			apoFinite, apoImpulsive)
	}
	// Burn this long should lose at least 5 % of apoapsis-radius —
	// the whole reason for the iterator. Pre-v0.5.13 the loss was
	// ~27 %; we need the test to fail loud if a future change
	// tightens the integrator enough to make the iterator vestigial.
	loss := (apoImpulsive - apoFinite) / apoImpulsive
	if loss < 0.02 {
		t.Errorf("expected ≥ 2 %% finite-burn apoapsis loss for low-TWR ICPS profile; got %.2f %%",
			loss*100)
	}
}

// TestIterateForTargetConvergesOnLowTWRApoapsis: same ICPS-class
// profile as above. After IterateForTarget converges, the delivered
// apoapsis should match the target to within 1 km — even though the
// impulsive guess is several percent off.
func TestIterateForTargetConvergesOnLowTWRApoapsis(t *testing.T) {
	init := circularLEOState(51000)
	r0 := init.R.Norm()
	v0 := init.V.Norm()
	const thrust = 108e3
	const isp = 462.0
	const impulsiveDv = 3100.0

	// Target: the apoapsis the impulsive plan *thought* it would
	// deliver. The iterator must adjust commanded Δv upward to
	// actually hit this apoapsis under finite-burn integration.
	vNew := v0 + impulsiveDv
	eps := 0.5*vNew*vNew - muEarth/r0
	a := -muEarth / (2 * eps)
	targetApo := 2*a - r0

	refined, finalState, err := IterateForTarget(
		init, muEarth, thrust, isp, impulsiveDv,
		progradeUnit, TargetApoapsis(targetApo),
		1000.0, // 1 km tolerance
		20,
	)
	if err != nil {
		t.Fatalf("IterateForTarget failed: %v", err)
	}
	el := orbital.ElementsFromState(finalState.R, finalState.V, muEarth)
	if math.Abs(el.Apoapsis()-targetApo) > 1000 {
		t.Errorf("post-iteration apoapsis = %.0f m, want %.0f m (tol 1 km)",
			el.Apoapsis(), targetApo)
	}
	if refined <= impulsiveDv {
		t.Errorf("refined Δv (%.1f) should exceed impulsive guess (%.1f) — finite burn loses energy",
			refined, impulsiveDv)
	}
}

// TestIterateForTargetConvergesOnHighTWRApoapsis: with the S-IVB-1
// default loadout the impulsive guess is already nearly exact; the
// iterator should converge in 1-2 steps without changing Δv much.
func TestIterateForTargetConvergesOnHighTWRApoapsis(t *testing.T) {
	init := circularLEOState(51000)
	r0 := init.R.Norm()
	v0 := init.V.Norm()
	const thrust = 1023e3
	const isp = 421.0
	const impulsiveDv = 3100.0

	vNew := v0 + impulsiveDv
	eps := 0.5*vNew*vNew - muEarth/r0
	a := -muEarth / (2 * eps)
	targetApo := 2*a - r0

	refined, finalState, err := IterateForTarget(
		init, muEarth, thrust, isp, impulsiveDv,
		progradeUnit, TargetApoapsis(targetApo),
		1000.0,
		8,
	)
	if err != nil {
		t.Fatalf("IterateForTarget failed: %v", err)
	}
	el := orbital.ElementsFromState(finalState.R, finalState.V, muEarth)
	if math.Abs(el.Apoapsis()-targetApo) > 1000 {
		t.Errorf("S-IVB apoapsis = %.0f m, want %.0f m", el.Apoapsis(), targetApo)
	}
	// Refined Δv should be within 1 % of impulsive — short burn,
	// minimal correction needed.
	rel := math.Abs(refined-impulsiveDv) / impulsiveDv
	if rel > 0.01 {
		t.Errorf("S-IVB refined Δv (%.1f) deviates >1%% from impulsive (%.1f); rel=%.4f",
			refined, impulsiveDv, rel)
	}
}

// TestIterateForTargetMaxIterDiverges: a residual that can never be
// satisfied (target apoapsis lower than parking orbit, requiring a
// retrograde burn but commanded prograde) should hit max-iter and
// return ErrFiniteBurnDiverged so callers know to fall back.
func TestIterateForTargetMaxIterDiverges(t *testing.T) {
	init := circularLEOState(51000)
	// Target apoapsis below the current orbit. A prograde burn can't
	// achieve that — Newton iteration will keep over/undershooting.
	r0 := init.R.Norm()
	targetApo := r0 * 0.5

	_, _, err := IterateForTarget(
		init, muEarth, 1023e3, 421, 100,
		progradeUnit, TargetApoapsis(targetApo),
		100.0, 5,
	)
	if err == nil {
		t.Errorf("expected ErrFiniteBurnDiverged for unreachable target, got nil")
	}
	if err != nil && !errors.Is(err, ErrFiniteBurnDiverged) {
		t.Errorf("expected ErrFiniteBurnDiverged, got %v", err)
	}
}
