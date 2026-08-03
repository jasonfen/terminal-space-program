package serve

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// ADR 0040 review: persistDocks only fired on chip-producing transitions, so
// several state changes lived only in memory until some later, unrelated
// transition happened to flush them. These tests each drive one such window
// through the real serve seam — build a Server, cause the transition, then
// stand a BRAND NEW Server over the same session directory (a restart, same
// shape as TestParkedHandoverSurvivesAServerRestart) with no further persist
// call in between. If the transition didn't flush itself, the restart won't
// see it.

// TestReleaseAskSurvivesRestart: the owner's [U] on a cross-player stack sets
// releaseAsk without persisting. If the server restarts before some other
// transition happens to flush it, the ask silently vanishes and the guest's
// component never gets released.
func TestReleaseAskSurvivesRestart(t *testing.T) {
	const guestFP = "SHA256:gern"
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, guestFP, "gern")
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 9, Owner: sessiondir.HostFingerprint, OwnerHandle: "host",
		DockerCraftID: 1, CompositeID: 50,
		GuestOwner: guestFP, GuestHandle: "gern", GuestCraftID: 2,
		Phase: relay.DockActive,
	}}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("test setup: persistDocks: %v", err)
	}

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	model := srv.HostModel(app)
	model, _ = model.Update(tui.ReleaseGuestMsg{})
	_ = model

	restarted := serverOver(t, dir)
	recs := restarted.dock.FullRecords()
	if len(recs) != 1 || !recs[0].ReleaseAsk {
		t.Fatalf("the release ask did not survive a restart: %+v", recs)
	}
}

// TestGuestParkingSurvivesRestart: the guest's own tick removes its craft
// from its World and parks it on the dock record (reconcileGuest's DockPending
// branch) — the record's ONLY copy of that craft from that instant on. It
// produces no chip, so reconcileDocking's chip-driven "changed" flag never
// fires and the parking never gets flushed. A restart right after must not
// destroy the craft.
func TestGuestParkingSurvivesRestart(t *testing.T) {
	const guestFP = "SHA256:gern"
	const guestCraftID = 21
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, guestFP, "gern")

	if _, ok := srv.dock.Claim(sessiondir.HostFingerprint, "host", 1, guestFP, "gern", guestCraftID); !ok {
		t.Fatalf("Claim refused")
	}
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("test setup: persistDocks: %v", err)
	}

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ActiveCraft().ID = guestCraftID

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	model := srv.withReporting(app, guestFP)
	rm, ok := model.(reportingModel)
	if !ok {
		t.Fatalf("wrapper model = %T", model)
	}
	rm.reconcileDocking(w, map[string]bool{}, map[string]relay.CraftReport{}, map[string]string{}, time.Now())

	if len(w.Crafts) != 0 {
		t.Fatalf("test setup: guest craft was not parked, still holds %d craft", len(w.Crafts))
	}

	restarted := serverOver(t, dir)
	recs := restarted.dock.FullRecords()
	if len(recs) != 1 || recs[0].GuestPayload == nil {
		t.Fatalf("the parked handover did not survive a restart — it exists in no World and no persisted ledger: %+v", recs)
	}
}

// TestSIGTERMFlushesLedgerBeforeExit: SIGTERM (what the hourly auto-adopt
// sends) announces, drains the listener and waits for sessions to unwind,
// then exits — but persistMiddleware only ever writes each player's OWN
// craft payload, not the cross-player dock ledger. Any ledger mutation that
// reached the ledger without going through a call site that flushes itself
// (belt-and-braces against the very gaps 2a/2b close) rode only in memory
// into the shutdown and the exit that follows it. The signal path must
// flush the ledger before the process exits.
func TestSIGTERMFlushesLedgerBeforeExit(t *testing.T) {
	dir := t.TempDir()
	srv := serverOver(t, dir)
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 30, Owner: sessiondir.HostFingerprint, OwnerHandle: "host",
		DockerCraftID: 1, CompositeID: 60,
		GuestOwner: "SHA256:gern", GuestHandle: "gern", GuestCraftID: 5,
		Phase: relay.DockActive,
	}}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("test setup: persistDocks: %v", err)
	}

	// A ledger mutation that lands only in memory — the way any of 2a/2b's
	// call sites would look if their own flush were missing (or a future
	// one nobody added a flush to yet). This is exactly what the signal
	// path exists to catch on the way down.
	if ok, _ := srv.dock.RequestRelease(sessiondir.HostFingerprint, func(string) bool { return true }); !ok {
		t.Fatalf("test setup: RequestRelease refused")
	}

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
		t.Fatalf("the SIGTERM path never completed")
	}
	select {
	case <-exited:
	default:
		t.Fatalf("the SIGTERM path never exited")
	}

	restarted := serverOver(t, dir)
	recs := restarted.dock.FullRecords()
	if len(recs) != 1 || !recs[0].ReleaseAsk {
		t.Fatalf("SIGTERM exited without flushing pending ledger state: %+v", recs)
	}
}

// TestTransferRequestSurvivesRestart: [J] from the owner seat sets transferTo
// without persisting.
func TestTransferRequestSurvivesRestart(t *testing.T) {
	const guestFP = "SHA256:gern"
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, guestFP, "gern")
	srv.presence.markOnline(guestFP) // RequestTransfer's presence gate needs a live recipient
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 11, Owner: sessiondir.HostFingerprint, OwnerHandle: "host",
		DockerCraftID: 1, CompositeID: 51,
		GuestOwner: guestFP, GuestHandle: "gern", GuestCraftID: 3,
		Phase: relay.DockActive,
	}}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("test setup: persistDocks: %v", err)
	}

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	model := srv.HostModel(app)
	model, _ = model.Update(tui.TransferControlMsg{})
	_ = model

	restarted := serverOver(t, dir)
	recs := restarted.dock.FullRecords()
	if len(recs) != 1 || recs[0].TransferTo != guestFP {
		t.Fatalf("the transfer request did not survive a restart: %+v", recs)
	}
}

// TestUndockAskSurvivesRestart: the guest's undock-anytime signal sets
// undockAsk without persisting.
func TestUndockAskSurvivesRestart(t *testing.T) {
	const guestFP = "SHA256:gern"
	const guestCraftID = 4
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, guestFP, "gern")
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 13, Owner: sessiondir.HostFingerprint, OwnerHandle: "host",
		DockerCraftID: 1, CompositeID: 52,
		GuestOwner: guestFP, GuestHandle: "gern", GuestCraftID: guestCraftID,
		Phase: relay.DockActive,
	}}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("test setup: persistDocks: %v", err)
	}

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	app.World().DockGuest = &sim.DockGuestLink{OwnerFP: sessiondir.HostFingerprint, OwnerHandle: "host", GuestCraftID: guestCraftID}
	model := srv.withReporting(app, guestFP)
	model, _ = model.Update(tui.UndockGuestMsg{})
	_ = model

	restarted := serverOver(t, dir)
	recs := restarted.dock.FullRecords()
	if len(recs) != 1 || !recs[0].UndockAsk {
		t.Fatalf("the undock ask did not survive a restart: %+v", recs)
	}
}
