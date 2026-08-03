package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// loadedSystems is the body catalog a restored dock payload rehydrates
// against — the session directory's restore path hands the same slice in.
func loadedSystems(t *testing.T) []bodies.System {
	t.Helper()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("bodies.LoadAll: %v", err)
	}
	return systems
}

// restart simulates the server going down and coming back: the ledger's
// full state is written to the session directory and a fresh ledger is
// seeded from it. Nothing else about the two Worlds changes — a restart
// does not fly anyone's craft.
func restart(t *testing.T, l *DockLedger) *DockLedger {
	t.Helper()
	fresh := NewDockLedger()
	fresh.SeedFull(l.FullRecords(), loadedSystems(t))
	return fresh
}

// TestTransferMidHandoverSurvivesRestart is #311, exactly as measured on the
// production host on 2026-08-02: [J] is pressed while the recipient is
// offline, so the composite leaves the sender's World and parks on the dock
// record awaiting the recipient's tick — and then the server restarts (the
// hourly auto-adopt of a release tag). Pre-fix the parked payload was
// transient: the composite came back in NO player's World and no save, while
// the record that named it outlived it. Delivery must instead complete on the
// recipient's next connect, exactly as if the restart had not happened.
func TestTransferMidHandoverSurvivesRestart(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 700
	wA, wB := alignedPair(t, guestID)
	dockerID := wA.ActiveCraft().ID
	now := time.Now()

	// Dock: A claims, B hands over, A fuses one cross-player stack.
	ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 1 || !sim.StackHasGuest(wA.Crafts[0]) {
		t.Fatalf("A did not fuse a cross-player stack: %d craft", len(wA.Crafts))
	}
	stackMass := wA.Crafts[0].TotalMass()
	stackR := wA.Crafts[0].State.R

	// [J] with B offline: A's tick migrates the stack out. It now exists
	// ONLY on the ledger record — B's session is not running to receive it.
	if ok, _ := ledger.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer refused")
	}
	ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 0 {
		t.Fatalf("A still holds %d craft after handing control over", len(wA.Crafts))
	}

	// The server restarts under the parked handover.
	fresh := restart(t, ledger)

	// B connects. Its payload holds no craft (its own rode along in the
	// stack, and the stack was mid-migration), so delivery is the only way
	// the composite can exist anywhere.
	wBReconnect := newWorld(t)
	wBReconnect.Crafts = nil
	wBReconnect.Clock.SimTime = wA.Clock.SimTime
	reports = reportMap(store, wA, wBReconnect, now.Add(time.Hour))
	chips := fresh.Reconcile(wBReconnect, fpB, reports)

	if len(wBReconnect.Crafts) != 1 {
		t.Fatalf("the composite was destroyed by the restart: B holds %d craft, want the delivered stack", len(wBReconnect.Crafts))
	}
	got := wBReconnect.Crafts[0]
	if !sim.StackHasGuest(got) {
		t.Errorf("delivered craft is not the cross-player stack: %+v", got.DockedComponents)
	}
	if got.TotalMass() != stackMass {
		t.Errorf("delivered stack mass = %v, want the pre-restart stack's %v", got.TotalMass(), stackMass)
	}
	if got.State.R != stackR {
		t.Errorf("delivered stack position = %v, want %v", got.State.R, stackR)
	}
	if !hasChip(chips, sim.SessionEventTransfer) {
		t.Errorf("delivery produced no moment for B: %+v", chips)
	}
	// And the record is still the live cross-ref for the dock, now owned by B.
	recs := fresh.Records()
	if len(recs) != 1 || recs[0].Owner != fpB {
		t.Errorf("post-delivery records = %+v, want one owned by B", recs)
	}
}

// TestParkedPayloadIsNotReapedAsPhantom: the #309 reaper ends a dock whose
// composite resolves in nobody's World. A restored record holding an
// undelivered payload looks exactly like that from the owner's seat — and
// reaping it would destroy the very craft durability exists to protect. A
// record holding a parked Parcel is healthy, not phantom (ADR 0040 §1).
func TestParkedPayloadIsNotReapedAsPhantom(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 710
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if ok, _ := ledger.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer refused")
	}
	ledger.Reconcile(wA, fpA, reports)

	fresh := restart(t, ledger)

	// A (now the guest) reconnects first and ticks for a while before B ever
	// does. Nothing may reap the record out from under the parked stack.
	wAReconnect := newWorld(t)
	wAReconnect.Crafts = nil
	wAReconnect.Clock.SimTime = wA.Clock.SimTime
	for i := 0; i < 3; i++ {
		reports = reportMap(store, wAReconnect, wB, now.Add(time.Duration(i)*time.Second))
		fresh.Reconcile(wAReconnect, fpA, reports)
	}
	if len(fresh.Records()) != 1 {
		t.Fatalf("the parked handover was reaped as a phantom: %+v", fresh.Records())
	}

	wBReconnect := newWorld(t)
	wBReconnect.Crafts = nil
	wBReconnect.Clock.SimTime = wA.Clock.SimTime
	reports = reportMap(store, wAReconnect, wBReconnect, now.Add(time.Minute))
	fresh.Reconcile(wBReconnect, fpB, reports)
	if len(wBReconnect.Crafts) != 1 {
		t.Errorf("B holds %d craft after delivery, want the stack", len(wBReconnect.Crafts))
	}
}

// TestRequestFlagsSurviveRestart: an undock ask raised while the owner is
// offline must still be waiting for them when they come back — the flags are
// as in-flight as the payloads, and dropping one silently swallows a keypress.
func TestRequestFlagsSurviveRestart(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 720
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}

	fresh := restart(t, ledger)
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	fresh.Reconcile(wA, fpA, reports) // the owner acts on the restored ask
	fresh.Reconcile(wB, fpB, reports)

	if _, _, ok := wB.CraftByID(guestID); !ok {
		t.Errorf("the undock ask was lost across the restart — B never got craft %d back", guestID)
	}
}
