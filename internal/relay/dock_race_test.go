package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Audit L2-5: the guest's [U] and the owner's u land in the same instant. The
// undock wins; the release that raced it must not chip "release refused" for
// a vessel that is already on its way back.
func TestSameInstantUndockAndReleaseDoesNotChipRefused(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1101
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	if ok, why := ledger.RequestRelease(fpA, allOnline); !ok {
		t.Fatalf("RequestRelease refused: %s", why)
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	chips := ledger.Reconcile(wA, fpA, reports)
	chips = append(chips, ledger.Reconcile(wA, fpA, reports)...) // owner's next tick, before the guest's
	if !hasChip(chips, sim.SessionEventUndocked) {
		t.Fatalf("owner saw no undocked chip: %+v", chips)
	}
	if hasChip(chips, sim.SessionEventReleaseRefused) {
		t.Errorf("release refused chip after the undock already happened: %+v", chips)
	}
	ledger.Reconcile(wB, fpB, reports)
	if _, _, ok := wB.CraftByID(guestID); !ok {
		t.Errorf("guest did not get its vessel back")
	}
}

// Audit L2-7: the owner flies a second vessel (a Moon probe) while the stack
// coasts. The guest's undock aims the STACK at the guest's vessel and leaves
// the probe's target alone.
func TestGuestUndockTargetsFromTheStackNotTheOwnersActiveVessel(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1102
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	probe := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	probe.Primary = wA.ActiveCraft().Primary
	probe.State = wA.ActiveCraft().State
	wA.AdoptCraft(probe, false)
	probeTarget := spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: 1}
	probe.Target = probeTarget

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	stackID := wA.Crafts[0].ID
	_, pi, ok := wA.CraftByID(probe.ID)
	if !ok {
		t.Fatalf("owner lost the probe")
	}
	wA.SetActiveCraftIdx(pi) // flying the probe; the stack is not active
	wA.Target = probeTarget

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports)

	if probe.Target != probeTarget || wA.Target != probeTarget {
		t.Errorf("probe target clobbered: craft %+v, world %+v, want %+v", probe.Target, wA.Target, probeTarget)
	}
	_, si, _ := wA.CraftByID(stackID)
	want := spacecraft.Target{Kind: sim.TargetGhost, CraftID: guestID, GhostOwner: fpB}
	if got := wA.Crafts[si].Target; got != want {
		t.Errorf("stack target = %+v, want %+v", got, want)
	}
}
