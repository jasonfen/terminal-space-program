package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const (
	fpA = "SHA256:alice"
	fpB = "SHA256:bob"
)

// reportMap ticks both Worlds into the store and returns the owner→report
// map the dock reconcile reads (for the guest's warp coupling).
func reportMap(store *Store, wA, wB *sim.World, now time.Time) map[string]CraftReport {
	NewReporter(store, fpA).Tick(wA, now)
	NewReporter(store, fpB).Tick(wB, now)
	out := map[string]CraftReport{}
	for _, r := range store.Snapshot("") {
		out[r.Owner] = r
	}
	return out
}

// alignedPair builds two Worlds in the same subspace with co-located,
// velocity-matched active craft (so contact detection and co-warp gate), and
// stamps a distinct guest craft ID.
func alignedPair(t *testing.T, guestID uint64) (*sim.World, *sim.World) {
	t.Helper()
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime
	wB.ActiveCraft().ID = guestID
	wB.ActiveCraft().State = wA.ActiveCraft().State // range 0, v_rel 0
	return wA, wB
}

// TestCrossPlayerDockHandshakeAndUndock is the master two-World test: A claims
// a dock on B's craft; B hands it over (docked-as-guest); A fuses one stack it
// owns; the guest is min-wins coupled to the owner and can't out-warp it; B
// undocks any-time and gets its craft back live at the matching seam.
func TestCrossPlayerDockHandshakeAndUndock(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 200
	wA, wB := alignedPair(t, guestID)
	dockerID := wA.ActiveCraft().ID
	now := time.Now()

	// A claims (as the detector would once co-warp coupled + within gates).
	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); !ok {
		t.Fatalf("Claim refused")
	}

	// Guest tick: B hands its craft over and goes docked-as-guest.
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	if wB.DockGuest == nil || wB.DockGuest.OwnerFP != fpA {
		t.Fatalf("B not docked-as-guest: %+v", wB.DockGuest)
	}
	if len(wB.Crafts) != 0 {
		t.Fatalf("B still holds %d craft after handover, want 0", len(wB.Crafts))
	}

	// Owner tick: A fuses the guest into one stack it owns.
	chips := ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 1 || !sim.StackHasGuest(wA.Crafts[0]) {
		t.Fatalf("A did not fuse a cross-player stack: crafts=%d", len(wA.Crafts))
	}
	if wA.Crafts[0].ID != dockerID {
		t.Errorf("stack identity = %d, want docker %d (docker owns)", wA.Crafts[0].ID, dockerID)
	}
	if !hasChip(chips, sim.SessionEventDocked) {
		t.Errorf("no docked chip: %+v", chips)
	}

	// Guest coupling min-wins: A picks 1×; B (docked-as-guest) can't out-warp it.
	wA.Clock.WarpIdx = 0 // owner 1×
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wB, fpB, reports)
	// The serve layer folds the coupling after ComputeCoWarp; do it here.
	wB.CoWarp = wB.CoWarp.WithDockCoupling(wB.DockGuest.OwnerHandle, wB.DockGuest.OwnerEffWarp)
	wB.Clock.WarpIdx = 5 // B wants max warp
	if got := wB.EffectiveWarp(); got != 1 {
		t.Errorf("docked-as-guest B EffectiveWarp = %v, want clamped to owner 1×", got)
	}

	stack := wA.Crafts[0]
	stackR, stackV := stack.State.R, stack.State.V

	// Undock any-time: B requests, A splits + returns, B receives home.
	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	reports = reportMap(store, wA, wB, now.Add(2*time.Second))
	ledger.Reconcile(wA, fpA, reports) // A splits the guest out
	uChips := ledger.Reconcile(wB, fpB, reports) // B receives its craft

	if wB.DockGuest != nil {
		t.Errorf("B still docked-as-guest after undock")
	}
	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not get craft %d back", guestID)
	}
	if got.State.R != stackR || got.State.V != stackV {
		t.Errorf("returned craft state %v/%v != stack seam %v/%v", got.State.R, got.State.V, stackR, stackV)
	}
	if !hasChip(uChips, sim.SessionEventUndocked) {
		t.Errorf("no undocked chip: %+v", uChips)
	}
	// A's stack reverted to its plain docker craft.
	if len(wA.Crafts) != 1 || sim.StackHasGuest(wA.Crafts[0]) {
		t.Errorf("A's composite did not revert after undock")
	}
	// The dock is fully torn down.
	if len(ledger.Records()) != 0 {
		t.Errorf("ledger still holds %d records after undock", len(ledger.Records()))
	}
}

// TestGuestVesselSwitchRetainsCoupling: a guest flying ANOTHER craft they own
// stays docked-as-guest and coupled — vessel-switch doesn't drop the ride.
func TestGuestVesselSwitchRetainsCoupling(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 300
	wA, wB := alignedPair(t, guestID)
	// Give B a second craft to fly while its first rides in A's stack.
	second := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	second.Primary = wB.ActiveCraft().Primary
	second.State = wB.ActiveCraft().State
	wB.AdoptCraft(second, false)
	secondID := second.ID
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports) // hand over craft 1
	ledger.Reconcile(wA, fpA, reports) // fuse

	// B flies its second craft.
	if _, idx, ok := wB.CraftByID(secondID); ok {
		wB.SetActiveCraftIdx(idx)
	} else {
		t.Fatalf("B lost its second craft on handover")
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wB, fpB, reports)
	if wB.DockGuest == nil {
		t.Errorf("guest dropped its docked-as-guest state after vessel switch")
	}
	if wB.ActiveCraft() == nil || wB.ActiveCraft().ID != secondID {
		t.Errorf("guest not flying its second craft")
	}
}

// TestTransferControlSwapsRolesRefusedMidBurn: a mid-burn stack refuses the
// transfer; once the burn ends the whole stack migrates to the guest and the
// roles swap (old owner becomes the guest).
func TestTransferControlSwapsRolesRefusedMidBurn(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 400
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	stack := wA.Crafts[0]

	// Mid-burn: transfer is refused (stack stays with A).
	stack.ManualBurn = &spacecraft.ManualBurn{StartTime: wA.Clock.SimTime}
	if !ledger.RequestTransfer(fpA) {
		t.Fatalf("RequestTransfer refused outright")
	}
	ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 1 || !sim.StackHasGuest(wA.Crafts[0]) {
		t.Fatalf("stack migrated while mid-burn (should be refused)")
	}

	// Burn ends: the transfer goes through, roles swap.
	stack.ManualBurn = nil
	chips := ledger.Reconcile(wA, fpA, reports) // A migrates the stack out
	if len(wA.Crafts) != 0 {
		t.Errorf("A still holds the stack after transfer: %d", len(wA.Crafts))
	}
	if !hasChip(chips, sim.SessionEventTransfer) {
		t.Errorf("no transfer chip: %+v", chips)
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wB, fpB, reports) // B adopts the stack (now owner)
	if len(wB.Crafts) != 1 || !sim.StackHasGuest(wB.Crafts[0]) {
		t.Fatalf("B did not adopt the transferred stack")
	}
	if wB.DockGuest != nil {
		t.Errorf("new owner B still marked docked-as-guest")
	}
	ledger.Reconcile(wA, fpA, reports) // A becomes the guest
	if wA.DockGuest == nil || wA.DockGuest.OwnerFP != fpB {
		t.Errorf("old owner A not demoted to guest: %+v", wA.DockGuest)
	}
}

// TestDisconnectReconnectResumesDockedAsGuest: the durable record survives a
// server restart (Seed) and a reconnecting guest resumes docked-as-guest,
// coupled to the stack — the craft rode along in the owner's stack.
func TestDisconnectReconnectResumesDockedAsGuest(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 500
	wA, wB := alignedPair(t, guestID)
	now := time.Now()
	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)

	// Persisted cross-ref (durable subset only).
	durable := ledger.Records()
	if len(durable) != 1 || durable[0].Phase != DockActive {
		t.Fatalf("durable record = %+v", durable)
	}

	// Server restart: fresh ledger seeded from disk; B reconnects with NO
	// craft for guestID (it rode along in A's stack, absent from B's payload).
	fresh := NewDockLedger()
	fresh.Seed(durable)
	wBReconnect := newWorld(t)
	wBReconnect.Crafts = nil
	wBReconnect.Clock.SimTime = wA.Clock.SimTime
	reports = reportMap(store, wA, wBReconnect, now.Add(time.Minute))
	fresh.Reconcile(wBReconnect, fpB, reports)
	if wBReconnect.DockGuest == nil || wBReconnect.DockGuest.OwnerFP != fpA {
		t.Errorf("reconnecting guest did not resume docked-as-guest: %+v", wBReconnect.DockGuest)
	}
}

func hasChip(chips []DockChip, kind sim.SessionEventKind) bool {
	for _, c := range chips {
		if c.Kind == kind {
			return true
		}
	}
	return false
}
