package tui

import (
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// stripANSIForTest removes SGR escapes so a test can read the cells.
func stripANSIForTest(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func newChromeApp(t *testing.T, w, h int) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return a
}

func firstRow(a *App) string {
	return strings.SplitN(stripANSIForTest(a.View()), "\n", 2)[0]
}

// TestManeuverScreenKeepsTitleRow (B11 / G9 contradiction 3, first commit
// of the slice): the maneuver planner's title row never reached the screen
// because the render was a row too tall. Through the production seam
// (App.View after the `m` screen is active) the whole frame must fit the
// terminal and row 0 must name the planner.
func TestManeuverScreenKeepsTitleRow(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		a := newChromeApp(t, sz[0], sz[1])
		a.active = screenManeuver
		out := a.View()
		if n := strings.Count(out, "\n") + 1; n > sz[1] {
			t.Errorf("%dx%d: frame is %d rows, want <= %d", sz[0], sz[1], n, sz[1])
		}
		if r := firstRow(a); !strings.Contains(r, "Maneuver planner") {
			t.Errorf("%dx%d: row 0 = %q, want the Maneuver planner title", sz[0], sz[1], r)
		}
	}
}

// TestFormScreensWearTheSharedTitleRow (B11 / G9 Q1+Q2): every screen's row
// 0 carries the title-case wordmark with the version, the screen name, the
// clock, and the way out: [Back] on a form, [Menu] on the launch view. A
// click on [Back] does what esc does.
func TestFormScreensWearTheSharedTitleRow(t *testing.T) {
	cases := []struct {
		name   string
		screen screenID
		want   string
	}{
		{"menu", screenMenu, "Menu"},
		{"settings", screenSettings, "Settings"},
		{"controls", screenControls, "Keyboard layout"},
		{"missions", screenMissions, "Missions"},
		{"saves", screenSaves, "Saves"},
		{"spawn", screenSpawn, "Spawn vessel"},
		{"vab", screenVAB, "Vehicle Assembly (VAB)"},
		{"help", screenHelp, "Help"},
		{"session", screenSession, "Session"},
		{"bodyinfo", screenBodyInfo, "Body info"},
		{"maneuver", screenManeuver, "Maneuver planner"},
	}
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for _, tc := range cases {
			a := newChromeApp(t, sz[0], sz[1])
			a.active = tc.screen
			row := firstRow(a)
			if !strings.HasPrefix(row, "Terminal Space Program ") {
				t.Errorf("%s %dx%d: row 0 %q does not start with the title-case wordmark", tc.name, sz[0], sz[1], row)
			}
			if strings.Contains(row, "terminal-space-program") {
				t.Errorf("%s: row 0 still shows the slug: %q", tc.name, row)
			}
			if !strings.Contains(row, tc.want) {
				t.Errorf("%s %dx%d: row 0 %q missing screen name %q", tc.name, sz[0], sz[1], row, tc.want)
			}
			if !strings.Contains(row, "T+") || !strings.Contains(row, "warp") {
				t.Errorf("%s %dx%d: row 0 %q has no clock/warp", tc.name, sz[0], sz[1], row)
			}
			if !strings.Contains(row, "[Back]") {
				t.Errorf("%s %dx%d: row 0 %q has no [Back]", tc.name, sz[0], sz[1], row)
			}
			// [Back] click == esc: the screen must change.
			before := a.active
			a.View()
			a.Update(tea.MouseMsg{X: a.backStart, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			if a.active == before {
				t.Errorf("%s %dx%d: a click on [Back] left the screen on %v", tc.name, sz[0], sz[1], before)
			}
		}
	}
}

// TestTitleRowClockStaysInOneColumn (B11 / G9 Q1): the row must not change
// shape between screens, so the clock sits the same distance from the
// right edge on the map, the launch view and a form (the buttons live in
// a fixed-width zone).
func TestTitleRowClockStaysInOneColumn(t *testing.T) {
	a := newChromeApp(t, 140, 40)
	fromRight := func() int {
		row := []rune(firstRow(a))
		idx := strings.Index(string(row), "T+")
		if idx < 0 {
			t.Fatalf("no clock in %q", string(row))
		}
		return a.width - len([]rune(string(row)[:idx]))
	}
	mapDist := fromRight()
	a.active = screenSettings
	if d := fromRight(); d != mapDist {
		t.Errorf("settings clock is %d cells from the right edge, map's is %d", d, mapDist)
	}
	a.active = screenOrbit
	a.world.ViewMode = sim.ViewLaunch
	if d := fromRight(); d != mapDist {
		t.Errorf("launch clock is %d cells from the right edge, map's is %d", d, mapDist)
	}
}

func esc(a *App) { a.Update(tea.KeyMsg{Type: tea.KeyEsc}) }

// TestBackReturnsToTheScreenThatOpenedYou (B11 / G9 Q3): screens opened
// from the pause menu go back to the menu, a second esc reaches the map;
// screens opened by a key on the map go straight back to the map. Drives
// the real seam: the menu's own action dispatch and the esc key.
func TestBackReturnsToTheScreenThatOpenedYou(t *testing.T) {
	fromMenu := []struct {
		name   string
		action screens.MenuAction
		screen screenID
	}{
		{"settings", screens.MenuActionSettings, screenSettings},
		{"keyboard layout", screens.MenuActionControls, screenControls},
		{"saves (save)", screens.MenuActionSave, screenSaves},
		{"saves (load)", screens.MenuActionLoad, screenSaves},
		{"help", screens.MenuActionHelp, screenHelp},
		{"vab", screens.MenuActionVAB, screenVAB},
	}
	for _, tc := range fromMenu {
		a := newChromeApp(t, 140, 40)
		a.menu.Reset()
		a.active = screenMenu
		a.applyMenuAction(tc.action)
		if a.active != tc.screen {
			t.Fatalf("%s: action opened screen %v, want %v", tc.name, a.active, tc.screen)
		}
		esc(a)
		if a.active != screenMenu {
			t.Errorf("%s: esc went to %v, want the pause menu", tc.name, a.active)
		}
		esc(a)
		if a.active != screenOrbit {
			t.Errorf("%s: second esc went to %v, want the map", tc.name, a.active)
		}
	}
	// [Back] on the Title Row is the same rule.
	a := newChromeApp(t, 140, 40)
	a.active = screenMenu
	a.applyMenuAction(screens.MenuActionSettings)
	a.View()
	a.Update(tea.MouseMsg{X: a.backStart, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if a.active != screenMenu {
		t.Errorf("[Back] from settings went to %v, want the pause menu", a.active)
	}
	// Key-opened screens go straight back to the map.
	for _, scr := range []screenID{screenMissions, screenSpawn, screenBodyInfo, screenSession} {
		a := newChromeApp(t, 140, 40)
		a.active = scr
		esc(a)
		if a.active != screenOrbit {
			t.Errorf("screen %v: esc went to %v, want the map", scr, a.active)
		}
	}
	// A screen opened from the map after a menu trip must not inherit the
	// menu as its opener (stale-flag guard).
	a = newChromeApp(t, 140, 40)
	a.active = screenMenu
	a.applyMenuAction(screens.MenuActionHelp)
	esc(a) // help -> menu
	esc(a) // menu -> map
	a.active = screenHelp
	a.fromMenu = false
	esc(a)
	if a.active != screenOrbit {
		t.Errorf("help opened from the map went back to %v, want the map", a.active)
	}
}
