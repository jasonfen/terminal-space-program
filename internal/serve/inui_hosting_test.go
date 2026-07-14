package serve

import (
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// hostServer digs the live listener out of a wrapped model (value
// receiver — the srv rides the returned copy forward).
func hostServer(t *testing.T, m tea.Model) *Server {
	t.Helper()
	rm, ok := m.(reportingModel)
	if !ok {
		t.Fatalf("model is %T, not reportingModel", m)
	}
	return rm.HostServer()
}

// A solo wrapper (nil server) is a transparent pass-through: no store,
// no reports, no session slate — the App plays as if unwrapped.
func TestSoloWrapperInert(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = WrapHost(app, nil, 0)
	if s := hostServer(t, m); s != nil {
		t.Fatalf("solo wrapper already has a server: %v", s)
	}
	// Several ticks must not spin up any hosting state.
	for i := 0; i < 3; i++ {
		m = tick(m)
	}
	if s := hostServer(t, m); s != nil {
		t.Error("solo wrapper started a server on its own")
	}
	if info := app.World().Session; info != nil {
		t.Errorf("solo wrapper populated a session slate: %+v", info)
	}
	// The frame is still the game (wrapper transparent).
	if out := stripANSI(m.View()); !strings.Contains(out, "warp 1x") {
		t.Errorf("solo wrapper broke rendering:\n%s", firstLines(out, 3))
	}
}

// [h] start: the wrapper lazily binds a listener, the host becomes
// roster entry #1 (IsHost), and the listener accepts a connection.
func TestStartHostingViaMessage(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = WrapHost(app, nil, 0) // :0 — OS picks a free port
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	m, _ = m.Update(screens.SessionHostMsg{Start: true})

	srv := hostServer(t, m)
	if srv == nil {
		t.Fatal("[h] start left the wrapper without a server")
	}
	t.Cleanup(func() { _ = srv.ln.Close() })

	// Roster flips to host-mode.
	info := app.World().Session
	if info == nil || !info.IsHost {
		t.Fatalf("host slate after start = %+v, want IsHost", info)
	}
	// The listener is live: a raw TCP connect succeeds (a full ssh
	// handshake is the S1 smoke test's job — kept to exactly two).
	conn, err := net.DialTimeout("tcp", srv.Addr(), 3*time.Second)
	if err != nil {
		t.Fatalf("listener not accepting after [h] start: %v", err)
	}
	_ = conn.Close()

	// A tick now reports the host's craft into the store.
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if seen := srv.relay.Snapshot("SHA256:someone"); len(seen) != 1 || seen[0].Owner != sessiondir.HostFingerprint {
		t.Errorf("host tick reports = %+v, want one owned by host", seen)
	}
}

// [h] stop: the listener shuts down, the wrapper drops back to solo,
// a guest's persisted envelope survives, and the host's own game is
// uninterrupted (clock unchanged).
func TestStopHostingDropsGuestsPersists(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = WrapHost(app, nil, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	m, _ = m.Update(screens.SessionHostMsg{Start: true})
	srv := hostServer(t, m)
	if srv == nil {
		t.Fatal("start failed")
	}

	// A guest enrolls and banks some progress.
	enrollDirect(t, srv, "SHA256:gern", "gern")
	wGuest, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	guestTime := wGuest.Clock.SimTime.Add(5 * 24 * time.Hour)
	wGuest.Clock.SimTime = guestTime
	if err := srv.store.SavePlayer("SHA256:gern", wGuest); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	hostClock := app.World().Clock.SimTime

	m, _ = m.Update(screens.SessionHostMsg{Start: false})
	if s := hostServer(t, m); s != nil {
		t.Errorf("stop left a live server: %v", s)
	}
	if info := app.World().Session; info != nil {
		t.Errorf("stop left a session slate: %+v", info)
	}
	// Host game uninterrupted: the clock never moved on the stop.
	if got := app.World().Clock.SimTime; !got.Equal(hostClock) {
		t.Errorf("host clock moved on stop: %v != %v", got, hostClock)
	}
	// Guest progress persisted through the drop.
	wBack, err := srv.store.LoadPlayer("SHA256:gern")
	if err != nil {
		t.Fatalf("guest payload lost after stop: %v", err)
	}
	if !wBack.Clock.SimTime.Equal(guestTime) {
		t.Errorf("guest payload time = %v, want %v", wBack.Clock.SimTime, guestTime)
	}

	// The listener really closes (shutdown runs in the background).
	addr := srv.Addr()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, derr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if derr != nil {
			return // closed — success
		}
		_ = conn.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("listener still accepting after stop at %s", addr)
}

// A bind failure (port already in use) surfaces as a toast on the
// host's own screen, not a crash — and no server is left behind.
func TestStartHostingPortConflict(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// Occupy a port on all interfaces so ":port" collides.
	occupier, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("occupier listen: %v", err)
	}
	defer occupier.Close()
	port := occupier.Addr().(*net.TCPAddr).Port

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = WrapHost(app, nil, port)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	m, _ = m.Update(screens.SessionHostMsg{Start: true})

	if s := hostServer(t, m); s != nil {
		t.Errorf("port conflict still produced a server: %v", s)
	}
	if info := app.World().Session; info != nil {
		t.Errorf("failed start populated a slate: %+v", info)
	}
	if out := stripANSI(m.View()); !strings.Contains(out, "can't host") {
		t.Errorf("port conflict didn't toast; frame:\n%s", firstLines(out, 4))
	}
}

// The --serve headless path (WrapHost with a live server) reports as
// the host on the first tick — parity with HostModel, exercised
// without the ssh front door.
func TestWrapHostEagerReports(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	m := tick(WrapHost(app, srv, DefaultPort))
	if s := hostServer(t, m); s != srv {
		t.Fatalf("eager WrapHost lost its server: %v", s)
	}
	seen := srv.relay.Snapshot("SHA256:guest")
	if len(seen) != 1 || seen[0].Owner != sessiondir.HostFingerprint {
		t.Fatalf("eager host reports = %+v, want one owned by host", seen)
	}
}
