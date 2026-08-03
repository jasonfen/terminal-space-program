package sim

import "math"

// The initiator flies the clock (ADR 0037 §2).
//
// Inside a rendezvous agreement's TERMINAL phase the pair's rate is the
// initiator's selected warp. The accepter takes the copilot seat: their
// keys default to following, they may brake the pair down or cancel out,
// but never push it faster. Either side's active burn holds the pair at
// the burn cap, and the physical clamp family (approach ramp, step cap,
// period guard) still floors everything in clampedWarp — a hands-on
// pilot's clock can never be yanked forward.
//
// This replaces min-wins inside the agreement only. A plain proximity
// couple between players with no agreement has no initiator and keeps
// today's slower-wins rule, as does an agreement whose seats can't be
// resolved unambiguously.
//
// The ratchet (#248) cannot recur here because the derivation is acyclic:
// each seat publishes a SELECTION (plus its own burn cap), never the rate
// it ended up running at, so neither side's number is a function of the
// other's.

// burnWarpCap is the warp ceiling an active burn imposes — high warp
// during thrust lets the integrator blast past the burn's EndTime in a
// single tick. Named here because the terminal phase publishes it across
// the wire as one seat's contribution to the pair's rate; clampedWarp
// applies the same number locally.
const burnWarpCap = 10.0

// RendezvousSeat is the viewer's role in a resolved terminal-phase
// agreement.
type RendezvousSeat int

const (
	// RendezvousSeatNone: no agreement in the terminal phase, no matched
	// partner, or seats that don't resolve to exactly one initiator.
	RendezvousSeatNone RendezvousSeat = iota
	// RendezvousSeatPilot: the viewer proposed the rendezvous and flies the
	// pair's clock.
	RendezvousSeatPilot
	// RendezvousSeatCopilot: the viewer joined; their warp keys brake the
	// pair or release back to following.
	RendezvousSeatCopilot
)

// RendezvousRateHolder classifies what is setting the pair's rate — the
// answer to "why do my warp keys do nothing", which was the exact 30
// minutes of confusion behind #305.
type RendezvousRateHolder int

const (
	RendezvousRateNone RendezvousRateHolder = iota
	// RendezvousRateYou: your own selection is the pair's rate.
	RendezvousRateYou
	// RendezvousRateFollowing: you are the copilot, following the
	// initiator's clock.
	RendezvousRateFollowing
	// RendezvousRatePartnerBraking: the copilot has braked the pair below
	// what the initiator selected.
	RendezvousRatePartnerBraking
	// RendezvousRatePartnerBurning: the partner is burning, so the pair is
	// held at the burn cap.
	RendezvousRatePartnerBurning
	// RendezvousRatePartnerPaused: the partner's clock is stopped — the
	// deepest brake there is.
	RendezvousRatePartnerPaused
)

// RendezvousRateState is the terminal phase's standing rate slate: the
// viewer's seat plus everything the PARTNER contributes to the pair's
// rate. Written each tick by DriveRendezvousWarp, read by clampedWarp and
// by the RENDEZVOUS chip. Transient like the rest of the rendezvous
// slate; the zero value means "no seated agreement", which is every solo
// tick.
//
// Deliberately holds only the partner's half. The viewer's own selection,
// brake and burn cap are read LIVE in clampedWarp and in
// RendezvousRateHold — a slate refreshed on the serve pass is a tick
// stale, and a warp key whose effect waits a tick for a relayed slate is
// the unresponsive-keys complaint all over again.
type RendezvousRateState struct {
	Seat   RendezvousSeat
	Handle string // partner's display name, for the chip's "held:" row

	// PartnerRate is the ceiling the partner's seat imposes (0 = none):
	// the initiator's selected warp, or the copilot's brake, either way
	// already folded with their own burn cap. A paused partner is carried
	// as a 1× crawl rather than "no ceiling" — see refreshRendezvousRate.
	PartnerRate    float64
	PartnerBurning bool
	PartnerPaused  bool
}

// RendezvousRateHold classifies what is setting the pair's rate right
// now — the chip's "held:" row, and the answer to "why do my warp keys do
// nothing" that #305 spent thirty minutes not having. Derived live
// against the viewer's own current selection, so it never lags a keypress.
func (w *World) RendezvousRateHold() RendezvousRateHolder {
	rr := w.RendezvousRate
	if rr.Seat == RendezvousSeatNone {
		return RendezvousRateNone
	}
	own, _ := w.rendezvousSeatRate()
	if rr.PartnerRate > 0 && rr.PartnerRate < own {
		switch {
		case rr.PartnerPaused:
			return RendezvousRatePartnerPaused
		case rr.PartnerBurning:
			return RendezvousRatePartnerBurning
		case rr.Seat == RendezvousSeatCopilot:
			// The copilot's ceiling IS the initiator's clock — following,
			// not somebody braking.
			return RendezvousRateFollowing
		default:
			return RendezvousRatePartnerBraking
		}
	}
	return RendezvousRateYou
}

// RendezvousSeatRate is the viewer's published contribution to the pair's
// rate — what CraftReport carries and the partner clamps to. The
// initiator publishes their selected warp; the copilot publishes their
// brake (nothing while following); either folds in its own burn cap. 0
// means this seat imposes no ceiling.
//
// Deliberately a selection and not the post-clamp Effective warp: see the
// file header on why the acyclic derivation is what keeps #248 dead.
func (w *World) RendezvousSeatRate() float64 {
	rate, _ := w.rendezvousSeatRate()
	if math.IsInf(rate, 1) {
		return 0
	}
	return rate
}

// RendezvousSeatBurning reports whether the viewer's published seat rate
// comes from an active burn, so the partner's chip can name the cause.
func (w *World) RendezvousSeatBurning() bool {
	_, burning := w.rendezvousSeatRate()
	return burning
}

// RendezvousInitiatorSeat reports whether the viewer holds the initiator
// seat of a live agreement — the bit that rides the wire so both sides
// agree on roles.
func (w *World) RendezvousInitiatorSeat() bool {
	return w.RendezvousArm != nil && w.RendezvousArm.Initiator
}

// rendezvousBrakeFactor is the copilot's live brake, if any.
func (w *World) rendezvousBrakeFactor() (float64, bool) {
	arm := w.RendezvousArm
	if arm == nil || !arm.Approach || arm.Initiator ||
		arm.BrakeIdx < 0 || arm.BrakeIdx >= len(WarpFactors) {
		return 0, false
	}
	return WarpFactors[arm.BrakeIdx], true
}

func (w *World) rendezvousSeatRate() (float64, bool) {
	rate := math.Inf(1)
	if arm := w.RendezvousArm; arm != nil && arm.Approach {
		if arm.Initiator {
			if sel := w.Clock.Warp(); sel > 0 {
				rate = sel
			}
		} else if b, ok := w.rendezvousBrakeFactor(); ok {
			rate = b
		}
	}
	burning := w.anyCraftThrusting()
	if burning && rate > burnWarpCap {
		rate = burnWarpCap
	}
	return rate, burning
}

// rendezvousSeatWith resolves the viewer's seat against a matched partner
// peer. Requires the terminal phase, a mutual arm, and exactly one side
// claiming the initiator seat — two claims (crossed invites) or none (a
// peer from before ADR 0037) leave the seat unresolved on purpose, and
// the pair keeps min-wins rather than one side silently assuming command.
func (w *World) rendezvousSeatWith(p *CoWarpPeer) RendezvousSeat {
	arm := w.RendezvousArm
	if arm == nil || !arm.Approach || p == nil ||
		!p.ArmedTowardViewer || p.Owner != arm.TargetOwner {
		return RendezvousSeatNone
	}
	switch {
	case arm.Initiator && !p.RendezvousInitiator:
		return RendezvousSeatPilot
	case !arm.Initiator && p.RendezvousInitiator:
		return RendezvousSeatCopilot
	}
	return RendezvousSeatNone
}

// refreshRendezvousRate rebuilds the terminal phase's rate slate from
// this tick's peer set. Single-writer, like the hold and wait slates:
// clampedWarp only ever reads it.
func (w *World) refreshRendezvousRate(peers []CoWarpPeer) {
	w.RendezvousRate = RendezvousRateState{}
	arm := w.RendezvousArm
	if arm == nil || !arm.Approach {
		return
	}
	var partner *CoWarpPeer
	for i := range peers {
		if peers[i].Owner == arm.TargetOwner && peers[i].ArmedTowardViewer {
			partner = &peers[i]
			break
		}
	}
	seat := w.rendezvousSeatWith(partner)
	if seat == RendezvousSeatNone {
		return
	}
	st := RendezvousRateState{
		Seat: seat, Handle: partner.Handle,
		PartnerRate:    partner.RendezvousRate,
		PartnerBurning: partner.RendezvousBurning,
		PartnerPaused:  partner.Paused,
	}
	// A paused partner is the deepest brake there is, but it must not read
	// as "no ceiling" (their published rate is 0 while stopped) — that
	// would let a following copilot max-seed away from a partner who is
	// standing still. Carry a 1× crawl instead: the side that is behind
	// closes the gap and the leader hold freezes it once level.
	if partner.Paused {
		st.PartnerRate = 1
	}
	w.RendezvousRate = st
}

// StepRendezvousBrake moves the copilot's brake one rung — down brakes
// the pair, up releases it back toward following (ADR 0037 §2). Returns
// the new brake factor (0 while following) and whether the press applied
// at all: it refuses outside the copilot seat, which is what makes "never
// push it faster" a property of the input layer rather than a convention.
//
// Braking from FOLLOWING starts one rung below whatever the pair is
// actually doing, so the first press is a real slowdown regardless of
// where the initiator had the clock; releasing past the top rung returns
// to following rather than pinning a ceiling the initiator can't exceed
// anyway.
func (w *World) StepRendezvousBrake(up bool) (float64, bool) {
	if w.RendezvousRate.Seat != RendezvousSeatCopilot {
		return 0, false
	}
	arm := w.RendezvousArm
	switch {
	case up:
		if arm.BrakeIdx == rendezvousFollowing {
			return 0, false // there is nothing above the initiator to select
		}
		if arm.BrakeIdx+1 >= len(WarpFactors) {
			arm.BrakeIdx = rendezvousFollowing
			return 0, true
		}
		arm.BrakeIdx++
	case arm.BrakeIdx == rendezvousFollowing:
		arm.BrakeIdx = rungBelow(w.EffectiveWarp())
	case arm.BrakeIdx > 0:
		arm.BrakeIdx--
	}
	return WarpFactors[arm.BrakeIdx], true
}

// rungBelow is the highest WarpFactors index strictly below rate, floored
// at 1×.
func rungBelow(rate float64) int {
	idx := 0
	for i, f := range WarpFactors {
		if f < rate {
			idx = i
		}
	}
	return idx
}
