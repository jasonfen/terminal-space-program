package serve

import (
	"errors"
	"fmt"
	"io/fs"
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
	if !s.dock.GrantReclaim(rec.ID, reclaimer, stack) {
		return errors.New("the dock changed while the stack was being handed over")
	}
	_ = s.persistDocks()
	return nil
}
