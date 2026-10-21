package relay

// Review fixes for PR #724 (10a): F2, F5. Grown from the review probes.

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func countX(r *rig, w *sim.World) (vessels, parts int) {
	for _, c := range w.Crafts {
		if c.ID == r.xID {
			vessels++
		}
		for _, dc := range c.DockedComponents {
			if dc.CraftID == r.xID && c.ID != r.xID {
				parts++
			}
		}
	}
	return
}

// F2: section 10 case 2 for O4. The recipient's live World had allocated more
// IDs than its stale payload, so the live adoption restamped C to an ID the
// stale payload's NextCraftID will not reproduce. Crash after the World write,
// before the ledger flush, then repair again.
func TestRepairOwnerAdoptionIsIdempotentUnderRestamp(t *testing.T) {
	r := newRig(t, 402)
	r.guestTick()
	r.ownerTick()
	r.ack()
	cid := r.snap().CompositeID
	bStale := viaDisk(t, r.wB) // riding, NextCraftID as of now
	// Live B spawns a Vessel that takes C's ID, and a second one so NextCraftID
	// drifts past what the stale payload knows.
	for i := 0; i < 2; i++ {
		v := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
		if i == 0 {
			v.ID = cid
		}
		r.wB.AdoptCraft(v, false)
	}
	if ok, why := r.l.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer: %s", why)
	}
	r.ownerTick()
	r.guestTick() // adoption: C restamped
	liveCID := r.snap().CompositeID
	t.Logf("C id in sender %d, after live adoption %d", cid, liveCID)
	r.crash()
	flushed := r.l.FullRecords()
	r.wB = bStale
	r.l.Repair(r.wB, fpB)
	firstCID := r.snap().CompositeID
	written := viaDisk(t, r.wB) // the World write
	// the ledger flush is lost:
	r.l = NewDockLedger()
	r.l.SeedFull(flushed, loadedSystems(t))
	r.wB = written
	r.l.Repair(r.wB, fpB)
	n := 0
	for _, c := range r.wB.Crafts {
		if sim.StackHasGuest(c) {
			n++
		}
	}
	t.Logf("CompositeID on disk %d, after first repair %d, after rerun %d; composites with guest parts in B: %d", liveCID, firstCID, r.snap().CompositeID, n)
	if n != 1 {
		t.Errorf("F2: %d Composites carrying the guest's parts in the recipient's World, want 1", n)
	}
}

// F5: the ack guard asks whether the owner's save carries the guest's parts,
// not whether the Composite's ID resolves. A hand-loaded older save can hold an
// unrelated Vessel at that ID.
func TestAckKeepsOwnerCopyWhenSaveHasOnlyAnUnrelatedVesselAtCompositeID(t *testing.T) {
	r := newRig(t, 405)
	older := viaDisk(t, r.wA) // lead alone, same ID the Composite will keep
	r.guestTick()
	r.ownerTick() // fuse: copy held
	r.l.AckPersisted(fpA, older)
	if !r.snap().OwnerDirty {
		t.Errorf("F5: an owner save without the guest's parts acknowledged the fuse")
	}
}

// F12: the reaper's returnPayload exemption. Once the guest's parts have peeled
// off, the Composite carries none of them, yet the dock is still answering
// asks until the Vessel lands: a second undock press in that window is refused
// (as before 10a), not silently dropped by the "no guest parts" gate.
func TestReaperExemptionKeepsAnsweringAsksWhileTheReturnIsInFlight(t *testing.T) {
	r := newRig(t, 406)
	r.guestTick()
	r.ownerTick()
	r.ack()
	if !r.l.RequestUndock(fpB, r.xID) {
		t.Fatal("test setup: RequestUndock refused")
	}
	r.ownerTick() // peels the Vessel off: returnPayload parked
	if !r.l.ReturnInFlight(r.snap().ID) {
		t.Fatal("test setup: no return in flight")
	}
	r.l.RequestUndock(fpB, r.xID) // the player presses again
	r.ownerTick()
	if !r.snap().UndockRefused {
		t.Errorf("a second undock press while the Vessel is in flight was dropped, not answered")
	}
}
