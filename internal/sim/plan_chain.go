package sim

import (
	"fmt"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// planChainStep is one planted node as the HUD's chained predictor sees
// it: the state at the node's trigger time and the frame it is in.
type planChainStep struct {
	Index int
	Node  spacecraft.ManeuverNode
	// State/Primary are post-burn when Missed is false, and the pre-burn
	// coast state in the frame the vessel is actually in when Missed.
	State   physics.StateVector
	Primary bodies.CelestialBody
	Clock   time.Time
	// Missed: the node is planted in another body's frame (a capture
	// node) but the predicted path is not inside that body's SOI at the
	// node's trigger time. The chain stops here; nothing past a missed
	// encounter is true.
	Missed bool
}

// frameReached is the one predicate behind every encounter-derived
// readout (NAVIGATION plan:, FRAME TRANSITION, CAPTURE PREVIEW, and the
// → arrows that ride PredictedFinalOrbit): does the predicted path
// actually show an SOI entry into target at clock? True when the path is
// within target's sphere of influence. Always reached, i.e. the label
// is trusted as before: a target that is an ancestor of the current
// frame (a Moon escape marker planted in the parent's frame), one with no
// SOI, and a planet of the system primary. The last is a limit of the
// chained predictor, not a judgement: its heliocentric leg is coarse
// (measured 1.8e9 km off at an Earth-Mars arrival), so it cannot say a
// planetary capture is missed. Only a moon of a planet is gated.
func (w *World) frameReached(state physics.StateVector, primary, target bodies.CelestialBody, clock time.Time) bool {
	if target.ID == primary.ID {
		return true
	}
	sys := w.System()
	for p := sys.ParentOf(primary); p != nil; p = sys.ParentOf(*p) {
		if p.ID == target.ID {
			return true
		}
		if p.ParentID == "" {
			break
		}
	}
	parent := sys.ParentOf(target)
	if parent == nil {
		return true
	}
	if parent.ParentID == "" && sys.Primary() != nil && parent.ID == sys.Primary().ID {
		return true
	}
	rSOI := physics.SOIRadius(target, *parent)
	if rSOI <= 0 {
		return true
	}
	craft := w.BodyPositionAt(primary, clock).Add(state.R)
	return craft.Sub(w.BodyPositionAt(target, clock)).Norm() <= rSOI
}

// planChainCache memoises one chain walk. The walk is the expensive part
// of the HUD (SOI-aware propagation over days) and three readouts ask for
// it every frame; inside a frame they all see the same craft, state, node
// list and clock, so one walk serves them all. Transient, never persisted.
type planChainCache struct {
	valid  bool
	key    string
	steps  []planChainStep
	solves int
}

// PlanChainWalks counts the walks that actually ran (the call count the
// cache test asserts on).
func (w *World) PlanChainWalks() int { return w.planChain.solves }

// planChainKey covers everything the walk reads: the craft (identity),
// the sim clock, the live state, its primary, the active system and the
// whole node list by value.
func (w *World) planChainKey(c *spacecraft.Spacecraft) string {
	return fmt.Sprintf("%p|%d|%d|%v|%s|%v", c, w.SystemIdx, w.Clock.SimTime.UnixNano(), c.State, c.Primary.ID, c.Nodes)
}

// walkPlantedChain walks the active craft's resolved planted nodes in
// trigger-time order (impulsive burns, SOI-aware coast), rebasing into a
// node's frame only when frameReached says the path is there. visit
// returns false to stop early. A missed encounter is visited once with
// Missed set, then the walk ends. The walk is memoised (planChainKey).
func (w *World) walkPlantedChain(visit func(planChainStep) bool) {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	key := w.planChainKey(c)
	if !w.planChain.valid || w.planChain.key != key {
		w.planChain.steps = w.computePlanChain(c)
		w.planChain.key, w.planChain.valid = key, true
		w.planChain.solves++
	}
	for _, st := range w.planChain.steps {
		if !visit(st) {
			return
		}
	}
}

func (w *World) computePlanChain(c *spacecraft.Spacecraft) []planChainStep {
	var steps []planChainStep
	state := c.State
	primary := c.Primary
	clock := w.Clock.SimTime
	for i, n := range c.Nodes {
		if !n.IsResolved() {
			continue
		}
		if dt := n.TriggerTime.Sub(clock).Seconds(); dt > 0 {
			state, primary = w.propagateStateWithPrimary(state, primary, clock, dt)
			clock = n.TriggerTime
		}
		if target, ok := bodies.LookupByID(w.Systems, n.PrimaryID); ok && target.ID != primary.ID {
			if !w.frameReached(state, primary, target, clock) {
				return append(steps, planChainStep{Index: i, Node: n, State: state, Primary: primary, Clock: clock, Missed: true})
			}
			oldInertial := w.BodyPositionAt(primary, clock)
			newInertial := w.BodyPositionAt(target, clock)
			vOld := w.bodyInertialVelocityAt(primary, clock)
			vNew := w.bodyInertialVelocityAt(target, clock)
			state = physics.Rebase(state, oldInertial, newInertial, vOld.Sub(vNew))
			primary = target
		}
		if dir := spacecraft.NodeBurnDirection(n, state.R, state.V); dir.Norm() != 0 && n.DV != 0 {
			state.V = state.V.Add(dir.Scale(n.DV))
		}
		steps = append(steps, planChainStep{Index: i, Node: n, State: state, Primary: primary, Clock: clock})
	}
	return steps
}
