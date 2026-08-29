package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLiveTransferControlStripsCrossOwnerTargetRefs — review finding
// following #294 review finding 3 / finding G: a LIVE (no-restart) control
// transfer hands the whole migrating composite into the recipient's World
// via w.AdoptCraft, with NO wire round-trip to sanitize it the way a
// restart-persisted handover does through save.CraftToWireForTransfer.
// AdoptCraft only remaps the composite's OWN id, not a local ref a planted
// node or the craft's Target carries against a SISTER craft in the sending
// world — craft ids are dense per-World small ints, so that ref can
// silently resolve against an unrelated craft that happens to hold the
// same number on the recipient's side and fire a real burn toward it.
//
// This exercises the exact live (same-server-lifetime) path: A docks B,
// then transfers control to B — the composite (carrying A's docker craft's
// own Target and a planted node bound to a SISTER craft still in A's
// World) must land in B's World with those refs stripped.
func TestLiveTransferControlStripsCrossOwnerTargetRefs(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 700
	wA, wB := alignedPair(t, guestID)
	dockerID := wA.ActiveCraft().ID
	now := time.Now()

	// A second, unrelated craft in A's World — the "sister craft" whose
	// local ID the docker craft's own Target/node will reference. Per-World
	// ID spaces are independent, so this same number can (and, on a real
	// host, eventually will) belong to something else entirely in B's
	// World.
	sister := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	sister.Primary = wA.ActiveCraft().Primary
	sister.State = wA.ActiveCraft().State
	wA.AdoptCraft(sister, false)
	sisterID := sister.ID

	// Point the docker craft's own Target and a planted node at the
	// sister craft — a bare LOCAL ref (no ghost owner), exactly the shape
	// #294 review finding G showed a stale wire round-trip could alias.
	wA.ActiveCraft().Target = spacecraft.Target{Kind: spacecraft.TargetCraft, CraftID: sisterID}
	wA.ActiveCraft().Nodes = append(wA.ActiveCraft().Nodes, spacecraft.ManeuverNode{
		Mode:          spacecraft.BurnTargetPrograde,
		DV:            10,
		TriggerTime:   wA.Clock.SimTime.Add(time.Minute),
		PrimaryID:     wA.ActiveCraft().Primary.ID,
		TargetCraftID: sisterID,
	})

	// Dock, then hand control to B — live, no restart involved anywhere in
	// this sequence.
	ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if ok, why := ledger.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer refused: %s", why)
	}
	ledger.Reconcile(wA, fpA, reports) // A migrates the stack out
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wB, fpB, reports) // B adopts the transferred stack

	if len(wB.Crafts) != 1 {
		t.Fatalf("B did not adopt the transferred stack: %d crafts", len(wB.Crafts))
	}
	comp := wB.Crafts[0]

	if comp.Target.Kind != spacecraft.TargetNone {
		t.Errorf("migrated composite Target = %+v, want stripped to TargetNone (A-local craft id %d)", comp.Target, sisterID)
	}
	if len(comp.Nodes) != 1 {
		t.Fatalf("migrated composite lost the node itself, not just its ref: %+v", comp.Nodes)
	}
	if comp.Nodes[0].TargetCraftID != 0 || comp.Nodes[0].TargetGhostOwner != "" {
		t.Errorf("migrated composite node still targets A-local craft id %d — would fire at whatever unrelated craft holds that id in B's World", comp.Nodes[0].TargetCraftID)
	}
	if comp.Nodes[0].DV != 10 || comp.Nodes[0].Mode != spacecraft.BurnTargetPrograde {
		t.Errorf("migrated composite node lost its burn geometry: %+v", comp.Nodes[0])
	}
}

// TestLiveAbortReturnKeepsFullFidelityRefs pins the IMPORTANT carve-out
// alongside the fix above: a live abort-return delivers the guest's own
// craft back to the SAME owner that planted its refs — those refs stayed
// valid the whole time and must NOT be stripped. This is
// TestFullRecordsPreservesReturnPayloadRefs's persisted-path guarantee,
// exercised on the live (no-restart) delivery path instead: the docker's
// craft vanishes between Claim and handover, so the guest's craft comes
// straight back via the abort branch's w.AdoptCraft — carrying a Target the
// guest had set on ITS OWN craft before ever handing it over.
func TestLiveAbortReturnKeepsFullFidelityRefs(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 701
	wA, wB := alignedPair(t, guestID)
	dockerID := wA.ActiveCraft().ID
	now := time.Now()

	// Give B a second craft so its guest craft can carry a LOCAL target ref
	// that stays perfectly meaningful within B's own World.
	sister := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	sister.Primary = wB.ActiveCraft().Primary
	sister.State = wB.ActiveCraft().State
	wB.AdoptCraft(sister, false)
	sisterID := sister.ID
	// The guest craft is B's ACTIVE craft, so its live Target lives on
	// w.Target (checked out per-craft only on a vessel switch) — set it
	// there, not directly on the Spacecraft struct, or RemoveCraftByID's
	// own outgoing-target checkpoint below overwrites it with the zero
	// value.
	wB.Target = spacecraft.Target{Kind: spacecraft.TargetCraft, CraftID: sisterID}

	ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports) // B hands its craft over

	// The docker's craft vanishes before A's tick can fuse it (staged /
	// ended flight) — reconcileOwner's abort branch fires and hands the
	// guest's craft straight back.
	wA.Crafts = nil
	ledger.Reconcile(wA, fpA, reports)
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wB, fpB, reports) // B reclaims its own craft

	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not reclaim its own craft after the abort")
	}
	if got.Target.Kind != spacecraft.TargetCraft || got.Target.CraftID != sisterID {
		t.Errorf("abort-return stripped a same-owner ref: Target = %+v, want it kept (%d)", got.Target, sisterID)
	}
}
