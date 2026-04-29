package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestCycleEngineMode: starts in EngineMain, toggles to EngineRCS
// and back to EngineMain.
func TestCycleEngineMode(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.EngineMode != spacecraft.EngineMain {
		t.Fatalf("default engine = %v, want EngineMain", w.EngineMode)
	}
	w.CycleEngineMode()
	if w.EngineMode != spacecraft.EngineRCS {
		t.Errorf("after first toggle = %v, want EngineRCS", w.EngineMode)
	}
	w.CycleEngineMode()
	if w.EngineMode != spacecraft.EngineMain {
		t.Errorf("after second toggle = %v, want EngineMain", w.EngineMode)
	}
}

// TestFireRCSPulseGatesOnEngineMode: in EngineMain mode, FireRCSPulse
// should be a no-op even with a fully-fueled craft. Switching to
// EngineRCS and pulsing must change |v|.
func TestFireRCSPulseGatesOnEngineMode(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	v0 := w.Craft.OrbitalSpeed()

	if w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Error("FireRCSPulse fired in EngineMain mode")
	}
	if w.Craft.OrbitalSpeed() != v0 {
		t.Errorf("|v| changed on gated pulse: %v → %v", v0, w.Craft.OrbitalSpeed())
	}

	w.CycleEngineMode() // → EngineRCS
	if !w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Fatal("FireRCSPulse did not fire in EngineRCS mode")
	}
	got := w.Craft.OrbitalSpeed()
	want := v0 + spacecraft.RCSDvQuantum
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("post-pulse |v| = %.6f, want %.6f", got, want)
	}
}

// TestFireRCSPulseUpdatesAttitudeMode: the HUD's "hold" line should
// reflect the last fired direction.
func TestFireRCSPulseUpdatesAttitudeMode(t *testing.T) {
	w, _ := NewWorld()
	w.CycleEngineMode()
	w.FireRCSPulse(spacecraft.BurnRetrograde)
	if w.AttitudeMode != spacecraft.BurnRetrograde {
		t.Errorf("AttitudeMode = %v, want Retrograde", w.AttitudeMode)
	}
}

// TestFireRCSPulseRecordsPuff: a fired pulse should appear in the
// puff buffer for the canvas renderer to surface.
func TestFireRCSPulseRecordsPuff(t *testing.T) {
	w, _ := NewWorld()
	w.CycleEngineMode()
	w.FireRCSPulse(spacecraft.BurnPrograde)
	puffs := w.RCSPuffs()
	if len(puffs) != 1 {
		t.Fatalf("expected 1 puff, got %d", len(puffs))
	}
	if puffs[0].AgeFrac != 0 {
		t.Errorf("AgeFrac on freshly-fired puff = %v, want 0", puffs[0].AgeFrac)
	}
}

// TestStartManualBurnGatesOnEngineMode: in RCS mode, StartManualBurn
// must be a no-op — main-engine sustained fire is gated to EngineMain.
func TestStartManualBurnGatesOnEngineMode(t *testing.T) {
	w, _ := NewWorld()
	w.CycleEngineMode() // → RCS
	w.StartManualBurn()
	if w.ManualBurn != nil {
		t.Error("StartManualBurn engaged a sustained burn in RCS mode")
	}
	w.CycleEngineMode() // → main
	w.StartManualBurn()
	if w.ManualBurn == nil {
		t.Error("StartManualBurn failed to engage in main mode")
	}
}
