package serve

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// newOfflineServer builds a Server with temp state but never starts
// the listener — the S4 semantics are headless.
func newOfflineServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		HostKeyPath: filepath.Join(t.TempDir(), "hostkey"),
		SessionDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.ln.Close() })
	return srv
}

// A new player joins at the frontier: the max subspace time across
// live reports and persisted payloads, never their past.
func TestJoinAtFrontier(t *testing.T) {
	srv := newOfflineServer(t)

	// A live session 10 days ahead holds the frontier.
	wAhead, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	ahead := wAhead.Clock.SimTime.Add(10 * 24 * time.Hour)
	wAhead.Clock.SimTime = ahead
	relay.NewReporter(srv.relay, "SHA256:veteran").Tick(wAhead, time.Now())

	app, err := srv.newGuestApp("SHA256:rookie")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	if got := app.World().Clock.SimTime; !got.Equal(ahead) {
		t.Errorf("new player joined at %v, want frontier %v", got, ahead)
	}
}

// Persisted payloads hold the frontier even when nobody is online —
// an offline veteran's stored time still floors a new join.
func TestJoinAtFrontierFromStoredPayload(t *testing.T) {
	srv := newOfflineServer(t)

	wVet, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	ahead := wVet.Clock.SimTime.Add(30 * 24 * time.Hour)
	wVet.Clock.SimTime = ahead
	if err := srv.store.SavePlayer("SHA256:offline-vet", wVet); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	app, err := srv.newGuestApp("SHA256:rookie")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	if got := app.World().Clock.SimTime; !got.Equal(ahead) {
		t.Errorf("new player joined at %v, want stored frontier %v", got, ahead)
	}
}

// A reconnect resumes the player's OWN stored time — behind the
// frontier is fine; only fresh joins snap forward.
func TestReconnectKeepsOwnTime(t *testing.T) {
	srv := newOfflineServer(t)

	wOwn, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	own := wOwn.Clock.SimTime.Add(2 * 24 * time.Hour)
	wOwn.Clock.SimTime = own
	if err := srv.store.SavePlayer("SHA256:me", wOwn); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	// Someone else is far ahead, both live and stored.
	wAhead, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	wAhead.Clock.SimTime = wAhead.Clock.SimTime.Add(20 * 24 * time.Hour)
	relay.NewReporter(srv.relay, "SHA256:veteran").Tick(wAhead, time.Now())

	app, err := srv.newGuestApp("SHA256:me")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	if got := app.World().Clock.SimTime; !got.Equal(own) {
		t.Errorf("reconnect resumed at %v, want own stored %v", got, own)
	}
}

// The host's wrapped model reports into the store on ticks under the
// host fingerprint — the host is a first-class session on the wire.
func TestHostModelReports(t *testing.T) {
	srv := newOfflineServer(t)
	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = srv.HostModel(hostApp)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	m, _ = m.Update(sim.TickMsg(time.Now()))

	seen := srv.relay.Snapshot("SHA256:some-guest")
	if len(seen) != 1 || seen[0].Owner != sessiondir.HostFingerprint {
		t.Fatalf("store after host tick = %+v, want one report owned by %q", seen, sessiondir.HostFingerprint)
	}
	if len(seen[0].Crafts) == 0 {
		t.Error("host report carries no craft")
	}
	// The wrapper is transparent: the frame is still the game.
	if out := stripANSI(m.View()); !strings.Contains(out, "warp 1x") {
		t.Errorf("wrapped host model broke rendering:\n%s", firstLines(out, 3))
	}
}
