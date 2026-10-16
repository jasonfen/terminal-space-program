package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0054 slice E: a purge ("kick and delete vessels") deletes the purged
// player's own vessels and nobody else's. These tests dock alice (A) and bob
// (B) for real, purge one of them from the ledger, and read the far end: who
// holds which vessel, and whether any record still names the purged player.

// dockedPair docks B's lander into A's stack (A owns, B rides) and returns
// the pieces. The lander makes the vessels distinguishable by mass.
type dockedPair struct {
	store      *Store
	ledger     *DockLedger
	wA, wB     *sim.World
	reports    map[string]CraftReport
	now        time.Time
	guestID    uint64
	dockerID   uint64
	massA      float64
	massB      float64
	stackParts int
}

func newDockedPair(t *testing.T) *dockedPair {
	t.Helper()
	d := &dockedPair{store: NewStore(), ledger: NewDockLedger(), guestID: 950, now: time.Now()}
	d.wA, d.wB = alignedPair(t, d.guestID)
	d.wB.ActiveCraft().Stages = spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID).Stages
	d.wB.ActiveCraft().SyncFields()
	d.dockerID = d.wA.ActiveCraft().ID
	d.massA, d.massB = d.wA.ActiveCraft().TotalMass(), d.wB.ActiveCraft().TotalMass()
	if d.massA == d.massB {
		t.Fatalf("test vehicles are indistinguishable by mass (%v)", d.massA)
	}
	d.ledger.Claim(fpA, "alice", d.dockerID, fpB, "bob", d.guestID)
	d.reports = reportMap(d.store, d.wA, d.wB, d.now)
	d.ledger.Reconcile(d.wB, fpB, d.reports)
	d.ledger.Reconcile(d.wA, fpA, d.reports)
	if len(d.wA.Crafts) != 1 || !sim.StackHasGuest(d.wA.Crafts[0]) {
		t.Fatalf("A did not fuse a cross-player stack")
	}
	d.stackParts = len(d.wA.Crafts[0].DockedComponents)
	return d
}

// namesAnyone reports every place a record still names fp or handle.
func namesAnyone(l *DockLedger, fp, handle string) []string {
	var out []string
	for _, r := range l.FullRecords() {
		if r.Owner == fp || r.GuestOwner == fp {
			out = append(out, "fingerprint on record")
		}
		if handle != "" && (r.OwnerHandle == handle || r.GuestHandle == handle) {
			out = append(out, "handle on record")
		}
	}
	return out
}

func TestForgetRemovesTheReport(t *testing.T) {
	store := NewStore()
	w := newWorld(t)
	NewReporter(store, fpB).Tick(w, time.Now())
	NewReporter(store, fpA).Tick(w, time.Now())
	if len(store.Snapshot("")) != 2 {
		t.Fatalf("setup: want two reports, got %d", len(store.Snapshot("")))
	}
	store.Forget(fpB)
	for _, r := range store.Snapshot("") {
		if r.Owner == fpB {
			t.Errorf("a report for the forgotten owner is still in the Snapshot")
		}
	}
	if len(store.Snapshot("")) != 1 {
		t.Errorf("Forget removed another owner's report too")
	}
	// A purged player who is invited back reports afresh.
	NewReporter(store, fpB).Tick(w, time.Now())
	if len(store.Snapshot("")) != 2 {
		t.Errorf("a forgotten owner could not report again")
	}
}

// Purging the GUEST: their vessel is in alice's stack, and becomes alice's
// for good. The record closes as a transfer, so alice's own [U] splits it as
// her own vessel (the local split, not a handback through the ledger).
func TestPurgingTheGuestLeavesTheirVesselWithTheStackOwner(t *testing.T) {
	d := newDockedPair(t)
	if why := d.wA.UndockRefusal(0); why == "" {
		t.Fatal("setup: a cross-player stack must refuse the local split before the purge")
	}

	d.ledger.DropRecordsNaming(fpB)
	if got := namesAnyone(d.ledger, fpB, "bob"); len(got) != 0 {
		t.Fatalf("a record still names the purged guest: %v", got)
	}
	d.ledger.Reconcile(d.wA, fpA, d.reports)

	if n := len(d.ledger.Records()); n != 0 {
		t.Errorf("ledger holds %d records after the owner reconciled, want the transfer closed", n)
	}
	if len(d.wA.Crafts) != 1 || len(d.wA.Crafts[0].DockedComponents) != d.stackParts {
		t.Fatalf("the purged guest's vessel left the stack: crafts=%d parts=%d want %d parts",
			len(d.wA.Crafts), len(d.wA.Crafts[0].DockedComponents), d.stackParts)
	}
	if sim.StackHasGuest(d.wA.Crafts[0]) {
		t.Error("the stack still carries a guest tag for a player who is gone")
	}
	if why := d.wA.UndockRefusal(0); why != "" {
		t.Fatalf("alice's [U] is still refused: %q", why)
	}
	if !d.wA.Undock(0) {
		t.Fatal("alice's local undock failed")
	}
	if len(d.wA.Crafts) != 2 {
		t.Fatalf("alice holds %d vessels after [U], want her own and the former guest's", len(d.wA.Crafts))
	}
	var sawGuestHull bool
	for _, c := range d.wA.Crafts {
		if c.TotalMass() > d.massB-1 && c.TotalMass() < d.massB+1 {
			sawGuestHull = true
		}
	}
	if !sawGuestHull {
		t.Errorf("none of alice's vessels has the former guest's mass %v", d.massB)
	}
}

// The owner is away when the guest is purged: the adoption waits on the
// record, through a restart, and lands when the owner next connects.
func TestPurgingTheGuestWhileTheOwnerIsAwayCompletesOnTheirReturn(t *testing.T) {
	d := newDockedPair(t)
	d.ledger.DropRecordsNaming(fpB)
	fresh := restart(t, d.ledger)
	if got := namesAnyone(fresh, fpB, "bob"); len(got) != 0 {
		t.Fatalf("restart brought back a record naming the purged guest: %v", got)
	}
	fresh.Reconcile(d.wA, fpA, d.reports)
	if sim.StackHasGuest(d.wA.Crafts[0]) || len(fresh.Records()) != 0 {
		t.Errorf("the adoption did not complete after a restart: guest=%v records=%d",
			sim.StackHasGuest(d.wA.Crafts[0]), len(fresh.Records()))
	}
}

// Purging the OWNER: the friend's vessel riding in their stack is handed back
// before the owner's save is deleted. Live friend: the ordinary handback.
func TestPurgingTheOwnerHandsTheFriendTheirVesselBack(t *testing.T) {
	d := newDockedPair(t)

	d.ledger.ReleaseGuestsOf(d.wA, fpA, allOnline)
	d.ledger.DropRecordsNaming(fpA)
	if got := namesAnyone(d.ledger, fpA, "alice"); len(got) != 0 {
		t.Fatalf("a record still names the purged owner: %v", got)
	}

	chips := d.ledger.Reconcile(d.wB, fpB, d.reports)
	got, _, ok := d.wB.CraftByID(d.guestID)
	if !ok {
		t.Fatalf("bob did not get his vessel back: slate=%d", len(d.wB.Crafts))
	}
	if m := got.TotalMass(); m != d.massB {
		t.Errorf("bob got a vessel of mass %v, want his own %v", m, d.massB)
	}
	if got.Throttle != 0 || got.ActiveBurn != nil {
		t.Errorf("the handed-back vessel is not safed")
	}
	if !hasChip(chips, sim.SessionEventParcelReturned) {
		t.Errorf("bob was handed a vessel with no explanation: %+v", chips)
	}
	if n := len(d.ledger.Records()); n != 0 {
		t.Errorf("ledger holds %d records after the handback, want none (no re-arm latch against a deleted vessel)", n)
	}
	if sim.StackHasGuest(d.wA.Crafts[0]) {
		t.Error("alice's stack still carries bob's vessel; her delete would take it")
	}
}

// The friend is away when the owner is purged: it arrives as a Parcel, after
// a restart, on their next connect.
func TestPurgingTheOwnerParcelsAnAbsentFriendsVessel(t *testing.T) {
	d := newDockedPair(t)
	gone := func(fp string) bool { return fp != fpB }

	d.ledger.ReleaseGuestsOf(d.wA, fpA, gone)
	d.ledger.DropRecordsNaming(fpA)
	fresh := restart(t, d.ledger)
	if got := namesAnyone(fresh, fpA, "alice"); len(got) != 0 {
		t.Fatalf("restart brought back a record naming the purged owner: %v", got)
	}

	wB2 := newWorld(t)
	wB2.Crafts = nil
	wB2.Clock.SimTime = d.wA.Clock.SimTime.Add(time.Hour)
	chips := fresh.Reconcile(wB2, fpB, d.reports)
	got, _, ok := wB2.CraftByID(d.guestID)
	if !ok {
		t.Fatalf("bob's parcel did not arrive")
	}
	if got.TotalMass() != d.massB {
		t.Errorf("parcel mass %v, want %v", got.TotalMass(), d.massB)
	}
	if !hasChip(chips, sim.SessionEventParcelReturned) {
		t.Errorf("parcel arrived unexplained: %+v", chips)
	}
	if len(fresh.Records()) != 0 {
		t.Errorf("record outlived the delivery")
	}
}

// After a control transfer the guest's parts sit UNDER the owner's, where a
// peel would swap hardware. Purging that owner cannot peel, so the whole
// stack goes to the friend: no friend's vessel is deleted.
func TestPurgingTheOwnerWhoCannotPeelGivesTheFriendTheStack(t *testing.T) {
	d := newDockedPair(t)
	if ok, _ := d.ledger.RequestTransfer(fpA, allOnline); !ok {
		t.Fatal("setup: transfer refused")
	}
	d.ledger.Reconcile(d.wA, fpA, d.reports)
	d.reports = reportMap(d.store, d.wA, d.wB, d.now.Add(time.Second))
	d.ledger.Reconcile(d.wB, fpB, d.reports) // bob owns; alice's parts are the guest tags
	if !sim.StackHasGuest(d.wB.Crafts[0]) || d.wB.GuestReleaseRefusal(0) == "" {
		t.Fatal("setup: expected bob to own a stack he cannot peel alice out of")
	}
	// Purge BOB, the owner: alice's parts are under his, so no peel.
	d.ledger.ReleaseGuestsOf(d.wB, fpB, allOnline)
	d.ledger.DropRecordsNaming(fpB)
	if got := namesAnyone(d.ledger, fpB, "bob"); len(got) != 0 {
		t.Fatalf("a record still names the purged owner: %v", got)
	}
	if len(d.wB.Crafts) != 0 {
		t.Fatalf("the stack stayed in the purged player's World (%d crafts); the delete would take alice's vessel", len(d.wB.Crafts))
	}
	d.reports = reportMap(d.store, d.wA, d.wB, d.now.Add(2*time.Second))
	d.ledger.Reconcile(d.wA, fpA, d.reports) // arrives and is adopted
	d.ledger.Reconcile(d.wA, fpA, d.reports)
	if len(d.wA.Crafts) != 1 {
		t.Fatalf("alice holds %d vessels, want the whole stack", len(d.wA.Crafts))
	}
	if sim.StackHasGuest(d.wA.Crafts[0]) {
		t.Error("the stack still carries guest tags")
	}
	if n := len(d.ledger.Records()); n != 0 {
		t.Errorf("ledger holds %d records, want none", n)
	}
}

// A stack in flight to the purged owner (control handed over, not yet
// adopted) carries the friend's parts: it goes back to the friend.
func TestPurgingAnOwnerWithAStackInTransitReturnsItToTheFriend(t *testing.T) {
	d := newDockedPair(t)
	if ok, _ := d.ledger.RequestTransfer(fpA, allOnline); !ok {
		t.Fatal("setup: transfer refused")
	}
	d.ledger.Reconcile(d.wA, fpA, d.reports) // stack parked for bob, not yet delivered
	if len(d.wA.Crafts) != 0 {
		t.Fatalf("setup: the stack did not leave alice's World")
	}
	d.ledger.DropRecordsNaming(fpB) // bob is purged: the stack was heading to him
	if got := namesAnyone(d.ledger, fpB, "bob"); len(got) != 0 {
		t.Fatalf("a record still names the purged owner: %v", got)
	}
	d.ledger.Reconcile(d.wA, fpA, d.reports)
	d.ledger.Reconcile(d.wA, fpA, d.reports)
	if len(d.wA.Crafts) != 1 {
		t.Fatalf("alice holds %d vessels, want the stack back", len(d.wA.Crafts))
	}
	if sim.StackHasGuest(d.wA.Crafts[0]) || len(d.ledger.Records()) != 0 {
		t.Errorf("returned stack not settled: guest=%v records=%d", sim.StackHasGuest(d.wA.Crafts[0]), len(d.ledger.Records()))
	}
}

// A claim not yet fused: the purged docker's claim ends; a friend's craft
// already parked on the record comes back to them.
func TestPurgingADockerMidClaimReturnsTheFriendsParkedCraft(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 960
	wA, wB := alignedPair(t, guestID)
	now := time.Now()
	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports) // bob's craft is now parked on the record
	if len(wB.Crafts) != 0 {
		t.Fatalf("setup: bob's craft did not park")
	}

	ledger.DropRecordsNaming(fpA)
	if got := namesAnyone(ledger, fpA, "alice"); len(got) != 0 {
		t.Fatalf("record names the purged docker: %v", got)
	}
	ledger.Reconcile(wB, fpB, reports)
	if _, _, ok := wB.CraftByID(guestID); !ok {
		t.Fatalf("bob's parked craft was deleted with the purged docker's claim")
	}
	if len(ledger.Records()) != 0 {
		t.Errorf("record survived")
	}
}

// Cooldown latches, claims with nothing parked, and a purged guest's pending
// claim go quietly.
func TestPurgingEndsCooldownLatchesAndBareClaims(t *testing.T) {
	ledger := NewDockLedger()
	ledger.Seed([]DockRecord{
		{ID: 1, Owner: fpA, OwnerHandle: "alice", DockerCraftID: 1, GuestOwner: fpB, GuestHandle: "bob", GuestCraftID: 2, Phase: DockCooldown},
		{ID: 2, Owner: fpA, OwnerHandle: "alice", DockerCraftID: 3, GuestOwner: fpB, GuestHandle: "bob", GuestCraftID: 4, Phase: DockPending},
	})
	ledger.DropRecordsNaming(fpB)
	if n := len(ledger.Records()); n != 0 {
		t.Errorf("%d records survive the purge of their guest", n)
	}
}
