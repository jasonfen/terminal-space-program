package relay

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0040 amendment (10a, slice A2): Repair. Each test builds a dock, takes
// "the payload as it was written" with viaDisk at the chosen crash point, lets
// the transition happen, then crashes (ledger restarts from its full snapshot,
// a Server restart) and repairs the stale Worlds. The claim under test is the
// one in the amendment: exactly one flyable copy of the Vessel afterwards,
// across {guest World, owner World, ledger in flight}, by ID and by mass.

type rig struct {
	t       *testing.T
	l       *DockLedger
	wA, wB  *sim.World // A owns the stack, B is the guest
	reports map[string]CraftReport
	xID     uint64  // the guest Vessel's pre-dock ID
	xDry    float64 // its dry mass, the "component mass" the copies are checked by
}

func newRig(t *testing.T, xID uint64) *rig {
	t.Helper()
	l := NewDockLedger()
	wA, wB := alignedPair(t, xID)
	r := &rig{t: t, l: l, wA: wA, wB: wB, xID: xID, xDry: wB.ActiveCraft().DryMass}
	if _, ok := l.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", xID); !ok {
		t.Fatalf("test setup: Claim refused")
	}
	r.reports = reportMap(NewStore(), wA, wB, time.Now())
	return r
}

func (r *rig) guestTick() []DockChip { return r.l.Reconcile(r.wB, fpB, r.reports) }
func (r *rig) ownerTick() []DockChip { return r.l.Reconcile(r.wA, fpA, r.reports) }
func (r *rig) ack()                  { ackAll(r.l, r.wA, r.wB) }

// crash is a hard crash of the host: the ledger comes back from what it had
// flushed, which here is everything it knew (the Worlds are the stale part).
func (r *rig) crash() { r.l = restart(r.t, r.l) }

// settle lets every in-flight payload land.
func (r *rig) settle() {
	for i := 0; i < 3; i++ {
		r.ownerTick()
		r.guestTick()
	}
}

// viaDisk is "this World was written to the player's payload now": the
// returned World is what the next load of that payload would give.
func viaDisk(t *testing.T, w *sim.World) *sim.World {
	t.Helper()
	p := filepath.Join(t.TempDir(), "w.json")
	if err := save.Save(w, p); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := save.Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return out
}

// vessels counts top-level Vessels with this ID across the Worlds, plus any
// the ledger holds in flight. parts counts Docked Components carrying it.
func (r *rig) tally(id uint64, worlds ...*sim.World) (vessels, parts int, dry []float64) {
	for _, w := range worlds {
		for _, c := range w.Crafts {
			if c.ID == id {
				vessels++
				dry = append(dry, c.DryMass)
			}
			for _, dc := range c.DockedComponents {
				if dc.CraftID == id && c.ID != id {
					parts++
					dry = append(dry, dc.DryMass)
				}
			}
		}
	}
	r.l.mu.Lock()
	defer r.l.mu.Unlock()
	for _, m := range []map[uint64]*DockRecord{r.l.records, r.l.settled} {
		for _, rec := range m {
			for _, p := range []*spacecraft.Spacecraft{rec.guestPayload, rec.returnPayload, rec.transferPayload} {
				if p != nil && p.ID == id {
					vessels++
					dry = append(dry, p.DryMass)
				}
			}
		}
	}
	return
}

// oneGuestVessel fails unless exactly one flyable copy of the guest Vessel
// exists and it has the guest Vessel's mass.
func (r *rig) oneGuestVessel(why string) {
	r.t.Helper()
	v, p, dry := r.tally(r.xID, r.wA, r.wB)
	if v+p != 1 {
		r.t.Errorf("%s: %d flyable copies of the guest Vessel (%d vessels, %d parts), want 1", why, v+p, v, p)
	}
	for _, d := range dry {
		if d != r.xDry {
			r.t.Errorf("%s: copy has dry mass %v, want %v", why, d, r.xDry)
		}
	}
}

func (r *rig) oneComposite(why string, id uint64) {
	r.t.Helper()
	if v, _, _ := r.tally(id, r.wA, r.wB); v != 1 {
		r.t.Errorf("%s: %d copies of the Composite %d, want 1", why, v, id)
	}
}

func (r *rig) snap() DockSnapshot { return only(r.t, r.l) }

func hasKind(chips []DockChip, k sim.SessionEventKind) bool { return hasChip(chips, k) }

// 7.2: the handover is in the ledger, the guest's payload is the old one.
func TestRepairDropsStaleGuestVesselAfterHandover(t *testing.T) {
	r := newRig(t, 301)
	stale := viaDisk(t, r.wB) // written before the handover: still holds X
	r.guestTick()
	r.crash()
	r.wB = stale
	if !r.l.Repair(r.wB, fpB) {
		t.Errorf("Repair reported no change for a guest World holding a Vessel the ledger took")
	}
	r.settle()
	r.oneGuestVessel("after handover + restart")
}

// 7.3 row 2: the fuse is in the ledger, the owner's payload predates it. The
// Composite keeps the lead's ID, so the stale World even "has" CompositeID.
func TestRepairRefusesCompositeAfterFuse(t *testing.T) {
	r := newRig(t, 302)
	r.guestTick()
	guestSaved := viaDisk(t, r.wB)
	ownerStale := viaDisk(t, r.wA)
	r.ownerTick()
	r.crash()
	r.wA, r.wB = ownerStale, guestSaved
	r.l.Repair(r.wB, fpB)
	if !r.l.Repair(r.wA, fpA) {
		t.Errorf("Repair reported no change for an owner World that predates the fuse")
	}
	var chips []DockChip
	for i := 0; i < 3; i++ {
		chips = append(chips, r.ownerTick()...)
		chips = append(chips, r.guestTick()...)
	}
	r.oneGuestVessel("after fuse + restart")
	if hasKind(chips, sim.SessionEventDockLost) {
		t.Errorf("the guest was told the dock was lost: %+v", chips)
	}
	if s := r.snap(); s.Phase != DockActive || s.Aborted {
		t.Errorf("record = phase %v aborted %v, want a live Active dock", s.Phase, s.Aborted)
	}
}

// 7.3 row 3: the owner's payload landed, only the ack flush was lost. The
// World agrees; Repair must not fuse a second copy, and drops the OwnerCopy.
func TestRepairDropsOwnerCopyWhenCompositePersisted(t *testing.T) {
	r := newRig(t, 303)
	r.guestTick()
	r.ownerTick()
	ownerSaved := viaDisk(t, r.wA)
	r.crash()
	r.wA = ownerSaved
	partsBefore := len(r.wA.Crafts[0].DockedComponents)
	r.l.Repair(r.wA, fpA)
	if s := r.snap(); s.OwnerCopy != nil {
		t.Errorf("OwnerCopy lingers after the owner's World proved to hold the Composite")
	}
	if got := len(r.wA.Crafts[0].DockedComponents); got != partsBefore {
		t.Errorf("owner Composite has %d components after Repair, want the %d it was saved with", got, partsBefore)
	}
	r.oneGuestVessel("after owner autosave + lost ack")
}

// 7.3 row 2 with the lead gone: the dock cannot be rebuilt, so the guest's
// Vessel comes back as a Parcel with the existing chip.
func TestRepairReturnsParcelWhenLeadIsGone(t *testing.T) {
	r := newRig(t, 304)
	r.guestTick()
	guestSaved := viaDisk(t, r.wB)
	r.ownerTick()
	r.crash()
	owner := newWorld(t)
	owner.Crafts = nil // an owner with no save of the lead
	r.wA, r.wB = owner, guestSaved
	r.l.Repair(r.wB, fpB)
	r.l.Repair(r.wA, fpA)
	var chips []DockChip
	for i := 0; i < 3; i++ {
		chips = append(chips, r.ownerTick()...)
		chips = append(chips, r.guestTick()...)
	}
	if hasKind(chips, sim.SessionEventDockLost) {
		t.Errorf("the dock was reaped as lost: %+v", chips)
	}
	if !hasKind(chips, sim.SessionEventParcelReturned) {
		t.Errorf("the guest was not told their Vessel came back: %+v", chips)
	}
	r.oneGuestVessel("after a dock that could not be rebuilt")
	if r.wB.ActiveCraft() == nil || r.wB.ActiveCraft().ID != r.xID {
		t.Errorf("the guest is not flying their Vessel again")
	}
}

// 7.4 row 2: the undock is in the ledger (X' in flight), the owner's payload
// still has the guest's parts.
func TestRepairStripsGuestPartsAfterUndock(t *testing.T) {
	r := newRig(t, 305)
	r.guestTick()
	r.ownerTick()
	r.ack()
	ownerStale := viaDisk(t, r.wA)
	r.l.RequestUndock(fpB, r.xID)
	r.ownerTick()
	r.crash()
	r.wA = ownerStale
	if !r.l.Repair(r.wA, fpA) {
		t.Errorf("Repair reported no change for an owner World still carrying handed-back parts")
	}
	r.settle()
	r.oneGuestVessel("after undock + restart")
}

// Same row, the owner released the guest while the guest was away (a Parcel).
func TestRepairStripsGuestPartsAfterReleaseToAbsentGuest(t *testing.T) {
	r := newRig(t, 306)
	r.guestTick()
	r.ownerTick()
	r.ack()
	ownerStale := viaDisk(t, r.wA)
	if ok, why := r.l.RequestRelease(fpA, func(string) bool { return false }); !ok {
		t.Fatalf("RequestRelease refused: %s", why)
	}
	r.ownerTick()
	r.crash()
	r.wA = ownerStale
	r.l.Repair(r.wA, fpA)
	var chips []DockChip
	for i := 0; i < 3; i++ {
		chips = append(chips, r.ownerTick()...)
		chips = append(chips, r.guestTick()...)
	}
	r.oneGuestVessel("after release to an absent guest + restart")
	if !hasKind(chips, sim.SessionEventParcelReturned) {
		t.Errorf("the Parcel arrived without its chip: %+v", chips)
	}
}

// 7.5 rows 2 and 3: the return was delivered into the guest's World and the
// guest's payload is stale (lacks X', or holds the pre-dock X); and the ack
// race, where the payload holds X' as flown and only the ledger flush was lost.
func TestRepairReadoptsReturnFromCopy(t *testing.T) {
	setup := func(t *testing.T) (*rig, *sim.World) {
		r := newRig(t, 307)
		pre := viaDisk(t, r.wB) // before the handover: has X
		r.guestTick()
		r.ownerTick()
		r.ack()
		r.l.RequestUndock(fpB, r.xID)
		r.ownerTick()
		r.guestTick() // delivered: GuestCopy, GuestDirty, Cooldown
		return r, pre
	}
	t.Run("payload lacks the Vessel", func(t *testing.T) {
		r := newRig(t, 307)
		r.guestTick()
		r.ownerTick()
		r.ack()
		guestSaved := viaDisk(t, r.wB) // no X
		r.l.RequestUndock(fpB, r.xID)
		r.ownerTick()
		r.guestTick()
		ownerSaved := viaDisk(t, r.wA)
		r.crash()
		r.wA, r.wB = ownerSaved, guestSaved
		r.l.Repair(r.wA, fpA)
		if !r.l.Repair(r.wB, fpB) {
			t.Errorf("Repair reported no change for a guest World lacking its returned Vessel")
		}
		r.oneGuestVessel("after return + restart, guest payload without X")
	})
	t.Run("payload holds the pre-dock Vessel", func(t *testing.T) {
		r, pre := setup(t)
		delivered, _, _ := r.wB.CraftByID(r.xID)
		wantFuel := delivered.Fuel
		pre.Crafts[0].Fuel = wantFuel + 777 // stale: not the delivered state
		ownerSaved := viaDisk(t, r.wA)
		r.crash()
		r.wA, r.wB = ownerSaved, pre
		r.l.Repair(r.wA, fpA)
		r.l.Repair(r.wB, fpB)
		r.oneGuestVessel("after return + restart, guest payload with the old X")
		if got, _, _ := r.wB.CraftByID(r.xID); got == nil || got.Fuel != wantFuel {
			t.Errorf("guest flies the stale Vessel, want the delivered one (fuel %v)", wantFuel)
		}
	})
}

// 7.5 row 3, pinned: the payload holds X' as flown after delivery and the ack
// flush was lost. The copy wins: the Vessel rolls back to delivery time. This
// is the accepted residual, not a bug (amendment 6).
func TestRepairPrefersCopyInAckRace(t *testing.T) {
	r := newRig(t, 308)
	r.guestTick()
	r.ownerTick()
	r.ack()
	r.l.RequestUndock(fpB, r.xID)
	r.ownerTick()
	r.guestTick()
	delivered, _, _ := r.wB.CraftByID(r.xID)
	wantFuel := delivered.Fuel
	guestSaved := viaDisk(t, r.wB)
	ownerSaved := viaDisk(t, r.wA)
	guestSaved.Crafts[0].Fuel = wantFuel - 50 // seconds of flying after the save
	r.crash()                                 // the ack flush never happened
	r.wA, r.wB = ownerSaved, guestSaved
	r.l.Repair(r.wA, fpA)
	r.l.Repair(r.wB, fpB)
	r.oneGuestVessel("ack race")
	if got, _, _ := r.wB.CraftByID(r.xID); got.Fuel != wantFuel {
		t.Errorf("fuel = %v, want the delivered copy's %v (residual 1: copy wins)", got.Fuel, wantFuel)
	}
}

// 7.6 row 2: [J] parked the Composite on the ledger; the old owner's payload
// still has it.
func TestRepairDropsSentCompositeAfterTransfer(t *testing.T) {
	r := newRig(t, 309)
	r.guestTick()
	r.ownerTick()
	r.ack()
	cid := r.snap().CompositeID
	ownerStale := viaDisk(t, r.wA)
	if ok, why := r.l.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer refused: %s", why)
	}
	r.ownerTick()
	r.crash()
	r.wA = ownerStale // A is the guest now
	if !r.l.Repair(r.wA, fpA) {
		t.Errorf("Repair reported no change for a World still flying the Composite it sent away")
	}
	r.settle()
	r.oneComposite("after transfer + restart", cid)
}

// 7.7 row 2: the recipient adopted the Composite; its payload predates that.
func TestRepairReadoptsCompositeAfterAdoption(t *testing.T) {
	r := newRig(t, 310)
	r.guestTick()
	r.ownerTick()
	r.ack()
	cid := r.snap().CompositeID
	if ok, why := r.l.RequestTransfer(fpA, allOnline); !ok {
		t.Fatalf("RequestTransfer refused: %s", why)
	}
	r.ownerTick()
	senderSaved := viaDisk(t, r.wA)    // the sender wrote after parking it
	recipientStale := viaDisk(t, r.wB) // the recipient has not written since
	r.guestTick()                      // adoption
	r.crash()
	r.wA, r.wB = senderSaved, recipientStale
	r.l.Repair(r.wA, fpA)
	if !r.l.Repair(r.wB, fpB) {
		t.Errorf("Repair reported no change for a recipient World that predates the adoption")
	}
	r.settle()
	r.oneComposite("after adoption + restart", cid)
}

// 7.10, O6: the guest was purged; the owner's payload still tags the parts.
func TestPurgedGuestPartsUntaggedOnStaleOwner(t *testing.T) {
	r := newRig(t, 311)
	r.guestTick()
	r.ownerTick()
	r.ack()
	ownerStale := viaDisk(t, r.wA)
	r.l.DropRecordsNaming(fpB)
	r.ownerTick() // the owner's tick adopts the parts and finishes the record
	r.crash()
	r.wA = ownerStale
	if !r.l.Repair(r.wA, fpA) {
		t.Errorf("Repair reported no change for a stale Composite still tagging a purged guest's parts")
	}
	c := r.wA.Crafts[0]
	if len(c.DockedComponents) < 2 {
		t.Fatalf("test setup: stale Composite has %d components", len(c.DockedComponents))
	}
	for _, dc := range c.DockedComponents {
		if dc.Owner != "" {
			t.Errorf("component %q still tagged %q", dc.Name, dc.Owner)
		}
	}
}

// Section 10, case 2: the repaired World was written, the ledger flush that
// followed was lost. Repairing again changes nothing in the World.
func TestRepairIsIdempotentAfterWorldWriteWithoutFlush(t *testing.T) {
	r := newRig(t, 312)
	r.guestTick()
	guestSaved := viaDisk(t, r.wB)
	ownerStale := viaDisk(t, r.wA)
	r.ownerTick()
	r.crash()
	flushed := r.l.FullRecords() // what is on disk: the record before any repair
	r.l.Repair(r.wB, fpB)
	r.l.Repair(ownerStale, fpA)
	repaired := viaDisk(t, ownerStale) // the World write that followed the repair
	// ... and the crash before the ledger flush:
	r.l = NewDockLedger()
	r.l.SeedFull(flushed, loadedSystems(t))
	r.wA, r.wB = repaired, guestSaved
	before := len(repaired.Crafts[0].DockedComponents)
	mass := repaired.Crafts[0].TotalMass()
	r.l.Repair(r.wA, fpA)
	r.l.Repair(r.wB, fpB)
	if got := len(r.wA.Crafts); got != 1 {
		t.Errorf("second repair left %d Vessels in the owner World, want 1", got)
	}
	if got := len(r.wA.Crafts[0].DockedComponents); got != before || r.wA.Crafts[0].TotalMass() != mass {
		t.Errorf("second repair changed the Composite: %d components (was %d), mass %v (was %v)",
			got, before, r.wA.Crafts[0].TotalMass(), mass)
	}
	r.oneGuestVessel("after a repeated repair")
}

// A seat whose World agrees (clean) is never touched, even when an unrelated
// Vessel there happens to reuse the guest Vessel's ID.
func TestRepairLeavesACleanSeatAlone(t *testing.T) {
	r := newRig(t, 313)
	r.guestTick()
	r.ownerTick()
	r.ack()
	v := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	v.ID = r.xID
	r.wB.AdoptCraft(v, false)
	if r.l.Repair(r.wB, fpB) {
		t.Errorf("Repair changed a World whose seat had acknowledged")
	}
	if _, _, ok := r.wB.CraftByID(r.xID); !ok {
		t.Errorf("an unrelated Vessel was removed from a clean World")
	}
}

// Residual 3 / section 7.4: UndockGuest on a stale Composite cannot refuse. The
// guest's parts are peelable on the stale Composite whenever they were on the
// live one, because the arrangement only ever loses peelability (a local dock
// on top, a transfer flips the tags) and a World that owns the stack always has
// the original orientation back. Checked here over the history with the most
// movement: fuse, [J] away, [J] back, then an undock; the owner's payload stale
// from each point it was written while owning the stack.
func TestRepairStripNeverRefusesOnAStaleComposite(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   int // which owned-stack payload survives the crash
	}{{"written after the fuse", 1}, {"written after the stack came back", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, 314)
			var stale *sim.World
			r.guestTick()
			r.ownerTick()
			r.ack()
			if tc.at == 1 {
				stale = viaDisk(t, r.wA)
			}
			if ok, why := r.l.RequestTransfer(fpA, allOnline); !ok {
				t.Fatalf("A -> B refused: %s", why)
			}
			r.ownerTick()
			r.guestTick()
			r.ack()
			if ok, why := r.l.RequestTransfer(fpB, allOnline); !ok {
				t.Fatalf("B -> A refused: %s", why)
			}
			r.guestTick()
			r.ownerTick()
			r.ack()
			if tc.at == 3 {
				stale = viaDisk(t, r.wA)
			}
			if !r.l.RequestUndock(fpB, r.xID) {
				t.Fatalf("test setup: no undock to ask for")
			}
			r.ownerTick()
			if r.snap().ReturnPayload == nil {
				t.Fatalf("test setup: the live undock refused")
			}
			r.crash()
			r.wA = stale
			if !r.l.Repair(r.wA, fpA) {
				t.Errorf("Repair did not strip the stale Composite (UndockGuest refused?)")
			}
			r.settle()
			r.oneGuestVessel("after undock on a round-tripped stack")
		})
	}
}
