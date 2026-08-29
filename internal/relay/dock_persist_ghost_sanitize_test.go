package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ghostBoundParkedCraft builds a craft carrying a ghost ref on Target, a
// planted node, and the active burn — standing in for a guest craft
// whose node/target is aimed at the HOST's own ghost right before it
// gets parked on a dock record for delivery into the host's world.
func ghostBoundParkedCraft(t *testing.T) *spacecraft.Spacecraft {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Target = spacecraft.Target{Kind: spacecraft.TargetGhost, CraftID: 987654, GhostOwner: fpA}
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode:             spacecraft.BurnTarget,
		DV:               42,
		TriggerTime:      w.Clock.SimTime.Add(time.Minute),
		PrimaryID:        c.Primary.ID,
		TargetGhostOwner: fpA,
		TargetCraftID:    987654,
	})
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:             spacecraft.BurnTarget,
		DVRemaining:      15,
		EndTime:          w.Clock.SimTime.Add(30 * time.Second),
		PrimaryID:        c.Primary.ID,
		Throttle:         1,
		TargetGhostOwner: fpA,
		TargetCraftID:    987654,
	}
	return c
}

// TestFullRecordsStripsGhostRefsFromParkedPayload — #294 review finding
// 3. A parked craft on a dock record (GuestPayload / ReturnPayload /
// TransferPayload, ADR 0040) is delivered into a DIFFERENT player's
// world across a restart, where (owner, craftID) can never resolve and
// can even alias the RECIPIENT's own fingerprint. FullRecords must go
// through the sanitized wire form, not the plain session one — the
// craft itself (mass, position, burn geometry) must still round-trip
// intact.
func TestFullRecordsStripsGhostRefsFromParkedPayload(t *testing.T) {
	ledger := NewDockLedger()
	c := ghostBoundParkedCraft(t)
	ledger.mu.Lock()
	ledger.records[1] = &DockRecord{ID: 1, Owner: fpA, GuestOwner: fpB, guestPayload: c}
	ledger.nextID = 2
	ledger.mu.Unlock()

	snaps := ledger.FullRecords()
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d, want 1", len(snaps))
	}
	gp := snaps[0].GuestPayload
	if gp == nil {
		t.Fatal("GuestPayload lost entirely")
	}
	if gp.Target != nil {
		t.Errorf("parked payload Target = %+v, want nil (ghost target dropped outright)", gp.Target)
	}
	if len(gp.Nodes) != 1 {
		t.Fatalf("parked payload lost the node itself: %+v", gp.Nodes)
	}
	if gp.Nodes[0].TargetGhostOwner != "" || gp.Nodes[0].TargetCraftID != 0 {
		t.Errorf("parked payload node ref = %+v, want the ghost ref stripped", gp.Nodes[0])
	}
	if gp.Nodes[0].DV != 42 {
		t.Errorf("parked payload node lost its Δv: %+v", gp.Nodes[0])
	}
	if gp.ActiveBurn == nil {
		t.Fatal("parked payload lost the active burn itself")
	}
	if gp.ActiveBurn.TargetGhostOwner != "" || gp.ActiveBurn.TargetCraftID != 0 {
		t.Errorf("parked payload ActiveBurn ref = %+v, want the ghost ref stripped", gp.ActiveBurn)
	}
	if gp.ActiveBurn.DVRemaining != 15 {
		t.Errorf("parked payload ActiveBurn lost its DVRemaining: %+v", gp.ActiveBurn)
	}
	if gp.M != c.State.M {
		t.Errorf("parked payload lost craft mass: got %v want %v", gp.M, c.State.M)
	}
}

// TestSeedFullDeliversSanitizedPayload — the restart round trip: a
// ghost-bound parked craft survives FullRecords -> SeedFull with the
// craft intact and the ghost refs gone, matching a fresh ledger built
// straight from a sanitized snapshot (no ghost ref ever reaches
// craftFromWire to rehydrate in the first place).
func TestSeedFullDeliversSanitizedPayload(t *testing.T) {
	ledger := NewDockLedger()
	c := ghostBoundParkedCraft(t)
	ledger.mu.Lock()
	ledger.records[1] = &DockRecord{ID: 1, Owner: fpA, GuestOwner: fpB, guestPayload: c}
	ledger.nextID = 2
	ledger.mu.Unlock()

	fresh := NewDockLedger()
	fresh.SeedFull(ledger.FullRecords(), loadedSystems(t))

	fresh.mu.Lock()
	rec := fresh.records[1]
	fresh.mu.Unlock()
	if rec == nil || rec.guestPayload == nil {
		t.Fatal("guestPayload lost across the restart round trip")
	}
	if rec.guestPayload.Target.Kind == spacecraft.TargetGhost {
		t.Errorf("restored payload still carries a ghost Target: %+v", rec.guestPayload.Target)
	}
	for _, n := range rec.guestPayload.Nodes {
		if n.TargetGhostOwner != "" {
			t.Errorf("restored payload node still carries a ghost ref: %+v", n)
		}
	}
	if ab := rec.guestPayload.ActiveBurn; ab != nil && ab.TargetGhostOwner != "" {
		t.Errorf("restored payload ActiveBurn still carries a ghost ref: %+v", ab)
	}
}

// TestFullRecordsStripsGhostRefsFromTransferPayload — same shape as
// TestFullRecordsStripsGhostRefsFromParkedPayload, for TransferPayload:
// a control transfer (or an empty-seat reclaim, GrantReclaim) hands the
// whole migrating stack to a DIFFERENT owner's world, so it must go
// through the same sanitized wire form GuestPayload does (#294
// second-round review finding 4 — this direction of the fix was
// already correct; this test pins it against a future regression).
func TestFullRecordsStripsGhostRefsFromTransferPayload(t *testing.T) {
	ledger := NewDockLedger()
	c := ghostBoundParkedCraft(t)
	ledger.mu.Lock()
	ledger.records[1] = &DockRecord{ID: 1, Owner: fpA, GuestOwner: fpB, transferPayload: c}
	ledger.nextID = 2
	ledger.mu.Unlock()

	snaps := ledger.FullRecords()
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d, want 1", len(snaps))
	}
	tp := snaps[0].TransferPayload
	if tp == nil {
		t.Fatal("TransferPayload lost entirely")
	}
	if tp.Target != nil {
		t.Errorf("transfer payload Target = %+v, want nil (ghost target dropped outright)", tp.Target)
	}
	if len(tp.Nodes) != 1 || tp.Nodes[0].TargetGhostOwner != "" || tp.Nodes[0].TargetCraftID != 0 {
		t.Errorf("transfer payload node ref not stripped: %+v", tp.Nodes)
	}
	if tp.ActiveBurn == nil || tp.ActiveBurn.TargetGhostOwner != "" || tp.ActiveBurn.TargetCraftID != 0 {
		t.Errorf("transfer payload ActiveBurn ref not stripped: %+v", tp.ActiveBurn)
	}
}

// TestFullRecordsPreservesReturnPayloadRefs — #294 second-round review
// finding 4. Unlike GuestPayload/TransferPayload, ReturnPayload never
// crosses into a different player's world: reconcileGuest always
// dispatches it back into the world of the session matching
// r.GuestOwner — the SAME owner who originally contributed the craft,
// whether via an abort-return (the guest's own pre-dock craft, parked
// verbatim), an undock, or a release. Sanitizing it (the pre-fix
// behaviour, sharing GuestPayload's CraftToWireForTransfer call) would
// strip a Target and node/burn refs that stayed perfectly valid the
// whole time — AdoptCraft only restamps the incoming craft's own ID, so
// a local ref to a SISTER craft in the guest's own slate survives
// untouched. FullRecords must round-trip ReturnPayload at full
// fidelity via the plain save.CraftToWire instead.
func TestFullRecordsPreservesReturnPayloadRefs(t *testing.T) {
	ledger := NewDockLedger()
	c := ghostBoundParkedCraft(t)
	ledger.mu.Lock()
	ledger.records[1] = &DockRecord{ID: 1, Owner: fpA, GuestOwner: fpB, returnPayload: c}
	ledger.nextID = 2
	ledger.mu.Unlock()

	snaps := ledger.FullRecords()
	if len(snaps) != 1 {
		t.Fatalf("len(snaps) = %d, want 1", len(snaps))
	}
	rp := snaps[0].ReturnPayload
	if rp == nil {
		t.Fatal("ReturnPayload lost entirely")
	}
	if rp.Target == nil || rp.Target.Kind != int(spacecraft.TargetGhost) || rp.Target.GhostOwner != fpA {
		t.Errorf("return payload lost its valid ghost Target: %+v", rp.Target)
	}
	if len(rp.Nodes) != 1 || rp.Nodes[0].TargetGhostOwner != fpA || rp.Nodes[0].TargetCraftID != 987654 {
		t.Errorf("return payload node ref stripped, want preserved: %+v", rp.Nodes)
	}
	if rp.ActiveBurn == nil || rp.ActiveBurn.TargetGhostOwner != fpA || rp.ActiveBurn.TargetCraftID != 987654 {
		t.Errorf("return payload ActiveBurn ref stripped, want preserved: %+v", rp.ActiveBurn)
	}

	// The restart round trip: rehydrating must still see the refs.
	fresh := NewDockLedger()
	fresh.SeedFull(snaps, loadedSystems(t))
	fresh.mu.Lock()
	rec := fresh.records[1]
	fresh.mu.Unlock()
	if rec == nil || rec.returnPayload == nil {
		t.Fatal("returnPayload lost across the restart round trip")
	}
	if rec.returnPayload.Target.Kind != spacecraft.TargetGhost || rec.returnPayload.Target.GhostOwner != fpA {
		t.Errorf("restored return payload lost its ghost Target: %+v", rec.returnPayload.Target)
	}
	if len(rec.returnPayload.Nodes) != 1 || rec.returnPayload.Nodes[0].TargetGhostOwner != fpA {
		t.Errorf("restored return payload lost its node ref: %+v", rec.returnPayload.Nodes)
	}
}
