package relay

import (
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The approacher rule (ADR 0053 decision 9, "Settled here" item 2; review
// 2026-10-14 finding 3). Cross-player, exactly ONE seat of a pair runs the
// five-second magnet: the approacher. The other seat is hands-off, unmoved and
// unturned, and rides in the approacher's stack when the Claim lands.
//
// Both Worlds must agree on who that is without a round trip, and a port gate
// is symmetric (the relative motion is the same whichever vessel you call the
// mover), so physics cannot name the approacher. What can be named is intent,
// and every seat of one `--serve` shares this process, so each seat publishes
// its intent here every tick and reads the others'. Precedence, highest first:
//
//  1. The rendezvous INITIATOR (ADR 0037 §2): the seat that proposed the
//     standing agreement with the partner's owner is pilot-in-command of the
//     pair; it is the approacher.
//  2. The seat that has the other's vessel TARGETED (the player who aimed at
//     it is the one flying to it; the Proximity View's port rows need it).
//  3. Tie (both, or neither, or the same rank): the lower (owner fingerprint,
//     craft ID). Arbitrary but stable, identical on both seats.
//
// A seat that is not the approacher never starts a pull, so it files no Claim;
// the approacher's Claim on completion makes the approacher the docker, which
// by ADR 0040 owns the stack. Ownership therefore follows the rule above, not
// which seat's tick ran first.
//
// The pull LATCH makes the answer stable once a pull is running: a pair has at
// most one registered puller, so an intent that changes mid-pull cannot start
// a second one.

// SeatCraft names one seat's vessel: its owner's fingerprint and craft ID.
type SeatCraft struct {
	Owner   string
	CraftID uint64
}

func (a SeatCraft) less(b SeatCraft) bool {
	if a.Owner != b.Owner {
		return a.Owner < b.Owner
	}
	return a.CraftID < b.CraftID
}

// approachStale is how long a seat's published intent or pull registration
// survives without a refresh. Seats tick ~20x/s, so this only ever fires for a
// seat that is gone or paused; it keeps a vanished puller from latching a pair
// forever.
const approachStale = 3 * time.Second

type seatIntent struct {
	craftID   uint64
	target    SeatCraft // the ghost this seat has targeted, zero when none
	initiator string    // owner fp of the rendezvous partner this seat initiated with, "" when none
	at        time.Time
}

type pairKey struct{ lo, hi SeatCraft }

func keyOf(a, b SeatCraft) pairKey {
	if b.less(a) {
		a, b = b, a
	}
	return pairKey{a, b}
}

type pullReg struct {
	puller SeatCraft
	at     time.Time
}

// ApproachBoard is the shared intent + pull registry. The zero value is ready.
type ApproachBoard struct {
	mu      sync.Mutex
	now     func() time.Time // nil: time.Now (tests inject)
	intents map[string]seatIntent
	pulls   map[pairKey]pullReg
}

func (b *ApproachBoard) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// Publish records seat me's intent for this tick: the ghost it has targeted
// (zero SeatCraft when none) and the owner it is the rendezvous initiator
// toward ("" when none).
func (b *ApproachBoard) Publish(me, target SeatCraft, initiatorToward string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.intents == nil {
		b.intents = map[string]seatIntent{}
	}
	b.intents[me.Owner] = seatIntent{craftID: me.CraftID, target: target, initiator: initiatorToward, at: b.clock()}
}

// Clear drops everything a seat published or holds: it has no active vessel,
// or it left.
func (b *ApproachBoard) Clear(owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.intents, owner)
	for k, p := range b.pulls {
		if p.puller.Owner == owner {
			delete(b.pulls, k)
		}
	}
}

// rank scores seat a's claim to be the approacher toward b. Caller holds mu.
func (b *ApproachBoard) rank(a, other SeatCraft) int {
	in, ok := b.intents[a.Owner]
	if !ok || in.craftID != a.CraftID || b.clock().Sub(in.at) > approachStale {
		return 0
	}
	if in.initiator != "" && in.initiator == other.Owner {
		return 2
	}
	if in.target == other {
		return 1
	}
	return 0
}

// Approacher names which of the two vessels is the approacher. Pure function
// of the published intents, so both seats get the same answer.
func (b *ApproachBoard) Approacher(x, y SeatCraft) SeatCraft {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.approacherLocked(x, y)
}

func (b *ApproachBoard) approacherLocked(x, y SeatCraft) SeatCraft {
	rx, ry := b.rank(x, y), b.rank(y, x)
	switch {
	case rx > ry:
		return x
	case ry > rx:
		return y
	case y.less(x):
		return y
	}
	return x
}

// BeginPull registers me as the puller for the pair, or refreshes the
// registration. It is refused when the other vessel already holds the pair, or
// (for a new pull) when the rule names the other the approacher.
func (b *ApproachBoard) BeginPull(me, other SeatCraft) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pulls == nil {
		b.pulls = map[pairKey]pullReg{}
	}
	k := keyOf(me, other)
	now := b.clock()
	if p, ok := b.pulls[k]; ok && now.Sub(p.at) <= approachStale {
		if p.puller != me {
			return false
		}
	} else if b.approacherLocked(me, other) != me {
		return false
	}
	b.pulls[k] = pullReg{puller: me, at: now}
	return true
}

// EndPull releases me's registration for the pair (completed, let go, gone).
func (b *ApproachBoard) EndPull(me, other SeatCraft) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := keyOf(me, other)
	if p, ok := b.pulls[k]; ok && p.puller == me {
		delete(b.pulls, k)
	}
}

// pullingToward reports whether other is the live registered puller of the pair.
func (b *ApproachBoard) pullingToward(me, other SeatCraft) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	p, ok := b.pulls[keyOf(me, other)]
	return ok && p.puller == other && b.clock().Sub(p.at) <= approachStale
}

// Gate is the sim.GhostPullGate for seat me.
func (b *ApproachBoard) Gate(me SeatCraft) sim.GhostPullGate { return approachGate{b, me} }

type approachGate struct {
	b  *ApproachBoard
	me SeatCraft
}

func other(r sim.GhostRef) SeatCraft { return SeatCraft{r.Owner, r.CraftID} }

func (g approachGate) Approacher(r sim.GhostRef) bool {
	return g.b.Approacher(g.me, other(r)) == g.me
}
func (g approachGate) BeginPull(r sim.GhostRef) bool { return g.b.BeginPull(g.me, other(r)) }
func (g approachGate) EndPull(r sim.GhostRef)        { g.b.EndPull(g.me, other(r)) }
func (g approachGate) PullingToward(r sim.GhostRef) bool {
	return g.b.pullingToward(g.me, other(r))
}
