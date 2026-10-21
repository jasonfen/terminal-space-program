package relay

import (
	"reflect"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0040 amendment (10a, slice A1): per-seat dirty marks, delivered copies
// and settled records. Every test reads the ledger through FullRecords, the
// view the session directory persists, so the persisted shape is under test
// too.

// fusedPair runs claim, handover and fuse and returns the ledger with one
// Active record, both Worlds and the report map.
func fusedPair(t *testing.T, guestID uint64) (*DockLedger, *sim.World, *sim.World, map[string]CraftReport) {
	t.Helper()
	store := NewStore()
	ledger := NewDockLedger()
	wA, wB := alignedPair(t, guestID)
	dockerID := wA.ActiveCraft().ID
	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); !ok {
		t.Fatalf("test setup: Claim refused")
	}
	reports := reportMap(store, wA, wB, time.Now())
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	return ledger, wA, wB, reports
}

// only returns the single snapshot in the ledger or fails.
func only(t *testing.T, l *DockLedger) DockSnapshot {
	t.Helper()
	recs := l.FullRecords()
	if len(recs) != 1 {
		t.Fatalf("ledger holds %d records, want 1: %+v", len(recs), recs)
	}
	return recs[0]
}

// ackAll acknowledges both seats with the Worlds as they stand.
func ackAll(l *DockLedger, wA, wB *sim.World) {
	l.AckPersisted(fpA, wA)
	l.AckPersisted(fpB, wB)
}

func TestHandoverMarksGuestDirty(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	wA, wB := alignedPair(t, 300)
	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", 300)
	ledger.Reconcile(wB, fpB, reportMap(store, wA, wB, time.Now()))

	s := only(t, ledger)
	if !s.GuestDirty || s.OwnerDirty || s.OwnerCopy != nil || s.GuestCopy != nil {
		t.Errorf("after handover want only GuestDirty, got %+v", s)
	}
}

func TestFuseMarksOwnerDirtyAndKeepsGuestCopy(t *testing.T) {
	ledger, _, _, _ := fusedPair(t, 301)
	s := only(t, ledger)
	if !s.OwnerDirty || s.OwnerCopy == nil {
		t.Fatalf("after fuse want OwnerDirty and an OwnerCopy of the guest's Vessel, got %+v", s)
	}
	if s.OwnerCopy.ID != 301 {
		t.Errorf("OwnerCopy is craft %d, want the guest's 301", s.OwnerCopy.ID)
	}
}

func TestUndockAndReturnMarkSeats(t *testing.T) {
	ledger, wA, wB, reports := fusedPair(t, 302)
	ackAll(ledger, wA, wB)
	if s := only(t, ledger); s.OwnerDirty || s.GuestDirty || s.OwnerCopy != nil {
		t.Fatalf("test setup: ack left marks %+v", s)
	}
	ledger.RequestUndock(fpB, 302)
	ledger.Reconcile(wA, fpA, reports)
	if s := only(t, ledger); !s.OwnerDirty || s.GuestDirty {
		t.Errorf("after undock want only OwnerDirty, got %+v", s)
	}
	ledger.Reconcile(wB, fpB, reports)
	s := only(t, ledger)
	if !s.GuestDirty || s.GuestCopy == nil {
		t.Errorf("after return want GuestDirty and a GuestCopy of the restored Vessel, got %+v", s)
	}
	if s.Phase != DockCooldown || s.Done {
		t.Errorf("live handback should hold the re-arm latch, got phase %v done %v", s.Phase, s.Done)
	}
}

func TestTransferAndAdoptionMarks(t *testing.T) {
	ledger, wA, wB, reports := fusedPair(t, 303)
	ackAll(ledger, wA, wB)
	compositeID := only(t, ledger).CompositeID
	if ok, why := ledger.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer refused: %s", why)
	}
	ledger.Reconcile(wA, fpA, reports)
	s := only(t, ledger)
	if s.FromCompositeID != compositeID {
		t.Errorf("FromCompositeID = %d, want the sender's composite %d", s.FromCompositeID, compositeID)
	}
	if !s.GuestDirty || s.OwnerDirty || s.OwnerCopy != nil || s.GuestCopy != nil {
		t.Errorf("after transfer park want only GuestDirty (the old owner), got %+v", s)
	}
	ledger.Reconcile(wB, fpB, reports)
	s = only(t, ledger)
	if !s.OwnerDirty || s.OwnerCopy == nil || !s.GuestDirty {
		t.Errorf("after adoption want OwnerDirty + OwnerCopy and the sender still dirty, got %+v", s)
	}
	// The sender's save clears FromCompositeID; the recipient's drops the copy.
	ledger.AckPersisted(fpA, wA)
	if s = only(t, ledger); s.FromCompositeID != 0 || s.GuestDirty {
		t.Errorf("sender ack should clear FromCompositeID and its mark, got %+v", s)
	}
}

func TestReclaimGrantRecordsFromCompositeOnly(t *testing.T) {
	ledger, wA, wB, _ := fusedPair(t, 304)
	ackAll(ledger, wA, wB)
	rec := only(t, ledger)
	stack, _ := wA.RemoveCraftByID(rec.CompositeID)
	if !ledger.GrantReclaim(rec.ID, fpB, stack, 0) {
		t.Fatalf("GrantReclaim refused")
	}
	s := only(t, ledger)
	if s.FromCompositeID != rec.CompositeID {
		t.Errorf("FromCompositeID = %d, want %d", s.FromCompositeID, rec.CompositeID)
	}
	if s.OwnerDirty || s.GuestDirty || s.OwnerCopy != nil || s.GuestCopy != nil {
		t.Errorf("a reclaim edits the payload on disk and marks no seat, got %+v", s)
	}
}

// A Parcel used to delete the record on delivery. It must now linger,
// invisible, until BOTH seats have saved.
func TestParcelRecordLingersUntilGuestSaves(t *testing.T) {
	ledger, wA, wB, reports := fusedPair(t, 305)
	if ok, why := ledger.RequestRelease(fpA, func(string) bool { return false }); !ok {
		t.Fatalf("RequestRelease refused: %s", why)
	}
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)

	recs := ledger.FullRecords()
	if len(recs) != 1 || !recs[0].Done || !recs[0].GuestDirty {
		t.Fatalf("record deleted before the guest saved: %+v", recs)
	}
	ledger.AckPersisted(fpB, wB)
	if recs = ledger.FullRecords(); len(recs) != 1 {
		t.Fatalf("record gone after only the guest saved (the owner has not): %+v", recs)
	}
	ledger.AckPersisted(fpA, wA)
	if recs = ledger.FullRecords(); len(recs) != 0 {
		t.Fatalf("record survived both acknowledgements: %+v", recs)
	}
}

func TestCooldownClearSettlesInsteadOfDeleting(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	wA, wB, dockerID := cooledPair(t, ledger, store, 306)
	_ = dockerID

	wA.Clock.SimTime = wA.Clock.SimTime.Add(sim.ReArmCeiling + time.Hour)
	ledger.Reconcile(wA, fpA, reportMap(store, wA, wB, time.Now()))

	if n := ledger.RecordCount(); n != 0 {
		t.Fatalf("test setup: the latch did not clear (%d live records)", n)
	}
	recs := ledger.FullRecords()
	if len(recs) != 1 || !recs[0].Done {
		t.Fatalf("record gone before the acks: %+v", recs)
	}
	ledger.AckPersisted(fpA, wA)
	ledger.AckPersisted(fpB, wB)
	if recs = ledger.FullRecords(); len(recs) != 0 {
		t.Fatalf("settled record survived both acks: %+v", recs)
	}
}

// A settled record must be invisible to every verb. Not red on main (main
// deletes the record), so the first assertion pins that the settled state
// exists and the sabotage run (settled kept in records) proves the rest.
func TestSettledRecordsAreInvisibleToClaimAndIsGuest(t *testing.T) {
	ledger, wA, wB, reports := fusedPair(t, 307)
	dockerID := wA.ActiveCraft().ID
	ledger.RequestRelease(fpA, func(string) bool { return false })
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)
	if recs := ledger.FullRecords(); len(recs) != 1 || !recs[0].Done {
		t.Fatalf("test setup: no settled record: %+v", recs)
	}

	if ledger.IsGuest(fpB) {
		t.Errorf("IsGuest sees a settled record")
	}
	if _, ok := ledger.ActiveGuestDock(fpB); ok {
		t.Errorf("ActiveGuestDock sees a settled record")
	}
	if n := len(ledger.Records()); n != 0 {
		t.Errorf("Records lists %d settled records", n)
	}
	if ledger.ClaimRefused(fpA, dockerID, fpB, 307) {
		t.Errorf("ClaimRefused counts a settled record")
	}
	if ledger.RequestUndock(fpB, 307) {
		t.Errorf("RequestUndock found a settled record")
	}
	if ok, _ := ledger.RequestRelease(fpA, allOnline); ok {
		t.Errorf("RequestRelease found a settled record")
	}
	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", 307); !ok {
		t.Errorf("Claim refused by a settled record")
	}
}

func TestAckGuardKeepsCopyWhenCompositeMissing(t *testing.T) {
	ledger, wA, _, _ := fusedPair(t, 308)
	if s := only(t, ledger); s.OwnerCopy == nil {
		t.Fatalf("test setup: no owner copy after the fuse")
	}
	// A World written without the Composite (a stale one) is not evidence that
	// the owner's durable World agrees.
	stale := newWorld(t)
	stale.Crafts = nil
	if ledger.AckPersisted(fpA, stale) {
		t.Errorf("AckPersisted claimed a change for a World without the Composite")
	}
	if s := only(t, ledger); s.OwnerCopy == nil || !s.OwnerDirty {
		t.Fatalf("copy dropped though the Composite is missing: %+v", s)
	}
	if !ledger.AckPersisted(fpA, wA) {
		t.Errorf("AckPersisted reported no change for a World that holds the Composite")
	}
	if s := only(t, ledger); s.OwnerCopy != nil || s.OwnerDirty {
		t.Errorf("copy kept though the Composite is present: %+v", s)
	}
}

func TestPurgedGuestSettlesWithOwnerMark(t *testing.T) {
	ledger, wA, wB, reports := fusedPair(t, 309)
	ackAll(ledger, wA, wB)
	ledger.DropRecordsNaming(fpB)
	ledger.Reconcile(wA, fpA, reports)
	s := only(t, ledger)
	if !s.Done || !s.OwnerDirty || s.GuestDirty {
		t.Errorf("purged guest: want Done + OwnerDirty, got %+v", s)
	}
	ledger.AckPersisted(fpA, wA)
	if recs := ledger.FullRecords(); len(recs) != 0 {
		t.Errorf("owner ack should delete the settled record: %+v", recs)
	}
}

// The new fields survive FullRecords -> SeedFull, live and settled.
func TestAckStateRoundTrips(t *testing.T) {
	ledger, wA, wB, reports := fusedPair(t, 310)
	ackAll(ledger, wA, wB)
	ledger.RequestTransfer(fpA, allOnline)
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports) // adoption: OwnerCopy + FromCompositeID live
	live := only(t, ledger)
	if live.OwnerCopy == nil || live.FromCompositeID == 0 || !live.OwnerDirty {
		t.Fatalf("test setup: live record lacks ack state: %+v", live)
	}

	fresh := restart(t, ledger)
	got := only(t, fresh)
	if !reflect.DeepEqual(got, live) {
		t.Errorf("live record changed across a restart:\n got %+v\nwant %+v", got, live)
	}

	// A settled record: finish the dock via a Parcel in a second ledger.
	l2, a2, b2, r2 := fusedPair(t, 311)
	l2.RequestRelease(fpA, func(string) bool { return false })
	l2.Reconcile(a2, fpA, r2)
	l2.Reconcile(b2, fpB, r2)
	settled := only(t, l2)
	if !settled.Done {
		t.Fatalf("test setup: record not settled: %+v", settled)
	}
	f2 := restart(t, l2)
	if got = only(t, f2); !reflect.DeepEqual(got, settled) {
		t.Errorf("settled record changed across a restart:\n got %+v\nwant %+v", got, settled)
	}
	if f2.RecordCount() != 0 || f2.IsGuest(fpB) {
		t.Errorf("a seeded settled record leaked into the live map")
	}
	f2.AckPersisted(fpA, a2)
	f2.AckPersisted(fpB, b2)
	if n := len(f2.FullRecords()); n != 0 {
		t.Errorf("seeded settled record not deleted by acks: %d left", n)
	}
}
