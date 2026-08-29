package relay

import (
	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Durable dock ledger (ADR 0040 S1, #311/#312/#313).
//
// The ledger used to persist a dock's IDENTITY but not its SUBSTANCE: the
// owner/composite/guest cross-ref survived a restart while everything in
// flight — the craft payloads and the request flags — did not. That was fine
// while a payload was in flight for milliseconds. It stopped being fine once
// delivery could wait on a recipient who is offline for hours: on the
// production host a [J] to an offline partner parked the only copy of a
// composite on a transient field, and the hourly auto-adopt restart destroyed
// it. The craft ended up in no player's World and no save, while the record
// that named it outlived it.
//
// So the whole record round-trips. The craft payloads go through the save
// package's per-craft wire form (exactly what a save carries, so a Parcel can
// never drift from a saved craft), and the conversion happens INSIDE the
// ledger lock: a payload pointer handed out to the persist path could
// otherwise be adopted and mutated by the recipient's tick while the
// serialiser walked it.

// DockSnapshot is one dock record in full, serialisable form — the durable
// cross-ref plus every in-flight handoff. The session directory persists
// these; a restart seeds them straight back and delivery resumes where it
// stopped.
type DockSnapshot struct {
	ID            uint64
	Owner         string
	OwnerHandle   string
	DockerCraftID uint64
	CompositeID   uint64
	GuestOwner    string
	GuestHandle   string
	GuestCraftID  uint64
	Phase         DockPhase

	// In-flight handoffs. Nil payloads / false flags are the common case
	// (a settled dock); a non-nil payload is a craft that exists nowhere
	// else and must be delivered.
	GuestPayload    *save.Craft
	ReturnPayload   *save.Craft
	TransferPayload *save.Craft
	UndockAsk       bool
	UndockRefused   bool
	TransferTo      string
	Aborted         bool
	ReleaseAsk      bool
	ReleaseAsParcel bool
	Parcel          bool
	ParcelAtNano    int64
	ReclaimNotice   bool
	ReclaimAtNano   int64
	// DockNotice (ADR 0038 S1) is the absorbed guest's pending "you just got
	// docked" chip, owed at fuse — round-tripped so a restart between the
	// fuse and the guest's next tick doesn't drop it.
	DockNotice bool
	// ReturnAtNano (ADR 0038 S2) is the release-time stamp a live undock's
	// subspace-gap placement reads at delivery, generalising ParcelAtNano to
	// every return, not just Parcels.
	ReturnAtNano int64
}

// FullRecords snapshots every live dock in full, converting the parked craft
// under the ledger lock. Records() remains the identity-only view for callers
// that only want the cross-ref.
func (l *DockLedger) FullRecords() []DockSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]DockSnapshot, 0, len(l.records))
	for _, r := range l.records {
		out = append(out, DockSnapshot{
			ID: r.ID, Owner: r.Owner, OwnerHandle: r.OwnerHandle,
			DockerCraftID: r.DockerCraftID, CompositeID: r.CompositeID,
			GuestOwner: r.GuestOwner, GuestHandle: r.GuestHandle,
			GuestCraftID:    r.GuestCraftID,
			Phase:           r.Phase,
			GuestPayload:    craftToWireTransfer(r.guestPayload),
			ReturnPayload:   craftToWireSameOwner(r.returnPayload),
			TransferPayload: craftToWireTransfer(r.transferPayload),
			UndockAsk:       r.undockAsk,
			UndockRefused:   r.undockRefused,
			TransferTo:      r.transferTo,
			Aborted:         r.aborted,
			ReleaseAsk:      r.releaseAsk,
			ReleaseAsParcel: r.releaseAsParcel,
			Parcel:          r.parcel,
			ParcelAtNano:    r.parcelAtNano,
			ReclaimNotice:   r.reclaimNotice,
			ReclaimAtNano:   r.reclaimAtNano,
			DockNotice:      r.dockNotice,
			ReturnAtNano:    r.returnAtNano,
		})
	}
	return out
}

// SeedFull installs full records (from the session directory on server start)
// so a dock — and anything it was mid-way through delivering — resumes. A
// payload that can't be rehydrated against the loaded catalog is dropped
// rather than failing the whole restore: losing one craft is bad, refusing to
// start the session is worse, and the #309 reaper still bounds the damage.
func (l *DockLedger) SeedFull(snaps []DockSnapshot, systems []bodies.System) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range snaps {
		rec := &DockRecord{
			ID: s.ID, Owner: s.Owner, OwnerHandle: s.OwnerHandle,
			DockerCraftID: s.DockerCraftID, CompositeID: s.CompositeID,
			GuestOwner: s.GuestOwner, GuestHandle: s.GuestHandle,
			GuestCraftID:    s.GuestCraftID,
			Phase:           s.Phase,
			guestPayload:    craftFromWire(s.GuestPayload, systems),
			returnPayload:   craftFromWire(s.ReturnPayload, systems),
			transferPayload: craftFromWire(s.TransferPayload, systems),
			undockAsk:       s.UndockAsk,
			undockRefused:   s.UndockRefused,
			transferTo:      s.TransferTo,
			aborted:         s.Aborted,
			releaseAsk:      s.ReleaseAsk,
			releaseAsParcel: s.ReleaseAsParcel,
			parcel:          s.Parcel,
			parcelAtNano:    s.ParcelAtNano,
			reclaimNotice:   s.ReclaimNotice,
			reclaimAtNano:   s.ReclaimAtNano,
			dockNotice:      s.DockNotice,
			returnAtNano:    s.ReturnAtNano,
		}
		l.records[rec.ID] = rec
		if rec.ID >= l.nextID {
			l.nextID = rec.ID
		}
	}
}

// craftToWireTransfer / craftToWireSameOwner / craftFromWire adapt one
// parked craft to and from the save package's per-craft form. Both
// `craftToWire*` variants tolerate nil (no payload parked).
//
// #294 review finding 3, corrected by the second-round review finding 4:
// NOT every parked payload crosses into a different player's world.
// GuestPayload (guest→docker: the guest's craft being fused into the
// DOCKER's composite) and TransferPayload (old owner→new owner on a
// control transfer, or the stack handed over on an empty-seat reclaim)
// really do land in someone else's World — those go through
// save.CraftToWireForTransfer, which sanitizes ghost refs (a Target or a
// node/burn ref that pointed at a craft on the OTHER side's slate is
// meaningless once the craft lands on this side's). ReturnPayload is
// different: reconcileGuest always dispatches it back into the world of
// the session matching r.GuestOwner — the SAME owner who originally
// contributed the craft, whether via an abort (the guest's own pre-dock
// craft, parked verbatim and handed straight back), an undock, or a
// release (both split fresh off the composite). Sanitizing THAT payload
// strips a Target and node/burn refs that stayed perfectly valid the
// whole time — AdoptCraft only ever restamps the incoming craft's own
// ID, so any local ref to a SISTER craft in the same (guest's) slate
// survives the round trip untouched. ReturnPayload goes through the
// plain save.CraftToWire a session save/reconnect uses instead.
//
// Keeping the two wrappers syntactically distinct (one call each, not a
// shared helper with a bool flag) is deliberate: it's the whole point,
// so the three payload paths can't silently drift back onto the same
// call as this ages.
func craftToWireTransfer(c *spacecraft.Spacecraft) *save.Craft {
	if c == nil {
		return nil
	}
	wc := save.CraftToWireForTransfer(c)
	return &wc
}

func craftToWireSameOwner(c *spacecraft.Spacecraft) *save.Craft {
	if c == nil {
		return nil
	}
	wc := save.CraftToWire(c)
	return &wc
}

func craftFromWire(wc *save.Craft, systems []bodies.System) *spacecraft.Spacecraft {
	if wc == nil {
		return nil
	}
	c, err := save.CraftFromWire(*wc, systems)
	if err != nil {
		return nil
	}
	return c
}
