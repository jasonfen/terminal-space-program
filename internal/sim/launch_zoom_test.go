// Package sim — v0.11.0 Slice 1 NudgeLaunchZoom tests. Exercise the
// ×0.8 / ×1.25 multiplicative step, the first-from-auto pin path,
// the 1.0 m/cell floor, and the no-active-craft no-op.
package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// First `+` from auto (LaunchZoom == 0) pins to the supplied
// auto-scale and then applies the ×0.8 zoom-in step in one call —
// matches the plan-grill expectation that the first press both pins
// and zooms by one stop.
func TestNudgeLaunchZoomFirstPlusFromAutoPins(t *testing.T) {
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
	w.LaunchZoom = 0
	w.NudgeLaunchZoom(+1, 100.0)
	want := 100.0 * 0.8
	if math.Abs(w.LaunchZoom-want) > 1e-9 {
		t.Errorf("LaunchZoom = %g, want %g", w.LaunchZoom, want)
	}
}

// After the first pin, subsequent `+` and `-` presses multiply by
// 0.8 and 1.25 (reciprocals — a +/- pair returns to origin within
// float epsilon).
func TestNudgeLaunchZoomPlusMinusReciprocals(t *testing.T) {
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
	w.LaunchZoom = 50.0
	w.NudgeLaunchZoom(+1, 0)
	w.NudgeLaunchZoom(-1, 0)
	if math.Abs(w.LaunchZoom-50.0) > 1e-9 {
		t.Errorf("after +/-: LaunchZoom = %g, want 50.0", w.LaunchZoom)
	}
}

// 1.0 m/cell floor: aggressive zoom-in past the floor clamps rather
// than letting the scene shrink to a single pixel.
func TestNudgeLaunchZoomFloorAtOneMeterPerCell(t *testing.T) {
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
	w.LaunchZoom = 1.5
	w.NudgeLaunchZoom(+1, 0) // 1.5 * 0.8 = 1.2
	w.NudgeLaunchZoom(+1, 0) // 1.2 * 0.8 = 0.96 → floor 1.0
	if w.LaunchZoom != 1.0 {
		t.Errorf("LaunchZoom = %g, want 1.0 floor", w.LaunchZoom)
	}
}

// releaseLaunchSession stamps LastLaunchReleaseEvent with the
// PrevViewMode label so the App's status flash can surface the
// `"ORBIT READY — returning to <prev view>"` toast and then clear
// the event. Same pattern as LastDockEvent. v0.11.0+ grill
// resolution 9.
func TestReleaseLaunchSessionStampsToastEvent(t *testing.T) {
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
	w.ViewMode = ViewTop
	w.routeToLaunchView()
	if !w.LaunchSessionActive || w.PrevViewMode != ViewTop {
		t.Fatalf("setup: session not opened correctly (active=%v, prev=%v)",
			w.LaunchSessionActive, w.PrevViewMode)
	}
	w.releaseLaunchSession()
	if w.LastLaunchReleaseEvent == nil {
		t.Fatal("LastLaunchReleaseEvent = nil, want stamped on release")
	}
	if got := w.LastLaunchReleaseEvent.PrevView; got != "top" {
		t.Errorf("PrevView label = %q, want %q", got, "top")
	}
}

// No active craft — the helper is a no-op rather than panicking on
// nil-deref. Reasonable: zoom keys with no active vessel have
// nothing meaningful to zoom.
func TestNudgeLaunchZoomNoActiveCraft(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// NewWorld auto-seeds an LEO craft; clear the slate to test
	// the no-active-craft path.
	w.Crafts = nil
	w.ActiveCraftIdx = 0
	w.LaunchZoom = 0
	w.NudgeLaunchZoom(+1, 100.0)
	if w.LaunchZoom != 0 {
		t.Errorf("no craft: LaunchZoom = %g, want 0 (no-op)", w.LaunchZoom)
	}
}
