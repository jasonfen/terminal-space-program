package relay

import (
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Purging a player from the dock ledger (ADR 0054 slice E, kick and delete
// vessels; ADR 0040 amended: a record can end by transfer).
//
// "Kick and delete vessels" deletes the purged player's own vessels and
// nobody else's. A cross-player dock puts one player's vessel inside another
// player's stack, so a purge cannot just delete every record that names the
// player:
//
//   - they are the GUEST (their vessel rides in a friend's stack): the vessel
//     becomes the stack owner's for good. The record ends by transfer.
//   - they are the OWNER (a friend's vessel rides in their stack): the
//     friend's vessel is handed back before the owner's save is deleted.
//
// Two steps because the second half needs the owner's World, which only the
// owner's own session may touch. ReleaseGuestsOf runs against that World (on
// the purged player's last tick, or against their loaded payload when they
// are offline); DropRecordsNaming then rewrites every record that still names
// them so that none does.

// adoptGuestComponents clears every guest tag in c: the components stay in
// the stack, as the stack owner's own.
func adoptGuestComponents(c *spacecraft.Spacecraft) {
	if c == nil {
		return
	}
	for i := range c.DockedComponents {
		c.DockedComponents[i].Owner = ""
	}
}

// ReleaseGuestsOf frees every friend's vessel riding in a stack fp owns, so
// that deleting fp's World deletes none of them. w is fp's World (live, or
// loaded from their payload). live reports session liveness, as for
// RequestRelease: a friend who is not there gets a Parcel.
//
// A guest at the top of the stack is peeled off and parked as a return. A
// guest whose parts sit under the owner's (a control transfer left them
// there, #314) cannot be peeled without mis-slicing, so the whole stack goes
// to the friend instead; their copy of the purged player's parts is then
// adopted by the friend as in any other transfer ending. Either way no
// friend's vessel is deleted.
func (l *DockLedger) ReleaseGuestsOf(w *sim.World, fp string, live func(string) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.records {
		if r.Owner != fp || r.Phase != DockActive || r.hasParkedPayload() {
			continue
		}
		_, cidx, ok := w.CraftByID(r.CompositeID)
		if !ok {
			continue
		}
		restored, ok := w.UndockGuest(cidx, r.GuestOwner, r.GuestCraftID)
		if ok {
			sim.SeparationPush(restored)
			r.returnPayload = restored
			r.returnAtNano = w.Clock.SimTime.UnixNano()
			r.parcel = live != nil && !live(r.GuestOwner)
			if r.parcel {
				r.parcelAtNano = r.returnAtNano
			}
			continue
		}
		// Hand the whole stack over, the way a control transfer does.
		removed, _ := w.RemoveCraftByID(r.CompositeID)
		if removed == nil {
			continue
		}
		sim.SafeHandback(removed)
		sim.RetagStackForTransfer(removed, fp, r.GuestOwner)
		r.transferPayload = removed
		r.transferTo = ""
		r.Owner, r.OwnerHandle, r.GuestOwner, r.GuestHandle =
			r.GuestOwner, r.GuestHandle, r.Owner, r.OwnerHandle
		r.DockerCraftID, r.GuestCraftID = r.GuestCraftID, r.DockerCraftID
	}
}

// DropRecordsNaming rewrites or removes every record that names fp, so that
// afterwards none does (fingerprint or handle). What happens to each record
// is decided by who fp was in it; see the file comment. It returns the
// number of records it ended or rewrote.
func (l *DockLedger) DropRecordsNaming(fp string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for id, r := range l.records {
		if r.Owner != fp && r.GuestOwner != fp {
			continue
		}
		n++
		if r.Phase == DockCooldown {
			delete(l.records, id)
			continue
		}
		if r.GuestOwner == fp {
			if l.endGuestSideLocked(r) {
				delete(l.records, id)
			}
			continue
		}
		if l.endOwnerSideLocked(r, fp) {
			delete(l.records, id)
		}
	}
	return n
}

// endGuestSideLocked ends a record whose guest is the purged player. It
// returns true when the record should simply go.
func (l *DockLedger) endGuestSideLocked(r *DockRecord) bool {
	// Pending: their vessel never reached the stack. Whatever of it is
	// parked on the record is theirs and goes with it. An aborted record, or
	// a handback already on its way to them, has nobody left to receive it.
	if r.Phase != DockActive || r.aborted || r.returnPayload != nil {
		return true
	}
	// Active: their vessel is in the owner's stack. It stays there, the
	// owner's. The record keeps only what the owner's tick needs to finish
	// the job.
	r.end = endTransfer
	r.endHandle = r.GuestHandle
	r.GuestOwner, r.GuestHandle, r.GuestCraftID = "", "", 0
	r.undockAsk, r.undockRefused, r.releaseAsk, r.releaseAsParcel = false, false, false, false
	r.dockNotice, r.reclaimNotice = false, false
	r.transferTo = ""
	return false
}

// endOwnerSideLocked ends a record whose stack owner is the purged player.
// It returns true when the record should simply go.
func (l *DockLedger) endOwnerSideLocked(r *DockRecord, fp string) bool {
	name := r.OwnerHandle
	clearOwner := func() {
		r.end = endHandback
		r.endHandle = name
		r.Owner, r.OwnerHandle, r.DockerCraftID = "", "", 0
		r.undockAsk, r.undockRefused, r.releaseAsk, r.releaseAsParcel = false, false, false, false
		r.transferTo = ""
		r.dockNotice, r.reclaimNotice = false, false
	}
	switch {
	case r.Phase == DockPending:
		if r.guestPayload == nil {
			// The friend still holds their craft; they only learn the claim
			// is gone.
			return true
		}
		// The friend's craft is parked on the record and exists nowhere
		// else: return it.
		r.returnPayload, r.guestPayload = r.guestPayload, nil
		r.aborted = true
		clearOwner()
	case r.transferPayload != nil:
		// A stack on its way to the purged player, carrying the friend's
		// parts: send it back to the friend and let them adopt the purged
		// player's parts (they are inseparable from here).
		sim.RetagStackForTransfer(r.transferPayload, fp, r.GuestOwner)
		r.Owner, r.OwnerHandle, r.GuestOwner, r.GuestHandle =
			r.GuestOwner, r.GuestHandle, r.Owner, r.OwnerHandle
		r.DockerCraftID, r.GuestCraftID = r.GuestCraftID, r.DockerCraftID
		return l.endGuestSideLocked(r)
	case r.returnPayload != nil:
		// The friend's component was already peeled for them: it arrives,
		// and the record ends with it.
		clearOwner()
	default:
		// No stack left to peel and nothing parked: the dock is over, as a
		// phantom composite's is (#309). The friend is told.
		r.aborted = true
		clearOwner()
	}
	return false
}
