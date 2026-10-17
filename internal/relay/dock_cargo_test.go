package relay

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// cargoPair builds the case ADR 0057 conflict 2a exists for: A (the docker, so
// the stack owner) flies a station with a Cargo Hold; B is the passive craft, a
// Cargo Dragon, riding in A's stack. Returns the fused, reconciled pair.
func cargoPair(t *testing.T, ledger *DockLedger, store *Store, guestID uint64) (wA, wB *sim.World, reports map[string]CraftReport) {
	t.Helper()
	wA, wB = alignedPair(t, guestID)
	st, warn := spacecraft.ComposeStage([]string{"probe-core", "docking-port", "cargo-hold"}, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	a := wA.ActiveCraft()
	a.Stages = []spacecraft.Stage{st}
	a.SyncFields()
	a.State.M = a.TotalMass()
	b := wB.ActiveCraft()
	b.Stages = spacecraft.NewFromLoadout("Cargo-Dragon").Stages
	b.SyncFields()
	b.State.M = b.TotalMass()

	now := time.Now()
	if _, ok := ledger.Claim(fpA, "alice", a.ID, fpB, "bob", guestID); !ok {
		t.Fatal("Claim refused")
	}
	reports = reportMap(store, wA, wB, now)
	ledger.Reconcile(wB, fpB, reports)
	ledger.Reconcile(wA, fpA, reports)
	if len(wA.Crafts) != 1 || !sim.StackHasGuest(wA.Crafts[0]) {
		t.Fatalf("A did not fuse a cross-player stack")
	}
	wA.RecomputeCommGraph()
	return wA, wB, reports
}

func holdAndCargo(c *spacecraft.Spacecraft) (cargo, hold float64) {
	for _, s := range c.Stages {
		cargo += s.CargoKg
		hold += s.HoldKg
	}
	return
}

func detailOf(chips []DockChip, kind sim.SessionEventKind) string {
	for _, c := range chips {
		if c.Kind == kind {
			return c.Detail
		}
	}
	return "<none>"
}

// ADR 0057 conflict 2a, through the real ledger and two Worlds: the Dragon's
// owner is the rider. B asks, A's tick moves the cargo into A's hold, B's tick
// states the mass; nothing moved without the ask; the ask is single-shot.
func TestRiderHandsOverCargoThroughTheLedger(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 930
	wA, wB, reports := cargoPair(t, ledger, store, guestID)
	stack := wA.Crafts[0]
	massBefore := stack.TotalMass()

	// No ask yet: ticks move nothing.
	ledger.Reconcile(wA, fpA, reports)
	if cargo, hold := holdAndCargo(stack); cargo != 3307 || hold != 0 {
		t.Fatalf("cargo moved with no ask: %g / %g", cargo, hold)
	}

	if !ledger.RequestHandOver(fpB, guestID) {
		t.Fatal("rider's hand-over ask refused with an active dock")
	}
	ledger.Reconcile(wA, fpA, reports)
	cargo, hold := holdAndCargo(stack)
	if cargo != 0 || hold != 3307 {
		t.Fatalf("after the owner's tick: cargo %g hold %g, want 0 / 3307", cargo, hold)
	}
	if math.Abs(stack.TotalMass()-massBefore) > 1e-6 {
		t.Errorf("mass %g -> %g", massBefore, stack.TotalMass())
	}
	chips := ledger.Reconcile(wB, fpB, reports)
	if got := detailOf(chips, sim.SessionEventHandOver); got != "3.307 t cargo handed over" {
		t.Errorf("rider's chip = %q, want 3.307 t cargo handed over", got)
	}
	if again := ledger.Reconcile(wB, fpB, reports); hasChip(again, sim.SessionEventHandOver) {
		t.Errorf("the answer repeated on the next tick: %+v", again)
	}

	// A second press has nothing of the rider's aboard.
	if !ledger.RequestHandOver(fpB, guestID) {
		t.Fatal("second ask refused")
	}
	ledger.Reconcile(wA, fpA, reports)
	chips = ledger.Reconcile(wB, fpB, reports)
	if got := detailOf(chips, sim.SessionEventHandOver); got != "no cargo aboard" {
		t.Errorf("second chip = %q, want no cargo aboard", got)
	}
	if _, hold := holdAndCargo(stack); hold != 3307 {
		t.Errorf("hold changed to %g", hold)
	}

	// The Dragon goes home empty-handed; the owner keeps the hold.
	if !ledger.RequestUndock(fpB, guestID) {
		t.Fatal("undock ask refused")
	}
	ledger.Reconcile(wA, fpA, reports)
	ledger.Reconcile(wB, fpB, reports)
	got, _, ok := wB.CraftByID(guestID)
	if !ok {
		t.Fatal("the Dragon did not come home")
	}
	if c, h := holdAndCargo(got); c != 0 || h != 0 || got.Cargo != 0 {
		t.Errorf("Dragon home with cargo %g hold %g Cargo %g, want 0", c, h, got.Cargo)
	}
	if _, h := holdAndCargo(wA.Crafts[0]); h != 3307 {
		t.Errorf("owner's craft after undock: hold %g, want 3307", h)
	}
}

// Only a rider with an active dock can ask: the stack owner has the key
// locally, and a stranger has nothing to hand over.
func TestRequestHandOverNeedsAnActiveGuestDock(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 931
	if ledger.RequestHandOver(fpB, guestID) {
		t.Error("ask accepted with no dock at all")
	}
	cargoPair(t, ledger, store, guestID)
	if ledger.RequestHandOver(fpA, guestID) {
		t.Error("the stack owner's seat was accepted as a rider")
	}
	if ledger.RequestHandOver(fpB, guestID+1) {
		t.Error("an ask for a craft that is not docked was accepted")
	}
}

// The ask is durable like every other ledger request (ADR 0040): a server
// restart between the press and the owner's next tick must not eat it.
func TestHandOverAskSurvivesARestart(t *testing.T) {
	store := NewStore()
	ledger := NewDockLedger()
	const guestID = 932
	wA, wB, reports := cargoPair(t, ledger, store, guestID)
	if !ledger.RequestHandOver(fpB, guestID) {
		t.Fatal("ask refused")
	}
	fresh := restart(t, ledger)
	fresh.Reconcile(wA, fpA, reports)
	if _, hold := holdAndCargo(wA.Crafts[0]); hold != 3307 {
		t.Fatalf("hold = %g after the restart, want the ask honoured (3307)", hold)
	}
	chips := fresh.Reconcile(wB, fpB, reports)
	if !hasChip(chips, sim.SessionEventHandOver) {
		t.Errorf("rider heard nothing: %+v", chips)
	}
}
