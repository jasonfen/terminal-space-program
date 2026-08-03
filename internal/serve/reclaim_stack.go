package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Empty-seat reclaim (ADR 0040 §4). A player riding in someone else's stack
// presses [J]; if the stack's owner has no live Session, the request is
// GRANTED rather than refused. The rule is the mirror of the presence gate on
// Transfer Control, and deliberately asymmetric: giving someone the stick
// needs a live recipient because delivery runs on their tick, while taking it
// back from an empty seat needs nobody's permission. The alternative — the ask
// waiting durably for the owner — is #312's stranding in a new coat.
//
// The migration itself is the serve layer's job rather than the ledger's,
// because the stack is not in any running World: it is sitting in the absent
// owner's persisted payload, and only the session store can open that.

// reclaimStack is the guest seat's [J]. It resolves the dock, performs the
// migration, and — whichever way it goes — says so.
func (m reportingModel) reclaimStack() (tea.Model, tea.Cmd) {
	rec, why := m.srv.dock.ReclaimTarget(m.owner, m.srv.presence.isOnline)
	if why == "" {
		if err := m.srv.reclaimFromEmptySeat(rec, m.owner); err != nil {
			why = "take control: " + err.Error()
		}
	}
	if why == "" {
		return m, nil
	}
	m.localEvents = append(m.localEvents, sim.SessionEvent{
		Kind: sim.SessionEventTransferRefused, Detail: why, At: time.Now(),
	})
	m.app.Toast(why)
	return m, nil
}

// reclaimFromEmptySeat lifts the stack out of the absent owner's persisted
// payload and parks it on the ledger for the reclaimer's next tick.
//
// Held under the absent player's admission lock for its whole length, so a
// connection arriving on their key cannot load the payload halfway through
// the migration — that lock is exactly the window sessionHandler holds while
// deciding whether to admit, and the liveness check is re-taken inside it.
//
// Write order is deliberate: the owner's payload is saved WITHOUT the stack
// before the ledger is asked to hold it. A crash in the gap loses one craft,
// which is visible and recoverable; the other order can leave the same vessel
// in the owner's save AND on the record, and two players each flying "the"
// stack is the silent, self-entrenching kind of wrong #307 taught us to fear.
func (s *Server) reclaimFromEmptySeat(rec relay.DockRecord, reclaimer string) error {
	defer s.admit.enter(rec.Owner)()
	if s.presence.isOnline(rec.Owner) {
		return errors.New("they just took their seat back — ask them to hand it over")
	}
	w, err := s.store.LoadPlayer(rec.Owner)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errors.New("their program has never been saved — there is nothing to take")
		}
		return fmt.Errorf("their program could not be opened: %w", err)
	}
	stack, ok := w.RemoveCraftByID(rec.CompositeID)
	if !ok {
		return errors.New("the stack is no longer in their program")
	}
	if err := s.store.SavePlayer(rec.Owner, w); err != nil {
		return fmt.Errorf("their program could not be written: %w", err)
	}
	// w.Clock.SimTime is the absent owner's sim-time as of the payload this
	// stack came from — the reclaim-review fix (§4 finding): the payload can
	// be hours stale by the time the reclaimer's own tick delivers it, so
	// this rides along and reconcileOwner Kepler-advances the stack to the
	// reclaimer's current sim-time before adopting it, the same way a
	// Parcel is placed via parcelAtNano.
	if !s.dock.GrantReclaim(rec.ID, reclaimer, stack, w.Clock.SimTime.UnixNano()) {
		return errors.New("the dock changed while the stack was being handed over")
	}
	if err := s.persistDocks(); err != nil {
		// The grant only lives in the in-process ledger until this persist
		// lands — a restart before the next successful one would drop the
		// stack silently, exactly the #311 shape this ledger exists to
		// close. Undo both halves of the migration so the stack survives
		// SOMEWHERE durable: back on the owner's saved program, and the
		// ledger record back to its pre-grant shape. rec is that pre-grant
		// snapshot — ReclaimTarget took it before GrantReclaim touched
		// anything.
		s.dock.RestoreRecord(rec.ID, rec)
		w.Crafts = append(w.Crafts, stack)
		if resaveErr := s.store.SavePlayer(rec.Owner, w); resaveErr != nil {
			// Both halves of the undo failed: the ledger record is back to
			// pre-grant shape (so it no longer claims to hold the stack),
			// but the owner's save was never rewritten with the stack back
			// in it either. The stack now exists only in the in-memory
			// `stack` this call is about to drop — log loudly, there is no
			// durable copy left to point at.
			log.Printf("reclaim: %s's stack could not be saved back after a ledger persist failure (persist: %v, resave: %v) — the stack has no durable copy", rec.Owner, err, resaveErr)
			return fmt.Errorf("the handover could not be saved, and could not be undone either — get help: %w", err)
		}
		return fmt.Errorf("the handover could not be saved, try again: %w", err)
	}
	return nil
}
