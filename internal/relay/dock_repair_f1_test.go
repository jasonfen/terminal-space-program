package relay

// Review fix F1 for PR #724 (10a). Grown from the review probe.

import "testing"

// F1: 7.6/7.7 with the recipient's payload written BEFORE their handover
// (within the guest's 5 min autosave window). After the swap the recipient is
// the Owner, so no G rule removes the stale X, and O4 adopts the Composite that
// also carries X.
func TestRepairTransferRecipientStalePreHandoverPayload(t *testing.T) {
	r := newRig(t, 401)
	bStale := viaDisk(t, r.wB) // pre-handover: still holds X as B's own
	r.guestTick()              // handover
	r.ownerTick()              // fuse
	r.ack()
	if ok, why := r.l.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer: %s", why)
	}
	r.ownerTick() // park, swap: B is Owner now
	r.guestTick() // B adopts (ownerCopy = C, OwnerDirty)
	r.crash()
	r.wB = bStale
	r.l.Repair(r.wB, fpB)
	v, p := countX(r, r.wB)
	t.Logf("recipient World after repair: %d free X vessels, %d X parts inside composites, %d crafts", v, p, len(r.wB.Crafts))
	if v+p != 1 {
		t.Errorf("F1: %d flyable copies of X in the recipient's World, want 1", v+p)
	}
}
