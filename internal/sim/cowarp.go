package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Proximity co-warp (v0.28 S1, ADR 0034 §5 + the 2026-07-14 addendum).
//
// Two players whose active craft sit in the same system + SOI primary +
// subspace and close together are "warp-coupled": their Effective Warp
// becomes the min over the coupled players' Effective warps, so a
// partner's 10× burn cap (or lower selection) propagates and both step
// in lock. This is a new member of the Effective-≤-Selected clamp family
// in clampedWarp — not a parallel mechanism — plus a split guard
// (EngageSyncWarp refuses while coupled). Chains A–B–C propagate through
// the min automatically because each player reports its *post-co-warp*
// Effective warp; no transitive-closure logic is needed (MVP tests at 2).
//
// The gate carries a velocity term (ADR addendum): a fast flyby that
// merely passes through the radius is a crossing, not a rendezvous, and
// must not couple. Decouple only past a hysteresis band so station-
// keeping at the boundary can't flap the clamp or spam chips.

const (
	// coWarpCoupleRangeM / coWarpCoupleSpeedMs are the couple gate: the
	// anchor and a peer craft must be within BOTH to begin coupling.
	// 35 km is a rendezvous neighbourhood — the v0.32 live playtest moved
	// it up from 10 km (#291): a real rendezvous transiently passed
	// ~14.9 km without coupling, and comparable games / real proximity
	// ops begin the terminal phase at 20–50 km (35 is that band's middle).
	// 100 m/s |v_rel| is slow enough to be station-keeping rather than a
	// flyby; the velocity term drew no complaints live and stayed put.
	// Tunables.
	coWarpCoupleRangeM  = 35_000.0
	coWarpCoupleSpeedMs = 100.0

	// coWarpDecoupleRangeM / coWarpDecoupleSpeedMs are the decouple gate.
	// Wider than the couple gate on purpose (hysteresis): once coupled,
	// separation past 42 km OR 120 m/s is required to release. The band
	// between the two gates is where a coupled pair stays coupled, so
	// small station-keeping excursions across 35 km / 100 m/s don't flap
	// the clamp or re-emit couple/release chips every tick. The range
	// scales with the couple range (same 1.2 ratio as the pre-#291
	// 10/12 km pair). Tunables.
	coWarpDecoupleRangeM  = 42_000.0
	coWarpDecoupleSpeedMs = 120.0

	// coWarpSubspaceToleranceSec bounds |Δt| between the viewer's sim-time
	// and a peer's subspace time for the pair to count as "same subspace".
	// Load-bearing: it rejects coupling to a time-shifted ghost whose
	// coasting orbit merely passes through the radius at the viewer's
	// clock — a different subspace, not a rendezvous (ADR §5). Generous
	// against the seconds of report/tick lag between genuinely co-warped
	// players (min-wins keeps their clocks locked, so Δt stays small),
	// tight against the hours/days a real subspace divergence spans.
	// Tunable.
	coWarpSubspaceToleranceSec = 120.0

	// coWarpStepSafety is how many ticks of the same-subspace window a
	// coupled player must leave in hand (#244). The min-warp clamp is
	// derived from the partner's relayed report, so it is never fresher
	// than the previous tick; capping the per-tick advance at the full
	// window would put a pair exactly on the edge after one unclamped
	// step and over it after any additional report lag. Two leaves a
	// whole tick of slack. Tunable.
	coWarpStepSafety = 2.0

	// degradeSlipFrac scales the degrade bar to the encounter it guards
	// (#251): the watchdog trips at max(coWarpCoupleRangeM, degradeSlipFrac
	// × baseline CA). The quantity thresholded is a PREDICTION — the
	// approach at τ from Kepler-stepping both craft across the remaining
	// horizon — so its error scales with the encounter's inputs (a 1 m/s
	// relayed-velocity error is tens of km at τ over a multi-hour coast),
	// while a fixed couple-radius bar read 0.18% of a 5,605 km encounter
	// as "partner drifted off the plan". 5% tolerates ~280 km on that
	// encounter yet keeps a genuine 50 km drift on a close one firing
	// (the coWarpCoupleRangeM floor still stands). Tunable.
	degradeSlipFrac = 0.05
)

// degradeRebaseAfter is how much sim-time may pass before a HEALTHY
// degrade baseline is recaptured from the current estimate (#251). The
// τ-prediction converges as the horizon shrinks, so comparing forever
// against the coast-start capture — the worst estimate ever taken —
// slowly accumulates pure estimator convergence into a trip. Re-basing
// while healthy keeps the reference recent (convergence creep per window
// is far under the bar), while a real maneuver's step-change lands within
// one tick against a ≤window-old baseline and still fires. The baseline
// FREEZES while degraded, so a genuine drift stays flagged instead of
// being absorbed by the next recapture. Tunable.
//
// Accepted blind spot: genuine drift that stays under the bar within
// every single window is absorbed into the recaptured baseline and never
// trips, no matter how far it compounds — the price of option 2 under
// #251. An impulsive burn can't slip through this way (its step-change
// lands whole against a ≤window-old baseline), and slow drift on the
// scale that matters here comes from low-thrust cheating over hours,
// which the encounter's own approach readout still exposes.
const degradeRebaseAfter = 10 * time.Minute

// CoWarpSubspaceTolerance is the exported form of the same-subspace gate
// (v0.29 S2): the Session screen's Rendezvous Warp row action refuses a
// partner whose |Δt| exceeds it ("Sync first") so the arm can actually
// couple rather than sit dead across a subspace divergence.
const CoWarpSubspaceTolerance = time.Duration(coWarpSubspaceToleranceSec) * time.Second

// CoWarpCraft is one peer craft placed in the anchor's frame — primary-
// relative position/velocity already propagated to the viewer's sim-time
// (the relay adapter Kepler-steps the last report across the subspace
// gap, exactly like a ghost). Only the SOI primary ID and the state
// vector are needed to gate range + |v_rel| against the viewer's active
// craft.
type CoWarpCraft struct {
	Primary string       // SOI primary ID; must match the anchor's to gate
	R       orbital.Vec3 // primary-relative position at the viewer's sim-time
	V       orbital.Vec3 // primary-relative velocity at the viewer's sim-time
}

// CoWarpPeer is one other player's co-warp contribution: their identity,
// subspace time, current Effective warp (for the min), and their craft
// in the viewer's system. Built by the relay adapter (CoWarpPeersFrom)
// from the store's reports — the sim-level, relay-agnostic input so the
// couple/decouple math + constants stay in sim (the clamp's home) while
// sim never imports the wire types above it.
type CoWarpPeer struct {
	Owner        string
	Handle       string
	SubspaceTime time.Time
	EffWarp      float64
	Crafts       []CoWarpCraft

	// Paused distinguishes a deliberately paused partner (frozen clock)
	// from one merely reporting EffWarp 0 — the rendezvous hold keys on
	// it (v0.29 review). EffWarp alone can't carry this: a held viewer
	// also reports 0, and treating any 0 as "paused" would deadlock both
	// sides into mutual holds.
	Paused bool

	// Away is the peer's live nobody-at-the-controls state (#253, ADR
	// 0036): their session is still simulating — a Commitment Reprieve
	// holds it open — but nobody is watching. Set by the relay adapter
	// from the server's per-session liveness; DriveRendezvousWarp mirrors
	// the armed partner's value onto the world slate so the flight view
	// carries a STANDING indication instead of only the 6 s went-quiet
	// chip. False for an offline peer — that is a different thing (their
	// craft stop flying), and it is the report set, not this flag, that
	// goes empty then.
	Away bool

	// ArmedTowardViewer is set by the relay adapter when this peer has a
	// live Rendezvous Warp intent aimed at the viewer (v0.29 S1, ADR 0034
	// v0.29 addendum). Combined with the viewer's own RendezvousArm
	// targeting this peer, the two are *mutually* armed and couple before
	// the proximity gate — the second Co-Warp trigger.
	//
	// "Live" means a live SESSION, not merely a live report (#252 review):
	// the adapter suppresses this flag (and RendezvousTau/CA) for an owner
	// with no live session, so a disconnected-for-good partner's frozen
	// report reads as a retract here rather than an immortal arm. A
	// reprieved-away session still counts as live — its silence is held
	// for, never cancelled.
	ArmedTowardViewer bool

	// RendezvousTau is the peer's committed encounter sim-time when
	// ArmedTowardViewer — the initiator's authoritative TCA, which the
	// responder adopts verbatim when it Engages back (v0.29 S1). Zero when
	// the peer is not armed toward the viewer.
	RendezvousTau time.Time

	// RendezvousCA is the peer's committed predicted approach at Tau (m) —
	// carried alongside RendezvousTau so a responder adopts the initiator's
	// authoritative baseline, not its own staler recompute (v0.29 S1).
	RendezvousCA float64

	// ActiveCraftName is the vessel this peer is flying, read off their
	// report's active-craft marker (#288). The join prompt names it (#295)
	// so the responder answers "gern's Relay Tug-1 wants to rendezvous",
	// not just "gern" — the live wrong-vessel arm was caught from this
	// seat, and only by an implausible CA. Empty when the peer's report
	// carries no marker.
	ActiveCraftName string
}

// RendezvousArm is the viewer's outgoing Rendezvous Warp intent (v0.29 S1,
// ADR 0034 v0.29 addendum): the partner they have Engaged toward, plus the
// CURRENT waypoint — encounter sim-time and predicted approach. Since #252
// the arm is a STANDING mutual intent ("we are rendezvousing"), not a
// commitment to one encounter: reaching Tau outside couple range advances
// the waypoint (Tau/CommittedCA are re-derived and the coast continues)
// rather than clearing the arm, so the range-free coupling and rate-lock
// span the whole multi-maneuver approach. The arm ends exactly two ways
// by intent: an explicit cancel by either player, or the proximity
// handoff when a waypoint arrives inside couple range and Proximity
// Co-Warp takes over.
//
// Lifetime requires a live partner SESSION (#252 review, finding 1).
// With the arm unbounded in time, "the partner is still in this" has to
// mean their session exists — attended or reprieved-away — not that a
// report of theirs exists: the relay store never scrubs reports, so a
// partner who disconnects for good leaves a frozen report whose intent
// bit would otherwise hold this arm (and its 0×-hold or dead-orbit
// coast) forever. The serve layer enforces it at the peer seam
// (relay.CoWarpPeersFrom's liveness input): a dead session's relayed arm
// is suppressed, which the coast reads as a genuine retract — so a
// partner disconnect releases the arm through the normal cancel path,
// not through any wire message the departed session never got to send.
// Transient like AutoWarp/CoWarp — never persisted.
type RendezvousArm struct {
	TargetOwner string    // fingerprint of the partner Engaged toward
	Handle      string    // partner display name, captured at Engage (chips/HUD never fall back to a raw fingerprint)
	CraftName   string    // the vessel that armed — captured at Engage (#295), so a wrong-vessel arm is visible from the arming seat
	Tau         time.Time // the current waypoint's absolute encounter sim-time
	CommittedCA float64   // m — the predicted approach at Tau, re-derived per waypoint (HUD "committed" row)

	// degradeBaseCA is the hold-τ warning baseline: the most recent
	// HEALTHY approach measured while the shared coast runs (v0.29
	// review, re-based per #251). CommittedCA can't serve as the baseline
	// — on the advisory path it is the POST-burn promise while the
	// recompute measures the current ballistic course, which would flag
	// "degraded" from the first tick with nothing drifted. Nor can the
	// coast-START measure serve forever: the approach is a prediction
	// whose error shrinks with the horizon, so the first capture is the
	// worst one, and holding it turns estimator convergence into fake
	// drift. degradeBaseAt is the capture's sim-time; refreshRendezvous-
	// Degrade recaptures every degradeRebaseAfter while healthy. Warning
	// on drift past a recent measure keeps the semantics "the encounter
	// got worse mid-coast".
	// Per-waypoint: degradeBaseSet is reset whenever the waypoint
	// advances or a partner's min-τ waypoint is adopted (#252), or every
	// advance would instantly read as a degraded encounter measured
	// against the previous waypoint — the next healthy recompute then
	// re-stamps BOTH degradeBaseCA and degradeBaseAt for the new leg.
	degradeBaseCA  float64
	degradeBaseAt  time.Time
	degradeBaseSet bool

	// peerGoneAt is the sim-time the partner's report first vanished from
	// the peer set entirely while the coast sat at/after τ (#252 review,
	// finding 3) — zero while the peer is present. A one-tick dropout at
	// exactly the crossing tick (all partner craft filtered: landed,
	// cross-system, degenerate KeplerStep) must not destroy the standing
	// intent when every other transient gap is held through; the coast
	// idles until the peer has been absent for more than the tolerance
	// window's worth of sim-time, then releases as a retract.
	peerGoneAt time.Time
}

// rendezvousArmedWith reports whether the viewer has Engaged a Rendezvous
// Warp toward owner. The mutual condition (both sides armed) also needs
// the peer's ArmedTowardViewer, checked in ComputeCoWarp.
func (w *World) rendezvousArmedWith(owner string) bool {
	return w.RendezvousArm != nil && w.RendezvousArm.TargetOwner == owner
}

// CoWarpState is the transient co-warp slate the reporting layer writes
// onto the World each tick and clampedWarp reads: whether the anchor is
// coupled to anyone this tick and the min Effective warp to clamp to.
// MinWarp 0 with Coupled set means coupled-with-no-clamp (#248): every
// coupled peer is the engaged rendezvous coast's partner, whose stale
// reported rate is exempt from min-wins — clampedWarp guards on
// MinWarp > 0, so both sides derive the coast rate independently instead
// of deadlocking on each other's report. Never persisted; empty in
// single-player. Partners is the coupled handles for HUD/debug.
type CoWarpState struct {
	Coupled  bool
	MinWarp  float64
	Partners []string
}

// CoWarpResult is ComputeCoWarp's full output: the State to store on the
// World, the per-owner coupled flags to feed back as `prev` next tick
// (the hysteresis memory), and the couple/release transitions the
// reporting layer turns into chips.
type CoWarpResult struct {
	State         CoWarpState
	CoupledOwners map[string]bool
	NewlyCoupled  []string // handles that transitioned uncoupled→coupled
	Released      []string // handles that transitioned coupled→uncoupled
}

// CoWarpCoupled reports whether the viewer is warp-coupled to any player
// this tick — read by the split guard (EngageSyncWarp) and available to
// the HUD. v0.28 S1.
func (w *World) CoWarpCoupled() bool { return w.CoWarp.Coupled }

// subspaceStepCap is the highest Effective Warp a player sharing a
// subspace may run at, or 0 when nothing is shared and the full ladder
// stands (#244).
//
// The same-subspace gate is a fixed span of sim-time while warp climbs in
// ×10 rungs, so at the top rungs a single tick covers more sim-time than
// the window can hold. That matters because the couple itself is gated on
// sameSubspace: a player who steps out of the window in one tick stops
// being coupled, which releases the min-warp clamp that was holding them
// — the escape removes its own brake, and the pair diverges without
// limit. Bounding the per-tick advance to a fraction of the window keeps
// the gate wider than one step at any selectable rate, so the clamp
// always gets a tick in which to act.
//
// Armed-but-not-yet-coupled counts too: refreshRendezvousInvite marks
// the incoming prompt Blocked outside sameSubspace (#250), so an
// initiator who races ahead turns their own invite non-joinable before
// anyone can answer it.
func (w *World) subspaceStepCap() float64 {
	if !w.CoWarp.Coupled && w.RendezvousArm == nil {
		return 0
	}
	step := w.Clock.BaseStep.Seconds()
	if step <= 0 {
		return 0
	}
	return coWarpSubspaceToleranceSec / (coWarpStepSafety * step)
}

// ComputeCoWarp evaluates proximity co-warp against the viewer's active
// craft (the anchor) for this tick. `prev` is the per-owner coupled map
// returned last tick — it supplies the hysteresis memory so a coupled
// pair uses the wider decouple gate. The returned CoupledOwners becomes
// next tick's `prev`. Pure over its inputs (no World mutation) so the
// caller assigns State to w.CoWarp; testable with hand-built peers.
//
// Anchor gating (ADR 0015 / 0025 precedent): only the viewer's active
// craft anchors co-warp in the MVP — a passive craft of the viewer near
// a partner won't couple. A landed or missing anchor couples to nobody.
func (w *World) ComputeCoWarp(peers []CoWarpPeer, prev map[string]bool) CoWarpResult {
	res := CoWarpResult{CoupledOwners: map[string]bool{}}
	anchor := w.ActiveCraft()
	anchorOK := anchor != nil && !anchor.Landed && !anchor.Crashed
	viewerT := w.Clock.SimTime

	minWarp := math.Inf(1)
	for _, p := range peers {
		wasCoupled := prev[p.Owner]
		coupledNow := false
		clampExempt := false
		// A non-positive Effective warp (a paused partner, Warp()==0)
		// imposes no co-warp constraint — the min would freeze the
		// viewer, which is not what a buddy tapping pause should do. Such
		// a partner simply isn't a couple; a real pause stops their
		// subspace clock, so Δt grows and the subspace gate releases them
		// within the tolerance window anyway. Gating coupledNow on it
		// keeps State/chips/clamp consistent (no couple without a clamp).
		if anchorOK && p.EffWarp > 0 && sameSubspace(viewerT, p.SubspaceTime) {
			switch {
			case w.rendezvousArmedWith(p.Owner) && p.ArmedTowardViewer:
				// Rendezvous trigger (v0.29 S1): both players Engaged toward
				// each other and share a Subspace — couple *before* the
				// proximity gate so they can coast to the encounter rate-
				// locked. The arm is a standing intent (#252): it persists
				// across waypoint advances, so this branch stays the
				// coupling source for the whole multi-maneuver approach. It
				// clears only at the proximity handoff (a waypoint reached
				// inside couple range) or on cancel; at the handoff the same
				// coupled state continues on the proximity branch below
				// (wasCoupled carries the hysteresis memory) — no
				// drop-and-recouple.
				coupledNow = true
				// #248: while the shared coast is ENGAGED, this partner's
				// reported EffWarp must NOT feed the min. The report is their
				// own post-clamp rate, always ≥1 tick stale, so min-wins over
				// it can only ratchet down — two players engaging at 1× pin
				// each other at 1× forever and the coast runs in real time.
				// Instead both sides derive the rate independently and
				// identically from the shared τ + constants: max-seed →
				// subspaceStepCap → approach ramp (clampedWarp). Per-peer
				// exemption, not a blanket skip: a third proximity-coupled
				// peer still contributes its min below. Legible here because
				// DriveRendezvousWarp runs before ComputeCoWarp each tick.
				// Armed-but-not-yet-coasting keeps min-wins (nothing seeds
				// the rate up yet, and the couple must not outrun the gate).
				clampExempt = w.rendezvousWarpEngaged()
			default:
				if rng, vrel, ok := closestApproach(anchor, p.Crafts); ok {
					coupledNow = coupleDecide(wasCoupled, rng, vrel)
				}
			}
		}
		res.CoupledOwners[p.Owner] = coupledNow
		switch {
		case coupledNow && !wasCoupled:
			res.NewlyCoupled = append(res.NewlyCoupled, p.Handle)
		case !coupledNow && wasCoupled:
			res.Released = append(res.Released, p.Handle)
		}
		if coupledNow {
			res.State.Partners = append(res.State.Partners, p.Handle)
			if !clampExempt && p.EffWarp < minWarp {
				minWarp = p.EffWarp
			}
		}
	}
	// A peer that vanished from the report set (left the system, ended
	// flight) while coupled is released silently by omission: it is absent
	// from CoupledOwners so next tick treats it as uncoupled, and the
	// clamp already dropped it from the min. No handle survives to chip a
	// release, which is acceptable for this edge (the common decouple —
	// drifting apart in-system — keeps the peer present, so it chips).
	//
	// Coupled-with-no-min-contribution (#248: every coupled peer is the
	// clamp-exempt coast partner) leaves MinWarp at its zero value —
	// clampedWarp guards on MinWarp > 0, so 0 naturally means "coupled,
	// no min-wins clamp" and the coast resolves its own rate.
	if len(res.State.Partners) > 0 {
		res.State.Coupled = true
		if !math.IsInf(minWarp, 1) {
			res.State.MinWarp = minWarp
		}
	}
	return res
}

// coupleDecide applies the hysteresis gate: an already-coupled pair stays
// coupled until it separates past the wider decouple band; an uncoupled
// pair couples only inside the tighter couple band. Range and |v_rel| are
// each independently sufficient to break the couple (OR), but both are
// required to form it (AND) — a slow drift-through at range, or a fast
// pass at close range, is not a rendezvous.
func coupleDecide(wasCoupled bool, rangeM, vrelMs float64) bool {
	if wasCoupled {
		return rangeM <= coWarpDecoupleRangeM && vrelMs <= coWarpDecoupleSpeedMs
	}
	return rangeM <= coWarpCoupleRangeM && vrelMs <= coWarpCoupleSpeedMs
}

// closestApproach returns the min range (and that craft's |v_rel|) among
// the peer's craft that share the anchor's SOI primary. ok is false when
// the peer has no same-primary craft — a cross-primary neighbour isn't a
// co-warp candidate. Both craft states are primary-relative in the same
// primary, so the primary's own motion cancels in both the separation and
// the relative velocity.
func closestApproach(anchor *spacecraft.Spacecraft, crafts []CoWarpCraft) (rangeM, vrelMs float64, ok bool) {
	best := math.Inf(1)
	for _, c := range crafts {
		if c.Primary != anchor.Primary.ID {
			continue
		}
		r := anchor.State.R.Sub(c.R).Norm()
		if r < best {
			best = r
			rangeM = r
			vrelMs = anchor.State.V.Sub(c.V).Norm()
			ok = true
		}
	}
	return rangeM, vrelMs, ok
}

// sameSubspace reports whether two subspace times are close enough to be
// the same subspace for co-warp purposes (see coWarpSubspaceToleranceSec).
func sameSubspace(a, b time.Time) bool {
	d := a.Sub(b).Seconds()
	if d < 0 {
		d = -d
	}
	return d <= coWarpSubspaceToleranceSec
}

// rendezvousCAAtTau returns the range between the anchor and its armed
// partner at the committed encounter τ — both Kepler-propagated from the
// viewer's sim-time to τ in the shared primary frame (v0.29 S1). Held τ:
// this measures the approach AT τ, it does not re-search for a new minimum
// (ADR 0034 v0.29 addendum — a degrading encounter warns, never
// re-targets). ok=false when not armed, the anchor can't propagate, or no
// same-primary partner craft is in the peer set.
func (w *World) rendezvousCAAtTau(peers []CoWarpPeer) (float64, bool) {
	if w.RendezvousArm == nil {
		return 0, false
	}
	anchor := w.ActiveCraft()
	if anchor == nil || anchor.Landed || anchor.Crashed {
		return 0, false
	}
	dt := w.RendezvousArm.Tau.Sub(w.Clock.SimTime).Seconds()
	if dt <= 0 {
		return 0, false
	}
	mu := anchor.Primary.GravitationalParameter()
	mine, ok := physics.KeplerStep(physics.StateVector{R: anchor.State.R, V: anchor.State.V, M: 1}, mu, dt)
	if !ok {
		return 0, false
	}
	// Min over the partner's same-primary craft (v0.29 review), mirroring
	// closestApproach: a partner flying near a deployed probe must not have
	// the probe's range — first in report order — masquerade as the
	// encounter.
	best := math.Inf(1)
	for _, p := range peers {
		if p.Owner != w.RendezvousArm.TargetOwner || !p.ArmedTowardViewer {
			continue
		}
		for _, c := range p.Crafts {
			if c.Primary != anchor.Primary.ID {
				continue
			}
			theirs, ok := physics.KeplerStep(physics.StateVector{R: c.R, V: c.V, M: 1}, mu, dt)
			if !ok {
				continue
			}
			if r := mine.R.Sub(theirs.R).Norm(); r < best {
				best = r
			}
		}
	}
	if math.IsInf(best, 1) {
		return 0, false
	}
	return best, true
}

// armedPartnerLacksLocalCraft reports whether the armed partner is still
// present in the peer set but has no craft left in the anchor's SOI —
// the partner flew the encounter's neighborhood entirely (v0.29 review).
// The degrade watchdog treats that as a broken encounter rather than
// going silently blind.
func (w *World) armedPartnerLacksLocalCraft(peers []CoWarpPeer) bool {
	anchor := w.ActiveCraft()
	if anchor == nil || w.RendezvousArm == nil {
		return false
	}
	for _, p := range peers {
		if p.Owner != w.RendezvousArm.TargetOwner || !p.ArmedTowardViewer {
			continue
		}
		for _, c := range p.Crafts {
			if c.Primary == anchor.Primary.ID {
				return false
			}
		}
		return true
	}
	return false
}

// refreshRendezvousDegrade recomputes the held encounter's approach each
// tick while the coast runs and flags a degrade when the encounter has
// worsened past a recent baseline by more than an encounter-scaled bar
// (v0.29 S1; see RendezvousArm.degradeBaseCA for why CommittedCA can't be
// the baseline, and degradeSlipFrac / degradeRebaseAfter for the #251
// false-fire the scaling + re-basing prevent) — the S2 warning chip's
// trigger. τ is held regardless. Clears the flag when not coasting.
// RendezvousApproachM carries the live approach for the chip's readout.
func (w *World) refreshRendezvousDegrade(peers []CoWarpPeer) {
	w.RendezvousDegraded = false
	w.RendezvousApproachM = 0
	arm := w.RendezvousArm
	if !w.rendezvousWarpEngaged() || arm == nil {
		return
	}
	caAtTau, ok := w.rendezvousCAAtTau(peers)
	if !ok {
		// No measurable approach. Almost always benign (τ reached, report
		// gap) — except a partner who left the anchor's SOI entirely: the
		// encounter is then broken and the watchdog must warn, not go
		// silently blind exactly when things are most wrong (v0.29 review).
		if w.Clock.SimTime.Before(arm.Tau) && w.armedPartnerLacksLocalCraft(peers) {
			w.RendezvousDegraded = true
		}
		return
	}
	if !arm.degradeBaseSet {
		arm.degradeBaseSet, arm.degradeBaseCA, arm.degradeBaseAt = true, caAtTau, w.Clock.SimTime
	}
	w.RendezvousApproachM = caAtTau
	// The bar scales with the encounter (#251): one couple-radius is the
	// floor — "would you still couple there" stays the close-encounter
	// meaning — but a distant encounter tolerates proportionate movement,
	// because at that scale the recomputed approach is a long-horizon
	// prediction whose input error alone dwarfs 35 km. Drift is signed:
	// only a WORSENING approach warns (an improving one is re-based below).
	bar := math.Max(coWarpCoupleRangeM, degradeSlipFrac*arm.degradeBaseCA)
	w.RendezvousDegraded = caAtTau-arm.degradeBaseCA > bar
	// While healthy, recapture the baseline every degradeRebaseAfter of
	// sim-time so estimator convergence (the estimate migrating as the
	// horizon shrinks) never accumulates into a trip; while degraded the
	// baseline freezes, so a genuine drift stays flagged against the
	// pre-drift measure until the partner corrects back inside the bar.
	if !w.RendezvousDegraded && w.Clock.SimTime.Sub(arm.degradeBaseAt) >= degradeRebaseAfter {
		arm.degradeBaseCA, arm.degradeBaseAt = caAtTau, w.Clock.SimTime
	}
}
