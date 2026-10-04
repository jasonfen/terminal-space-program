package tui

import (
	"strings"
	"testing"
)

// TestManeuverPlannerLegendSurvivesAFlash pins review #555 L1: the planner
// keeps its own layout with the key legend as the last row, and a flash
// overlays the last row, so for the flash's lifetime the legend was gone
// (porkchop refusals, a flash carried over from the previous screen). The
// flash now has a row of its own. Through the production seam (App.View
// with a live flash) both must be on screen and the frame must still fit,
// at both Design Sizes.
func TestManeuverPlannerLegendSurvivesAFlash(t *testing.T) {
	const flash = "porkchop: same system as your orbit, use H"
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		a := newChromeApp(t, sz[0], sz[1])
		a.active = screenManeuver
		if clean := stripANSIForTest(a.View()); !strings.Contains(clean, "[tab] field") {
			t.Fatalf("%dx%d: the legend is not on the clean planner at all (guard blind):\n%s", sz[0], sz[1], clean)
		}
		a.flash(flash)
		out := stripANSIForTest(a.View())
		if !strings.Contains(out, flash) {
			t.Errorf("%dx%d: the flash is not drawn:\n%s", sz[0], sz[1], out)
		}
		if !strings.Contains(out, "[tab] field") || !strings.Contains(out, "[ctrl+k] clear all") {
			t.Errorf("%dx%d: the flash covers the key legend:\n%s", sz[0], sz[1], out)
		}
		if n := strings.Count(out, "\n") + 1; n > sz[1] {
			t.Errorf("%dx%d: frame is %d rows, want <= %d", sz[0], sz[1], n, sz[1])
		}
	}
}
