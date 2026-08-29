package save

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestCraftFromWireTearsDownOwnerlessTargetRelativeBurn — #294 review
// round 3 finding D (second half). A v9 save (schema < 10, written
// before CraftToWire started preserving ghost refs) saved mid-ghost-burn
// carries an ActiveBurn whose Mode is target-relative but whose
// TargetCraftID / TargetGhostOwner were dropped by the old
// drop-on-save behavior — migrateV9PayloadToV10 is an identity
// transform (save_migrate_v9_to_v10.go), so that shape reaches
// CraftFromWire completely unchanged on load. The same shape is also
// exactly what a pre-round-3 reconcileTargetLock give-up used to leave
// behind (strip the ref, keep the burn "alive"). Either way, a
// target-relative burn with no ref can never resolve, thrust, or tear
// itself down again — nodeTargetRelState refuses unconditionally for
// craftID==0, so activeBurnTargetReady never returns true, burnExhausted
// never fires, and the zombie burn wedges canKeplerStep's per-craft gate
// (warp stays clamped ≤10× for the rest of the session). CraftFromWire
// must tear it down defensively at load instead of letting it load in
// that state.
func TestCraftFromWireTearsDownOwnerlessTargetRelativeBurn(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	wc := CraftToWire(c)
	wc.ActiveBurn = &ActiveBurn{
		Mode:        int(spacecraft.BurnTarget), // target-relative
		DVRemaining: 100,
		EndTimeNano: w.Clock.SimTime.Add(time.Minute).UnixNano(),
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
		// TargetCraftID / TargetGhostOwner both zero — the exact shape
		// the old drop-on-save behavior (and a pre-round-3 give-up) left
		// behind.
	}

	loaded, err := CraftFromWire(wc, systems)
	if err != nil {
		t.Fatalf("CraftFromWire: %v", err)
	}
	if loaded.ActiveBurn != nil {
		t.Errorf("ownerless target-relative burn survived load: %+v", loaded.ActiveBurn)
	}
}

// TestCraftFromWireKeepsResolvableTargetRelativeBurn — the defensive
// teardown above must be surgical: a target-relative burn that DOES
// carry a bound ref (local or ghost) loads unchanged.
func TestCraftFromWireKeepsResolvableTargetRelativeBurn(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	wc := CraftToWire(c)
	wc.ActiveBurn = &ActiveBurn{
		Mode:          int(spacecraft.BurnTarget),
		DVRemaining:   100,
		EndTimeNano:   w.Clock.SimTime.Add(time.Minute).UnixNano(),
		PrimaryID:     c.Primary.ID,
		Throttle:      1,
		TargetCraftID: 987654,
	}

	loaded, err := CraftFromWire(wc, systems)
	if err != nil {
		t.Fatalf("CraftFromWire: %v", err)
	}
	if loaded.ActiveBurn == nil {
		t.Fatal("resolvable target-relative burn torn down on load")
	}
	if loaded.ActiveBurn.TargetCraftID != 987654 {
		t.Errorf("TargetCraftID = %d, want 987654", loaded.ActiveBurn.TargetCraftID)
	}
}

// TestCraftFromWireLeavesNonTargetBurnAlone — regression guard: a
// non-target-relative ActiveBurn (e.g. a plain prograde burn) with no
// ref is entirely ordinary — it must never be torn down by the
// defensive teardown above, which only fires for target-relative modes.
func TestCraftFromWireLeavesNonTargetBurnAlone(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	wc := CraftToWire(c)
	wc.ActiveBurn = &ActiveBurn{
		Mode:        int(spacecraft.BurnPrograde),
		DVRemaining: 100,
		EndTimeNano: w.Clock.SimTime.Add(time.Minute).UnixNano(),
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
	}

	loaded, err := CraftFromWire(wc, systems)
	if err != nil {
		t.Fatalf("CraftFromWire: %v", err)
	}
	if loaded.ActiveBurn == nil {
		t.Fatal("ordinary non-target-relative burn torn down on load")
	}
}
