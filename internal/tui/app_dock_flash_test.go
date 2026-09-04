package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestAutoFuseFlashesInUndockVoice (#421 / ADR 0048 §2): spawning
// "alongside active" (or any other proximity auto-fuse) must raise an
// Event Flash in the same voice `U`/undock already uses — a player must
// never lose track of which vessel they're flying in total silence, the
// #301/#421 "docking swallows its own announcement" family. Reproduces
// the auto-fuse directly (place a second craft inside both docking
// gates and let the real Tick path run checkDocking) rather than going
// through the spawn form, so the test pins the announcement itself, not
// the spawn flow.
func TestAutoFuseFlashesInUndockVoice(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	active := a.world.ActiveCraft()
	if active == nil {
		t.Fatal("setup: no active craft")
	}
	// Named distinctly from the default active craft (itself "S-IVB-1",
	// the default loadout's name) so the assertions below can't pass by
	// coincidentally matching the WRONG craft's name.
	partner := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	partner.Name = "Tug-1"
	partner.Primary = active.Primary
	partner.State = physics.StateVector{
		R: active.State.R.Add(orbital.Vec3{X: 10}), // inside DockingDistM (50 m)
		V: active.State.V,                          // matched — inside DockingVMS (0.1 m/s)
		M: partner.TotalMass(),
	}
	a.world.Crafts = append(a.world.Crafts, partner)
	if len(a.world.Crafts) != 2 {
		t.Fatalf("setup: expected 2 vessels before the fuse, got %d", len(a.world.Crafts))
	}

	tickAt(a, time.Now())

	if len(a.world.Crafts) != 1 {
		t.Fatalf("expected the pair to auto-fuse into 1 composite, got %d craft", len(a.world.Crafts))
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
