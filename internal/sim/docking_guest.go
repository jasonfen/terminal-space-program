package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Cross-player docking (v0.28 S5, ADR 0034 §6 + the 2026-07-14 addendum).
//
// A cross-player dock fuses ONE stack simulated by exactly one client —
// the docker (active approacher). The other player's craft rides in that
// stack as a DockedComponent tagged with its owner's fingerprint and its
// pre-dock stable ID, so the guest can undock it — anywhere, anytime —
// and get it back home unchanged. These are the sim-level primitives the
// serve/relay reconcile loop calls once the cross-World handover has moved
// the guest craft into (or out of) the docker's World; the cross-World
// message plumbing lives in internal/relay (DockLedger). The MVP posture
// is active-vehicle-docks-to-passive-station (ADR addendum): a coasting
// guest is Kepler-exact between reports, so final approach isn't
// heartbeat-gated.

// DockGuestCraft fuses guest into the docker's craft at dockerIdx, producing
// one composite OWNED by this World (the docker's). The composite keeps the
// docker's identity; guest rides as one or more DockedComponents tagged with
// guestOwner (the guest player's fingerprint) so UndockGuest can peel exactly
// the guest's sub-stack back out. guest is appended to the slate and consumed
// by the fusion — the caller (serve, after the guest World handed the payload
// over) passes a craft that already carries its pre-dock stable ID. Returns
// the composite, its slate index, and ok=false for a bad index / nil guest /
// failed fuse.
//
// Ownership: the docker is always the fusion lead, regardless of which slot
// is active, so the stack's identity + this World's simulation authority are
// the docker's per ADR 0034 §6. The guest's contributed components are the
// trailing entries of the flattened DockedComponents list (fuseComposite
// appends the drop's components last); tagging those with guestOwner is the
// whole cross-player marking.
func (w *World) DockGuestCraft(dockerIdx int, guest *spacecraft.Spacecraft, guestOwner string) (*spacecraft.Spacecraft, int, bool) {
	if dockerIdx < 0 || dockerIdx >= len(w.Crafts) || guest == nil || guestOwner == "" {
		return nil, -1, false
	}
	// How many components the guest contributes once flattened: its own
	// component list, or one (AsDockedComponent) when it's a plain craft.
	guestComps := len(guest.DockedComponents)
	if guestComps == 0 {
		guestComps = 1
	}
	w.Crafts = append(w.Crafts, guest)
	guestIdx := len(w.Crafts) - 1
	comp, idx, ok := w.fuseComposite(dockerIdx, guestIdx)
	if !ok {
		return nil, -1, false
	}
	// Tag the guest's trailing components. Leave any already-owned tag
	// intact (a guest sub-stack that itself contains a further-nested guest
	// keeps that provenance — not reachable in the 2-party MVP, but the
	// guard keeps the tagging idempotent under a re-dock).
	n := len(comp.DockedComponents)
	for i := n - guestComps; i < n; i++ {
		if i >= 0 && comp.DockedComponents[i].Owner == "" {
			comp.DockedComponents[i].Owner = guestOwner
		}
	}
	return comp, idx, true
}

// UndockGuest splits the guest's sub-stack out of the composite at
// compositeIdx and RETURNS the restored guest craft (NOT added to this
// World — the serve layer injects it into the guest's own World at the
// matching seam). The docker's composite shrinks in place, keeping its
// current flight (no active switch, no burn stop — like Deploy, since the
// composite persists). Reverts to a plain craft when only the docker's own
// components remain.
//
// The restored craft is placed at the composite's CURRENT state (r, v) and
// stamped with the guest's original stable ID (guestCraftID), so it returns
// to the guest exactly where the stack is — "times always match at the seam"
// (ADR addendum: this is what keeps undock-anytime sound). guestOwner selects
// which components peel off; guestCraftID both stamps the return identity and,
// when non-zero, disambiguates one guest among several sharing an owner (not
// reachable in the 2-party MVP).
//
// Returns ok=false when the composite has no components tagged for guestOwner
// (nothing to undock) or the indices are invalid. MVP limitation: a guest that
// docked a multi-component composite is rebuilt as a single multi-stage craft
// from its first component's identity — the sub-composite seams are not
// preserved (passive-station posture docks a single craft; noted for playtest).
func (w *World) UndockGuest(compositeIdx int, guestOwner string, guestCraftID uint64) (*spacecraft.Spacecraft, bool) {
	if compositeIdx < 0 || compositeIdx >= len(w.Crafts) || guestOwner == "" {
		return nil, false
	}
	c := w.Crafts[compositeIdx]
	if c == nil || len(c.DockedComponents) < 2 {
		return nil, false
	}

	// Collect the guest's components (by owner, optionally pinned to
	// guestCraftID) and the count of live stages they occupy. Components map
	// to c.Stages in order (bottom-to-top); the guest's ride on TOP of the
	// docker's, so their live stages are the tail of c.Stages — the same
	// geometry Deploy peels.
	var guestComps []spacecraft.DockedComponent
	guestStageCnt := 0
	keepComps := make([]spacecraft.DockedComponent, 0, len(c.DockedComponents))
	for _, dc := range c.DockedComponents {
		isGuest := dc.Owner == guestOwner && (guestCraftID == 0 || dc.CraftID == guestCraftID)
		if isGuest {
			guestComps = append(guestComps, dc)
			guestStageCnt += len(dc.Stages)
		} else {
			keepComps = append(keepComps, dc)
		}
	}
	if len(guestComps) == 0 {
		return nil, false
	}
	// The guest's stages must tile the top of the composite exactly for the
	// live-stage peel — every cross-player component carries its Stages
	// breakdown (DockGuestCraft fuses live craft, never the legacy flat
	// form), so this holds; refuse rather than mis-slice if it doesn't.
	if guestStageCnt <= 0 || guestStageCnt >= len(c.Stages) {
		return nil, false
	}
	keep := len(c.Stages) - guestStageCnt
	payloadStages := append([]spacecraft.Stage(nil), c.Stages[keep:]...)

	// Rebuild the guest craft from its first component's identity + the
	// peeled live stages, at the composite's current state. restoreComponentCraft
	// backfills a command source + syncs the flat mirrors from Stages[0].
	restored := w.restoreComponentCraft(c, guestComps[0], payloadStages, c.State.R, c.State.V)
	restored.ID = guestCraftID // return home with the same stable identity

	// Shrink the composite: drop the guest's stages + components. Own backing
	// arrays so the departing craft never aliases the composite's slices.
	c.Stages = append([]spacecraft.Stage(nil), c.Stages[:keep]...)
	c.DockedComponents = keepComps
	if len(c.DockedComponents) < 2 {
		c.DockedComponents = nil // only the docker's own core is left
	}
	c.SyncFields()
	c.State.M = c.TotalMass()
	return restored, true
}

// RemoveCraftByID lifts the craft with the given stable ID out of the slate
// and returns it (v0.28 S5) — the guest side of a cross-player dock, where a
// player's craft leaves their own World to ride in the docker's stack. The
// active-craft index follows the removal so the player keeps flying a valid
// craft (or, if they removed their only craft while docking as guest, the
// index clamps and ActiveCraft() may be nil until they switch/undock). ok is
// false when no craft matches. The returned craft is detached — the caller
// hands it to the ledger; nothing in this World still references it.
func (w *World) RemoveCraftByID(id uint64) (*spacecraft.Spacecraft, bool) {
	_, idx, ok := w.craftByID(id)
	if !ok {
		return nil, false
	}
	c := w.Crafts[idx]
	// Checkpoint the outgoing active target before the slate shifts.
	if w.ActiveCraftIdx >= 0 && w.ActiveCraftIdx < len(w.Crafts) {
		if a := w.Crafts[w.ActiveCraftIdx]; a != nil {
			a.Target = w.Target
		}
	}
	active := w.ActiveCraftIdx
	w.Crafts = append(w.Crafts[:idx], w.Crafts[idx+1:]...)
	switch {
	case idx < active:
		active--
	case idx == active:
		// The active craft left; land on a neighbouring slot if any.
		if active >= len(w.Crafts) {
			active = len(w.Crafts) - 1
		}
	}
	if active < 0 {
		w.ActiveCraftIdx = 0 // empty slate — index is inert until a craft returns
		w.Target = spacecraft.Target{}
	} else {
		w.ActiveCraftIdx = -1 // force SetActiveCraftIdx to load the new active's target
		w.SetActiveCraftIdx(active)
	}
	return c, true
}

// AdoptCraft appends a craft carrying a stable ID into the slate on the
// receiving side of a cross-player handoff (v0.28 S5) — the docker adopting a
// handed-over guest craft, the guest receiving its component back on undock, or
// a Transfer-Control recipient adopting the whole migrating composite. Craft-ID
// spaces are PER-WORLD and independent (both start low), so an incoming ID from
// the origin World can collide with a native craft already in this slate; when
// it does — or when the craft carries no ID at all — we stamp a FRESH id from
// NextCraftID (restamped in place, so a caller reading c.ID after the call sees
// the new id). A non-zero, unused incoming ID is PRESERVED (the guest-gets-its-
// component-back case, which must keep guestCraftID; NextCraftID only moves
// forward, so a preserved id never collides with a future local spawn).
// Advances NextCraftID past a preserved ID and optionally makes it active.
// Returns the new slate index.
func (w *World) AdoptCraft(c *spacecraft.Spacecraft, makeActive bool) int {
	if c == nil {
		return -1
	}
	// Collision check must run BEFORE the append so craftByID scans only the
	// existing slate, not c itself. A hit forces a fresh stamp below.
	if c.ID != 0 {
		if _, _, taken := w.craftByID(c.ID); taken {
			c.ID = 0
		}
	}
	w.Crafts = append(w.Crafts, c)
	if c.ID != 0 && w.NextCraftID <= c.ID {
		w.NextCraftID = c.ID + 1
	}
	w.stampCraftID(c) // no-op when it kept its ID; fresh id when zeroed above
	idx := len(w.Crafts) - 1
	if makeActive {
		w.SetActiveCraftIdx(idx)
	}
	return idx
}

// tagStackOwnership is the Transfer-control retag (v0.28 S5, ADR 0034 §6 +
// addendum): handing an entire cross-player stack from fromOwner to toOwner
// flips every component's provenance. Components owned by the World that held
// the stack (Owner=="") become fromOwner's (they're now the GUEST's ride);
// components owned by toOwner become the new holder's own (Owner clears). The
// stack's physical fusion is unchanged — only the ownership tags move, so the
// new owner's World adopts the composite and the old owner becomes the guest.
// Idempotent for a same-owner call. Pure over c; the caller migrates the
// composite between Worlds around it.
func tagStackOwnership(c *spacecraft.Spacecraft, fromOwner, toOwner string) {
	if c == nil || fromOwner == toOwner {
		return
	}
	for i := range c.DockedComponents {
		switch c.DockedComponents[i].Owner {
		case "":
			c.DockedComponents[i].Owner = fromOwner
		case toOwner:
			c.DockedComponents[i].Owner = ""
		}
	}
}

// RetagStackForTransfer is the exported form of tagStackOwnership for the
// serve/relay layer that migrates a stack between player Worlds on a Transfer
// Control (v0.28 S5). fromOwner is the fingerprint of the World the stack is
// leaving; toOwner is the fingerprint of the World adopting it. After this the
// composite's components describe the post-transfer roles: the departing owner
// rides as a guest, the recipient owns its own core.
func RetagStackForTransfer(c *spacecraft.Spacecraft, fromOwner, toOwner string) {
	tagStackOwnership(c, fromOwner, toOwner)
}

// DockGuestLink is the transient docked-as-guest slate the serve layer
// writes onto World.DockGuest each tick while one of this player's craft
// rides in another player's stack (v0.28 S5). It names the stack owner (for
// the coupling + the "docked with X" status) and carries the owner's current
// Effective warp so the guest's clampedWarp can fold in the min-wins coupling.
// GuestCraftID is the guest's own craft riding in the stack — the ID the
// Undock-as-guest signal hands to the owner's UndockGuest. Never persisted.
type DockGuestLink struct {
	OwnerFP      string  // stack owner's fingerprint (the docker / current holder)
	OwnerHandle  string  // stack owner's display handle (for chips + status)
	OwnerEffWarp float64 // owner's reported Effective warp — the coupling min
	GuestCraftID uint64  // the guest's craft riding in the stack
}

// WithDockCoupling folds a docked-as-guest coupling into the co-warp state
// (v0.28 S5). While a player is Docked-as-Guest their whole subspace is
// warp-coupled to the stack regardless of range — the guest's craft is IN
// the stack, not near it, so the range-gated ComputeCoWarp can't express
// this; instead the serve layer folds the stack owner's Effective warp in
// here after ComputeCoWarp. Reuses the exact S1 clamp: clampedWarp already
// reads CoWarp.Coupled / MinWarp, so no new clamp is needed. min-wins — the
// resulting MinWarp is the lesser of any range-coupled peer and the owner's
// warp, so the guest can always select lower (slam 1× and burn) but can't
// out-warp the owner. A non-positive ownerEffWarp (paused owner) imposes no
// coupling, matching ComputeCoWarp's paused-peer handling.
func (s CoWarpState) WithDockCoupling(ownerHandle string, ownerEffWarp float64) CoWarpState {
	if ownerEffWarp <= 0 {
		return s
	}
	if !s.Coupled || s.MinWarp <= 0 || ownerEffWarp < s.MinWarp {
		s.MinWarp = ownerEffWarp
	}
	s.Coupled = true
	// Surface the owner in Partners for the HUD when not already present via
	// a range couple (dedup keeps a guest who is ALSO range-near the owner
	// from showing twice).
	found := false
	for _, p := range s.Partners {
		if p == ownerHandle {
			found = true
			break
		}
	}
	if !found && ownerHandle != "" {
		// Append in place: this runs every tick while docked-as-guest, so the
		// full-copy form was needless churn. Safe against caller aliasing —
		// ComputeCoWarp builds a fresh Partners slice each tick and the caller
		// folds straight back into the same field (w.CoWarp = w.CoWarp.With...),
		// so no other reference retains the pre-fold slice (v0.28 finding 5).
		s.Partners = append(s.Partners, ownerHandle)
	}
	return s
}

// StackMidBurn reports whether the craft is actively thrusting — a planted
// finite burn (ActiveBurn) or a player-held manual burn (ManualBurn) in
// flight (v0.28 S5). Transfer Control is refused while a cross-player stack
// is mid-burn (ADR 0034 addendum: "refused mid-burn") so the integrator and
// mass-loss state don't hand off across the ownership seam.
func StackMidBurn(c *spacecraft.Spacecraft) bool {
	return c != nil && (c.ActiveBurn != nil || c.ManualBurn != nil)
}

// StackHasGuest reports whether the craft is a composite carrying any
// cross-player (guest-owned) component — i.e. a cross-player stack rather than
// a plain single-World composite. Used by the transfer-control and
// undock-as-guest routing to tell a cross-player stack apart.
func StackHasGuest(c *spacecraft.Spacecraft) bool {
	if c == nil {
		return false
	}
	for _, dc := range c.DockedComponents {
		if dc.Owner != "" {
			return true
		}
	}
	return false
}
