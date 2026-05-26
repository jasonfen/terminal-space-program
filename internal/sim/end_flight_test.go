// Package sim — v0.11.4+ end-flight action tests (ADR 0004).

package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestEndFlightRemovesCrashedVessel — after end-flight on a Crashed
// active vessel, the slate length drops by one and the vessel is no
// longer reachable via index lookup.
func TestEndFlightRemovesCrashedVessel(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active vessel")
	}
	c.Crashed = true
	before := len(w.Crafts)
	if !w.EndFlightActive() {
		t.Fatal("EndFlightActive returned false on Crashed vessel; want true")
	}
	if len(w.Crafts) != before-1 {
		t.Errorf("slate length: got %d, want %d (one removed)", len(w.Crafts), before-1)
	}
	for _, slot := range w.Crafts {
		if slot == c {
			t.Error("removed vessel is still present in slate")
		}
	}
}

// TestEndFlightSwitchesActive — when the removed vessel was the
// active one and the slate has at least one survivor, active
// auto-switches to a real vessel (not nil).
func TestEndFlightSwitchesActive(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Spawn a second vessel alongside so removal of the active one
	// has a successor to fall through to.
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatalf("SpawnCraft alongside: %v", err)
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("setup: expected 2 crafts in slate, got %d", len(w.Crafts))
	}
	// Mark the second (currently active per spawn-alongside) Crashed.
	active := w.ActiveCraft()
	active.Crashed = true
	if !w.EndFlightActive() {
		t.Fatal("EndFlightActive: false on Crashed active; want true")
	}
	if w.ActiveCraft() == nil {
		t.Fatal("after end-flight with one survivor: ActiveCraft = nil, want the survivor")
	}
	if w.ActiveCraft().Crashed {
		t.Error("after end-flight: new active is Crashed; should have skipped to a live vessel")
	}
}

// TestEndFlightFallsBackToNilActive — removing the only vessel
// leaves the slate empty and ActiveCraft() reads nil. The screen
// layer (sub-scope 5) renders the "no active vessel" message in
// that state.
func TestEndFlightFallsBackToNilActive(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Crashed = true
	if !w.EndFlightActive() {
		t.Fatal("EndFlightActive: false on Crashed lone vessel; want true")
	}
	if len(w.Crafts) != 0 {
		t.Errorf("after removing the only vessel: slate length = %d, want 0", len(w.Crafts))
	}
	if w.ActiveCraft() != nil {
		t.Errorf("after empty-slate end-flight: ActiveCraft = %+v, want nil", w.ActiveCraft())
	}
}

// TestEndFlightNoOpOnLiveVessel — end-flight on a non-Crashed
// vessel is a defense-in-depth no-op (the screen prompt won't open
// in this state, but a direct API call shouldn't drop a live
// vessel either).
func TestEndFlightNoOpOnLiveVessel(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Crashed = false
	before := len(w.Crafts)
	if w.EndFlightActive() {
		t.Error("EndFlightActive: true on live vessel; want false (no-op)")
	}
	if len(w.Crafts) != before {
		t.Errorf("slate length changed on no-op: got %d, want %d", len(w.Crafts), before)
	}
}

// Reference imports so the test file compiles even when the
// non-test path doesn't pull in spacecraft directly.
var _ = spacecraft.LoadoutSaturnVID
