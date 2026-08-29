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
