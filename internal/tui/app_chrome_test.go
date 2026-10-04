package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
	"github.com/muesli/termenv"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/save"
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
	// Paused (B11 follow-up): the clock must not move when PAUSED appears,
	// and the middle reads the warp you will resume at, never 0x.
	a.world.ViewMode = sim.ViewTilted
	a.world.Clock.WarpIdx = 2
	runningDist := fromRight()
	a.world.Clock.Paused = true
	for name, screen := range map[string]screenID{"map": screenOrbit, "settings": screenSettings, "help": screenHelp} {
		a.active = screen
		if d := fromRight(); d != runningDist {
			t.Errorf("paused %s clock is %d cells from the right edge, running map's is %d", name, d, runningDist)
		}
		row := firstRow(a)
		want := fmt.Sprintf("warp %.0fx", sim.WarpFactors[2])
		if !strings.Contains(row, want+"  ") || !strings.Contains(row, "PAUSED") || strings.Contains(row, "warp 0x") {
			t.Errorf("paused %s row = %q, want %q then PAUSED and no warp 0x", name, row, want)
		}
		if strings.Index(row, "PAUSED") < strings.Index(row, want) {
			t.Errorf("paused %s row = %q, PAUSED should follow the warp", name, row)
		}
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
	// menu as its opener (stale-flag guard). A stale fromMenu is injected on
	// the map, then a real key (F1) opens Help: the map-key line in Update
	// (app.go, "A key on the map clears any stale menu-opener") must clear
	// it, so esc goes to the map and not back to the menu.
	a = newChromeApp(t, 140, 40)
	a.active = screenOrbit
	a.fromMenu = true
	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	if a.active != screenHelp {
		t.Fatalf("F1 on the map opened %v, want Help", a.active)
	}
	esc(a)
	if a.active != screenOrbit {
		t.Errorf("Help opened from the map went back to %v, want the map", a.active)
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

// TestPauseMenuReallyPauses (B11 / G9 Q6): the "pause menu" now pauses. Esc on
// the map stops the clock, a world tick through the real Update path does not
// advance sim time while it is open, the flight bar reads PAUSED, and esc
// hands the clock back exactly as it was (also when it was already stopped).
func TestPauseMenuReallyPauses(t *testing.T) {
	for _, wasPaused := range []bool{false, true} {
		a := newChromeApp(t, 140, 40)
		a.world.Clock.Paused = wasPaused
		esc(a)
		if a.active != screenMenu {
			t.Fatalf("esc on the map opened %v, want the menu", a.active)
		}
		if !a.world.Clock.Paused {
			t.Fatalf("wasPaused=%v: the menu is open and the clock is running", wasPaused)
		}
		t0 := a.world.Clock.SimTime
		for i := 0; i < 5; i++ {
			a.Update(sim.TickMsg(time.Now()))
		}
		if !a.world.Clock.SimTime.Equal(t0) {
			t.Errorf("wasPaused=%v: sim time moved %v -> %v behind the open menu", wasPaused, t0, a.world.Clock.SimTime)
		}
		if row := firstRow(a); !strings.Contains(row, "PAUSED") {
			t.Errorf("wasPaused=%v: flight bar does not read PAUSED: %q", wasPaused, row)
		}
		esc(a)
		if a.active != screenOrbit {
			t.Fatalf("esc in the menu went to %v, want the map", a.active)
		}
		if a.world.Clock.Paused != wasPaused {
			t.Errorf("wasPaused=%v: after closing the menu the clock is paused=%v", wasPaused, a.world.Clock.Paused)
		}
	}
	// The [Menu] button pauses the same way.
	a := newChromeApp(t, 140, 40)
	a.View()
	col, ok := findMenuButtonCol(a)
	if !ok {
		t.Fatal("no [Menu] hit range")
	}
	a.Update(tea.MouseMsg{X: col, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if a.active != screenMenu || !a.world.Clock.Paused {
		t.Errorf("[Menu] click: active %v paused %v, want the menu and a stopped clock", a.active, a.world.Clock.Paused)
	}
}

func findMenuButtonCol(a *App) (int, bool) {
	for c := 0; c < a.width; c++ {
		if a.orbitView.HitMenuButton(c, 0) {
			return c, true
		}
	}
	return 0, false
}

// TestPauseCardOverDimmedMap (B11 / G9 Q6): the menu is a 40x13 card centred
// over the map, which stays in view, dimmed; clicks land on the card's rows.
func TestPauseCardOverDimmedMap(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })
	const cyan, grey = "38;2;95;215;255", "38;2;95;95;95"
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		a := newChromeApp(t, sz[0], sz[1])
		esc(a)
		raw := a.View()
		rawLines := strings.Split(raw, "\n")
		plain := strings.Split(stripANSIForTest(raw), "\n")
		if len(plain) != sz[1] {
			t.Fatalf("%dx%d: %d rows", sz[0], sz[1], len(plain))
		}
		// The card: find its top edge (the row holding the wordmark minus one).
		wordRow := -1
		for i := 1; i < len(plain); i++ {
			if strings.Contains(plain[i], "Terminal Space Program") {
				wordRow = i
			}
		}
		if wordRow < 0 {
			t.Fatalf("%dx%d: no card wordmark on screen", sz[0], sz[1])
		}
		wl := plain[wordRow]
		x := lipgloss.Width(wl[:strings.Index(wl, "Terminal Space Program")]) - 3 // "│  " before the wordmark
		cell := func(row string, col int) string {
			return ansi.Truncate(ansi.TruncateLeft(row, col, ""), 1, "")
		}
		if cell(plain[wordRow-1], x) != "╭" || cell(plain[wordRow-1], x+39) != "╮" ||
			cell(plain[wordRow+11], x) != "╰" || cell(plain[wordRow+11], x+39) != "╯" {
			t.Fatalf("%dx%d: card is not a 40x13 box at col %d:\n%s", sz[0], sz[1], x, strings.Join(plain[wordRow-1:wordRow+12], "\n"))
		}
		// Centred (within a cell).
		if want := (sz[0] - 40) / 2; x < want-1 || x > want+1 {
			t.Errorf("%dx%d: card left edge at %d, want about %d", sz[0], sz[1], x, want)
		}
		// The map is still there under the dim: braille cells outside the card.
		braille := 0
		for _, ln := range plain[2 : len(plain)-1] {
			for _, r := range ln {
				if r >= 0x2801 && r <= 0x28FF {
					braille++
				}
			}
		}
		if braille < 100 {
			t.Errorf("%dx%d: only %d braille cells visible: the map is gone behind the card", sz[0], sz[1], braille)
		}
		// Dimmed: the frame's top edge (row 1, outside the card) is grey, not cyan.
		if strings.Contains(rawLines[1], cyan) || !strings.Contains(rawLines[1], grey) {
			t.Errorf("%dx%d: map frame row is not dimmed: %q", sz[0], sz[1], rawLines[1])
		}
		// Clicking the Settings row opens Settings.
		settingsRow := -1
		for i, ln := range plain {
			if strings.Contains(ln, "Keyboard layout") {
				settingsRow = i - 1 // Settings is the row above
			}
		}
		a.Update(tea.MouseMsg{X: x + 5, Y: settingsRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if a.active != screenSettings {
			t.Errorf("%dx%d: click on the Settings row opened %v", sz[0], sz[1], a.active)
		}
	}
}

// TestQuitFromThePauseMenuStillAutosaves (B11 / G9 Q6): the menu now holds
// the clock, and autosave refuses a paused world, so quitting from the menu
// must give the menu's hold back first or the quit would silently write
// nothing (and a restart mid-menu would come back frozen).
func TestQuitFromThePauseMenuStillAutosaves(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	esc(a)
	press(a, "q")
	if !a.quitConfirm {
		t.Fatal("q in the menu did not arm the quit prompt")
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y did not quit")
	}
	if n := len(savesDirFiles(t, dir)); n == 0 {
		t.Error("quitting from the pause menu wrote no autosave")
	}
}

// clickOn finds text in the plain View and clicks its first cell through
// App.Update, the way a mouse would arrive (screen coordinates, row 0 the
// Title Row). Returns false when the text is not on screen.
func clickOn(a *App, text string) bool {
	for y, ln := range strings.Split(stripANSIForTest(a.View()), "\n") {
		if i := strings.Index(ln, text); i >= 0 {
			x := lipgloss.Width(ln[:i])
			a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
			return true
		}
	}
	return false
}

// TestFormClicksStillLandAfterTheFrame (B11 / G9 Q4): the frame, the boxes
// and the Title Row moved every form row; clicks, which arrive in screen
// coordinates, must still land on what is drawn. Production seam: real
// View, real MouseMsg through App.Update.
func TestFormClicksStillLandAfterTheFrame(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		// Settings: a chip row toggles, the autosave row cycles.
		a := newChromeApp(t, sz[0], sz[1])
		a.active = screenSettings
		a.settingsScreen.Reset()
		first := a.orbitView.Settings().ChipEnabled(settings.AllChips[2])
		if !clickOn(a, settings.AllChips[2].Label()) {
			t.Fatalf("%dx%d: no %s row on screen", sz[0], sz[1], settings.AllChips[2].Label())
		}
		if a.orbitView.Settings().ChipEnabled(settings.AllChips[2]) == first {
			t.Errorf("%dx%d: clicking the %s row did not toggle it", sz[0], sz[1], settings.AllChips[2].Label())
		}
		before := a.orbitView.Settings().AutosaveIntervalMinutes()
		if !clickOn(a, "Autosave interval") {
			t.Fatalf("%dx%d: no autosave row on screen", sz[0], sz[1])
		}
		if a.orbitView.Settings().AutosaveIntervalMinutes() == before {
			t.Errorf("%dx%d: clicking the autosave row did not cycle it", sz[0], sz[1])
		}
		// Saves (save mode): clicking the already-selected New-save row opens the name prompt.
		a = newChromeApp(t, sz[0], sz[1])
		a.active = screenMenu
		a.applyMenuAction(screens.MenuActionSave)
		if !clickOn(a, "New save") {
			t.Fatalf("%dx%d: no New save row", sz[0], sz[1])
		}
		if !strings.Contains(stripANSIForTest(a.View()), "name the new save") {
			t.Errorf("%dx%d: clicking the selected New-save row did not open the name prompt", sz[0], sz[1])
		}
		// Keyboard layout: the row cycles QWERTY <-> QWERTZ.
		a = newChromeApp(t, sz[0], sz[1])
		a.active = screenControls
		lay := a.layout
		if !clickOn(a, "Keyboard layout:") {
			t.Fatalf("%dx%d: no layout row", sz[0], sz[1])
		}
		if a.layout == lay {
			t.Errorf("%dx%d: clicking the layout row did not change the layout", sz[0], sz[1])
		}
	}
}

// TestSaveFromThePauseMenuLoadsAsFlown (B11 follow-up): the pause menu and
// the Saves screen both hold the clock while they are up. A save written
// from there must record the pause state from BEFORE those holds, or every
// menu save loads frozen. Real key path: menu, Save Game, save as, load it
// back. A save made while the player had really paused still loads paused.
func TestSaveFromThePauseMenuLoadsAsFlown(t *testing.T) {
	for _, wasPaused := range []bool{false, true} {
		a := newChromeApp(t, 140, 40)
		a.world.Clock.Paused = wasPaused
		openSavesVia(t, a, "s")
		press(a, "enter") // New save row, naming
		press(a, "enter") // accept the default name
		if a.active != screenMenu {
			t.Fatalf("wasPaused=%v: Save-As left %v, want the menu", wasPaused, a.active)
		}
		press(a, "esc") // fly on
		if a.world.Clock.Paused != wasPaused {
			t.Errorf("wasPaused=%v: live clock paused=%v after the save", wasPaused, a.world.Clock.Paused)
		}
		openSavesVia(t, a, "l")
		press(a, "enter") // the one named save
		press(a, "enter") // confirm the load
		if a.active != screenOrbit {
			t.Fatalf("wasPaused=%v: load left %v, want the map", wasPaused, a.active)
		}
		if a.world.Clock.Paused != wasPaused {
			t.Errorf("wasPaused=%v: the loaded save has Clock.Paused=%v", wasPaused, a.world.Clock.Paused)
		}
	}
	// Overwrite goes through the same hold.
	a := newChromeApp(t, 140, 40)
	if _, err := save.WriteNamed(a.world, "Old"); err != nil {
		t.Fatal(err)
	}
	openSavesVia(t, a, "s")
	press(a, "down")  // onto the existing save
	press(a, "enter") // overwrite confirm
	press(a, "enter")
	if a.active != screenMenu {
		t.Fatalf("overwrite left %v, want the menu", a.active)
	}
	press(a, "esc")
	openSavesVia(t, a, "l")
	press(a, "enter")
	press(a, "enter")
	if a.active != screenOrbit || a.world.Clock.Paused {
		t.Errorf("overwritten save loaded: active %v paused %v, want the map running", a.active, a.world.Clock.Paused)
	}
}

// TestLoadFromThePauseMenuReleasesTheMenuHold (B11 review M1): loadWorldByID
// must drop the pause menu's hold (menuHeld). If it did not, the next quit
// would put the stale pre-load menuPrevPaused back on the NEW world's clock,
// and a true there makes autosave write nothing (the silent failure #553
// fixed for the plain menu quit). Scenario: the player had paused, opened the
// menu, loaded a save that was written while running, then quit from the map
// (ctrl+c, y). Real key path throughout; the esc-then-q route would mask the
// bug because openMenu re-reads the clock.
func TestLoadFromThePauseMenuReleasesTheMenuHold(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.world.Clock.Paused = false
	if _, err := save.WriteNamed(a.world, "Running"); err != nil {
		t.Fatal(err)
	}
	a.world.Clock.Paused = true // the player paused before opening the menu
	openSavesVia(t, a, "l")
	press(a, "enter") // the one named save
	press(a, "enter") // confirm the load
	if a.active != screenOrbit {
		t.Fatalf("load left %v, want the map", a.active)
	}
	if a.menuHeld {
		t.Error("menuHeld still true after loading from the pause menu")
	}
	if a.world.Clock.Paused {
		t.Fatal("the loaded world should be running (saved while running)")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !a.quitConfirm {
		t.Fatal("ctrl+c did not arm the quit prompt")
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("y did not quit")
	}
	auto := 0
	for _, f := range savesDirFiles(t, dir) {
		if strings.HasPrefix(f, "autosave-") {
			auto++
		}
	}
	if auto == 0 {
		t.Error("quitting after a menu load wrote no autosave (stale menu hold paused the clock)")
	}
}

// TestPauseCardKeepsAClearMarginOverTheMap (B11 review L11): the card is
// spliced over the map by cell, so a map box border used to join the card
// corner (`╭───╭`). One clear cell all round keeps the card's edge its own.
// Pinned over the map and the launch view at both design sizes, with the
// card's rows and the rows just above and below it.
func TestPauseCardKeepsAClearMarginOverTheMap(t *testing.T) {
	for _, launch := range []bool{false, true} {
		for _, sz := range [][2]int{{140, 40}, {181, 49}} {
			a := newChromeApp(t, sz[0], sz[1])
			if launch {
				a.world.ViewMode = sim.ViewLaunch
			}
			esc(a)
			plain := strings.Split(stripANSIForTest(a.View()), "\n")
			top := -1
			for i, ln := range plain {
				if strings.Contains(ln, "╭") && i > 0 && strings.Contains(plain[i+1], "Terminal Space Program") {
					top = i
				}
			}
			if top < 0 {
				t.Fatalf("launch=%v %dx%d: no card found", launch, sz[0], sz[1])
			}
			x := (sz[0] - 40) / 2
			cell := func(row string, col int) string {
				return ansi.Truncate(ansi.TruncateLeft(row, col, ""), 1, "")
			}
			for r := top - 1; r <= top+13; r++ {
				if r < 1 || r >= len(plain) {
					continue
				}
				inCard := r >= top && r < top+13
				cols := []int{x - 1, x + 40}
				if !inCard {
					cols = cols[:0]
					for c := x - 1; c <= x+40; c++ {
						cols = append(cols, c)
					}
				}
				for _, c := range cols {
					if got := cell(plain[r], c); got != " " {
						t.Errorf("launch=%v %dx%d: row %d col %d is %q, want a clear margin cell:\n%s",
							launch, sz[0], sz[1], r, c, got, plain[r])
					}
				}
			}
		}
	}
}
