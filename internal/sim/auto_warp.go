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

	// Rendezvous (v0.29 S1, ADR 0034 v0.29 addendum; reshaped for #252):
	// when true the driver is the shared coast of a standing mutual
	// rendezvous intent. T is the CURRENT waypoint's τ — held while
	// coasted at, but re-frozen by driveRendezvousCoast whenever the
	// waypoint advances (τ reached outside couple range) or the partner's
	// earlier relayed τ is adopted. The driver releases only at the
	// proximity handoff (τ reached inside couple range, where Proximity
	// Co-Warp takes over) or on either player's cancel. Started by
	// DriveRendezvousWarp only once both players are armed.
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
// disengage path — [G] toggle, [/] — must clear the arm too, or
// DriveRendezvousWarp restarts the coast (and force-unpauses) on the
// next serve tick, making the cancel a silent no-op. The manual warp
// keys no longer reach here during an engaged coast (#249): their
// intent is "adjust the rate", not "cancel", so the tui refuses them
// with a toast instead of calling this.
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
// driveRendezvousCoast at the proximity handoff (the coast reached a
// waypoint inside couple range, #252), consumed (and cleared) by the
// serve wrapper to fire the arrival chip. Transient, like SyncArrival.
type RendezvousArrival struct {
	Handle string // the partner whose encounter we arrived at
	Owner  string // their fingerprint
}

// RendezvousWaypoint marks a passed waypoint on a standing rendezvous
// intent (#252) — set by driveRendezvousCoast when the coast reaches the
// committed τ outside couple range and advances to a newly derived
// encounter. Consumed (and cleared) by the serve wrapper to fire the
// waypoint chip: an advance must be visible (a silent one reads as the
// coast being broken), but it is neither an arrival nor a cancel.
type RendezvousWaypoint struct {
	Handle string // the partner the intent is held with
	Owner  string // their fingerprint
}

// EngageRendezvousWarp records the viewer's Rendezvous Warp intent toward
// partner, committed to the encounter sim-time tau — the initiator's
// authoritative TCA, which becomes the standing intent's FIRST waypoint
// (#252) (v0.29 S1, ADR 0034 v0.29 addendum). handle is the
// partner's display name, captured here so chips and the HUD never have
// to resolve a fingerprint through a possibly-stale roster. It does NOT
// start the shared coast: DriveRendezvousWarp starts it only once the
// partner has Engaged back, so the first to Engage never warps solo.
// Forward-only (tau at/behind SimTime is refused — the laggard Syncs
// forward). Replaces any prior arm.
func (w *World) EngageRendezvousWarp(partner, handle string, tau time.Time, committedCA float64) bool {
	return w.EngageRendezvousWarpAs(partner, handle, tau, committedCA, false)
}

// EngageRendezvousWarpAs is EngageRendezvousWarp with the seat named
// (ADR 0037 §2). The Session screen's row action arms as the INITIATOR —
// pilot-in-command of the pair's time once the terminal phase begins —
// and the main-screen [y] join arms as the copilot, which is what the
// plain EngageRendezvousWarp wrapper above does. Roles are fixed here, at
// invite time, and relayed, so neither side can drift into disagreeing
// about who flies the clock.
func (w *World) EngageRendezvousWarpAs(partner, handle string, tau time.Time, committedCA float64, initiator bool) bool {
	if !tau.After(w.Clock.SimTime) {
		return false
	}
	// The acting craft is captured here for the same reason the handle is
	// (#295): arming acts on whatever craft is active, and a player who
	// cycled their slot earlier has no other way to see which vessel just
	// committed to the encounter.
	craftName := ""
	if c := w.ActiveCraft(); c != nil {
		craftName = c.Name
	}
	w.RendezvousArm = &RendezvousArm{
		TargetOwner: partner, Handle: handle, CraftName: craftName,
		Tau: tau, CommittedCA: committedCA,
		Initiator: initiator,
		BrakeIdx:  rendezvousFollowing,
	}
	return true
}

// rendezvousFollowing is RendezvousArm.BrakeIdx's "no brake" value — the
// copilot's default seat behaviour (ADR 0037 §2). Not zero, which is a
// real 1× brake.
const rendezvousFollowing = -1

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

// rendezvousApproachPhase reports whether a demoted agreement is standing:
// the τ handoff has happened, the driver is released and the pilot is
// flying the terminal phase by hand, but the pair is still time-locked
// (ADR 0037 §1).
func (w *World) rendezvousApproachPhase() bool {
	return w.RendezvousArm != nil && w.RendezvousArm.Approach
}

// RendezvousApproachPhase is the exported form for the tui — the
// RENDEZVOUS chip's standing approach state and the copilot's warp-key
// semantics both fork on it.
func (w *World) RendezvousApproachPhase() bool { return w.rendezvousApproachPhase() }

// rendezvousRateGoverned reports whether the agreement itself is setting
// the pair's rate — the shared coast pre-τ (τ-derived on both sides) or
// the initiator's clock in the terminal phase (ADR 0037 §2). Either way
// the partner's relayed Effective warp must NOT feed the co-warp min:
// that is the #248 stale-report ratchet, and both phases derive the rate
// from inputs neither side reads back off the other.
func (w *World) rendezvousRateGoverned() bool {
	return w.rendezvousWarpEngaged() || w.rendezvousApproachPhase()
}

// EndRendezvousOnDock releases a standing agreement with partner because
// the pair have docked — one of ADR 0037 §1's exactly two end conditions
// (the other being an explicit cancel). Reports whether it ended one, so
// the serve layer can suppress the cancel chip: docking is the rendezvous
// succeeding, and the dock's own moment already says so.
func (w *World) EndRendezvousOnDock(partner string) bool {
	if w.RendezvousArm == nil || w.RendezvousArm.TargetOwner != partner {
		return false
	}
	w.DisengageRendezvousWarp()
	return true
}

// RendezvousInvite is a peer's live Rendezvous Warp intent aimed at the
// viewer, awaiting a response (v0.29 S2): who, and the committed τ +
// predicted approach the responder adopts verbatim on join. The World
// slate field of the same name carries at most one (pairwise MVP).
type RendezvousInvite struct {
	Owner     string    // partner fingerprint — EngageRendezvousWarp's target on respond
	Handle    string    // display name for the prompt/chip
	CraftName string    // the vessel the initiator armed (#295) — empty when their report carries no marker
	Tau       time.Time // the initiator's committed encounter sim-time
	CA        float64   // m — the initiator's committed predicted approach

	// Blocked marks an invite from a subspace-diverged peer (#250): the
	// intent is live, but the coast could never start across the gap, so
	// the prompt renders as a non-joinable attribution ([y] suppressed,
	// the direction-correct Sync named as the way in) instead of silently
	// vanishing. AheadBy is the signed viewer-minus-initiator subspace
	// offset (positive: the viewer is ahead — Sync is forward-only, so
	// then the initiator is the one who must Sync). Both zero while
	// joinable.
	Blocked bool
	AheadBy time.Duration
}

// refreshRendezvousInvite rebuilds the invite slate from this tick's
// peer set (v0.29 S2). At most one invite surfaces: the first armed
// peer with a still-future τ, and only while the viewer has no outgoing
// arm — once mutually armed (or armed elsewhere) there is nothing to
// respond to. A past-τ arm is dropped here rather than surfaced, since
// Engage would refuse it (forward-only).
//
// A subspace-diverged peer's arm is kept but Blocked (#250) rather than
// dropped: Engage would succeed yet the coast could never start, so the
// join affordance is a lie — but so is a prompt that vanishes without
// attribution. A joinable invite always wins over a blocked one; the
// blocked one turns joinable again if the pair converges.
func (w *World) refreshRendezvousInvite(peers []CoWarpPeer) {
	w.RendezvousInvite = nil
	if w.RendezvousArm != nil {
		return
	}
	var blocked *RendezvousInvite
	for i := range peers {
		p := &peers[i]
		if !p.ArmedTowardViewer || !p.RendezvousTau.After(w.Clock.SimTime) {
			continue
		}
		if sameSubspace(w.Clock.SimTime, p.SubspaceTime) {
			w.RendezvousInvite = &RendezvousInvite{
				Owner: p.Owner, Handle: p.Handle, CraftName: p.ActiveCraftName,
				Tau: p.RendezvousTau, CA: p.RendezvousCA,
			}
			return
		}
		if blocked == nil {
			blocked = &RendezvousInvite{
				Owner: p.Owner, Handle: p.Handle, CraftName: p.ActiveCraftName,
				Tau: p.RendezvousTau, CA: p.RendezvousCA,
				Blocked: true, AheadBy: w.Clock.SimTime.Sub(p.SubspaceTime),
			}
		}
	}
	w.RendezvousInvite = blocked
}

// RendezvousWaitReason classifies why an armed Rendezvous Warp has not
// started coasting (#250). Deliberately an enum-plus-data shape rather
// than a bag of bools — #221's CommGraph work wants the same "classify
// the reason and say it" pattern, so the vocabulary should converge.
type RendezvousWaitReason int

const (
	// RendezvousWaitNone: no arm held, or the shared coast is running.
	RendezvousWaitNone RendezvousWaitReason = iota
	// RendezvousWaitPartner: genuinely waiting — the partner has not
	// Engaged back (or has no report in this tick's peer set).
	RendezvousWaitPartner
	// RendezvousWaitSubspaceGap: the pair has diverged past
	// CoWarpSubspaceTolerance, so the coast cannot start no matter what
	// the partner does — Sync is the only way back.
	RendezvousWaitSubspaceGap
	// RendezvousWaitSelf: the partner HAS armed back, but the viewer's
	// own non-rendezvous Auto-Warp (a Sync or node-chase) is engaged —
	// driveRendezvousCoast defers the coast start rather than clobber it
	// (#260), so the wait is the viewer's own doing, not the partner's.
	RendezvousWaitSelf
)

// RendezvousWait is the classified armed-but-not-coasting slate (#250):
// the reason the coast has not started, plus the signed viewer-minus-
// partner subspace offset when that reason is a gap (positive: the
// viewer warped ahead). Zero value when idle or coasting.
type RendezvousWait struct {
	Reason  RendezvousWaitReason
	AheadBy time.Duration // gap direction/magnitude; zero unless Reason is SubspaceGap
}

// refreshRendezvousWait reclassifies the armed-but-not-coasting state
// each tick (#250), after driveRendezvousCoast so it reflects this
// tick's engaged state. Set/cleared only here — same single-writer
// ownership as RendezvousHold and the invite slate. The gap check reads
// the partner's report regardless of ArmedTowardViewer: the initiator
// case (partner not yet armed back) is exactly where the divergence is
// otherwise invisible.
func (w *World) refreshRendezvousWait(peers []CoWarpPeer) {
	w.RendezvousWait = RendezvousWait{}
	arm := w.RendezvousArm
	// A demoted agreement is not waiting for anything (ADR 0037 §1) — the
	// coast is over by design, not deferred, so classifying it here would
	// put the armed-waiting chip back up behind the terminal phase.
	if arm == nil || arm.Approach || w.rendezvousWarpEngaged() {
		return
	}
	for i := range peers {
		p := &peers[i]
		if p.Owner != arm.TargetOwner {
			continue
		}
		// Own-driver deferral (#260): the partner HAS armed back, but the
		// viewer's own Sync or node-chase holds the driver, so
		// driveRendezvousCoast defers the coast start. The predicate is an
		// exact mirror of that don't-clobber branch (partner armed back +
		// `w.AutoWarp != nil` with the coast not engaged), so the
		// classifier can never disagree with the driver. Checked BEFORE
		// the gap — also mirroring the driver's order — because Self is
		// the actionable reason: the player must release their own driver
		// first regardless, and that driver may be the very Sync that is
		// closing the gap ("Sync to rejoin" while already Syncing would
		// be nonsense advice).
		if p.ArmedTowardViewer && w.AutoWarp != nil && !w.AutoWarp.Rendezvous {
			w.RendezvousWait = RendezvousWait{Reason: RendezvousWaitSelf}
			return
		}
		if !sameSubspace(w.Clock.SimTime, p.SubspaceTime) {
			w.RendezvousWait = RendezvousWait{
				Reason:  RendezvousWaitSubspaceGap,
				AheadBy: w.Clock.SimTime.Sub(p.SubspaceTime),
			}
			return
		}
		break
	}
	w.RendezvousWait = RendezvousWait{Reason: RendezvousWaitPartner}
}

// DriveRendezvousWarp starts, holds, advances, or cancels the shared
// coast of the standing rendezvous intent from this tick's mutual-arm
// state (v0.29 S1, reshaped for #252). Called each tick after the
// co-warp peers are built. The coast starts only once BOTH players are
// armed toward each other in the same Subspace (no solo drift); a
// genuine retract or disconnect mid-coast cancels — either side's cancel
// releases both. Reaching the committed τ is resolved HERE, not in
// resolveAutoWarp, because deciding between the proximity handoff and a
// waypoint advance needs this tick's peer set (craft ranges + relayed
// τs), which the sim tick path doesn't carry. An armless world with the
// coast still flagged just releases it here defensively.
func (w *World) DriveRendezvousWarp(peers []CoWarpPeer) {
	w.driveRendezvousCoast(peers)
	// One shared tail (v0.29 review): the wait classification and degrade
	// recompute reflect this tick's engaged state, and the invite refresh
	// runs after start/cancel so a retract this tick can immediately
	// surface another pending arm (the unarmed viewer is the responder
	// case).
	w.refreshRendezvousWait(peers)
	w.refreshRendezvousDegrade(peers)
	// The seat/rate slate is part of the same tail (ADR 0037 §2): it reads
	// this tick's peer set and the phase the drive above just settled, and
	// clampedWarp reads it on the sim tick that follows.
	w.refreshRendezvousRate(peers)
	w.refreshRendezvousInvite(peers)
}

func (w *World) driveRendezvousCoast(peers []CoWarpPeer) {
	w.RendezvousHold = false
	w.RendezvousPartnerAway = false
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
	// all future invites suppressed). A DEMOTED arm is exempt (ADR 0037 §1):
	// its τ is deliberately in the past — the handoff happened there — and
	// the terminal phase it now stands for has no time bound at all.
	if !w.rendezvousWarpEngaged() && !arm.Approach && !arm.Tau.After(w.Clock.SimTime) {
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
	// as a retract and destroy the mutual encounter. peerPresent tells the
	// waypoint resolution apart the two ways partner can be nil (#252
	// review, finding 3): report in the set with the arm withdrawn — a
	// genuine retract (the serve liveness gate also lands here for a dead
	// session, finding 1) — versus the whole peer absent this tick, which
	// at/after τ gets the dropout grace instead of an instant release.
	var partner *CoWarpPeer
	peerPresent := false
	for i := range peers {
		p := &peers[i]
		if p.Owner != arm.TargetOwner {
			continue
		}
		peerPresent = true
		if p.ArmedTowardViewer {
			partner = p
		}
		break
	}
	// Mirror the armed partner's Away onto the slate (#253): a standing
	// state the chip renders for as long as it is true, cleared at the
	// top of every drive tick like RendezvousHold. State, not an event —
	// the 6 s went-quiet chip expires while Away lasts hours by design.
	// Must run on EVERY tick with a matched partner, so it sits here,
	// before any of the branches/returns below: the τ-resolution path
	// (#252) can idle past τ for hours retrying a failing derivation and
	// return early each tick — exactly the stretch an asleep partner
	// produces (they can't burn the next encounter into existence), so
	// mirroring after that return would blank the away line in precisely
	// its motivating scenario (#253).
	w.RendezvousPartnerAway = partner != nil && partner.Away
	// Terminal phase (ADR 0037 §1): the agreement is demoted, not ended —
	// no driver, no waypoints, no τ. All that is left to drive is its
	// lifetime (retract / dropout grace) and the leader hold that keeps the
	// pair inside one subspace while the pilot brakes.
	if arm.Approach {
		w.driveRendezvousApproach(arm, partner, peerPresent)
		return
	}
	// τ authority across a waypoint advance (#252): both sides re-derive
	// the next waypoint independently at their own τ-crossing, so their
	// new τs will disagree (different advisories, different relayed craft
	// states). Deterministic rule, no negotiation loop: the EARLIER future
	// τ wins (min-τ) — min is commutative and idempotent, so both sides
	// converge on the same waypoint within one relay hop, leaning on the
	// re-freeze below to keep the driver, the arm, and the wire agreeing.
	// The future-only guard ignores the partner's still-relayed PREVIOUS τ
	// (now in the past) in the ticks right after an advance; it also lets
	// a side whose own derivation failed (stale past τ, coasting at the 1×
	// floor) adopt the partner's fresh waypoint as a rescue. Residual skew
	// while converging is absorbed by the same-subspace tolerance and the
	// hold. Documented trade-off: a deliberate re-Engage to a LATER τ
	// mid-coast cannot win against the partner's earlier relayed τ — with
	// the arm a standing intent, a waypoint is sequencing, not commitment,
	// and the earlier encounter is always an acceptable next waypoint.
	// The min-lead floor (#252 review, finding 4) applies to adoption
	// exactly as it does to our own derivation: a relayed τ 1 s out would
	// be crossed next tick, forcing an immediate re-resolution and a
	// spurious waypoint chip. Deterministic on both sides — the floor is a
	// shared constant over each side's own clock, and skipping changes
	// nothing about convergence: a skipped sub-lead τ means we cross our
	// own τ and re-derive, the partner crosses theirs within the lead and
	// re-derives too (their derivation refuses sub-lead τs), and min-τ
	// then converges the two fresh waypoints within one relay hop as usual.
	if partner != nil && w.rendezvousWarpEngaged() &&
		partner.RendezvousTau.After(w.Clock.SimTime.Add(rendezvousWaypointMinLead)) &&
		(partner.RendezvousTau.Before(arm.Tau) || !arm.Tau.After(w.Clock.SimTime)) {
		// ADR 0039 S3 (batch-review follow-up, PR #319): adoption is a
		// waypoint transition exactly like resolveRendezvousWaypoint's own
		// derivation below — the degradeBaseSet reset on the next line
		// already treats it that way ("a new waypoint means a new
		// baseline"), so the CA trend row must too, or PrevCommittedCA goes
		// stale across an adoption and the row can compare across TWO
		// transitions instead of one, misreporting the direction.
		arm.PrevCommittedCA, arm.PrevCommittedCASet = arm.CommittedCA, true
		arm.Tau, arm.CommittedCA = partner.RendezvousTau, partner.RendezvousCA
		arm.degradeBaseSet = false // a new waypoint means a new baseline (#251 interaction)
		w.AutoWarp.T = arm.Tau
	}
	// τ reached (#252): the arm is a standing mutual intent — the
	// committed encounter is a waypoint, not the end.
	if w.rendezvousWarpEngaged() && !w.Clock.SimTime.Before(arm.Tau) {
		w.resolveRendezvousWaypoint(arm, partner, peerPresent, peers)
		// Hold-the-leader applies whenever the coast is engaged, waypoint
		// resolved or not (#252 review, finding 2). The past-τ idle
		// (derivation failing and retrying — a legitimately long-lived
		// state under the standing intent) used to return before the hold
		// case below, so a paused or behind-diverged partner no longer
		// froze the ahead side: the viewer walked on at the 1× floor,
		// the pair decoupled past the tolerance window, and both then sat
		// at 1× with a live arm and a gap that never closes. Evaluated
		// after the resolution so a handoff/release this tick (coast now
		// disengaged) is never held.
		if w.rendezvousWarpEngaged() && partner != nil {
			w.holdRendezvousLeader(partner)
		}
		return
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
		// Arrival window (v0.29 review, re-read for #252): near τ the
		// partner's arm clearing is their own proximity handoff, not a
		// retract — Δt inside the subspace tolerance means the laggard
		// crosses τ within that window. Finish the coast; the waypoint
		// resolution above then decides handoff-vs-advance at τ.
		if w.AutoWarp.T.Sub(w.Clock.SimTime).Seconds() <= coWarpSubspaceToleranceSec {
			return
		}
		// Partner genuinely retracted or dropped mid-coast — release both.
		w.DisengageRendezvousWarp()
	case partner != nil && w.rendezvousWarpEngaged():
		w.holdRendezvousLeader(partner)
	}
}

// driveRendezvousApproach runs one tick of the demoted terminal phase
// (ADR 0037 §1). The agreement ends exactly two ways here — an explicit
// cancel by either side (the partner's retract arrives as their arm
// vanishing from a report that is still present) and the dock
// (EndRendezvousOnDock, driven from the serve layer's ledger). Everything
// else is held through, on purpose: there is no distance tripwire, no τ
// expiry, and a partner who has gone quiet is waited for.
//
// The lifetime rules are lifted verbatim from the coast so the two phases
// can't disagree about what a missing peer means: a whole-peer dropout
// gets the tolerance-window grace (a single filtered tick must not tear
// down a live rendezvous), while a present peer with the arm withdrawn —
// which is also where the serve liveness gate lands a dead session — is a
// genuine retract and releases at once.
func (w *World) driveRendezvousApproach(arm *RendezvousArm, partner *CoWarpPeer, peerPresent bool) {
	if partner == nil {
		if peerPresent {
			w.DisengageRendezvousWarp()
			return
		}
		if arm.peerGoneAt.IsZero() {
			arm.peerGoneAt = w.Clock.SimTime
			return
		}
		if w.Clock.SimTime.Sub(arm.peerGoneAt).Seconds() > coWarpSubspaceToleranceSec {
			w.DisengageRendezvousWarp()
		}
		return
	}
	arm.peerGoneAt = time.Time{} // peer back inside the grace — full window next time
	w.holdRendezvousLeader(partner)
}

// holdRendezvousLeader raises the hold flag when the engaged coast's
// viewer must wait for its partner (v0.29 review): a paused partner
// (frozen clock) or a divergence with the viewer ahead must not let the
// leader sail on alone. The leader freezes (clampedWarp reads the flag);
// the one behind coasts on and closes the gap; the pair re-locks inside
// the tolerance. Deadlock-free because only the AHEAD side ever holds.
// Applies to every engaged coasting tick — pre-τ and the past-τ idle
// alike (#252 review, finding 2).
func (w *World) holdRendezvousLeader(partner *CoWarpPeer) {
	ahead := w.Clock.SimTime.Sub(partner.SubspaceTime).Seconds()
	if (partner.Paused && ahead >= 0) || ahead > coWarpSubspaceToleranceSec {
		w.RendezvousHold = true
	}
}

// resolveRendezvousWaypoint decides what reaching the committed τ means
// for the standing intent (#252):
//   - inside the couple RANGE — velocity term deliberately not consulted
//     (#299) → the intent has done its job: drop to 1×, chip the arrival,
//     release arm + driver. A transfer-orbit encounter always arrives
//     fast, and killing that velocity needs the pilot burning at closest
//     approach, which needs the driver released — requiring the velocity
//     term here made the coast blow through every good encounter. The
//     velocity term still gates Proximity Co-Warp coupling itself: a slow
//     arrival continues the SAME coupled state (the wasCoupled hysteresis
//     in ComputeCoWarp carries it) with no drop-and-recouple tick, a fast
//     arrival is released-but-not-coupled and couples once the pilot has
//     matched velocities;
//   - outside, mutual arm intact → advance: re-derive the next encounter,
//     re-freeze the driver on it, re-base the degrade baseline, and
//     record the advance for the waypoint chip;
//   - outside, peer present but arm gone → the partner cancelled at the
//     waypoint (or handed off on a range measurement we don't share, or
//     their session died and the liveness gate suppressed their frozen
//     arm — finding 1): there is no standing intent left to advance, so
//     release both immediately, consistent with the mid-coast retract
//     rule;
//   - outside, peer absent from the set entirely → dropout grace (#252
//     review, finding 3): every pre-τ transient gap is held through, so a
//     one-tick peer dropout at exactly the crossing tick (all partner
//     craft filtered that tick) must not destroy the standing intent and
//     mis-chip "cancelled". Idle — don't advance, don't release — until
//     the peer has been absent for more than the tolerance window's worth
//     of sim-time (arm.peerGoneAt), then release as a retract.
//
// The handoff check deliberately runs only AT a waypoint, not every
// coasting tick: a transient pass through couple range at warp mid-coast is a
// crossing, not the rendezvous being complete, and must not strand the
// pair at 1× short of their encounter. At τ the approach ramp has already
// glided the rate to the 1× floor, which is exactly the state Proximity
// Co-Warp expects to inherit.
func (w *World) resolveRendezvousWaypoint(arm *RendezvousArm, partner *CoWarpPeer, peerPresent bool, peers []CoWarpPeer) {
	anchor := w.ActiveCraft()
	if anchor != nil && !anchor.Landed && !anchor.Crashed {
		// Partner matched by identity alone — at the handoff the partner's
		// own arm may already be cleared (their side resolved first), so
		// ArmedTowardViewer is deliberately not required here.
		for i := range peers {
			if peers[i].Owner != arm.TargetOwner {
				continue
			}
			if rng, _, ok := closestApproach(anchor, peers[i].Crafts); ok &&
				rng <= coWarpCoupleRangeM {
				w.Clock.WarpIdx = 0
				w.LastRendezvousArrival = &RendezvousArrival{
					Handle: w.AutoWarp.RendezvousHandle, Owner: w.AutoWarp.RendezvousOwner,
				}
				// ADR 0037 §1: DEMOTE, don't end. #299's handoff is kept
				// exactly — the driver goes and the ship is handed back at 1×
				// so the pilot can brake at closest approach — but the mutual
				// agreement survives into the terminal phase, which is the
				// stretch it was invented for (#302: 32 real minutes of 1×
				// waiting between braking burns, with solo warp splitting the
				// subspaces and proximity co-warp unable to couple until
				// |v_rel| is under the very number being worked down).
				arm.Approach = true
				arm.BrakeIdx = rendezvousFollowing
				arm.degradeBaseSet = false
				arm.peerGoneAt = time.Time{}
				// Not DisengageAutoWarp: that clears the arm on every path by
				// design (#249/#259), which is exactly what must not happen here.
				w.AutoWarp = nil
				return
			}
			break
		}
	}
	if partner == nil {
		// Peer present with the arm withdrawn is a genuine retract (or the
		// liveness gate's dead-session suppression) — release immediately,
		// as mid-coast. Only a whole-peer dropout gets the grace.
		if peerPresent {
			w.DisengageRendezvousWarp()
			return
		}
		if arm.peerGoneAt.IsZero() {
			arm.peerGoneAt = w.Clock.SimTime
			return // idle: clampedWarp floors the past-τ coast at 1×
		}
		if w.Clock.SimTime.Sub(arm.peerGoneAt).Seconds() > coWarpSubspaceToleranceSec {
			w.DisengageRendezvousWarp()
		}
		return
	}
	arm.peerGoneAt = time.Time{} // peer back inside the grace — full window next time
	if tau, ca, ok := w.rendezvousNextWaypoint(partner); ok {
		// ADR 0039 S3: capture the outgoing CA as the trend row's "previous"
		// point before it's overwritten — must happen every advance, not
		// just the first, so the trend always compares the two most recent
		// waypoints.
		arm.PrevCommittedCA, arm.PrevCommittedCASet = arm.CommittedCA, true
		arm.Tau, arm.CommittedCA = tau, ca
		arm.degradeBaseSet = false // per-waypoint re-baseline (#251 interaction)
		w.AutoWarp.T = tau
		w.LastRendezvousWaypoint = &RendezvousWaypoint{
			Handle: w.AutoWarp.RendezvousHandle, Owner: w.AutoWarp.RendezvousOwner,
		}
		return
	}
	// Derivation came up empty this tick (partner craft missing from the
	// report, or no approach inside the horizon). NOT a cancel — the
	// intent ends only on explicit cancel or the proximity handoff. Keep
	// the passed τ: clampedWarp floors a past-τ rendezvous coast at 1×,
	// and we retry every tick; the partner's relayed τ can also rescue us
	// via the min-τ adoption above.
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
	// Rendezvous mode (v0.29 S1, reshaped for #252): the arm is a standing
	// mutual intent and T is only the CURRENT waypoint, so reaching it is
	// NOT resolved here — deciding between the proximity handoff and a
	// waypoint advance needs the peer set, which this sim-tick path never
	// sees. driveRendezvousCoast resolves it on the serve pass of the same
	// tick; until then clampedWarp floors a past-T rendezvous coast at 1×
	// so nothing races ahead of an unresolved waypoint.
	if w.AutoWarp.Rendezvous {
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
