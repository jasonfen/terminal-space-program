package serve

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// ADR 0040 amendment (10a, slice A2): the call sites of Repair. The ledger
// rules are tested crash-point by crash-point in internal/relay; these pin
// that each way a World enters the server from storage goes through them.

const (
	repairOwnerFP = "SHA256:gern"
	repairGuestFP = "SHA256:jason"
	repairXID     = 200
)

// guestLander is the guest's Vessel as it was at dock time.
func guestLander(w *sim.World) *spacecraft.Spacecraft {
	g := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	g.Primary = w.ActiveCraft().Primary
	g.State = w.ActiveCraft().State
	g.ID = repairXID
	return g
}

// staleFuseRecord is section 7.3 row 2: the ledger fused the guest's Vessel
// into the owner's lead, the owner's payload (lead only, written before) did
// not follow. The record keeps the guest's Vessel as OwnerCopy, owner dirty.
func staleFuseRecord(t *testing.T) relay.DockSnapshot {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	cp := save.CraftToWireForTransfer(guestLander(w))
	return relay.DockSnapshot{
		ID: 7, Owner: repairOwnerFP, OwnerHandle: "gern", DockerCraftID: 1, CompositeID: 1,
		GuestOwner: repairGuestFP, GuestHandle: "kip", GuestCraftID: repairXID,
		Phase: relay.DockActive, OwnerDirty: true, OwnerCopy: &cp,
	}
}

func snapshotByID(t *testing.T, srv *Server, id uint64) relay.DockSnapshot {
	t.Helper()
	for _, r := range srv.dock.FullRecords() {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no record %d in the ledger", id)
	return relay.DockSnapshot{}
}

func carriesPart(c *save.Craft, id uint64) bool {
	if c == nil {
		return false
	}
	for _, dc := range c.DockedComponents {
		if dc.CraftID == id {
			return true
		}
	}
	return false
}

// Call site 1: a guest connecting. The priming reconcile in withReporting runs
// before any tick, so the stale Vessel is gone from the World and from the
// payload, and the save that followed acknowledged.
func TestRepairRunsAtGuestConnect(t *testing.T) {
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, repairGuestFP, "kip")
	stale, err := sim.NewWorld() // written before the handover: still holds X
	if err != nil {
		t.Fatal(err)
	}
	stale.AdoptCraft(guestLander(stale), false)
	if err := srv.store.SavePlayer(repairGuestFP, stale); err != nil {
		t.Fatal(err)
	}
	rec := staleFuseRecord(t)
	rec.OwnerDirty, rec.OwnerCopy = false, nil
	rec.GuestDirty = true
	srv.dock.SeedFull([]relay.DockSnapshot{rec}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatal(err)
	}

	app, err := srv.newGuestApp(repairGuestFP)
	if err != nil {
		t.Fatal(err)
	}
	srv.withReporting(app, repairGuestFP)

	if _, _, ok := app.World().CraftByID(repairXID); ok {
		t.Errorf("the guest connected holding a Vessel the ledger says is in the stack")
	}
	saved, err := srv.store.LoadPlayer(repairGuestFP)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := saved.CraftByID(repairXID); ok {
		t.Errorf("the repaired World was not saved: the payload still holds the Vessel")
	}
	if got := snapshotByID(t, srv, rec.ID); got.GuestDirty {
		t.Errorf("the save that followed the repair did not acknowledge")
	}
	if got := snapshotByID(t, serverOver(t, dir), rec.ID); got.GuestDirty {
		t.Errorf("the acknowledgement was not flushed to session.json")
	}
}

// Call site 4: any World that replaces the one this session last reconciled
// (a hand Load while hosting swaps the App's World) is repaired before it ticks.
func TestRepairRunsWhenTheWorldIsReplaced(t *testing.T) {
	srv := serverOver(t, t.TempDir())
	enrollDirect(t, srv, repairGuestFP, "kip")
	app, err := srv.newGuestApp(repairGuestFP)
	if err != nil {
		t.Fatal(err)
	}
	rm, ok := srv.withReporting(app, repairGuestFP).(reportingModel)
	if !ok {
		t.Fatal("wrapper is not a reportingModel")
	}
	rec := staleFuseRecord(t)
	rec.OwnerDirty, rec.OwnerCopy, rec.GuestDirty = false, nil, true
	srv.dock.SeedFull([]relay.DockSnapshot{rec}, nil)

	loaded, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	loaded.AdoptCraft(guestLander(loaded), false)
	app2, err := tui.NewWithWorld(loaded)
	if err != nil {
		t.Fatal(err)
	}
	app2.SetGuestPersistence(func(*sim.World) error { return nil })
	rm.app = app2
	rm.refreshSession(time.Now())
	if _, _, ok := loaded.CraftByID(repairXID); ok {
		t.Errorf("a replaced World was not repaired before its first reconcile")
	}
}

// Call site 2 and the new order: the grant is flushed before the absent
// owner's payload is touched. A payload write that fails no longer undoes or
// refuses the reclaim; the stale payload keeps the stack and is relieved of it
// by the repair on its next open.
func TestReclaimIsLedgerFirst(t *testing.T) {
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, repairOwnerFP, "gern")
	rec := parkedStackFor(t, srv, repairOwnerFP, repairGuestFP)
	if err := srv.persistDocks(); err != nil {
		t.Fatal(err)
	}
	players := filepath.Join(dir, "players")
	if err := os.Chmod(players, 0o500); err != nil { // SavePlayer cannot rename into it
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(players, 0o755) })

	if err := srv.reclaimFromEmptySeat(rec, repairGuestFP); err != nil {
		t.Fatalf("a failed payload write refused the reclaim after the ledger had it: %v", err)
	}
	_ = os.Chmod(players, 0o755)
	restarted := serverOver(t, dir)
	got := snapshotByID(t, restarted, rec.ID)
	if got.TransferPayload == nil || got.FromCompositeID != rec.CompositeID {
		t.Fatalf("the grant did not reach session.json before the payload write: %+v", got)
	}
	stale, err := restarted.store.LoadPlayer(repairOwnerFP)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := stale.CraftByID(rec.CompositeID); !ok {
		t.Fatalf("test setup: the owner payload should still hold the stack")
	}
	if !restarted.dock.Repair(stale, repairOwnerFP) {
		t.Errorf("Repair did not relieve the stale payload of the stack")
	}
	if _, _, ok := stale.CraftByID(rec.CompositeID); ok {
		t.Errorf("the stack is in the owner's payload and on the ledger: two pilots")
	}
}

// 7.3 row 2 then [J]: the owner's payload predates the fuse. The reclaim
// repairs it first, so the stack that migrates carries the guest's Vessel.
func TestReclaimRepairsStaleOwnerPayloadFirst(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, repairOwnerFP, "gern")
	owner, err := sim.NewWorld() // lead only
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.SavePlayer(repairOwnerFP, owner); err != nil {
		t.Fatal(err)
	}
	snap := staleFuseRecord(t)
	srv.dock.SeedFull([]relay.DockSnapshot{snap}, nil)

	rec, why := srv.dock.ReclaimTarget(repairGuestFP, func(string) bool { return false })
	if why != "" {
		t.Fatalf("test setup: %s", why)
	}
	if err := srv.reclaimFromEmptySeat(rec, repairGuestFP); err != nil {
		t.Fatalf("reclaim refused: %v", err)
	}
	got := snapshotByID(t, srv, snap.ID)
	if !carriesPart(got.TransferPayload, repairXID) {
		t.Errorf("the stack that migrated lacks the guest's Vessel (%+v): it exists nowhere", got.TransferPayload)
	}
	after, err := srv.store.LoadPlayer(repairOwnerFP)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Crafts) != 0 {
		t.Errorf("the owner's payload still holds %d Vessels after the stack left", len(after.Crafts))
	}
	if got.FromCompositeID != 0 {
		t.Errorf("FromCompositeID = %d after the owner's payload was saved without the stack, want it acknowledged", got.FromCompositeID)
	}
}

// The owner has no payload at all (never saved, or a program reset): pressing
// [J] gets the guest their Vessel back as a Parcel instead of "nothing to take".
func TestReclaimAgainstOwnerWithNoSaveReturnsParcel(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, repairOwnerFP, "gern")
	snap := staleFuseRecord(t)
	srv.dock.SeedFull([]relay.DockSnapshot{snap}, nil)
	rec, why := srv.dock.ReclaimTarget(repairGuestFP, func(string) bool { return false })
	if why != "" {
		t.Fatalf("test setup: %s", why)
	}
	if err := srv.reclaimFromEmptySeat(rec, repairGuestFP); err != nil {
		t.Fatalf("reclaim refused: %v", err)
	}
	got := snapshotByID(t, srv, snap.ID)
	if got.ReturnPayload == nil || got.ReturnPayload.ID != repairXID || !got.Parcel {
		t.Fatalf("the guest's Vessel is not on its way back as a Parcel: %+v", got)
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.Crafts = nil
	chips := srv.dock.Reconcile(w, repairGuestFP, nil)
	if _, _, ok := w.CraftByID(repairXID); !ok {
		t.Errorf("the guest does not have their Vessel after the Parcel: %d crafts", len(w.Crafts))
	}
	var told bool
	for _, c := range chips {
		told = told || c.Kind == sim.SessionEventParcelReturned
	}
	if !told {
		t.Errorf("the Parcel arrived without its chip: %+v", chips)
	}
}

// Call site 3: purging the owner whose payload predates the fuse hands the
// friend their own Vessel back, not the owner's lead.
func TestPurgeRepairsStaleOwnerPayloadFirst(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, repairOwnerFP, "gern")
	owner, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.SavePlayer(repairOwnerFP, owner); err != nil {
		t.Fatal(err)
	}
	snap := staleFuseRecord(t)
	srv.dock.SeedFull([]relay.DockSnapshot{snap}, nil)

	if done := srv.finishPurge(repairOwnerFP, "gern", nil); done.err != nil {
		t.Fatal(done.err)
	}
	var friend relay.DockSnapshot
	for _, r := range srv.dock.FullRecords() {
		if r.ID == snap.ID {
			friend = r
		}
	}
	if friend.ReturnPayload == nil || friend.ReturnPayload.ID != repairXID {
		t.Errorf("the friend's Vessel is not on its way back: %+v", friend)
	}
	if friend.TransferPayload != nil {
		t.Errorf("the owner's lead was handed to the friend instead of their own Vessel: %+v", friend.TransferPayload)
	}
}
