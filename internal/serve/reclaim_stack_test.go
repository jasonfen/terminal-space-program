package serve

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// parkedStackFor builds a cross-player stack owned by ownerFP, persists it as
// that player's saved program, and returns the ledger record naming it — the
// state a dock is in when its owner has gone offline holding the stack.
func parkedStackFor(t *testing.T, srv *Server, ownerFP, guestFP string) relay.DockRecord {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	guest.Primary = w.ActiveCraft().Primary
	guest.State = w.ActiveCraft().State
	guest.ID = 200
	w.AdoptCraft(guest, false)
	comp, _, ok := w.DockGuestCraft(0, guest, guestFP)
	if !ok {
		t.Fatalf("DockGuestCraft refused")
	}
	if err := srv.store.SavePlayer(ownerFP, w); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}
	rec := relay.DockSnapshot{
		ID: 7, Owner: ownerFP, OwnerHandle: "gern", DockerCraftID: 1,
		CompositeID: comp.ID, GuestOwner: guestFP, GuestHandle: "jason",
		GuestCraftID: 200, Phase: relay.DockActive,
	}
	srv.dock.SeedFull([]relay.DockSnapshot{rec}, nil)
	return relay.DockRecord{
		ID: rec.ID, Owner: rec.Owner, OwnerHandle: rec.OwnerHandle,
		DockerCraftID: rec.DockerCraftID, CompositeID: rec.CompositeID,
		GuestOwner: rec.GuestOwner, GuestHandle: rec.GuestHandle,
		GuestCraftID: rec.GuestCraftID, Phase: rec.Phase,
	}
}

// TestReclaimFromAnEmptySeatGrantsImmediately (ADR 0040 §4): the mirror of the
// presence gate. A player riding a stack whose owner is not in the session
// presses [J] and gets the stick — the stack migrates out of the absent
// owner's saved program and into theirs. Waiting durably for the owner
// instead would be #312's stranding wearing a different hat.
func TestReclaimFromAnEmptySeatGrantsImmediately(t *testing.T) {
	const absentFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, absentFP, "gern")
	rec := parkedStackFor(t, srv, absentFP, sessiondir.HostFingerprint)

	if srv.presence.isOnline(absentFP) {
		t.Fatalf("test setup: the owner must be offline")
	}
	if err := srv.reclaimFromEmptySeat(rec, sessiondir.HostFingerprint); err != nil {
		t.Fatalf("reclaim from an empty seat refused: %v", err)
	}

	// The absent owner's saved program no longer holds the stack — no clone.
	after, err := srv.store.LoadPlayer(absentFP)
	if err != nil {
		t.Fatalf("LoadPlayer: %v", err)
	}
	if _, _, ok := after.CraftByID(rec.CompositeID); ok {
		t.Errorf("the stack is still in the absent owner's program — it now exists twice")
	}

	// And it is delivered on the reclaimer's next tick.
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Crafts = nil
	chips := srv.dock.Reconcile(w, sessiondir.HostFingerprint, nil)
	if len(w.Crafts) != 1 || !sim.StackHasGuest(w.Crafts[0]) {
		t.Fatalf("the reclaimer holds %d craft, want the migrated stack", len(w.Crafts))
	}
	var told bool
	for _, c := range chips {
		if c.Kind == sim.SessionEventTransfer {
			told = true
		}
	}
	if !told {
		t.Errorf("the reclaim arrived with no moment: %+v", chips)
	}

	// The returning owner is told what happened rather than finding their
	// vehicle simply gone.
	ownerWorld, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	ownerWorld.Crafts = nil
	oChips := srv.dock.Reconcile(ownerWorld, absentFP, nil)
	var explained bool
	for _, c := range oChips {
		if c.Kind == sim.SessionEventControlReclaimed {
			explained = true
		}
	}
	if !explained {
		t.Errorf("the returning owner got no account of the missing stack: %+v", oChips)
	}
}

// TestReclaimRefusedWhileTheOwnerIsAtTheControls: the asymmetry only runs one
// way. A live owner keeps their stack; the reclaimer is told to ask.
func TestReclaimRefusedWhileTheOwnerIsAtTheControls(t *testing.T) {
	const ownerFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, ownerFP, "gern")
	parkedStackFor(t, srv, ownerFP, sessiondir.HostFingerprint)
	srv.presence.markOnline(ownerFP)

	_, why := srv.dock.ReclaimTarget(sessiondir.HostFingerprint, srv.presence.isOnline)
	if why == "" {
		t.Fatalf("a live owner's stack was taken out from under them")
	}
	if !strings.Contains(why, "gern") {
		t.Errorf("refusal %q does not name who is flying it", why)
	}
}

// TestGuestPressingJRoutesToReclaim: [J] from the guest seat reaches the
// reclaim path rather than the old "not flying a cross-player stack" no-op,
// and a refusal there is still said out loud.
func TestGuestPressingJRoutesToReclaim(t *testing.T) {
	const ownerFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, ownerFP, "gern")
	parkedStackFor(t, srv, ownerFP, sessiondir.HostFingerprint)
	srv.presence.markOnline(ownerFP) // live owner → the refusing branch

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	app.World().DockGuest = &sim.DockGuestLink{OwnerFP: ownerFP, OwnerHandle: "gern", GuestCraftID: 1}

	var model tea.Model = srv.HostModel(app)
	model, _ = model.Update(tui.TransferControlMsg{})
	rm, ok := model.(reportingModel)
	if !ok {
		t.Fatalf("wrapper model = %T", model)
	}
	var told string
	for _, e := range rm.localEvents {
		if e.Kind == sim.SessionEventTransferRefused {
			told = e.Detail
		}
	}
	if told == "" {
		t.Fatalf("[J] from the guest seat produced no moment: %+v", rm.localEvents)
	}
	if !strings.Contains(told, "controls") {
		t.Errorf("guest-seat refusal %q does not explain the live owner", told)
	}
}

// TestSupervisorRestartAnnouncesAndNeverGates (ADR 0040 §6): the stop signal
// the hourly auto-adopt sends fires the restart moment before the listener
// drains — and it fires with docks live, because with a durable ledger a
// gate protects nothing and would let one long docked mission pin the server
// to an old build.
func TestSupervisorRestartAnnouncesAndNeverGates(t *testing.T) {
	srv := newOfflineServer(t)
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 1, Owner: "a", GuestOwner: "b", GuestCraftID: 10, Phase: relay.DockActive,
	}}, nil)

	oldExit, oldGrace := exitFunc, restartAnnounceGrace
	defer func() { exitFunc, restartAnnounceGrace = oldExit, oldGrace }()
	restartAnnounceGrace = 0
	exited := make(chan int, 1)
	exitFunc = func(code int) { exited <- code }

	sig := make(chan os.Signal, 1)
	sig <- syscall.SIGTERM
	done := make(chan struct{})
	go func() { srv.restartOnSignal(sig, nil); close(done) }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("the restart never completed")
	}
	select {
	case code := <-exited:
		if code != 0 {
			t.Errorf("exit code = %d, want 0 (the supervisor asked for this stop)", code)
		}
	default:
		t.Fatalf("the restart never exited")
	}

	var announced bool
	for _, e := range srv.presence.eventsFor("someone") {
		if e.Kind == sim.SessionEventServerRestart {
			announced = true
		}
	}
	if !announced {
		t.Errorf("the restart was a silent yank — no server-restart moment reached the session")
	}
	if len(srv.dock.Records()) != 1 {
		t.Errorf("the restart path disturbed the dock ledger: %+v", srv.dock.Records())
	}
}
