package relay

import (
	"log"
	"sort"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Repair puts a World that has just come from durable storage right against
// the ledger (ADR 0040 amendment, 10a). seat is the fingerprint whose World w
// is; call it before w's first Reconcile, then save w (which acknowledges) and
// flush the ledger. It reports whether it changed w or any record.
//
// Every rule is "make it so", never "do it": a Vessel the ledger says left is
// removed if present, one it says is here is put back if absent, parts it says
// were handed back are stripped if still there. A crash during repair, or a
// repeat of it, therefore reruns the same rules against a World that already
// agrees and changes nothing (amendment draft section 10).
//
// Each seat's rules are gated on that seat's dirty mark, because the mark is
// what says the durable World may disagree with the record. A clean seat's
// World agrees, and Vessel IDs are per World, so touching it could remove an
// unrelated Vessel that reuses an ID.
func (l *DockLedger) Repair(w *sim.World, seat string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	var recs []*DockRecord
	for _, m := range []map[uint64]*DockRecord{l.records, l.settled} {
		for _, r := range m {
			if r.GuestOwner == seat || r.Owner == seat {
				recs = append(recs, r)
			}
		}
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	changed := false
	for _, r := range recs {
		if r.GuestOwner == seat && repairGuestSeat(w, r) {
			changed = true
		}
		if r.Owner == seat && repairOwnerSeat(w, r) {
			changed = true
		}
	}
	return changed
}

// ReturnInFlight reports whether record id is holding a Vessel on its way back
// to the guest. A repair that could not rebuild a dock leaves the record so.
func (l *DockLedger) ReturnInFlight(id uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.records[id]
	return ok && r.returnPayload != nil
}

// repairGuestSeat is rules G1 to G3.
func repairGuestSeat(w *sim.World, r *DockRecord) bool {
	// G1: the handover has not happened; the Vessel belongs in this World.
	if r.Phase == DockPending && r.guestPayload == nil && r.returnPayload == nil && !r.aborted {
		return false
	}
	changed := false
	// G2: the Vessel is on the ledger, riding, or sent away. FromCompositeID
	// is the stack this seat sent by [J] or lost to a reclaim.
	if r.GuestDirty || r.FromCompositeID != 0 {
		for _, id := range []uint64{r.GuestCraftID, r.FromCompositeID} {
			if id == 0 {
				continue
			}
			if _, ok := w.RemoveCraftByID(id); ok {
				changed = true
			}
		}
	}
	// G3: a delivered return the guest has not saved. The copy wins over
	// whatever this World held under that ID (residual 1).
	if r.guestCopy != nil {
		if c := craftFromWire(r.guestCopy, w.Systems); c != nil {
			w.AdoptCraft(c, true)
			changed = true
		}
	}
	return changed
}

// carriesGuest reports whether c has a component the guest's seat put there.
func carriesGuest(c *spacecraft.Spacecraft, guestOwner string, guestCraftID uint64) bool {
	if guestOwner == "" {
		return false
	}
	for _, dc := range c.DockedComponents {
		if dc.Owner == guestOwner && (guestCraftID == 0 || dc.CraftID == guestCraftID) {
			return true
		}
	}
	return false
}

// repairOwnerSeat is rules O1 to O6.
func repairOwnerSeat(w *sim.World, r *DockRecord) bool {
	if r.transferPayload != nil { // O1: this seat is the recipient, nothing here yet
		return false
	}
	if !r.OwnerDirty || r.aborted {
		return false
	}
	c, cidx, here := w.CraftByID(r.CompositeID)
	// O6: a purged guest's parts stay in the stack as the owner's own.
	if r.Done && r.end == endTransfer {
		if here && sim.StackHasGuest(c) {
			adoptGuestComponents(c)
			return true
		}
		return false
	}
	carries := here && carriesGuest(c, r.GuestOwner, r.GuestCraftID)
	// O2, return side: the guest's parts were handed back, in flight, delivered
	// or settled. The ledger's copy is the truth, so strip and discard.
	if r.returnPayload != nil || r.guestCopy != nil || r.Phase == DockCooldown || r.Done {
		if !carries {
			return false
		}
		if _, ok := w.UndockGuest(cidx, r.GuestOwner, r.GuestCraftID); !ok {
			log.Printf("dock %d: stale Composite %d would not give up the guest's parts; left as is", r.ID, r.CompositeID)
			return false
		}
		return true
	}
	if r.Phase != DockActive {
		return false
	}
	// O2, agreeing side: the Composite is here, so any copy is acknowledged.
	// The Composite keeps the lead's ID, so "present" means "carries the
	// guest's parts", not "an ID resolves".
	if carries {
		if r.ownerCopy == nil {
			return false
		}
		r.ownerCopy = nil
		return true
	}
	if r.ownerCopy == nil { // O5: gone since; the per-tick reaper handles it
		return false
	}
	cp := craftFromWire(r.ownerCopy, w.Systems)
	if cp == nil {
		return false
	}
	if sim.StackHasGuest(cp) { // O4: an adopted Composite
		w.AdoptCraft(cp, true)
		r.CompositeID = cp.ID
		return true
	}
	// O3: the guest's Vessel as fused. Fuse it again if the lead is here.
	if _, idx, ok := w.CraftByID(r.DockerCraftID); ok {
		if comp, _, ok := w.DockGuestCraft(idx, cp, r.GuestOwner); ok {
			r.CompositeID = comp.ID
			return true
		}
	}
	// The dock cannot be rebuilt: the guest gets their Vessel back as it was
	// at dock time, as a Parcel. Not `aborted`: reconcileGuest's aborted
	// branch delivers without the Parcel chip.
	r.returnPayload, r.ownerCopy = cp, nil
	r.parcel, r.dockNotice = true, false
	return true
}
