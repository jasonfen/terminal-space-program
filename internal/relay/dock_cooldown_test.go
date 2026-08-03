package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The ADR 0038 §5 re-arm latch, hardened (#326). The latch itself is pinned by
// TestReArmLatchBlocksImmediateReclaimUntilSeparation; what these cover is the
// four ways a latch could outlive its purpose and brick a vessel — the #312
// shape ADR 0040 was written to close.

const fpC = "SHA256:carol"

// cooledPair runs a full dock + undock between A and B and returns the two
// Worlds with a standing Cooldown latch in the ledger, plus the docker's craft
// ID. Every test below starts from exactly the state a live undock leaves.
func cooledPair(t *testing.T, ledger *DockLedger, store *Store, guestID uint64) (wA, wB *sim.World, dockerID uint64) {
	t.Helper()
	wA, wB = alignedPair(t, guestID)
	dockerID = wA.ActiveCraft().ID
	now := time.Now()

	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); !ok {
		t.Fatalf("test setup: Claim refused")
	}
	reports := reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatalf("test setup: RequestUndock refused")
	}
	reports = reportMap(store, wA, wB, now.Add(time.Second))
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)

	recs := ledger.Records()
	if len(recs) != 1 || recs[0].Phase != DockCooldown {
		t.Fatalf("test setup: ledger after undock = %+v, want one Cooldown latch", recs)
	}
	return wA, wB, dockerID
}

// TestCooldownLatchDoesNotBlockAThirdParty is #326 defect 1. Claim refused when
// EITHER endpoint of any record matched, and that guard now also sees Cooldown
// records — so one pair's re-arm latch disabled docking for both of their craft
// against ANYONE. ADR 0038 §5 disarms auto-dock "with that partner"; a latch
// must never be a third player's problem.
func TestCooldownLatchDoesNotBlockAThirdParty(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1101
	_, _, dockerID := cooledPair(t, ledger, store, guestID)

	// The latch still holds against the pair it names.
	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); ok {
		t.Fatalf("the latch let its own pair re-claim immediately")
	}

	// Carol, who was never part of that dock, closes on A's craft.
	const carolCraftID = 9001
	if _, ok := ledger.Claim(fpC, "carol", carolCraftID, fpA, "alice", dockerID); !ok {
		t.Errorf("a third player's dock on A was refused by A and B's re-arm latch")
	}
}

// TestCooldownLatchExpiresAtItsCeiling is #326 defect 3. reconcileCooldown only
// clears on a POSITIVE range reading, and every unresolvable case — the partner
// landed, in another system, in another SOI, or simply absent from the report
// set — holds the latch. So the ordinary reason to undock (one of you leaves)
// is exactly the case that can never clear it. Measured production shape: B
// does a TLI after the undock and A's craft is un-dockable until --reset-fleet.
func TestCooldownLatchExpiresAtItsCeiling(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1102
	wA, _, dockerID := cooledPair(t, ledger, store, guestID)

	// The partner is unresolvable from here — no report at all, the same
	// answer a craft in another SOI or another system produces.
	none := map[string]CraftReport{}
	ledger.Reconcile(wA, fpA, none)
	if len(ledger.Records()) != 1 {
		t.Fatalf("the latch cleared against an unresolvable partner — it should hold until the ceiling")
	}

	// Well past the ceiling of sim-time later, it must let go regardless.
	wA.Clock.SimTime = wA.Clock.SimTime.Add(sim.ReArmCeiling + time.Minute)
	ledger.Reconcile(wA, fpA, none)
	if recs := ledger.Records(); len(recs) != 0 {
		t.Fatalf("the latch outlived its ceiling with an unresolvable partner: %+v", recs)
	}
	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); !ok {
		t.Errorf("re-claim still refused after the latch's ceiling expired")
	}
}

// TestCooldownCeilingSurvivesARestart is the durability half of defect 3: the
// record is durable, so the ceiling has to be too. A restart re-seeds the latch
// from disk with an empty in-memory report store, which is the very state that
// holds the latch forever — if the ceiling stamp doesn't round-trip, the
// restart resets the clock on an already-doomed latch every hour.
func TestCooldownCeilingSurvivesARestart(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1103
	wA, _, _ := cooledPair(t, ledger, store, guestID)

	fresh := restart(t, ledger)
	if len(fresh.Records()) != 1 {
		t.Fatalf("the latch did not survive the restart at all")
	}
	wA.Clock.SimTime = wA.Clock.SimTime.Add(sim.ReArmCeiling + time.Minute)
	fresh.Reconcile(wA, fpA, map[string]CraftReport{})
	if recs := fresh.Records(); len(recs) != 0 {
		t.Errorf("the latch's ceiling did not survive the restart: %+v", recs)
	}
}

// TestCooldownRefusalTellsThePilotOnce is #326 defect 4. DetectGuestContact
// fires every tick, so a latched pair sitting inside the gates refuses a Claim
// every tick with nothing on screen — "a refused action tells you nothing", at
// the one verb with no key to press. The pilot gets told, and told once: a
// chip per tick would be its own kind of broken.
func TestCooldownRefusalTellsThePilotOnce(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 1104
	wA, _, dockerID := cooledPair(t, ledger, store, guestID)
	none := map[string]CraftReport{}

	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); ok {
		t.Fatalf("test setup: the latch did not refuse")
	}
	chips := ledger.Reconcile(wA, fpA, none)
	if len(chips) != 1 || chips[0].Detail == "" {
		t.Fatalf("a latch refusal produced no explanation: %+v", chips)
	}

	// Still drifting inside the gates: the detector claims again every tick,
	// and the pilot must not get the same chip again every tick.
	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID); ok {
		t.Fatalf("the latch stopped refusing")
	}
	if again := ledger.Reconcile(wA, fpA, none); len(again) != 0 {
		t.Errorf("the latch refusal chipped a second time: %+v", again)
	}
}

// TestUnknownDockPhaseSelfRetires is #337. DockLink.Phase crosses session.json
// as a bare int, so a binary rolled back past the release that introduced a
// phase value gets seeded with a number it has no case for: reconcileOwner and
// reconcileGuest both fall through, nothing ever ends the record, and Claim's
// engaged-record guard refuses BOTH craft forever — an immortal record no
// reaper can reach. A phase this binary cannot advance is a record it cannot
// honour, so it retires rather than bricking the pair.
func TestUnknownDockPhaseSelfRetires(t *testing.T) {
	ledger := NewDockLedger()
	w := newWorld(t)
	const dockerID = 4001
	w.ActiveCraft().ID = dockerID

	ledger.SeedFull([]DockSnapshot{{
		ID: 5, Owner: fpA, OwnerHandle: "alice", DockerCraftID: dockerID,
		CompositeID: dockerID, GuestOwner: fpB, GuestHandle: "bob",
		GuestCraftID: 4002, Phase: DockPhase(99),
	}}, loadedSystems(t))

	ledger.Reconcile(w, fpA, map[string]CraftReport{})
	if recs := ledger.Records(); len(recs) != 0 {
		t.Fatalf("a record in an unhandled phase outlived a reconcile: %+v", recs)
	}
	if _, ok := ledger.Claim(fpA, "alice", dockerID, fpB, "bob", 4002); !ok {
		t.Errorf("the pair is still un-dockable after the unhandled record retired")
	}
}

// TestUnknownDockPhaseHoldingACraftIsKept is the ADR 0040 §1 exception to the
// retirement above: a record parking a payload is holding the ONLY copy of a
// craft. Retiring it would destroy the vessel that durability exists to save,
// and an immortal record is recoverable by running a binary that understands
// the phase again — a deleted craft is not.
func TestUnknownDockPhaseHoldingACraftIsKept(t *testing.T) {
	ledger := NewDockLedger()
	w := newWorld(t)
	const dockerID = 4003
	w.ActiveCraft().ID = dockerID
	wire := save.CraftToWire(w.ActiveCraft())

	ledger.SeedFull([]DockSnapshot{{
		ID: 6, Owner: fpA, OwnerHandle: "alice", DockerCraftID: dockerID,
		CompositeID: dockerID, GuestOwner: fpB, GuestHandle: "bob",
		GuestCraftID: 4004, Phase: DockPhase(99), ReturnPayload: &wire,
	}}, loadedSystems(t))

	ledger.Reconcile(w, fpA, map[string]CraftReport{})
	if recs := ledger.Records(); len(recs) != 1 {
		t.Errorf("an unhandled record holding the only copy of a craft was reaped: %+v", recs)
	}
}
