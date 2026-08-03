package relay

import (
	"strings"
	"testing"
	"time"
)

// TestTransferControlRefusedWhenRecipientIsNotInSession (#311, ADR 0040 §2):
// handing someone the stick needs someone there to take it. With no live
// Session on the other side the sender would become a passenger in a stack
// that exists in nobody's sky, parked outside time — so [J] refuses, and says
// why, instead of creating the state durability then has to rescue.
func TestTransferControlRefusedWhenRecipientIsNotInSession(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 800
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)

	offline := func(fp string) bool { return fp != fpB }
	ok, why := ledger.RequestTransfer(fpA, offline)
	if ok {
		t.Fatalf("transfer to an absent partner was accepted")
	}
	if !strings.Contains(why, "bob") {
		t.Errorf("refusal %q does not name the partner who isn't there", why)
	}
	if why == "" {
		t.Errorf("transfer refused in silence — the key reads as dead")
	}
	// And nothing moved: A still flies the stack.
	ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 1 {
		t.Errorf("A holds %d craft after a refused transfer, want the stack it still owns", len(wA.Crafts))
	}

	// The same press with the partner present goes through.
	online := func(string) bool { return true }
	if ok, why := ledger.RequestTransfer(fpA, online); !ok {
		t.Fatalf("transfer to a live partner refused: %s", why)
	}
	ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 0 {
		t.Errorf("A still holds the stack after a transfer to a live partner")
	}
}

// TestTransferRefusalIsExhaustive: every path by which RequestTransfer says no
// carries a reason, so a refusal can never reach the player as silence
// (#308's lesson, applied at the second cross-player verb).
func TestTransferRefusalIsExhaustive(t *testing.T) {
	ledger := NewDockLedger()
	online := func(string) bool { return true }
	// No dock at all.
	if ok, why := ledger.RequestTransfer(fpA, online); ok || why == "" {
		t.Errorf("transfer with no cross-player stack: ok=%v why=%q, want a refusal with a reason", ok, why)
	}
	// A pending (not yet fused) dock is not a stack to hand over.
	ledger.Claim(fpA, "alice", 1, fpB, "bob", 2)
	if ok, why := ledger.RequestTransfer(fpA, online); ok || why == "" {
		t.Errorf("transfer of a half-formed dock: ok=%v why=%q, want a refusal with a reason", ok, why)
	}
	// The refusal string is what the seat renders, so it must be a sentence,
	// not a code.
	_, why := ledger.RequestTransfer(fpA, online)
	if strings.TrimSpace(why) != why || len(why) < 10 {
		t.Errorf("refusal %q does not read as something a pilot was told", why)
	}
}
