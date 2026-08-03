package serve

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// TestClearedCooldownLatchDoesNotResurrectAfterRestart is #326 defect 2, driven
// through the real serve seam. reconcileDocking decides whether to flush from a
// successful Claim, an emitted chip, or a changed craft count — and a re-arm
// latch CLEARING produces none of the three, so the delete lands in memory
// only. Entering cooldown persists (it rides the undocked chip); leaving it did
// not. On the production host the hourly auto-adopt restart then re-seeded the
// latch from disk, in the one state (empty report store, partner long gone)
// where nothing can ever clear it again.
func TestClearedCooldownLatchDoesNotResurrectAfterRestart(t *testing.T) {
	const guestFP = "SHA256:gern"
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, guestFP, "gern")

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// A latch whose local craft is no longer in this World — the guest staged
	// it, or ended the flight. Nothing left of theirs to guard, so the next
	// reconcile clears the latch.
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 77, Owner: sessiondir.HostFingerprint, OwnerHandle: "host",
		DockerCraftID: 1, CompositeID: 90,
		GuestOwner: guestFP, GuestHandle: "gern", GuestCraftID: 9999,
		Phase:        relay.DockCooldown,
		ReturnAtNano: w.Clock.SimTime.UnixNano(),
	}}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("test setup: persistDocks: %v", err)
	}

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

	if recs := srv.dock.Records(); len(recs) != 0 {
		t.Fatalf("test setup: the latch did not clear in memory: %+v", recs)
	}

	restarted := serverOver(t, dir)
	if recs := restarted.dock.FullRecords(); len(recs) != 0 {
		t.Errorf("a cleared re-arm latch came back from disk after a restart: %+v", recs)
	}
}
