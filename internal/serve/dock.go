package serve

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Cross-player docking wiring (v0.28 S5). The reconcile hub in reporting.go
// calls reconcileDocking once per tick after ghosts + co-warp are refreshed:
// detect a fresh contact against a co-warp-coupled ghost, advance every dock
// touching this session against its World, fold the guest's warp coupling, and
// persist the durable cross-ref when a dock transitions. The heavy lifting is
// in relay.DockLedger; this is the serve-side glue that gives it the current
// World, roster handles, and disk.

// reconcileDocking runs this session's cross-player docking for one tick.
// coupled is ComputeCoWarp's per-owner coupled map; reports is the owner→report
// map; handles is fingerprint→handle. Chips it produces are appended to the
// session's local event slate. w is the session's World (already ghost/co-warp
// refreshed this tick).
func (m *reportingModel) reconcileDocking(w *sim.World, coupled map[string]bool, reports map[string]relay.CraftReport, handles map[string]string, now time.Time) {
	changed := false

	// Detect a fresh contact: the active craft has closed on a co-warp-coupled
	// ghost within the docking gates. Claim it (docker owns) — the guest's next
	// tick hands the craft over. Idempotent via the ledger's engaged-craft guard.
	if ghostOwner, ghostCraftID, ok := relay.DetectGuestContact(w, coupled); ok {
		if active := w.ActiveCraft(); active != nil {
			ownHandle := handles[m.owner]
			if _, claimed := m.srv.dock.Claim(m.owner, ownHandle, active.ID, ghostOwner, handles[ghostOwner], ghostCraftID); claimed {
				changed = true
			}
		}
	}

	// Advance every dock touching this session against its World.
	craftsBefore := len(w.Crafts)
	recordsBefore := m.srv.dock.RecordCount()
	chips := m.srv.dock.Reconcile(w, m.owner, reports)
	for _, c := range chips {
		m.localEvents = append(m.localEvents, sim.SessionEvent{
			Kind: c.Kind, Handle: c.Handle, Detail: c.Detail, At: now,
		})
		changed = true
	}
	// A record ENDING is a durable transition with none of the three traces
	// above (#326). The ADR 0038 §5 re-arm latch clearing is the case that
	// bit: entering cooldown persisted (it rides the undocked chip), leaving
	// it did not, so the delete lived in memory and the next restart re-seeded
	// a latch that by then had no way left to clear — a vessel permanently
	// un-dockable. The record count is the observable trace of any teardown.
	if m.srv.dock.RecordCount() != recordsBefore {
		changed = true
	}
	// The guest side of a DockPending record parks its craft on the record
	// (reconcileGuest lifts it out of w and into the ledger's in-memory
	// payload) without producing a chip — nothing player-facing happened
	// yet, so the chip loop above never sets changed. But that parking IS
	// the moment the craft's only copy moves off this World and onto the
	// ledger, so it needs the same flush a chip-producing transition gets:
	// a restart between here and whatever later transition happens to flush
	// would otherwise drop the craft silently (ADR 0040 review). A changed
	// craft count is the observable trace of that move.
	if len(w.Crafts) != craftsBefore {
		changed = true
	}

	// Docked-as-guest: fold the min-wins coupling to the stack owner into the
	// co-warp state (reuses S1's clampedWarp clamp), on top of any range couple.
	// The owner's live Away state is stamped here too (#253): the ledger
	// rebuilds DockGuest each tick from records + reports, neither of which
	// knows session liveness — only the server does.
	if w.DockGuest != nil {
		w.DockGuest.OwnerAway = m.srv.isAway(w.DockGuest.OwnerFP)
		w.CoWarp = w.CoWarp.WithDockCoupling(w.DockGuest.OwnerHandle, w.DockGuest.OwnerEffWarp)
	}

	// Docking is one of the two ways a standing rendezvous agreement ends
	// (ADR 0037 §1) — the other is an explicit cancel. Once the pair are
	// one stack there is no rendezvous left to hold, and the dock's own
	// coupling takes over the warp lock. Clearing the transition memory
	// alongside suppresses the cancel chip: this is the rendezvous
	// succeeding, and "docked with X" already says so.
	if arm := w.RendezvousArm; arm != nil && m.dockedWith(arm.TargetOwner) {
		if w.EndRendezvousOnDock(arm.TargetOwner) {
			m.rzPartnerOwner, m.rzPartnerHandle = "", ""
		}
	}

	// Persist the durable cross-ref on any transition so a reconnecting guest
	// resumes.
	if changed {
		_ = m.srv.persistDocks()
	}
}

// dockedWith reports whether this session and partner share a FUSED
// cross-player dock, in either direction (ADR 0037 §1's dock end
// condition — the seat that owns the stack flips on transfer, so the
// check must not care which side is which). Deliberately DockActive only:
// a pending claim is a dock being attempted, and it can still abort back
// to two free craft — ending the agreement on the claim would drop the
// pair's time lock in the middle of the approach that the claim is part
// of, which is the exact failure ADR 0037 exists to fix.
func (m *reportingModel) dockedWith(partner string) bool {
	if m.srv == nil || partner == "" {
		return false
	}
	for _, r := range m.srv.dock.Records() {
		if r.Phase != relay.DockActive {
			continue
		}
		if (r.Owner == m.owner && r.GuestOwner == partner) ||
			(r.GuestOwner == m.owner && r.Owner == partner) {
			return true
		}
	}
	return false
}

// transferControl handles a [J] press (ADR 0040 §2). The ledger decides;
// this turns a refusal into a moment the player actually sees, carrying the
// reason verbatim. A silent no-op here is what made [U] read as a broken key
// (#308) — the second cross-player verb does not repeat it.
func (m reportingModel) transferControl() (tea.Model, tea.Cmd) {
	// ADR 0040 §4: from the GUEST seat the same key means the mirror thing —
	// take the stick back. It is granted, not requested, when the owner's
	// seat is empty.
	if w := m.app.World(); w != nil && w.DockGuest != nil {
		return m.reclaimStack()
	}
	ok, why := m.srv.dock.RequestTransfer(m.owner, m.srv.presence.isOnline)
	if ok {
		// The ask lives only in the in-process ledger until this lands — a
		// restart before some later, unrelated transition happens to flush
		// it would silently drop the request (ADR 0040 review).
		_ = m.srv.persistDocks()
		return m, nil
	}
	m.localEvents = append(m.localEvents, sim.SessionEvent{
		Kind: sim.SessionEventTransferRefused, Detail: why, At: time.Now(),
	})
	m.app.Toast(why)
	return m, nil
}

// releaseGuest handles the owner seat's [U] on a cross-player stack (ADR
// 0040 §3). The App has already refused the structural case against its own
// World; what is decided here is whether the guest is there to catch it —
// live, and the handback is the ordinary one; gone, and it becomes a Parcel.
func (m reportingModel) releaseGuest() (tea.Model, tea.Cmd) {
	ok, why := m.srv.dock.RequestRelease(m.owner, m.srv.presence.isOnline)
	if ok {
		// Same reasoning as RequestTransfer above: flush the ask now rather
		// than leaving it to whatever transition happens to come next.
		_ = m.srv.persistDocks()
		return m, nil
	}
	m.localEvents = append(m.localEvents, sim.SessionEvent{
		Kind: sim.SessionEventTransferRefused, Detail: why, At: time.Now(),
	})
	m.app.Toast(why)
	return m, nil
}

// persistDocks writes the dock ledger's current durable cross-ref to disk under
// the persist guard (v0.28 finding 3). The ledger is the source of truth; the
// guard serialises persists across sessions and dock.Records() is re-snapshotted
// INSIDE the lock, so the last writer always persists the truly-current full
// ledger — a concurrent session's newly-added dock can't be lost to a stale
// snapshot racing to disk (ledger mutations are already ledger-mutex serial, so
// re-reading under the guard converges the persisted state).
func (s *Server) persistDocks() error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	return s.store.SetDocks(snapshotsToDockLinks(s.dock.FullRecords()))
}

// dockLinksToSnapshots adapts the persisted cross-ref into live ledger
// records. ADR 0040: the in-flight half rides along, so a handover parked
// when the server went down completes on the recipient's next connect.
func dockLinksToSnapshots(links []sessiondir.DockLink) []relay.DockSnapshot {
	out := make([]relay.DockSnapshot, 0, len(links))
	for _, l := range links {
		out = append(out, relay.DockSnapshot{
			ID: l.ID, Owner: l.Owner, OwnerHandle: l.OwnerHandle,
			DockerCraftID: l.DockerCraftID, CompositeID: l.CompositeID,
			GuestOwner: l.GuestOwner, GuestHandle: l.GuestHandle,
			GuestCraftID: l.GuestCraftID, Phase: relay.DockPhase(l.Phase),
			GuestPayload: l.GuestPayload, ReturnPayload: l.ReturnPayload,
			TransferPayload: l.TransferPayload,
			UndockAsk:       l.UndockAsk, UndockRefused: l.UndockRefused,
			TransferTo: l.TransferTo, Aborted: l.Aborted,
			ReleaseAsk: l.ReleaseAsk, ReleaseAsParcel: l.ReleaseAsParcel,
			Parcel: l.Parcel, ParcelAtNano: l.ParcelAtNano,
			ReclaimNotice: l.ReclaimNotice, ReclaimAtNano: l.ReclaimAtNano,
			DockNotice: l.DockNotice, ReturnAtNano: l.ReturnAtNano,
		})
	}
	return out
}

// snapshotsToDockLinks projects the live ledger's full state to the persisted
// form.
func snapshotsToDockLinks(snaps []relay.DockSnapshot) []sessiondir.DockLink {
	out := make([]sessiondir.DockLink, 0, len(snaps))
	for _, r := range snaps {
		out = append(out, sessiondir.DockLink{
			ID: r.ID, Owner: r.Owner, OwnerHandle: r.OwnerHandle,
			DockerCraftID: r.DockerCraftID, CompositeID: r.CompositeID,
			GuestOwner: r.GuestOwner, GuestHandle: r.GuestHandle,
			GuestCraftID: r.GuestCraftID, Phase: int(r.Phase),
			GuestPayload: r.GuestPayload, ReturnPayload: r.ReturnPayload,
			TransferPayload: r.TransferPayload,
			UndockAsk:       r.UndockAsk, UndockRefused: r.UndockRefused,
			TransferTo: r.TransferTo, Aborted: r.Aborted,
			ReleaseAsk: r.ReleaseAsk, ReleaseAsParcel: r.ReleaseAsParcel,
			Parcel: r.Parcel, ParcelAtNano: r.ParcelAtNano,
			ReclaimNotice: r.ReclaimNotice, ReclaimAtNano: r.ReclaimAtNano,
			DockNotice: r.DockNotice, ReturnAtNano: r.ReturnAtNano,
		})
	}
	return out
}
