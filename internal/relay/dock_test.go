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

// allOnline is the presence predicate for tests that are not about the
// presence gate: both players are in the session.
func allOnline(string) bool { return true }

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
	ledger.Reconcile(wA, fpA, reports)           // A splits the guest out
	uChips := ledger.Reconcile(wB, fpB, reports) // B receives its craft

	if wB.DockGuest != nil {
		t.Errorf("B still docked-as-guest after undock")
	}
	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not get craft %d back", guestID)
	}
	// ADR 0038 §4/§5: the returned craft clears the seam by the separation
	// push rather than landing frozen on top of it (was an exact-equality
	// check pre-0038 — see TestLiveUndockPushesClearOfTheStack for the push
	// magnitude and TestLiveUndockPlacesAcrossSubspaceGap for the
	// subspace-gap propagation this generalises from the Parcel-only path).
	if d := got.State.R.Sub(stackR).Norm(); d <= sim.DockingDistM {
		t.Errorf("returned craft state %v is only %v m from stack seam %v — inside the docking gate", got.State.R, d, stackR)
	}
	if dv := got.State.V.Sub(stackV).Norm(); dv <= sim.DockingVMS {
		t.Errorf("returned craft closing rate %v is only %v m/s from the seam — inside the docking gate", got.State.V, dv)
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
	if ok, _ := ledger.RequestTransfer(fpA, allOnline); !ok {
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

// TestTransferAdoptRestampsCollidingCompositeID: the migrating composite's
// origin-World ID collides with a craft native to the recipient's World
// (per-World ID spaces are independent). The recipient must restamp on adopt so
// CompositeID resolves the composite — not the colliding native craft — and a
// later undock-as-guest still hands the old owner its craft back (v0.28
// finding 1: the permanent-desync regression).
func TestTransferAdoptRestampsCollidingCompositeID(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 400
	wA, wB := alignedPair(t, guestID)
	dockerID := wA.ActiveCraft().ID
	now := time.Now()

	// Give B a NATIVE craft whose ID equals A's docker/composite ID. After the
	// stack migrates to B, its origin ID (dockerID) would alias this craft.
	collider := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	collider.Primary = wB.ActiveCraft().Primary
	collider.State = wB.ActiveCraft().State
	collider.ID = dockerID
	wB.AdoptCraft(collider, false)
	if collider.ID != dockerID {
		t.Fatalf("collider not seeded with docker id: %d != %d", collider.ID, dockerID)
	}

	// Dock: A claims, B hands over, A fuses.
	ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 1 || !sim.StackHasGuest(wA.Crafts[0]) {
		t.Fatalf("A did not fuse a cross-player stack")
	}

	// Transfer control to B (roles swap). A migrates out, B adopts.
	if ok, _ := ledger.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer refused")
	}
	ledger.Reconcile(wA, fpA, reports) // A migrates the stack out
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wB, fpB, reports) // B adopts the transferred stack

	// B now owns the composite AND the colliding native craft — both must be
	// independently addressable, and CompositeID must resolve the composite.
	rec := ledger.Records()
	if len(rec) != 1 {
		t.Fatalf("ledger records = %d, want 1", len(rec))
	}
	compID := rec[0].CompositeID
	comp, _, ok := wB.CraftByID(compID)
	if !ok || !sim.StackHasGuest(comp) {
		t.Fatalf("CompositeID %d does not resolve the migrated composite (collision with native id %d)", compID, dockerID)
	}
	if c, _, ok := wB.CraftByID(dockerID); !ok || c != collider {
		t.Errorf("native collider id %d no longer resolves to the collider craft", dockerID)
	}

	// A (now the guest) undocks. Its components are at the BOTTOM of the stack
	// — the transfer flipped the tags without restacking the vehicle — so the
	// split is refused rather than handing each player the other's hardware
	// (#307). A stays docked and is TOLD; see
	// TestUndockAfterTransferRefusesAndTellsTheGuest for the seam-level
	// assertions and the recovery.
	if !ledger.RequestUndock(fpA, dockerID) {
		t.Fatalf("RequestUndock refused for the demoted old owner")
	}
	reports = reportMap(store, wA, wB, now.Add(2*time.Second))
	ledger.Reconcile(wB, fpB, reports) // B attempts the split
	ledger.Reconcile(wA, fpA, reports) // A learns it was refused
	if len(wA.Crafts) != 0 {
		t.Errorf("A received %d craft from a refused undock", len(wA.Crafts))
	}
	if wA.DockGuest == nil {
		t.Errorf("A dropped out of docked-as-guest on a refused undock")
	}
	if len(ledger.Records()) != 1 {
		t.Errorf("ledger holds %d records after a refused undock, want the dock still standing", len(ledger.Records()))
	}
}

// TestUndockAfterTransferRefusesAndTellsTheGuest (#307 + #308): the sequence
// that swapped the two players' vehicles live. A docks B's craft, hands control
// to B, then asks to undock. Its components now sit at the bottom of the stack
// while the tags call them the guest's, so the tail peel would return B's
// vehicle under A's name and stable ID. The split is refused, A is told, and
// handing control back makes the release work — with the right hardware, checked
// by MASS, the one field neither the name-rewrite nor the ID-restamp preserves.
func TestUndockAfterTransferRefusesAndTellsTheGuest(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 400
	wA, wB := alignedPair(t, guestID)
	// Distinct vehicles: same-loadout craft would make the mass assertion
	// vacuous, and mass is the only discriminator the bug leaves intact.
	wB.ActiveCraft().Stages = spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID).Stages
	wB.ActiveCraft().SyncFields()
	dockerID := wA.ActiveCraft().ID
	massA, massB := wA.ActiveCraft().TotalMass(), wB.ActiveCraft().TotalMass()
	if massA == massB {
		t.Fatalf("test vehicles are indistinguishable by mass (%v) — assertions would be vacuous", massA)
	}
	now := time.Now()

	// Dock, then hand control to B.
	ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if ok, _ := ledger.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer refused")
	}
	ledger.Reconcile(wA, fpA, reports)
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wB, fpB, reports)

	// A asks to undock. Refused — and A hears about it.
	if !ledger.RequestUndock(fpA, dockerID) {
		t.Fatalf("RequestUndock refused for the demoted old owner")
	}
	reports = reportMap(store, wA, wB, now.Add(2*time.Second))
	ledger.Reconcile(wB, fpB, reports)
	chips := ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 0 {
		t.Fatalf("A received %d craft from a refused undock (mass %v — B's is %v)",
			len(wA.Crafts), wA.Crafts[0].TotalMass(), massB)
	}
	if !hasChip(chips, sim.SessionEventUndockRefused) {
		t.Errorf("refused undock produced no moment for A — the key reads as dead: %+v", chips)
	}
	if wA.DockGuest == nil {
		t.Errorf("A left docked-as-guest by a refused undock")
	}

	// Recovery: B hands control back, which puts B's own components on top,
	// and B releases. Both players end up holding their own vehicle.
	if ok, _ := ledger.RequestTransfer(fpB, allOnline); !ok {
		t.Fatalf("RequestTransfer back to A refused")
	}
	reports = reportMap(store, wA, wB, now.Add(3*time.Second))
	ledger.Reconcile(wB, fpB, reports) // B migrates the stack out
	ledger.Reconcile(wA, fpA, reports) // A adopts it and owns again
	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused for B after the handback")
	}
	reports = reportMap(store, wA, wB, now.Add(4*time.Second))
	ledger.Reconcile(wA, fpA, reports) // A splits B's component out
	ledger.Reconcile(wB, fpB, reports) // B receives its craft home

	if len(wB.Crafts) != 1 {
		t.Fatalf("B holds %d craft after the handback release, want 1", len(wB.Crafts))
	}
	if got := wB.Crafts[0].TotalMass(); got != massB {
		t.Errorf("B got a vehicle of mass %v, want its own %v (A's is %v)", got, massB, massA)
	}
	if len(wA.Crafts) != 1 {
		t.Fatalf("A holds %d craft after the handback release, want 1", len(wA.Crafts))
	}
	if got := wA.Crafts[0].TotalMass(); got != massA {
		t.Errorf("A kept a vehicle of mass %v, want its own %v (B's is %v)", got, massA, massB)
	}
}

// TestRestoredDockWithNoLiveCompositeIsReaped (#309): a record seeded from the
// session directory outlives the craft payloads, which are transient. Measured
// on the production host, such a record sat in DockActive for 5½ hours pointing
// at a composite that existed in nobody's save — nothing reaped it, and [U] did
// not clear it either, so the guest stayed flagged docked-as-guest to a phantom
// stack. The owner's first reconcile must notice and end the dock.
func TestRestoredDockWithNoLiveCompositeIsReaped(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	wA, wB := alignedPair(t, 400)
	now := time.Now()

	// Exactly the persisted shape: durable fields only, DockActive, naming a
	// composite ID that resolves in neither World.
	const phantomID = 9999
	if _, _, ok := wA.CraftByID(phantomID); ok {
		t.Fatalf("phantom composite id %d unexpectedly resolves", phantomID)
	}
	ledger.Seed([]DockRecord{{
		ID: 3, Owner: fpA, OwnerHandle: "alice", DockerCraftID: wA.ActiveCraft().ID,
		CompositeID: phantomID, GuestOwner: fpB, GuestHandle: "bob",
		GuestCraftID: 400, Phase: DockActive,
	}})

	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wA, fpA, reports) // owner notices the composite is gone
	chips := ledger.Reconcile(wB, fpB, reports)

	if len(ledger.Records()) != 0 {
		t.Errorf("phantom dock survived reconcile: %+v", ledger.Records())
	}
	if wB.DockGuest != nil {
		t.Errorf("B still flagged docked-as-guest to a stack that does not exist: %+v", wB.DockGuest)
	}
	if !hasChip(chips, sim.SessionEventDockLost) {
		t.Errorf("the dock ended with no moment for B — the marker just vanishes: %+v", chips)
	}
}

// TestUndockAskOnPhantomCompositeReapsRecord (#309): pressing [U] against a
// restored record whose composite is gone must not leave the record standing.
// The pre-fix undockAsk branch looked the composite up, failed, and returned
// false anyway — its comment claimed the guest side would time out, and a sweep
// for that reaper found none.
func TestUndockAskOnPhantomCompositeReapsRecord(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	wA, wB := alignedPair(t, 400)
	now := time.Now()
	ledger.Seed([]DockRecord{{
		ID: 4, Owner: fpA, OwnerHandle: "alice", DockerCraftID: wA.ActiveCraft().ID,
		CompositeID: 8888, GuestOwner: fpB, GuestHandle: "bob",
		GuestCraftID: 400, Phase: DockActive,
	}})
	if !ledger.RequestUndock(fpB, 400) {
		t.Fatalf("RequestUndock refused on the restored record")
	}
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)
	if len(ledger.Records()) != 0 {
		t.Errorf("[U] left the phantom dock standing: %+v", ledger.Records())
	}
}

// TestReconcileEmptyLedgerFastPath: with no live docks, Reconcile takes the
// O(1) fast path — it still clears any stale docked-as-guest slate and returns
// no chips (v0.28 finding 4).
func TestReconcileEmptyLedgerFastPath(t *testing.T) {
	ledger := NewDockLedger()
	w := newWorld(t)
	w.DockGuest = &sim.DockGuestLink{OwnerFP: fpA, OwnerHandle: "alice"} // stale
	chips := ledger.Reconcile(w, fpB, nil)
	if chips != nil {
		t.Errorf("empty-ledger Reconcile returned chips: %+v", chips)
	}
	if w.DockGuest != nil {
		t.Errorf("empty-ledger Reconcile did not clear stale DockGuest: %+v", w.DockGuest)
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
