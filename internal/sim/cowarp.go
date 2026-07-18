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
	// 10 km is a rendezvous neighbourhood; 100 m/s |v_rel| is slow
	// enough to be station-keeping rather than a flyby. Tunables —
	// playtest may move them (ADR addendum: "seems good for now").
	coWarpCoupleRangeM  = 10_000.0
	coWarpCoupleSpeedMs = 100.0

	// coWarpDecoupleRangeM / coWarpDecoupleSpeedMs are the decouple gate.
	// Wider than the couple gate on purpose (hysteresis): once coupled,
	// separation past 12 km OR 120 m/s is required to release. The band
	// between the two gates is where a coupled pair stays coupled, so
	// small station-keeping excursions across 10 km / 100 m/s don't flap
	// the clamp or re-emit couple/release chips every tick. Tunables.
	coWarpDecoupleRangeM  = 12_000.0
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
)

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

	// ArmedTowardViewer is set by the relay adapter when this peer has a
	// live Rendezvous Warp intent aimed at the viewer (v0.29 S1, ADR 0034
	// v0.29 addendum). Combined with the viewer's own RendezvousArm
	// targeting this peer, the two are *mutually* armed and couple before
	// the proximity gate — the second Co-Warp trigger.
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
}

// RendezvousArm is the viewer's outgoing Rendezvous Warp intent (v0.29 S1,
// ADR 0034 v0.29 addendum): the partner they have Engaged toward and the
// committed encounter sim-time (the initiator's authoritative Time of
// Closest Approach). Transient like AutoWarp/CoWarp — never persisted,
// cleared on cancel, on arrival at Tau, and on partner disconnect.
type RendezvousArm struct {
	TargetOwner string    // fingerprint of the partner Engaged toward
	Tau         time.Time // committed absolute encounter sim-time
	CommittedCA float64   // m — the predicted approach at Tau when Engaged (the degrade baseline)
}

// rendezvousArmedWith reports whether the viewer has Engaged a Rendezvous
// Warp toward owner. The mutual condition (both sides armed) also needs
// the peer's ArmedTowardViewer, checked in ComputeCoWarp.
func (w *World) rendezvousArmedWith(owner string) bool {
	return w.RendezvousArm != nil && w.RendezvousArm.TargetOwner == owner
}

// CoWarpState is the transient co-warp slate the reporting layer writes
// onto the World each tick and clampedWarp reads: whether the anchor is
// coupled to anyone this tick and, if so, the min Effective warp to clamp
// to. Never persisted; empty in single-player. Partners is the coupled
// handles for HUD/debug.
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
				// locked. On arrival the arm clears; the same coupled state
				// then continues on the proximity branch below (wasCoupled
				// carries the hysteresis memory) — no drop-and-recouple.
				coupledNow = true
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
			if p.EffWarp < minWarp {
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
	if len(res.State.Partners) > 0 && !math.IsInf(minWarp, 1) {
		res.State.Coupled = true
		res.State.MinWarp = minWarp
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
			return mine.R.Sub(theirs.R).Norm(), true
		}
	}
	return 0, false
}

// refreshRendezvousDegrade recomputes the held encounter's approach each
// tick while the coast runs and flags a degrade when the partner has
// drifted more than a couple-radius past the committed baseline (v0.29
// S1) — the S2 warning chip's trigger. τ is held regardless. Clears the
// flag when not coasting. RendezvousApproachM carries the live approach
// for the chip's readout.
func (w *World) refreshRendezvousDegrade(peers []CoWarpPeer) {
	w.RendezvousDegraded = false
	w.RendezvousApproachM = 0
	if !w.rendezvousWarpEngaged() || w.RendezvousArm == nil {
		return
	}
	caAtTau, ok := w.rendezvousCAAtTau(peers)
	if !ok {
		return
	}
	w.RendezvousApproachM = caAtTau
	// A couple-radius of drift past the committed approach is "the
	// encounter you agreed to has meaningfully slipped" — reusing the
	// couple gate keeps the threshold tied to whether you'd still couple.
	w.RendezvousDegraded = caAtTau-w.RendezvousArm.CommittedCA > coWarpCoupleRangeM
}
