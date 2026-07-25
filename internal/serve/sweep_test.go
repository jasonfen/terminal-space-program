package serve

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	cssh "golang.org/x/crypto/ssh"
)

// register puts a session in the registry with a stub connection whose
// peer last spoke at lastIO, and hands back the stub for inspection.
func register(t *testing.T, srv *Server, fp string, lastIO time.Time) *stubConn {
	t.Helper()
	stub := &stubConn{}
	conn := newActivityConn(stub, func() time.Time { return lastIO })
	srv.live.add(fp, &liveSession{conn: conn})
	return stub
}

// mutualArm reports both players as Engaged toward each other, which is
// what an in-flight shared coast looks like on the wire.
func mutualArm(a, b string, tau time.Time) []relay.CraftReport {
	return []relay.CraftReport{armed(a, b, tau), armed(b, a, tau)}
}

func publish(srv *Server, reports []relay.CraftReport) {
	for _, r := range reports {
		srv.relay.Report(r)
	}
}

// Only a Commitment earns a Reprieve. An uncommitted session keeps the
// deadline its own I/O path set and is reaped at the idle timeout like
// any other absent peer.
func TestExtendReprievesOnlyCommittedSessions(t *testing.T) {
	srv := newOfflineServer(t)
	now := time.Now()
	absent := now.Add(-30 * time.Minute) // both peers long gone

	committed := register(t, srv, fpA, absent)
	uncommitted := register(t, srv, fpC, absent)
	publish(srv, mutualArm(fpA, fpB, simNoon.Add(time.Hour)))

	srv.extendReprieves(now)

	if got := committed.deadlinesSet(); len(got) != 1 {
		t.Errorf("committed session got %d deadline extensions, want 1 — its partner is mid-coast", len(got))
	}
	if got := uncommitted.deadlinesSet(); len(got) != 0 {
		t.Errorf("uncommitted session got %d extensions, want 0 — nothing is waiting on it", len(got))
	}
}

// The sweeper may only ever push a deadline further out. It runs on a
// short window, so a naive "set now + window" would pull a *present*
// player's deadline in from the idle timeout to a couple of minutes —
// and a paused player renders no frames, so they would be disconnected
// while sitting right there.
func TestExtendReprieveNeverShortensADeadline(t *testing.T) {
	srv := newOfflineServer(t)
	now := time.Now()
	recent := now.Add(-time.Second) // spoke a moment ago: present, just quiet

	stub := register(t, srv, fpA, recent)
	publish(srv, mutualArm(fpA, fpB, simNoon.Add(time.Hour)))

	srv.extendReprieves(now)

	got := stub.deadlinesSet()
	if len(got) != 1 {
		t.Fatalf("got %d deadline extensions, want 1", len(got))
	}
	if want := recent.Add(srv.idleTimeout); !got[0].Equal(want) {
		t.Errorf("deadline = %v, want %v — the deadline this session's own I/O path would have set",
			got[0], want)
	}
	if got[0].Before(now.Add(srv.reprieveWindow)) {
		t.Errorf("deadline %v is inside the reprieve window ending %v — the sweeper shortened it",
			got[0], now.Add(srv.reprieveWindow))
	}
}

// The cap is what bounds unattended simulation. Past it the sweeper
// stops extending and the standing deadline fires — it never
// force-expires, so exactly one code path ever writes a deadline and
// there is no ordering race with teardown.
func TestExtendReprieveStopsAtTheCap(t *testing.T) {
	srv := newOfflineServer(t)
	now := time.Now()
	// Docked, with a peer absent for longer than a dock's flat cap.
	stub := register(t, srv, fpA, now.Add(-dockReprieveCap-time.Minute))
	srv.dock.Seed([]relay.DockRecord{{ID: 1, Owner: fpA, GuestOwner: fpB, Phase: relay.DockActive}})

	srv.extendReprieves(now)

	if got := stub.deadlinesSet(); len(got) != 0 {
		t.Errorf("a capped-out session was extended to %v — the cap is the only thing bounding "+
			"unattended simulation", got)
	}
}

// The sweeper must be running for the listener's life. This is the
// finding that inverts the obvious implementation: updateDeadline() runs
// at the *start* of Write, then Write blocks against the absent peer and
// nothing on the I/O path runs again, so whatever deadline was set last
// is final. A goroutine whose only job is to call SetDeadline is not
// redundant — without it every Reprieve silently caps at one window.
func TestSweeperRunsForTheListenersLife(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		HostKeyPath: filepath.Join(t.TempDir(), "ssh_host_ed25519_key"),
		SessionDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.sweepEvery = 20 * time.Millisecond
	now := time.Now()
	stub := register(t, srv, fpA, now.Add(-30*time.Minute))
	publish(srv, mutualArm(fpA, fpB, simNoon.Add(time.Hour)))

	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()

	deadline := time.Now().Add(5 * time.Second)
	for len(stub.deadlinesSet()) == 0 {
		if time.Now().After(deadline) {
			_ = srv.ln.Close()
			<-done
			t.Fatal("no sweep ever extended a committed session's deadline — nothing is holding " +
				"a Reprieve open, so every one of them expires at a single idle window")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// And it stops with the listener: a sweeper outliving Serve would keep
	// poking connections belonging to a server that is gone.
	_ = srv.ln.Close()
	<-done
	settled := len(stub.deadlinesSet())
	time.Sleep(100 * time.Millisecond) // several sweep intervals
	if got := len(stub.deadlinesSet()); got != settled {
		t.Errorf("deadline extensions went %d → %d after Serve returned — the sweeper outlived the listener",
			settled, got)
	}
}

// End to end, on the behaviour the whole ADR exists for: a co-op partner
// closes their laptop mid-coast, and their session must NOT be reaped at
// the idle timeout, because the coast they committed to is still running
// and the other player is waiting on it. When the Commitment ends, the
// same session goes away like any other absent peer.
func TestReprievedSessionOutlivesTheIdleTimeout(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		HostKeyPath: filepath.Join(t.TempDir(), "ssh_host_ed25519_key"),
		SessionDir:  t.TempDir(),
		IdleTimeout: 400 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.sweepEvery = 25 * time.Millisecond
	srv.reprieveWindow = 250 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() { _ = srv.ln.Close(); <-done })

	signer, fp := newClientKey(t)
	enrollDirect(t, srv, fp, "vex")
	client, err := cssh.Dial("tcp", srv.Addr(), &cssh.ClientConfig{
		User:            "guest",
		Auth:            []cssh.AuthMethod{cssh.PublicKeys(signer)},
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := sess.RequestPty("xterm-256color", 30, 120, cssh.TerminalModes{}); err != nil {
		t.Fatalf("pty: %v", err)
	}
	if _, err := sess.StdoutPipe(); err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	// From here the client never drains stdout and never types: a laptop
	// with the lid shut.
	waitPresence(t, srv, fp, true, 10*time.Second, "the session never came online")

	// Hold a live Commitment across the wire. The session's own reports
	// would otherwise overwrite this with its armless truth, so it is
	// re-asserted until the survival window is over — which is exactly
	// what a real partner mid-coast keeps reporting.
	stopArming := make(chan struct{})
	arming := make(chan struct{})
	go func() {
		defer close(arming)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stopArming:
				return
			case <-tick.C:
				publish(srv, mutualArm(fp, fpB, time.Now().UTC().Add(time.Hour)))
			}
		}
	}()

	// Survive several idle timeouts. Without a Reprieve this session is
	// gone inside the first one.
	survive := 6 * 400 * time.Millisecond
	deadline := time.Now().Add(survive)
	for time.Now().Before(deadline) {
		if !srv.presence.isOnline(fp) {
			close(stopArming)
			<-arming
			t.Fatal("a session mid-Commitment was reaped: its partner is left waiting out a " +
				"coast that will now never complete — the regression the Reprieve exists to prevent")
		}
		time.Sleep(25 * time.Millisecond)
	}

	// Commitment over. The Reprieve is released by ceasing to extend, so
	// the standing deadline carries it off within one window.
	close(stopArming)
	<-arming
	waitPresence(t, srv, fp, false, 30*time.Second,
		"an absent peer with no Commitment left was never reaped — the Reprieve became permanent")
}

// A session leaves the registry when it ends. Otherwise the registry
// only ever grows, and every sweep for the rest of the server's life
// pokes deadlines onto connections that are long closed.
func TestEndedSessionLeavesTheRegistry(t *testing.T) {
	srv := startTestServer(t)
	signer, fp := newClientKey(t)
	enrollDirect(t, srv, fp, "vex")

	client, err := cssh.Dial("tcp", srv.Addr(), &cssh.ClientConfig{
		User:            "guest",
		Auth:            []cssh.AuthMethod{cssh.PublicKeys(signer)},
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := sess.RequestPty("xterm-256color", 30, 120, cssh.TerminalModes{}); err != nil {
		t.Fatalf("pty: %v", err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	waitPresence(t, srv, fp, true, 10*time.Second, "the session never came online")
	if _, ok := srv.live.snapshot()[fp]; !ok {
		t.Fatal("a live session is missing from the registry — the sweeper cannot find its connection, " +
			"so it can never be reprieved")
	}

	_ = client.Close()
	waitPresence(t, srv, fp, false, 10*time.Second, "the session never went offline")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := srv.live.snapshot()[fp]; !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("an ended session is still in the registry — it grows without bound and every " +
				"sweep pokes deadlines onto closed connections")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// A survival window shorter than the reprieve window would pass even
// with a broken sweeper, since a single extension would cover it.
func TestSweepTestWindowsAreMeaningful(t *testing.T) {
	srv := newOfflineServer(t)
	if srv.reprieveWindow <= srv.sweepEvery {
		t.Errorf("reprieve window %v <= sweep interval %v — a Reprieve would lapse between sweeps",
			srv.reprieveWindow, srv.sweepEvery)
	}
	if srv.idleTimeout <= 0 {
		t.Error("idleTimeout unrecorded — the sweeper cannot reconstruct the deadline it must not shorten")
	}
}
