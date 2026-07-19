package sim

import "time"

// Auto-Warp (v0.16 / ADR 0016). One control warps time to a fixed lead
// before the next burn, then hands off to 1× so the player can watch it
// arm and fire. The driver never mutates Selected Warp (Clock.WarpIdx):
// while engaged it max-seeds clampedWarp's "selected" baseline and adds
// one approach term anchored at T (= BurnStart − lead), so every existing
// warp clamp still picks the actual rate — Auto-Warp only automates
// *picking and releasing* warp, never inventing a step that aliases past
// a burn. The target is frozen by node identity (ADR 0016 Slice 1) so it
// follows the burn the player engaged for across edits.

// autoWarpLeadTime is the sim-time gap between where Auto-Warp drops to
// 1× and the target burn's BurnStart. Fixed in v1 (not configurable):
// 30 s of 1× coast is ample warning to watch the burn arm.
const autoWarpLeadTime = 30 * time.Second

// AutoWarpTarget is the engaged driver's frozen aim. CraftID+NodeID is
// the stable identity of the burn being chased (ADR 0016 Slice 1); T is
// the sim-time the driver seeks before releasing to 1×. Transient — not
// persisted, so a save/load mid-warp lands disengaged.
type AutoWarpTarget struct {
	CraftID uint64
	NodeID  uint64
	T       time.Time

	// Sync (v0.27 S7, ADR 0034): when true the driver chases a fixed
	// sim-time — another player's subspace — instead of a node. No
	// node identity, no re-freeze, no lead: arrival is AT T, at 1×,
	// in the shared subspace. Every warp clamp (burn cap, SOI guard,
	// node ramp, the approach term anchored at T) applies unchanged —
	// planted nodes en route are lived through, not skipped.
	Sync       bool
	SyncHandle string // whose time we're chasing (arrival chip text)
	SyncOwner  string // their fingerprint — the serve layer re-freezes T from their latest report (a leader at warp is a moving target)

	// Rendezvous (v0.29 S1, ADR 0034 v0.29 addendum): when true the driver
	// is the shared coast to a mutually-armed encounter — a fixed sim-time
	// target like Sync, but the target is held (never re-frozen) and
	// arrival clears the viewer's RendezvousArm so Proximity Co-Warp takes
	// over the final approach. Started by DriveRendezvousWarp only once
	// both players are armed.
	Rendezvous       bool
	RendezvousOwner  string // partner fingerprint (retract detection + chip)
	RendezvousHandle string // partner handle (arrival chip text)
}

// autoWarpEngaged reports whether the driver is active.
func (w *World) autoWarpEngaged() bool { return w.AutoWarp != nil }

// AutoWarpEngaged is the exported form for the tui (HUD chip + button state).
func (w *World) AutoWarpEngaged() bool { return w.autoWarpEngaged() }

// AutoWarpEligible reports whether engaging right now would find a burn
// to chase — drives the dimmed/active state of the title-bar button.
func (w *World) AutoWarpEligible() bool {
	_, _, _, ok := w.soonestEligibleBurn()
	return ok
}

// AutoWarpSecondsToTarget returns the sim-seconds until the engaged
// driver's release point T, and ok=false when not engaged — feeds the
// `AUTO → Nx ⏱ Ms` HUD chip.
func (w *World) AutoWarpSecondsToTarget() (float64, bool) {
	if !w.autoWarpEngaged() {
		return 0, false
	}
	dt := w.AutoWarp.T.Sub(w.Clock.SimTime).Seconds()
	if dt < 0 {
		dt = 0
	}
	return dt, true
}

// EngageAutoWarp aims the driver at the globally-soonest eligible burn
// across all vessels and returns true on success. Eligible ⇔ BurnStart
// is more than autoWarpLeadTime in the future; otherwise the press is a
// no-op returning false (the button is dimmed). Engaging while paused
// auto-unpauses so time actually advances.
func (w *World) EngageAutoWarp() bool {
	craftID, nodeID, burnStart, ok := w.soonestEligibleBurn()
	if !ok {
		return false
	}
	w.AutoWarp = &AutoWarpTarget{
		CraftID: craftID,
		NodeID:  nodeID,
		T:       burnStart.Add(-autoWarpLeadTime),
	}
	w.Clock.Paused = false // engaging while paused auto-unpauses
	return true
}

// ToggleAutoWarp engages the driver, or disengages it if already on
// (a manual cancel — Selected Warp is left untouched). Returns the
// engaged state after the toggle.
func (w *World) ToggleAutoWarp() bool {
	if w.autoWarpEngaged() {
		w.DisengageAutoWarp()
		return false
	}
	return w.EngageAutoWarp()
}

// DisengageAutoWarp releases the driver without touching Selected Warp,
// so the player falls back to exactly the warp they had. This is the
// manual-cancel / node-invalidated path; the reached-target path in
// resolveAutoWarp additionally forces WarpIdx to 1×.
//
// A rendezvous coast is arm + driver as one unit (v0.29 review): every
// disengage path — manual warp keys, [G] toggle, [/] — must clear the
// arm too, or DriveRendezvousWarp restarts the coast (and force-unpauses)
// on the next serve tick, making the cancel a silent no-op.
func (w *World) DisengageAutoWarp() {
	if w.rendezvousWarpEngaged() {
		w.RendezvousArm = nil
	}
	w.AutoWarp = nil
}

// SyncArrival marks a completed Sync (v0.27 S7) — set by
// resolveAutoWarp at release, consumed (and cleared) by the serve
// wrapper to fire the arrival chips on both sides. Transient.
type SyncArrival struct {
	Handle string // whose subspace we arrived in
	Owner  string // their fingerprint — addresses the "synced to you" chip
}

// RendezvousArrival marks a completed Rendezvous Warp (v0.29 S1) — set by
// resolveAutoWarp when the shared coast reaches τ, consumed (and cleared)
// by the serve wrapper to fire the arrival chip. Transient, like
// SyncArrival.
type RendezvousArrival struct {
	Handle string // the partner whose encounter we arrived at
	Owner  string // their fingerprint
}

// EngageRendezvousWarp records the viewer's Rendezvous Warp intent toward
// partner, committed to the encounter sim-time tau — the initiator's
// authoritative TCA (v0.29 S1, ADR 0034 v0.29 addendum). handle is the
// partner's display name, captured here so chips and the HUD never have
// to resolve a fingerprint through a possibly-stale roster. It does NOT
// start the shared coast: DriveRendezvousWarp starts it only once the
// partner has Engaged back, so the first to Engage never warps solo.
// Forward-only (tau at/behind SimTime is refused — the laggard Syncs
// forward). Replaces any prior arm.
func (w *World) EngageRendezvousWarp(partner, handle string, tau time.Time, committedCA float64) bool {
	if !tau.After(w.Clock.SimTime) {
		return false
	}
	w.RendezvousArm = &RendezvousArm{TargetOwner: partner, Handle: handle, Tau: tau, CommittedCA: committedCA}
	return true
}

// DisengageRendezvousWarp cancels the viewer's Rendezvous Warp: clear the
// arm and, if the shared coast had started, release the Auto-Warp
// (Selected Warp untouched). Either player's cancel releases both — the
// retraction travels the wire and the partner's DriveRendezvousWarp sees
// the arm vanish and cancels in turn.
func (w *World) DisengageRendezvousWarp() {
	w.RendezvousArm = nil
	if w.rendezvousWarpEngaged() {
		w.DisengageAutoWarp()
	}
}

// rendezvousWarpEngaged reports whether the Auto-Warp driver is the shared
// rendezvous coast (vs a node chase or a Sync).
func (w *World) rendezvousWarpEngaged() bool {
	return w.AutoWarp != nil && w.AutoWarp.Rendezvous
}

// RendezvousWarpEngaged is the exported form for the tui (v0.29 S2) —
// the RENDEZVOUS chip forks its armed-waiting vs coasting state on it.
func (w *World) RendezvousWarpEngaged() bool { return w.rendezvousWarpEngaged() }

// RendezvousInvite is a peer's live Rendezvous Warp intent aimed at the
// viewer, awaiting a response (v0.29 S2): who, and the committed τ +
// predicted approach the responder adopts verbatim on join. The World
// slate field of the same name carries at most one (pairwise MVP).
type RendezvousInvite struct {
	Owner  string    // partner fingerprint — EngageRendezvousWarp's target on respond
	Handle string    // display name for the prompt/chip
	Tau    time.Time // the initiator's committed encounter sim-time
	CA     float64   // m — the initiator's committed predicted approach
}

// refreshRendezvousInvite rebuilds the invite slate from this tick's
// peer set (v0.29 S2). At most one invite surfaces: the first armed
// peer with a still-future τ, and only while the viewer has no outgoing
// arm — once mutually armed (or armed elsewhere) there is nothing to
// respond to. A past-τ arm is dropped here rather than surfaced, since
// Engage would refuse it (forward-only).
func (w *World) refreshRendezvousInvite(peers []CoWarpPeer) {
	w.RendezvousInvite = nil
	if w.RendezvousArm != nil {
		return
	}
	for i := range peers {
		p := &peers[i]
		// Same-subspace gate (v0.29 review): an invite from a diverged
		// peer is not joinable — Engage would succeed but the coast could
		// never start, so surfacing it would make the [y] prompt a lie.
		// The prompt reappears if the pair converges again.
		if p.ArmedTowardViewer && p.RendezvousTau.After(w.Clock.SimTime) &&
			sameSubspace(w.Clock.SimTime, p.SubspaceTime) {
			w.RendezvousInvite = &RendezvousInvite{
				Owner: p.Owner, Handle: p.Handle, Tau: p.RendezvousTau, CA: p.RendezvousCA,
			}
			return
		}
	}
}

// DriveRendezvousWarp starts, holds, or cancels the shared coast to the
// committed encounter from this tick's mutual-arm state (v0.29 S1).
// Called each tick after the co-warp peers are built. The coast starts
// only once BOTH players are armed toward each other in the same Subspace
// (no solo drift); a genuine retract or disconnect mid-coast cancels —
// either side's cancel releases both. Arrival is handled in
// resolveAutoWarp (it clears the arm), so an armless world with the coast
// still flagged just releases it here defensively.
func (w *World) DriveRendezvousWarp(peers []CoWarpPeer) {
	w.driveRendezvousCoast(peers)
	// One shared tail (v0.29 review): the degrade recompute reflects this
	// tick's engaged state, and the invite refresh runs after start/cancel
	// so a retract this tick can immediately surface another pending arm
	// (the unarmed viewer is the responder case).
	w.refreshRendezvousDegrade(peers)
	w.refreshRendezvousInvite(peers)
}

func (w *World) driveRendezvousCoast(peers []CoWarpPeer) {
	w.RendezvousHold = false
	arm := w.RendezvousArm
	if arm == nil {
		if w.rendezvousWarpEngaged() {
			w.DisengageAutoWarp()
		}
		return
	}
	// Arm expiry (v0.29 review): an un-started arm whose τ has passed can
	// never couple — the partner's invite already dropped it (forward-only),
	// so holding it would freeze the state machine (stuck "waiting" chip,
	// all future invites suppressed).
	if !w.rendezvousWarpEngaged() && !arm.Tau.After(w.Clock.SimTime) {
		w.RendezvousArm = nil
		return
	}
	// Re-arm reconciliation (v0.29 review): the coast must always reflect
	// the CURRENT arm. A new partner drops the old coast (the start case
	// below re-aims); a re-committed τ re-freezes T so the driver, the
	// arm, and the wire never disagree on the encounter time.
	if w.rendezvousWarpEngaged() {
		if w.AutoWarp.RendezvousOwner != arm.TargetOwner {
			w.AutoWarp = nil // the arm survives — this is a re-aim, not a cancel
		} else if !w.AutoWarp.T.Equal(arm.Tau) {
			w.AutoWarp.T = arm.Tau
		}
	}
	// Partner match is by identity only. The same-subspace gate applies to
	// STARTING the coast (below) — an engaged coast instead holds through a
	// transient divergence (v0.29 review): pause/report gaps must not read
	// as a retract and destroy the mutual encounter.
	var partner *CoWarpPeer
	for i := range peers {
		p := &peers[i]
		if p.Owner == arm.TargetOwner && p.ArmedTowardViewer {
			partner = p
			break
		}
	}
	switch {
	case partner != nil && !w.rendezvousWarpEngaged():
		// Don't clobber an engaged Sync or node-chase (v0.29 review): the
		// player's later explicit Auto-Warp wins; the mutual arm waits and
		// the coast starts once that driver releases.
		if w.AutoWarp != nil {
			return
		}
		if !sameSubspace(w.Clock.SimTime, partner.SubspaceTime) {
			return
		}
		handle := partner.Handle
		if handle == "" {
			handle = arm.Handle
		}
		w.AutoWarp = &AutoWarpTarget{
			T:                arm.Tau,
			Rendezvous:       true,
			RendezvousOwner:  arm.TargetOwner,
			RendezvousHandle: handle,
		}
		w.Clock.Paused = false
	case partner == nil && w.rendezvousWarpEngaged():
		// Arrival window (v0.29 review): near τ the partner's arm clearing
		// is their own ARRIVAL, not a retract — Δt inside the subspace
		// tolerance means the laggard crosses τ within that window. Finish
		// the coast; resolveAutoWarp fires the arrival normally.
		if w.AutoWarp.T.Sub(w.Clock.SimTime).Seconds() <= coWarpSubspaceToleranceSec {
			return
		}
		// Partner genuinely retracted or dropped mid-coast — release both.
		w.DisengageRendezvousWarp()
	case partner != nil && w.rendezvousWarpEngaged():
		// Hold-the-leader (v0.29 review): a paused partner (frozen clock)
		// or a divergence with the viewer ahead must not let the leader
		// sail to τ alone. The leader freezes (clampedWarp reads the
		// flag); the one behind coasts on and closes the gap; the pair
		// re-locks inside the tolerance. Deadlock-free because only the
		// AHEAD side ever holds.
		ahead := w.Clock.SimTime.Sub(partner.SubspaceTime).Seconds()
		if (partner.Paused && ahead >= 0) || ahead > coWarpSubspaceToleranceSec {
			w.RendezvousHold = true
		}
	}
}

// EngageSyncWarp aims Auto-Warp at a fixed sim-time — Sync to another
// player (v0.27 S7, ADR 0034). Forward only: a target at or behind
// SimTime returns false (the laggard always comes forward; rewinding
// would fork recorded history). handle labels the arrival chip.
// Engaging replaces any node-chase in progress and auto-unpauses.
func (w *World) EngageSyncWarp(target time.Time, owner, handle string) bool {
	if !target.After(w.Clock.SimTime) {
		return false
	}
	// v0.28 S1 (ADR 0034 §5): subspace splits are blocked while co-warped.
	// Syncing to another subspace would warp the viewer away from the
	// player they are coupled to — the couple must break (separate past
	// the hysteresis band) first. min-wins already caps the rate so the
	// warp couldn't make progress; refusing outright is the legible form.
	if w.CoWarpCoupled() {
		return false
	}
	w.AutoWarp = &AutoWarpTarget{T: target, Sync: true, SyncOwner: owner, SyncHandle: handle}
	w.Clock.Paused = false
	return true
}

// soonestEligibleBurn finds the earliest BurnStart among the vessels in
// the active vessel's System whose nodes are resolved, identified, and
// more than autoWarpLeadTime out, and returns that craft+node identity
// and BurnStart. ok=false when none qualifies (or there is no active
// vessel to anchor the System).
//
// Scoped to the active vessel's System (v0.16 / ADR 0015 interaction):
// since the camera follows the active vessel's System, stopping before a
// burn on a vessel in another System would warp to an off-screen event
// and lose the "watch it arm and fire" payoff. Within a System it still
// stops before whatever burn comes first, anyone's (ADR 0016).
func (w *World) soonestEligibleBurn() (craftID, nodeID uint64, burnStart time.Time, ok bool) {
	active := w.ActiveCraft()
	if active == nil {
		return 0, 0, time.Time{}, false
	}
	system := active.SystemIdx
	threshold := w.Clock.SimTime.Add(autoWarpLeadTime)
	for _, c := range w.Crafts {
		if c == nil || c.ID == 0 || c.SystemIdx != system {
			continue
		}
		for i := range c.Nodes {
			n := &c.Nodes[i]
			if !n.IsResolved() || n.ID == 0 {
				continue
			}
			bs := n.BurnStart()
			if !bs.After(threshold) {
				continue
			}
			if !ok || bs.Before(burnStart) {
				craftID, nodeID, burnStart, ok = c.ID, n.ID, bs, true
			}
		}
	}
	return
}

// resolveAutoWarp advances or releases the engaged target each tick,
// called before clampedWarp so the rate this tick reflects the result:
//   - target node gone or unresolved → disengage (Selected Warp kept);
//   - its BurnStart shifted → re-freeze T to track the edit;
//   - SimTime reached T → force Selected Warp to 1× and disengage.
//
// No-op when not engaged.
func (w *World) resolveAutoWarp() {
	if !w.autoWarpEngaged() {
		return
	}
	// Sync mode (v0.27 S7): a fixed sim-time target — nothing to
	// invalidate or re-freeze. The approach term has ramped the rate
	// to the 1× floor by T, so release overshoot is at most one base
	// step.
	if w.AutoWarp.Sync {
		if !w.Clock.SimTime.Before(w.AutoWarp.T) {
			w.Clock.WarpIdx = 0
			w.LastSyncArrival = &SyncArrival{Handle: w.AutoWarp.SyncHandle, Owner: w.AutoWarp.SyncOwner}
			w.DisengageAutoWarp()
		}
		return
	}
	// Rendezvous mode (v0.29 S1): fixed encounter τ, held (never re-frozen
	// like a Sync leader). At τ, hand off to Proximity Co-Warp — drop to
	// 1×, clear the arm, and record the arrival for the S2 chip.
	if w.AutoWarp.Rendezvous {
		if !w.Clock.SimTime.Before(w.AutoWarp.T) {
			w.Clock.WarpIdx = 0
			w.LastRendezvousArrival = &RendezvousArrival{Handle: w.AutoWarp.RendezvousHandle, Owner: w.AutoWarp.RendezvousOwner}
			w.RendezvousArm = nil
			w.DisengageAutoWarp()
		}
		return
	}
	n, ok := w.nodeByID(w.AutoWarp.CraftID, w.AutoWarp.NodeID)
	if !ok || !n.IsResolved() {
		w.DisengageAutoWarp()
		return
	}
	if want := n.BurnStart().Add(-autoWarpLeadTime); !want.Equal(w.AutoWarp.T) {
		w.AutoWarp.T = want // re-freeze on a node edit
	}
	if !w.Clock.SimTime.Before(w.AutoWarp.T) {
		w.Clock.WarpIdx = 0 // hand off to 1× to watch the burn arm
		w.DisengageAutoWarp()
	}
}
