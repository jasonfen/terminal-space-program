package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
)

// The screen renders a row for every togglable Chip, in AllChips order,
// each carrying its display Label — the Settings screen's contract that
// nothing in the canonical list is silently unlisted.
func TestSettingsRenderListsEveryChip(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	out := s.Render(settings.Default(), 80, 0)
	for _, c := range settings.AllChips {
		if !strings.Contains(out, c.Label()) {
			t.Errorf("Render is missing chip %q (label %q)", c, c.Label())
		}
	}
	// Default: every chip on (checked), plus Tutorial (on unless switched
	// off, #425); only Challenge ladder defaults off.
	wantChecked := len(settings.AllChips) + 1
	if got := strings.Count(out, "[x]"); got != wantChecked {
		t.Errorf("checked-box count = %d, want %d (all chips + Tutorial on)", got, wantChecked)
	}
	if got := strings.Count(out, "[ ]"); got != gameplayRows-1 {
		t.Errorf("empty-box count = %d, want %d (only Challenge ladder off by default)", got, gameplayRows-1)
	}
	// Both gameplay toggle rows are listed.
	for _, label := range []string{"Tutorial", "Challenge ladder"} {
		if !strings.Contains(out, label) {
			t.Errorf("Render is missing gameplay toggle %q", label)
		}
	}
}

// A disabled Chip renders an empty box; the rest stay checked.
func TestSettingsRenderReflectsDisabled(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	prefs := settings.Default()
	prefs.SetChip(settings.ChipTarget, false)
	out := s.Render(prefs, 80, 0)
	// One disabled chip + the one off-by-default gameplay toggle (Challenge
	// ladder; Tutorial defaults on, #425) = 2 empty.
	if got, want := strings.Count(out, "[ ]"), 1+(gameplayRows-1); got != want {
		t.Errorf("empty-box count = %d, want %d (disabled chip + off gameplay toggles):\n%s", got, want, out)
	}
	if got, want := strings.Count(out, "[x]"), len(settings.AllChips)-1+1; got != want {
		t.Errorf("checked-box count = %d, want %d", got, want)
	}
}

// Up/down move the cursor with wrap-around over the whole list.
func TestSettingsCursorNavigation(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	n := len(settings.AllChips)

	// down lands on the second chip…
	if a, c := s.HandleKey("down"); a != SettingsActionNone {
		t.Fatalf("down returned action %v (want None)", a)
	} else if c != "" {
		t.Fatalf("down returned chip %q (want zero)", c)
	}
	if a, c := s.HandleKey(" "); a != SettingsActionToggle || c != settings.AllChips[1] {
		t.Errorf("toggle after one down = (%v,%q), want (Toggle,%q)", a, c, settings.AllChips[1])
	}

	// up from row 1 → row 0.
	s.HandleKey("up")
	if _, c := s.HandleKey("enter"); c != settings.AllChips[0] {
		t.Errorf("toggle at row 0 = %q, want %q", c, settings.AllChips[0])
	}

	// up wraps from row 0 to the last row, now the Empty readings row
	// (ADR 0051 W6); one more up lands on the autosave-interval row.
	s.HandleKey("up")
	if a, _ := s.HandleKey(" "); a != SettingsActionCycleEmptyReadings {
		t.Errorf("up-wrap toggle action = %v, want CycleEmptyReadings (last row)", a)
	}
	s.HandleKey("up")
	if a, _ := s.HandleKey(" "); a != SettingsActionCycleAutosave {
		t.Errorf("second up toggle action = %v, want CycleAutosave", a)
	}
	_ = n
}

// The two gameplay rows below the chips toggle the tutorial / challenge
// mission programs (v0.21 Slice 7).
func TestSettingsGameplayToggles(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	n := len(settings.AllChips)
	for i := 0; i < n; i++ { // walk down onto the first gameplay row (Tutorial)
		s.HandleKey("down")
	}
	if a, _ := s.HandleKey(" "); a != SettingsActionToggleTutorial {
		t.Errorf("toggle at tutorial row = %v, want ToggleTutorial", a)
	}
	s.HandleKey("down")
	if a, _ := s.HandleKey("enter"); a != SettingsActionToggleChallenges {
		t.Errorf("toggle at challenges row = %v, want ToggleChallenges", a)
	}
}

// The autosave-interval row sits below the gameplay toggles and cycles
// on space/enter (v0.26 S4 / ADR 0033 §E).
func TestSettingsAutosaveIntervalRow(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	n := len(settings.AllChips) + gameplayRows
	for i := 0; i < n; i++ { // walk down onto the autosave row (last)
		s.HandleKey("down")
	}
	if a, _ := s.HandleKey(" "); a != SettingsActionCycleAutosave {
		t.Errorf("action at autosave row = %v, want CycleAutosave", a)
	}
	if a, _ := s.HandleKey("enter"); a != SettingsActionCycleAutosave {
		t.Errorf("enter at autosave row = %v, want CycleAutosave", a)
	}
}

// The autosave row renders the current interval — "5 min" at the
// default, "off" when disabled.
func TestSettingsRenderShowsAutosaveInterval(t *testing.T) {
	s := NewSettingsScreen(Theme{})

	out := s.Render(settings.Default(), 80, 0)
	if !strings.Contains(out, "Autosave interval") {
		t.Errorf("Render is missing the autosave-interval row:\n%s", out)
	}
	if !strings.Contains(out, "5 min") {
		t.Errorf("Render is missing the default interval value %q:\n%s", "5 min", out)
	}

	prefs := settings.Default()
	prefs.SetAutosaveIntervalMin(0)
	out = s.Render(prefs, 80, 0)
	if !strings.Contains(out, "off") {
		t.Errorf("Render with interval 0 is missing %q:\n%s", "off", out)
	}
}

// Clicking the autosave row cycles it, mirroring the chip rows'
// full-width click targets.
func TestSettingsClickAutosaveRow(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	const width = 80
	out := s.Render(settings.Default(), width, 0)
	lines := strings.Split(out, "\n")

	row := -1
	for i, ln := range lines {
		if strings.Contains(ln, "Autosave interval") {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatalf("could not locate the autosave-interval row in render")
	}
	if a, _ := s.HandleClick(0, row); a != SettingsActionCycleAutosave {
		t.Errorf("click autosave row = %v, want CycleAutosave", a)
	}
}

// Esc cancels; an unknown key is a no-op.
func TestSettingsHandleKeyCancelAndNoop(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	if a, _ := s.HandleKey("esc"); a != SettingsActionCancel {
		t.Errorf("esc = %v, want Cancel", a)
	}
	if a, _ := s.HandleKey("z"); a != SettingsActionNone {
		t.Errorf("unknown key = %v, want None", a)
	}
}

// Reset returns the cursor to the top so the screen always reopens on
// the first chip.
func TestSettingsReset(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	s.HandleKey("down")
	s.HandleKey("down")
	s.Reset()
	if _, c := s.HandleKey(" "); c != settings.AllChips[0] {
		t.Errorf("after Reset, toggle = %q, want first chip %q", c, settings.AllChips[0])
	}
}

// Clicking a chip row toggles that chip (and moves the cursor to it);
// clicking [Back] cancels. The row index is derived from the rendered
// layout so the hit-test tracks the real line positions.
func TestSettingsHandleClick(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	const width = 80
	out := s.Render(settings.Default(), width, 0)
	lines := strings.Split(out, "\n")

	// Find the rendered row of the third chip by its label, then click it.
	want := settings.AllChips[2]
	row := -1
	for i, ln := range lines {
		if strings.Contains(ln, want.Label()) {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatalf("could not locate chip %q in render", want)
	}
	if a, c := s.HandleClick(0, row); a != SettingsActionToggle || c != want {
		t.Errorf("click row %d = (%v,%q), want (Toggle,%q)", row, a, c, want)
	}

	// [Back] is the App's Title Row button now (B11); see
	// TestFormScreensWearTheSharedTitleRow in package tui.

	// A click in dead space (the divider row) is a no-op.
	if a, _ := s.HandleClick(0, 1); a != SettingsActionNone {
		t.Errorf("click on divider row = %v, want None", a)
	}
}

// The Empty readings row (ADR 0051 W6) renders the effective mode, Tidy
// by default, and a click on it cycles.
func TestSettingsEmptyReadingsRow(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	const width = 80
	out := s.Render(settings.Default(), width, 0)
	if !strings.Contains(out, "Empty readings: ‹Tidy›") {
		t.Errorf("default render missing Empty readings: ‹Tidy›:\n%s", out)
	}
	prefs := settings.Default()
	prefs.SetEmptyReadings(settings.EmptyCompact)
	out = s.Render(prefs, width, 0)
	if !strings.Contains(out, "Empty readings: ‹Compact›") {
		t.Errorf("Compact render missing its label:\n%s", out)
	}
	row := -1
	for i, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "Empty readings:") {
			row = i
		}
	}
	if a, _ := s.HandleClick(0, row); a != SettingsActionCycleEmptyReadings {
		t.Errorf("click Empty readings row = %v, want CycleEmptyReadings", a)
	}
}

// F6 of the #482 review: the window used to be capped at 12 body rows
// whatever the terminal height, so the Empty readings row (the last line)
// was only reachable by scrolling. At the 140x40 design size the whole
// body fits, so all of it must draw with no "more" markers.
func TestSettingsShowsEveryRowAtDesignSize(t *testing.T) {
	s := NewSettingsScreen(chipTestTheme())
	out := s.Render(settings.Default(), 140, 40)
	for _, want := range []string{"Empty readings", "Autosave interval", "Tutorial", "SOI pass"} {
		if !strings.Contains(out, want) {
			t.Errorf("140x40 settings screen is missing %q (window too small):\n%s", want, out)
		}
	}
	if strings.Contains(out, "more") {
		t.Errorf("140x40 settings screen still scrolls (a 'more' marker is drawn):\n%s", out)
	}
	if n := len(strings.Split(out, "\n")); n > 40 {
		t.Errorf("settings screen is %d lines, taller than the 40-row terminal", n)
	}
}

// A short terminal must still window: never taller than the terminal, and
// the cursor's row stays on screen.
func TestSettingsWindowsOnAShortTerminal(t *testing.T) {
	s := NewSettingsScreen(chipTestTheme())
	out := s.Render(settings.Default(), 140, 16)
	if n := len(strings.Split(out, "\n")); n > 16 {
		t.Errorf("settings screen is %d lines on a 16-row terminal", n)
	}
	if !strings.Contains(out, "> [x] Engine") {
		t.Errorf("cursor row not on screen on a short terminal:\n%s", out)
	}
}
