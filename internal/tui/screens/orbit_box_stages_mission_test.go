package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

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

// TestStagesBoxNamesSpaceAndFitsTheTier (ADR 0052 decision 5): with more than
// one stage the row names what [space] drops, and a long stage name is
// truncated to make room rather than pushing the row past its tier width.
func TestStagesBoxNamesSpaceAndFitsTheTier(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "S-IC", FuelMass: 1, FuelCapacity: 1},
		{Name: "S-II", FuelMass: 1, FuelCapacity: 1},
		{Name: "S-IVB", FuelMass: 1, FuelCapacity: 1},
	}
	line := v.buildStagesBox(w)[0]
	if !strings.Contains(line, "▸ S-IC (1/3) [space]") {
		t.Errorf("STAGES row = %q, want it to contain %q", line, "▸ S-IC (1/3) [space]")
	}

	c.Stages[0].Name = "An Extremely Long Booster Stage Name Indeed"
	line = v.buildStagesBox(w)[0]
	if !strings.Contains(line, "(1/3) [space]") {
		t.Errorf("long name pushed the suffix out: %q", line)
	}
	if got := lipgloss.Width(line); got > tierBottomLeftWidth-2 {
		t.Errorf("STAGES row is %d cells, budget is %d: %q", got, tierBottomLeftWidth-2, line)
	}

	// A lone stage cannot be dropped, so the row does not promise a drop.
	c.Stages = c.Stages[:1]
	if line := v.buildStagesBox(w)[0]; strings.Contains(line, "[space]") {
		t.Errorf("single-stage row advertises [space]: %q", line)
	}
}
