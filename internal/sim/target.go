package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TargetKind enumerates what World.Target points at. v0.9.0+ unifies
// the implicit body cursor (read by `H` PlanTransfer and `I`
// PlanInclinationChange pre-v0.9) and the rendezvous-introduced
// target-craft idx (planned for v0.9.3) into a single slot. Every
// planner that needs to ask "what is the player aiming at?" reads
// the same field.
//
// TargetSite is reserved for landing-site targeting, slated for
// post-v0.9 ground-ops work; populating it is a no-op until that
// surface ships.
type TargetKind int

const (
	// TargetNone — no target set. Planners that consume Target fall
	// back to their kind-less default (equatorial inclination match,
	// "pick a body cursor first" status flash).
	TargetNone TargetKind = iota
	// TargetBody references a body by index in System().Bodies.
	TargetBody
	// TargetCraft references a non-active craft by index in World.Crafts.
	TargetCraft
	// TargetSite is reserved; not populated until landing-site
	// targeting ships post-v0.9.
	TargetSite
)

// Target identifies what the player is aiming at. The zero value
// (TargetNone) is the v0.9.0 default and round-trips through save as
// an absent JSON field.
type Target struct {
	Kind     TargetKind
	BodyIdx  int // when Kind==TargetBody
	CraftIdx int // when Kind==TargetCraft
}

// SetTargetBody sets the body target by system index. Out-of-range
// or system-primary (idx 0) selections clear the target — neither is
// a valid Hohmann / plane-match consumer.
func (w *World) SetTargetBody(idx int) {
	sys := w.System()
	if idx <= 0 || idx >= len(sys.Bodies) {
		w.ClearTarget()
		return
	}
	w.Target = Target{Kind: TargetBody, BodyIdx: idx}
}

// SetTargetCraft sets the craft target by slate index. The active
// craft can't target itself; out-of-range or self-targeting clears.
func (w *World) SetTargetCraft(idx int) {
	if idx < 0 || idx >= len(w.Crafts) || idx == w.ActiveCraftIdx {
		w.ClearTarget()
		return
	}
	if w.Crafts[idx] == nil {
		w.ClearTarget()
		return
	}
	w.Target = Target{Kind: TargetCraft, CraftIdx: idx}
}

// ClearTarget drops any target. After ClearTarget,
// Target.Kind == TargetNone.
func (w *World) ClearTarget() { w.Target = Target{} }

// CycleTarget advances Target through non-active sibling crafts →
// system bodies (non-root) → None → repeat. Forward=false steps
// backwards through the same cycle. No-op when no targetable entry
// exists.
//
// Cycle order: every non-active craft in the slate first (the small
// set the player most often wants to target after spawning a sister
// craft), then bodies in the current system (idx 1 .. n-1, skipping
// the system primary which has no orbital radius), then TargetNone,
// then repeat. Sibling-frame restriction is intentionally not
// enforced on the craft branch so the player can pre-select a target
// before transferring into its frame.
func (w *World) CycleTarget(forward bool) {
	cycle := w.targetCycle()
	if len(cycle) == 0 {
		return
	}
	idx := 0
	for i, t := range cycle {
		if t == w.Target {
			idx = i
			break
		}
	}
	if forward {
		idx = (idx + 1) % len(cycle)
	} else {
		idx = (idx - 1 + len(cycle)) % len(cycle)
	}
	w.Target = cycle[idx]
}

// targetCycle enumerates the valid target slots for the current
// system + craft slate, in cycle order. Rebuilt each call so a
// freshly spawned craft or a system swap participates without
// requiring a cache invalidation.
func (w *World) targetCycle() []Target {
	cycle := []Target{{Kind: TargetNone}}
	for i, c := range w.Crafts {
		if c == nil || i == w.ActiveCraftIdx {
			continue
		}
		cycle = append(cycle, Target{Kind: TargetCraft, CraftIdx: i})
	}
	for i := 1; i < len(w.System().Bodies); i++ {
		cycle = append(cycle, Target{Kind: TargetBody, BodyIdx: i})
	}
	return cycle
}

// TargetState resolves the current target to its inertial state in
// the system primary's frame (heliocentric for Sol). Returns ok=false
// when Target.Kind is TargetNone, the index is stale, or the craft
// doesn't share enough state to surface (a non-active craft's
// inertial position is built from its primary plus its primary-
// relative R, the same way CraftInertial does for the active craft).
//
// Used by the rendezvous-tooling slice (v0.9.3) for closest-approach
// computation; v0.9.0 callers limit themselves to the body case but
// the craft branch ships now so consumers don't need to special-case
// the API surface later.
func (w *World) TargetState() (orbital.Vec3State, bool) {
	switch w.Target.Kind {
	case TargetBody:
		sys := w.System()
		if w.Target.BodyIdx <= 0 || w.Target.BodyIdx >= len(sys.Bodies) {
			return orbital.Vec3State{}, false
		}
		b := sys.Bodies[w.Target.BodyIdx]
		r := w.BodyPosition(b)
		v := w.bodyInertialVelocity(b)
		return orbital.Vec3State{R: r, V: v}, true
	case TargetCraft:
		if w.Target.CraftIdx < 0 || w.Target.CraftIdx >= len(w.Crafts) {
			return orbital.Vec3State{}, false
		}
		c := w.Crafts[w.Target.CraftIdx]
		if c == nil {
			return orbital.Vec3State{}, false
		}
		primaryPos := w.BodyPosition(c.Primary)
		primaryV := w.bodyInertialVelocity(c.Primary)
		return orbital.Vec3State{
			R: primaryPos.Add(c.State.R),
			V: primaryV.Add(c.State.V),
		}, true
	}
	return orbital.Vec3State{}, false
}

// CraftInertialVelocity returns a craft's velocity in the system-
// inertial (heliocentric) frame. Mirrors CraftInertial for position.
// Useful to consumers outside the sim package (HUD readouts, target
// resolution) that need a craft's inertial state without re-doing the
// primary-velocity addition. v0.9.0+.
func (w *World) CraftInertialVelocity(c *spacecraft.Spacecraft) orbital.Vec3 {
	if c == nil {
		return orbital.Vec3{}
	}
	return w.bodyInertialVelocity(c.Primary).Add(c.State.V)
}

// TargetName returns a short human label for the current target,
// suitable for the TARGET HUD block. Empty string when no target is
// set or the index is stale.
func (w *World) TargetName() string {
	switch w.Target.Kind {
	case TargetBody:
		sys := w.System()
		if w.Target.BodyIdx > 0 && w.Target.BodyIdx < len(sys.Bodies) {
			return sys.Bodies[w.Target.BodyIdx].EnglishName
		}
	case TargetCraft:
		if w.Target.CraftIdx >= 0 && w.Target.CraftIdx < len(w.Crafts) {
			if c := w.Crafts[w.Target.CraftIdx]; c != nil {
				return c.Name
			}
		}
	}
	return ""
}
