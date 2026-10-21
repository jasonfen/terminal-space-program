package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"strings"
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
// Write order is ledger first (ADR 0040 amendment 2, 10a): the payload is
// repaired against the ledger, the grant is flushed, and only then is the
// owner's payload saved without the stack. A crash or a failed write in the
// gap leaves the stack in the stale payload AND on the record, which is no
// longer a hazard: the owner's next open repairs the payload against the
// record's FromCompositeID and removes it. Nothing is lost and nothing is
// undone.
func (s *Server) reclaimFromEmptySeat(rec relay.DockRecord, reclaimer string) error {
	defer s.admit.enter(rec.Owner)()
	if s.presence.isOnline(rec.Owner) {
		return errors.New("they just took their seat back — ask them to hand it over")
	}
	// A payload that does not exist is repaired as an empty World, so a guest
	// riding a stack whose owner never saved gets their Vessel back.
	w, err := s.store.LoadPlayer(rec.Owner)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if w, err = emptyWorld(); err != nil {
			return fmt.Errorf("their program could not be opened: %w", err)
		}
	case err != nil:
		return fmt.Errorf("their program could not be opened: %w", err)
	}
	if s.dock.Repair(w, rec.Owner) {
		if err := s.persistDocks(); err != nil {
			return fmt.Errorf("the dock could not be saved, try again: %w", err)
		}
	}
	// The repair could not rebuild the dock: the guest's Vessel is already on
	// its way back as a Parcel, which is what they pressed [J] to get.
	if s.dock.ReturnInFlight(rec.ID) {
		return nil
	}
	// The repair can move the Composite's ID (a re-adopted stack); read it again.
	cur, why := s.dock.ReclaimTarget(reclaimer, s.presence.isOnline)
	if why != "" {
		return errors.New(strings.TrimPrefix(why, "take control: "))
	}
	stack, ok := w.RemoveCraftByID(cur.CompositeID)
	if !ok {
		return errors.New("the stack is no longer in their program")
	}
	// w.Clock.SimTime is the absent owner's sim-time as of the payload this
	// stack came from — the reclaim-review fix (§4 finding): the payload can
	// be hours stale by the time the reclaimer's own tick delivers it, so
	// this rides along and reconcileOwner Kepler-advances the stack to the
	// reclaimer's current sim-time before adopting it, the same way a
	// Parcel is placed via parcelAtNano.
	if !s.dock.GrantReclaim(cur.ID, reclaimer, stack, w.Clock.SimTime.UnixNano()) {
		return errors.New("the dock changed while the stack was being handed over")
	}
	if err := s.persistDocks(); err != nil {
		// The grant is in memory and the stack is still in the owner's saved
		// payload, so nothing is lost; the tick loop keeps retrying the flush
		// (#335). Do not touch the payload until the ledger holds the grant.
		s.markDockFlushFailed(err)
		return fmt.Errorf("the handover could not be saved yet, it will retry: %w", err)
	}
	if err := s.store.SavePlayer(rec.Owner, w); err != nil {
		log.Printf("reclaim: %s's program could not be saved without the stack (%v); their next open repairs it", rec.Owner, err)
		return nil
	}
	s.ackPersisted(rec.Owner, w)
	return nil
}

// emptyWorld is a World with no Vessels: the stand-in for a player who has no
// saved program, so a repair has something to run against.
func emptyWorld() (*sim.World, error) {
	w, err := sim.NewWorld()
	if err != nil {
		return nil, err
	}
	w.Crafts = nil
	return w, nil
}
