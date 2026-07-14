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
	// arrival chip) — appended to the world's event slate alongside
	// the broadcast ring, pruned by the same wall TTL.
	localEvents []sim.SessionEvent
}

// localEventTTL matches the chip's on-screen life; pruning here just
// keeps the slice from growing over a long session.
const localEventTTL = 10 * time.Second

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

	// Session-admin intents from the Session screen (v0.27 S6): the
	// wrapper owns the store access; the App below only dispatched.
	// Inert until a server exists (solo has nothing to administer).
	if admin, ok := msg.(screens.SessionAdminMsg); ok && m.srv != nil {
		switch admin.Cmd.Kind {
		case screens.SessionCmdMint:
			_, _ = m.srv.store.MintInvite(admin.Cmd.Handle)
		case screens.SessionCmdRevoke:
			_ = m.srv.store.RevokeInvite(admin.Cmd.Code)
		case screens.SessionCmdRemove:
			_ = m.srv.store.RemovePlayer(admin.Cmd.Fingerprint)
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
	w.Ghosts = relay.GhostsFor(w, m.srv.relay.Snapshot(m.owner), handles)

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
	info := &sim.SessionInfo{
		IsHost: m.owner == sessiondir.HostFingerprint,
		Self:   m.owner,
	}
	for _, p := range m.meta.Roster {
		row := sim.SessionPlayer{
			Fingerprint: p.Fingerprint,
			Handle:      p.Handle,
			Role:        p.Role,
			Online:      m.srv.presence.isOnline(p.Fingerprint),
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
	if info.IsHost {
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
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
		srv.Wait(5 * time.Second)
	}()
	m.srv, m.rep, m.owner = nil, nil, ""
	// Back to solo: clear the slates the wrapper had been feeding so the
	// Session screen shows the [h]-start dead-end again.
	w := m.app.World()
	w.Session, w.Ghosts, w.SessionEvents = nil, nil, nil
	m.meta, m.metaAt = sessiondir.Meta{}, time.Time{}
	m.localEvents = nil
	m.app.Toast("hosting stopped")
	return m, nil
}

// Relay exposes the session store (tests and later slices read it).
func (s *Server) Relay() *relay.Store { return s.relay }
