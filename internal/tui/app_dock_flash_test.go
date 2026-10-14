package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestAutoFuseFlashesInUndockVoice (#421 / ADR 0048 §2): a dock must raise an
// Event Flash in the same voice `U`/undock already uses, a player must never
// lose track of which vessel they're flying in total silence, the #301/#421
// "docking swallows its own announcement" family. Reproduces the dock
// directly (two ported tugs port to port inside the gate, then the real Tick
// path runs the five-second magnet) rather than going through the spawn form,
// so the test pins the announcement itself, not the spawn flow.
func TestAutoFuseFlashesInUndockVoice(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	old := a.world.ActiveCraft()
	if old == nil {
		t.Fatal("setup: no active craft")
	}
	// The default S-IVB has no port; fly a ported tug in its place, nose
	// along its velocity and holding it.
	active := spacecraft.NewFromLoadout("RCS-tug")
	active.Primary = old.Primary
	active.State = physics.StateVector{R: old.State.R, V: old.State.V, M: active.TotalMass()}
	active.CurrentAttitudeDir = old.State.V.Unit()
	active.Name = "Home"
	a.world.Crafts[0] = active

	// Named distinctly from the active craft so the assertions below can't
	// pass by coincidentally matching the WRONG craft's name.
	partner := spacecraft.NewFromLoadout("RCS-tug")
	partner.Name = "Tug-1"
	partner.Primary = active.Primary
	partner.CurrentAttitudeDir = active.CurrentAttitudeDir.Scale(-1)
	partner.SlewRateDegPerSec = 1e-9 // hold the facing through the ticks
	ga, gb := sim.VesselPortGeometry(active), sim.VesselPortGeometry(partner)
	partner.State = physics.StateVector{
		// Ports 0.3 m apart, noses facing: inside the whole gate.
		R: active.State.R.Add(ga.Axis.Scale(ga.Ports[0].OffsetM + 0.3 + gb.Ports[0].OffsetM)),
		V: active.State.V, // matched
		M: partner.TotalMass(),
	}
	a.world.Crafts = append(a.world.Crafts, partner)
	if len(a.world.Crafts) != 2 {
		t.Fatalf("setup: expected 2 vessels before the fuse, got %d", len(a.world.Crafts))
	}

	// The magnet pulls for five sim-seconds at 1x (50 ms per tick).
	for i := 0; i < 200 && len(a.world.Crafts) == 2; i++ {
		tickAt(a, time.Now())
	}

	if len(a.world.Crafts) != 1 {
		t.Fatalf("expected the pair to dock into 1 composite, got %d craft", len(a.world.Crafts))
	}
	if !strings.Contains(a.statusMsg, "docked with Tug-1") {
		t.Errorf("statusMsg = %q, want it to name the partner in undock's voice (\"docked with Tug-1\")", a.statusMsg)
	}
	if !strings.Contains(a.statusMsg, "now 1 vessel") || !strings.Contains(a.statusMsg, "2 components") {
		t.Errorf("statusMsg = %q, want the vessel/component counts (\"now 1 vessel, 2 components\")", a.statusMsg)
	}
	if a.statusExpires.Before(time.Now()) {
		t.Error("statusExpires already elapsed — the flash won't render")
	}
}
