package relay

import (
	"sync"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Cross-player docking ledger (v0.28 S5, ADR 0034 §6 + the 2026-07-14
// addendum). A cross-player dock spans two players' Worlds, each of which
// lives in its own session and only ever ticks its OWN World — exactly the
// constraint co-warp already works under. So the dock is mediated by shared,
// serialisable ledger state (this file, sibling to the report Store) that
// each session reconciles against its World once per tick: the guest's tick
// hands its craft over, the docker's tick fuses it, undock and transfer flow
// back the same way. Nothing crosses a World boundary except through a ledger
// payload, so a v2 WebSocket layer serialises the same records + craft
// payloads (which round-trip through the save package) — store discipline.
//
// The durable subset of a record (owner, composite/guest IDs, phase) is what
// the session directory persists as the reconnect cross-ref; the craft
// payloads and request flags are transient in-flight handoffs.

// DockPhase is a cross-player dock's lifecycle stage.
type DockPhase int

const (
	// DockPending: the docker has claimed a guest craft; the guest hasn't
	// yet handed its craft over (first the guest's tick removes it from its
	// World and parks it on the record, then the docker's tick fuses it).
	DockPending DockPhase = iota
	// DockActive: the guest craft is fused into the docker's stack and the
	// ride is live — the guest is Docked-as-Guest, warp-coupled to the stack.
	DockActive
)

// DockRecord is one cross-player dock. The exported fields are the durable
// cross-ref (persisted in the session directory); the unexported fields are
// transient in-process handoffs consumed on the reconciling side's tick.
type DockRecord struct {
	ID            uint64
	Owner         string // current stack owner fingerprint (flips on transfer)
	OwnerHandle   string
	DockerCraftID uint64 // the owner's craft that leads the fused stack
	CompositeID   uint64 // the fused stack's craft ID in the owner's World (0 until fused)
	GuestOwner    string // the guest player fingerprint
	GuestHandle   string
	GuestCraftID  uint64 // the guest's craft riding in the stack (returned on undock)
	Phase         DockPhase

	// transient handoffs — in-process craft moves (a v2 wire serialises them)
	guestPayload    *spacecraft.Spacecraft // guest→docker: craft to fuse
	returnPayload   *spacecraft.Spacecraft // docker→guest: restored craft on undock/abort
	transferPayload *spacecraft.Spacecraft // old owner→new owner: the whole migrating stack
	undockAsk       bool                   // guest→docker: split my component
	transferTo      string                 // owner→recipient: pending control transfer target
	aborted         bool                   // docker couldn't fuse — guest reclaims its craft
}

// DockChip is a moment the reconcile surfaces for the caller to turn into a
// session chip (docked / undocked / control transfer).
type DockChip struct {
	Kind   sim.SessionEventKind
	Handle string
}

// DockLedger is the shared, in-process ledger of live cross-player docks.
// Every mutation takes the lock; a v2 wire keeps the same call surface with
// the store behind it.
type DockLedger struct {
	mu      sync.Mutex
	records map[uint64]*DockRecord
	nextID  uint64
}

// NewDockLedger builds an empty ledger.
func NewDockLedger() *DockLedger {
	return &DockLedger{records: map[uint64]*DockRecord{}}
}

// Claim opens a cross-player dock: the docker (owner) claims a guest craft it
// has closed on. Refused (ok=false) when either craft is already engaged in a
// dock — the guard that keeps a simultaneous mutual approach from opening two
// crossed records (the ledger mutex serialises, so the first writer wins; the
// passive-station MVP posture means only one side is actively claiming).
func (l *DockLedger) Claim(owner, ownerHandle string, dockerCraftID uint64, guestOwner, guestHandle string, guestCraftID uint64) (*DockRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.records {
		if r.involvesCraft(owner, dockerCraftID) || r.involvesCraft(guestOwner, guestCraftID) {
			return nil, false
		}
	}
	l.nextID++
	r := &DockRecord{
		ID:            l.nextID,
		Owner:         owner,
		OwnerHandle:   ownerHandle,
		DockerCraftID: dockerCraftID,
		GuestOwner:    guestOwner,
		GuestHandle:   guestHandle,
		GuestCraftID:  guestCraftID,
		Phase:         DockPending,
	}
	l.records[r.ID] = r
	return r, true
}

// involvesCraft reports whether (fp, craftID) is either endpoint of r.
func (r *DockRecord) involvesCraft(fp string, craftID uint64) bool {
	return (r.Owner == fp && r.DockerCraftID == craftID) ||
		(r.GuestOwner == fp && r.GuestCraftID == craftID)
}

// RequestUndock flags the guest's active dock for a split (guest-initiated,
// any time — ADR 0034 §6). The docker's next reconcile performs the actual
// UndockGuest and hands the craft back. ok is false when no active dock
// matches (nothing to undock).
func (l *DockLedger) RequestUndock(guestOwner string, guestCraftID uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.records {
		if r.GuestOwner == guestOwner && r.GuestCraftID == guestCraftID && r.Phase == DockActive {
			r.undockAsk = true
			return true
		}
	}
	return false
}

// RequestTransfer flags the owner's active stack for a control handover to
// the guest (2-party: the recipient is unambiguous — ADR 0034 addendum). The
// docker's next reconcile migrates the stack unless it's mid-burn (refused,
// retried). ok is false when the caller owns no active cross-player stack.
func (l *DockLedger) RequestTransfer(owner string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.records {
		if r.Owner == owner && r.Phase == DockActive {
			r.transferTo = r.GuestOwner
			return true
		}
	}
	return false
}

// ActiveGuestDock returns the active record in which fp is the guest, if any —
// the tui reads it to route the Undock key to RequestUndock and to show the
// docked-as-guest status.
func (l *DockLedger) ActiveGuestDock(fp string) (*DockRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.records {
		if r.GuestOwner == fp && r.Phase == DockActive {
			cp := *r
			return &cp, true
		}
	}
	return nil, false
}

// Records returns a durable-field snapshot of every live dock — the session
// directory persists this as the reconnect cross-ref.
func (l *DockLedger) Records() []DockRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]DockRecord, 0, len(l.records))
	for _, r := range l.records {
		out = append(out, DockRecord{
			ID: r.ID, Owner: r.Owner, OwnerHandle: r.OwnerHandle,
			DockerCraftID: r.DockerCraftID, CompositeID: r.CompositeID,
			GuestOwner: r.GuestOwner, GuestHandle: r.GuestHandle,
			GuestCraftID: r.GuestCraftID, Phase: r.Phase,
		})
	}
	return out
}

// Seed installs durable records (from the session directory on server start)
// so a dock that outlived a restart resumes. Only the durable fields are
// carried; the in-flight payload handoffs were transient and are gone.
func (l *DockLedger) Seed(records []DockRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range records {
		rec := r
		l.records[rec.ID] = &rec
		if rec.ID >= l.nextID {
			l.nextID = rec.ID
		}
	}
}

// Reconcile advances every dock touching owner against owner's World w for
// one tick, moving craft across the World seam through the ledger payloads,
// and returns any chips to surface. reports supplies the current per-owner
// CraftReport (for the guest's warp coupling to the stack owner). w.DockGuest
// is rebuilt each call — set when this player is Docked-as-Guest, nil otherwise.
func (l *DockLedger) Reconcile(w *sim.World, owner string, reports map[string]CraftReport) []DockChip {
	l.mu.Lock()
	defer l.mu.Unlock()
	var chips []DockChip
	w.DockGuest = nil // rebuilt below if still a guest in some active dock
	for id, r := range l.records {
		switch {
		case r.Owner == owner:
			if l.reconcileOwner(w, r, &chips) {
				delete(l.records, id)
			}
		case r.GuestOwner == owner:
			if l.reconcileGuest(w, r, reports, &chips) {
				delete(l.records, id)
			}
		}
	}
	return chips
}

// reconcileOwner runs the stack owner's side of a dock. Returns true when the
// record is finished and should be dropped.
func (l *DockLedger) reconcileOwner(w *sim.World, r *DockRecord, chips *[]DockChip) bool {
	// Transfer arrival: this session just became the stack's owner (roles
	// swapped on the old owner's tick). Adopt the migrated composite into
	// this World and fly it — the new owner is no longer a guest.
	if r.transferPayload != nil {
		w.AdoptCraft(r.transferPayload, true)
		r.CompositeID = r.transferPayload.ID
		r.transferPayload = nil
		*chips = append(*chips, DockChip{Kind: sim.SessionEventTransfer, Handle: r.GuestHandle})
		return false
	}
	switch r.Phase {
	case DockPending:
		if r.guestPayload == nil {
			return false // waiting for the guest to hand over
		}
		guest := r.guestPayload
		r.guestPayload = nil
		_, idx, ok := w.CraftByID(r.DockerCraftID)
		if !ok {
			// The docker's craft vanished (staged / ended flight) between
			// claim and handover — abort: hand the guest's craft back.
			r.returnPayload, r.aborted = guest, true
			return false
		}
		comp, _, ok := w.DockGuestCraft(idx, guest, r.GuestOwner)
		if !ok {
			r.returnPayload, r.aborted = guest, true
			return false
		}
		r.CompositeID = comp.ID
		r.Phase = DockActive
		*chips = append(*chips, DockChip{Kind: sim.SessionEventDocked, Handle: r.GuestHandle})
		return false

	case DockActive:
		// Undock request: split the guest's component and hand it back.
		if r.undockAsk {
			r.undockAsk = false
			_, cidx, ok := w.CraftByID(r.CompositeID)
			if ok {
				if restored, ok := w.UndockGuest(cidx, r.GuestOwner, r.GuestCraftID); ok {
					r.returnPayload = restored
					*chips = append(*chips, DockChip{Kind: sim.SessionEventUndocked, Handle: r.GuestHandle})
				}
			}
			// If the composite or component is gone, fall through — the guest
			// side will time out waiting; MVP treats this as the dock ending.
			return false
		}
		// Transfer request: migrate the whole stack to the guest (roles swap),
		// unless mid-burn (refused, retried next tick).
		if r.transferTo != "" {
			comp, _, ok := w.CraftByID(r.CompositeID)
			if !ok {
				r.transferTo = ""
				return false
			}
			if sim.StackMidBurn(comp) {
				return false // refused mid-burn — retry
			}
			newOwner := r.transferTo
			oldOwner, oldOwnerHandle := r.Owner, r.OwnerHandle
			oldDockerCraftID := r.DockerCraftID
			removed, _ := w.RemoveCraftByID(r.CompositeID)
			sim.RetagStackForTransfer(removed, oldOwner, newOwner)
			// Swap roles: the old owner becomes the guest of the new owner.
			r.transferPayload = removed
			r.transferTo = ""
			r.Owner, r.OwnerHandle = newOwner, r.GuestHandle
			r.DockerCraftID = r.GuestCraftID
			r.GuestOwner, r.GuestHandle = oldOwner, oldOwnerHandle
			r.GuestCraftID = oldDockerCraftID
			*chips = append(*chips, DockChip{Kind: sim.SessionEventTransfer, Handle: r.OwnerHandle})
			return false
		}
		return false
	}
	return false
}

// reconcileGuest runs the guest's side of a dock. Returns true when the
// record is finished and should be dropped.
func (l *DockLedger) reconcileGuest(w *sim.World, r *DockRecord, reports map[string]CraftReport, chips *[]DockChip) bool {
	// Abort: the docker couldn't fuse — reclaim the handed-over craft.
	if r.aborted && r.returnPayload != nil {
		w.AdoptCraft(r.returnPayload, true)
		return true
	}
	switch r.Phase {
	case DockPending:
		if r.guestPayload == nil && !r.aborted {
			c, ok := w.RemoveCraftByID(r.GuestCraftID)
			if !ok {
				return true // my craft is gone — abandon the dock
			}
			r.guestPayload = c
		}
		l.setDockGuest(w, r, reports)
		return false

	case DockActive:
		// Undock/abort completion: the docker handed my craft back.
		if r.returnPayload != nil {
			w.AdoptCraft(r.returnPayload, true)
			r.returnPayload = nil
			*chips = append(*chips, DockChip{Kind: sim.SessionEventUndocked, Handle: r.OwnerHandle})
			return true
		}
		l.setDockGuest(w, r, reports)
		return false
	}
	return false
}

// setDockGuest writes the guest's docked-as-guest link so the serve layer can
// fold the min-wins coupling to the stack owner into the guest's co-warp state.
func (l *DockLedger) setDockGuest(w *sim.World, r *DockRecord, reports map[string]CraftReport) {
	var ownerEff float64
	if rep, ok := reports[r.Owner]; ok {
		ownerEff = rep.EffWarp
	}
	w.DockGuest = &sim.DockGuestLink{
		OwnerFP:      r.Owner,
		OwnerHandle:  r.OwnerHandle,
		OwnerEffWarp: ownerEff,
		GuestCraftID: r.GuestCraftID,
	}
}

// DetectGuestContact returns a guest ghost the viewer's active craft has
// closed to within the docking gates, among owners the viewer is co-warp
// coupled to (coupled) — the cross-player analogue of checkDocking, which can't
// fire because a ghost isn't in the local slate. The viewer must not already
// be flying a cross-player stack or riding as a guest. ok is false when there's
// no contact. The serve layer turns a hit into a ledger Claim.
func DetectGuestContact(w *sim.World, coupled map[string]bool) (ghostOwner string, ghostCraftID uint64, ok bool) {
	active := w.ActiveCraft()
	if active == nil || active.Landed || active.Crashed || w.DockGuest != nil {
		return "", 0, false
	}
	if sim.StackHasGuest(active) {
		return "", 0, false // already a cross-player stack
	}
	for _, g := range w.Ghosts {
		if !coupled[g.Owner] || g.PrimaryID != active.Primary.ID {
			continue
		}
		if active.State.R.Sub(g.RelPos).Norm() > sim.DockingDistM {
			continue
		}
		if active.State.V.Sub(g.Vel).Norm() > sim.DockingVMS {
			continue
		}
		return g.Owner, g.CraftID, true
	}
	return "", 0, false
}
