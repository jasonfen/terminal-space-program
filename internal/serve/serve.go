// Package serve hosts the embedded SSH front door for multiplayer
// sessions (ADR 0034, v0.27). `--serve` starts a wish listener next
// to the host's in-process game. S1 gave every connection a fresh
// ephemeral World; S3 adds identity and persistence: unknown keys go
// through the invite-code enroll flow, enrolled keys resume their
// per-player world from the session directory, and every guest's
// autosaves write back there — never into the host's local saves.
package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// DefaultPort is the SSH listener port when --serve-port isn't given.
const DefaultPort = 23234

// Config shapes a Server. Addr is a listen address ("[host]:port";
// use port 0 to let the OS pick — Addr() reports the bound address).
// HostKeyPath locates the server's ed25519 identity; a missing key is
// generated there on first start. SessionDir is the session store
// (roster, invites, per-player payloads); empty means the XDG default.
type Config struct {
	Addr        string
	HostKeyPath string
	SessionDir  string
}

// Server is a running (or startable) SSH listener whose sessions each
// run their own game. The listener is bound in New so port conflicts
// surface before the host's own TUI takes the screen.
type Server struct {
	ssh      *ssh.Server
	ln       net.Listener
	store    *sessiondir.Store
	relay    *relay.Store
	presence *presence

	// sessions tracks in-flight session handlers (including their
	// final persist) so shutdown can wait for payload writes instead
	// of racing process exit (review follow-up).
	sessions sync.WaitGroup
}

// DefaultHostKeyPath returns the per-host SSH identity path, sibling
// to the save state: $XDG_STATE_HOME/terminal-space-program/
// ssh_host_ed25519_key, falling back to ~/.local/state (same
// resolution as save.DefaultPath).
func DefaultHostKeyPath() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "ssh_host_ed25519_key"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "terminal-space-program", "ssh_host_ed25519_key"), nil
}

// New binds cfg.Addr, opens the session store (auto-enrolling the
// host as roster entry #1 on first serve), and prepares the wish
// server. The host key is created at cfg.HostKeyPath if absent.
// Serve must be called to start accepting sessions.
func New(cfg Config) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.HostKeyPath), 0o755); err != nil {
		return nil, fmt.Errorf("serve: host key dir: %w", err)
	}
	dir := cfg.SessionDir
	if dir == "" {
		var err error
		if dir, err = sessiondir.DefaultDir(); err != nil {
			return nil, fmt.Errorf("serve: %w", err)
		}
	}
	store, err := sessiondir.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("serve: %w", err)
	}
	if _, err := store.EnsureHost(hostHandle()); err != nil {
		return nil, fmt.Errorf("serve: enroll host: %w", err)
	}
	srv := &Server{store: store, relay: relay.NewStore(), presence: newPresence()}
	// The host plays in-process and is online for the session's whole
	// life — no join chip for them (serve start isn't a "moment").
	srv.presence.markOnline(sessiondir.HostFingerprint)
	s, err := wish.NewServer(
		wish.WithAddress(cfg.Addr),
		wish.WithHostKeyPath(cfg.HostKeyPath),
		// Any key may connect — identity is resolved in-session: known
		// fingerprints resume, unknown ones face the invite-code flow.
		wish.WithPublicKeyAuth(func(ssh.Context, ssh.PublicKey) bool { return true }),
		wish.WithMiddleware(
			bm.Middleware(srv.sessionHandler),
			srv.persistMiddleware,   // runs after the game ends — final payload write
			activeterm.Middleware(), // require a PTY before handing off to bubbletea
		),
	)
	if err != nil {
		return nil, fmt.Errorf("serve: %w", err)
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("serve: listen %s: %w", cfg.Addr, err)
	}
	srv.ssh, srv.ln = s, ln
	return srv, nil
}

// hostHandle names the host's roster entry: the OS username, or
// "host" when that's unresolvable.
func hostHandle() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "host"
}

// Addr reports the bound listen address (useful with ":0").
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Serve accepts sessions until Shutdown; it blocks. A graceful
// shutdown returns nil.
func (s *Server) Serve() error {
	err := s.ssh.Serve(s.ln)
	if err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown stops the listener and ends every live session.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.ssh.Shutdown(ctx)
}

// Wait blocks until every session handler (and its final payload
// persist) has finished, or the timeout passes. Call after Shutdown —
// force-closed connections still unwind through persistMiddleware,
// and the process must not exit under them.
func (s *Server) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		s.sessions.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// Context keys stashed on the ssh session so the persist middleware
// can reach the game state after the program ends.
type ctxKey int

const (
	ctxKeyApp ctxKey = iota
	ctxKeyFingerprint
	ctxKeyJoined // set once this session marked presence-online
)

// sessionHandler builds the per-connection model. Enrolled keys go
// straight to their game (restored from the session directory when a
// payload exists); unknown keys get the guest flow (card → code →
// handle). The bubbletea middleware wires the session's PTY as the
// program's input/output and translates window changes.
func (s *Server) sessionHandler(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseAllMotion()}
	key := sess.PublicKey()
	if key == nil {
		wish.Fatalln(sess, "terminal-space-program: public-key auth required")
		return nil, nil
	}
	fp := gossh.FingerprintSHA256(key)

	// One live session per key (review follow-up): a second connection
	// would load the same payload into a second divergent World and the
	// two would fight over the relay report and the payload file.
	if s.presence.isOnline(fp) {
		wish.Fatalln(sess, "terminal-space-program: this key already has a live session — disconnect it first")
		return nil, nil
	}

	app, err := s.newGuestApp(fp)
	if err != nil {
		if errors.Is(err, save.ErrCatalogMismatch) {
			wish.Fatalln(sess, "terminal-space-program: your saved program was created under a different body catalog than this build — ask your host to sort the session directory out")
			return nil, nil
		}
		wish.Fatalln(sess, "terminal-space-program:", err)
		return nil, nil
	}
	sess.Context().SetValue(ctxKeyApp, app)
	sess.Context().SetValue(ctxKeyFingerprint, fp)

	game := s.withReporting(app, fp) // v0.27 S4: sessions feed the store
	if p, err := s.store.FindPlayer(fp); err == nil {
		// Enrolled reconnect: no card, no code — resume. Presence +
		// join chip fire now; the middleware pairs the leave.
		s.presence.markOnline(fp)
		s.presence.event(sim.SessionEventJoin, fp, p.Handle, "")
		sess.Context().SetValue(ctxKeyJoined, true)
		return game, opts
	}
	// Unknown key → enroll flow. Presence starts when (if) the flow
	// commits the enrollment.
	ctx := sess.Context()
	onEnroll := func(handle string) {
		// Re-apply the frontier at the COMMIT (review follow-up): the
		// connect-time sample goes stale while the player sits in the
		// card/code/handle prompts and other subspaces advance. The
		// game's tick loop hasn't started yet, so the relabel is safe.
		if f, ok := s.frontier(); ok && f.After(app.World().Clock.SimTime) {
			app.World().Clock.SimTime = f
		}
		s.presence.markOnline(fp)
		s.presence.event(sim.SessionEventJoin, fp, handle, "")
		ctx.SetValue(ctxKeyJoined, true)
	}
	return newGuestFlow(s.store, fp, game, onEnroll), opts
}

// newGuestApp builds the game for fp: the persisted world when a
// payload exists (catalog-checked by the save machinery), a fresh
// default start otherwise. Either way autosaves are redirected into
// the per-player payload.
func (s *Server) newGuestApp(fp string) (*tui.App, error) {
	var app *tui.App
	w, err := s.store.LoadPlayer(fp)
	switch {
	case err == nil:
		if app, err = tui.NewWithWorld(w); err != nil {
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist):
		if app, err = tui.New(nil); err != nil {
			return nil, err
		}
		// Join at the frontier (v0.27 S4, ADR 0034): a NEW player's
		// clock starts at the max subspace time across live sessions
		// and persisted payloads — you can never start in someone's
		// past. Craft state vectors are time-local, so relabelling
		// "now" is safe. Reconnects (the branch above) keep their own
		// stored time instead.
		if f, ok := s.frontier(); ok && f.After(app.World().Clock.SimTime) {
			app.World().Clock.SimTime = f
		}
	default:
		return nil, err
	}
	app.SetGuestPersistence(func(w *sim.World) error {
		return s.store.SavePlayer(fp, w)
	})
	return app, nil
}

// frontier combines the live store's max subspace time with the
// persisted payloads' — offline players hold the frontier too.
func (s *Server) frontier() (time.Time, bool) {
	live, okLive := s.relay.Frontier()
	stored, okStored := s.store.LatestSimTime()
	switch {
	case okLive && okStored:
		if stored.After(live) {
			return stored, true
		}
		return live, true
	case okLive:
		return live, true
	case okStored:
		return stored, true
	}
	return time.Time{}, false
}

// persistMiddleware writes the final per-player payload after the
// session's program has fully stopped (bm's handler returns only
// then, so the read is race-free). Covers hard disconnects — the
// in-App quit path persists too, via the guest sink. Unenrolled
// visitors (flow abandoned) leave nothing behind.
func (s *Server) persistMiddleware(next ssh.Handler) ssh.Handler {
	return func(sess ssh.Session) {
		s.sessions.Add(1)
		defer s.sessions.Done()
		next(sess)
		fp, ok := sess.Context().Value(ctxKeyFingerprint).(string)
		if !ok {
			return
		}
		// Presence: pair the join marked at connect/enroll (v0.27 S6).
		if joined, _ := sess.Context().Value(ctxKeyJoined).(bool); joined {
			s.presence.markOffline(fp)
			if p, err := s.store.FindPlayer(fp); err == nil {
				s.presence.event(sim.SessionEventLeave, fp, p.Handle, "")
			}
		}
		app, ok := sess.Context().Value(ctxKeyApp).(*tui.App)
		if !ok {
			return
		}
		if _, err := s.store.FindPlayer(fp); err != nil {
			return // never enrolled — ephemeral visit
		}
		_ = s.store.SavePlayer(fp, app.World())
	}
}

// newSessionApp is the S1-era ephemeral factory, kept for the
// headless World-independence test.
func newSessionApp() (tea.Model, []tea.ProgramOption, error) {
	app, err := tui.New(nil)
	if err != nil {
		return nil, nil, err
	}
	return app, []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseAllMotion()}, nil
}
