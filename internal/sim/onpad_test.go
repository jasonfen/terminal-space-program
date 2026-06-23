// Package sim — v0.11.4+ OnPad route-gate tests (ADR 0004).
// The ViewLaunch auto-route handler fires on `OnPad && Landed`
// transitions, not just `Landed` transitions. Soft-lands clear
// OnPad on liftoff so the post-flight Landed=false→true transition
// does NOT rip the player into ViewLaunch mid-touchdown.

package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLaunchpadSpawnRoutesToViewLaunch — Slice 1 regression. A
// fresh launchpad spawn sets both Landed=true and OnPad=true; the
// route handler fires the Landed=false→true transition with the
// OnPad gate satisfied. Mirrors TestLaunchRouteOnLandedTransition
// but additionally pins OnPad=true post-spawn — the
// disambiguating flag the ADR added.
func TestLaunchpadSpawnRoutesToViewLaunch(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	c := w.ActiveCraft()
	if !c.OnPad {
		t.Fatal("setup: launchpad spawn should set OnPad=true")
	}
	w.Tick()
	if w.ViewMode != ViewLaunch {
		t.Errorf("OnPad launchpad spawn: ViewMode = %v, want ViewLaunch", w.ViewMode)
	}
	if !w.LaunchSessionActive {
		t.Error("OnPad launchpad spawn: LaunchSessionActive=false, want true")
	}
}

// TestLiftoffClearsOnPad — first engine ignition (StartManualBurn)
// clears the OnPad flag along with Landed. Without this, a vessel
// that flew once and later soft-landed would still carry OnPad=true
// and trip the ViewLaunch auto-route on its post-flight Landed
// transition. The clear is symmetric with the Landed clear, in
// both ignition paths (manual + planted-burn fire).
func TestLiftoffClearsOnPad(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	c := w.ActiveCraft()
	if !c.OnPad {
		t.Fatal("setup: launchpad spawn should set OnPad=true")
	}
	c.AttitudeMode = spacecraft.BurnRadialOut
	c.Throttle = 1.0
	crewTend(c) // command gate (ADR 0027): this test exercises onpad/liftoff logic, not comms
	w.StartManualBurn()
	if c.OnPad {
		t.Errorf("after StartManualBurn: OnPad=true, want false")
	}
	if c.Landed {
		t.Errorf("after StartManualBurn: Landed=true, want false")
	}
}

// TestSoftLandDoesNotRouteToViewLaunch — the headline regression
// the ADR called out. A post-flight soft-landing sets Landed=true
// but leaves OnPad=false (OnPad was cleared on the original
// liftoff). The route handler gates on `OnPad && Landed`, so the
// transition does NOT fire — the player stays in their current
// ViewMode for the touchdown sequence.
//
// Simulates the post-flight state directly (Landed=false +
// OnPad=false then Landed=true) to isolate the route-gate
// behaviour without driving a full ascent + descent.
func TestSoftLandDoesNotRouteToViewLaunch(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Start from a non-Landed, non-OnPad world (the NewWorld default
	// puts the craft in LEO with Landed=false). Pre-condition: the
	// route handler has not fired in any prior tick.
	c := w.ActiveCraft()
	c.Landed = false
	c.OnPad = false
	w.ViewMode = ViewTilted
	w.LaunchSessionActive = false
	w.wasActiveLanded = false
	// First tick seeds shadows; nothing should change.
	w.Tick()
	if w.LaunchSessionActive {
		t.Fatal("setup: shadow-seed tick should not open a session")
	}
	// Simulate a soft-landing transition: Landed flips false→true,
	// but OnPad stays false (cleared on the original liftoff). The
	// next tick's route handler should NOT fire.
	priorView := w.ViewMode
	c.Landed = true
	c.OnPad = false
	w.Tick()
	if w.ViewMode != priorView {
		t.Errorf("soft-landing (Landed && !OnPad): ViewMode changed to %v, want %v (no route)",
			w.ViewMode, priorView)
	}
	if w.LaunchSessionActive {
		t.Error("soft-landing: LaunchSessionActive=true, want false (no session opened)")
	}
}
