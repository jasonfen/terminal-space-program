package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
	"regexp"
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
	testStateDirs(t) // never touch the real save/settings directories
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

// formFrameScreens are the screens that live inside the shared frame
// (B11 / G9 Q4). The pause menu is a card over the map (Q6) and the
// maneuver planner keeps its own two-box layout.
var formFrameScreens = []struct {
	name   string
	screen screenID
}{
	{"settings", screenSettings},
	{"controls", screenControls},
	{"missions", screenMissions},
	{"saves", screenSaves},
	{"spawn", screenSpawn},
	{"vab", screenVAB},
	{"help", screenHelp},
	{"session", screenSession},
	{"bodyinfo", screenBodyInfo},
}

// TestFormsLiveInsideTheSharedFrame (B11 / G9 Q4): through App.View every
// form is exactly the terminal's size, no row wraps or overflows, the
// block under the Title Row opens with the rounded frame's top edge and
// closes with a bottom edge carrying the key legend (the band a flash
// overlays), and the flash lands on that edge without breaking the frame.
func TestFormsLiveInsideTheSharedFrame(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for _, tc := range formFrameScreens {
			a := newChromeApp(t, sz[0], sz[1])
			a.active = tc.screen
			plain := stripANSIForTest(a.View())
			lines := strings.Split(plain, "\n")
			if len(lines) != sz[1] {
				t.Errorf("%s %dx%d: %d rows, want %d", tc.name, sz[0], sz[1], len(lines), sz[1])
				continue
			}
			for i, ln := range lines {
				if w := lipgloss.Width(ln); w > sz[0] {
					t.Errorf("%s %dx%d: row %d is %d cells wide (overflow)", tc.name, sz[0], sz[1], i, w)
				}
			}
			if !strings.HasPrefix(lines[1], "╭") || !strings.HasSuffix(lines[1], "╮") {
				t.Errorf("%s %dx%d: row 1 is not the frame's top edge: %q", tc.name, sz[0], sz[1], lines[1])
			}
			last := lines[len(lines)-1]
			if !strings.HasPrefix(last, "╰─ ") || !strings.HasSuffix(last, "╯") || !strings.Contains(last, "[") {
				t.Errorf("%s %dx%d: bottom edge carries no legend: %q", tc.name, sz[0], sz[1], last)
			}
			// A flash rides the same edge and leaves the frame whole.
			a.flash("hello flash")
			lines = strings.Split(stripANSIForTest(a.View()), "\n")
			last = lines[len(lines)-1]
			if !strings.HasPrefix(last, "╰") || !strings.HasSuffix(last, "╯") || !strings.Contains(last, "hello flash") {
				t.Errorf("%s %dx%d: flash broke the bottom edge: %q", tc.name, sz[0], sz[1], last)
			}
		}
	}
}

// TestFormsUseOneCursorAndOneCyclingMark (B11 / G9 Q5): ▸ is the only list
// cursor, ‹ value › the only "left/right changes this" mark, and ➤ (the
// vessel's mark on the map) is never a list bullet.
func TestFormsUseOneCursorAndOneCyclingMark(t *testing.T) {
	for _, tc := range formFrameScreens {
		a := newChromeApp(t, 140, 40)
		a.active = tc.screen
		plain := stripANSIForTest(a.View())
		for _, bad := range []string{"➤", "◀", "▶"} {
			if strings.Contains(plain, bad) {
				t.Errorf("%s: screen still draws %q", tc.name, bad)
			}
		}
		if strings.Contains(plain, "│> ") {
			t.Errorf("%s: screen still uses the '>' cursor", tc.name)
		}
		if regexp.MustCompile(`│ *→ `).MatchString(plain) {
			t.Errorf("%s: screen still uses the '→' cursor", tc.name)
		}
	}
	// The screens that have cursor rows draw the one cursor.
	for _, tc := range []struct {
		name   string
		screen screenID
		open   screens.MenuAction
	}{
		{"settings", screenSettings, screens.MenuActionNone}, {"spawn", screenSpawn, screens.MenuActionNone},
		{"vab", screenVAB, screens.MenuActionVAB}, {"saves", screenSaves, screens.MenuActionSave},
		{"help", screenHelp, screens.MenuActionNone}, {"controls", screenControls, screens.MenuActionNone},
	} {
		a := newChromeApp(t, 140, 40)
		a.active = tc.screen
		if tc.open != screens.MenuActionNone {
			a.applyMenuAction(tc.open) // the real opener resets the screen's state
		}
		if !strings.Contains(stripANSIForTest(a.View()), "▸") {
			t.Errorf("%s: no ▸ cursor on screen", tc.name)
		}
	}
	// The cycling fields use ‹ value ›.
	for _, tc := range []struct {
		name   string
		screen screenID
	}{{"settings", screenSettings}, {"spawn", screenSpawn}, {"controls", screenControls}} {
		a := newChromeApp(t, 140, 40)
		a.active = tc.screen
		if !strings.Contains(stripANSIForTest(a.View()), "‹ ") {
			t.Errorf("%s: no ‹ value › cycling mark on screen", tc.name)
		}
	}
}
