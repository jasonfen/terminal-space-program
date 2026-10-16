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
		massAt := init.M - mdot*(float64(i)+0.5)*h
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

// FiniteBurnSolution is what SolveFiniteBurn found: the best commanded
// delta-v it evaluated (smallest |residual|, the initial guess
// included), the post-burn state and residual at that delta-v, how
// many Newton steps it took, and whether the residual met the
// tolerance.
type FiniteBurnSolution struct {
	Dv         float64
	State      physics.StateVector
	Residual   float64
	Iterations int
	Converged  bool
}

// finiteDiffStep is the delta-v nudge (m/s) used for the numerical
// derivative. It is small on purpose: apoapsis against delta-v is
// steeply convex near a transfer to the Moon (a raise from 500 km moves
// Ap 3 to 4 Mm per m/s and the slope itself changes by tens of percent
// over 30 m/s), so the old 1 %-of-dv step (30 m/s at 3 km/s) gave a
// secant slope wrong enough to converge only linearly. The burn model
// is deterministic, so a tenth of a m/s adds no noise.
const finiteDiffStep = 0.1

// SolveFiniteBurn Newton-iterates the commanded delta-v until the
// residual is within `tolerance` (residual units; metres for
// TargetApoapsis). Each iteration runs SimulateFiniteBurn twice: at the
// current guess and at a nudged guess for dResidual/dDv.
//
// It never discards work: on failure (maxIter reached, or the
// derivative collapsed) it still returns the best delta-v it evaluated,
// with Converged false, so the caller decides what an unconverged
// answer means instead of silently reverting to the initial guess.
func SolveFiniteBurn(
	init physics.StateVector,
	mu, thrust, isp, initialDvGuess float64,
	direction DirectionFn,
	residual ResidualFn,
	tolerance float64,
	maxIter int,
) (FiniteBurnSolution, error) {
	if initialDvGuess <= 0 {
		return FiniteBurnSolution{State: init}, errors.New("iterateforTarget: initialDvGuess must be > 0")
	}
	if maxIter < 1 {
		maxIter = 1
	}
	best := FiniteBurnSolution{Residual: math.Inf(1)}
	consider := func(dv float64, st physics.StateVector, res float64) {
		if math.Abs(res) < math.Abs(best.Residual) {
			best = FiniteBurnSolution{Dv: dv, State: st, Residual: res}
		}
	}
	dv := initialDvGuess
	for i := 0; i < maxIter; i++ {
		final := SimulateFiniteBurn(init, mu, thrust, isp, dv, direction)
		err := residual(final, mu)
		consider(dv, final, err)
		if math.Abs(err) <= tolerance {
			best.Iterations, best.Converged = i, true
			return best, nil
		}
		nudged := SimulateFiniteBurn(init, mu, thrust, isp, dv+finiteDiffStep, direction)
		dResidual := (residual(nudged, mu) - err) / finiteDiffStep
		if dResidual == 0 || math.IsNaN(dResidual) {
			best.Iterations = i
			return best, ErrFiniteBurnDiverged
		}
		// Newton: dv_new = dv - residual/residual'. residual = target -
		// delivered, so dResidual/dDv < 0 and an under-delivering burn
		// (err > 0) steps up.
		step := -err / dResidual
		// Guard against runaway: cap any single step at +-50 % of dv.
		maxStep := dv * 0.5
		if step > maxStep {
			step = maxStep
		} else if step < -maxStep {
			step = -maxStep
		}
		// dv stays in [0.5*dv, 1.5*dv] and enters strictly positive.
		dv += step
	}
	// The last Newton step was never evaluated; do it so a solution
	// that landed on the final step is not thrown away.
	final := SimulateFiniteBurn(init, mu, thrust, isp, dv, direction)
	err := residual(final, mu)
	consider(dv, final, err)
	best.Iterations = maxIter
	if math.Abs(err) <= tolerance {
		best.Converged = true
		return best, nil
	}
	return best, ErrFiniteBurnDiverged
}

// IterateForTarget is SolveFiniteBurn returning the historical
// (dv, state, error) triple. On ErrFiniteBurnDiverged the delta-v is
// the best one evaluated, not the last iterate.
func IterateForTarget(
	init physics.StateVector,
	mu, thrust, isp, initialDvGuess float64,
	direction DirectionFn,
	residual ResidualFn,
	tolerance float64,
	maxIter int,
) (float64, physics.StateVector, error) {
	sol, err := SolveFiniteBurn(init, mu, thrust, isp, initialDvGuess, direction, residual, tolerance, maxIter)
	return sol.Dv, sol.State, err
}

// ErrFiniteBurnDiverged means IterateForTarget hit maxIter without
// landing within tolerance, or the numerical derivative collapsed to
// zero. Callers should fall back to the impulsive guess.
var ErrFiniteBurnDiverged = errors.New("planner: finite-burn iteration did not converge")
