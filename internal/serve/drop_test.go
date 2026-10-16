package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// ADR 0054 slice D. The drop ends a live session from outside it; these
// tests drive it the way the host does (a SessionAdminMsg through the
// host's reportingModel) and read the far end: the guest's ssh session,
// its stderr, its payload file and the roster.

// dropClock stands in for time.After so the drop's waits are driven, not
// slept through. fire releases every wait that asked for d.
type dropClock struct {
	mu    sync.Mutex
	waits []time.Duration
	chans []chan time.Time
}

func (c *dropClock) after(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.waits = append(c.waits, d)
	c.chans = append(c.chans, ch)
	return ch
}

func (c *dropClock) fire(d time.Duration) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for i, w := range c.waits {
		if w == d && c.chans[i] != nil {
			c.chans[i] <- time.Now()
			c.chans[i] = nil
			n++
		}
	}
	return n
}

func (c *dropClock) requested() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// dropFixture is a hosted server with one enrolled guest connected over
// real ssh and the host's own wrapped model.
type dropFixture struct {
	srv   *Server
	dir   string
	host  tea.Model
	app   *tui.App
	guest *gameSession
	gfp   string
	done  chan error
}

func startDropFixture(t *testing.T) *dropFixture {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	srv, err := New(Config{Addr: "127.0.0.1:0", HostKeyPath: filepath.Join(t.TempDir(), "k"), SessionDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-done
		srv.Wait(5 * time.Second)
	})
	signer, gfp := newClientKey(t)
	enrollDirect(t, srv, gfp, "pilot2")
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	f := &dropFixture{srv: srv, dir: dir, host: srv.HostModel(app), app: app, gfp: gfp, done: done}
	f.guest = dialGameSession(t, srv.Addr(), signer)
	f.guest.waitFor(t, "Sol")
	return f
}

func (f *dropFixture) payloadFile() string {
	sum := sha256.Sum256([]byte(f.gfp))
	return filepath.Join(f.dir, "players", hex.EncodeToString(sum[:])+".json")
}

func (f *dropFixture) hostHandle(t *testing.T) string {
	t.Helper()
	p, err := f.srv.store.FindPlayer(sessiondir.HostFingerprint)
	if err != nil {
		t.Fatalf("host not on the roster: %v", err)
	}
	return p.Handle
}

// admin sends the host's admin intent through the real Update path.
func (f *dropFixture) admin(cmd screens.SessionCommand) {
	f.host, _ = f.host.Update(screens.SessionAdminMsg{Cmd: cmd})
}

// waitGone fails unless the guest's ssh session ends within limit.
func (f *dropFixture) waitGone(t *testing.T, limit time.Duration, what string) time.Duration {
	t.Helper()
	t0 := time.Now()
	select {
	case <-f.guest.exited:
		return time.Since(t0)
	case <-time.After(limit):
		t.Fatalf("%s: the guest's session was still alive after %v", what, limit)
		return 0
	}
}

func eventsNamed(srv *Server, viewer string, kind sim.SessionEventKind) []sim.SessionEvent {
	var out []sim.SessionEvent
	for _, e := range srv.presence.eventsFor(viewer) {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// d: the guest's session ends within a second, their terminal is left with
// the disconnect line, their world is saved, they read offline, and the
// host's chip says who did it. The key stays valid: they are still on the
// roster.
func TestDisconnectEndsTheGuestSessionCleanlyOverSSH(t *testing.T) {
	f := startDropFixture(t)
	_ = os.Remove(f.payloadFile()) // so a file at the end was written by the teardown
	hostHandle := f.hostHandle(t)

	f.admin(screens.SessionCommand{Kind: screens.SessionCmdDisconnect, Fingerprint: f.gfp, Handle: "pilot2"})
	f.waitGone(t, time.Second, "disconnect")

	want := "terminal-space-program: disconnected by " + hostHandle + " — reconnect any time, your progress is saved"
	if got := f.guest.stderr(); !strings.Contains(got, want) {
		t.Errorf("guest stderr = %q, want it to contain %q", got, want)
	}
	if !f.srv.store.HasPayload(f.gfp) {
		t.Error("the guest's payload was not written on disconnect")
	}
	if f.srv.presence.isOnline(f.gfp) {
		t.Error("the guest still reads online")
	}
	if _, err := f.srv.store.FindPlayer(f.gfp); err != nil {
		t.Errorf("a disconnect must keep the roster row: %v", err)
	}
	ev := eventsNamed(f.srv, sessiondir.HostFingerprint, sim.SessionEventDisconnectedBy)
	if len(ev) != 1 || ev[0].Handle != "pilot2" || ev[0].Detail != hostHandle {
		t.Errorf("host chip events = %+v, want one DisconnectedBy pilot2 by %s", ev, hostHandle)
	}
}

// x: the same, but the row is gone. The save must still land, with the
// burn that was in progress, and the chip must still name the player.
func TestKickKeepsTheFinalSaveAndTheBurnOverSSH(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	srv, err := New(Config{Addr: "127.0.0.1:0", HostKeyPath: filepath.Join(t.TempDir(), "k"), SessionDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-done
		srv.Wait(5 * time.Second)
	})
	signer, gfp := newClientKey(t)
	enrollDirect(t, srv, gfp, "pilot2")

	// A guest mid-burn: seed the payload with a long burn in progress.
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.ActiveBurn = &sim.ActiveBurn{
		Mode: spacecraft.BurnPrograde, DVRemaining: 5000, Throttle: 1,
		EndTime: w.Clock.SimTime.Add(10 * time.Hour), PrimaryID: c.Primary.ID,
	}
	if err := srv.store.SavePlayer(gfp, w); err != nil {
		t.Fatalf("seed payload: %v", err)
	}
	app, _ := tui.New(nil)
	f := &dropFixture{srv: srv, dir: dir, host: srv.HostModel(app), gfp: gfp, done: done}
	f.guest = dialGameSession(t, srv.Addr(), signer)
	f.guest.waitFor(t, "Sol")
	hostHandle := f.hostHandle(t)
	if err := os.Remove(f.payloadFile()); err != nil {
		t.Fatalf("remove seeded payload: %v", err)
	}

	f.admin(screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: gfp, Handle: "pilot2"})
	f.waitGone(t, time.Second, "kick")

	want := "terminal-space-program: kicked from this session by " + hostHandle + " — ask them for a new invite; your vessels are saved"
	if got := f.guest.stderr(); !strings.Contains(got, want) {
		t.Errorf("guest stderr = %q, want it to contain %q", got, want)
	}
	if _, err := srv.store.FindPlayer(gfp); err == nil {
		t.Error("the kicked player is still on the roster")
	}
	if !srv.store.HasPayload(gfp) {
		t.Fatal("a kick lost the final save: FindPlayer failing must not gate the write")
	}
	got, err := srv.store.LoadPlayer(gfp)
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if got.ActiveCraft().ActiveBurn == nil {
		t.Error("the burn in progress was not saved with the kick")
	}
	ev := eventsNamed(srv, sessiondir.HostFingerprint, sim.SessionEventKickedBy)
	if len(ev) != 1 || ev[0].Handle != "pilot2" || ev[0].Detail != hostHandle {
		t.Errorf("host chip events = %+v, want one KickedBy pilot2 by %s (the row is gone, the chip must still name them)", ev, hostHandle)
	}
	if left := eventsNamed(srv, sessiondir.HostFingerprint, sim.SessionEventLeave); len(left) != 0 {
		t.Errorf("a kick also chipped as an ordinary leave: %+v", left)
	}
	_ = save.SchemaVersion
}

// The purge flag (call 3): a purging drop writes no final payload.
func TestPurgingDropWritesNoFinalPayload(t *testing.T) {
	f := startDropFixture(t)
	if err := os.Remove(f.payloadFile()); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if !f.srv.dropPlayer(f.gfp, &dropRecord{kind: dropKick, actor: "jason", target: "pilot2", purge: true}) {
		t.Fatal("the drop found no live session")
	}
	f.waitGone(t, time.Second, "purging drop")
	if f.srv.store.HasPayload(f.gfp) {
		t.Error("a purging drop wrote a final payload, which would resurrect the purged file")
	}
	want := "terminal-space-program: kicked from this session by jason — ask them for a new invite"
	if got := f.guest.stderr(); !strings.Contains(got, want) || strings.Contains(got, "your vessels are saved") {
		t.Errorf("guest stderr = %q, want the purge wording %q", got, want)
	}
}

// A peer that is not draining never lets the unwind finish. The drop gives
// it dropWait and then closes the connection under it.
func TestDroppedDeadPeerIsClosedAfterDropWait(t *testing.T) {
	srv := newOfflineServer(t)
	clk := &dropClock{}
	srv.after = clk.after
	stub := &stubConn{}
	done := make(chan struct{})
	ls := &liveSession{conn: newActivityConn(stub, time.Now), done: done}
	srv.live.add(fpA, ls)

	srv.dropSession(ls, &dropRecord{kind: dropDisconnect, actor: "jason", target: "vex"})

	stub.mu.Lock()
	closedEarly := stub.closed
	stub.mu.Unlock()
	if closedEarly {
		t.Fatal("the connection was closed before dropWait elapsed")
	}
	// Wait for the watchdog to have asked for its timer, then release it.
	deadline := time.Now().Add(2 * time.Second)
	for clk.fire(srv.dropWait) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the drop never waited dropWait; waits requested: %v", clk.requested())
		}
		time.Sleep(time.Millisecond)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		stub.mu.Lock()
		closed := stub.closed
		stub.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dropWait elapsed and the connection was never closed")
		}
		time.Sleep(time.Millisecond)
	}
	if srv.dropWait != 2*time.Second {
		t.Errorf("dropWait = %v, want 2s (call 4)", srv.dropWait)
	}
}

// A session that unwinds by itself is left alone: no close after dropWait.
func TestDroppedSessionThatUnwindsIsNotClosed(t *testing.T) {
	srv := newOfflineServer(t)
	clk := &dropClock{}
	srv.after = clk.after
	stub := &stubConn{}
	done := make(chan struct{})
	ls := &liveSession{conn: newActivityConn(stub, time.Now), done: done}
	srv.dropSession(ls, &dropRecord{kind: dropDisconnect, actor: "jason"})
	close(done)
	time.Sleep(20 * time.Millisecond)
	clk.fire(srv.dropWait)
	time.Sleep(20 * time.Millisecond)
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.closed {
		t.Error("a session that finished unwinding had its connection closed anyway")
	}
}

// small seams so the restart tests read as their subject
const sessionHostFP = sessiondir.HostFingerprint

var restartKind = sim.SessionEventServerRestart

func sessionRestartMsg() tea.Msg { return screens.SessionRestartMsg{} }

func eventsFor(srv *Server, viewer string, kind sim.SessionEventKind) []sim.SessionEvent {
	return eventsNamed(srv, viewer, kind)
}

// A kicked player's roster row is gone the moment the kick lands, but their
// ghost keeps its name: the report carries the handle its reporter last knew.
func TestKickedPlayersGhostKeepsItsName(t *testing.T) {
	f := startDropFixture(t)
	// The guest has reported with its handle once its first tick has run.
	deadline := time.Now().Add(5 * time.Second)
	for {
		reps := f.srv.relay.Snapshot("")
		if len(reps) == 1 && reps[0].Handle == "pilot2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the guest's report never carried its handle: %+v", reps)
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.admin(screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: f.gfp, Handle: "pilot2"})
	f.waitGone(t, time.Second, "kick")
	if _, err := f.srv.store.FindPlayer(f.gfp); err == nil {
		t.Fatal("setup: the roster row survived the kick")
	}
	// The host's next refresh rebuilds the ghost slate against a roster
	// with no row for the guest.
	f.host, _ = f.host.Update(sim.TickMsg{})
	var named []string
	for _, g := range f.app.World().Ghosts {
		named = append(named, g.Handle)
	}
	if len(named) == 0 {
		t.Fatal("the kicked player's ghost is not on the host's map at all")
	}
	for _, h := range named {
		if h != "pilot2" {
			t.Errorf("ghost handle = %q, want pilot2", h)
		}
	}
}
