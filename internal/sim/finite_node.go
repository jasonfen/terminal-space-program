package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// nodeBurnState is the state a planted node leaves the vessel in,
// expressed at the node's own epoch, for every predictor that walks the
// planted slate (PredictedLegs, the chained readouts, the post-burn
// preview).
//
// A finite node is flown as a burn centred on its trigger time, so the
// vessel ends on the conic it would reach by burning for Duration, not
// on the impulsive one. The two differ little for a short burn and a lot
// for a long one near a transfer: on the 500 km Comsat Carrier x3's
// 10.5 minute Moon departure the refined delta-v, applied as an impulse,
// predicts Ap 597 Mm for a burn that flies to Ap 392 Mm (Ap moves 3 to 4
// Mm per m/s there). #666 made the planted delta-v the finite-corrected
// one, so the predictors must fly it as a finite burn too.
//
// The finite state is the burn simulated from half a burn before the
// node (planner.SimulateFiniteBurn, the model refineFiniteDeparture
// solves with) and carried back to the node epoch along its own orbit,
// so everything downstream that treats the result as "post-burn at the
// trigger time" is unchanged. Impulsive nodes, target-relative and
// plane-change modes, a vessel with no thrust data, and states that are
// not on a bound orbit keep the impulsive application.
func (w *World) nodeBurnState(state physics.StateVector, primary bodies.CelestialBody, n spacecraft.ManeuverNode, dir orbital.Vec3) physics.StateVector {
	impulsive := state
	if dir.Norm() != 0 && n.DV != 0 {
		impulsive.V = state.V.Add(dir.Scale(n.DV))
	}
	c := w.ActiveCraft()
	if c == nil {
		return impulsive
	}
	return finiteNodeState(state, primary.GravitationalParameter(), n, nodeThrust(c, n), c.Isp, impulsive)
}

// nodeThrust is the thrust the executor delivers while firing n: the lit set
// at the node's throttle, with the player's booster throttle ignored as
// stepThrust ignores it for an ActiveBurn (#691).
func nodeThrust(c *spacecraft.Spacecraft, n spacecraft.ManeuverNode) float64 {
	saved := c.BoosterThrottle
	c.BoosterThrottle = nil
	defer func() { c.BoosterThrottle = saved }()
	return c.Thrust * c.LitThrottle(n.EffectiveThrottle())
}

// finiteNodeState is nodeBurnState's burn model with the engine passed in,
// so the Moon-transfer planner (splitCaptureAim) measures the plane change
// with the very model the chained readouts and the executor use. impulsive
// is the answer for nodes that stay impulsive.
//
// The burn runs from half the node's Duration before its trigger time (the
// executor's BurnStart) until the Δv is delivered, then the result is
// carried to the node epoch along its own orbit. Prograde and retrograde
// follow the velocity (the burn re-aims every sub-step, as the nose follows
// it in flight); a mode with a normal or radial component holds, for the
// whole burn, the direction the node gives at its own epoch
// (spacecraft.HoldsInertialDirection, #702; the executor resolves it once
// at ignition from the state it will have at that epoch, heldIgnitionDir),
// and a BurnVector node its captured direction.
func finiteNodeState(state physics.StateVector, mu float64, n spacecraft.ManeuverNode, thrust, isp float64, impulsive physics.StateVector) physics.StateVector {
	if n.DV <= 0 || n.Duration <= 0 || thrust <= 0 || isp <= 0 || state.M <= 0 {
		return impulsive
	}
	const g0 = 9.80665
	half := n.Duration.Seconds() / 2
	start, ok := physics.KeplerStep(state, mu, -half)
	if !ok {
		return impulsive
	}
	var direction planner.DirectionFn
	switch {
	case spacecraft.HoldsInertialDirection(n.Mode):
		fixed := spacecraft.NodeBurnDirection(n, state.R, state.V)
		if fixed.Norm() == 0 {
			return impulsive
		}
		direction = func(_, _ orbital.Vec3) orbital.Vec3 { return fixed }
	case n.Mode == spacecraft.BurnPrograde, n.Mode == spacecraft.BurnRetrograde:
		mode := n.Mode
		direction = func(r, v orbital.Vec3) orbital.Vec3 { return spacecraft.DirectionUnit(mode, r, v) }
	case n.Mode == spacecraft.BurnVector:
		fixed := n.BurnDirUnit.Unit()
		direction = func(_, _ orbital.Vec3) orbital.Vec3 { return fixed }
	default:
		return impulsive
	}
	fin := planner.SimulateFiniteBurn(start, mu, thrust, isp, n.DV, direction)
	burnSecs := start.M * (1 - math.Exp(-n.DV/(isp*g0))) / (thrust / (isp * g0))
	post, ok := physics.KeplerStep(fin, mu, half-burnSecs)
	if !ok {
		return impulsive
	}
	post.M = fin.M
	return post
}

// errDepartureBurnOffAim: the finite fixed-direction departure burn
// cannot be sized to reach the aimed transfer point. Facts only.
var errDepartureBurnOffAim = transferError("departure burn does not reach the aimed transfer")

const (
	fusedRefineMaxIter = 20
	fusedRefineTol     = 5000.0 // m at the arrival point; about 1.5 mm/s of delta-v on a lunar transfer
	fusedRefineMissMax = 0.01   // of the arrival distance
)

// refineFusedDeparture is refineFiniteDeparture for the combined
// (fused-Lambert) plan. Its departure is a BurnVector: a fixed inertial
// direction, so a long burn does not follow the velocity round the orbit
// and delivers its delta-v less usefully than the impulse the Lambert
// solve assumed (on the Comsat Carrier x3 at TWR 0.42 a 10.5 minute burn
// planted at the Lambert delta-v reached Ap 232 Mm against a Moon at
// 390 Mm, #666).
//
// The unknown is the whole burn vector u (magnitude and direction). The
// residual is where the burn's own conic puts the vessel at the Lambert
// arrival epoch, minus where the impulsive Lambert departure puts it:
// the burn is simulated as the centred finite burn (planner.
// SimulateFiniteBurn from half a burn before the node), carried to the
// arrival epoch by Kepler, and u is Newton-iterated on a numerical 3x3
// Jacobian. The plan's DV and BurnDir are replaced by the solution. When
// the solution misses the arrival point by more than fusedRefineMissMax
// of its distance the plan is refused, never planted as if refined.
func refineFusedDeparture(plan *planner.TransferPlan, dep physics.StateVector, mu, mass, thrust, isp float64) error {
	if thrust <= 0 || mass <= 0 || isp <= 0 || plan.Departure.DV <= 0 || plan.Departure.BurnDir.Norm() == 0 {
		return nil
	}
	const g0 = 9.80665
	tof := plan.TransferDt.Seconds()
	dep.M = mass
	ideal := dep
	ideal.V = dep.V.Add(plan.Departure.BurnDir.Unit().Scale(plan.Departure.DV))
	aimState, ok := physics.KeplerStep(ideal, mu, tof)
	if !ok {
		return errDepartureBurnOffAim
	}
	aim := aimState.R
	miss := func(u orbital.Vec3) (orbital.Vec3, bool) {
		dv := u.Norm()
		if dv <= 0 {
			return orbital.Vec3{}, false
		}
		dir := u.Scale(1 / dv)
		half := mass * (1 - math.Exp(-dv/(isp*g0))) / (thrust / (isp * g0)) / 2
		start, ok := physics.KeplerStep(dep, mu, -half)
		if !ok {
			return orbital.Vec3{}, false
		}
		fin := planner.SimulateFiniteBurn(start, mu, thrust, isp, dv, func(_, _ orbital.Vec3) orbital.Vec3 { return dir })
		end, ok := physics.KeplerStep(fin, mu, tof-half)
		if !ok {
			return orbital.Vec3{}, false
		}
		return end.R.Sub(aim), true
	}
	u := plan.Departure.BurnDir.Unit().Scale(plan.Departure.DV)
	best, bestMiss := u, math.Inf(1)
	f, ok := miss(u)
	if ok {
		bestMiss = f.Norm()
	}
	for i := 0; ok && i < fusedRefineMaxIter && f.Norm() > fusedRefineTol; i++ {
		// Numerical Jacobian, one column per axis.
		const h = 0.1
		var jac [3][3]float64
		for k := 0; k < 3 && ok; k++ {
			du := orbital.Vec3{}
			switch k {
			case 0:
				du.X = h
			case 1:
				du.Y = h
			default:
				du.Z = h
			}
			var fk orbital.Vec3
			if fk, ok = miss(u.Add(du)); ok {
				d := fk.Sub(f).Scale(1 / h)
				jac[0][k], jac[1][k], jac[2][k] = d.X, d.Y, d.Z
			}
		}
		if !ok {
			break
		}
		step, okS := solve3(jac, orbital.Vec3{X: -f.X, Y: -f.Y, Z: -f.Z})
		if !okS {
			break
		}
		if lim := 0.5 * u.Norm(); step.Norm() > lim {
			step = step.Scale(lim / step.Norm())
		}
		// The first miss is hundreds of Mm (the fixed-direction burn
		// starts well off the Lambert arc), far outside the linear
		// range, and a full step can leave the bound orbit altogether.
		// Backtrack until the miss shrinks.
		accepted := false
		for bt := 0; bt < 8; bt++ {
			fn, okN := miss(u.Add(step))
			if okN && fn.Norm() < f.Norm() {
				u, f, accepted = u.Add(step), fn, true
				break
			}
			step = step.Scale(0.5)
		}
		if !accepted {
			break
		}
		if m := f.Norm(); m < bestMiss {
			best, bestMiss = u, m
		}
	}
	if math.IsInf(bestMiss, 1) || bestMiss > fusedRefineMissMax*aim.Norm() {
		return errDepartureBurnOffAim
	}
	plan.Departure.DV = best.Norm()
	plan.Departure.BurnDir = best.Unit()
	return nil
}

// solve3 solves the 3x3 system a·x = b by Cramer's rule; ok=false when a
// is singular.
func solve3(a [3][3]float64, b orbital.Vec3) (orbital.Vec3, bool) {
	det := func(m [3][3]float64) float64 {
		return m[0][0]*(m[1][1]*m[2][2]-m[1][2]*m[2][1]) -
			m[0][1]*(m[1][0]*m[2][2]-m[1][2]*m[2][0]) +
			m[0][2]*(m[1][0]*m[2][1]-m[1][1]*m[2][0])
	}
	d := det(a)
	if d == 0 || math.IsNaN(d) {
		return orbital.Vec3{}, false
	}
	col := func(k int, v [3]float64) [3][3]float64 {
		m := a
		for r := 0; r < 3; r++ {
			m[r][k] = v[r]
		}
		return m
	}
	bv := [3]float64{b.X, b.Y, b.Z}
	return orbital.Vec3{X: det(col(0, bv)) / d, Y: det(col(1, bv)) / d, Z: det(col(2, bv)) / d}, true
}
