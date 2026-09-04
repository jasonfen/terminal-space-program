package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The mission checklist chip (ADR 0025 / Slice 5) is a one-liner: the active
// mission name, its current objective, and N/M progress — with a transient
// failure flash when a mission dies. missionChipLines is the pure content
// selector, exercised here without needing a live World to arm the flash.
// The trailing relayCount int (#426) only matters for a relay_coverage
// objective; every other test here passes 0 and ignores it.

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
	lines := v.missionChipLines("", false, m, 0)
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
	lines := v.missionChipLines("Land Test failed: crashed", true, m, 0)
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
	lines := v.missionChipLines("", false, m, 0)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Press [v]") {
		t.Errorf("tutorial chip should surface the hint instruction:\n%s", joined)
	}
	if len(lines) != 3 {
		t.Errorf("tutorial chip = %d lines, want 3 (header + objective + hint):\n%s", len(lines), joined)
	}

	// A challenge-program mission keeps the clean one-liner (no hint).
	m.Program = missions.ProgramChallenge
	if got := v.missionChipLines("", false, m, 0); len(got) != 2 {
		t.Errorf("challenge chip = %d lines, want 2 (no hint):\n%s", len(got), strings.Join(got, "\n"))
	}
}

func TestMissionChipLinesNilWhenIdle(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	if got := v.missionChipLines("", false, nil, 0); got != nil {
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

// TestMissionChipShowsLiveRelayCount — #426 (CONTEXT.md Player surface entry):
// while a relay_coverage objective is the current one, the chip carries a
// live "relays online N/3" row alongside the generic progress line, in both
// the Full and Compact forms. World.ConnectedRelayCount() is the same count
// the evaluator itself uses (evalRelayCoverage via missionEvalContext) — not
// re-derived here.
func TestMissionChipShowsLiveRelayCount(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	m := &missions.Mission{
		ID:      "chal-relay",
		Name:    "Relay Constellation",
		Program: missions.ProgramChallenge,
		Objectives: []missions.Objective{{
			Kind:   missions.KindRelayCoverage,
			Name:   "Three relays connected",
			Params: missions.Params{MinRelays: 3},
		}},
	}
	full := strings.Join(v.missionChipLines("", false, m, 1), "\n")
	if !strings.Contains(full, "relays online 1/3") {
		t.Errorf("full-form chip missing live relay count:\n%s", full)
	}
	compact := strings.Join(v.missionChipLinesCompact("", false, m, 1), "\n")
	if !strings.Contains(compact, "relays online 1/3") {
		t.Errorf("compact-form chip missing live relay count:\n%s", compact)
	}

	// A non-relay-coverage objective never carries the row, regardless of
	// what relayCount is passed.
	other := &missions.Mission{
		ID: "m2", Name: "Reach Orbit",
		Objectives: []missions.Objective{{Kind: missions.KindOrbitInsertion, Name: "make orbit"}},
	}
	if got := strings.Join(v.missionChipLines("", false, other, 2), "\n"); strings.Contains(got, "relays online") {
		t.Errorf("non-relay-coverage chip should not show relay count:\n%s", got)
	}
}

// TestBuildMissionsChipReadsLiveRelayCountFromWorld is the wiring test:
// buildMissionsChip pulls the count from World.ConnectedRelayCount() rather
// than a caller-supplied value.
func TestBuildMissionsChipReadsLiveRelayCountFromWorld(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = []missions.Mission{{
		ID:   "chal-relay",
		Name: "Relay Constellation",
		Objectives: []missions.Objective{{
			Kind:   missions.KindRelayCoverage,
			Name:   "Three relays connected",
			Params: missions.Params{MinRelays: 3},
		}},
	}}
	// No CommGraph built yet (no Tick run) → ConnectedRelayCount is 0.
	chip := strings.Join(v.buildMissionsChip(w), "\n")
	if !strings.Contains(chip, "relays online 0/3") {
		t.Errorf("chip should read World.ConnectedRelayCount() (0 before any Tick):\n%s", chip)
	}
}

// TestMissionChipHintWrapsInsteadOfWideningTheBox (ADR 0046 / #422): a
// long tutorial hint ("Warp to your burn ([G]) or fire manually ([b])…",
// 87 columns in the 2026-09-02 UX review's gameplay-flow-progression-09
// dump) used to size the whole chip to its longest line and overdraw the
// navball beside it at 120 columns. The hint must wrap at
// missionChipWrapWidth instead of setting the box's width, in both the
// Full form and the Compact Form.
func TestMissionChipHintWrapsInsteadOfWideningTheBox(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	longHint := "Warp to your burn ([G]) or fire it manually ([b]) once the countdown reaches zero and the node is armed."
	m := &missions.Mission{
		ID:      "tut",
		Name:    "Flight School",
		Program: missions.ProgramTutorial,
		Objectives: []missions.Objective{{
			Kind:        missions.KindEvent,
			Name:        "Plan a Burn",
			Description: longHint,
		}},
	}

	full := v.missionChipLines("", false, m, 0)
	for _, l := range full {
		if w := lipgloss.Width(l); w > missionChipWrapWidth+4 { // +4 for the "    " hint indent
			t.Errorf("full-form line exceeds the wrap width (%d): %q (%d cols)", missionChipWrapWidth, l, w)
		}
	}
	if len(full) < 3 {
		t.Fatalf("full form should wrap the long hint across multiple rows, got %d lines:\n%s", len(full), strings.Join(full, "\n"))
	}

	compact := v.missionChipLinesCompact("", false, m, 0)
	for _, l := range compact {
		if w := lipgloss.Width(l); w > missionChipWrapWidth+4 {
			t.Errorf("compact-form line exceeds the wrap width (%d): %q (%d cols)", missionChipWrapWidth, l, w)
		}
	}
	// Compact keeps objective + ONE wrapped hint line, not the hint's full
	// multi-line text.
	if len(compact) != 3 {
		t.Errorf("compact form = %d lines, want 3 (header + objective + one hint row):\n%s", len(compact), strings.Join(compact, "\n"))
	}
	if !strings.Contains(compact[len(compact)-1], "…") {
		t.Errorf("compact hint row should end with an ellipsis when the hint needed more than one wrapped line:\n%s", strings.Join(compact, "\n"))
	}
}
