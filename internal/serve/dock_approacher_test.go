package serve

import (
	"math"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/relay"
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
			srv, host, guest, hw, gw := seats(t, 0.55)
			guestRef := gw.Crafts[0].ID
			// The passive vessel holds its pointing (a pilot with the stick
			// still); a magnet pull on it would turn it onto the guest's axis.
			hostCraft := hw.Crafts[0]
			hostCraft.SlewRateDegPerSec = 1e-9
			hostAtt0 := hostCraft.CurrentAttitudeDir
			// The guest flies it: it has the host's vessel targeted.
			host = tick(host) // ghost slate fresh on both seats
			guest = tick(guest)
			if len(gw.Ghosts) == 0 || len(hw.Ghosts) == 0 {
				t.Fatalf("ghosts not exchanged: guest sees %d, host sees %d", len(gw.Ghosts), len(hw.Ghosts))
			}
			gw.SetTargetGhost(gw.Ghosts[0].Owner, gw.Ghosts[0].CraftID)

			hostPulled, guestPulled, hostTurned := false, false, false
			host, guest = flyBoth(host, guest, hw, gw, guestFirst, 600,
				func() bool { return guestStackHasHost(gw) },
				func() {
					hostPulled = hostPulled || ghostPulling(hw)
					guestPulled = guestPulled || ghostPulling(gw)
					if !guestStackHasHost(gw) && len(hw.Crafts) > 0 && angleBetween(hw.Crafts[0].CurrentAttitudeDir, hostAtt0) > 1e-6 {
						hostTurned = true
					}
				})
			_, _ = host, guest
			if hostTurned {
				t.Error("the passive seat's vessel was turned before the dock")
			}

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

// runToStack flies both seats until either owns a cross-player stack, in the
// given tick order, recording which seat ever ran a pull.
func runToStack(host, guest tea.Model, hw, gw *sim.World, guestFirst bool) (hostPulled, guestPulled bool) {
	flyBoth(host, guest, hw, gw, guestFirst, 600,
		func() bool { return hostStackHasGuest(hw) || guestStackHasHost(gw) },
		func() {
			hostPulled = hostPulled || ghostPulling(hw)
			guestPulled = guestPulled || ghostPulling(gw)
		})
	return
}

func primeGhosts(host, guest tea.Model) (tea.Model, tea.Model) {
	return tick(host), tick(guest)
}

// Both seats aim at each other, or neither does: the pair still resolves to
// ONE puller and ONE stack owner, the same one whichever seat ticks first. The
// stable key (lower fingerprint) breaks the tie; "SHA256:..." sorts before
// "local", so the guest's seat is the approacher here.
func TestTiedApproachersResolveToTheSameOneSeatInEitherTickOrder(t *testing.T) {
	for _, bothAim := range []bool{true, false} {
		for _, guestFirst := range []bool{false, true} {
			srv, host, guest, hw, gw := seats(t, 0.55)
			host, guest = primeGhosts(host, guest)
			if bothAim {
				aimAtGhost(t, hw)
				aimAtGhost(t, gw)
			}
			hp, gp := runToStack(host, guest, hw, gw, guestFirst)
			if hp || !gp {
				t.Errorf("bothAim=%v guestFirst=%v: pulls host=%v guest=%v, want only the guest", bothAim, guestFirst, hp, gp)
			}
			if !guestStackHasHost(gw) || hostStackHasGuest(hw) || hw.DockGuest == nil || gw.DockGuest != nil {
				t.Errorf("bothAim=%v guestFirst=%v: ownership guestOwns=%v hostOwns=%v hostRides=%v guestRides=%v",
					bothAim, guestFirst, guestStackHasHost(gw), hostStackHasGuest(hw), hw.DockGuest != nil, gw.DockGuest != nil)
			}
			if srv.dock.RecordCount() != 1 {
				t.Errorf("bothAim=%v guestFirst=%v: ledger records = %d", bothAim, guestFirst, srv.dock.RecordCount())
			}
		}
	}
}

// The rendezvous initiator is pilot-in-command (ADR 0037 §2): the host, as
// initiator, owns the stack even though the guest has it targeted and is the
// lower key.
func TestRendezvousInitiatorIsTheApproacher(t *testing.T) {
	for _, guestFirst := range []bool{false, true} {
		_, host, guest, hw, gw := seats(t, 0.55)
		host, guest = primeGhosts(host, guest)
		aimAtGhost(t, gw)
		hw.EngageRendezvousWarpAs("SHA256:gern-port", "gern", time.Time{}, 0, true)
		gw.EngageRendezvousWarpAs("local", "host", time.Time{}, 0, false)
		hp, gp := runToStack(host, guest, hw, gw, guestFirst)
		if !hp || gp || !hostStackHasGuest(hw) || guestStackHasHost(gw) {
			t.Errorf("guestFirst=%v: pulls host=%v guest=%v hostOwns=%v guestOwns=%v, want the initiator (host) alone",
				guestFirst, hp, gp, hostStackHasGuest(hw), guestStackHasHost(gw))
		}
	}
}

// The ledger refuses the Claim at the moment the pull completes (here: the pair
// is in its re-arm cooldown, re-seeded every tick the pull runs because the
// ledger retires a hand-built latch on its first reconcile). Both seats end
// free and consistent: no stack, no rider, no record, the pull registration
// released, and the approacher does not loop the magnet.
func TestRefusedClaimLeavesBothSeatsFreeAndConsistent(t *testing.T) {
	srv, host, guest, hw, gw := seats(t, 0.55)
	host, guest = primeGhosts(host, guest)
	aimAtGhost(t, gw)
	hostID, guestID := hw.Crafts[0].ID, gw.Crafts[0].ID
	pulls, pullTicks := false, 0
	for i := 0; i < 400; i++ {
		if ghostPulling(gw) || pulls {
			// From the first pull on the refusal stands (a latch that does not
			// lapse), so the pair is blocked: the hulls may bounce apart, but
			// no second magnet may start.
			pulls = true
			srv.dock.Seed([]relay.DockRecord{{
				ID: 90, Owner: "local", DockerCraftID: hostID,
				GuestOwner: "SHA256:gern-port", GuestCraftID: guestID, Phase: relay.DockCooldown,
			}})
		}
		gw.Clock.SimTime = hw.Clock.SimTime
		// The guest ticks first: the host's reconcile would retire the
		// hand-seeded latch before the guest files its Claim.
		guest, _ = guest.Update(sim.TickMsg(time.Now()))
		host, _ = host.Update(sim.TickMsg(time.Now()))
		if ghostPulling(gw) {
			pullTicks++
		}
	}
	if !pulls {
		t.Fatal("the guest never pulled")
	}
	if guestStackHasHost(gw) || hostStackHasGuest(hw) || gw.DockGuest != nil || hw.DockGuest != nil {
		t.Errorf("a stack formed despite the refused Claim: guestOwns=%v hostOwns=%v", guestStackHasHost(gw), hostStackHasGuest(hw))
	}
	if len(gw.Crafts) != 1 || len(hw.Crafts) != 1 {
		t.Errorf("a seat lost its vessel: guest=%d host=%d", len(gw.Crafts), len(hw.Crafts))
	}
	if srv.dock.RecordCount() != 0 {
		t.Errorf("ledger records = %d, want none", srv.dock.RecordCount())
	}
	if ghostPulling(gw) || ghostPulling(hw) {
		t.Error("a magnet is still pulling after the refusal")
	}
	if pullTicks > 150 {
		t.Errorf("the magnet ran %d ticks: it looped after the refused Claim", pullTicks)
	}
	if p, ok := srv.dock.Approach.PullerOf(relay.SeatCraft{Owner: "SHA256:gern-port", CraftID: guestID}, relay.SeatCraft{Owner: "local", CraftID: hostID}); ok {
		t.Errorf("pull registration left latched on %v", p)
	}
}

// Another dock wins the host's vessel during the five seconds (a third player's
// Claim lands first). The approacher's magnet lets go when the ghost goes, no
// Claim is filed by it, and it keeps its own vessel and its registration is
// released.
func TestAnotherDockTakingThePartnerMidPullReleasesTheMagnet(t *testing.T) {
	srv, host, guest, hw, gw := seats(t, 0.55)
	host, guest = primeGhosts(host, guest)
	aimAtGhost(t, gw)
	hostID, guestID := hw.Crafts[0].ID, gw.Crafts[0].ID
	taken := false
	for i := 0; i < 400; i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		host, _ = host.Update(sim.TickMsg(time.Now()))
		guest, _ = guest.Update(sim.TickMsg(time.Now()))
		if !taken && ghostPulling(gw) {
			if _, ok := srv.dock.Claim("SHA256:third", "third", 5, "local", "host", hostID); !ok {
				t.Fatal("setup claim refused")
			}
			taken = true
		}
	}
	if !taken {
		t.Fatal("the guest never pulled")
	}
	if guestStackHasHost(gw) || gw.DockGuest != nil || len(gw.Crafts) != 1 {
		t.Errorf("the approacher is not free with its own vessel: crafts=%d stack=%v rides=%v", len(gw.Crafts), guestStackHasHost(gw), gw.DockGuest != nil)
	}
	if srv.dock.RecordCount() != 1 {
		t.Errorf("ledger records = %d, want only the third party's", srv.dock.RecordCount())
	}
	if ghostPulling(gw) {
		t.Error("the magnet is still pulling a ghost that is gone")
	}
	if p, ok := srv.dock.Approach.PullerOf(relay.SeatCraft{Owner: "SHA256:gern-port", CraftID: guestID}, relay.SeatCraft{Owner: "local", CraftID: hostID}); ok {
		t.Errorf("pull registration left latched on %v", p)
	}
}

// The friend turns away mid-pull: the gate fails on the approacher's seat, the
// magnet lets go, the registration is released, nothing is claimed, and the
// passive seat's vessel is untouched (it never held a pull).
func TestMagnetLettingGoMidPullLeavesBothSeatsFree(t *testing.T) {
	srv, host, guest, hw, gw := seats(t, 0.55)
	host, guest = primeGhosts(host, guest)
	aimAtGhost(t, gw)
	hostCraft := hw.Crafts[0]
	hostCraft.SlewRateDegPerSec = 1e-9
	hostID, guestID := hostCraft.ID, gw.Crafts[0].ID
	turned := false
	for i := 0; i < 400; i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		host, _ = host.Update(sim.TickMsg(time.Now()))
		guest, _ = guest.Update(sim.TickMsg(time.Now()))
		if !turned && ghostPulling(gw) {
			// The host's pilot swings the nose 30 degrees off.
			hostCraft.CurrentAttitudeDir = orbital.Rotate(hostCraft.CurrentAttitudeDir, orbital.Vec3{Z: 1}, 30*math.Pi/180).Unit()
			turned = true
			// The reporter re-sends a turned nose at most once a wall second.
			time.Sleep(1100 * time.Millisecond)
		}
	}
	if !turned {
		t.Fatal("the guest never pulled")
	}
	if ghostPulling(gw) || ghostPulling(hw) {
		t.Error("a pull is still running after the gate failed")
	}
	if guestStackHasHost(gw) || hostStackHasGuest(hw) || srv.dock.RecordCount() != 0 {
		t.Errorf("a dock formed or a Claim was filed: records=%d", srv.dock.RecordCount())
	}
	if len(gw.Crafts) != 1 || len(hw.Crafts) != 1 || gw.DockGuest != nil || hw.DockGuest != nil {
		t.Error("a seat is not free after the magnet let go")
	}
	if p, ok := srv.dock.Approach.PullerOf(relay.SeatCraft{Owner: "SHA256:gern-port", CraftID: guestID}, relay.SeatCraft{Owner: "local", CraftID: hostID}); ok {
		t.Errorf("pull registration left latched on %v", p)
	}
}

func angleBetween(a, b orbital.Vec3) float64 {
	d := a.Unit().Dot(b.Unit())
	return math.Acos(math.Max(-1, math.Min(1, d)))
}
