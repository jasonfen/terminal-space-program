package relay

import (
	"log"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Per-seat acknowledgement (ADR 0040 amendment, 10a). A transition that
// changes what one seat's World should contain marks that seat dirty and,
// when the seat will need the Vessel again, keeps a copy of it. The seat's next
// successful durable write is the acknowledgement: it clears the mark and
// drops the copy. A finished dock is settled, out of `records` and so invisible
// to every verb, and is deleted once both seats have acknowledged.

// finishLocked retires the record at id. It moves to `settled` while either
// seat is still unacknowledged and is deleted outright otherwise.
func (l *DockLedger) finishLocked(id uint64) {
	r := l.records[id]
	delete(l.records, id)
	if r == nil {
		return
	}
	r.Done = true
	if r.OwnerDirty || r.GuestDirty {
		l.settled[id] = r
	}
}

// swapSeatMarks follows a role swap (transfer, reclaim): the marks belong to
// the players, not the roles, and a copy delivered to the old role is dropped.
func (r *DockRecord) swapSeatMarks() {
	r.OwnerDirty, r.GuestDirty = r.GuestDirty, r.OwnerDirty
	r.ownerCopy, r.guestCopy = nil, nil
}

// AckPersisted is called straight after a SUCCESSFUL durable write of seat's
// World w (with the World that was written). It clears seat's dirty mark and
// copy on every live and settled record naming it, clears FromCompositeID when
// seat is the sender (guest after the swap), then deletes every settled record
// with both seats clean. It reports whether anything changed; the caller
// flushes the ledger when it did.
//
// One guard, because a wrong acknowledgement is a loss: a live Active record
// whose owner copy is held keeps the copy unless w holds a Vessel carrying the guest's parts.
func (l *DockLedger) AckPersisted(seat string, w *sim.World) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	changed := false
	ack := func(r *DockRecord) {
		if r.Owner == seat && (r.OwnerDirty || r.ownerCopy != nil) {
			if r.ownerCopy != nil && !r.Done && r.Phase == DockActive {
				if _, _, ok := compositeIn(w, r); !ok {
					log.Printf("dock %d: owner save lacks Composite %d, keeping the delivered copy", r.ID, r.CompositeID)
					return
				}
			}
			r.OwnerDirty, r.ownerCopy, changed = false, nil, true
		}
		if r.GuestOwner == seat && (r.GuestDirty || r.guestCopy != nil || r.FromCompositeID != 0) {
			r.GuestDirty, r.guestCopy, r.FromCompositeID, changed = false, nil, 0, true
		}
	}
	for _, r := range l.records {
		ack(r)
	}
	for id, r := range l.settled {
		ack(r)
		if !r.OwnerDirty && !r.GuestDirty {
			delete(l.settled, id)
			changed = true
		}
	}
	return changed
}
