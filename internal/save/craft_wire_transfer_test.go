package save

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ghostBoundCraft builds a craft carrying a ghost ref on all three
// surfaces CraftToWire round-trips: Target, a planted node, and the
// active burn. Shared by the transfer-sanitize tests below.
func ghostBoundCraft(t *testing.T) *spacecraft.Spacecraft {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Target = spacecraft.Target{Kind: spacecraft.TargetGhost, CraftID: 987654, GhostOwner: "SHA256:gern"}
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode:             spacecraft.BurnTarget,
		DV:               42,
		TriggerTime:      w.Clock.SimTime.Add(time.Minute),
		PrimaryID:        c.Primary.ID,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	})
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:             spacecraft.BurnTarget,
		DVRemaining:      15,
		EndTime:          w.Clock.SimTime.Add(30 * time.Second),
		PrimaryID:        c.Primary.ID,
		Throttle:         1,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}
	return c
}

// TestCraftToWireKeepsGhostRefsForSession is the control: the plain
// session save/reconnect path (CraftToWire) preserves every ghost ref,
// as #294 review finding 5 already established and ghost_node_ref_test.go
// / ghost_target_test.go cover end to end via Save/Load. Restated here,
// side by side with the transfer variant below, so the two paths' actual
// divergence is visible in one place.
func TestCraftToWireKeepsGhostRefsForSession(t *testing.T) {
	c := ghostBoundCraft(t)
	wc := CraftToWire(c)

	if wc.Target == nil || wc.Target.Kind != int(spacecraft.TargetGhost) || wc.Target.GhostOwner != "SHA256:gern" {
		t.Errorf("session wire Target = %+v, want the ghost ref kept", wc.Target)
	}
	if len(wc.Nodes) != 1 || wc.Nodes[0].TargetGhostOwner != "SHA256:gern" || wc.Nodes[0].TargetCraftID != 987654 {
		t.Errorf("session wire node ref = %+v, want the ghost ref kept", wc.Nodes)
	}
	if wc.ActiveBurn == nil || wc.ActiveBurn.TargetGhostOwner != "SHA256:gern" || wc.ActiveBurn.TargetCraftID != 987654 {
		t.Errorf("session wire ActiveBurn ref = %+v, want the ghost ref kept", wc.ActiveBurn)
	}
}

// TestCraftToWireForTransferStripsGhostRefs — #294 review finding 3. A
// dock-ledger parcel/return/transfer payload is delivered into a
// DIFFERENT player's world, where a ghost ref (owner fingerprint +
// remote craft ID) can never resolve and can even alias the
// RECIPIENT's own fingerprint. The sanitized wrapper must drop the
// ghost Target outright and strip the ghost ref from every node and
// the active burn — while leaving everything else (burn geometry, Δv,
// mode) untouched.
func TestCraftToWireForTransferStripsGhostRefs(t *testing.T) {
	c := ghostBoundCraft(t)
	wc := CraftToWireForTransfer(c)

	if wc.Target != nil {
		t.Errorf("transfer wire Target = %+v, want nil (ghost target dropped outright)", wc.Target)
	}
	if len(wc.Nodes) != 1 {
		t.Fatalf("transfer wire lost the node itself, not just its ref: %+v", wc.Nodes)
	}
	if wc.Nodes[0].TargetGhostOwner != "" || wc.Nodes[0].TargetCraftID != 0 {
		t.Errorf("transfer wire node ref = %+v, want the ghost ref stripped", wc.Nodes[0])
	}
	if wc.Nodes[0].DV != 42 || wc.Nodes[0].Mode != int(spacecraft.BurnTarget) {
		t.Errorf("transfer wire node lost its burn geometry: %+v", wc.Nodes[0])
	}
	if wc.ActiveBurn == nil {
		t.Fatal("transfer wire lost the active burn itself, not just its ref")
	}
	if wc.ActiveBurn.TargetGhostOwner != "" || wc.ActiveBurn.TargetCraftID != 0 {
		t.Errorf("transfer wire ActiveBurn ref = %+v, want the ghost ref stripped", wc.ActiveBurn)
	}
	if wc.ActiveBurn.DVRemaining != 15 {
		t.Errorf("transfer wire ActiveBurn lost its DVRemaining: %+v", wc.ActiveBurn)
	}
}

// TestCraftToWireForTransferLeavesLocalRefsAlone — a LOCAL craft ref
// (no ghost owner) is a different craft in the SAME world; the
// transfer sanitizer must not touch it.
func TestCraftToWireForTransferLeavesLocalRefsAlone(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode:          spacecraft.BurnTargetPrograde,
		DV:            10,
		TriggerTime:   w.Clock.SimTime.Add(time.Minute),
		PrimaryID:     c.Primary.ID,
		TargetCraftID: 12345, // local ref, no ghost owner
	})

	wc := CraftToWireForTransfer(c)

	if len(wc.Nodes) != 1 || wc.Nodes[0].TargetCraftID != 12345 {
		t.Errorf("local node ref touched by the ghost sanitizer: %+v", wc.Nodes)
	}
}
