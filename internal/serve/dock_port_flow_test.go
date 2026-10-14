package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// seats builds a host and a guest on one server, both flying ported tugs,
// the guest gapM port-to-port ahead of the host's nose.
func seats(t *testing.T, gapM float64) (srv *Server, host, guest tea.Model, hw, gw *sim.World) {
	t.Helper()
	const guestFP = "SHA256:gern-port"
	srv = newOfflineServer(t)
	enrollDirect(t, srv, guestFP, "gern")
	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	guestApp, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	host = srv.HostModel(hostApp)
	guest = srv.withReporting(guestApp, guestFP)
	hw, gw = hostApp.World(), guestApp.World()
	gw.Clock.SimTime = hw.Clock.SimTime
	arrangePortContactAt(t, hw, gw, 777, gapM)
	guest = tick(guest)
	host = tick(host)
	return
}

// A cross-player dock is the magnet, not the gate: the ledger holds no record
// while the pull runs (the five seconds of sim time), the host is hands-off
// and at 1x, and the Claim lands only when the pull completes. Then both
// seats' Worlds agree: one stack on the host, the guest riding.
func TestCrossPlayerDockClaimWaitsForTheMagnet(t *testing.T) {
	srv, host, guest, hw, gw := seats(t, 0.4)
	start := hw.Clock.SimTime
	var claimedAt time.Duration
	sawPull := false
	for i := 0; i < 400 && !hostStackHasGuest(hw); i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		host, _ = host.Update(sim.TickMsg(time.Now()))
		guest, _ = guest.Update(sim.TickMsg(time.Now()))
		if ghostPulling(hw) {
			sawPull = true
		}
		if claimedAt == 0 && srv.dock.RecordCount() > 0 {
			claimedAt = hw.Clock.SimTime.Sub(start)
		}
	}
	if !hostStackHasGuest(hw) {
		t.Fatalf("no cross-player stack (crafts=%d)", len(hw.Crafts))
	}
	if !sawPull {
		t.Error("the host never ran a magnet pull against the guest's ghost")
	}
	if claimedAt < 4900*time.Millisecond {
		t.Errorf("the Claim landed %v after the gate first held, want >= the 5 s magnet", claimedAt)
	}
	if gw.DockGuest == nil {
		t.Error("guest world not docked-as-guest")
	}
	if len(gw.Crafts) != 0 {
		t.Errorf("guest still holds %d vessel(s) after handing it over", len(gw.Crafts))
	}
}

// ghostPulling reports whether w is running a cross-player pull on any ghost.
func ghostPulling(w *sim.World) bool {
	for _, g := range w.Ghosts {
		if w.GhostCapturing(g.Owner, g.CraftID) {
			return true
		}
	}
	return false
}

// The old click is gone cross-player: 25 m port to port, at rest relative, in
// the old 50 m / 0.1 m/s box. Nothing docks, and the ledger stays empty.
func TestCrossPlayerNoClickAtTwentyFiveMetres(t *testing.T) {
	srv, host, guest, hw, gw := seats(t, 25)
	for i := 0; i < 500; i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		host, _ = host.Update(sim.TickMsg(time.Now()))
		guest, _ = guest.Update(sim.TickMsg(time.Now()))
	}
	if hostStackHasGuest(hw) || srv.dock.RecordCount() != 0 {
		t.Fatalf("a 25 m cross-player pair docked (records=%d)", srv.dock.RecordCount())
	}
	if len(gw.Crafts) != 1 {
		t.Errorf("guest lost its vessel: %d", len(gw.Crafts))
	}
}
