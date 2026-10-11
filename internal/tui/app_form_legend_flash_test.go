package tui

import (
	"strings"
	"testing"
)

// TestFormLegendSurvivesFlash pins #599: the key legend of spawn, VAB,
// settings and saves rides the form frame's bottom edge, the band a status
// flash used to overwrite, so for the flash's lifetime the legend was gone.
// Every form now gets the planner's mechanism (review #555 L1): the flash
// has a row of its own under the frame. Through the production seam
// (App.View with a live flash) both must be on screen and the frame must
// still fit, at both Design Sizes.
func TestFormLegendSurvivesFlash(t *testing.T) {
	const flash = "settings: guest sessions cannot change this"
	forms := []struct {
		name   string
		screen screenID
		legend string
	}{
		{"spawn", screenSpawn, "[tab] field"},
		{"vab", screenVAB, "[esc] back to the menu"},
		{"settings", screenSettings, "[space] toggle"},
		{"saves", screenSaves, "[esc] back"},
	}
	for _, f := range forms {
		for _, sz := range [][2]int{{140, 40}, {181, 49}} {
			a := newChromeApp(t, sz[0], sz[1])
			a.active = f.screen
			if clean := stripANSIForTest(a.View()); !strings.Contains(clean, f.legend) {
				t.Fatalf("%s %dx%d: legend %q not on the clean form (guard blind):\n%s", f.name, sz[0], sz[1], f.legend, clean)
			}
			a.flash(flash)
			out := stripANSIForTest(a.View())
			if !strings.Contains(out, flash) {
				t.Errorf("%s %dx%d: the flash is not drawn:\n%s", f.name, sz[0], sz[1], out)
			}
			if !strings.Contains(out, f.legend) {
				t.Errorf("%s %dx%d: the flash covers the key legend %q", f.name, sz[0], sz[1], f.legend)
			}
			if n := strings.Count(out, "\n") + 1; n > sz[1] {
				t.Errorf("%s %dx%d: frame is %d rows, want <= %d", f.name, sz[0], sz[1], n, sz[1])
			}
		}
	}
}
