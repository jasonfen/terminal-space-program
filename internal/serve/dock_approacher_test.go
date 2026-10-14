package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0053 decision 9 / "Settled here" item 2, review finding 3: cross-player
// exactly ONE seat runs the magnet for a pair, the approacher, and that seat
// owns the stack. These tests run both seats' Worlds and the ledger and assert
// the outcome (who pulled, who moved, who owns, what the ledger holds), not
// that a function ran.

// flyBoth ticks both seats (host first or guest first) until stop() or n ticks,
// calling each(i) after every tick pair.
func flyBoth(host, guest tea.Model, hw, gw *sim.World, guestFirst bool, n int, stop func() bool, each func()) (tea.Model, tea.Model) {
	for i := 0; i < n && !stop(); i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		if guestFirst {
			guest, _ = guest.Update(sim.TickMsg(time.Now()))
			host, _ = host.Update(sim.TickMsg(time.Now()))
		} else {
			host, _ = host.Update(sim.TickMsg(time.Now()))
			guest, _ = guest.Update(sim.TickMsg(time.Now()))
		}
		if each != nil {
			each()
		}
	}
	return host, guest
}

func guestStackHasHost(w *sim.World) bool {
	return len(w.Crafts) > 0 && sim.StackHasGuest(w.Crafts[0])
}

// The guest flies the approach (it targets the host's ghost, it is the one
// drifting in). Only the guest pulls; the host is hands-off and unturned;
// whichever seat ticks first, the GUEST owns the stack and the host rides.
func TestOnlyTheApproacherPullsAndOwnsTheStack(t *testing.T) {
	for _, guestFirst := range []bool{false, true} {
		name := "host-ticks-first"
		if guestFirst {
			name = "guest-ticks-first"
		}
		t.Run(name, func(t *testing.T) {
			srv, host, guest, hw, gw := seats(t, 0.4)
			guestRef := gw.Crafts[0].ID
			// The guest flies it: it has the host's vessel targeted.
			host = tick(host) // ghost slate fresh on both seats
			guest = tick(guest)
			if len(gw.Ghosts) == 0 || len(hw.Ghosts) == 0 {
				t.Fatalf("ghosts not exchanged: guest sees %d, host sees %d", len(gw.Ghosts), len(hw.Ghosts))
			}
			gw.SetTargetGhost(gw.Ghosts[0].Owner, gw.Ghosts[0].CraftID)

			hostPulled, guestPulled := false, false
			host, guest = flyBoth(host, guest, hw, gw, guestFirst, 600,
				func() bool { return guestStackHasHost(gw) },
				func() {
					hostPulled = hostPulled || ghostPulling(hw)
					guestPulled = guestPulled || ghostPulling(gw)
				})
			_, _ = host, guest

			if !guestPulled {
				t.Error("the approacher (guest) never ran a pull")
			}
			if hostPulled {
				t.Error("the passive seat (host) ran a magnet pull against the approacher")
			}
			if !guestStackHasHost(gw) {
				t.Fatalf("no stack on the approacher's seat (host stack has guest: %v)", hostStackHasGuest(hw))
			}
			if hostStackHasGuest(hw) {
				t.Error("the passive seat owns a stack too")
			}
			if hw.DockGuest == nil {
				t.Error("passive seat does not see the dock (DockGuest unset)")
			}
			if gw.DockGuest != nil {
				t.Error("the approacher is riding")
			}
			if len(hw.Crafts) != 0 {
				t.Errorf("host still holds %d vessel(s) after handing it over", len(hw.Crafts))
			}
			if srv.dock.RecordCount() != 1 {
				t.Fatalf("ledger records = %d, want 1", srv.dock.RecordCount())
			}
			for _, r := range srv.dock.Records() {
				if r.DockerCraftID != guestRef {
					t.Errorf("ledger docker craft = %d, want the approacher's %d", r.DockerCraftID, guestRef)
				}
			}
		})
	}
}
