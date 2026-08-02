package serve

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// #288 across the whole seam: a guest flying a Moon craft while their
// first slot orbits Earth must be listed at the Moon on the host's
// roster. Live this was a pilot 577 km from their rendezvous partner at
// Earth showing as "Sol/sun" the entire session.
func TestRosterLocationFollowsTheActiveCraft(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")
	srv.presence.markOnline("SHA256:gern")

	guestApp, err := srv.newGuestApp("SHA256:gern")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	gw := guestApp.World()
	moon := gw.Systems[0].FindBody("Moon")
	if moon == nil {
		t.Skip("Moon not in catalog")
	}
	if _, err := gw.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	gw.Crafts[1].Primary = *moon
	gw.SetActiveCraftIdx(1) // flying the Moon craft; slot 0 is still at Earth
	guest := tick(srv.withReporting(guestApp, "SHA256:gern"))
	_ = guest

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	_ = tick(srv.HostModel(hostApp))

	info := hostApp.World().Session
	if info == nil {
		t.Fatal("host has no session slate")
	}
	var gern *sim.SessionPlayer
	for i := range info.Players {
		if info.Players[i].Handle == "gern" {
			gern = &info.Players[i]
		}
	}
	if gern == nil {
		t.Fatalf("no gern row: %+v", info.Players)
	}
	if gern.Primary != moon.ID {
		t.Errorf("roster LOCATION primary = %q, want %q (the craft they are flying, not slot 0)",
			gern.Primary, moon.ID)
	}
	if gern.CraftCount != 2 {
		t.Errorf("craft count = %d, want 2 — LOCATION follows the active craft, the count still covers the fleet",
			gern.CraftCount)
	}
}
