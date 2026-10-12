package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// App-level guards for #602: the bar is in what App.View() returns, in every
// map projection the `v` key reaches, follows the zoom and focus keys, and
// is there in the Proximity and launch views. (Honesty against the canvas is
// pinned in screens/scalebar_test.go, where the canvas is reachable.)

var appScaleBarRE = regexp.MustCompile(`├─+┤ \d+(?:\.\d+)? (?:m|km|Mm|Gm|AU)`)
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func appBar(a *App) string {
	return appScaleBarRE.FindString(ansiRE.ReplaceAllString(a.View(), ""))
}

func sizedApp(t *testing.T, cols, rows int) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: cols, Height: rows})
	return a
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

var scaleBarSizes = [][2]int{{140, 40}, {181, 49}}

func TestScaleBarAppEveryProjectionViaTheVKey(t *testing.T) {
	for _, sz := range scaleBarSizes {
		a := sizedApp(t, sz[0], sz[1])
		a.world.ViewMode = sim.ViewTilted
		for i := 0; i < len(sim.ProjectionViewModes); i++ {
			if bar := appBar(a); bar == "" {
				t.Errorf("%dx%d %s: App.View() has no scale bar", sz[0], sz[1], a.world.ViewMode.Label())
			}
			a.Update(runeKey('v'))
		}
	}
}

func TestScaleBarAppFollowsZoomAndFocusKeys(t *testing.T) {
	for _, sz := range scaleBarSizes {
		a := sizedApp(t, sz[0], sz[1])
		a.world.ViewMode = sim.ViewTop
		base := appBar(a)
		if base == "" {
			t.Fatalf("%dx%d: no bar to start from", sz[0], sz[1])
		}
		for i := 0; i < 5; i++ {
			a.Update(runeKey('+'))
		}
		in := appBar(a)
		if in == "" || in == base {
			t.Errorf("%dx%d: [+] x5 left the bar at %q (was %q)", sz[0], sz[1], in, base)
		}
		for i := 0; i < 10; i++ {
			a.Update(runeKey('-'))
		}
		out := appBar(a)
		if out == "" || out == in || out == base {
			t.Errorf("%dx%d: [-] x10 gave %q (zoomed in %q, start %q)", sz[0], sz[1], out, in, base)
		}
		a.Update(runeKey('g'))
		if appBar(a) == "" {
			t.Errorf("%dx%d: the bar is gone after [g]", sz[0], sz[1])
		}
		before := appBar(a)
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}, Alt: true})
		after := appBar(a)
		if after == "" || after == before {
			t.Errorf("%dx%d: [alt+g] (focus: system) left the bar at %q (was %q)", sz[0], sz[1], after, before)
		}
	}
}

func TestScaleBarAppProximityAndLaunch(t *testing.T) {
	for _, sz := range scaleBarSizes {
		a := proximityApp(t)
		a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		if bar := appBar(a); bar == "" {
			t.Errorf("%dx%d: Proximity View has no scale bar", sz[0], sz[1])
		}
		pressO(a) // back to the map
		pressV(a)
		if a.world.ViewMode != sim.ViewLaunch {
			t.Fatalf("setup: not in the launch view (%s)", a.world.ViewMode)
		}
		if bar := appBar(a); bar == "" {
			t.Errorf("%dx%d: launch view has no scale bar\n%s", sz[0], sz[1], strings.Split(a.View(), "\n")[sz[1]-2])
		}
	}
}
