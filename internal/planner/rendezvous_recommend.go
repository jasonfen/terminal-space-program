package planner

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// AxisLabel identifies which of the eight velocity-frame burn axes
// RecommendRendezvousNudge picked. The sim layer (which does have
// the spacecraft package in scope) maps this to a spacecraft.BurnMode
// before plant. Kept here as a planner-local enum so this file stays
// dependency-clean (planner is a sibling of spacecraft — neither
// imports the other; see CLAUDE.md "Architecture").
//
// Order matches spacecraft.AllBurnModes for the eight non-position
// modes. The two position-relative modes (BurnTarget / BurnAntiTarget)
// are intentionally excluded — Lambert's Δv is a velocity correction;
// a position-axis pick would be physically unjustified. v0.10.2+.
type AxisLabel int

const (
	AxisPrograde AxisLabel = iota
	AxisRetrograde
	AxisNormalPlus
	AxisNormalMinus
	AxisRadialOut
	AxisRadialIn
	AxisTargetPrograde
	AxisTargetRetrograde
)

// String labels match the spacecraft.BurnMode.String() canonical names
// so HUD callers don't need a parallel naming table.
func (a AxisLabel) String() string {
	switch a {
	case AxisPrograde:
		return "Prograde"
	case AxisRetrograde:
		return "Retrograde"
	case AxisNormalPlus:
		return "Normal+"
	case AxisNormalMinus:
		return "Normal-"
	case AxisRadialOut:
		return "Radial+"
	case AxisRadialIn:
		return "Radial-"
	case AxisTargetPrograde:
		return "Target Prograde"
	case AxisTargetRetrograde:
		return "Target Retrograde"
	}
	return "?"
}

// RendezvousAdvisory is the result of a single-burn nudge
// recommendation. v0.10.2+.
//
// Ok=true: DV / Axis / AxisUnit / AchievableCA / TArrival populated; a
// plant-side caller can build the maneuver node directly from these
// fields.
//
// Ok=false: Reason carries a short tag for the gate that fired. The
// HUD surfaces the "no improvement available" tag specifically; other
// reasons mean the advisory block is hidden (the existing TARGET HUD
// readouts already convey state).
type RendezvousAdvisory struct {
	Ok       bool
	DV       float64      // scalar Δv along AxisUnit, m/s
	Axis     AxisLabel    // discrete pick from the eight velocity-frame axes
	AxisUnit orbital.Vec3 // unit vector for the recommended axis (in same frame as stateA)

	CurrentCA    float64 // m — what the player would get with no burn
	AchievableCA float64 // m — what the recommended burn delivers
	TArrival     float64 // s — time-to-CA from now after the burn

	LambertIdealDV float64 // m/s — |full Lambert ΔV| (always ≥ DV; gap shows projection loss)

	Reason string // populated when Ok=false: "no improvement available" | "no lambert convergence" | "degenerate axes" | "horizon too short" | "burn too large — use H/I/m" | "burn drops periapsis unsafely"
}

// RecommendRendezvousNudge picks a single-burn nudge that brings the
// chaser (stateA) closer to the target (stateB) at a future closest
// approach, given they share a primary with gravitational parameter
// mu. currentCA is the no-burn closest-approach distance the caller
// already has on the HUD (typically from NextClosestApproach); the
// function uses it for the two-prong improvement floor.
//
// Algorithm (see designdocs/terminal-space-program/v0.10-plan.md §v0.10.2 / plan file):
//
//  1. Scan Lambert intercept solutions at T_k = {0.15, 0.3, 0.5, 0.8,
//     1.2}·P_B; pick the lookahead that minimises |Δv_full|.
//  2. Project Δv_full onto the eight velocity-frame axes; pick the
//     axis with the largest positive projection (scalar Δv ≥ 0).
//  3. Re-run NextClosestApproach with the axis-aligned perturbation
//     applied — this is the *honest* post-burn CA, not the Lambert
//     ideal (the projection is lossy by design; the slice's loop is
//     "iterate until DOCK READY").
//  4. Two-prong improvement floor:
//     (CA_improvement ≥ 10 %) OR (Δv ≥ 0.5 m/s AND
//     CA_improvement ≥ 100 m absolute). Fails the gate ⇒ Ok=false.
//  5. Nudge-scale ceiling (v0.10.3+): bestProj ≤ maxNudgeDV — single-
//     burn recommendations above this aren't "nudges", they're major
//     orbit-shape changes that belong in the manual planner (H / I /
//     m). Without this ceiling the gate would happily plant a 1.7
//     km/s K-burn whenever it improved CA by ≥10 %, because the
//     Lambert lookahead fan converges on whatever transfer fits T_k
//     even when the orbits are wildly mismatched.
//  6. Orbit-safety gate (v0.10.3+): the projection in Step 2 is
//     lossy — the perturbed orbit is NOT the Lambert transfer, just
//     the chaser's orbit + a scalar push in one axis. A large
//     retrograde or radial-in nudge can drop the chaser's periapsis
//     into the atmosphere while still nominally "improving CA."
//     Reject burns that put post-periapsis below primary+50 km or
//     drop it by more than 100 km from pre-burn.
//
// Caller-side gates (no target, target == active, different
// primaries, already DOCK READY) live in the sim layer; the planner
// is not exercised on those paths.
func RecommendRendezvousNudge(
	stateA, stateB orbital.Vec3State,
	primary bodies.CelestialBody,
	mu, horizon, currentCA float64,
) RendezvousAdvisory {
	_ = primary // captured for symmetry with NextClosestApproach; future cross-frame work

	out := RendezvousAdvisory{CurrentCA: currentCA}

	if mu <= 0 || horizon <= 0 {
		out.Reason = "horizon too short"
		return out
	}

	axes := buildVelocityFrameAxes(stateA, stateB)
	if len(axes) == 0 {
		out.Reason = "degenerate axes"
		return out
	}

	// Step 1: Lambert lookahead scan.
	pB := orbitalPeriod(physics.StateVector{R: stateB.R, V: stateB.V}, mu)
	if math.IsInf(pB, 0) || pB <= 0 {
		out.Reason = "horizon too short"
		return out
	}
	lookaheads := []float64{0.15, 0.3, 0.5, 0.8, 1.2}
	var bestDV orbital.Vec3
	var bestT float64
	bestMag := math.Inf(1)
	for _, k := range lookaheads {
		T := k * pB
		if T <= 0 || T > horizon {
			continue
		}
		stateBatT := propagateStateVerlet(stateB, mu, T)
		v1, _, err := LambertSolve(stateA.R, stateBatT.R, T, mu, false)
		if err != nil {
			continue
		}
		dv := v1.Sub(stateA.V)
		m := dv.Norm()
		if math.IsNaN(m) || math.IsInf(m, 0) {
			continue
		}
		if m < bestMag {
			bestMag = m
			bestDV = dv
			bestT = T
		}
	}
	if math.IsInf(bestMag, 0) {
		out.Reason = "no lambert convergence"
		return out
	}
	_ = bestT
	out.LambertIdealDV = bestMag

	// Step 2: axis projection. Pick axis with the largest positive
	// projection onto bestDV. Equivalent to maximising the
	// along-axis component magnitude when both signs of each axis
	// pair are in the map (which they are).
	var bestAxis AxisLabel
	var bestAxisUnit orbital.Vec3
	bestProj := 0.0
	found := false
	for _, label := range allAxisLabels {
		axisUnit, ok := axes[label]
		if !ok {
			continue
		}
		proj := bestDV.Dot(axisUnit)
		if proj > bestProj {
			bestProj = proj
			bestAxis = label
			bestAxisUnit = axisUnit
			found = true
		}
	}
	if !found {
		// All projections ≤ 0 — bestDV is a zero vector or the
		// degenerate axis set didn't include the right direction.
		out.Reason = "degenerate axes"
		return out
	}

	// Step 3: achievable-CA verification. Apply the axis-aligned
	// scalar Δv to stateA and re-run NextClosestApproach for the
	// honest post-burn number.
	perturbed := orbital.Vec3State{
		R: stateA.R,
		V: stateA.V.Add(bestAxisUnit.Scale(bestProj)),
	}
	tStar, caStar, _, err := NextClosestApproach(perturbed, stateB, primary, mu, horizon)
	if err != nil {
		out.Reason = "ca-verify failed"
		return out
	}

	// Step 4: two-prong improvement floor.
	improvement := currentCA - caStar
	relImprovement := 0.0
	if currentCA > 0 {
		relImprovement = improvement / currentCA
	}
	twoProng := (relImprovement >= 0.10) || (bestProj >= 0.5 && improvement >= 100.0)
	if caStar >= currentCA || !twoProng {
		out.AchievableCA = caStar
		out.TArrival = tStar
		out.DV = bestProj
		out.Axis = bestAxis
		out.AxisUnit = bestAxisUnit
		out.Reason = "no improvement available"
		return out
	}

	// Step 5: nudge-scale ceiling. Above maxNudgeDV the recommendation
	// isn't a "nudge", it's an orbit-shape change the player should
	// plan deliberately (H / I / m). Hide rather than plant.
	if bestProj > maxNudgeDV {
		out.AchievableCA = caStar
		out.TArrival = tStar
		out.DV = bestProj
		out.Axis = bestAxis
		out.AxisUnit = bestAxisUnit
		out.Reason = "burn too large — use H/I/m"
		return out
	}

	// Step 6 (v0.10.3+): orbit-safety gate. The projection step in
	// Step 2 applies the Lambert Δv along a single axis, so the
	// perturbed orbit is NOT the Lambert transfer orbit — it's the
	// chaser's existing orbit pushed once in one direction. A large
	// retrograde nudge can drop periapsis far below atmosphere while
	// still "improving" CA (the chaser sweeps past the target on a
	// re-entry arc). Reject the burn if the post-burn periapsis
	// either falls below a hard floor (primary surface + 50 km) or
	// drops more than 100 km from the pre-burn periapsis. Hard floor
	// uses primary.RadiusMeters() when available; with a zero-radius
	// primary (tests) only the relative-drop check fires. Without
	// this gate, a K-plant has been observed deorbiting the chaser
	// to a 25 km periapsis while ostensibly improving CA.
	prePeri := orbital.ElementsFromState(stateA.R, stateA.V, mu).Periapsis()
	postPeri := orbital.ElementsFromState(perturbed.R, perturbed.V, mu).Periapsis()
	periSurfaceFloor := primary.RadiusMeters() + 50_000.0
	periDropLimit := prePeri - 100_000.0
	if (primary.RadiusMeters() > 0 && postPeri < periSurfaceFloor) || postPeri < periDropLimit {
		out.AchievableCA = caStar
		out.TArrival = tStar
		out.DV = bestProj
		out.Axis = bestAxis
		out.AxisUnit = bestAxisUnit
		out.Reason = "burn drops periapsis unsafely"
		return out
	}

	out.Ok = true
	out.DV = bestProj
	out.Axis = bestAxis
	out.AxisUnit = bestAxisUnit
	out.AchievableCA = caStar
	out.TArrival = tStar
	return out
}

// maxNudgeDV is the ceiling for a single-burn rendezvous nudge
// recommendation. Calibrated v0.10.3+ after a 1.7-km/s K-plant was
// observed for chaser/target in mismatched orbits (the 10 %
// CA-improvement gate alone wasn't enough — Lambert happily returned
// a fast-transfer solution that "improved" CA but was effectively a
// full orbit-change burn). Tuned to cover small phasing burns and
// modest plane corrections; major orbit-shape changes belong in the
// H / I / m planners. Const, not a knob — the player intent is
// "small nudge to refine an already-close intercept."
const maxNudgeDV = 300.0 // m/s

var allAxisLabels = []AxisLabel{
	AxisPrograde,
	AxisRetrograde,
	AxisNormalPlus,
	AxisNormalMinus,
	AxisRadialOut,
	AxisRadialIn,
	AxisTargetPrograde,
	AxisTargetRetrograde,
}

// buildVelocityFrameAxes constructs unit vectors for the eight
// velocity-frame burn axes from chaser + target states in a common
// frame. Skips degenerate axes (e.g. target-prograde when v_rel is
// zero) — the projection loop simply doesn't consider them.
func buildVelocityFrameAxes(stateA, stateB orbital.Vec3State) map[AxisLabel]orbital.Vec3 {
	axes := make(map[AxisLabel]orbital.Vec3, 8)

	rMag := stateA.R.Norm()
	vMag := stateA.V.Norm()
	if rMag > 0 && vMag > 0 {
		prograde := stateA.V.Scale(1 / vMag)
		radialOut := stateA.R.Scale(1 / rMag)
		axes[AxisPrograde] = prograde
		axes[AxisRetrograde] = prograde.Scale(-1)
		axes[AxisRadialOut] = radialOut
		axes[AxisRadialIn] = radialOut.Scale(-1)

		h := stateA.R.Cross(stateA.V)
		if hMag := h.Norm(); hMag > 0 {
			normal := h.Scale(1 / hMag)
			axes[AxisNormalPlus] = normal
			axes[AxisNormalMinus] = normal.Scale(-1)
		}
	}

	// KSP convention: target-prograde = unit(v_active − v_target)
	// (chaser's motion relative to target); target-retrograde =
	// unit(v_target − v_active) (the closing / null-v_rel axis).
	vRel := stateA.V.Sub(stateB.V)
	if m := vRel.Norm(); m > 0 {
		tp := vRel.Scale(1 / m)
		axes[AxisTargetPrograde] = tp
		axes[AxisTargetRetrograde] = tp.Scale(-1)
	}

	return axes
}

// propagateStateVerlet steps a state forward by T seconds using
// Verlet integration with a period/200 substep (matches
// NextClosestApproach's stability budget). For Lambert lookahead
// fans, T is fractional-period so this is bounded: 0.15·P at
// period/200 substep = ~30 sub-steps.
func propagateStateVerlet(s orbital.Vec3State, mu, T float64) orbital.Vec3State {
	sv := physics.StateVector{R: s.R, V: s.V}
	period := orbitalPeriod(sv, mu)
	var subStep float64
	if math.IsInf(period, 0) || period <= 0 {
		subStep = T / 100
	} else {
		subStep = period / 200
	}
	if subStep <= 0 || subStep > T/4 {
		subStep = T / 4
	}
	if subStep <= 0 {
		return s
	}
	n := int(math.Ceil(T / subStep))
	if n < 4 {
		n = 4
	}
	dt := T / float64(n)
	for i := 0; i < n; i++ {
		sv = physics.StepVerlet(sv, mu, dt)
	}
	return orbital.Vec3State{R: sv.R, V: sv.V}
}
