// Package relay is the multiplayer store (v0.27 S4, ADR 0034): a
// no-physics, no-clock report/subscribe hub. Sessions report their
// craft as messages; subscribers read the latest report per player
// and evaluate ghosts at their own sim-time. Everything crossing this
// interface is a plain serialisable value — the ssh-only MVP keeps
// the store in shared memory, and the v2 WebSocket layer must be a
// pure transport swap over these same messages (store discipline,
// ADR 0034 addendum).
package relay

import (
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// CraftState is one vessel on the wire: primary-relative state vector
// at the owner's subspace time (the exact representation the save
// envelope uses, and what physics.KeplerStep propagates for ghost
// evaluation), plus the addressing a viewer needs (system, SOI
// primary) and display identity. Landed craft carry no meaningful
// orbit — flagged so renderers skip them while rosters still count
// them.
type CraftState struct {
	ID      uint64       `json:"id"`
	Name    string       `json:"name"`
	Glyph   string       `json:"glyph,omitempty"`
	System  string       `json:"system"`
	Primary string       `json:"primary"`
	R       orbital.Vec3 `json:"r"`
	V       orbital.Vec3 `json:"v"`
	Landed  bool         `json:"landed,omitempty"`
}

// CraftReport is one player's full craft set at a moment of their
// subspace time — set-replace semantics, so a vanished craft (staged
// away, ended flight) disappears by omission. Identity is the ssh key
// fingerprint; handles live in the session roster (sessiondir) and
// are joined by the UI, not duplicated onto the wire.
type CraftReport struct {
	Owner        string       `json:"owner"`
	SubspaceTime time.Time    `json:"subspace_time"`
	Crafts       []CraftState `json:"crafts"`

	// ActiveCraftID names which of Crafts the reporter is actually
	// flying (#288). Without it a consumer can only read a fixed slot,
	// which told partners a four-craft pilot was at the Sun for a whole
	// session because slot 0 held a heliocentric craft. Zero (omitted)
	// means "unmarked" — ActiveCraft falls back to the first slot, the
	// pre-#288 behaviour.
	ActiveCraftID uint64 `json:"active_craft_id,omitempty"`

	// EffWarp is the reporter's current Effective warp — the post-clamp
	// rate its World actually stepped this report (v0.28 S1, ADR 0034 §5).
	// Proximity co-warp reads it to take the min over coupled players, so
	// a partner's 10× burn cap propagates. A plain float, serialisable
	// like everything else crossing this interface (store discipline).
	EffWarp float64 `json:"eff_warp,omitempty"`

	// RendezvousTarget / RendezvousTau carry the reporter's outgoing
	// Rendezvous Warp intent (v0.29 S1, ADR 0034 v0.29 addendum): the
	// fingerprint they have Engaged toward and the committed encounter
	// sim-time. Empty when not armed. CoWarpPeersFrom turns a report whose
	// RendezvousTarget names the viewer into CoWarpPeer.ArmedTowardViewer,
	// so the mutual-arm couple trigger can fire; the responder reads the
	// τ to adopt the initiator's authoritative encounter time. Plain
	// serialisable values — store discipline preserved.
	RendezvousTarget string    `json:"rendezvous_target,omitempty"`
	RendezvousTau    time.Time `json:"rendezvous_tau,omitempty"`
	RendezvousCA     float64   `json:"rendezvous_ca,omitempty"` // committed predicted approach at τ (m) — the responder's adopted baseline

	// RendezvousMeetingPlace / RendezvousMeetingLaps (ADR 0045 S7, #400)
	// carry the reporter's chosen Meeting Place + lap count alongside
	// RendezvousTau/CA, when their commit came from a planted Meeting
	// Burn node — agreement state the accepter's chip names (and cannot
	// change; see sim.RendezvousArm's doc comment). Empty/zero otherwise,
	// including the whole "agreed, no plan yet" state (RendezvousTau the
	// zero time), which by definition never had one.
	RendezvousMeetingPlace string `json:"rendezvous_meeting_place,omitempty"`
	RendezvousMeetingLaps  int    `json:"rendezvous_meeting_laps,omitempty"`

	// RendezvousInitiator / RendezvousRate / RendezvousBurning carry the
	// reporter's SEAT and its contribution to the pair's rate in a
	// rendezvous agreement's terminal phase (ADR 0037 §2). The initiator
	// publishes their selected warp, the copilot their brake, either folded
	// with their own burn cap; 0 means "this seat imposes no ceiling".
	//
	// The role must be unambiguous under reconnect, so it rides the wire
	// explicitly rather than being inferred from who Engaged first — a
	// reconnecting session rebuilds its arm from its own state, and two
	// sides guessing from report order could disagree about who is in
	// command. A peer that publishes neither bit (an older build) leaves
	// the seats unresolved, and the pair keeps min-wins.
	//
	// RendezvousRate is a SELECTION, never a post-clamp rate: the receiving
	// side's own rate is derived from it, so relaying a derived value back
	// would close the #248 loop and ratchet the pair to 1×.
	RendezvousInitiator bool    `json:"rendezvous_initiator,omitempty"`
	RendezvousRate      float64 `json:"rendezvous_rate,omitempty"`
	RendezvousBurning   bool    `json:"rendezvous_burning,omitempty"`

	// Paused marks a deliberately paused reporter (Clock.Paused), as
	// opposed to an EffWarp of 0 from a hold or clamp — the rendezvous
	// hold-the-leader logic keys on it (v0.29 review).
	Paused bool `json:"paused,omitempty"`
}

// ActiveCraft returns the craft the reporter is flying — the one a
// partner means by "where are they" (#288) and the one any verb aimed at
// the player acts through. Falls back to the first slot when the report
// carries no marker or names a craft that has since left the set, so an
// unmarked report reads exactly as it did before the marker existed.
// ok=false only for a genuinely empty slate.
func (r CraftReport) ActiveCraft() (CraftState, bool) {
	if len(r.Crafts) == 0 {
		return CraftState{}, false
	}
	for _, cs := range r.Crafts {
		if cs.ID == r.ActiveCraftID {
			return cs, true
		}
	}
	return r.Crafts[0], true
}

// Store holds the latest report per owner and fans new reports out to
// subscribers. It never inspects craft physics and holds no clock of
// its own (ADR 0034: the server stores and relays, nothing else).
type Store struct {
	mu      sync.RWMutex
	reports map[string]CraftReport
	subs    map[int]chan CraftReport
	nextSub int
}

func NewStore() *Store {
	return &Store{
		reports: map[string]CraftReport{},
		subs:    map[int]chan CraftReport{},
	}
}

// Report replaces the owner's craft set and notifies subscribers. A
// subscriber that has fallen behind misses intermediate reports, not
// the latest state — Snapshot always has that.
func (s *Store) Report(r CraftReport) {
	s.mu.Lock()
	s.reports[r.Owner] = r
	chans := make([]chan CraftReport, 0, len(s.subs))
	for _, ch := range s.subs {
		chans = append(chans, ch)
	}
	s.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- r:
		default: // slow subscriber: drop — Snapshot recovers
		}
	}
}

// Snapshot returns the latest report per owner, excluding one (a
// viewer never ghosts itself). Reports are copied; callers may hold
// them across frames.
func (s *Store) Snapshot(excludeOwner string) []CraftReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]CraftReport, 0, len(s.reports))
	for owner, r := range s.reports {
		if owner == excludeOwner {
			continue
		}
		cp := r
		cp.Crafts = append([]CraftState(nil), r.Crafts...)
		out = append(out, cp)
	}
	return out
}

// Frontier is the maximum subspace time across every stored report.
// It backs --reset-fleet's epoch (internal/serve/resetfleet.go),
// which wants every clock aligned forward to the furthest-advanced
// session. It is NOT where a new player joins — see Earliest for
// that (ADR 0034 §7 amendment / ADR 0045 S3). ok is false while the
// store is empty.
func (s *Store) Frontier() (time.Time, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var max time.Time
	ok := false
	for _, r := range s.reports {
		if !ok || r.SubspaceTime.After(max) {
			max = r.SubspaceTime
			ok = true
		}
	}
	return max, ok
}

// Earliest is the minimum subspace time across every stored report
// whose owner satisfies live — where a new player joins (ADR 0034 §7
// amendment / ADR 0045 S3, closing #247/#396): joining behind the
// group is recoverable with Sync, which is forward-only, so joining
// ahead of anyone actually playing is the trap to avoid, not joining
// behind.
//
// Report never evicts on disconnect, so the store keeps every
// owner's last report indefinitely; Frontier's max-fold tolerates
// that (a stale entry can't raise a maximum live players have
// already passed), but a min-fold is dominated by it — a player who
// played briefly and left would permanently drag every new joiner to
// their stale clock. live is the caller's liveness check, so a
// departed owner's lingering report is excluded from the fold; the
// store itself tracks no notion of "live" (ADR 0034: it stores and
// relays, nothing else — that bookkeeping belongs to whoever tracks
// sessions, e.g. serve's presence).
//
// ok is false when no report satisfies live; the caller falls back
// to the furthest-behind persisted payload
// (sessiondir.Store.EarliestSimTime).
//
// live is called once per stored report WHILE s.mu is held (RLock) —
// it must not touch the store (no Report/Snapshot/Earliest/Frontier
// call back in, directly or transitively), or a liveness source that
// itself reads the relay store deadlocks. The only production caller
// (Server.isOnline) locks presence.mu instead, a separate lock, so
// this is safe today; a nil live would panic, so one is substituted
// silently.
func (s *Store) Earliest(live func(owner string) bool) (time.Time, bool) {
	if live == nil {
		live = func(string) bool { return true }
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var min time.Time
	ok := false
	for owner, r := range s.reports {
		if !live(owner) {
			continue
		}
		if !ok || r.SubspaceTime.Before(min) {
			min = r.SubspaceTime
			ok = true
		}
	}
	return min, ok
}

// Subscribe returns a channel of future reports and a cancel func.
// The channel is buffered; a subscriber that stalls drops messages
// rather than blocking reporters.
func (s *Store) Subscribe() (<-chan CraftReport, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextSub
	s.nextSub++
	ch := make(chan CraftReport, 16)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subs, id)
	}
}
