package serve

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestNoClaimOnAPlayerWhoAlreadyLeft (audit #12): a disconnected player's last
// report lingers in the relay store, so co-warp still coupled to their ghost
// and the host's magnet filed a Claim on it. That record had no exit (the
// owner has no verb for a Pending claim) and, when the player returned, took
// their vessel from wherever it was. Contact detection must only capture a
// player with a live session. The guest here has never been marked online,
// which is exactly the state after persistMiddleware marks them offline.
func TestNoClaimOnAPlayerWhoAlreadyLeft(t *testing.T) {
	const guestFP = "SHA256:gern-port"
	srv, host, guest, hw, gw := seats(t, 0.4)
	srv.presence.markOffline(guestFP)
	aimAtGhost(t, hw)
	if srv.presence.isOnline(guestFP) {
		t.Fatalf("test setup: the guest must be offline")
	}
	// The guest session is gone: only their frozen report remains, so just
	// the host's seat ticks.
	_ = guest
	for i := 0; i < 400; i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		host, _ = host.Update(sim.TickMsg(time.Now()))
		if srv.dock.RecordCount() > 0 {
			break
		}
	}
	if n := srv.dock.RecordCount(); n != 0 {
		t.Errorf("ledger holds %d dock record(s) against a player who is not connected", n)
	}
}
