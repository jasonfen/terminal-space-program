package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The mission checklist chip (ADR 0025 / Slice 5) is a one-liner: the active
// mission name, its current objective, and N/M progress — with a transient
// failure flash when a mission dies. missionChipLines is the pure content
// selector, exercised here without needing a live World to arm the flash.

func TestMissionChipLinesActive(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	m := &missions.Mission{
		ID:   "m1",
		Name: "Circularize",
		Objectives: []missions.Objective{
			{Kind: missions.KindReachAltitude, Name: "reach 100 km", Status: missions.Passed},
			{Kind: missions.KindCircularize, Name: "circular orbit"},
		},
	}
	lines := v.missionChipLines("", false, m)
	if lines == nil {
		t.Fatal("active mission chip = nil, want content")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "MISSION") {
		t.Errorf("chip missing MISSION header:\n%s", joined)
	}
	if !strings.Contains(joined, "Circularize") {
		t.Errorf("chip missing mission name:\n%s", joined)
	}
	if !strings.Contains(joined, "circular orbit") {
		t.Errorf("chip missing current objective:\n%s", joined)
	}
	if !strings.Contains(joined, "1/2") {
		t.Errorf("chip missing 1/2 progress:\n%s", joined)
	}
	// The chip is a one-liner: header line + a single objective line.
	if len(lines) != 2 {
		t.Errorf("active chip = %d lines, want 2 (one-liner):\n%s", len(lines), joined)
	}
}

func TestMissionChipLinesFailureFlash(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	// The flash wins even with an active mission present (it just failed).
	m := &missions.Mission{ID: "next", Name: "Luna Flyby"}
	lines := v.missionChipLines("Land Test failed: crashed", true, m)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "✗") {
		t.Errorf("failure flash missing ✗ marker:\n%s", joined)
	}
	if !strings.Contains(joined, "Land Test failed: crashed") {
		t.Errorf("failure flash missing the message:\n%s", joined)
	}
	if strings.Contains(joined, "Luna Flyby") {
		t.Errorf("failure flash should not show the next mission yet:\n%s", joined)
	}
}

func TestMissionChipLinesTutorialHint(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	// A tutorial-program mission surfaces the current step's instruction in
	// the chip (Slice 7), so the player learns the control without leaving flight.
	m := &missions.Mission{
		ID:      "tut",
		Name:    "Flight School",
		Program: missions.ProgramTutorial,
		Objectives: []missions.Objective{{
			Kind:        missions.KindEvent,
			Name:        "Change your view",
			Description: "Press [v] to cycle the camera view.",
			Params:      missions.Params{Action: missions.ActionCycleView},
		}},
	}
	lines := v.missionChipLines("", false, m)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Press [v]") {
		t.Errorf("tutorial chip should surface the hint instruction:\n%s", joined)
	}
	if len(lines) != 3 {
		t.Errorf("tutorial chip = %d lines, want 3 (header + objective + hint):\n%s", len(lines), joined)
	}

	// A challenge-program mission keeps the clean one-liner (no hint).
	m.Program = missions.ProgramChallenge
	if got := v.missionChipLines("", false, m); len(got) != 2 {
		t.Errorf("challenge chip = %d lines, want 2 (no hint):\n%s", len(got), strings.Join(got, "\n"))
	}
}

func TestMissionChipLinesNilWhenIdle(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	if got := v.missionChipLines("", false, nil); got != nil {
		t.Errorf("no mission + no flash chip = %v, want nil", got)
	}
}

// TestBuildMissionsChipReadsWorld confirms the builder pulls the active
// mission out of the World (the no-flash path) — the wiring missionChipLines
// can't cover on its own.
func TestBuildMissionsChipReadsWorld(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{{
		ID:   "m1",
		Name: "Reach Orbit",
		Objectives: []missions.Objective{
			{Kind: missions.KindOrbitInsertion, Name: "make orbit"},
		},
	}}
	chip := v.buildMissionsChip(w)
	if chip == nil {
		t.Fatal("buildMissionsChip = nil with an active mission")
	}
	if !strings.Contains(strings.Join(chip, "\n"), "Reach Orbit") {
		t.Errorf("chip did not read the active mission name:\n%s", strings.Join(chip, "\n"))
	}
}
