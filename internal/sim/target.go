package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/bodies"
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
	TargetGhost = spacecraft.TargetGhost
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

// SetTargetCraft sets the craft target by slate index, storing the
// craft's stable ID (ADR 0012) so the binding survives slate shifts.
// The active craft can't target itself; out-of-range or self-targeting
// clears.
func (w *World) SetTargetCraft(idx int) {
	if idx < 0 || idx >= len(w.Crafts) || idx == w.ActiveCraftIdx {
		w.ClearTarget()
		return
	}
	c := w.Crafts[idx]
	if c == nil {
		w.ClearTarget()
		return
	}
	w.stampCraftID(c) // defensive: never bind to a zero ID
	w.Target = Target{Kind: TargetCraft, CraftID: c.ID}
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
		w.stampCraftID(c)
		cycle = append(cycle, Target{Kind: TargetCraft, CraftID: c.ID})
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
		c, _, ok := w.craftByID(w.Target.CraftID)
		if !ok {
			return orbital.Vec3State{}, false
		}
		primaryPos := w.BodyPosition(c.Primary)
		primaryV := w.bodyInertialVelocity(c.Primary)
		return orbital.Vec3State{
			R: primaryPos.Add(c.State.R),
			V: primaryV.Add(c.State.V),
		}, true
	case TargetGhost:
		// v0.27 S6 (ADR 0034): a remote player's craft, resolved from
		// the transient ghost slate — already evaluated at this world's
		// sim-time. A stale ref (owner offline before this server run,
		// craft gone, other system) simply doesn't resolve.
		g, ok := w.ghostByRef(w.Target.GhostOwner, w.Target.CraftID)
		if !ok {
			return orbital.Vec3State{}, false
		}
		primary, ok := w.bodyInSystemByID(g.PrimaryID)
		if !ok {
			return orbital.Vec3State{}, false
		}
		return orbital.Vec3State{
			R: g.Pos,
			V: w.bodyInertialVelocity(primary).Add(g.Vel),
		}, true
	}
	return orbital.Vec3State{}, false
}

// ghostByRef finds a ghost by owner + craft ID in the transient slate.
func (w *World) ghostByRef(owner string, craftID uint64) (Ghost, bool) {
	for _, g := range w.Ghosts {
		if g.Owner == owner && g.CraftID == craftID {
			return g, true
		}
	}
	return Ghost{}, false
}

// HasRelativeTarget reports whether the target slot holds something
// with a live relative state — a local craft or a remote ghost (v0.27
// review follow-up). Every gate that used to spell Kind==TargetCraft
// for "can I do target-relative work" goes through here so ghost
// targets light up the same surfaces.
func (w *World) HasRelativeTarget() bool {
	return w.Target.Kind == TargetCraft || w.Target.Kind == TargetGhost
}

// ResolveTargetGhost resolves a ghost target to its slate entry and
// SOI primary. ok=false when the target isn't a ghost or the ref is
// stale (owner gone, craft gone, other system).
func (w *World) ResolveTargetGhost() (Ghost, bodies.CelestialBody, bool) {
	if w.Target.Kind != TargetGhost {
		return Ghost{}, bodies.CelestialBody{}, false
	}
	g, ok := w.ghostByRef(w.Target.GhostOwner, w.Target.CraftID)
	if !ok {
		return Ghost{}, bodies.CelestialBody{}, false
	}
	primary, ok := w.bodyInSystemByID(g.PrimaryID)
	if !ok {
		return Ghost{}, bodies.CelestialBody{}, false
	}
	return g, primary, true
}

// bodyInSystemByID scans the active system for a body ID.
func (w *World) bodyInSystemByID(id string) (bodies.CelestialBody, bool) {
	for _, b := range w.System().Bodies {
		if b.ID == id {
			return b, true
		}
	}
	return bodies.CelestialBody{}, false
}

// SetTargetGhost aims the active craft at a remote player's craft
// (v0.27 S6). The Session screen is the selection surface.
func (w *World) SetTargetGhost(owner string, craftID uint64) {
	w.Target = Target{Kind: TargetGhost, CraftID: craftID, GhostOwner: owner}
	w.mirrorTargetToActiveCraft()
	w.reconcileNavMode() // ghost targets keep NavTarget valid (HasRelativeTarget)
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
	if w.Target.Kind != TargetCraft && w.Target.Kind != TargetGhost {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	active := w.ActiveCraft()
	if active == nil {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	// Ghost targets (v0.27 S6): the slate already holds the ghost's
	// world-frame position at this world's sim-time, so rendezvous
	// tooling (closest approach, |v_rel|, TGT nav modes) works against
	// a remote player's craft exactly as against a local one.
	if w.Target.Kind == TargetGhost {
		g, ok := w.ghostByRef(w.Target.GhostOwner, w.Target.CraftID)
		if !ok {
			return orbital.Vec3{}, orbital.Vec3{}, false
		}
		primary, ok := w.bodyInSystemByID(g.PrimaryID)
		if !ok {
			return orbital.Vec3{}, orbital.Vec3{}, false
		}
		activePrimaryR := w.BodyPosition(active.Primary)
		activePrimaryV := w.bodyInertialVelocity(active.Primary)
		ghostInertialV := w.bodyInertialVelocity(primary).Add(g.Vel)
		return g.Pos.Sub(activePrimaryR), ghostInertialV.Sub(activePrimaryV), true
	}
	t, _, ok := w.craftByID(w.Target.CraftID)
	if !ok {
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
		if c, _, ok := w.craftByID(w.Target.CraftID); ok {
			return c.Name
		}
	case TargetGhost:
		if g, ok := w.ghostByRef(w.Target.GhostOwner, w.Target.CraftID); ok {
			if g.Handle != "" {
				return g.Handle + "'s " + g.Name
			}
			return g.Name
		}
	}
	return ""
}
