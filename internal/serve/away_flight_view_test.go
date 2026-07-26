package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// #253: Away must reach the FLIGHT view as standing state, the way it
// already reaches the [O] roster — not only as the 6 s went-quiet chip.
// These tests drive the full serve seam: Server.isAway → the co-warp peer
// slate → the world's standing flags, following the partner's live session
// through silent → back transitions.

// TestRendezvousPartnerAwayFollowsSession: mid-coast, the armed partner's
// session going silent raises the world's RendezvousPartnerAway flag, and
// their next drained frame drops it — driven by session liveness, not by
// an expiring SessionEvent.
func TestRendezvousPartnerAwayFollowsSession(t *testing.T) {
	const gernFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, gernFP, "gern")
	// The partner needs a live session for their arm to count at all
	// (#252 review: a dead session's relayed arm is suppressed at the
	// peer seam). Presence is the liveness input; the srv.live registry
	// below separately drives the Away verdict.
	srv.presence.markOnline(gernFP)

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = srv.HostModel(hostApp)
	m = tick(m)

	w := hostApp.World()
	tau := w.Clock.SimTime.Add(3 * time.Hour)
	w.EngageRendezvousWarp(gernFP, "gern", tau, 500)
	srv.relay.Report(gernArmedReport(w, sessiondir.HostFingerprint, tau))
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if !w.RendezvousWarpEngaged() {
		t.Fatal("mutual arm did not start the coast")
	}
	// No conn in the live registry for gern yet: nothing has gone
	// silent, so the partner must not read as away.
	if w.RendezvousPartnerAway {
		t.Fatal("partner marked away with no live session")
	}

	// gern's session goes silent past the away threshold — still simulating.
	register(t, srv, gernFP, time.Now().Add(-90*time.Second))
	srv.relay.Report(gernArmedReport(w, sessiondir.HostFingerprint, tau))
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if !w.RendezvousPartnerAway {
		t.Error("armed partner's silent session did not raise RendezvousPartnerAway")
	}

	// gern drains a frame: the standing state drops on the next tick.
	srv.live = newSessionRegistry()
	register(t, srv, gernFP, time.Now())
	srv.relay.Report(gernArmedReport(w, sessiondir.HostFingerprint, tau))
	m, _ = m.Update(sim.TickMsg(time.Now()))
	_ = m
	if w.RendezvousPartnerAway {
		t.Error("RendezvousPartnerAway still up after the partner returned")
	}
}

// TestDockGuestOwnerAwayFollowsSession: the other Commitment kind. While
// docked as guest, the stack owner's session going silent surfaces on the
// guest's DockGuest slate, and drops when the owner returns.
func TestDockGuestOwnerAwayFollowsSession(t *testing.T) {
	const guestFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, guestFP, "gern")
	srv.dock.Seed([]relay.DockRecord{{
		ID: 1, Owner: sessiondir.HostFingerprint, OwnerHandle: "vex",
		GuestOwner: guestFP, GuestHandle: "gern", GuestCraftID: 777,
		Phase: relay.DockActive,
	}})

	guestApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var guest tea.Model = srv.withReporting(guestApp, guestFP)
	guest = tick(guest)
	gw := guestApp.World()
	if gw.DockGuest == nil {
		t.Fatal("guest world not docked-as-guest from the seeded ledger")
	}
	if gw.DockGuest.OwnerAway {
		t.Fatal("owner marked away with no live session (that is offline)")
	}

	// The stack owner's session goes silent past the away threshold.
	register(t, srv, sessiondir.HostFingerprint, time.Now().Add(-90*time.Second))
	guest, _ = guest.Update(sim.TickMsg(time.Now()))
	if gw.DockGuest == nil || !gw.DockGuest.OwnerAway {
		t.Errorf("stack owner's silent session not surfaced on the DockGuest slate: %+v", gw.DockGuest)
	}

	// The owner drains a frame: the standing state drops.
	srv.live = newSessionRegistry()
	register(t, srv, sessiondir.HostFingerprint, time.Now())
	guest, _ = guest.Update(sim.TickMsg(time.Now()))
	_ = guest
	if gw.DockGuest == nil || gw.DockGuest.OwnerAway {
		t.Errorf("OwnerAway still up after the owner returned: %+v", gw.DockGuest)
	}
}
