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
	out := s.Render(settings.Default(), 80)
	for _, c := range settings.AllChips {
		if !strings.Contains(out, c.Label()) {
			t.Errorf("Render is missing chip %q (label %q)", c, c.Label())
		}
	}
	// Default: every chip on (checked); the two gameplay toggles default off.
	if got := strings.Count(out, "[x]"); got != len(settings.AllChips) {
		t.Errorf("checked-box count = %d, want %d (all chips on, gameplay off)", got, len(settings.AllChips))
	}
	if got := strings.Count(out, "[ ]"); got != gameplayRows {
		t.Errorf("empty-box count = %d, want %d (off-by-default gameplay toggles)", got, gameplayRows)
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
	out := s.Render(prefs, 80)
	// One disabled chip + the two off-by-default gameplay toggles = 3 empty.
	if got, want := strings.Count(out, "[ ]"), 1+gameplayRows; got != want {
		t.Errorf("empty-box count = %d, want %d (disabled chip + off gameplay toggles):\n%s", got, want, out)
	}
	if got, want := strings.Count(out, "[x]"), len(settings.AllChips)-1; got != want {
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

	// up wraps from row 0 to the last row — now the autosave-interval row
	// (v0.26 S4).
	s.HandleKey("up")
	if a, _ := s.HandleKey(" "); a != SettingsActionCycleAutosave {
		t.Errorf("up-wrap toggle action = %v, want CycleAutosave (last row)", a)
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

	out := s.Render(settings.Default(), 80)
	if !strings.Contains(out, "Autosave interval") {
		t.Errorf("Render is missing the autosave-interval row:\n%s", out)
	}
	if !strings.Contains(out, "5 min") {
		t.Errorf("Render is missing the default interval value %q:\n%s", "5 min", out)
	}

	prefs := settings.Default()
	prefs.SetAutosaveIntervalMin(0)
	out = s.Render(prefs, 80)
	if !strings.Contains(out, "off") {
		t.Errorf("Render with interval 0 is missing %q:\n%s", "off", out)
	}
}

// Clicking the autosave row cycles it, mirroring the chip rows'
// full-width click targets.
func TestSettingsClickAutosaveRow(t *testing.T) {
	s := NewSettingsScreen(Theme{})
	const width = 80
	out := s.Render(settings.Default(), width)
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
	out := s.Render(settings.Default(), width)
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

	// [Back] on row 0 cancels.
	if a, _ := s.HandleClick(width-3, 0); a != SettingsActionCancel {
		t.Errorf("click [Back] = %v, want Cancel", a)
	}

	// A click in dead space (the divider row) is a no-op.
	if a, _ := s.HandleClick(0, 1); a != SettingsActionNone {
		t.Errorf("click on divider row = %v, want None", a)
	}
}
