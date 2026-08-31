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

// A new player joins at the earliest LIVE clock, not the frontier
// (ADR 0034 §7 amendment / ADR 0045 S3, closing #247/#396): with two
// players online at different subspace times, the far-ahead one must
// not pull a rookie forward. Sync is forward-only, so joining behind
// the group is recoverable and joining ahead of it is a permanent
// trap — the opposite of the old "you can never start in someone's
// past" rule this replaces.
func TestJoinAtEarliestLiveClock(t *testing.T) {
	srv := newOfflineServer(t)

	wBehind, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	behind := wBehind.Clock.SimTime.Add(6 * 24 * time.Hour)
	wBehind.Clock.SimTime = behind
	relay.NewReporter(srv.relay, "SHA256:closer").Tick(wBehind, time.Now())

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
	if got := app.World().Clock.SimTime; !got.Equal(behind) {
		t.Errorf("new player joined at %v, want earliest live %v", got, behind)
	}
}

// A stored (offline) payload far ahead of the live group must not
// pull a new joiner forward: while anyone is live, offline payloads
// are ignored entirely for the join point, however far ahead they
// are. This is the trap #247/#396 exists to close — a lid-open
// session that warped for days and persisted on disconnect must not
// keep dropping newcomers into a time nobody is actually at.
func TestJoinIgnoresStoredPayloadAhead(t *testing.T) {
	srv := newOfflineServer(t)

	wLive, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	live := wLive.Clock.SimTime.Add(6 * 24 * time.Hour)
	wLive.Clock.SimTime = live
	relay.NewReporter(srv.relay, "SHA256:present").Tick(wLive, time.Now())

	wOfflineVet, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	wOfflineVet.Clock.SimTime = wOfflineVet.Clock.SimTime.Add(30 * 24 * time.Hour)
	if err := srv.store.SavePlayer("SHA256:offline-vet", wOfflineVet); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	app, err := srv.newGuestApp("SHA256:rookie")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	if got := app.World().Clock.SimTime; !got.Equal(live) {
		t.Errorf("new player joined at %v, want the live player's time %v (stored payload must not pull forward)", got, live)
	}
}

// With nobody online, a new player joins alongside the
// furthest-BEHIND stored payload, not the furthest-ahead one — the
// minimum, mirroring joinTime's live-session rule onto the offline
// fallback so an unattended session that ran far ahead can't keep
// springing the ADR 0034 §7 trap after its owner has disconnected.
func TestJoinAtFurthestBehindStoredPayloadWhenEmpty(t *testing.T) {
	srv := newOfflineServer(t)

	wBehind, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	behind := wBehind.Clock.SimTime.Add(5 * 24 * time.Hour)
	wBehind.Clock.SimTime = behind
	if err := srv.store.SavePlayer("SHA256:behind-vet", wBehind); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	wAhead, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	wAhead.Clock.SimTime = wAhead.Clock.SimTime.Add(30 * 24 * time.Hour)
	if err := srv.store.SavePlayer("SHA256:ahead-vet", wAhead); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	app, err := srv.newGuestApp("SHA256:rookie")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	if got := app.World().Clock.SimTime; !got.Equal(behind) {
		t.Errorf("new player joined at %v, want furthest-behind stored payload %v", got, behind)
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
