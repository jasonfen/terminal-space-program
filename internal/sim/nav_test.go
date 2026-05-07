package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestResolveAttitudeIntentOrbit — NavOrbit (the default) maps the six
// SAS-axis intents 1:1 to the v0.7.3+ orbit-frame burn modes.
func TestResolveAttitudeIntentOrbit(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	cases := []struct {
		intent AttitudeIntent
		want   spacecraft.BurnMode
	}{
		{IntentPrograde, spacecraft.BurnPrograde},
		{IntentRetrograde, spacecraft.BurnRetrograde},
		{IntentNormalPlus, spacecraft.BurnNormalPlus},
		{IntentNormalMinus, spacecraft.BurnNormalMinus},
		{IntentRadialOut, spacecraft.BurnRadialOut},
		{IntentRadialIn, spacecraft.BurnRadialIn},
	}
	for _, tc := range cases {
		if got := w.ResolveAttitudeIntent(tc.intent); got != tc.want {
			t.Errorf("orbit intent %v: got %v, want %v", tc.intent, got, tc.want)
		}
	}
}

// TestResolveAttitudeIntentSurface — NavSurface only redefines
// prograde / retrograde; the other axes still resolve to their
// orbital meanings (KSP behaviour: the surface navball still shows
// orbital normal/radial markers).
func TestResolveAttitudeIntentSurface(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavSurface
	cases := []struct {
		intent AttitudeIntent
		want   spacecraft.BurnMode
	}{
		{IntentPrograde, spacecraft.BurnSurfacePrograde},
		{IntentRetrograde, spacecraft.BurnSurfaceRetrograde},
		{IntentNormalPlus, spacecraft.BurnNormalPlus},
		{IntentNormalMinus, spacecraft.BurnNormalMinus},
		{IntentRadialOut, spacecraft.BurnRadialOut},
		{IntentRadialIn, spacecraft.BurnRadialIn},
	}
	for _, tc := range cases {
		if got := w.ResolveAttitudeIntent(tc.intent); got != tc.want {
			t.Errorf("surface intent %v: got %v, want %v", tc.intent, got, tc.want)
		}
	}
}

// TestResolveAttitudeIntentTarget — NavTarget rebinds prograde /
// retrograde to relative-velocity and radial± to BurnTarget /
// BurnAntiTarget (toward / away). Normal axes stay orbital because
// there is no target-relative normal SAS mode.
func TestResolveAttitudeIntentTarget(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	// SpawnSisterCraft promotes the new craft to active (idx 1); the
	// original LEO craft is now at idx 0 and is the targetable sibling.
	w.SetTargetCraft(0)
	if w.Target.Kind != TargetCraft {
		t.Fatalf("setup: target kind = %v, want TargetCraft", w.Target.Kind)
	}
	w.NavMode = NavTarget
	cases := []struct {
		intent AttitudeIntent
		want   spacecraft.BurnMode
	}{
		{IntentPrograde, spacecraft.BurnTargetPrograde},
		{IntentRetrograde, spacecraft.BurnTargetRetrograde},
		{IntentRadialOut, spacecraft.BurnTarget},
		{IntentRadialIn, spacecraft.BurnAntiTarget},
		{IntentNormalPlus, spacecraft.BurnNormalPlus},
		{IntentNormalMinus, spacecraft.BurnNormalMinus},
	}
	for _, tc := range cases {
		if got := w.ResolveAttitudeIntent(tc.intent); got != tc.want {
			t.Errorf("target intent %v: got %v, want %v", tc.intent, got, tc.want)
		}
	}
}

// TestResolveAttitudeIntentTargetFallback — NavTarget without a craft
// target silently degrades to NavOrbit so a stale mode doesn't yield
// a zero-direction SAS hold.
func TestResolveAttitudeIntentTargetFallback(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavTarget
	// No craft target bound. ResolveAttitudeIntent must fall back to
	// orbit-frame.
	if got := w.ResolveAttitudeIntent(IntentPrograde); got != spacecraft.BurnPrograde {
		t.Errorf("target+notarget prograde: got %v, want BurnPrograde", got)
	}
	if got := w.ResolveAttitudeIntent(IntentRadialOut); got != spacecraft.BurnRadialOut {
		t.Errorf("target+notarget radial+: got %v, want BurnRadialOut", got)
	}
}

// TestCycleNavModeSkipsTargetWithoutCraftTarget — without a craft
// target, the cycle goes Orbit → Surface → Orbit (Target skipped) so
// the player never lands on a mode that silently degrades.
func TestCycleNavModeSkipsTargetWithoutCraftTarget(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.NavMode != NavOrbit {
		t.Fatalf("default nav: got %v, want NavOrbit", w.NavMode)
	}
	if got := w.CycleNavMode(); got != NavSurface {
		t.Errorf("cycle 1: got %v, want NavSurface", got)
	}
	if got := w.CycleNavMode(); got != NavOrbit {
		t.Errorf("cycle 2 (skips target — no craft target): got %v, want NavOrbit", got)
	}
}

// TestCycleNavModeIncludesTargetWhenCraftTargetBound — once a sibling
// craft is targeted, the cycle visits all three: Orbit → Surface →
// Target → Orbit.
func TestCycleNavModeIncludesTargetWhenCraftTargetBound(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	w.SetTargetCraft(0)
	if got := w.CycleNavMode(); got != NavSurface {
		t.Errorf("cycle 1: got %v, want NavSurface", got)
	}
	if got := w.CycleNavMode(); got != NavTarget {
		t.Errorf("cycle 2: got %v, want NavTarget", got)
	}
	if got := w.CycleNavMode(); got != NavOrbit {
		t.Errorf("cycle 3: got %v, want NavOrbit", got)
	}
}

// TestClearTargetSnapsNavTargetToOrbit — dropping the target while
// NavMode is NavTarget reconciles back to NavOrbit so the HUD doesn't
// claim a frame it can no longer resolve.
func TestClearTargetSnapsNavTargetToOrbit(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	w.SetTargetCraft(0)
	w.NavMode = NavTarget
	w.ClearTarget()
	if w.NavMode != NavOrbit {
		t.Errorf("after ClearTarget: nav = %v, want NavOrbit", w.NavMode)
	}
}

// TestSetTargetBodySnapsNavTargetToOrbit — swapping from a craft
// target to a body target also takes nav out of NavTarget (KSP target
// mode is craft-only in our model).
func TestSetTargetBodySnapsNavTargetToOrbit(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	w.SetTargetCraft(0)
	w.NavMode = NavTarget
	w.SetTargetBody(1) // any non-root body
	if w.NavMode != NavOrbit {
		t.Errorf("after SetTargetBody: nav = %v, want NavOrbit", w.NavMode)
	}
}
