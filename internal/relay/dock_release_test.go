package relay

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestOwnerReleasesAnAbsentGuestAsAParcel is #312, as it stranded a player on
// 2026-08-02: the guest disconnected while docked and the composite had no
// release path from either seat — the docker could not split it (correctly),
// and the guest could not ask for it because they were gone. Recovery took a
// server-side --reset-fleet.
//
// The rule is now one a pilot can fly by: you can always get your own ship
// back to yourself. [U] from the owner seat works whether or not the guest is
// there; an absent guest's component becomes a Parcel — safed, placed across
// the subspace gap, and delivered with an explanation on their next connect.
func TestOwnerReleasesAnAbsentGuestAsAParcel(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 900
	wA, wB := alignedPair(t, guestID)
	// A distinct vehicle, so "B got its own ship back" is checkable by mass.
	wB.ActiveCraft().Stages = spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID).Stages
	wB.ActiveCraft().SyncFields()
	guestMass := wB.ActiveCraft().TotalMass()
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 1 || !sim.StackHasGuest(wA.Crafts[0]) {
		t.Fatalf("A did not fuse a cross-player stack")
	}

	// B drops. A releases from the owner seat.
	gone := func(fp string) bool { return fp != fpB }
	if ok, why := ledger.RequestRelease(fpA, gone); !ok {
		t.Fatalf("owner-seat release refused with a live composite: %s", why)
	}
	chips := ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 1 || sim.StackHasGuest(wA.Crafts[0]) {
		t.Fatalf("A is still stuck holding a cross-player stack: %+v", wA.Crafts)
	}
	if !hasChip(chips, sim.SessionEventUndocked) {
		t.Errorf("the release said nothing on the owner's seat: %+v", chips)
	}
	stackR := wA.Crafts[0].State.R

	// The Parcel outlives a restart — that is what makes "park it for later"
	// a real answer rather than a new way to lose the craft.
	fresh := restart(t, ledger)

	// B connects an hour of sim-time later.
	wBReconnect := newWorld(t)
	wBReconnect.Crafts = nil
	wBReconnect.Clock.SimTime = wA.Clock.SimTime.Add(time.Hour)
	reports = reportMap(store, wA, wBReconnect, now.Add(time.Hour))
	gChips := fresh.Reconcile(wBReconnect, fpB, reports)

	got, _, ok := wBReconnect.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not get craft %d back: slate=%d", guestID, len(wBReconnect.Crafts))
	}
	if got.TotalMass() != guestMass {
		t.Errorf("B got a vehicle of mass %v, want its own %v", got.TotalMass(), guestMass)
	}
	if !hasChip(gChips, sim.SessionEventParcelReturned) {
		t.Errorf("the Parcel arrived with no explanation: %+v", gChips)
	}
	// Safed: nothing can fire or swing on its own metres from a stack.
	if got.Throttle != 0 {
		t.Errorf("returned craft throttle = %v, want 0", got.Throttle)
	}
	if got.EngineMode != spacecraft.EngineMain {
		t.Errorf("returned craft engine mode = %v, want main", got.EngineMode)
	}
	if got.AttitudeMode != spacecraft.BurnPrograde {
		t.Errorf("returned craft attitude hold = %v, want the neutral default", got.AttitudeMode)
	}
	if got.ActiveBurn != nil || got.ManualBurn != nil {
		t.Errorf("returned craft arrived with a burn running")
	}
	// Placed, not frozen: an hour of coasting separates it from where the
	// stack was when the owner let go.
	if got.State.R == stackR {
		t.Errorf("returned craft materialised at the stack's hour-old position %v", stackR)
	}
	if got.State.R.Sub(stackR).Norm() < sim.DockingDistM {
		t.Errorf("returned craft is inside the docking gate of the stack it left (%v m)", got.State.R.Sub(stackR).Norm())
	}
	// The dock is over.
	if len(fresh.Records()) != 0 {
		t.Errorf("ledger still holds %d records after the Parcel landed", len(fresh.Records()))
	}
}

// TestOwnerReleaseWithTheGuestPresentIsTheNormalUndock: decision 3's first
// branch — a live guest gets the ordinary both-seats-chipped handback, not a
// Parcel. The owner-seat key is a second door onto the same release, not a
// second kind of release.
func TestOwnerReleaseWithTheGuestPresentIsTheNormalUndock(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 910
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	stackR := wA.Crafts[0].State.R

	if ok, why := ledger.RequestRelease(fpA, allOnline); !ok {
		t.Fatalf("owner-seat release refused: %s", why)
	}
	ledger.Reconcile(wA, fpA, reports)
	gChips := ledger.Reconcile(wB, fpB, reports)

	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not get craft %d back", guestID)
	}
	if !hasChip(gChips, sim.SessionEventUndocked) {
		t.Errorf("a live release chipped no undock: %+v", gChips)
	}
	if hasChip(gChips, sim.SessionEventParcelReturned) {
		t.Errorf("a live release arrived as a Parcel: %+v", gChips)
	}
	if got.State.R != stackR {
		t.Errorf("live handback state %v != the seam %v", got.State.R, stackR)
	}
}

// TestOwnerReleaseRefusesTheBottomPeelAndNamesTheWayOut (ADR 0040 §5,
// re-affirming #314): after a control transfer the other party's components
// sit at the BOTTOM of the stack. Peeling the tail there hands each player the
// other's hardware, so the release refuses — and says which key gets them out.
func TestOwnerReleaseRefusesTheBottomPeelAndNamesTheWayOut(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 920
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
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wB, fpB, reports) // B now owns; A's components are at the bottom

	why := wB.GuestReleaseRefusal(wB.ActiveCraftIdx)
	if why == "" {
		t.Fatalf("the bottom peel was allowed — each player would get the other's vehicle")
	}
	if !strings.Contains(why, "[J]") {
		t.Errorf("refusal %q does not name the way out", why)
	}
}
