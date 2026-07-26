package serve

import (
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	cssh "golang.org/x/crypto/ssh"
)

// shellClient is a connected test client running the game, accumulating
// whatever the server sent it (when draining).
type shellClient struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (c *shellClient) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *shellClient) drain(r io.Reader) {
	b := make([]byte, 4096)
	for {
		n, err := r.Read(b)
		if n > 0 {
			c.mu.Lock()
			c.buf.Write(b[:n])
			c.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// dialShell connects signer's key and starts the game. A draining client
// is a player at the keyboard; a non-draining one is a laptop with the
// lid shut — the server's frames back up behind it exactly as they would
// against a sleeping peer.
func dialShell(t *testing.T, srv *Server, signer cssh.Signer, drain bool) *shellClient {
	t.Helper()
	client, err := cssh.Dial("tcp", srv.Addr(), &cssh.ClientConfig{
		User:            "guest",
		Auth:            []cssh.AuthMethod{cssh.PublicKeys(signer)},
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := sess.RequestPty("xterm-256color", 30, 120, cssh.TerminalModes{}); err != nil {
		t.Fatalf("pty: %v", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}
	sc := &shellClient{}
	if drain {
		go sc.drain(stdout)
		go sc.drain(stderr)
	}
	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	return sc
}

// registerWithDone is register() plus the payload-write signal the
// reclaim path waits on.
func registerWithDone(t *testing.T, srv *Server, fp string, lastIO time.Time) (*stubConn, chan struct{}) {
	t.Helper()
	stub := &stubConn{}
	done := make(chan struct{})
	srv.live.add(fp, &liveSession{
		conn: newActivityConn(stub, func() time.Time { return lastIO }),
		done: done,
	})
	return stub, done
}

// The invariant the whole reclaim path is built around: the displaced
// session's payload must be on disk before the reclaiming one loads it.
// Otherwise the new session reads a world the old one is still about to
// overwrite, and the coast the player reconnected to watch is lost.
func TestDisplaceWaitsForThePayloadWrite(t *testing.T) {
	srv := newOfflineServer(t)
	stub, done := registerWithDone(t, srv, fpA, time.Now().Add(-30*time.Minute))

	returned := make(chan reclaimResult, 1)
	go func() { returned <- srv.displaceAbsent(fpA) }()

	// It must have torn the old connection down...
	deadline := time.Now().Add(2 * time.Second)
	for !stub.isClosed() {
		if time.Now().After(deadline) {
			t.Fatal("the displaced session's connection was never closed — it keeps running, " +
				"and the reclaiming player is still locked out")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// ...but not yet handed the world over.
	select {
	case <-returned:
		t.Fatal("displaceAbsent returned before the displaced session's payload was written — " +
			"the reclaiming session will load a world the old one is about to overwrite")
	case <-time.After(150 * time.Millisecond):
	}

	close(done) // the payload has landed
	select {
	case ok := <-returned:
		if ok != reclaimTookOver {
			t.Error("displaceAbsent did not take over after a clean handover")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("displaceAbsent never returned after the payload landed")
	}
	if _, still := srv.live.snapshot()[fpA]; still {
		t.Error("the displaced session is still in the registry — the sweeper would keep reprieving it")
	}
}

// A session whose peer is still talking is somebody at the controls.
// Reprieve or not, it is never displaced: the connection stays up and
// the second one is refused, exactly as before ADR 0036.
func TestDisplaceRefusesAnAttendedSession(t *testing.T) {
	srv := newOfflineServer(t)
	stub, _ := registerWithDone(t, srv, fpA, time.Now().Add(-time.Second))

	if srv.displaceAbsent(fpA) != reclaimRefused {
		t.Error("displaced a session whose peer spoke a second ago — that is a player at the controls")
	}
	if stub.isClosed() {
		t.Error("an attended session's connection was closed")
	}
	if _, still := srv.live.snapshot()[fpA]; !still {
		t.Error("an attended session was dropped from the registry")
	}
}

// Presence says online but nothing is registered: the absence cannot be
// verified, so the connection is refused rather than admitted on faith.
// The host's in-process game is exactly this — marked online, never a
// session — and letting a key in there would fork its world.
func TestDisplaceRefusesWhenNothingIsRegistered(t *testing.T) {
	srv := newOfflineServer(t)
	if srv.displaceAbsent(fpA) != reclaimRefused {
		t.Error("displaceAbsent took over with no registered session — it displaced nothing and said it did")
	}
}

// If the payload never lands, the reclaiming connection must not load
// the world anyway. Being told to try again is recoverable; silently
// resuming a stale world is not.
func TestDisplaceGivesUpIfThePayloadNeverLands(t *testing.T) {
	srv := newOfflineServer(t)
	srv.reclaimWait = 100 * time.Millisecond
	registerWithDone(t, srv, fpA, time.Now().Add(-30*time.Minute))

	start := time.Now()
	if got := srv.displaceAbsent(fpA); got != reclaimStalled {
		t.Errorf("displaceAbsent = %v, want reclaimStalled — the payload never landed", got)
	}
	if elapsed := time.Since(start); elapsed < srv.reclaimWait {
		t.Errorf("gave up after %v, before the %v it should wait", elapsed, srv.reclaimWait)
	}
}

// End to end, and the reason reclaim exists at all: without it the
// Reprieve reintroduces the exact lockout #246 was written to fix, and
// worst for the players most likely to want back in — the ones mid-coast,
// whose lockout would last as long as the encounter they committed to.
func TestReconnectDisplacesItsOwnReprievedSession(t *testing.T) {
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
	t.Cleanup(func() { stopServer(t, srv, done) })

	signer, fp := newClientKey(t)
	enrollDirect(t, srv, fp, "vex")

	// The laptop that went to sleep mid-coast.
	asleep := dialShell(t, srv, signer, false)
	waitPresence(t, srv, fp, true, 10*time.Second, "the session never came online")
	first, ok := srv.live.snapshot()[fp]
	if !ok {
		t.Fatal("the live session is not in the registry")
	}

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
	defer func() { close(stopArming); <-arming }()

	// Confirm it really is reprieved — still online well past the idle
	// timeout, so the reconnect below is displacing a live session rather
	// than walking into a slot that already emptied.
	time.Sleep(3 * 400 * time.Millisecond)
	if !srv.presence.isOnline(fp) {
		t.Fatal("the session was reaped before the reconnect — nothing left to displace")
	}

	// The player opens the lid somewhere else and reconnects.
	back := dialShell(t, srv, signer, true)
	deadline := time.Now().Add(15 * time.Second)
	for {
		if cur, ok := srv.live.snapshot()[fp]; ok && cur != first {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the reconnecting player never took over their own reprieved session — " +
				"locked out of their own program behind a socket nobody is using, which is " +
				"the lockout the idle timeout exists to prevent")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if msg := back.text(); strings.Contains(msg, "already has a live session") {
		t.Errorf("the reconnecting player was refused: %q", msg)
	}

	// And it reads as one player resuming, not one leaving and another
	// arriving (ADR 0036 S5). Mid-coast a leave chip reads as the
	// rendezvous dying, which is the opposite of what a reclaim preserves.
	if got := eventsOf(srv, sim.SessionEventLeave, fpB); len(got) != 0 {
		t.Errorf("a reclaim announced %d departures — mid-coast that reads as the coast dying", len(got))
	}
	if got := eventsOf(srv, sim.SessionEventJoin, fpB); len(got) != 1 {
		t.Errorf("a reclaim announced %d joins, want only the original connect's — an unpaired "+
			"join arrives as an arrival to a partner who never saw a departure", len(got))
	}
	_ = asleep
}

// The other side of the same guard: a second key-holder cannot boot a
// player who is sitting right there.
func TestReconnectRefusedWhileAttended(t *testing.T) {
	srv := startTestServer(t)
	signer, fp := newClientKey(t)
	enrollDirect(t, srv, fp, "vex")

	attended := dialShell(t, srv, signer, true) // drains stdout: present
	waitPresence(t, srv, fp, true, 10*time.Second, "the session never came online")
	first, ok := srv.live.snapshot()[fp]
	if !ok {
		t.Fatal("the live session is not in the registry")
	}

	second := dialShell(t, srv, signer, true)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(second.text(), "already has a live session") {
		if time.Now().After(deadline) {
			t.Fatalf("a second connection was not refused while the first is attended; it saw %q",
				second.text())
		}
		time.Sleep(25 * time.Millisecond)
	}
	if cur, ok := srv.live.snapshot()[fp]; !ok || cur != first {
		t.Error("the attended session was displaced by the second connection")
	}
	_ = attended
}

// Review finding 2. Reclaim reads the registry, tears a session down and
// then waits up to reclaimWait for its payload. Without a per-key lock
// two connections on the same key both pass that test and are both
// admitted — two live sessions loading the same payload into divergent
// Worlds, which is exactly what the one-session-per-key guard exists to
// prevent. The wait makes the window orders of magnitude wider than the
// instantaneous check it replaced.
func TestReclaimIsSerialisedPerKey(t *testing.T) {
	srv := newOfflineServer(t)
	srv.reclaimWait = 2 * time.Second
	_, done := registerWithDone(t, srv, fpA, time.Now().Add(-30*time.Minute))

	results := make(chan reclaimResult, 2)
	for range 2 {
		go func() {
			defer srv.admit.enter(fpA)()
			results <- srv.displaceAbsent(fpA)
		}()
	}
	// The first one through closes the handover; the second must find the
	// slot already gone rather than displacing it a second time.
	time.Sleep(100 * time.Millisecond)
	close(done)

	var tookOver int
	for range 2 {
		select {
		case r := <-results:
			if r == reclaimTookOver {
				tookOver++
			}
		case <-time.After(10 * time.Second):
			t.Fatal("a reclaim never returned")
		}
	}
	if tookOver != 1 {
		t.Errorf("%d connections took the slot over, want exactly 1 — two live sessions would load "+
			"the same payload into divergent Worlds", tookOver)
	}
}

// Review finding 3. A reclaim that gives up has already closed the old
// session and cannot un-close it. Leaving it flagged as displaced means
// its teardown announces nothing and keeps its banked interval, so the
// player is disconnected in silence and their next connect replays an
// interval the saved payload already reflects.
func TestStalledReclaimReleasesTheOldSession(t *testing.T) {
	srv := newOfflineServer(t)
	srv.reclaimWait = 50 * time.Millisecond
	registerWithDone(t, srv, fpA, time.Now().Add(-30*time.Minute))
	ls, _ := srv.live.get(fpA)

	if got := srv.displaceAbsent(fpA); got != reclaimStalled {
		t.Fatalf("displaceAbsent = %v, want reclaimStalled", got)
	}
	if ls.displaced.Load() {
		t.Error("the old session is still flagged as handing over to a connection that gave up — " +
			"its departure goes unannounced and its bank never drops")
	}
}
