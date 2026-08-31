package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestPlanInclBodyTargetUnchanged is the ADR 0045 S4 (#397) regression
// guard for app.go's `[I]` dispatch: adding a vessel-target branch to
// the sim.TargetCraft/sim.TargetGhost switch must leave the pre-existing
// sim.TargetBody path untouched. It presses `I` with a body target
// bound through the ordinary app.go input path and checks the result
// against calling world.PlanPlaneMatch directly on an identically
// set-up world — the two must plant byte-identical nodes, proving the
// TUI dispatch still routes a body target through the unmodified
// PlanPlaneMatch function rather than the new PlanVesselPlaneMatch.
func TestPlanInclBodyTargetUnchanged(t *testing.T) {
	setup := func(t *testing.T) (*App, int) {
		t.Helper()
		a, err := New(nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		sys := a.world.System()
		moonIdx := -1
		for i, b := range sys.Bodies {
			if b.EnglishName == "Moon" {
				moonIdx = i
			}
		}
		if moonIdx < 0 {
			t.Skip("Moon not in loaded Sol system")
		}
		a.active = screenOrbit
		return a, moonIdx
	}

	// Reference: call PlanPlaneMatch directly on a freshly built world —
	// this is the function app.go called for TargetBody before #397 and
	// still must call unchanged after.
	refApp, moonIdx := setup(t)
	refPlan, refErr := refApp.world.PlanPlaneMatch(moonIdx)
	if refErr != nil {
		t.Fatalf("reference PlanPlaneMatch: %v", refErr)
	}

	// Under test: press `I` with the same body targeted via the normal
	// app.go input path (a fresh, identically-constructed world).
	a, _ := setup(t)
	a.world.SetTargetBody(moonIdx)
	pressKey(a, 'I')

	got := a.world.ActiveCraft().Nodes
	if len(got) != 1 {
		t.Fatalf("expected 1 planted node via [I], got %d", len(got))
	}
	if got[0].DV != refPlan.DV || got[0].PlaneChangeRad != refPlan.PlaneChangeRad {
		t.Errorf("[I]-planted node = {DV:%.6f, PlaneChangeRad:%.6f}, want byte-identical to direct PlanPlaneMatch {DV:%.6f, PlaneChangeRad:%.6f}",
			got[0].DV, got[0].PlaneChangeRad, refPlan.DV, refPlan.PlaneChangeRad)
	}
	if got[0].TriggerTime != refApp.world.Clock.SimTime.Add(refPlan.OffsetTime) {
		t.Errorf("[I]-planted TriggerTime = %v, want %v", got[0].TriggerTime, refApp.world.Clock.SimTime.Add(refPlan.OffsetTime))
	}
}

// TestPlanInclCraftTargetDispatchesVesselPlaneMatch is a thin app.go
// wiring check (#397): pressing `I` with a craft target bound must
// route through PlanVesselPlaneMatch, not fall through to the
// equatorial-drop default or the retired "plan via [m]" refusal.
func TestPlanInclCraftTargetDispatchesVesselPlaneMatch(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 350e3, Inclination: 51.6}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(a.world.Crafts) < 2 {
		t.Fatalf("expected 2 crafts after spawn, got %d", len(a.world.Crafts))
	}
	a.world.ActiveCraftIdx = 0
	a.world.SetTargetCraft(1)
	a.active = screenOrbit

	pressKey(a, 'I')

	if got := a.world.ActiveCraft().Nodes; len(got) != 1 {
		t.Fatalf("expected 1 planted node via [I] against a craft target, got %d (statusMsg=%q)", len(got), a.statusMsg)
	}
	if a.statusMsg == "" || a.statusMsg == "I targets bodies — for vessels, plan via [m]" {
		t.Errorf("statusMsg = %q, want an inclination-plan flash, not the retired vessel refusal", a.statusMsg)
	}
}
