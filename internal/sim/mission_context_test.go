package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// v0.21 Slice 2 (ADR 0025) — the sim→missions seam. missionEvalContext
// snapshots Active-Vessel + world state into the read-only EvalContext the
// evaluator consumes. These tests prove every field is wired from real
// World state (including the resource/outcome fields no kind reads yet).

func TestMissionEvalContextSnapshotsActiveCraft(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	// Arrange a distinctive landed + noded + targeted + staged state.
	c.Landed = true
	c.LandedLatDeg, c.LandedLonDeg = 28.6, -80.6
	c.Crashed = false
	c.Nodes = append(c.Nodes, ManeuverNode{PrimaryID: c.Primary.ID})
	w.Target = spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: 1}
	w.stagedThisSession = true

	ctx := w.missionEvalContext()

	if ctx.PrimaryID != c.Primary.ID {
		t.Errorf("PrimaryID: got %q want %q", ctx.PrimaryID, c.Primary.ID)
	}
	if !ctx.Landed {
		t.Error("Landed not propagated")
	}
	if ctx.SurfaceLatDeg != 28.6 || ctx.SurfaceLonDeg != -80.6 {
		t.Errorf("surface coords: got (%v,%v) want (28.6,-80.6)", ctx.SurfaceLatDeg, ctx.SurfaceLonDeg)
	}
	if ctx.FuelKg != c.ActiveStageFuel() {
		t.Errorf("FuelKg: got %v want %v", ctx.FuelKg, c.ActiveStageFuel())
	}
	if ctx.MonopropKg != c.Monoprop {
		t.Errorf("MonopropKg: got %v want %v", ctx.MonopropKg, c.Monoprop)
	}
	if ctx.DvBudget != c.RemainingDeltaV() {
		t.Errorf("DvBudget: got %v want %v", ctx.DvBudget, c.RemainingDeltaV())
	}
	if !ctx.HasNode {
		t.Error("HasNode not set with a planted node")
	}
	if !ctx.HasTarget {
		t.Error("HasTarget not set with a body target")
	}
	if !ctx.Staged {
		t.Error("Staged not propagated")
	}
}

// TestMissionEvalContextSurfaceFallsBackToLaunchSite — with no soft-touchdown
// coords, the surface position falls back to the launchpad-spawn site
// (mirrors integrateLanded).
func TestMissionEvalContextSurfaceFallsBackToLaunchSite(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.LaunchLatDeg, c.LaunchLonDeg = 45, 10
	c.LandedLatDeg, c.LandedLonDeg = 0, 0 // no touchdown coords

	ctx := w.missionEvalContext()
	if ctx.SurfaceLatDeg != 45 || ctx.SurfaceLonDeg != 10 {
		t.Errorf("fallback to launch site: got (%v,%v) want (45,10)", ctx.SurfaceLatDeg, ctx.SurfaceLonDeg)
	}
}

// TestMissionEvalContextRendezvousRelativeState — a craft target feeds the
// instantaneous range / closing speed used by the rendezvous kind. A
// co-located twin yields ~0 separation and ~0 relative speed.
func TestMissionEvalContextRendezvousRelativeState(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	twin := *active // co-located second vessel, same primary/state
	twin.ID = active.ID + 1000
	w.Crafts = append(w.Crafts, &twin)
	w.Target = spacecraft.Target{Kind: spacecraft.TargetCraft, CraftID: twin.ID}

	ctx := w.missionEvalContext()
	if !ctx.HasTargetCraft {
		t.Fatal("HasTargetCraft not set with a craft target")
	}
	if ctx.TargetRangeM > 1 {
		t.Errorf("co-located range: got %v m, want ~0", ctx.TargetRangeM)
	}
	if ctx.TargetRelSpeedMs > 0.001 {
		t.Errorf("co-located rel speed: got %v m/s, want ~0", ctx.TargetRelSpeedMs)
	}
}

// TestTickPassesLandAtBody — the integration path: a land_at_body mission
// passes through Tick→evaluateMissions once the active craft is Landed on
// the named primary.
func TestTickPassesLandAtBody(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.Missions = []missions.Mission{{
		ID: "land-anywhere",
		Objectives: []missions.Objective{
			{Kind: missions.KindLandAtBody, Params: missions.Params{PrimaryID: c.Primary.ID}},
		},
	}}
	c.Landed = true
	w.Tick()
	if w.Missions[0].Status != missions.Passed {
		t.Fatalf("land_at_body after landing: got %v, want Passed", w.Missions[0].Status)
	}
}

// TestStageActiveLatchesStagedFlag — decoupling a stage latches the
// session staged flag the outcome context reads.
func TestStageActiveLatchesStagedFlag(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.stagedThisSession {
		t.Fatal("stagedThisSession should start false")
	}
	c := w.ActiveCraft()
	if len(c.Stages) < 2 {
		// Give the active craft a second stage so StageActive has something
		// to drop (the default loadout may be single-stage).
		c.Stages = append([]spacecraft.Stage{c.Stages[0]}, c.Stages...)
		c.SyncFields()
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if !w.stagedThisSession {
		t.Error("stagedThisSession not latched after StageActive")
	}
}
