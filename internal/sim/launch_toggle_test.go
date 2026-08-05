// Package sim — regression suite for the launch/surface view seams
// (issue #348 §4, ADR 0043): ToggleLaunchView's enter/leave round trip
// and its refusal, `v`-cycle removal, and the DESCENDING hint's
// crossing state machine (launch_hint.go). Mirrors proximity_test.go's
// coverage of ToggleProximityView, the sibling toggle key.
package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestToggleLaunchViewRoundTrip: press to enter (a Framing Event into
// ViewLaunch, with a session open), press again to return to the map
// EXACTLY as it was left — the same contract ToggleProximityView already
// guarantees for `o`.
func TestToggleLaunchViewRoundTrip(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ViewMode = ViewOrbitFlat

	entered, refusal := w.ToggleLaunchView()
	if !entered || refusal != "" {
		t.Fatalf("enter: entered=%v refusal=%q", entered, refusal)
	}
	if w.ViewMode != ViewLaunch {
		t.Fatalf("ViewMode = %s, want launch", w.ViewMode)
	}
	if !w.LaunchSessionActive {
		t.Error("LaunchSessionActive = false after entry, want true")
	}
	if w.PrevViewMode != ViewOrbitFlat {
		t.Errorf("PrevViewMode = %s, want orbit-flat (the view we jumped from)", w.PrevViewMode)
	}

	entered, refusal = w.ToggleLaunchView()
	if entered || refusal != "" {
		t.Fatalf("leave: entered=%v refusal=%q", entered, refusal)
	}
	if w.ViewMode != ViewOrbitFlat {
		t.Errorf("ViewMode = %s, want orbit-flat (the view we jumped from)", w.ViewMode)
	}
	if w.LaunchSessionActive {
		t.Error("LaunchSessionActive = true after leaving, want false")
	}
}

// TestToggleLaunchViewRefusesWithoutActiveCraft: unlike the retired
// one-way `V` jump (which rendered an explanatory empty-canvas message
// instead), the toggle refuses at the door — same "a refused action
// tells you nothing" discipline ToggleProximityView already follows.
// The refusal is visible and the camera doesn't move.
func TestToggleLaunchViewRefusesWithoutActiveCraft(t *testing.T) {
	w := mustWorld(t)
	// NewWorld seeds an active craft in LEO by default (world.go) — empty
	// the slate explicitly, same pattern as TestNudgeLaunchZoomNoActiveCraft.
	w.Crafts = nil
	w.ActiveCraftIdx = 0
	w.ViewMode = ViewTop

	entered, refusal := w.ToggleLaunchView()
	if entered {
		t.Fatal("entered ViewLaunch with no active craft")
	}
	if refusal == "" {
		t.Error("refusal is empty — a silent no-op reads as broken")
	}
	if w.ViewMode != ViewTop {
		t.Errorf("ViewMode = %s, want top (a refused jump must not move the camera)", w.ViewMode)
	}
}

// TestToggleLaunchViewAlwaysLeaves: leaving never refuses, even from a
// slate that has since gone empty (e.g. end-flight cleared the only
// vessel mid-session) — the same guarantee ToggleProximityView makes for
// a stale target.
func TestToggleLaunchViewAlwaysLeaves(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.ViewMode = ViewTilted
	if entered, _ := w.ToggleLaunchView(); !entered {
		t.Fatal("precondition: could not enter ViewLaunch")
	}

	w.Crafts = nil
	w.ActiveCraftIdx = 0

	entered, refusal := w.ToggleLaunchView()
	if entered {
		t.Error("leaving reported entered=true")
	}
	if refusal != "" {
		t.Errorf("leaving refused with %q — the exit must never be gated", refusal)
	}
	if w.ViewMode != ViewTilted {
		t.Errorf("ViewMode = %s, want tilted", w.ViewMode)
	}
}

// TestToggleLaunchViewEntrySeedsFreshSession: entering via the toggle
// resets the session-scoped state exactly like the auto-route path
// (routeToLaunchView) does — LaunchZoom back to 0 (auto/altitude-driven,
// the entry-scale Framing Event), MaxQ/trail cleared — proving the
// manual jump and the auto-route land on an identical session.
func TestToggleLaunchViewEntrySeedsFreshSession(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	w.LaunchZoom = 42.0
	w.LaunchMaxQ = 99.0
	w.LaunchTrail = []TrailPoint{{LatDeg: 1, LonDeg: 2, AltM: 3}}

	if entered, refusal := w.ToggleLaunchView(); !entered || refusal != "" {
		t.Fatalf("enter: entered=%v refusal=%q", entered, refusal)
	}
	if w.LaunchZoom != 0 {
		t.Errorf("LaunchZoom = %v, want 0 (auto/altitude-driven entry scale)", w.LaunchZoom)
	}
	if w.LaunchMaxQ == 99.0 {
		t.Error("LaunchMaxQ unchanged from pollution, want reset by entry")
	}
	if len(w.LaunchTrail) == 1 && w.LaunchTrail[0].LatDeg == 1 {
		t.Error("LaunchTrail still contains the polluted pre-entry sample")
	}
}

// TestToggleLaunchViewReturnsFromAutoRoute: the auto-route-on-liftoff
// behaviour (World.Tick routing into ViewLaunch on a Landed-false→true
// transition) is unchanged by this slice — it's an existing behaviour
// the issue didn't revoke. The toggle key still works as the way back
// out of a session it didn't open itself.
func TestToggleLaunchViewReturnsFromAutoRoute(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	priorView := w.ViewMode

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}
	w.Tick()

	if w.ViewMode != ViewLaunch {
		t.Fatalf("precondition: auto-route did not fire, ViewMode = %s", w.ViewMode)
	}
	if !w.LaunchSessionActive {
		t.Fatal("precondition: auto-route did not open a session")
	}

	entered, refusal := w.ToggleLaunchView()
	if entered || refusal != "" {
		t.Fatalf("leave: entered=%v refusal=%q", entered, refusal)
	}
	if w.ViewMode != priorView {
		t.Errorf("ViewMode = %s, want %s (the view the auto-route entered from)", w.ViewMode, priorView)
	}
	if w.LaunchSessionActive {
		t.Error("LaunchSessionActive = true after the toggle-out, want false")
	}
}
