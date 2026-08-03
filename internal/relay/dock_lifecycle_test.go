package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0038 S1/S2/S3 (#301, #303, #304): the lifecycle around the dock/undock
// fuse, generalising ADR 0040's Parcel-only safe-handback and subspace-gap
// helpers to the live cross-player path, plus the re-arm-by-leaving latch.

// TestFuseChipsTheAbsorbedGuestToo is #301, live: the docker's own reconcile
// already got a "docked" chip when the stack fused (TestCrossPlayerDockHandshakeAndUndock
// pins that). What was missing is the ABSORBED party's own moment — measured
// on the 2026-08-02 session as a total UI blank with no chip of any kind on
// the passive seat. The guest's own next reconcile, once the fuse has
// happened on the owner's side, must carry its own "docked" chip.
func TestFuseChipsTheAbsorbedGuestToo(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1001
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	if _, ok := ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID); !ok {
		t.Fatalf("Claim refused")
	}
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports) // B hands its craft over — no chip yet, nothing happened to B yet

	ownerChips := ledger.Reconcile(wA, fpA, reports) // A fuses the stack
	if !hasChip(ownerChips, sim.SessionEventDocked) {
		t.Fatalf("owner got no docked chip: %+v", ownerChips)
	}

	// B's own next reconcile must carry the moment that just happened to it:
	// its craft joined someone else's stack.
	guestChips := ledger.Reconcile(wB, fpB, reports)
	if !hasChip(guestChips, sim.SessionEventDocked) {
		t.Errorf("the absorbed guest got no docked chip at fuse (#301): %+v", guestChips)
	}
}

// TestFuseNoticeSurvivesRestart (#301, durability): the absorbed guest's
// pending "docked" chip is owed at fuse, but the guest might not tick again
// before a restart. DockNotice has to round-trip through the session
// directory the same way every other in-flight flag on the record does
// (ADR 0040 §1), or a restart in that narrow window drops the guest's own
// moment silently — the exact kind of silence #301 was about.
func TestFuseNoticeSurvivesRestart(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1007
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports) // B hands its craft over
	ledger.Reconcile(wA, fpA, reports) // A fuses — dockNotice set for B

	// The server restarts before B's own next tick ever consumes the notice.
	fresh := restart(t, ledger)

	guestChips := fresh.Reconcile(wB, fpB, reports)
	if !hasChip(guestChips, sim.SessionEventDocked) {
		t.Errorf("the absorbed guest's fuse notice did not survive a restart: %+v", guestChips)
	}
}
