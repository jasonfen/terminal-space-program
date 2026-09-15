package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestStagesBoxRendersSingleStageVessel: decision 5 amends the retired
// buildStagesChip's "nil for len(Stages) <= 1", every vessel gets a
// STAGES box now, single-stage vehicles included.
func TestStagesBoxRendersSingleStageVessel(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{{Name: "solo stage", FuelMass: 100, FuelCapacity: 100}}
	lines := v.buildStagesBox(w)
	if len(lines) != 1 {
		t.Fatalf("buildStagesBox returned %d lines, want 1 (STAGES is a single combined line)", len(lines))
	}
	if !strings.Contains(lines[0], "solo stage") {
		t.Errorf("STAGES line = %q, want the single stage named", lines[0])
	}
}

// TestMissionBoxDashWhenNothingToReport: decision 2, MISSION never
// vanishes; with no active mission and no ladder sendoff, it reads a
// dash rather than dropping (the retired buildMissionsChip returned nil
// here).
func TestMissionBoxDashWhenNothingToReport(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = nil
	lines := v.buildMissionBox(w)
	if len(lines) != 1 || !strings.Contains(lines[0], "—") {
		t.Errorf("buildMissionBox with no mission = %v, want a single dash line", lines)
	}
}
