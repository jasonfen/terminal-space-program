package relay

import (
	"sync"
	"time"

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

// DockRecord is one cross-player dock. The exported fields are the cross-ref
// (owner, composite/guest IDs, phase); the unexported fields are the in-flight
// handoffs consumed on the reconciling side's tick. ADR 0040: BOTH halves are
// durable now — FullRecords/SeedFull round-trip the payloads and request flags
// through the session directory, so a handover in flight when the server
// restarts completes on the recipient's next connect instead of destroying the
// craft (#311). Nothing on a record is transient any more.
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

	// in-flight handoffs — craft moves between Worlds (persisted: see
	// dock_persist.go; a v2 wire serialises the same shape)
	guestPayload    *spacecraft.Spacecraft // guest→docker: craft to fuse
	returnPayload   *spacecraft.Spacecraft // docker→guest: restored craft on undock/abort
	transferPayload *spacecraft.Spacecraft // old owner→new owner: the whole migrating stack
	undockAsk       bool                   // guest→docker: split my component
	undockRefused   bool                   // docker→guest: your undock was refused, you're still docked (#307)
	transferTo      string                 // owner→recipient: pending control transfer target
	aborted         bool                   // docker couldn't fuse / the stack is gone — the dock ends
	releaseAsk      bool                   // owner→own tick: release the guest's component (ADR 0040 §3)
	releaseAsParcel bool                   // that release is going to a guest who isn't there
	// parcel marks a returnPayload the owner released while the guest was
	// NOT there to receive it live (ADR 0040 §3). It changes what the guest
	// is told on delivery — a Parcel arrives with an explanation rather than
	// reading as an undock they did not ask for — and, with parcelAtNano,
	// how far it is propagated to reach the guest's sim-time.
	parcel       bool
	parcelAtNano int64
}

// hasParkedPayload reports whether the record is holding a craft that exists
// NOWHERE else — the only copy, waiting on a recipient's tick. Such a record
// is healthy however phantom it looks from the other seat: the #309 reaper
// must not end a dock whose composite is "missing" precisely because it is
// sitting on the record awaiting delivery (ADR 0040 §1).
func (r *DockRecord) hasParkedPayload() bool {
	return r.guestPayload != nil || r.returnPayload != nil || r.transferPayload != nil
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
// retried). live reports whether a fingerprint has a live Session.
//
// ok is false with the player-facing reason from TransferRefusal, which it
// consults rather than re-deriving — the two cannot drift, so a refused [J]
// can never be silent (#308's lesson at the second cross-player verb).
func (l *DockLedger) RequestTransfer(owner string, live func(string) bool) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if why := l.transferRefusalLocked(owner, live); why != "" {
		return false, why
	}
	for _, r := range l.records {
		if r.Owner == owner && r.Phase == DockActive {
			r.transferTo = r.GuestOwner
			return true, ""
		}
	}
	return false, transferNoStack
}

// TransferRefusal says, in the player's words, why RequestTransfer(owner)
// will refuse — or "" when the stack will change hands. Exhaustive over the
// synchronous refusals by construction (RequestTransfer consults it); the
// mid-burn refusal is deliberately NOT here, because it is a wait rather than
// a no — the reconcile retries it every tick until the burn ends.
func (l *DockLedger) TransferRefusal(owner string, live func(string) bool) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.transferRefusalLocked(owner, live)
}

const transferNoStack = "transfer: not flying a cross-player stack"

// transferRefusalLocked is the body of TransferRefusal; the caller holds the
// ledger mutex.
func (l *DockLedger) transferRefusalLocked(owner string, live func(string) bool) string {
	for _, r := range l.records {
		if r.Owner != owner || r.Phase != DockActive {
			continue
		}
		// ADR 0040 §2: handing someone the stick needs someone there to take
		// it. Delivery runs on the RECIPIENT's tick, so a handover to an
		// absent partner strips the stack out of the only World simulating it
		// and leaves the sender a passenger in a vehicle that exists in
		// nobody's sky, parked outside time until they come back. Durability
		// (S1) makes that survivable; it should not be reachable on purpose.
		if live != nil && !live(r.GuestOwner) {
			who := r.GuestHandle
			if who == "" {
				who = "your partner"
			}
			return "transfer: " + who + " is not in the session — nobody is there to take the stick"
		}
		return ""
	}
	return transferNoStack
}

// RequestRelease is the docker-side release (ADR 0040 §3): the owner of a
// cross-player stack asks for the guest's component to be handed back, and
// the answer does not depend on the guest being there. Before this, the only
// path out of a composite was guest-initiated, so a guest who disconnected
// while docked stranded the docker indefinitely — no in-game recourse, and on
// 2026-08-02 recovery took a server-side --reset-fleet (#312).
//
// live reports session liveness. A live guest gets the ordinary handback on
// the owner's next reconcile; an absent one gets a Parcel: safed, placed
// across the subspace gap, and delivered with an explanation when they next
// connect. Deciding which at ASK time (rather than at reconcile) keeps the
// ledger's own reconcile free of session knowledge; a guest who reconnects in
// the gap simply receives a Parcel that is also correct.
//
// ok is false with the player-facing reason. The structural refusal — the
// guest's components sitting under the owner's after a control transfer (#314,
// ADR 0040 §5) — is decided against the World by GuestReleaseRefusal at the
// seat, which is where the composite can actually be inspected.
func (l *DockLedger) RequestRelease(owner string, live func(string) bool) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.records {
		if r.Owner != owner || r.Phase != DockActive {
			continue
		}
		r.releaseAsk = true
		r.releaseAsParcel = live != nil && !live(r.GuestOwner)
		return true, ""
	}
	return false, "release: not flying a cross-player stack"
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

// IsGuest reports whether fp is the guest in any active cross-player dock —
// the source for the Session roster's Docked-as-Guest marker (v0.28 S5).
func (l *DockLedger) IsGuest(fp string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.records {
		if r.GuestOwner == fp && r.Phase == DockActive {
			return true
		}
	}
	return false
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
	// Fast path (v0.28 finding 4): the overwhelmingly common tick has no live
	// docks, so skip the full owner scan — just clear any stale docked-as-guest
	// slate and return. Still under the single global lock: the 2-party MVP
	// runs at a scale where per-tick contention doesn't warrant fine-grained
	// locking; this only removes the O(records) walk when there's nothing to do.
	if len(l.records) == 0 {
		w.DockGuest = nil
		return nil
	}
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
		// #309: the composite this record names resolves in nobody's World.
		// Restore keeps the durable fields but the craft payloads are
		// transient, so a server restart that finds no live composite leaves
		// the record pointing at nothing — and nothing reaped it: the
		// fall-through below kept it, [U] kept it, and the guest was flagged
		// docked-as-guest to a phantom stack indefinitely (measured on the
		// production host: 5½ hours, cleared only by --reset-fleet). The stack
		// is gone, so the dock is over; abort it and let the guest's own
		// reconcile learn that and say so.
		//
		// ADR 0040 §1: unless the record is holding a payload. A restored
		// handover parks the ONLY copy of the stack on the record, which from
		// this seat is indistinguishable from a phantom — and reaping there
		// would destroy the craft durability exists to save.
		_, cidx, live := w.CraftByID(r.CompositeID)
		if !live {
			if !r.hasParkedPayload() {
				r.aborted = true
				r.undockAsk, r.transferTo = false, ""
			}
			// Either the dock just ended, or this record is holding the stack
			// itself pending delivery. Nothing below can act on a composite
			// that isn't in this World.
			return false
		}
		// Undock request: split the guest's component and hand it back.
		if r.undockAsk {
			r.undockAsk = false
			restored, ok := w.UndockGuest(cidx, r.GuestOwner, r.GuestCraftID)
			if !ok {
				// #307: the guest's components are no longer the top of the
				// stack — a control transfer moved them to the bottom without
				// restacking the vehicle, and peeling the tail there would
				// hand each player the other's hardware. Refusing is right;
				// staying silent is what made this look like a broken key, so
				// tell the guest and leave the dock standing.
				r.undockRefused = true
				return false
			}
			r.returnPayload = restored
			*chips = append(*chips, DockChip{Kind: sim.SessionEventUndocked, Handle: r.GuestHandle})
			return false
		}
		// Release request: the owner hands the guest's component back (ADR
		// 0040 §3). Same split as the guest's own ask; what differs is only
		// what happens when nobody is there to catch it.
		if r.releaseAsk {
			r.releaseAsk = false
			asParcel := r.releaseAsParcel
			r.releaseAsParcel = false
			restored, ok := w.UndockGuest(cidx, r.GuestOwner, r.GuestCraftID)
			if !ok {
				// The guest's components are under the owner's — a control
				// transfer moved them there without restacking the vehicle
				// (#314, §5). The seat refuses this synchronously via
				// GuestReleaseRefusal; reaching here means the stack changed
				// under the ask, so say so rather than mis-slice.
				*chips = append(*chips, DockChip{Kind: sim.SessionEventReleaseRefused, Handle: r.GuestHandle})
				return false
			}
			if asParcel {
				// Nobody is at the other seat, so the craft has to arrive fit
				// to be found: clear of the stack it left (here, while the
				// stack's state is what the push is relative to) and, at
				// delivery, inert and placed at the guest's own sim-time.
				sim.SeparationPush(restored)
				r.parcel = true
				r.parcelAtNano = w.Clock.SimTime.UnixNano()
			}
			r.returnPayload = restored
			*chips = append(*chips, DockChip{Kind: sim.SessionEventUndocked, Handle: r.GuestHandle})
			return false
		}
		// Transfer request: migrate the whole stack to the guest (roles swap),
		// unless mid-burn (refused, retried next tick).
		if r.transferTo != "" {
			comp := w.Crafts[cidx] // resolved above
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
	// Abort: the dock ended on the docker's side.
	if r.aborted {
		if r.returnPayload != nil {
			// The docker couldn't fuse — reclaim the handed-over craft.
			w.AdoptCraft(r.returnPayload, true)
			return true
		}
		// #309: nothing comes back — the stack this dock named no longer
		// exists, so the craft riding in it went with it. Drop the record so
		// the guest stops being flagged docked-as-guest to a phantom, and say
		// what happened: the docked marker vanishing on its own is the same
		// silence this batch is about.
		*chips = append(*chips, DockChip{Kind: sim.SessionEventDockLost, Handle: r.OwnerHandle})
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
			kind := sim.SessionEventUndocked
			if r.parcel {
				// A Parcel: released while I was not there. It has been
				// coasting on the record ever since, so place it at MY
				// sim-time before it joins my slate — a craft dropped in at
				// the owner's hour-old state would be kilometres wrong.
				if r.parcelAtNano != 0 {
					dt := w.Clock.SimTime.Sub(time.Unix(0, r.parcelAtNano)).Seconds()
					sim.PlaceAcrossSubspaceGap(r.returnPayload, dt)
				}
				// Safed at DELIVERY, not at release: the throttle and the
				// live burn state are not part of a craft's persisted form,
				// so safing at release would be undone by the first restart
				// the Parcel outlived. Arriving inert is the invariant; where
				// it is applied has to be the arrival.
				sim.SafeHandback(r.returnPayload)
				kind = sim.SessionEventParcelReturned
			}
			w.AdoptCraft(r.returnPayload, true)
			r.returnPayload = nil
			*chips = append(*chips, DockChip{Kind: kind, Handle: r.OwnerHandle})
			return true
		}
		// #307: my undock was refused on the owner's side. I'm still docked —
		// surface it so the key doesn't read as dead, then carry on.
		if r.undockRefused {
			r.undockRefused = false
			*chips = append(*chips, DockChip{Kind: sim.SessionEventUndockRefused, Handle: r.OwnerHandle})
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
