package serve

import (
	"os"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// TestFailedDockFlushIsRetriedNotSwallowed is #335. reconcileDocking's flush
// was `_ = m.srv.persistDocks()`, while reclaimFromEmptySeat treats the
// IDENTICAL failure as fatal — undoing both halves of its migration because
// the parked payload is the only copy of a craft. ADR 0038 §4 widened the
// undock handback from the Parcel-only path to every live undock, so the
// handback is now the common path through that same window: w.UndockGuest has
// already shrunken the owner's composite in place and the restored craft
// exists only on the record. Drop the error and the owner's world later saves
// without it, while disk still holds the pre-undock record — the guest's [U]
// then finds nothing of theirs in the composite and refuses forever.
//
// The flush is only ever driven by a transition, so a swallowed failure has no
// second chance: this drives one failing flush, restores the disk, and then
// ticks a reconcile with nothing whatsoever changed. Pre-fix that tick does
// nothing and the stale record survives the restart.
func TestFailedDockFlushIsRetriedNotSwallowed(t *testing.T) {
	const guestFP = "SHA256:gern"
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, guestFP, "gern")

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 78, Owner: sessiondir.HostFingerprint, OwnerHandle: "host",
		DockerCraftID: 1, CompositeID: 91,
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

	// The disk goes away underneath the transition (full volume, EIO, a
	// read-only mount): the atomic tmpfile+rename cannot land.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod session dir: %v", err)
	}
	if err := srv.persistDocks(); err == nil {
		t.Skip("this filesystem still writes to a read-only directory — cannot inject the failure")
	}

	// The transition: the latch clears (the guest's craft is not in this
	// World), and the flush that should carry it to disk fails.
	rm.reconcileDocking(w, map[string]bool{}, map[string]relay.CraftReport{}, map[string]string{}, time.Now())
	if recs := srv.dock.Records(); len(recs) != 0 {
		t.Fatalf("test setup: the latch did not clear in memory: %+v", recs)
	}

	// The disk comes back. The next tick has no transition of its own — no
	// claim, no chip, no craft moved, no record ended — so the only thing that
	// can still save the ledger is the failure not having been swallowed.
	if err := os.Chmod(dir, info.Mode().Perm()); err != nil {
		t.Fatalf("restoring session dir mode: %v", err)
	}
	rm.reconcileDocking(w, map[string]bool{}, map[string]relay.CraftReport{}, map[string]string{}, time.Now())

	restarted := serverOver(t, dir)
	if recs := restarted.dock.FullRecords(); len(recs) != 0 {
		t.Errorf("a failed dock flush was swallowed — disk still holds the pre-transition ledger: %+v", recs)
	}
}
