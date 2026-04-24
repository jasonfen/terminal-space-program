package sim

import (
	"math"
	"sort"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ManeuverNode represents a planned burn that will fire when
// World.Clock.SimTime reaches TriggerTime. Nodes are forward-looking only;
// once fired, they are removed from World.Nodes.
//
// Duration controls finite vs impulsive: zero = instant Δv (legacy v0.1
// path); non-zero = sustained engine burn lasting up to Duration or
// until DV is delivered, whichever first. Finite-burn execution is
// driven by World.ActiveBurn during subsequent ticks.
type ManeuverNode struct {
	TriggerTime time.Time
	Mode        spacecraft.BurnMode
	DV          float64
	Duration    time.Duration
}

// ActiveBurn is the runtime state of an in-progress finite burn. Set by
// executeDueNodes when a node with Duration>0 fires; cleared by the
// integrator when DVRemaining hits zero or SimTime passes EndTime.
type ActiveBurn struct {
	Mode        spacecraft.BurnMode
	DVRemaining float64
	EndTime     time.Time
}

// PlanNode inserts a node into World.Nodes, keeping the slice sorted by
// TriggerTime. Past-dated nodes are allowed — they fire on the next Tick.
func (w *World) PlanNode(n ManeuverNode) {
	w.Nodes = append(w.Nodes, n)
	sort.Slice(w.Nodes, func(i, j int) bool {
		return w.Nodes[i].TriggerTime.Before(w.Nodes[j].TriggerTime)
	})
}

// ClearNodes wipes every pending node.
func (w *World) ClearNodes() { w.Nodes = nil }

// executeDueNodes fires every node whose TriggerTime has passed, applying
// the burn to the spacecraft in order. Called from Tick after sim-time
// advances. Re-entrant: if two nodes fall in the same tick, both fire.
//
// Impulsive nodes (Duration==0) apply their Δv inline and are popped.
// Finite nodes (Duration>0) start an ActiveBurn and are popped; the burn
// then runs across subsequent ticks via the RK4 branch in
// integrateSpacecraft. If a finite burn is already active when a new
// finite node fires, the new one replaces it (last-write-wins; the
// planner UI is responsible for not over-stacking).
func (w *World) executeDueNodes() {
	if w.Craft == nil {
		return
	}
	fired := 0
	for _, n := range w.Nodes {
		if n.TriggerTime.After(w.Clock.SimTime) {
			break
		}
		if n.Duration == 0 {
			w.Craft.ApplyImpulsive(n.Mode, n.DV)
		} else {
			w.ActiveBurn = &ActiveBurn{
				Mode:        n.Mode,
				DVRemaining: n.DV,
				EndTime:     n.TriggerTime.Add(n.Duration),
			}
		}
		fired++
	}
	if fired > 0 {
		w.Nodes = w.Nodes[fired:]
	}
}

// NodeInertialPosition returns the inertial (system-primary-centered)
// position where the node will fire. Forward-integrates the craft state
// from now to the node's trigger time using the same Verlet integrator
// as the live sim, then adds the primary's inertial position.
//
// Returns zero Vec3 if the craft is nil or the node is already past-due.
func (w *World) NodeInertialPosition(n ManeuverNode) orbital.Vec3 {
	if w.Craft == nil {
		return orbital.Vec3{}
	}
	dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	if dt <= 0 {
		return w.CraftInertial()
	}
	state := w.propagateCraft(dt)
	primaryPos := w.BodyPosition(w.Craft.Primary)
	return primaryPos.Add(state.R)
}

// PostBurnState returns the craft's primary-relative state vector
// immediately after the given node would fire. Forward-integrates to the
// trigger time, then applies the Δv in the node's direction mode. Used
// by OrbitView to predict the post-burn trajectory without disturbing
// live state.
func (w *World) PostBurnState(n ManeuverNode) physics.StateVector {
	if w.Craft == nil {
		return physics.StateVector{}
	}
	dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	var state physics.StateVector
	if dt <= 0 {
		state = w.Craft.State
	} else {
		state = w.propagateCraft(dt)
	}
	dir := spacecraft.DirectionUnit(n.Mode, state.R, state.V)
	if dir.Norm() == 0 || n.DV == 0 {
		return state
	}
	state.V = state.V.Add(dir.Scale(n.DV))
	return state
}

// propagateCraft forward-integrates the craft's primary-relative state
// dt seconds into the future without mutating live state. Used by
// NodeInertialPosition and by OrbitView's predicted-trajectory preview.
func (w *World) propagateCraft(dt float64) physics.StateVector {
	mu := w.Craft.Primary.GravitationalParameter()
	period := orbitalPeriod(w.Craft.State, mu)
	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}
	nSteps := int(math.Ceil(dt / maxStep))
	if nSteps < 1 {
		nSteps = 1
	}
	// Cap large propagation windows so a 10-period look-ahead doesn't
	// grind. At 1024 sub-steps over `period` we still resolve each orbit
	// at ≈10× the live-sim fidelity.
	if nSteps > 1024 {
		nSteps = 1024
	}
	step := dt / float64(nSteps)
	state := w.Craft.State
	for i := 0; i < nSteps; i++ {
		state = physics.StepVerlet(state, mu, step)
	}
	return state
}
