package serve

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
)

// Reprieve bounds (ADR 0036). A Reprieve ends at the earliest of its
// Commitment resolving, the cap below, or the owner reconnecting.
const (
	// dockReprieveCap is how long a cross-player dock holds a session up
	// after its player goes quiet. Flat: a dock has no committed end time
	// to derive one from — it lasts until somebody undocks.
	dockReprieveCap = 2 * time.Hour

	// rendezvousReprieveMargin covers the gap between the coast's arithmetic
	// end and the session actually noticing: arrival lands on the session's
	// own tick, the report crosses to the store, and the sweeper sees it a
	// tick later.
	rendezvousReprieveMargin = 5 * time.Minute

	// maxUnattendedReprieve is the ceiling on unattended simulation, counted
	// from the last time the peer spoke. It exists for the pathological
	// rendezvous: a TCA committed weeks of sim-time out is normally minutes
	// of wall time under warp, but a paused or 1×-pinned coast would hold
	// the connection open for weeks on the same arithmetic. Generous on
	// purpose — the longest coast actually observed was 2h19m — since
	// cutting a real coast short is the regression this whole mechanism
	// exists to prevent, and the ceiling is a backstop, not the working
	// bound.
	maxUnattendedReprieve = 4 * time.Hour
)

// commitmentKind distinguishes the two obligations that earn a Reprieve.
type commitmentKind int

const (
	commitRendezvous commitmentKind = iota
	commitDock
)

func (k commitmentKind) String() string {
	if k == commitDock {
		return "dock"
	}
	return "rendezvous"
}

// commitment is a live mutual obligation between two players that
// outlives either one's attention (CONTEXT.md). Its session keeps
// simulating while its player is silent, so that a closed laptop on one
// side doesn't strand the other mid-coast or tear a docked craft out of
// their Stack.
type commitment struct {
	kind commitmentKind
	peer string // the other player's fingerprint — the one left waiting

	// toGo is the sim-time still to run before the committed Time of
	// Closest Approach (rendezvous only).
	toGo time.Duration
}

// expiry reports when this Commitment's Reprieve runs out. now is wall
// clock; lastIO is the instant the session's peer last moved bytes (see
// activityConn) — the cap has to be counted from there, because counting
// from now would let an absent session renew its own cap on every sweep.
func (c commitment) expiry(now, lastIO time.Time) time.Time {
	if c.kind == commitDock {
		return lastIO.Add(dockReprieveCap)
	}
	// The coast is what has to finish, and its worst case is 1× real time:
	// the sim-time left to the TCA is therefore an upper bound on the wall
	// time left, and any warp only brings it in sooner. Recomputed every
	// sweep, so it converges as the unattended session ticks toward τ.
	coast := now.Add(c.toGo + rendezvousReprieveMargin)
	if ceiling := lastIO.Add(maxUnattendedReprieve); coast.After(ceiling) {
		return ceiling
	}
	return coast
}

// commitmentFor reports fp's live Commitment, if it has one.
//
// Pure over two snapshots — relay.Store.Snapshot and DockLedger.Records —
// which is the point: the sweeper runs outside every session's goroutine
// and must never touch a *sim.World (internal/relay/reporter.go:35). Both
// Commitments are already published across that seam under their own
// locks, and that is exactly why Proximity Co-Warp is excluded: its state
// (w.CoWarp.Coupled) lives only on the World, so admitting it would mean
// either a new publishing channel or recomputing the coupling — physics
// in the reaper, which ADR 0034 rules out.
//
// A session holding both obligations reports the dock: it is the one with
// another player's hardware physically inside it, and in practice the
// longer-lived of the two.
func commitmentFor(reports []relay.CraftReport, docks []relay.DockRecord, fp string) (commitment, bool) {
	// Both endpoints of a live dock are committed. The guest's craft is
	// fused into the owner's Stack, so if the guest drops the owner is
	// flying hardware they cannot hand back, and if the owner drops the
	// guest's craft stops being simulated inside a Stack they cannot
	// undock from — the split runs on the owner's tick.
	for _, d := range docks {
		if d.Phase != relay.DockActive {
			continue
		}
		switch fp {
		case d.GuestOwner:
			return commitment{kind: commitDock, peer: d.Owner}, true
		case d.Owner:
			return commitment{kind: commitDock, peer: d.GuestOwner}, true
		}
	}
	// A Rendezvous Warp is Engaged only once BOTH players have Engaged
	// toward each other (CONTEXT.md) — that is when the coast starts and
	// when either side's disappearance starts costing the other real time.
	// A lone arm is a pending invite: no coast, nobody committed but the
	// sender, and the partner simply sees the prompt vanish.
	own, ok := reportFor(reports, fp)
	if !ok || own.RendezvousTarget == "" {
		return commitment{}, false
	}
	partner, ok := reportFor(reports, own.RendezvousTarget)
	if !ok || partner.RendezvousTarget != fp {
		return commitment{}, false
	}
	return commitment{
		kind: commitRendezvous,
		peer: partner.Owner,
		// Read from fp's own report: it is fp's session being reprieved, so
		// its clock and its committed τ are the consistent pair.
		toGo: own.RendezvousTau.Sub(own.SubspaceTime),
	}, true
}

func reportFor(reports []relay.CraftReport, fp string) (relay.CraftReport, bool) {
	for _, r := range reports {
		if r.Owner == fp {
			return r, true
		}
	}
	return relay.CraftReport{}, false
}
