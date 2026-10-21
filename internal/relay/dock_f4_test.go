package relay

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// stageThroughLead presses stage until the owner's own components are gone.
func (r *rig) stageThroughLead() {
	r.t.Helper()
	c, idx, ok := r.wA.CraftByID(r.snap().CompositeID)
	if !ok {
		r.t.Fatal("test setup: no Composite in the owner World")
	}
	for n, i := len(c.DockedComponents[0].Stages), 0; i < n && !sim.StackIsGuestOnly(c); i++ {
		ni, _, err := r.wA.StageActive(idx)
		if err != nil {
			r.t.Fatalf("StageActive: %v", err)
		}
		idx = ni
		c, idx, _ = r.wA.CraftByID(r.snap().CompositeID)
	}
	if !sim.StackIsGuestOnly(c) {
		r.t.Fatalf("test setup: staging left the owner's components in the stack")
	}
	r.xDry = c.DryMass // what the guest still has after staging took its share
}

// F4: the owner stages through their whole lead while a guest is docked. What
// is left of the stack is the guest's Vessel, so it goes back to the guest as a
// Parcel (the release path), not DockLost, and the owner stops flying it.
func TestOwnerStagingThroughLeadHandsTheGuestVesselBack(t *testing.T) {
	r := newRig(t, 407)
	r.guestTick()
	r.ownerTick()
	r.ack()
	r.stageThroughLead()
	chips := append(r.ownerTick(), r.guestTick()...)
	chips = append(chips, r.guestTick()...)
	if hasKind(chips, sim.SessionEventDockLost) {
		t.Errorf("F4: a stack that only staged was reaped as DockLost: %+v", chips)
	}
	if !hasKind(chips, sim.SessionEventParcelReturned) {
		t.Errorf("F4: the guest got no 'released your vessel' chip: %+v", chips)
	}
	r.oneGuestVessel("after the owner staged through the lead")
	for _, c := range r.wA.Crafts {
		if sim.StackHasGuest(c) || c.ID == r.xID {
			t.Errorf("F4: the owner still holds the guest's hardware: craft %d %q", c.ID, c.Name)
		}
	}
	found := false
	for _, c := range r.wB.Crafts {
		found = found || c.ID == r.xID
	}
	if !found {
		t.Errorf("F4: the guest's Vessel is not on the guest's slate")
	}
}

// Crash after the hand-back: the owner's stale payload still holds the
// pre-staging stack; repair must not resurrect a second copy of the Vessel.
func TestRepairDoesNotResurrectGuestVesselHandedBackAfterStaging(t *testing.T) {
	r := newRig(t, 408)
	r.guestTick()
	r.ownerTick()
	r.ack()
	ownerStale := viaDisk(t, r.wA) // written before the staging
	r.stageThroughLead()
	r.ownerTick() // hand-back parked on the record
	r.crash()
	r.wA = ownerStale
	r.l.Repair(r.wA, fpA)
	r.l.Repair(r.wB, fpB)
	r.settle()
	r.oneGuestVessel("after a crash following the hand-back")
	for _, c := range r.wA.Crafts {
		if sim.StackHasGuest(c) {
			t.Errorf("F4: the owner's repaired World still carries the guest's parts")
		}
	}
}
