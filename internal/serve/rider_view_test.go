package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// TestDockGuestFollowsStackCamera drives a real cross-player dock through
// the reporting seam (the same harness as
// TestCrossPlayerDockThroughReportingModels) and asserts the ADR 0038 S4
// follow-through: once absorbed, the guest's camera is repointed at the
// stack's ghost — not left on FocusCraft with nothing to show (#301) — and
// that ghost ref actually resolves (DockGuestStackGhost), so the rider-view
// camera and badged panels have real data to render, not a dangling ref.
func TestDockGuestFollowsStackCamera(t *testing.T) {
	const guestFP = "SHA256:gern-rider"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, guestFP, "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New host: %v", err)
	}
	guestApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New guest: %v", err)
	}
	var host tea.Model = srv.HostModel(hostApp)
	var guest tea.Model = srv.withReporting(guestApp, guestFP)

	hw, gw := hostApp.World(), guestApp.World()
	gw.Clock.SimTime = hw.Clock.SimTime
	gw.ActiveCraft().ID = 778
	gw.ActiveCraft().State = hw.ActiveCraft().State
	// The guest was flying normally before the dock (FocusCraft) — the
	// baseline #301 regressed from.
	gw.Focus = sim.Focus{Kind: sim.FocusCraft}

	guest = tick(guest)
	host = tick(host)

	for i := 0; i < 6 && !hostStackHasGuest(hw); i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		if gc, hc := gw.ActiveCraft(), hw.ActiveCraft(); gc != nil && hc != nil && !hostStackHasGuest(hw) {
			gc.State = hc.State
		}
		host, _ = host.Update(sim.TickMsg(time.Now()))
		guest, _ = guest.Update(sim.TickMsg(time.Now()))
	}
	if !hostStackHasGuest(hw) {
		t.Fatalf("host never fused a cross-player stack (crafts=%d, coupled=%v)", len(hw.Crafts), hw.CoWarp.Coupled)
	}
	if gw.DockGuest == nil {
		t.Fatalf("guest world not docked-as-guest")
	}

	// A few more rounds: the guest's own report of "my craft is gone" and the
	// host's report of its now-fused ActiveCraftID both need to round-trip
	// through the store before the guest's tick can resolve a ghost for it.
	for i := 0; i < 4; i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		host, _ = host.Update(sim.TickMsg(time.Now()))
		guest, _ = guest.Update(sim.TickMsg(time.Now()))
	}

	if gw.Focus.Kind != sim.FocusGhost {
		t.Fatalf("guest camera not following the stack: Focus = %+v", gw.Focus)
	}
	if gw.Focus.GhostOwner != sessiondir.HostFingerprint {
		t.Errorf("guest camera following the wrong owner: %+v", gw.Focus)
	}
	if _, _, ok := gw.DockGuestStackGhost(); !ok {
		t.Errorf("guest camera points at a ghost ref that does not resolve: Focus=%+v Ghosts=%+v", gw.Focus, gw.Ghosts)
	}
	if len(gw.Crafts) != 0 {
		t.Errorf("guest slate not empty as expected post-fuse: %d crafts", len(gw.Crafts))
	}
}
