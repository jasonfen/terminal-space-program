package sim

import (
	"math"

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

// RestoreTarget resets Target and NavMode directly to a previously
// captured pair, bypassing the individual Set*/Clear setters'
// validation — the caller already knows this was a valid live state (it
// read it straight off the World a moment earlier). Mirrors the
// restored Target onto the active craft the same way every other
// setter does, so the per-craft binding stays consistent.
//
// Added for PR #392 review finding 2: SessionCmdRendezvous
// (app.go) calls SetTargetGhost before RendezvousCommit to give the
// commit search something to aim at, but a refusal must not leave that
// switch behind — the player's previous target (mid-transfer TargetBody
// Moon, say) would otherwise be silently replaced, and the stale ghost
// ref would persist into the next save, even though the game said no to
// the rendezvous itself.
func (w *World) RestoreTarget(t Target, nav NavMode) {
	w.Target = t
	w.mirrorTargetToActiveCraft()
	w.NavMode = nav
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

// CycleTarget advances Target through the nearest-first order
// targetCycle builds → None → repeat. Forward=false steps backwards
// through the same cycle; nothing in the tui binds it (#425: no reverse
// cycle — there's no free obvious key, and `T` already clears). No-op
// when no targetable entry exists.
//
// Cycle order (grilled 2026-09-04, #425; CONTEXT.md §"Target Cycle"):
// none, then the moons of the active Vessel's CURRENT primary, then
// other Vessels in slate order, then the remaining Bodies outward in
// catalog order with their own moons right after each — the active
// Vessel's own primary is skipped entirely (never a useful aim). From
// LEO the first press is the Moon; from lunar orbit it's Earth.
// Sibling-frame restriction is intentionally not enforced on the craft
// branch so the player can pre-select a target before transferring into
// its frame.
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
// system + craft slate, in nearest-first cycle order (grilled
// 2026-09-04, #425; CONTEXT.md §"Target Cycle"): none, then the moons
// of the active Vessel's current primary, then other Vessels (existing
// slate order), then the remaining bodies outward in catalog order with
// their own moons appended right after each. The active Vessel's own
// primary never appears — it has no orbital radius relative to itself,
// so it was never a useful aim. Rebuilt each call so a freshly spawned
// craft, a system swap, or an SOI transition participates without
// requiring a cache invalidation.
func (w *World) targetCycle() []Target {
	cycle := []Target{{Kind: TargetNone}}
	sys := w.System()
	if len(sys.Bodies) == 0 {
		return cycle
	}

	activePrimaryIdx := -1
	if ac := w.ActiveCraft(); ac != nil {
		activePrimaryIdx, _ = bodyIndexByID(&sys, ac.Primary.ID)
	}

	// isChildOf reports whether body bi's gravitational parent is body
	// pi, honouring System.ParentOf's convention that an empty ParentID
	// means "orbits the system primary" (index 0) rather than literally
	// matching an empty string.
	isChildOf := func(bi, pi int) bool {
		b := sys.Bodies[bi]
		if b.ParentID != "" {
			return sys.Bodies[pi].ID == b.ParentID
		}
		return pi == 0
	}

	added := make(map[int]bool, len(sys.Bodies))

	// 1. Moons of the active Vessel's CURRENT primary — nearest first,
	// in catalog order. Never includes the primary itself (index
	// activePrimaryIdx) or the system primary (index 0, which has no
	// orbital radius and is never a valid target).
	if activePrimaryIdx >= 0 {
		for i := range sys.Bodies {
			if i == 0 || i == activePrimaryIdx {
				continue
			}
			if isChildOf(i, activePrimaryIdx) {
				cycle = append(cycle, Target{Kind: TargetBody, BodyIdx: i})
				added[i] = true
			}
		}
	}

	// 2. Other Vessels, existing slate order.
	for i, c := range w.Crafts {
		if c == nil || i == w.ActiveCraftIdx {
			continue
		}
		w.stampCraftID(c)
		cycle = append(cycle, Target{Kind: TargetCraft, CraftID: c.ID})
	}

	// 3. Remaining bodies OUTWARD from wherever the active Vessel
	// actually is, each immediately followed by its own moons — even
	// though the catalog itself lists every moon after every top-level
	// body, not interleaved.
	//
	// "Outward" is anchored, not a fixed global start: it begins at the
	// top-level ancestor of the active primary (that's the primary
	// itself when it's already top-level — LEO's Earth — or the parent
	// it orbits when it's a moon — lunar orbit's Earth again, one level
	// up), then walks the rest of the top-level bodies in catalog order,
	// wrapping around to catch anything "behind" (closer to the system
	// primary) last. Without this anchor, "from lunar orbit it is
	// Earth" (CONTEXT.md) would be impossible — a fixed catalog-order
	// walk starting at Mercury would visit Mercury and Venus before
	// Earth even though both are farther from where the player actually
	// is. When the active primary IS the top-level anchor (LEO), that
	// anchor is the skipped own-primary, so the walk starts right after
	// it instead of at it.
	var topLevel []int
	for i := 1; i < len(sys.Bodies); i++ {
		if sys.Bodies[i].ParentID == "" {
			topLevel = append(topLevel, i)
		}
	}
	startPos := 0
	if activePrimaryIdx > 0 {
		anchorIdx := activePrimaryIdx
		for sys.Bodies[anchorIdx].ParentID != "" {
			pid, ok := bodyIndexByID(&sys, sys.Bodies[anchorIdx].ParentID)
			if !ok {
				break
			}
			anchorIdx = pid
		}
		for pos, idx := range topLevel {
			if idx != anchorIdx {
				continue
			}
			startPos = pos
			if anchorIdx == activePrimaryIdx && len(topLevel) > 0 {
				startPos = (pos + 1) % len(topLevel)
			}
			break
		}
	}
	for k := 0; k < len(topLevel); k++ {
		i := topLevel[(startPos+k)%len(topLevel)]
		if i != activePrimaryIdx && !added[i] {
			cycle = append(cycle, Target{Kind: TargetBody, BodyIdx: i})
			added[i] = true
		}
		for j := range sys.Bodies {
			if j == activePrimaryIdx || added[j] {
				continue
			}
			if isChildOf(j, i) {
				cycle = append(cycle, Target{Kind: TargetBody, BodyIdx: j})
				added[j] = true
			}
		}
	}
	return cycle
}

// bodyIndexByID returns the index of the body with the given catalog ID
// in sys.Bodies, or ok=false if none matches.
func bodyIndexByID(sys *bodies.System, id string) (int, bool) {
	for i, b := range sys.Bodies {
		if b.ID == id {
			return i, true
		}
	}
	return -1, false
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

// nodeTargetRelState resolves a maneuver node's (or active burn's) bound
// target to its state in the given primary's relative frame, handling
// both a local craft ref (owner == "") and a remote ghost ref
// (owner != "", v0.28 S4 / ADR 0034). Rendezvous tooling is same-primary
// gated, so ok=false when the target orbits a different body — matching
// the pre-v0.28 craftByID call sites. A stale ref (craft or ghost gone,
// other primary) resolves to ok=false, degrading the burn to no-op
// exactly as a vanished local craft does.
//
// Ghost state is taken from g.Pos (world-frame marker at this world's
// sim-time) minus the primary's position — the same Pos-based derivation
// TargetStateRelativeToActivePrimary uses, so it doesn't depend on the
// optional RelPos field being populated. Velocity is g.Vel (already
// primary-relative); for the shared-primary case the primary's own
// velocity cancels.
func (w *World) nodeTargetRelState(owner string, craftID uint64, primary bodies.CelestialBody) (rT, vT orbital.Vec3, ok bool) {
	if craftID == 0 {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	if owner != "" {
		g, ok := w.ghostByRef(owner, craftID)
		if !ok || g.PrimaryID != primary.ID {
			return orbital.Vec3{}, orbital.Vec3{}, false
		}
		return g.Pos.Sub(w.BodyPosition(primary)), g.Vel, true
	}
	tc, _, ok := w.craftByID(craftID)
	if !ok || tc.Primary.ID != primary.ID {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	return tc.State.R, tc.State.V, true
}

// TargetRelState is the exported door onto nodeTargetRelState for
// callers outside the sim package (internal/tui/screens's maneuver form,
// #294 review round 3 finding I): resolves a captured (owner, craftID)
// target-relative ref — local craft (owner=="") or remote ghost — to its
// state in the given primary's relative frame, exactly as a planted
// node or the active burn resolves its own bound target at fire time.
// ok=false when the ref is empty or doesn't resolve.
func (w *World) TargetRelState(owner string, craftID uint64, primary bodies.CelestialBody) (rT, vT orbital.Vec3, ok bool) {
	return w.nodeTargetRelState(owner, craftID, primary)
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
//
// #294 review finding 4 (round 2) REVERTED round 1's "a ghost target
// must actually RESOLVE to count here": that version made a docker
// flying NavTarget through ADR 0038 undock/proximity ops get demoted
// to NavOrbit mid-op the instant the ghost slate went briefly empty
// (reconcileNavMode runs off this every tick), even though the lock
// itself was fine and about to re-latch — the comment on SetTargetGhost
// ("ghost targets keep NavTarget valid") stopped being true. The
// zero-direction-SAS-hold hazard round 1 was guarding against doesn't
// need HasRelativeTarget's help: attitudeContext (world.go) already
// falls back to an orbit-frame hold whenever a target-relative mode's
// (rT, vT) resolve fails, independently of NavMode or this function —
// see TestAttitudeContextFallsBackWhenGhostUnresolved. So Kind alone is
// enough here again: a ghost target counts as relative whether or not
// it currently resolves, exactly like a local craft target always has.
func (w *World) HasRelativeTarget() bool {
	switch w.Target.Kind {
	case TargetCraft, TargetGhost:
		return true
	}
	return false
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

// TargetLeadAngleDeg answers the phase-order question a rendezvous
// planning step needs first (#287): is the active craft ahead of or
// behind its craft/ghost target along the shared orbit? Returns the
// signed along-track angle from the active craft to the target, in
// degrees, range (-180, 180]. Positive means the target is AHEAD of the
// active craft along its direction of orbital motion — i.e. the active
// craft is trailing and must lower its orbit to catch up; negative means
// the target is behind.
//
// The angle is measured about the active craft's own specific angular
// momentum axis (h = r x v, unit vector), which is exactly "signed by
// the direction of the craft's motion" — the sign is intrinsic to how
// the active craft is actually flying, not an assumption about which
// way orbits in this system usually go. The target's position is used
// as-is (not first forced coplanar): projecting a vector onto the plane
// perpendicular to h and then taking the signed angle from the active
// craft's radius vector is exactly what
// atan2(hHat . (r_active x r_target), r_active . r_target) computes (the
// out-of-plane component of r_target cancels in both the numerator and
// denominator), so a non-coplanar target still yields a usable
// along-track answer via that projection rather than refusing outright.
//
// ok=false when there is no craft/ghost target, the target doesn't
// resolve, or the target orbits a different primary than the active
// craft — a phase angle spanning two different SOIs has no shared
// reference orbit to measure it in, so the caller should render "—"
// rather than a misleading number (issue #287 decision: craft targets
// only, and only when both craft share a primary).
func (w *World) TargetLeadAngleDeg() (float64, bool) {
	if w.Target.Kind != TargetCraft && w.Target.Kind != TargetGhost {
		return 0, false
	}
	c := w.ActiveCraft()
	if c == nil {
		return 0, false
	}
	var targetPrimary bodies.CelestialBody
	switch w.Target.Kind {
	case TargetCraft:
		t, _, ok := w.craftByID(w.Target.CraftID)
		if !ok {
			return 0, false
		}
		targetPrimary = t.Primary
	case TargetGhost:
		_, primary, ok := w.ResolveTargetGhost()
		if !ok {
			return 0, false
		}
		targetPrimary = primary
	}
	if targetPrimary.ID != c.Primary.ID {
		return 0, false
	}
	rT, _, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return 0, false
	}
	a := c.State.R
	h := a.Cross(c.State.V)
	hNorm := h.Norm()
	if hNorm == 0 || a.Norm() == 0 {
		return 0, false // degenerate orbit (e.g. radial fall) — no defined plane
	}
	hHat := h.Scale(1 / hNorm)
	theta := math.Atan2(hHat.Dot(a.Cross(rT)), a.Dot(rT))
	return theta * 180 / math.Pi, true
}

// TargetSharesActivePrimary reports whether the active craft's bound
// craft/ghost target orbits the SAME primary as the active craft — the
// gate every same-primary-only piece of rendezvous-adjacent tooling
// built on TargetStateRelativeToActivePrimary needs (TargetPlaneNodePositions,
// the map's Closest-Approach marker, #346): cross-SOI rendezvous is
// already out of scope for the rendezvous tooling (see CONTEXT.md's
// Rendezvous entry), and a target re-expressed in the active craft's
// frame across two different primaries doesn't correspond to a point on
// that target's own drawn orbit. False for a body/site target or no
// target at all.
func (w *World) TargetSharesActivePrimary() bool {
	c := w.ActiveCraft()
	if c == nil {
		return false
	}
	switch w.Target.Kind {
	case TargetCraft:
		t, _, ok := w.craftByID(w.Target.CraftID)
		return ok && t.Primary.ID == c.Primary.ID
	case TargetGhost:
		_, primary, ok := w.ResolveTargetGhost()
		return ok && primary.ID == c.Primary.ID
	default:
		return false
	}
}

// TargetPlaneNodePositions locates the active craft's crossings of its
// bound target's orbital plane — an Ascending / Descending Node pair
// measured against the TARGET's plane, unlike orbital.TimeToNodeCrossing's
// usual reference-plane sense (the primary's equator / the ecliptic; see
// the Ascending Node / Descending Node glossary entries). The map's ◇ / ◆
// markers (ADR 0020 §3 / #346) plot these two points on the active
// craft's own orbit.
//
// Reuses TimeToNodeCrossing exactly rather than re-deriving node-crossing
// math: PlanPlaneMatch already solves "coplanar with X" by re-expressing
// the craft's state in a frame whose Z axis is X's orbit normal
// (orbital.FrameFromNormal) and asking the ordinary reference-plane
// helper for its crossings in that frame — this does the same thing with
// the target CRAFT/ghost's relative angular momentum (rT × vT) standing
// in for a body's catalog-derived orbit normal.
//
// ok (hasAN / hasDN) is false when: there's no active craft, no bound
// craft/ghost target, the target orbits a different primary (see
// TargetSharesActivePrimary), the target's relative state is degenerate
// (coincident position, zero relative angular momentum), or the two
// planes are within TimeToNodeCrossing's own equatorial tolerance of
// coincident (no defined line of nodes). hasAN and hasDN are reported
// independently in case only one resolves.
func (w *World) TargetPlaneNodePositions() (anPos, dnPos orbital.Vec3, hasAN, hasDN bool) {
	c := w.ActiveCraft()
	if c == nil || !w.TargetSharesActivePrimary() {
		return orbital.Vec3{}, orbital.Vec3{}, false, false
	}
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return orbital.Vec3{}, orbital.Vec3{}, false, false
	}
	nTarget := rT.Cross(vT)
	if nTarget.Norm() == 0 {
		return orbital.Vec3{}, orbital.Vec3{}, false, false
	}
	mu := c.Primary.GravitationalParameter()
	planeFrame := orbital.FrameFromNormal(nTarget)
	stateTF := orbital.Vec3State{
		R: planeFrame.FromWorld(c.State.R),
		V: planeFrame.FromWorld(c.State.V),
	}
	tAN := orbital.TimeToNodeCrossing(stateTF, mu, true)
	tDN := orbital.TimeToNodeCrossing(stateTF, mu, false)
	if tAN >= 0 {
		post, postPrimary := w.propagateCraftWithPrimary(tAN)
		anPos = w.BodyPosition(postPrimary).Add(post.R)
		hasAN = true
	}
	if tDN >= 0 {
		post, postPrimary := w.propagateCraftWithPrimary(tDN)
		dnPos = w.BodyPosition(postPrimary).Add(post.R)
		hasDN = true
	}
	return anPos, dnPos, hasAN, hasDN
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
		// #294 review finding 4: an unresolved ghost still HOLDS the lock
		// (Kind stays TargetGhost — see HasRelativeTarget) — returning ""
		// here made the TARGET chip vanish outright, indistinguishable
		// from TargetNone / a cleared target. Show the pending state
		// instead, using the roster handle when a live session has one
		// (GhostOwnerHandle), so the player can tell "still locked, just
		// waiting to resolve" from "not locked at all".
		if h := w.GhostOwnerHandle(w.Target.GhostOwner); h != "" {
			return h + "'s vessel (pending)"
		}
		return "pending target lock"
	}
	return ""
}

// GhostOwnerHandle resolves a ghost owner fingerprint to a display
// handle via the live session roster (#294 review finding 4) — nil
// w.Session (solo play, or a session whose roster hasn't refreshed yet)
// or no matching row both return "". Same lookup shape as
// DockOwnerOnline (session_info.go).
func (w *World) GhostOwnerHandle(owner string) string {
	if w.Session == nil || owner == "" {
		return ""
	}
	for _, p := range w.Session.Players {
		if p.Fingerprint == owner {
			return p.Handle
		}
	}
	return ""
}
