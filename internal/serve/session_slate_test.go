package serve

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// tick drives one WindowSize + Tick through a wrapped model.
func tick(m tea.Model) tea.Model {
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	m, _ = m.Update(sim.TickMsg(time.Now()))
	return m
}

// The wrapper populates the world's session slate: roster rows with
// presence, Δt, craft counts; invites only for the host; and recent
// join/leave events (viewer's own excluded).
func TestSessionSlatePopulated(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")
	if _, err := srv.store.MintInvite("newbie"); err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	srv.presence.markOnline("SHA256:gern")
	srv.presence.event(sim.SessionEventJoin, "SHA256:gern", "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	host := tick(srv.HostModel(hostApp))
	_ = host
	w := hostApp.World()
	info := w.Session
	if info == nil {
		t.Fatal("host world has no session slate after a tick")
	}
	if !info.IsHost {
		t.Error("host slate not marked IsHost")
	}
	if len(info.Players) != 2 {
		t.Fatalf("players = %d, want 2 (host + gern)", len(info.Players))
	}
	var gern *sim.SessionPlayer
	for i := range info.Players {
		if info.Players[i].Handle == "gern" {
			gern = &info.Players[i]
		}
	}
	if gern == nil || !gern.Online {
		t.Fatalf("gern row missing or offline: %+v", info.Players)
	}
	if len(info.Invites) != 1 || info.Invites[0].Handle != "newbie" {
		t.Errorf("host invites = %+v", info.Invites)
	}
	// The join chip event reaches the host's world (owner ≠ viewer).
	if len(w.SessionEvents) != 1 || w.SessionEvents[0].Handle != "gern" {
		t.Errorf("host SessionEvents = %+v", w.SessionEvents)
	}

	// A guest's slate: no invites, and their own join excluded.
	guestApp, err := srv.newGuestApp("SHA256:gern")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	_ = tick(srv.withReporting(guestApp, "SHA256:gern"))
	ginfo := guestApp.World().Session
	if ginfo == nil || ginfo.IsHost {
		t.Fatalf("guest slate = %+v", ginfo)
	}
	if len(ginfo.Invites) != 0 {
		t.Error("guest slate leaked invites")
	}
	if len(guestApp.World().SessionEvents) != 0 {
		t.Errorf("guest sees their own join: %+v", guestApp.World().SessionEvents)
	}
}

// A sync arrival fires chips on both sides: "synced to X" locally,
// "X synced to you" through the broadcast ring (S7).
func TestSyncArrivalChips(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")

	guestApp, err := srv.newGuestApp("SHA256:gern")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	guest := tick(srv.withReporting(guestApp, "SHA256:gern"))

	// Engage a short sync and jump past its target — the next tick's
	// resolve releases and stamps LastSyncArrival; the wrapper turns
	// it into events.
	w := guestApp.World()
	if !w.EngageSyncWarp(w.Clock.SimTime.Add(time.Hour), "jason") {
		t.Fatal("EngageSyncWarp refused")
	}
	w.Clock.SimTime = w.Clock.SimTime.Add(2 * time.Hour)
	guest, _ = guest.Update(sim.TickMsg(time.Now()))

	var sawLocal bool
	for _, e := range w.SessionEvents {
		if e.Kind == sim.SessionEventSyncedTo && e.Handle == "jason" {
			sawLocal = true
		}
	}
	if !sawLocal {
		t.Errorf("syncer's own arrival chip missing: %+v", w.SessionEvents)
	}

	// The host (any other viewer) sees the broadcast side.
	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	_ = tick(srv.HostModel(hostApp))
	var sawBroadcast bool
	for _, e := range hostApp.World().SessionEvents {
		if e.Kind == sim.SessionEventSync && e.Handle == "gern" {
			sawBroadcast = true
		}
	}
	if !sawBroadcast {
		t.Errorf("host missing the synced-to-you chip: %+v", hostApp.World().SessionEvents)
	}
	_ = guest
}

// SessionAdminMsg executes mint/revoke/remove against the store and
// refreshes the slate immediately.
func TestSessionAdminCommands(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	m := tick(srv.HostModel(hostApp))

	m, _ = m.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdMint, Handle: "newpal",
	}})
	meta, _ := srv.store.Meta()
	if len(meta.Invites) != 1 || meta.Invites[0].Handle != "newpal" {
		t.Fatalf("mint via admin msg: invites = %+v", meta.Invites)
	}
	if inv := hostApp.World().Session.Invites; len(inv) != 1 {
		t.Errorf("slate not refreshed after mint: %+v", inv)
	}

	m, _ = m.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdRevoke, Code: meta.Invites[0].Code,
	}})
	if meta, _ = srv.store.Meta(); len(meta.Invites) != 0 {
		t.Errorf("revoke via admin msg left invites: %+v", meta.Invites)
	}

	_, _ = m.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdRemove, Fingerprint: "SHA256:gern",
	}})
	if _, err := srv.store.FindPlayer("SHA256:gern"); !errors.Is(err, sessiondir.ErrNotEnrolled) {
		t.Errorf("remove via admin msg: %v", err)
	}
}
