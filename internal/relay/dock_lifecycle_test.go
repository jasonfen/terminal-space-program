package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLiveUndockPushesClearOfTheStack (#304, zero subspace skew): a live
// (both-present) cross-player undock — the guest-ask path — must clear the
// stack by the same 75 m / 0.15 m/s push the Parcel path already carries.
// Pre-fix, this path had NO push at all: the returned craft landed at
// exactly the stack's position.
func TestLiveUndockPushesClearOfTheStack(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1002
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	stackR := wA.Crafts[0].State.R

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)

	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not get craft %d back", guestID)
	}
	d := got.State.R.Sub(stackR).Norm()
	if d <= sim.DockingDistM {
		t.Errorf("returned craft is %v m from the stack — inside the %v m docking gate, no push applied", d, sim.DockingDistM)
	}
	// Bound the OTHER direction too: with zero subspace skew this should be
	// JUST the push, not a huge propagated jump (that's the next test).
	if d > 500 {
		t.Errorf("returned craft is %v m from the stack with zero skew — expected just the separation push", d)
	}
}

// TestLiveUndockPlacesAcrossSubspaceGap (#304, nonzero subspace skew): the
// guest's own World can be a few seconds ahead of or behind the docker's when
// the split lands — the ordinary case, since subspace clocks are only
// guaranteed equal within co-warp tolerance. The returned craft must be
// Kepler-propagated to the GUEST's own sim-time, not adopted verbatim at the
// docker's. At LEO speed a few seconds of unpropagated skew is kilometres of
// error (the exact magnitude measured live) — so a large gap should produce
// a displacement far bigger than the separation push alone.
func TestLiveUndockPlacesAcrossSubspaceGap(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1003
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	stackR := wA.Crafts[0].State.R

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	// B's subspace clock runs 5s ahead of A's by the time the split lands.
	wB.Clock.SimTime = wA.Clock.SimTime.Add(5 * time.Second)

	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports) // A splits the guest out (at A's sim-time)
	ledger.Reconcile(wB, fpB, reports) // B receives it, placed at B's OWN sim-time

	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not get craft %d back", guestID)
	}
	d := got.State.R.Sub(stackR).Norm()
	// 5 s at LEO orbital speed (~km/s) is kilometres — far more than the 75 m
	// push alone could produce. A verbatim (unpropagated) copy would land at
	// ~75 m, same as the zero-skew case above.
	if d < 1000 {
		t.Errorf("returned craft displaced only %v m across a 5s subspace gap — not Kepler-propagated", d)
	}
}

// TestUndockSetsMutualTargets (ADR 0038 §6): at handback the docker's target
// slot holds the returned craft and the guest's holds the stack they just
// left — range and closure readable from the first frame, rather than
// starting blind (pre-fix, undock cleared the guest's target selection
// entirely).
func TestUndockSetsMutualTargets(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1004
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	compositeID := wA.Crafts[0].ID

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)

	if wA.Target.Kind != sim.TargetGhost || wA.Target.GhostOwner != fpB || wA.Target.CraftID != guestID {
		t.Errorf("docker target = %+v, want a ghost target at (%s, %d)", wA.Target, fpB, guestID)
	}
	if wB.Target.Kind != sim.TargetGhost || wB.Target.GhostOwner != fpA || wB.Target.CraftID != compositeID {
		t.Errorf("guest target = %+v, want a ghost target at (%s, %d)", wB.Target, fpA, compositeID)
	}
}

// TestUndockTargetsTheStackWithAMultiVesselGuest is #327: ADR 0038 §6 on the
// guest side worked only by accident. SetTargetGhost ran BEFORE AdoptCraft,
// and AdoptCraft(_, true) ends in SetActiveCraftIdx, whose last act is to
// reload w.Target from the INCOMING craft — a restored component with a zero
// Target. With a one-vessel guest the outgoing craft IS the incoming one, so
// the ghost target is checkpointed and read straight back; with two vessels
// the ghost target lands on the guest's OTHER vessel (silently replacing
// whatever it was aimed at) and the guest undocks blind. Very reachable:
// DetectGuestContact scans every ghost a guest owns, so a docker can claim a
// vessel the guest isn't even flying.
func TestUndockTargetsTheStackWithAMultiVesselGuest(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1009
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	// B owns a second vessel and is flying it while the first rides in A's
	// stack. It is aimed at a body of its own, which the undock must not touch.
	second := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	second.Primary = wB.ActiveCraft().Primary
	second.State = wB.ActiveCraft().State
	wB.AdoptCraft(second, false)
	secondTarget := spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: 1}
	second.Target = secondTarget
	secondID := second.ID

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports) // B hands craft 1 over
	ledger.Reconcile(wA, fpA, reports) // A fuses
	compositeID := wA.Crafts[0].ID

	// B flies its second vessel while docked-as-guest.
	_, idx, ok := wB.CraftByID(secondID)
	if !ok {
		t.Fatalf("B lost its second vessel on handover")
	}
	wB.SetActiveCraftIdx(idx)

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)

	if wB.Target.Kind != sim.TargetGhost || wB.Target.GhostOwner != fpA || wB.Target.CraftID != compositeID {
		t.Errorf("guest target = %+v, want a ghost target at (%s, %d) — the returned vessel undocked blind", wB.Target, fpA, compositeID)
	}
	if second.Target != secondTarget {
		t.Errorf("the guest's OTHER vessel had its target overwritten: %+v, want %+v", second.Target, secondTarget)
	}
}

// TestReturnAtNanoSurvivesRestart (#304, durability): the release-time stamp
// a live undock's subspace-gap placement reads at delivery must round-trip
// the same way, or a restart between the split and the guest's own tick
// drops the stamp and the returned craft is adopted verbatim again.
func TestReturnAtNanoSurvivesRestart(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1008
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	stackR := wA.Crafts[0].State.R

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	wB.Clock.SimTime = wA.Clock.SimTime.Add(5 * time.Second)
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports) // A splits the guest out, stamps returnAtNano

	// The server restarts before B's own tick ever delivers the return.
	fresh := restart(t, ledger)

	guestChips := fresh.Reconcile(wB, fpB, reports)
	if !hasChip(guestChips, sim.SessionEventUndocked) {
		t.Fatalf("no undocked chip after restart: %+v", guestChips)
	}
	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not get craft %d back after restart", guestID)
	}
	if d := got.State.R.Sub(stackR).Norm(); d < 1000 {
		t.Errorf("returned craft displaced only %v m across the 5s gap — returnAtNano did not survive the restart", d)
	}
}

// ADR 0038 S1/S2/S3 (#301, #303, #304): the lifecycle around the dock/undock
// fuse, generalising ADR 0040's Parcel-only safe-handback and subspace-gap
// helpers to the live cross-player path, plus the re-arm-by-leaving latch.

// TestFuseChipsTheAbsorbedGuestToo is #301, live: the docker's own reconcile
// already got a "docked" chip when the stack fused (TestCrossPlayerDockHandshakeAndUndock
// pins that). What was missing is the ABSORBED party's own moment — measured
// on the 2026-08-02 session as a total UI blank with no chip of any kind on
// the passive seat. The guest's own next reconcile, once the fuse has
// happened on the owner's side, must carry its own "docked" chip.
func TestFuseChipsTheAbsorbedGuestToo(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1001
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	if _, ok := ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID); !ok {
		t.Fatalf("Claim refused")
	}
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports) // B hands its craft over — no chip yet, nothing happened to B yet

	ownerChips := ledger.Reconcile(wA, fpA, reports) // A fuses the stack
	if !hasChip(ownerChips, sim.SessionEventDocked) {
		t.Fatalf("owner got no docked chip: %+v", ownerChips)
	}

	// B's own next reconcile must carry the moment that just happened to it:
	// its craft joined someone else's stack.
	guestChips := ledger.Reconcile(wB, fpB, reports)
	if !hasChip(guestChips, sim.SessionEventDocked) {
		t.Errorf("the absorbed guest got no docked chip at fuse (#301): %+v", guestChips)
	}
}

// TestFuseNoticeSurvivesRestart (#301, durability): the absorbed guest's
// pending "docked" chip is owed at fuse, but the guest might not tick again
// before a restart. DockNotice has to round-trip through the session
// directory the same way every other in-flight flag on the record does
// (ADR 0040 §1), or a restart in that narrow window drops the guest's own
// moment silently — the exact kind of silence #301 was about.
func TestFuseNoticeSurvivesRestart(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1007
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports) // B hands its craft over
	ledger.Reconcile(wA, fpA, reports) // A fuses — dockNotice set for B

	// The server restarts before B's own next tick ever consumes the notice.
	fresh := restart(t, ledger)

	guestChips := fresh.Reconcile(wB, fpB, reports)
	if !hasChip(guestChips, sim.SessionEventDocked) {
		t.Errorf("the absorbed guest's fuse notice did not survive a restart: %+v", guestChips)
	}
}

// TestLiveUndockAlwaysReturnsASafedCraft (#303) extends the Parcel-only
// safing (TestOwnerReleasesAnAbsentGuestAsAParcel) to the LIVE cross-player
// path: the guest's craft comes back inert regardless of what the docker had
// dialled in while flying the stack. Measured repro: 10% throttle, RCS,
// Target Retrograde inherited from the docker onto a craft the guest had left
// at 100%/main/prograde.
func TestLiveUndockAlwaysReturnsASafedCraft(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1005
	wA, wB := alignedPair(t, guestID)
	now := time.Now()

	ledger.Claim(fpA, "alice", wA.ActiveCraft().ID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)

	// The docker flies the fused stack with a different control setup than
	// the guest ever chose.
	stack := wA.Crafts[0]
	stack.Throttle = 0.1
	stack.EngineMode = spacecraft.EngineRCS
	stack.AttitudeMode = spacecraft.BurnTargetRetrograde
	stack.ManualBurn = &spacecraft.ManualBurn{StartTime: wA.Clock.SimTime}

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)

	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatalf("B did not get craft %d back", guestID)
	}
	if got.Throttle != 0 {
		t.Errorf("returned craft throttle = %v, want 0", got.Throttle)
	}
	if got.EngineMode != spacecraft.EngineMain {
		t.Errorf("returned craft engine mode = %v, want main", got.EngineMode)
	}
	if got.AttitudeMode != spacecraft.BurnPrograde {
		t.Errorf("returned craft attitude hold = %v, want the neutral default", got.AttitudeMode)
	}
	if got.ActiveBurn != nil || got.ManualBurn != nil {
		t.Errorf("returned craft arrived with a burn running")
	}
}

// TestReArmLatchBlocksImmediateReclaimUntilSeparation (ADR 0038 §5): undocking
// disarms auto-dock with that partner (same craft pair) until they back away
// past ReArmDistM. Pre-fix there is no latch at all — the ledger record is
// torn down completely the instant the guest receives its craft, so a fresh
// Claim on the exact same pair succeeds immediately, which is exactly the
// silent-instant-re-fuse risk the ADR calls out.
func TestReArmLatchBlocksImmediateReclaimUntilSeparation(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1006
	wA, wB := alignedPair(t, guestID)
	dockerID := wA.ActiveCraft().ID
	now := time.Now()

	ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID)
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)

	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("RequestUndock refused")
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)

	// Immediately drifting back within the gates (the "pilot still setting up
	// the safed ship" scenario): a fresh claim on the SAME pair must refuse.
	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); ok {
		t.Fatalf("re-claim succeeded before the pair backed away — the re-arm latch did not hold")
	}

	// Separate for real, well past ReArmDistM, and let the owner's side
	// notice on its next reconcile.
	guestIdx := -1
	for i, c := range wB.Crafts {
		if c != nil && c.ID == guestID {
			guestIdx = i
		}
	}
	if guestIdx < 0 {
		t.Fatalf("guest craft not found in B's slate")
	}
	wB.Crafts[guestIdx].State.R = wB.Crafts[guestIdx].State.R.Add(orbital.Vec3{X: 2 * sim.ReArmDistM})

	reports = reportMap(store, wA, wB, now.Add(2*time.Second))
	ledger.Reconcile(wA, fpA, reports) // owner side notices the separation, clears the latch

	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); !ok {
		t.Fatalf("re-claim still refused after backing away past the re-arm distance")
	}
}
