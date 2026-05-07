package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Target / TargetKind moved to the `spacecraft` package in v0.9.3
// polish so each Spacecraft can carry its own per-craft target as a
// struct field (per-craft target binding — each vessel remembers
// its own target across active-craft switches). The aliases below
// preserve the existing API surface so readers like
// `w.Target.Kind == sim.TargetCraft` continue to compile unchanged.
type (
	TargetKind = spacecraft.TargetKind
	Target     = spacecraft.Target
)

// Re-exported constants — preserve the `sim.TargetNone` etc.
// identifiers that 75+ readers depend on.
const (
	TargetNone  = spacecraft.TargetNone
	TargetBody  = spacecraft.TargetBody
	TargetCraft = spacecraft.TargetCraft
	TargetSite  = spacecraft.TargetSite
)

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
	w.mirrorTargetToActiveCraft()
	w.reconcileNavMode()
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
	w.mirrorTargetToActiveCraft()
}

// ClearTarget drops any target. After ClearTarget,
// Target.Kind == TargetNone. Also reconciles NavMode (snap NavTarget
// → NavOrbit) so the HUD doesn't claim a mode it can no longer
// resolve. v0.9.3+.
func (w *World) ClearTarget() {
	w.Target = Target{}
	w.mirrorTargetToActiveCraft()
	w.reconcileNavMode()
}

// mirrorTargetToActiveCraft writes w.Target onto the active craft's
// per-craft Target field so the binding survives an active-craft
// switch (v0.9.3 polish). Maintains the invariant
// w.Target == w.Crafts[w.ActiveCraftIdx].Target whenever an active
// craft exists. No-op when there is no active craft.
func (w *World) mirrorTargetToActiveCraft() {
	if w.ActiveCraftIdx < 0 || w.ActiveCraftIdx >= len(w.Crafts) {
		return
	}
	if c := w.Crafts[w.ActiveCraftIdx]; c != nil {
		c.Target = w.Target
	}
}

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
	w.mirrorTargetToActiveCraft()
	w.reconcileNavMode()
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

// TargetStateRelativeToActivePrimary returns the target craft's state
// expressed in the active craft's primary-relative frame, so the same
// (R, V) basis as ActiveCraft().State can be used for relative-vector
// math (closest approach, target-prograde direction, |v_rel|, range).
// Returns ok=false when no craft target is set, the index is stale,
// or there is no active craft.
//
// Same-primary case (the common one — rendezvous in LEO): both craft
// share a primary, so the target's primary-relative state is already
// in the active's frame. Cross-primary case: convert via inertial,
// subtract the active primary's pose. v0.9.3+.
func (w *World) TargetStateRelativeToActivePrimary() (rT, vT orbital.Vec3, ok bool) {
	if w.Target.Kind != TargetCraft {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	active := w.ActiveCraft()
	if active == nil {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	if w.Target.CraftIdx < 0 || w.Target.CraftIdx >= len(w.Crafts) {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	t := w.Crafts[w.Target.CraftIdx]
	if t == nil {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	if t.Primary.EnglishName == active.Primary.EnglishName {
		return t.State.R, t.State.V, true
	}
	targetInertialR := w.BodyPosition(t.Primary).Add(t.State.R)
	targetInertialV := w.bodyInertialVelocity(t.Primary).Add(t.State.V)
	activePrimaryR := w.BodyPosition(active.Primary)
	activePrimaryV := w.bodyInertialVelocity(active.Primary)
	return targetInertialR.Sub(activePrimaryR), targetInertialV.Sub(activePrimaryV), true
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
