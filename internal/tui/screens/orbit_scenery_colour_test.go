package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestSceneryOrbitDotsPaintInLighterGrey (#593): through the real orbit view
// with colour forced, a planet or moon orbit paints in #8A8A8A, not the old
// #6E6E6E.
func TestSceneryOrbitDotsPaintInLighterGrey(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	has := func(s string, r, g, b int) bool {
		return strings.Contains(s, fmt.Sprintf("38;2;%d;%d;%d", r, g, b))
	}
	// Planets and moons share one draw site (orbit.go, ClassScenery); the Sun-focus map shows the planet orbits.
	for _, tc := range []struct {
		id   string
		zoom int
	}{{"sun", 30}} {
		id := tc.id
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		idx := -1
		for i, b := range w.System().Bodies {
			if b.ID == id {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("%s not in the default system", id)
		}
		w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: idx}
		w.ViewMode = sim.ViewTop
		v := NewOrbitView(plainTheme())
		v.Render(w, 0, 140, 40)
		for i := 0; i < tc.zoom; i++ {
			v.ZoomOut()
		}
		out := v.Render(w, 0, 140, 40)
		// Instrument check: forced colour yields truecolor foregrounds at all.
		if !strings.Contains(out, "38;2;") {
			t.Fatal("test setup broken: no truecolor foreground in the frame")
		}
		if has(out, 0x6E, 0x6E, 0x6E) {
			t.Errorf("%s focus: scenery orbit still paints in the old #6E6E6E", id)
		}
		if !has(out, 0x6E, 0x6E, 0x6E) && !has(out, 0x8A, 0x8A, 0x8A) {
			t.Errorf("%s focus: no scenery orbit drawn at all (setup broken)", id)
		}
		if !has(out, 0x8A, 0x8A, 0x8A) {
			t.Errorf("%s focus: no cell paints in the new scenery grey #8A8A8A", id)
		}
	}
}
