package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestLocalReArmLatchHoldsDriftedPairApart is the #343 decision-2 regression
// test: the local Undock split now arms a same-World re-arm latch
// (mirroring ADR 0038 §5's cross-player DockCooldown) for every pair it just
// released. This covers the case geometry alone cannot: two of the
// restored craft drifting back inside BOTH docking gates for reasons that
// have nothing to do with the separation push (station-keeping error, a
// third body's perturbation, or simply flying them back together) must
// still not silently re-fuse the instant they touch.
func TestLocalReArmLatchHoldsDriftedPairApart(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 2-component composite")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("expected 2 craft after undock, got %d", len(w.Crafts))
	}

	// Simulate the pair drifting back together: park them well inside both
	// gates, matched velocity — exactly the state checkDocking looks for.
	a, b := w.Crafts[0], w.Crafts[1]
	b.State.R = a.State.R.Add(orbital.Vec3{X: 5})
	b.State.V = a.State.V

	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("checkDocking fused a latched pair that drifted back inside both gates — the local re-arm latch did not hold")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("slate count = %d after a latched drift-together, want 2 (no fuse)", len(w.Crafts))
	}

	// Once they separate past ReArmDistM, the latch clears and an ordinary
	// rendezvous (same position/velocity gates, no history) may fuse again.
	b.State.R = a.State.R.Add(orbital.Vec3{X: ReArmDistM + 10})
	b.State.V = a.State.V
	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("checkDocking fused while the pair is still beyond the proximity gate — unexpected match on distance alone")
	}

	// Prune runs at the top of checkDocking; separating past ReArmDistM
	// should have dropped the latch even though the pair is currently too
	// far apart to dock. Bring them back together now that the latch is
	// gone and confirm an ordinary re-dock succeeds.
	b.State.R = a.State.R.Add(orbital.Vec3{X: 5})
	b.State.V = a.State.V
	if _, _, ok := w.checkDocking(); !ok {
		t.Fatal("checkDocking refused to re-fuse a pair that had legitimately cleared the re-arm latch by separating past ReArmDistM")
	}
	if len(w.Crafts) != 1 {
		t.Errorf("slate count = %d after the cleared-latch re-dock, want 1", len(w.Crafts))
	}
}

// TestReArmDockingClearsTargetedLatch is the #372 acceptance test for the
// targeted case: with the just-undocked partner set as the Target, ReArmDocking
// clears exactly that pair's latch, names the partner, and — because nothing
// else about checkDocking's gates changed — a pair already sitting inside both
// proximity gates fuses on the very next tick.
func TestReArmDockingClearsTargetedLatch(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 2-component composite")
	}
	a, b := w.Crafts[0], w.Crafts[1]
	// Drift back together, matched velocity — exactly the state the issue
	// describes (undock, do nothing, settle back inside the gates).
	b.State.R = a.State.R.Add(orbital.Vec3{X: 5})
	b.State.V = a.State.V
	w.SetTargetCraft(1)

	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("precondition: checkDocking fused a freshly-latched pair")
	}

	partners, ok := w.ReArmDocking()
	if !ok {
		t.Fatal("ReArmDocking reported nothing cleared against a live, targeted latch")
	}
	if len(partners) != 1 || partners[0] != b.Name {
		t.Errorf("ReArmDocking partners = %v, want [%q]", partners, b.Name)
	}
	if len(w.localReArms) != 0 {
		t.Errorf("localReArms still holds %d entries after ReArmDocking cleared the only latch", len(w.localReArms))
	}

	// Nothing else about docking changed: the pair fuses on the very next
	// tick exactly as an un-latched approach would.
	if _, _, ok := w.checkDocking(); !ok {
		t.Fatal("checkDocking still refused a pair inside both gates after ReArmDocking cleared their latch")
	}
	if len(w.Crafts) != 1 {
		t.Errorf("slate count = %d after the re-armed re-dock, want 1", len(w.Crafts))
	}
}

// TestReArmDockingUntargetedClearsAllNamingActive: with no vessel targeted,
// ReArmDocking clears every latch naming the ACTIVE vessel — but leaves a
// latch between two OTHER craft untouched. A 3-component undock arms latches
// for every pair among the three restored components (0,1) (0,2) (1,2); the
// active vessel (component 0) is a party to the first two but not the third.
func TestReArmDockingUntargetedClearsAllNamingActive(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 3)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 3-component composite")
	}
	if len(w.Crafts) != 3 {
		t.Fatalf("expected 3 craft after undock, got %d", len(w.Crafts))
	}
	if len(w.localReArms) != 3 {
		t.Fatalf("expected 3 latches after a 3-way undock, got %d", len(w.localReArms))
	}
	active := w.ActiveCraft()
	if active != w.Crafts[0] {
		t.Fatal("precondition: active craft is not slate index 0")
	}

	// No target set — ClearTarget/zero-value World.Target is TargetNone.
	partners, ok := w.ReArmDocking()
	if !ok {
		t.Fatal("ReArmDocking reported nothing cleared with two live latches naming the active vessel")
	}
	if len(partners) != 2 {
		t.Errorf("ReArmDocking cleared %d latches, want 2 (every latch naming the active vessel)", len(partners))
	}
	if w.isLocalReArmed(active.ID, w.Crafts[1].ID) {
		t.Error("latch (active, craft1) still held after the untargeted re-arm")
	}
	if w.isLocalReArmed(active.ID, w.Crafts[2].ID) {
		t.Error("latch (active, craft2) still held after the untargeted re-arm")
	}
	// The pair NOT naming the active vessel must survive untouched.
	if !w.isLocalReArmed(w.Crafts[1].ID, w.Crafts[2].ID) {
		t.Error("latch (craft1, craft2) was cleared by an untargeted re-arm of the ACTIVE vessel — it names neither party")
	}
	if len(w.localReArms) != 1 {
		t.Errorf("localReArms has %d entries after the untargeted re-arm, want 1 (craft1, craft2) surviving", len(w.localReArms))
	}
}

// TestReArmDockingNoLatchSaysSo: pressing the key with nothing held must
// report ok=false rather than silently doing nothing — a dead keypress reads
// as a broken key (#372 acceptance).
func TestReArmDockingNoLatchSaysSo(t *testing.T) {
	w, _ := NewWorld()
	if partners, ok := w.ReArmDocking(); ok || len(partners) != 0 {
		t.Errorf("ReArmDocking() = (%v, %v) with no latch held, want (nil, false)", partners, ok)
	}

	// Still says so when a target IS set but no latch involves it.
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.SetTargetCraft(1)
	if partners, ok := w.ReArmDocking(); ok || len(partners) != 0 {
		t.Errorf("ReArmDocking() = (%v, %v) targeting an untangled craft, want (nil, false)", partners, ok)
	}
}

// TestReArmDockingClearedLatchStaysCleared: once ReArmDocking clears a latch,
// it must not come back on its own — only a fresh Undock arms a new one
// (#372 acceptance). Repeated pruning passes (via checkDocking) must not
// resurrect it.
func TestReArmDockingClearedLatchStaysCleared(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 2-component composite")
	}
	if _, ok := w.ReArmDocking(); !ok {
		t.Fatal("precondition: ReArmDocking found nothing to clear right after Undock")
	}
	if len(w.localReArms) != 0 {
		t.Fatalf("localReArms not empty after ReArmDocking: %d entries", len(w.localReArms))
	}

	// pruneLocalReArms runs at the top of every checkDocking pass — call it
	// several times (leaving the pair at Undock's post-separation distance,
	// outside both gates, so no fuse muddies the picture) and confirm the
	// cleared latch never reappears on its own.
	for i := 0; i < 3; i++ {
		w.checkDocking()
	}
	if len(w.localReArms) != 0 {
		t.Errorf("a cleared latch reappeared: localReArms has %d entries", len(w.localReArms))
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("slate count = %d, want 2 (still separated, no fuse expected)", len(w.Crafts))
	}
}

// TestLocalReArmLatchClearsAtCeiling: a latch that can never observe the
// pair separating (in this test, because they never actually move apart)
// must still release once ReArmCeiling of SIM-time has passed — mirroring
// the cross-player ceiling's #326 rationale, so a latch can never hold a
// locally-undocked pair un-dockable indefinitely.
func TestLocalReArmLatchClearsAtCeiling(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 2-component composite")
	}
	a, b := w.Crafts[0], w.Crafts[1]
	b.State.R = a.State.R.Add(orbital.Vec3{X: 5})
	b.State.V = a.State.V

	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("checkDocking fused a freshly-latched pair — latch should hold immediately after undock")
	}

	w.Clock.SimTime = w.Clock.SimTime.Add(ReArmCeiling + time.Minute)
	if _, _, ok := w.checkDocking(); !ok {
		t.Fatal("checkDocking still refused to fuse after ReArmCeiling of sim-time elapsed — latch never expires")
	}
	if len(w.Crafts) != 1 {
		t.Errorf("slate count = %d after the ceiling-expired re-dock, want 1", len(w.Crafts))
	}
}

// TestLocalReArmRefusalChipsOncePerLatch is the #372 acceptance test for
// part 3: checkDocking's refusal must surface as a chip-ready event naming
// the partner, and exactly once for the life of a given latch — a per-tick
// chip inside the gate would spam every frame the pair sits there (mirroring
// relay/dock.go's raiseReArmNotice one-shot semantics locally).
func TestLocalReArmRefusalChipsOncePerLatch(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 2-component composite")
	}
	a, b := w.Crafts[0], w.Crafts[1]
	b.State.R = a.State.R.Add(orbital.Vec3{X: 5})
	b.State.V = a.State.V

	if w.LastLocalReArmRefusal != nil {
		t.Fatal("precondition: a refusal event already exists before any refused checkDocking pass")
	}
	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("checkDocking fused a latched pair sitting inside both gates")
	}
	ev := w.LastLocalReArmRefusal
	if ev == nil {
		t.Fatal("checkDocking refused a latched pair inside both gates but raised no chip event")
	}
	if ev.PartnerName != b.Name {
		t.Errorf("refusal event PartnerName = %q, want %q", ev.PartnerName, b.Name)
	}

	// Simulate app.go consuming the chip (LastDockEvent's own pattern).
	w.LastLocalReArmRefusal = nil

	// Same still-latched pair, same gates — a second refused pass must NOT
	// raise a second chip.
	if _, _, ok := w.checkDocking(); ok {
		t.Fatal("checkDocking fused a still-latched pair on the second pass")
	}
	if w.LastLocalReArmRefusal != nil {
		t.Error("checkDocking raised a SECOND refusal chip for the same still-live latch — must be once per latch")
	}
}
