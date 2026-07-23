package serve

import (
	"context"
	"fmt"
	"os"
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
}

// localEventTTL matches the chip's on-screen life; pruning here just
// keeps the slice from growing over a long session.
const localEventTTL = 10 * time.Second

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
func (s *Server) withReporting(app *tui.App, owner string) tea.Model {
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
				m.srv.dock.RequestUndock(m.owner, w.DockGuest.GuestCraftID)
			}
		}
		return m, nil
	}
	if _, ok := msg.(tui.TransferControlMsg); ok {
		if m.srv != nil {
			m.srv.dock.RequestTransfer(m.owner)
		}
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
			_, _ = m.srv.store.MintInvite(admin.Cmd.Handle)
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
// new arm toward the viewer, arrival at τ (consuming the sim's
// LastRendezvousArrival, mirroring the Sync arrival), a cancel/retract
// releasing an arm before τ, and the hold-τ degrade flag going up. All
// local-only chips — each side derives its own from its own World.
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
	// (initiator cancelled, or τ passed) chips as cancelled.
	switch inv := w.RendezvousInvite; {
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

	// Co-warp (v0.28 S1, ADR 0034 §5): couple the viewer's active craft
	// to any nearby same-subspace player and write the min-over-Effective
	// clamp onto the World for next tick's clampedWarp; emit couple/
	// release chips on transitions. Same seam as ghosts — reads the
	// store's reports (which now carry EffWarp), writes transient state.
	peers := relay.CoWarpPeersFrom(w, others, handles, m.owner)
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
	for _, p := range peers {
		if p.ArmedTowardViewer {
			armedTowardViewer[p.Owner] = true
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

			WantsRendezvous: armedTowardViewer[p.Fingerprint],
			RendezvousOut:   w.RendezvousArm != nil && w.RendezvousArm.TargetOwner == p.Fingerprint,
		}
		if rep, ok := reports[p.Fingerprint]; ok {
			row.HasReport = true
			row.DeltaT = rep.SubspaceTime.Sub(w.Clock.SimTime)
			row.CraftCount = len(rep.Crafts)
			if len(rep.Crafts) > 0 {
				row.System = rep.Crafts[0].System
				row.Primary = rep.Crafts[0].Primary
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
	kept := m.localEvents[:0]
	for _, e := range m.localEvents {
		if now.Sub(e.At) <= localEventTTL {
			kept = append(kept, e)
		}
	}
	m.localEvents = kept
	w.SessionEvents = append(events, m.localEvents...)
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
	w.Session, w.Ghosts, w.SessionEvents = nil, nil, nil
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
	// Arrival slates too (v0.29 review): a coast or sync arriving on the
	// same tick hosting stops must not fire a spurious chip in the next
	// hosting session.
	w.LastRendezvousArrival = nil
	w.LastSyncArrival = nil
	m.rzPartnerOwner, m.rzPartnerHandle = "", ""
	m.rzInviteFrom, m.rzInviteHandle = "", ""
	m.rzDegraded = false
	m.coWarp = nil
	m.meta, m.metaAt = sessiondir.Meta{}, time.Time{}
	m.localEvents = nil
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
