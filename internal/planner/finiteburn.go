package planner

import (
	"errors"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// stdGravity is the standard gravity constant used throughout the
// rocket-equation surface (m/s²). Duplicated locally so the planner
// package stays free of imports from spacecraft (per planner's
// "math/algorithms only" charter — see TransferNode doc).
const stdGravity = 9.80665

// DirectionFn returns the unit vector a burn-mode pushes the craft
// along, given a (r, v) state. Callers wrap their existing direction
// helpers (e.g. spacecraft.DirectionUnit applied to a fixed BurnMode)
// into this signature so the planner never needs to know what
// "prograde" means structurally.
type DirectionFn func(r, v orbital.Vec3) orbital.Vec3

// ResidualFn returns the signed error between a post-burn state and
// the planner's target. Positive when the burn under-delivered (e.g.
// "we want apoapsis higher than this"), negative when overshot. The
// Newton iteration drives this toward zero.
type ResidualFn func(state physics.StateVector, mu float64) float64

// TargetApoapsis builds a ResidualFn whose zero crossing is the
// post-burn orbit's apoapsis matching targetApoMeters (distance from
// primary's centre, not altitude).
func TargetApoapsis(targetApoMeters float64) ResidualFn {
	return func(s physics.StateVector, mu float64) float64 {
		el := orbital.ElementsFromState(s.R, s.V, mu)
		return targetApoMeters - el.Apoapsis()
	}
}

// TargetPeriapsis is the perigee analogue of TargetApoapsis.
func TargetPeriapsis(targetPeriMeters float64) ResidualFn {
	return func(s physics.StateVector, mu float64) float64 {
		el := orbital.ElementsFromState(s.R, s.V, mu)
		return targetPeriMeters - el.Periapsis()
	}
}

// SimulateFiniteBurn forward-integrates a finite burn delivering the
// commanded Δv (m/s) at constant thrust, returning the post-burn
// state. Mass is reduced via the Tsiolkovsky rocket equation;
// duration follows from `dt = m0/mdot · (1 − exp(−Δv / (Isp·g0)))`.
//
// The integration uses physics.StepRK4 with an accel closure that
// adds thrust along direction(r, v) on top of two-body gravity. Mass
// is snapshotted per sub-step (linear within each step) — at 200
// sub-steps the per-step mass error is < 0.5%, well below other
// modelling errors. Sufficient for a planner-side predictor; the
// in-flight integrator does the same trick at finer granularity.
//
// Returns init unchanged when any input is non-positive (degenerate)
// or when commandedDv ≤ 0.
func SimulateFiniteBurn(
	init physics.StateVector,
	mu, thrust, isp, commandedDv float64,
	direction DirectionFn,
) physics.StateVector {
	if commandedDv <= 0 || thrust <= 0 || isp <= 0 || init.M <= 0 || mu <= 0 {
		return init
	}
	mdot := thrust / (isp * stdGravity)
	massFrac := 1 - math.Exp(-commandedDv/(isp*stdGravity))
	dt := init.M * massFrac / mdot
	if dt <= 0 {
		return init
	}
	const nSub = 200
	h := dt / float64(nSub)

	state := init
	for i := 0; i < nSub; i++ {
		massAt := init.M - mdot*float64(i)*h
		if massAt <= 0 {
			break
		}
		accel := thrust / massAt
		accelFn := func(r, v orbital.Vec3, _ float64) orbital.Vec3 {
			grav := physics.Accel(r, mu)
			dir := direction(r, v)
			return grav.Add(dir.Scale(accel))
		}
		state = physics.StepRK4(state, h, accelFn, 0)
	}
	state.M = init.M - mdot*dt
	return state
}

// IterateForTarget Newton-iterates the commanded Δv until the
// residual function evaluates within `tolerance` (in residual units —
// for TargetApoapsis that's metres). Each iteration runs
// SimulateFiniteBurn twice: once at the current guess and once at a
// nudged guess to estimate dResidual/dDv numerically.
//
// Returns the converged commandedDv and the post-burn state. Errors
// out with ErrFiniteBurnDiverged after maxIter without convergence —
// callers should fall back to the impulsive guess in that case
// (still better than no plan).
//
// Use case: v0.5.10's S-IVB-1 default vessel makes finite-burn
// gravity-rotation loss < 0.1% on the Earth → Luna profile, so the
// impulsive Hohmann is "good enough" out of the box. v0.6.2 ships
// this iterator for low-TWR loadouts (revived ICPS, future ion
// stages) where the impulsive guess can mis-deliver apoapsis by
// 20 %+.
func IterateForTarget(
	init physics.StateVector,
	mu, thrust, isp, initialDvGuess float64,
	direction DirectionFn,
	residual ResidualFn,
	tolerance float64,
	maxIter int,
) (float64, physics.StateVector, error) {
	if initialDvGuess <= 0 {
		return 0, init, errors.New("iterateforTarget: initialDvGuess must be > 0")
	}
	if maxIter < 1 {
		maxIter = 1
	}
	dv := initialDvGuess
	var final physics.StateVector
	for i := 0; i < maxIter; i++ {
		final = SimulateFiniteBurn(init, mu, thrust, isp, dv, direction)
		err := residual(final, mu)
		if math.Abs(err) <= tolerance {
			return dv, final, nil
		}
		// Numerical derivative — perturb by 1 % of current guess (or
		// 1 m/s minimum so single-digit guesses still get a stable
		// finite difference).
		eps := math.Max(dv*0.01, 1.0)
		nudged := SimulateFiniteBurn(init, mu, thrust, isp, dv+eps, direction)
		errNudged := residual(nudged, mu)
		dResidual := (errNudged - err) / eps
		if dResidual == 0 || math.IsNaN(dResidual) {
			return dv, final, ErrFiniteBurnDiverged
		}
		// Newton's method root-finding: dv_new = dv − residual / residual'.
		// residual = target − delivered → residual decreases as dv
		// rises (delivering more apo), so dResidual/dDv < 0. With err
		// > 0 (under-delivered) the step is +|err / dResidual| → dv
		// increases, exactly the correction we want.
		step := -err / dResidual
		// Guard against runaway: cap any single step at ±50 % of current dv.
		maxStep := dv * 0.5
		if step > maxStep {
			step = maxStep
		} else if step < -maxStep {
			step = -maxStep
		}
		next := dv + step
		if next <= 0 {
			next = dv * 0.5 // halving guard for negative-bound runaways
		}
		dv = next
	}
	return dv, final, ErrFiniteBurnDiverged
}

// ErrFiniteBurnDiverged means IterateForTarget hit maxIter without
// landing within tolerance, or the numerical derivative collapsed to
// zero. Callers should fall back to the impulsive guess.
var ErrFiniteBurnDiverged = errors.New("planner: finite-burn iteration did not converge")
