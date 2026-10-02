package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// lmDescent57 is the 57-6 Apollo LM state (G5 grounding A) with the
// descent tank at fuelKg.
func lmDescent57(t *testing.T, fuelKg float64) *sim.World {
	t.Helper()
	w := descendingMoonCraft(t, 7528, 11.6)
	c := w.ActiveCraft()
	stages, _ := spacecraft.BuildModule(spacecraft.StageModuleApolloCSMLMID)
	c.Stages = stages[2:]
	c.Stages[0].FuelMass = fuelKg
	c.SyncFields()
	c.State.V = orbital.Vec3{X: -11.6, Y: 63.5}
	c.State.M = c.TotalMass()
	return w
}

func navStopRow(t *testing.T, lines []string) string {
	t.Helper()
	for _, l := range lines {
		if strings.Contains(l, "impact:") {
			return l
		}
	}
	t.Fatal("no impact:/stop: row")
	return ""
}

// TestNavigationStopNamesTheLitStage (#465, G5 Q2/Q3): the stop: cell
// names the lit stage, the title goes TIGHT then NO LAND before the tank
// is dry, and a dry lit stage says which stage is dry and which is aboard.
// Every line stays inside the right tier (64 outer).
func TestNavigationStopNamesTheLitStage(t *testing.T) {
	cases := []struct {
		name      string
		fuel      float64
		wantRow   string
		wantTitle string
	}{
		// 460 kg, not the 57-6 capture's 477: with the corrected suicide-burn cost
		// (review LOW 57) 477 kg leaves 29 m/s spare in this harness (OK, over the
		// 10% line); 460 kg is TIGHT.
		{"tight", 460, "(Descent)", "⚠ TIGHT"},
		{"no land", 250, "(Descent)", "⚠ NO LAND"},
		{"dry", 0, "Descent dry", "Descent dry, Ascent aboard"},
	}
	for _, tc := range cases {
		v := NewOrbitView(launchThemeForTest())
		w := lmDescent57(t, tc.fuel)
		lines := v.buildNavigationBox(w)
		if row := navStopRow(t, lines); !strings.Contains(row, tc.wantRow) {
			t.Errorf("%s: stop row %q lacks %q", tc.name, row, tc.wantRow)
		}
		if !strings.Contains(lines[0], tc.wantTitle) {
			t.Errorf("%s: title %q lacks %q", tc.name, lines[0], tc.wantTitle)
		}
		for _, l := range lines {
			if lipgloss.Width(l) > tierRightWidth-2 {
				t.Errorf("%s: line %q is %d cells, over the right tier's %d", tc.name, l, lipgloss.Width(l), tierRightWidth-2)
			}
		}
	}
}

func TestStopMarginLabelFitsBudget(t *testing.T) {
	for _, stage := range []string{"Descent", "S-IVB", "Falcon-9 first stage", "X"} {
		for _, dist := range []string{"7.45 km", "999.9 km", "120 m"} {
			if l := stopMarginLabel(dist, stage); lipgloss.Width(l) > stopCellBudget {
				t.Errorf("%q is %d cells, budget %d", l, lipgloss.Width(l), stopCellBudget)
			}
		}
	}
}
