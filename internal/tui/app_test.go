package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// Clicking the navball panel's [MODE] button cycles NavMode and
// surfaces the same status toast the CycleNavMode key does.
func TestDispatchNavballControlMode(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.NavMode = sim.NavOrbit
	a.dispatchNavballControl(screens.NavballControlMode)
	if a.world.NavMode == sim.NavOrbit {
		t.Errorf("NavMode did not cycle off NavOrbit")
	}
	if a.statusMsg == "" {
		t.Errorf("expected a nav-mode status toast")
	}
}

// Clicking an axis button holds that SAS intent — same path as the
// keyboard, with NavMode rebinding applied via ResolveAttitudeIntent.
func TestDispatchNavballControlAxis(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.NavMode = sim.NavOrbit
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft on the spawn-state world")
	}
	// Force the main-engine path so the click sets the held attitude
	// rather than firing a one-off RCS pulse.
	c.EngineMode = spacecraft.EngineMain

	a.dispatchNavballControl(screens.NavballControlRadialOut)
	want := a.world.ResolveAttitudeIntent(sim.IntentRadialOut)
	if c.AttitudeMode != want {
		t.Errorf("AttitudeMode = %v, want %v (resolved radial-out)", c.AttitudeMode, want)
	}
}

// Clicking the RCS toggle flips EngineMode both ways and toasts.
func TestDispatchNavballControlRCS(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.EngineMode = spacecraft.EngineMain

	a.dispatchNavballControl(screens.NavballControlRCS)
	if c.EngineMode != spacecraft.EngineRCS {
		t.Errorf("EngineMode = %v, want EngineRCS after first toggle", c.EngineMode)
	}
	if a.statusMsg == "" {
		t.Errorf("expected an RCS status toast")
	}
	a.dispatchNavballControl(screens.NavballControlRCS)
	if c.EngineMode != spacecraft.EngineMain {
		t.Errorf("EngineMode = %v, want EngineMain after second toggle", c.EngineMode)
	}
}

// Clicking the [SAS] tag flips World.InstantSAS both ways and toasts
// the new model name — the locked-decision non-silent surfacing.
func TestDispatchNavballControlSAS(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.world.InstantSAS {
		t.Fatalf("InstantSAS should default false (slew is the v0.10 default)")
	}
	a.dispatchNavballControl(screens.NavballControlSAS)
	if !a.world.InstantSAS {
		t.Errorf("InstantSAS = false, want true after first toggle")
	}
	if a.statusMsg == "" {
		t.Errorf("expected a SAS-model status toast")
	}
	a.dispatchNavballControl(screens.NavballControlSAS)
	if a.world.InstantSAS {
		t.Errorf("InstantSAS = true, want false after second toggle")
	}
}

// Clicking the target ± buttons holds BurnTarget / BurnAntiTarget.
func TestDispatchNavballControlTarget(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.EngineMode = spacecraft.EngineMain

	a.dispatchNavballControl(screens.NavballControlTargetPlus)
	if c.AttitudeMode != spacecraft.BurnTarget {
		t.Errorf("AttitudeMode = %v, want BurnTarget", c.AttitudeMode)
	}
	a.dispatchNavballControl(screens.NavballControlTargetMinus)
	if c.AttitudeMode != spacecraft.BurnAntiTarget {
		t.Errorf("AttitudeMode = %v, want BurnAntiTarget", c.AttitudeMode)
	}
}
