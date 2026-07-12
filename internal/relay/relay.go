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

// Frontier is the maximum subspace time across every stored report —
// where a new player joins (you can never start in someone's past,
// ADR 0034). ok is false while the store is empty.
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
