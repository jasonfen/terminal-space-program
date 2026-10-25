package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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
	placePortToPort(a, b, 0.3)

	if startsMagnet(w) {
		t.Fatal("checkDocking fused a latched pair that drifted back inside both gates — the local re-arm latch did not hold")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("slate count = %d after a latched drift-together, want 2 (no fuse)", len(w.Crafts))
	}

	// Once they separate past ReArmDistM, the latch clears and an ordinary
	// rendezvous (same position/velocity gates, no history) may fuse again.
	placePortToPort(a, b, ReArmDistM+10)
	if startsMagnet(w) {
		t.Fatal("checkDocking fused while the pair is still beyond the proximity gate — unexpected match on distance alone")
	}

	// Prune runs at the top of checkDocking; separating past ReArmDistM
	// should have dropped the latch even though the pair is currently too
	// far apart to dock. Bring them back together now that the latch is
	// gone and confirm an ordinary re-dock succeeds.
	placePortToPort(a, b, 0.3)
	if !startsMagnet(w) {
		t.Fatal("checkDocking refused to re-fuse a pair that had legitimately cleared the re-arm latch by separating past ReArmDistM")
	}
	tickFor(w, 5.5) // the capture runs its five seconds, then docks
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
	placePortToPort(a, b, 0.3)
	w.SetTargetCraft(1)

	if startsMagnet(w) {
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
	if !startsMagnet(w) {
		t.Fatal("checkDocking still refused a pair inside both gates after ReArmDocking cleared their latch")
	}
	tickFor(w, 5.5) // the capture runs its five seconds, then docks
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
	placePortToPort(a, b, 0.3)

	if startsMagnet(w) {
		t.Fatal("checkDocking fused a freshly-latched pair — latch should hold immediately after undock")
	}

	w.Clock.SimTime = w.Clock.SimTime.Add(ReArmCeiling + time.Minute)
	if !startsMagnet(w) {
		t.Fatal("checkDocking still refused to fuse after ReArmCeiling of sim-time elapsed — latch never expires")
	}
	tickFor(w, 5.5) // the capture runs its five seconds, then docks
	if len(w.Crafts) != 1 {
		t.Errorf("slate count = %d after the ceiling-expired re-dock, want 1", len(w.Crafts))
	}
}

// undockedTugs docks two tugs, undocks them (arming the latch) and parks the
// pair port to port gapM apart, aligned, b closing on a at closingMS. The
// nose a flies on is prograde, so the pair is a straight approach.
func undockedTugs(t *testing.T, gapM, closingMS float64) (*World, *spacecraft.Spacecraft, *spacecraft.Spacecraft) {
	t.Helper()
	w, _ := NewWorld()
	buildStack(t, w, 2)
	if !w.Undock(0) {
		t.Fatal("Undock returned false on a 2-component composite")
	}
	a, b := w.Crafts[0], w.Crafts[1]
	placePortToPort(a, b, gapM)
	b.State.V = a.State.V.Sub(a.CurrentAttitudeDir.Unit().Scale(closingMS))
	if !w.isLocalReArmed(a.ID, b.ID) {
		t.Fatal("fixture: Undock armed no latch")
	}
	return w, a, b
}

// tickCountingBumps runs the real Tick for secs, taking each BumpEvent the way
// the App does (read and clear), and returns how many fired and the last one.
func tickCountingBumps(w *World, secs float64) (n int, last BumpEvent) {
	end := w.Clock.SimTime.Add(time.Duration(secs * float64(time.Second)))
	for w.Clock.SimTime.Before(end) {
		w.Tick()
		if e := w.LastBumpEvent; e != nil {
			n++
			last = *e
			w.LastBumpEvent = nil
		}
	}
	return n, last
}

// #794: the re-arm distance is port to port. A latched pair clears once its
// ports are more than ReArmDistM apart, and not before; the boundary is
// pinned from both sides. The 6 m case is the player's: a piece backed off a
// few metres and flown home.
func TestLocalReArmClearsAtPortGapNotAnchorGap(t *testing.T) {
	if ReArmDistM < 4 || ReArmDistM > 6 {
		t.Errorf("ReArmDistM = %v, want about 5 m (port to port, sized for the 0.5 m gate)", ReArmDistM)
	}
	for _, c := range []struct {
		name string
		gap  float64
		held bool
	}{
		{"just inside", ReArmDistM - 1e-6, true},
		{"just outside", ReArmDistM + 1e-6, false},
		{"6 m", 6, false},
		{"touching", 0.3, true},
	} {
		w, a, b := undockedTugs(t, c.gap, 0)
		w.checkDocking()
		if got := w.isLocalReArmed(a.ID, b.ID); got != c.held {
			t.Errorf("%s (ports %.6f m apart): latched = %v, want %v", c.name, c.gap, got, c.held)
		}
	}
}

// The measure is the ports, not the anchors: a long pair touching nose to
// nose has anchors tens of metres apart (CSM + S-IVB+LM: 43 m) and must stay
// latched, or a bare "100 m becomes 5 m" would clear it the moment it forms.
func TestLocalReArmHoldsWhileLongPairPortsTouch(t *testing.T) {
	w, a, b := longPair(t, 0.3)
	if d := a.State.R.Sub(b.State.R).Norm(); d < 30 {
		t.Fatalf("fixture: anchors only %.1f m apart, want a long pair", d)
	}
	w.localReArms = append(w.localReArms, localReArm{idA: a.ID, idB: b.ID})
	w.checkDocking()
	if !w.isLocalReArmed(a.ID, b.ID) {
		t.Error("a long pair with its ports 0.3 m apart lost its latch (measured anchor to anchor)")
	}
	placePortToPort(a, b, 6)
	w.checkDocking()
	if w.isLocalReArmed(a.ID, b.ID) {
		t.Error("a long pair with its ports 6 m apart still latched")
	}
}

// longPair is a CSM (active) and an S-IVB+LM stack, port to port gapM apart.
func longPair(t *testing.T, gapM float64) (*World, *spacecraft.Spacecraft, *spacecraft.Spacecraft) {
	t.Helper()
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	a := spacecraft.NewFromStages([]spacecraft.Stage{stageOf(t, "csm")})
	b := spacecraft.NewFromStages([]spacecraft.Stage{stageOf(t, "s-ivb-1"), stageOf(t, "apollo-descent"), stageOf(t, "apollo-ascent")})
	a.Name, b.Name = "CSM", "LM stack"
	a.Primary, b.Primary = *earth, *earth
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	a.State = physics.StateVector{R: r, V: v, M: a.TotalMass()}
	b.State = physics.StateVector{R: r, V: v, M: b.TotalMass()}
	a.CurrentAttitudeDir = v.Unit()
	w.Crafts = []*spacecraft.Spacecraft{a, b}
	w.ActiveCraftIdx = 0
	w.stampCraftID(a)
	w.stampCraftID(b)
	placePortToPort(a, b, gapM)
	return w, a, b
}

// #794, the player's report through the real Tick: undock, turn back, touch
// at 0.4 m port gap and 0.05 m/s. The latch holds the pair off, the hulls
// meet and are pushed apart (the push stays), and the bump says it is the
// latch, not the approach speed.
func TestLatchedTouchBumpsAndNamesTheLatch(t *testing.T) {
	w, _, _ := undockedTugs(t, 0.4, 0.05)
	n, ev := tickCountingBumps(w, 12)
	if n == 0 {
		t.Fatal("a latched pair touched and was not pushed apart (the push must stay)")
	}
	if !ev.Latched {
		t.Errorf("bump of a latched pair does not say so: %+v", ev)
	}
}

// A pair backed past the re-arm distance and flown back in docks with no
// bump. (The flight home is placed rather than flown: free drift in orbit
// stalls a 0.05 m/s closing short of the gate, #652.)
func TestBackedOffPairFlownBackInDocksWithoutBump(t *testing.T) {
	w, a, b := undockedTugs(t, 6, 0)
	tickFor(w, 1) // the scan sees the ports 6 m apart and drops the latch
	if w.isLocalReArmed(a.ID, b.ID) {
		t.Fatal("fixture: the latch survived the pair backing off to 6 m")
	}
	placePortToPort(a, b, 0.4)
	b.State.V = a.State.V.Sub(a.CurrentAttitudeDir.Unit().Scale(0.05))
	n, ev := tickCountingBumps(w, 12)
	if n != 0 {
		t.Errorf("a pair that had cleared the re-arm distance bumped %d times (last %+v)", n, ev)
	}
	if len(w.Crafts) != 1 {
		t.Errorf("slate = %d vessels, want the pair docked", len(w.Crafts))
	}
}

// alt+u with a latched pair in the gate docks it, with no bump.
func TestReArmKeyInTheGateDocksWithoutBump(t *testing.T) {
	w, _, _ := undockedTugs(t, 0.4, 0.02)
	if n, _ := tickCountingBumps(w, 1); n != 0 {
		t.Fatalf("fixture: bumped %d times before the key", n)
	}
	if _, ok := w.ReArmDocking(); !ok {
		t.Fatal("alt+u found no latch")
	}
	n, ev := tickCountingBumps(w, 12)
	if n != 0 {
		t.Errorf("bumped %d times after alt+u (last %+v)", n, ev)
	}
	if len(w.Crafts) != 1 {
		t.Errorf("slate = %d vessels after alt+u in the gate, want the pair docked", len(w.Crafts))
	}
}
