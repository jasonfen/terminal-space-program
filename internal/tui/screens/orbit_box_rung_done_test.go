package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #594 (GS Q5): the MISSION box keeps a standing "done: <last finished rung>"
// row under the current rung's title; it is green while the completion flash
// runs and Dim after.

func finishedOrientationWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	w.RecordAction(missions.ActionCycleView)
	w.Tick()
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.Target = spacecraft.Target{Kind: spacecraft.TargetBody, BodyIdx: i}
		}
	}
	w.RecordAction(missions.ActionCycleTarget)
	w.Tick()
	return w
}

func rungTheme() Theme {
	th := chipTestTheme()
	th.Dim = lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5F5F"))
	th.Alert = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F5F"))
	return th
}

func TestMissionBoxStandingDoneRow(t *testing.T) {
	w := finishedOrientationWorld(t)
	v := NewOrbitView(chipTestTheme())
	lines := v.buildMissionBox(w)
	if len(lines) < 3 {
		t.Fatalf("box = %q, want title, done row, step", lines)
	}
	if !strings.Contains(lines[0], "Plan a Burn") {
		t.Errorf("title row = %q", lines[0])
	}
	if got := strings.TrimSpace(lines[1]); got != "✓ done: Orientation" {
		t.Errorf("row under the title = %q, want %q", got, "✓ done: Orientation")
	}
	if !strings.Contains(lines[2], "Open the planner") {
		t.Errorf("step row = %q", lines[2])
	}
}

// Before any rung has finished there is no done row (nothing to say).
func TestMissionBoxNoDoneRowAtStart(t *testing.T) {
	w, _ := sim.NewWorld()
	w.SetEnabledMissionPrograms(map[string]bool{missions.ProgramTutorial: true})
	v := NewOrbitView(chipTestTheme())
	if joined := strings.Join(v.buildMissionBox(w), "\n"); strings.Contains(joined, "done:") {
		t.Errorf("done row with no finished rung:\n%s", joined)
	}
}

func TestMissionBoxDoneRowGreenOnlyDuringFlash(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })
	const green = "38;2;95;255;135"

	// Positive first: the instrument sees colour at all.
	if out := rungTheme().Alert.Render("x"); !strings.Contains(out, "38;2;255;95;95") {
		t.Fatalf("colour profile not forced, Alert rendered %q", out)
	}

	w := finishedOrientationWorld(t)
	v := NewOrbitView(rungTheme())
	if !w.RungDoneFlash() {
		t.Fatal("precondition: flash armed")
	}
	during := v.buildMissionBox(w)[1]
	if !strings.Contains(during, green) {
		t.Errorf("done row not green during the flash: %q", during)
	}
	w.ExpireRungDoneFlash()
	after := v.buildMissionBox(w)[1]
	if strings.Contains(after, green) {
		t.Errorf("done row still green after the flash: %q", after)
	}
	if !strings.Contains(after, "done: Orientation") {
		t.Errorf("standing row lost after the flash: %q", after)
	}
}

// The extra row must not outgrow the tier or push the box past 8 rows
// (6 content + 2 border) at any rung: the 140x40 capture leaves 4 free
// rows under MISSION, and Orientation step 1 is already 7 rows.
func TestMissionBoxDoneRowFitsTier(t *testing.T) {
	cat, err := missions.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	w, _ := sim.NewWorld()
	v := NewOrbitView(chipTestTheme())
	for _, m := range cat.Missions {
		name := strings.TrimPrefix(m.Name, "Flight School: ")
		if got := lipgloss.Width("  ✓ done: " + name); got > tierBottomLeftWidth-2 {
			t.Errorf("done row for %q is %d cells, tier content is %d", m.ID, got, tierBottomLeftWidth-2)
		}
		for cur := range m.Objectives {
			mm := m
			mm.Objectives = append([]missions.Objective(nil), m.Objectives...)
			for j := range mm.Objectives {
				mm.Objectives[j].Status = missions.InProgress
				if j < cur {
					mm.Objectives[j].Status = missions.Passed
				}
			}
			mm.Status = missions.InProgress
			w.Missions = []missions.Mission{
				{ID: "done", Name: "Prev", Program: mm.Program, Status: missions.Passed}, mm,
			}
			mm.Requires = nil // keep the fixture rung active
			w.Missions[1] = mm
			lines := v.withDoneRow(v.missionChipLines("", false, &mm, 0), w)
			if len(lines) < 2 || !strings.Contains(lines[1], "done: Prev") {
				t.Fatalf("%s step %d: fixture produced no done row (vacuous): %q", m.ID, cur, lines)
			}
			if len(lines) > 6 {
				t.Errorf("%s step %d: %d content rows with the done row, want <= 6", m.ID, cur, len(lines))
			}
		}
	}
}
