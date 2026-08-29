package serve

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// reportingModel wraps a session's game: it feeds the relay store on
// every sim tick (v0.27 S4), refreshes the world's ghost slate (S5)
// and session slate (S6), and executes the Session screen's admin
// commands against the session store — everything runs inside the
// session's own update loop, same goroutine as the World mutation.
// The wrapper is transparent to everything else.
type reportingModel struct {
	inner tea.Model
	app   *tui.App
	rep   *relay.Reporter
	srv   *Server
	owner string

	// port is the listener port a lazy [h] start binds (v0.28 S3).
	// When srv is nil the wrapper is inert — solo play — until the
	// Session screen's [h] starts hosting; srv/rep/owner come alive
	// then. The --serve headless path hands srv in already live.
	port int

	// meta cache: roster + invites, refreshed lazily so each tick
	// doesn't re-read session.json. Admin commands force a refresh so
	// a freshly minted code shows immediately.
	meta   sessiondir.Meta
	metaAt time.Time

	// localEvents are this session's own moments (the "synced to X"
	// arrival chip, co-warp couple/release) — appended to the world's
	// event slate alongside the broadcast ring, pruned by the same wall
	// TTL.
	localEvents []sim.SessionEvent

	// coWarp is the per-owner coupled memory (v0.28 S1) — ComputeCoWarp's
	// hysteresis input, carried across ticks so a coupled pair keeps the
	// wider decouple gate. Owner fingerprint → coupled-last-tick.
	coWarp map[string]bool

	// Rendezvous Warp transition memory (v0.29 S2): last tick's arm /
	// invite / degrade state, so refreshSession can chip only the
	// transitions (armed / cancelled / degraded) instead of every tick.
	rzPartnerOwner  string // outgoing arm's target last tick ("" = unarmed)
	rzPartnerHandle string
	rzInviteFrom    string // incoming invite's owner last tick ("" = none)
	rzInviteHandle  string
	rzDegraded      bool

	// targetLockOwner / targetLockCraftID identify the specific ghost ref
	// the give-up countdown below is tracking (#294 review finding 2) —
	// retargeting to a different peer mid-grace must start a fresh watch
	// rather than inherit the old ref's timer and (worst case) chip the
	// wrong peer's name. A tick that finds w.Target pointing at a
	// different ref than these (including the very first tick, when both
	// are zero-valued) resets targetLockPendingSince and
	// targetLockResolvedOnce below.
	targetLockOwner   string
	targetLockCraftID uint64

	// targetLockPendingSince tracks the deferred re-latch of a craft/ghost
	// target lock across a reconnect (#294): a ghost target now survives
	// the per-player save round-trip (CraftToWire/CraftFromWire), so a
	// session that comes up already aimed at one just needs the target
	// owner's craft reports to resume before ResolveTargetGhost finds it
	// again.
	//
	// #294 review round 3 (presence rule): this timer only ever runs for
	// an ABSENT owner — one who is not a member of this session's roster
	// at all (see reconcileTargetLock). A PRESENT owner's unresolved
	// ghost (landed, viewing a different system, or simply hasn't
	// reported yet) gets the same tolerance an ordinary momentarily-stale
	// ghost already has: wait silently, forever if need be, re-latch
	// whenever it resolves. Zero means "not counting down" (no ghost
	// target, the ref changed, it already resolved, or the owner is
	// present). Set the tick reconcileTargetLock first finds the current
	// ref unresolved AND absent; cleared on resolution, on the ref
	// changing to something else, on the owner becoming present, or on
	// giving up past targetLockRelatchGrace (which also fires the loss
	// chip).
	targetLockPendingSince time.Time

	// targetLockResolvedOnce is set the first time the CURRENTLY TRACKED
	// ref (targetLockOwner/targetLockCraftID) resolves. #294 review
	// finding 1: once true, the give-up countdown retires permanently for
	// this ref — a later resolve failure (the viewer browsing to another
	// system, since relay.GhostsFor only emits ghosts for the VIEWED
	// system, or the peer landing/transferring for a minute, both of
	// which drop the ghost out of the slate through no fault of the lock)
	// reverts to the pre-#294 tolerance: keep the lock, re-latch silently
	// whenever it resolves again, never clear it and never chip a loss.
	targetLockResolvedOnce bool
}

// localEventTTL matches the chip's on-screen life; pruning here just
// keeps the slice from growing over a long session.
const localEventTTL = 10 * time.Second

// targetLockRelatchGrace bounds how long a reconnected session waits for
// an unresolved craft/ghost target lock to come back before giving up and
// telling the player (#294). Sized like defaultAwayAfter (60s, away.go):
// long enough to cover the ordinary reconnect skew between two players
// both dropped by the same [u] restart, short enough that a lock that
// really is gone doesn't sit silently un-explained for minutes.
const targetLockRelatchGrace = 45 * time.Second

// restartExitCode is the dedicated marker the supervising service
// manager keys on to tell an admin-requested restart from a crash
// (v0.30 S4, contract agreed with the deploy host): systemd's
// ExecStopPost runs tsp-adopt (pull + verify + install) only on
// $EXIT_STATUS == 42, then Restart=always relaunches. 42 is clear of
// clean-exit (0), Go panic (2), and signal death (128+N), so it is
// unambiguous. A plain os.Exit, not a signal — a child can't cleanly
// signal its supervisor to do work between restarts.
const restartExitCode = 42

// exitFunc indirects os.Exit so the drain-and-restart path is testable
// without killing the test process. restartAnnounceGrace is the pause
// between broadcasting the restart notice and closing the listener, so
// connected screens render the warning before they are dropped; tests
// zero it.
var (
	exitFunc             = os.Exit
	restartAnnounceGrace = 1500 * time.Millisecond
)

const metaRefresh = 5 * time.Second

// withReporting wraps app so its world reports to the store as owner.
// Called once per new session (ssh connect/reconnect, or the host
// starting to host), so it's also the single place that delivers a
// player's pending session note (#274) — today only the legacy
// handle-collision auto-rename produces one, surfaced as a toast on
// the owner's own screen the moment their session comes up, then
// cleared so it fires exactly once.
func (s *Server) withReporting(app *tui.App, owner string) tea.Model {
	if note, err := s.store.ConsumePendingNote(owner); err == nil && note != "" {
		app.Toast(note)
	}
	return reportingModel{
		inner: app, app: app,
		rep: relay.NewReporter(s.relay, owner),
		srv: s, owner: owner,
	}
}

func (m reportingModel) Init() tea.Cmd { return m.inner.Init() }

func (m reportingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Lazy hosting lifecycle (v0.28 S3): [h] on the Session screen
	// arrives here as a SessionHostMsg. Start binds the listener;
	// stop shuts it down. Handled before the pass-through so the
	// inner App never sees it.
	if host, ok := msg.(screens.SessionHostMsg); ok {
		if host.Start {
			return m.startHosting()
		}
		return m.stopHosting()
	}

	// Admin server restart (v0.30 S4): drain everyone and exit with the
	// supervisor marker. Inert without a server; authorization enforced
	// here, not just in the UI.
	if _, ok := msg.(screens.SessionRestartMsg); ok && m.srv != nil {
		return m.restartServer()
	}

	// Cross-player docking intents (v0.28 S5): the App can't reach the dock
	// ledger (sim sits below serve), so a flight key emits one of these and
	// the wrapper acts on it. Inert without a server. RequestUndock is the
	// guest's undock-anytime signal; RequestTransfer hands the stack over.
	if _, ok := msg.(tui.UndockGuestMsg); ok {
		if m.srv != nil {
			if w := m.app.World(); w.DockGuest != nil {
				if m.srv.dock.RequestUndock(m.owner, w.DockGuest.GuestCraftID) {
					// The ask lives only in the in-process ledger until this
					// lands — same unflushed-window fix as RequestTransfer /
					// RequestRelease (ADR 0040 review).
					_ = m.srv.persistDocks()
				}
			}
		}
		return m, nil
	}
	if _, ok := msg.(tui.ReleaseGuestMsg); ok {
		if m.srv != nil {
			return m.releaseGuest()
		}
		return m, nil
	}
	if _, ok := msg.(tui.TransferControlMsg); ok {
		if m.srv != nil {
			return m.transferControl()
		}
		return m, nil
	}

	// Chat send (ADR 0035 S2): the input overlay emits the line; the
	// wrapper owns the ring. The overlay already refuses unmatched or
	// offline @handles (keeping the typed text), but that is UX — this
	// handler re-checks liveness so a stale roster snapshot can't slip a
	// DM to a dead session, and resolves the sender's handle through the
	// roster so the rendered name can't be spoofed by the screen.
	if chat, ok := msg.(tui.ChatSendMsg); ok {
		if m.srv == nil || strings.TrimSpace(chat.Text) == "" {
			return m, nil
		}
		if chat.To != "" && !m.srv.presence.isOnline(chat.To) {
			// Hand the draft back instead of destroying it — the target
			// can drop in the gap between the roster tick that showed
			// them online and Enter (v0.32 review finding). Mirrors the
			// App-side refusals, which all keep the text.
			m.app.Toast(chat.ToHandle + " is offline — not sent")
			m.app.RestoreChatDraft("@" + chat.ToHandle + " " + chat.Text)
			return m, nil
		}
		ownHandle := m.owner
		h, ok := m.handleOf(m.owner)
		if !ok {
			// A send can beat the first tick's lazy meta refresh — read
			// the roster now rather than render a raw fingerprint.
			if meta, err := m.srv.store.Meta(); err == nil {
				m.meta, m.metaAt = meta, time.Now()
				h, ok = m.handleOf(m.owner)
			}
		}
		if ok {
			ownHandle = h
		}
		m.srv.chat.post(m.owner, ownHandle, chat.To, chat.ToHandle, chat.Text)
		return m, nil
	}

	// Session-admin intents from the Session screen (v0.27 S6): the
	// wrapper owns the store access; the App below only dispatched.
	// Inert until a server exists (solo has nothing to administer).
	if admin, ok := msg.(screens.SessionAdminMsg); ok && m.srv != nil {
		// Authorization is a capability enforced HERE, not in the UI (v0.30
		// S1, #222). The Session screen hides admin keys from guests, but
		// that is UX, not the security boundary: a guest's forged intent
		// reaches this handler directly and must be refused. Refusal is a
		// silent no-op plus a toast to the sender — never a crash.
		if !m.srv.store.MayAdminister(m.owner) {
			m.app.Toast("you can't administer this session")
			return m, nil
		}
		switch admin.Cmd.Kind {
		case screens.SessionCmdMint:
			// #274: MintInvite refuses a handle that case-insensitively
			// collides with the roster or another outstanding invite —
			// this used to be silently discarded (`_, _ = ...`), so the
			// host never learned a mint failed and could hand out a code
			// bound to a handle that could never enroll cleanly.
			if _, err := m.srv.store.MintInvite(admin.Cmd.Handle); err != nil {
				m.app.Toast(err.Error())
			}
		case screens.SessionCmdRevoke:
			_ = m.srv.store.RevokeInvite(admin.Cmd.Code)
		case screens.SessionCmdRemove:
			// Target-aware guardrail (v0.30 S3): an admin may remove guests
			// but not the host, another admin, or themselves. MayAdminister
			// passed above; MayRemove adds the actor×target rules.
			if !m.srv.store.MayRemove(m.owner, admin.Cmd.Fingerprint) {
				m.app.Toast("you can't remove that player")
				return m, nil
			}
			_ = m.srv.store.RemovePlayer(admin.Cmd.Fingerprint)
		case screens.SessionCmdPromote, screens.SessionCmdDemote:
			// Delegation is host-only — an admin can neither create nor
			// remove another admin (single-rooted, v0.30 S2). MayAdminister
			// passed (host or admin); narrow to the host via MayDelegate.
			if !m.srv.store.MayDelegate(m.owner) {
				m.app.Toast("only the host can promote or demote admins")
				return m, nil
			}
			if admin.Cmd.Kind == screens.SessionCmdPromote {
				_ = m.srv.store.PromoteAdmin(admin.Cmd.Fingerprint)
			} else {
				_ = m.srv.store.DemoteAdmin(admin.Cmd.Fingerprint)
			}
		}
		m.metaAt = time.Time{} // force refresh — the list is the feedback
		m.refreshSession(time.Now())
		return m, nil
	}

	inner, cmd := m.inner.Update(msg)
	m.inner = inner
	// Solo (no listener): pure pass-through — no store, no reports.
	if _, ok := msg.(sim.TickMsg); ok && m.srv != nil {
		now := time.Now()
		w := m.app.World()
		m.rep.Tick(w, now)
		// Sync arrival (S7): chip on both sides — broadcast "X synced
		// to you" through the presence ring, keep "synced to X" local.
		if arr := w.LastSyncArrival; arr != nil {
			w.LastSyncArrival = nil
			ownHandle := m.owner
			if h, ok := m.handleOf(m.owner); ok {
				ownHandle = h
			}
			// Addressed at the player whose subspace was joined — third
			// parties don't get told "X synced to you" (review follow-up).
			m.srv.presence.event(sim.SessionEventSync, m.owner, ownHandle, arr.Owner)
			m.localEvents = append(m.localEvents, sim.SessionEvent{
				Kind: sim.SessionEventSyncedTo, Owner: m.owner, Handle: arr.Handle, At: now,
			})
		}
		m.refreshSession(now)
	}
	return m, cmd
}

// emitRendezvousEvents derives the Rendezvous Warp session moments from
// the World slate's tick-over-tick transitions (v0.29 S2): a partner's
// new arm toward the viewer, the proximity handoff (consuming the sim's
// LastRendezvousArrival, mirroring the Sync arrival), a waypoint advance
// on the standing intent (#252, consuming LastRendezvousWaypoint), a
// cancel/retract releasing the arm, and the hold-τ degrade flag going
// up. All local-only chips — each side derives its own from its own
// World.
func (m *reportingModel) emitRendezvousEvents(w *sim.World, now time.Time) {
	chip := func(kind sim.SessionEventKind, handle string) {
		m.localEvents = append(m.localEvents, sim.SessionEvent{Kind: kind, Handle: handle, At: now})
	}

	// Arrival first: it clears the arm too, and must not read as a cancel.
	arrived := false
	if arr := w.LastRendezvousArrival; arr != nil {
		w.LastRendezvousArrival = nil
		chip(sim.SessionEventRendezvousArrived, arr.Handle)
		arrived = true
	}
	// Waypoint advance (#252): the standing intent passed an encounter
	// outside couple range and re-aimed at the next one. The arm stays
	// up, so this can never read as a cancel — but it must be visible
	// (a silent advance reads as the coast being broken).
	if wp := w.LastRendezvousWaypoint; wp != nil {
		w.LastRendezvousWaypoint = nil
		chip(sim.SessionEventRendezvousWaypoint, wp.Handle)
	}
	// Outgoing arm released before τ — own cancel, partner retract, or
	// partner drop all land here.
	if m.rzPartnerOwner != "" && w.RendezvousArm == nil && !arrived {
		chip(sim.SessionEventRendezvousCancelled, m.rzPartnerHandle)
	}
	if arm := w.RendezvousArm; arm != nil {
		// The arm carries its own display handle (captured at Engage) so
		// the cancel chip never needs a roster lookup (v0.29 review).
		m.rzPartnerOwner, m.rzPartnerHandle = arm.TargetOwner, arm.Handle
	} else {
		m.rzPartnerOwner, m.rzPartnerHandle = "", ""
	}

	// Incoming invite: chip the arm moment; a retracted-unanswered invite
	// (initiator cancelled, or τ passed) chips as cancelled. A Blocked
	// invite (#250 subspace gap) is neither: the "[y] join" moment would
	// lie while the join is suppressed (the persistent chip carries the
	// attribution), and its appearance is a gap, not a retract — so the
	// bookkeeping just resets, and re-convergence chips the arm moment
	// fresh.
	switch inv := w.RendezvousInvite; {
	case inv != nil && inv.Blocked:
		m.rzInviteFrom, m.rzInviteHandle = "", ""
	case inv != nil && inv.Owner != m.rzInviteFrom:
		chip(sim.SessionEventRendezvousArmed, inv.Handle)
		m.rzInviteFrom, m.rzInviteHandle = inv.Owner, inv.Handle
	case inv == nil && m.rzInviteFrom != "" && w.RendezvousArm == nil:
		// Gone without the viewer arming back — not a respond, a retract.
		chip(sim.SessionEventRendezvousCancelled, m.rzInviteHandle)
		fallthrough
	case inv == nil:
		m.rzInviteFrom, m.rzInviteHandle = "", ""
	}

	// Hold-τ degrade: warn on the up-transition only (the persistent
	// RENDEZVOUS chip carries the live approach readout).
	if w.RendezvousDegraded && !m.rzDegraded {
		chip(sim.SessionEventRendezvousDegraded, m.rzPartnerHandle)
	}
	m.rzDegraded = w.RendezvousDegraded
}

// handleOf resolves a fingerprint through the cached roster.
func (m *reportingModel) handleOf(fp string) (string, bool) {
	for _, p := range m.meta.Roster {
		if p.Fingerprint == fp {
			return p.Handle, true
		}
	}
	return "", false
}

// reconcileTargetLock drives the deferred re-latch of a craft/ghost
// target lock across a reconnect (#294). Called every tick after
// w.Ghosts is refreshed, so ResolveTargetGhost sees this tick's data.
//
// #294 review round 3 (presence rule) replaced the earlier
// first-tick-eligibility inference (rounds 1 + 2: targetLockResolvedOnce
// / targetLockEligible / targetLockTicked) with a simpler test that
// needs no session-timing bookkeeping at all: the 45s give-up countdown
// runs ONLY when the ref has never resolved this session AND its owner
// is ABSENT from the session — not a member of the roster `handles`
// derives from, at all. Presence, not timing, is what the countdown was
// always trying to approximate:
//
//   - An owner who IS present (enrolled in this session) but whose
//     craft isn't currently resolvable — landed, viewing a different
//     system (relay.GhostsFor only emits ghosts for the VIEWED system),
//     or simply hasn't reported yet this tick — is never a countdown
//     case. The lock waits silently and re-latches the moment it comes
//     back into view, no matter how long that takes.
//
//   - A LIVE SetTargetGhost (a player's own retarget, a Session-screen
//     pick, or ADR 0038's undock handback aiming the docker at the
//     guest's departing craft) always points at a PRESENT owner — they
//     have to be in the roster to have a ghost to aim at in the first
//     place — so it never starts a countdown either. The old
//     eligibility flags existed only to approximate this same fact from
//     session timing; presence gets it directly, and for every case at
//     once (undock races included), not just the first tick.
//
//   - Only a ref whose owner was never enrolled in this session (a
//     standalone save loaded outside the session it was bound in), or
//     has since been removed from the roster, is genuinely a "will this
//     ever come back" question — that's the case the countdown bounds.
//
//   - Target isn't a ghost: nothing pending, reset tracking, no-op.
//
//   - Target names a different ref than the one being tracked
//     (including the very first tick, since the tracked ref starts
//     zero-valued): fresh ref, fresh watch — never inherit another
//     ref's timer, resolved-once state, or display handle (a cached
//     handle from the old ref would chip the WRONG peer's name).
//
//   - Target resolves: mark this ref resolved-once and clear any
//     pending timer — either it never needed re-latching (ordinary
//     play, the common case) or the owner's reports just resumed.
//     Silent either way; the player already has the lock they expect.
//
//   - Target doesn't resolve and has already resolved once before:
//     old-behavior tolerance — no timer, no clearing, no chip.
//
//   - Target doesn't resolve and the owner IS present: no countdown —
//     clear any timer a prior absence had started (a removed-then-
//     re-added owner isn't punished for the gap) and wait.
//
//   - Target doesn't resolve and the owner is ABSENT: start (or
//     continue) the timer.
//
//   - Still unresolved past targetLockRelatchGrace with the owner
//     absent throughout: give up — clear the target, abort any
//     matching planted node / active burn (CancelGhostNodeRefs), and
//     chip the loss (handle read now, at fire time — never cached at
//     timer-start) so the drop is legible instead of a silent dangling
//     aim.
func (m *reportingModel) reconcileTargetLock(w *sim.World, handles map[string]string, now time.Time) {
	if w.Target.Kind != sim.TargetGhost {
		m.resetTargetLockWatch()
		return
	}
	if w.Target.GhostOwner != m.targetLockOwner || w.Target.CraftID != m.targetLockCraftID {
		m.targetLockOwner, m.targetLockCraftID = w.Target.GhostOwner, w.Target.CraftID
		m.targetLockPendingSince = time.Time{}
		m.targetLockResolvedOnce = false
	}
	if _, _, ok := w.ResolveTargetGhost(); ok {
		m.targetLockResolvedOnce = true
		m.targetLockPendingSince = time.Time{}
		return
	}
	if m.targetLockResolvedOnce {
		return
	}
	if _, present := handles[w.Target.GhostOwner]; present {
		m.targetLockPendingSince = time.Time{}
		return
	}
	if m.targetLockPendingSince.IsZero() {
		m.targetLockPendingSince = now
		return
	}
	if now.Sub(m.targetLockPendingSince) <= targetLockRelatchGrace {
		return
	}
	// Handle may be "" (the owner left the roster) — the chip builder
	// renders a handle-less fallback line rather than showing a blank.
	// Read now, not cached at timer-start.
	handle := handles[w.Target.GhostOwner]
	owner, craftID := w.Target.GhostOwner, w.Target.CraftID
	w.ClearTarget()
	w.CancelGhostNodeRefs(owner, craftID)
	m.localEvents = append(m.localEvents, sim.SessionEvent{
		Kind: sim.SessionEventTargetLockLost, Handle: handle, At: now,
	})
	m.resetTargetLockWatch()
}

// resetTargetLockWatch clears every field reconcileTargetLock uses to
// track its CURRENT ref, so the next ref bound starts from a clean
// slate — shared by "no ghost target" and "gave up past grace". Also
// called by startHosting/stopHosting (#294 review finding 3) so a stale
// hours-old timer from one hosting session can never survive into the
// next and fire an instant false-loss chip on its first tick.
func (m *reportingModel) resetTargetLockWatch() {
	m.targetLockOwner, m.targetLockCraftID = "", 0
	m.targetLockPendingSince = time.Time{}
	m.targetLockResolvedOnce = false
}

// refreshSession rebuilds the world's ghost + session slates from the
// store, roster, and presence.
func (m *reportingModel) refreshSession(now time.Time) {
	if m.meta.Version == 0 || now.Sub(m.metaAt) >= metaRefresh {
		if meta, err := m.srv.store.Meta(); err == nil {
			m.meta = meta
			m.metaAt = now
		}
	}
	w := m.app.World()

	handles := make(map[string]string, len(m.meta.Roster))
	for _, p := range m.meta.Roster {
		handles[p.Fingerprint] = p.Handle
	}
	// Ghosts (S5): everyone else's craft at this world's sim-time.
	others := m.srv.relay.Snapshot(m.owner)
	w.Ghosts = relay.GhostsFor(w, others, handles)

	// Deferred re-latch of a craft/ghost target lock (#294). w.Target
	// arrives here already TargetGhost if it was saved that way (the
	// save package now persists it, see CraftToWire) — a reconnect right
	// after a restart lands with the ghost slate still empty, same as
	// this world's very first tick after connect. Nothing here forces
	// resolution; it just watches ResolveTargetGhost each tick (now that
	// w.Ghosts is fresh) and gives up after targetLockRelatchGrace.
	m.reconcileTargetLock(w, handles, now)

	// Co-warp (v0.28 S1, ADR 0034 §5): couple the viewer's active craft
	// to any nearby same-subspace player and write the min-over-Effective
	// clamp onto the World for next tick's clampedWarp; emit couple/
	// release chips on transitions. Same seam as ghosts — reads the
	// store's reports (which now carry EffWarp), writes transient state.
	// Session liveness for the adapter's rendezvous-arm gate (#252 review,
	// finding 1): presence is the serve layer's "has a live session right
	// now" — marked online at connect/enroll, offline only when the session
	// unwinds through persistMiddleware. A reprieved-away session's
	// connection is still up, so it stays online and its arm stays honored
	// (silence is not retract); a session reaped at the Reprieve ceiling,
	// or any for-good disconnect, drops out of presence while its final
	// report sits frozen in the relay store forever — exactly the report
	// whose arm must NOT keep the survivor's standing intent alive.
	//
	// Away rides along per owner (#253): reports say what a peer's world
	// is doing, only the server knows whether anyone is at its controls,
	// and the flight view needs that as standing state, not a 6 s chip.
	live := make(map[string]bool, len(others))
	away := make(map[string]bool, len(others))
	for _, r := range others {
		if m.srv.presence.isOnline(r.Owner) {
			live[r.Owner] = true
		}
		away[r.Owner] = m.srv.isAway(r.Owner)
	}
	peers := relay.CoWarpPeersFrom(w, others, handles, m.owner, live, away)
	// Rendezvous Warp (v0.29 S1): start or cancel the shared coast to the
	// committed encounter from this tick's mutual-arm state, before the
	// clamp reads the couple. Arrival + arm bookkeeping live in the sim.
	w.DriveRendezvousWarp(peers)
	// Rendezvous Warp chips (v0.29 S2): turn this tick's slate
	// transitions into session moments.
	m.emitRendezvousEvents(w, now)
	cw := w.ComputeCoWarp(peers, m.coWarp)
	m.coWarp = cw.CoupledOwners
	w.CoWarp = cw.State
	for _, h := range cw.NewlyCoupled {
		m.localEvents = append(m.localEvents, sim.SessionEvent{
			Kind: sim.SessionEventCoWarpCoupled, Handle: h, At: now,
		})
	}
	for _, h := range cw.Released {
		m.localEvents = append(m.localEvents, sim.SessionEvent{
			Kind: sim.SessionEventCoWarpReleased, Handle: h, At: now,
		})
	}

	// Session slate (S6). Snapshot("") includes the viewer's own
	// report — the roster row marked "you".
	reports := map[string]relay.CraftReport{}
	for _, r := range m.srv.relay.Snapshot("") {
		reports[r.Owner] = r
	}
	// A sync target is a moving clock (review follow-up): while the
	// chase runs, re-freeze T from the leader's latest report — the
	// node-edit re-freeze pattern applied to subspaces. Same goroutine
	// as the tick, so the write is safe.
	if w.AutoWarp != nil && w.AutoWarp.Sync && w.AutoWarp.SyncOwner != "" {
		if rep, ok := reports[w.AutoWarp.SyncOwner]; ok && rep.SubspaceTime.After(w.AutoWarp.T) {
			w.AutoWarp.T = rep.SubspaceTime
		}
	}
	// Cross-player docking (v0.28 S5): detect contact against a co-warp-
	// coupled ghost, advance every dock touching this session, fold the
	// docked-as-guest coupling, and persist the cross-ref on transitions.
	// Runs after ghosts + co-warp so detection sees fresh ghost positions
	// and cw.CoupledOwners, and before the roster build so DockedGuest is
	// current. Mutates w (fuses/splits craft) — same goroutine as the tick.
	m.reconcileDocking(w, cw.CoupledOwners, reports, handles, now)

	// Rider view (ADR 0038 S4): while riding in another player's stack,
	// name which of the owner's reported craft IS that stack (their
	// ActiveCraftID — DockGuestCraft always fuses onto the docker's
	// existing craft in place, so it keeps naming the composite for as
	// long as the owner flies it) and point the camera at it. Read here
	// rather than inside reconcileDocking/setDockGuest because both those
	// live in the relay ledger, which only knows dock records — not which
	// of a report's crafts the owner is actually flying.
	if w.DockGuest != nil {
		if rep, ok := reports[w.DockGuest.OwnerFP]; ok {
			w.DockGuest.OwnerActiveCraftID = rep.ActiveCraftID
		}
		w.FollowDockGuestStack()
	}

	info := &sim.SessionInfo{
		IsHost:        m.owner == sessiondir.HostFingerprint,
		CanAdminister: m.srv.store.MayAdminister(m.owner),
		Self:          m.owner,
	}
	// Version surface (v0.30 S5): universal readout, adopt gated.
	if m.srv.ver != nil {
		info.RunningVersion, info.AvailableVersion, info.AdoptCapable = m.srv.ver.snapshot()
	}
	// Rendezvous roster markers (v0.29 S2): who is armed toward the
	// viewer, and whom the viewer is armed toward.
	armedTowardViewer := map[string]bool{}
	// Live range per player (ADR 0037 §5): measured off the same peer set
	// the couple gate itself reads, so the number in the RANGE column and
	// the number the clamp decides on can never be two different things.
	rangeTo := map[string]float64{}
	for _, p := range peers {
		if p.ArmedTowardViewer {
			armedTowardViewer[p.Owner] = true
		}
		if r, ok := w.PeerRange(p); ok {
			rangeTo[p.Owner] = r
		}
	}
	for _, p := range m.meta.Roster {
		row := sim.SessionPlayer{
			Fingerprint: p.Fingerprint,
			Handle:      p.Handle,
			Role:        p.Role,
			Online:      m.srv.presence.isOnline(p.Fingerprint),
			// Docked-as-Guest marker goes live in v0.28 S5 (inert in v0.27):
			// true while any of this player's craft rides in another player's
			// live stack.
			DockedGuest: m.srv.dock.IsGuest(p.Fingerprint),

			// Away (ADR 0036 S5): still simulating, nobody at the controls.
			// Without it a reprieved session reads as Online for hours and
			// the roster tells a partner someone is there who is not.
			Away: m.srv.isAway(p.Fingerprint),

			WantsRendezvous: armedTowardViewer[p.Fingerprint],
			RendezvousOut:   w.RendezvousArm != nil && w.RendezvousArm.TargetOwner == p.Fingerprint,
		}
		if r, ok := rangeTo[p.Fingerprint]; ok {
			row.HasRange, row.RangeM = true, r
		}
		if rep, ok := reports[p.Fingerprint]; ok {
			row.HasReport = true
			row.DeltaT = rep.SubspaceTime.Sub(w.Clock.SimTime)
			row.CraftCount = len(rep.Crafts)
			// LOCATION follows the craft they are FLYING, not a fixed slot
			// (#288) — a partner reads this column to find someone, and slot
			// 0 is wherever their oldest vessel happens to be.
			if active, aok := rep.ActiveCraft(); aok {
				row.System = active.System
				row.Primary = active.Primary
			}
		}
		info.Players = append(info.Players, row)
	}
	if info.CanAdminister {
		for _, inv := range m.meta.Invites {
			info.Invites = append(info.Invites, sim.SessionInvite{
				Code:   inv.Code,
				Handle: inv.Handle,
				Age:    now.Sub(inv.CreatedAt),
			})
		}
	}
	w.Session = info

	// Broadcast moments (own excluded) + this session's local ones.
	events := m.srv.presence.eventsFor(m.owner)
	// ADR 0036 S6: bank this session's own moments while nobody is
	// watching, and replay them when somebody is. Before the TTL trim
	// below — that trim is what would otherwise destroy them.
	m.bankOrReplay(w.Clock.SimTime, now)
	kept := m.localEvents[:0]
	for _, e := range m.localEvents {
		if now.Sub(e.At) <= localEventTTL {
			kept = append(kept, e)
		}
	}
	m.localEvents = kept
	w.SessionEvents = append(events, m.localEvents...)

	// Chat slate (ADR 0035): the viewer's cut of the chat ring — own
	// lines included, other players' DMs excluded.
	w.ChatLines = m.srv.chat.linesFor(m.owner)
}

func (m reportingModel) View() string { return m.inner.View() }

// HostModel wraps the host's own in-process game so the host's craft
// enter the store like any guest's (the host is roster entry #1, not
// a special case on the wire). main runs the returned model.
func (s *Server) HostModel(app *tui.App) tea.Model {
	return s.withReporting(app, sessiondir.HostFingerprint)
}

// WrapHost always wraps app in the reporting model (v0.28 S3): the
// wrapper is now present in solo play too. A non-nil srv (the --serve
// headless path) reports immediately as the host; a nil srv stays
// inert until [h] on the Session screen lazily binds a listener on
// port. Value-receiver models: main reads back the final model's
// HostServer() to shut a lazily started listener down at exit.
func WrapHost(app *tui.App, srv *Server, port int) tea.Model {
	m := reportingModel{inner: app, app: app, port: port}
	if srv != nil {
		m.srv = srv
		m.owner = sessiondir.HostFingerprint
		m.rep = relay.NewReporter(srv.relay, m.owner)
	}
	return m
}

// HostServer returns the live listener when this wrapper is hosting,
// else nil — the door main uses after Run to shut a lazily started
// server down gracefully.
func (m reportingModel) HostServer() *Server { return m.srv }

// startHosting lazily binds the SSH listener and flips the wrapper
// live as the host (v0.28 S3). Bind failures — port already in use,
// host-key trouble — surface as a toast on the host's own screen
// instead of a pre-TUI stderr line. Idempotent: a second [h] while
// already hosting is a no-op.
func (m reportingModel) startHosting() (tea.Model, tea.Cmd) {
	if m.srv != nil {
		return m, nil
	}
	// #294 review finding 3: begin the target-lock watchdog fresh, same
	// as stopHosting's reset below — defensive here since a value-typed
	// m should already be zeroed on a first-ever [h], but guarantees a
	// stale timer from any path can't survive into this session's first
	// tick and fire an instant false "lost on reconnect" chip.
	m.resetTargetLockWatch()
	keyPath, err := DefaultHostKeyPath()
	if err != nil {
		m.app.Toast(fmt.Sprintf("can't host: %v", err))
		return m, nil
	}
	srv, err := New(Config{Addr: fmt.Sprintf(":%d", m.port), HostKeyPath: keyPath})
	if err != nil {
		m.app.Toast(fmt.Sprintf("can't host: %v", err))
		return m, nil
	}
	go func() {
		// A post-bind listener failure (rare) goes to stderr — this
		// goroutine must not touch the App, which the tea Update loop
		// owns. The common failure (port already in use) is caught
		// synchronously by New above and toasted on the Update goroutine.
		if err := srv.Serve(); err != nil {
			fmt.Fprintf(os.Stderr, "terminal-space-program: ssh listener: %v\n", err)
		}
	}()
	m.srv = srv
	m.owner = sessiondir.HostFingerprint
	m.rep = relay.NewReporter(srv.relay, m.owner)
	m.app.Toast(fmt.Sprintf("hosting on %s — invite guests with serve invite", srv.Addr()))
	// Populate the roster now so the Session screen flips to host-mode
	// immediately instead of on the next tick.
	m.refreshSession(time.Now())
	return m, nil
}

// stopHosting shuts the listener down and drops back to solo (v0.28
// S3). The confirm ("drops N guests — progress persists") is the
// screen's; here we execute. Shutdown runs in the background so the
// host's tick loop isn't blocked — guests' final payloads still
// unwind through persistMiddleware. Idempotent.
func (m reportingModel) stopHosting() (tea.Model, tea.Cmd) {
	if m.srv == nil {
		return m, nil
	}
	srv := m.srv
	go srv.drainAndClose()
	m.srv, m.rep, m.owner = nil, nil, ""
	// Back to solo: clear the slates the wrapper had been feeding so the
	// Session screen shows the [h]-start dead-end again.
	w := m.app.World()
	w.Session, w.Ghosts, w.SessionEvents, w.ChatLines = nil, nil, nil, nil
	// Clear the multiplayer coupling slates too (v0.28 finding 2): the tick
	// path that recomputes co-warp / docked-as-guest is gated on m.srv != nil,
	// so once hosting stops it never runs again. A stale w.CoWarp.MinWarp would
	// throttle solo warp forever, and a stale w.DockGuest would keep a bogus
	// docked-as-guest status. Also drop the per-owner hysteresis memory.
	w.CoWarp = sim.CoWarpState{}
	w.DockGuest = nil
	// Rendezvous Warp state is driven by the same gated tick path — clear
	// it too, or a stale arm/coast/invite would outlive the session
	// (v0.29 S2, same reasoning as the CoWarp clear above).
	w.DisengageRendezvousWarp()
	w.RendezvousInvite = nil
	w.RendezvousDegraded, w.RendezvousApproachM = false, 0
	w.RendezvousHold = false
	w.RendezvousPartnerAway = false
	w.RendezvousWait = sim.RendezvousWait{}
	// ADR 0037 §2's seat/rate slate rides the same gated tick path — a
	// stale copilot ceiling would clamp solo warp forever, exactly as a
	// stale CoWarp.MinWarp would.
	w.RendezvousRate = sim.RendezvousRateState{}
	// Arrival slates too (v0.29 review): a coast or sync arriving on the
	// same tick hosting stops must not fire a spurious chip in the next
	// hosting session.
	w.LastRendezvousArrival = nil
	w.LastRendezvousWaypoint = nil
	w.LastSyncArrival = nil
	m.rzPartnerOwner, m.rzPartnerHandle = "", ""
	m.rzInviteFrom, m.rzInviteHandle = "", ""
	m.rzDegraded = false
	m.coWarp = nil
	m.meta, m.metaAt = sessiondir.Meta{}, time.Time{}
	m.localEvents = nil
	// #294 review finding 3: stopHosting reset every other reporting
	// field above but not the target-lock watchdog's — the model value
	// is reused by the next startHosting, so a timer left running from
	// this session (an hours-old targetLockPendingSince, or a stale
	// targetLockOwner/CraftID) would fire an instant false "lost on
	// reconnect" chip on the very first tick of the NEXT hosting
	// session, for a target that session never even had.
	m.resetTargetLockWatch()
	m.app.Toast("hosting stopped")
	return m, nil
}

// restartServer drains every connected player and exits with the
// supervisor marker so the service manager relaunches the process
// (v0.30 S4). Authorization is enforced here — an admin (or the host)
// only; a guest's forged intent is refused with a toast. It persists the
// restarter's own world synchronously (the one payload the drain can't
// write), then drains in the background so the triggering session's tick
// loop isn't blocked; the drain announces the restart, pauses briefly so
// connected screens render the warning, then closes the listener
// (persisting every guest's final payload via persistMiddleware) before
// os.Exit(42).
func (m reportingModel) restartServer() (tea.Model, tea.Cmd) {
	if m.srv == nil {
		return m, nil
	}
	if !m.srv.store.MayAdminister(m.owner) {
		m.app.Toast("only an admin can restart the server")
		return m, nil
	}
	srv, actor := m.srv, m.owner
	// Persist the restarter's OWN world first (v0.30 S7 review). The
	// drain writes every other player's payload through
	// persistMiddleware, but this App never unwinds that way: the host
	// has no ssh session at all, and an admin guest's session is the one
	// session that can't finish while it is the one doing the draining.
	// os.Exit skips the quit path's autosave, so without this the player
	// who pressed [u] loses everything since their last periodic save —
	// all of it, on a box with the autosave interval set to off.
	// Synchronous, on the update goroutine, because it reads the live
	// world; the drain goroutine below must not touch it.
	m.app.PersistNow()
	// Announce now so the next tick carries the warning to every screen.
	srv.presence.event(sim.SessionEventServerRestart, actor, "", "")
	m.app.Toast("restarting server — draining sessions, progress saved")
	go func() {
		time.Sleep(restartAnnounceGrace)
		srv.drainAndClose()
		// persistMiddleware (folded into the drain above) only ever writes a
		// session's OWN craft payload — it knows nothing about the
		// cross-player dock ledger. Same backstop as the SIGTERM path
		// (restart_signal.go): this is the last stop before the process
		// exits, so flush the ledger's current state once more here
		// regardless of whether whatever changed it also flushed itself.
		// The admin restart is a real, reachable, player-triggered exit
		// (CONTEXT.md's Admin capability) — not swallowing the error: there
		// is nothing left to recover once the process is gone, so a silent
		// failure here is data loss that's only discovered on next boot,
		// exactly the #311 shape this ledger exists to close.
		if err := srv.persistDocks(); err != nil {
			log.Printf("admin restart: dock ledger did not flush before exit: %v", err)
		}
		exitFunc(restartExitCode)
	}()
	return m, nil
}

// drainAndClose gracefully stops the listener and waits for every
// in-flight session to unwind, which writes each guest's final payload
// through persistMiddleware (v0.28 S3). Shared by the host's
// stop-hosting toggle and the admin restart (v0.30 S4) so the drain
// lives in one place. It does not exit the process.
func (s *Server) drainAndClose() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = s.Shutdown(ctx)
	cancel()
	s.Wait(5 * time.Second)
}

// Relay exposes the session store (tests and later slices read it).
func (s *Server) Relay() *relay.Store { return s.relay }
