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
