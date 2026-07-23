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
	srv.presence.event(sim.SessionEventJoin, "SHA256:gern", "gern", "")

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
	// The guest's own join is excluded from their chips. (Co-warp couple
	// chips can appear here — both worlds spawn the same coincident LEO
	// craft in the same subspace, which legitimately couples, v0.28 S1 —
	// so assert specifically on the absence of a join chip, the thing
	// this case is about.)
	for _, e := range guestApp.World().SessionEvents {
		if e.Kind == sim.SessionEventJoin {
			t.Errorf("guest sees a join chip (own join not excluded): %+v", e)
		}
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
	if !w.EngageSyncWarp(w.Clock.SimTime.Add(time.Hour), sessiondir.HostFingerprint, "jason") {
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

// Authorization is enforced in the handler, not the UI (v0.30 S1, #222):
// an admin intent forged from a guest-owned session — the UI never
// offers it, but the message reaches the handler directly — is refused,
// leaving the store untouched.
func TestSessionAdminAuthorizedInHandler(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")
	enrollDirect(t, srv, "SHA256:mallory", "mallory")

	guestApp, err := srv.newGuestApp("SHA256:mallory")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	// The guest's own wrapper, owned by the guest fingerprint.
	guest := tick(srv.withReporting(guestApp, "SHA256:mallory"))

	// Forge each admin intent from the guest session.
	guest, _ = guest.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdMint, Handle: "sneaky",
	}})
	guest, _ = guest.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdRemove, Fingerprint: "SHA256:gern",
	}})
	_ = guest

	meta, _ := srv.store.Meta()
	if len(meta.Invites) != 0 {
		t.Errorf("guest forged a mint: invites = %+v", meta.Invites)
	}
	if _, err := srv.store.FindPlayer("SHA256:gern"); err != nil {
		t.Errorf("guest forged a removal: gern is gone (%v)", err)
	}
}

// The host promotes a guest to admin; the admin can then mint/revoke
// invites, but delegation stays host-only — a forged promote from the
// admin's own session is refused (v0.30 S2, #223).
func TestAdminRoleCapabilities(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")
	enrollDirect(t, srv, "SHA256:newbie", "newbie")

	// Host promotes gern.
	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	host := tick(srv.HostModel(hostApp))
	host, _ = host.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdPromote, Fingerprint: "SHA256:gern",
	}})
	_ = host
	if p, _ := srv.store.FindPlayer("SHA256:gern"); p.Role != sessiondir.RoleAdmin {
		t.Fatalf("host promote didn't take: role = %q", p.Role)
	}

	// gern's own session: admin may mint.
	gernApp, err := srv.newGuestApp("SHA256:gern")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	gern := tick(srv.withReporting(gernApp, "SHA256:gern"))
	// The admin sees the invite pane.
	if info := gernApp.World().Session; info == nil || !info.CanAdminister || info.IsHost {
		t.Fatalf("admin slate = %+v (want CanAdminister, not IsHost)", info)
	}
	gern, _ = gern.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdMint, Handle: "fromadmin",
	}})
	if meta, _ := srv.store.Meta(); len(meta.Invites) != 1 || meta.Invites[0].Handle != "fromadmin" {
		t.Errorf("admin mint failed: invites = %+v", meta.Invites)
	}

	// But delegation is host-only: gern forging a promote is refused.
	gern, _ = gern.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdPromote, Fingerprint: "SHA256:newbie",
	}})
	_ = gern
	if p, _ := srv.store.FindPlayer("SHA256:newbie"); p.Role != sessiondir.RoleGuest {
		t.Errorf("admin promoted another player: newbie role = %q", p.Role)
	}
}

// An admin can remove a guest, but the removal guardrails hold at the
// handler even for forged intents: an admin can't remove another admin
// or the host (v0.30 S3, #224).
func TestAdminRemovalGuardrails(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:adminA", "adminA")
	enrollDirect(t, srv, "SHA256:adminB", "adminB")
	enrollDirect(t, srv, "SHA256:guest", "guest")
	if err := srv.store.PromoteAdmin("SHA256:adminA"); err != nil {
		t.Fatalf("promote adminA: %v", err)
	}
	if err := srv.store.PromoteAdmin("SHA256:adminB"); err != nil {
		t.Fatalf("promote adminB: %v", err)
	}

	adminApp, err := srv.newGuestApp("SHA256:adminA")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	admin := tick(srv.withReporting(adminApp, "SHA256:adminA"))

	// Forged removal of the host and of another admin: both refused.
	admin, _ = admin.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdRemove, Fingerprint: sessiondir.HostFingerprint,
	}})
	admin, _ = admin.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdRemove, Fingerprint: "SHA256:adminB",
	}})
	if _, err := srv.store.FindPlayer(sessiondir.HostFingerprint); err != nil {
		t.Errorf("admin removed the host: %v", err)
	}
	if _, err := srv.store.FindPlayer("SHA256:adminB"); err != nil {
		t.Errorf("admin removed another admin: %v", err)
	}

	// A guest, though: allowed.
	admin, _ = admin.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdRemove, Fingerprint: "SHA256:guest",
	}})
	_ = admin
	if _, err := srv.store.FindPlayer("SHA256:guest"); !errors.Is(err, sessiondir.ErrNotEnrolled) {
		t.Errorf("admin couldn't remove a guest: %v", err)
	}
}

// An admin restart drains and exits with the supervisor marker; a guest
// can't trigger it, enforced at the handler (v0.30 S4, #225). The restart
// is announced to connected players before the listener closes.
func TestAdminRestartExitsWithMarker(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")

	// Capture the exit instead of killing the test process; skip the grace.
	done := make(chan int, 1)
	oldExit, oldGrace := exitFunc, restartAnnounceGrace
	exitFunc = func(code int) { done <- code }
	restartAnnounceGrace = 0
	t.Cleanup(func() { exitFunc, restartAnnounceGrace = oldExit, oldGrace })

	// A guest can't restart: the forged intent is refused, no exit.
	guestApp, err := srv.newGuestApp("SHA256:gern")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	guest := tick(srv.withReporting(guestApp, "SHA256:gern"))
	_, _ = guest.Update(screens.SessionRestartMsg{})
	select {
	case code := <-done:
		t.Fatalf("guest triggered a restart (exit %d)", code)
	case <-time.After(150 * time.Millisecond):
	}

	// The host can: it exits with the dedicated marker.
	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	host := tick(srv.HostModel(hostApp))
	_, _ = host.Update(screens.SessionRestartMsg{})

	// The announcement is broadcast synchronously, before the drain.
	sawAnnounce := false
	for _, e := range srv.presence.eventsFor("SHA256:gern") {
		if e.Kind == sim.SessionEventServerRestart {
			sawAnnounce = true
		}
	}
	if !sawAnnounce {
		t.Error("restart wasn't announced to connected players")
	}

	select {
	case code := <-done:
		if code != restartExitCode {
			t.Errorf("restart exit code = %d, want %d", code, restartExitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restart never exited")
	}
}
