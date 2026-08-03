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

// Docking ends the standing agreement (ADR 0037 §1) — one of its exactly
// two end conditions. The sim can't see it: only the serve layer's dock
// ledger knows a cross-player dock fused, so the release runs from there.
// A PENDING claim must not end it (the claim can still abort back to two
// free craft, mid-approach), and neither must a dock with somebody else.
func TestFusedDockEndsRendezvousAgreement(t *testing.T) {
	const gernFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, gernFP, "gern")
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
	if w.RendezvousArm == nil {
		t.Fatal("precondition: mutual arm standing")
	}

	// A dock with a third party leaves this agreement alone.
	srv.dock.Seed([]relay.DockRecord{{
		ID: 91, Owner: sessiondir.HostFingerprint, GuestOwner: "SHA256:someone-else",
		GuestCraftID: 7, Phase: relay.DockActive,
	}})
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if w.RendezvousArm == nil {
		t.Fatal("an unrelated dock ended the agreement")
	}

	// A pending claim with the partner is a dock being attempted, not a dock.
	srv.dock.Seed([]relay.DockRecord{{
		ID: 92, Owner: sessiondir.HostFingerprint, GuestOwner: gernFP,
		GuestCraftID: 42, Phase: relay.DockPending,
	}})
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if w.RendezvousArm == nil {
		t.Fatal("a pending claim ended the agreement — it can still abort back to two free craft")
	}

	// Fused: the rendezvous is over because it succeeded.
	srv.dock.Seed([]relay.DockRecord{{
		ID: 92, Owner: sessiondir.HostFingerprint, GuestOwner: gernFP,
		GuestCraftID: 42, Phase: relay.DockActive,
	}})
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if w.RendezvousArm != nil {
		t.Fatal("fused cross-player dock did not end the standing agreement")
	}
	// The dock's own moment says what happened; a "cancelled" chip on top of
	// it would report a failure that did not occur.
	m, _ = m.Update(sim.TickMsg(time.Now()))
	_ = m
	for _, e := range w.SessionEvents {
		if e.Kind == sim.SessionEventRendezvousCancelled {
			t.Errorf("dock end chipped as a cancel: %+v", w.SessionEvents)
		}
	}
}
