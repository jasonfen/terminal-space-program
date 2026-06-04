package screens

import (
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/settings"
)

// SettingsScreen is the v0.13 slice-3 menu-reached screen that toggles
// each orbit-screen Chip's default visibility. It lists every Chip in
// settings.AllChips with an on/off box; toggling a row writes through to
// the shared settings.Settings (via the App, which then persists to
// settings.json immediately — there is no apply button). The slim HUD
// column's core telemetry is fixed by ADR 0010 and is deliberately absent
// here; only Chips are configurable.
//
// Like Menu, the screen is decoupled from the sim / config packages: it
// owns only the cursor + click-target ranges and emits a SettingsAction
// the App turns into a real toggle + persist. The current visibility
// state is read from the settings.Settings passed into Render, so the
// App's orbitView.Settings() stays the single source of truth.
type SettingsScreen struct {
	theme  Theme
	cursor int // index into settings.AllChips of the highlighted row

	// Click-target ranges, recomputed each Render so terminal-resize
	// can't stale the hit-tests. backBtn is the title-row [Back]; rowBtns
	// is index-aligned with settings.AllChips, each spanning its full row.
	backBtn buttonRange
	rowBtns []buttonRange
}

func NewSettingsScreen(th Theme) *SettingsScreen { return &SettingsScreen{theme: th} }

// Reset returns the cursor to the top. Called by the App when entering
// the screen so it always opens on the first Chip, not wherever the
// cursor last sat.
func (s *SettingsScreen) Reset() { s.cursor = 0 }

// SettingsAction enumerates the screen's outcomes. Returned by HandleKey
// and HandleClick so the App performs the actual toggle + persist — the
// screen stays decoupled from the settings package's Save.
type SettingsAction int

const (
	SettingsActionNone   SettingsAction = iota // unhandled key / click
	SettingsActionCancel                       // esc / [Back] — return to orbit
	SettingsActionToggle                       // flip the returned Chip's visibility
)

// HandleKey maps a raw key string to a SettingsAction. Up/down (and
// k/j) move the cursor with wrap-around; space / enter toggles the
// highlighted Chip; esc backs out to orbit. On a toggle the returned
// Chip is the highlighted one; for every other action the Chip is the
// zero value (callers switch on the action first).
func (s *SettingsScreen) HandleKey(key string) (SettingsAction, settings.Chip) {
	n := len(settings.AllChips)
	switch key {
	case "up", "k":
		if n > 0 {
			s.cursor = (s.cursor - 1 + n) % n
		}
	case "down", "j":
		if n > 0 {
			s.cursor = (s.cursor + 1) % n
		}
	case " ", "enter":
		if n > 0 {
			return SettingsActionToggle, settings.AllChips[s.cursor]
		}
	case "esc":
		return SettingsActionCancel, ""
	}
	return SettingsActionNone, ""
}

// HandleClick maps a (col, row) click to a SettingsAction. A click on
// the title-row [Back] cancels; a click anywhere on a Chip row moves the
// cursor there and toggles it (rows are full-width click targets so a
// thumb doesn't have to land on the box). Anything else is a no-op.
func (s *SettingsScreen) HandleClick(col, row int) (SettingsAction, settings.Chip) {
	if s.backBtn.Hit(col, row) {
		return SettingsActionCancel, ""
	}
	for i, br := range s.rowBtns {
		if br.Hit(col, row) {
			s.cursor = i
			return SettingsActionToggle, settings.AllChips[i]
		}
	}
	return SettingsActionNone, ""
}

// Render returns the settings screen for the given visibility state.
// width is the terminal width — used to right-align [Back] on row 0 the
// same way the menu / missions screens do, and to size the full-row
// click targets. The on/off box for each Chip reads prefs.ChipEnabled.
func (s *SettingsScreen) Render(prefs settings.Settings, width int) string {
	var lines []string

	// Row 0: title + right-aligned [Back] button.
	const titleText = "settings"
	const backLabel = "[Back]"
	pad := width - len([]rune(titleText)) - len([]rune(backLabel))
	if pad < 1 {
		pad = 1
	}
	backCol := len([]rune(titleText)) + pad
	s.backBtn = buttonRange{
		row:      0,
		colStart: backCol,
		colEnd:   backCol + len([]rune(backLabel)),
		set:      true,
	}
	lines = append(lines, s.theme.Title.Render(titleText)+
		strings.Repeat(" ", pad)+
		s.theme.Primary.Render(backLabel))

	lines = append(lines, s.theme.Dim.Render("─── chips ───"))
	lines = append(lines, "")
	lines = append(lines, s.theme.Dim.Render("  Default visibility of each orbit-screen chip."))
	lines = append(lines, "")

	// One row per Chip, in AllChips display order. Record each row as a
	// full-width click target (index-aligned with AllChips) before
	// appending it, so buttonRange.row matches the rendered line index.
	s.rowBtns = make([]buttonRange, len(settings.AllChips))
	for i, c := range settings.AllChips {
		marker := "  "
		if i == s.cursor {
			marker = "> "
		}
		box := "[ ]"
		if prefs.ChipEnabled(c) {
			box = "[x]"
		}
		s.rowBtns[i] = buttonRange{row: len(lines), colStart: 0, colEnd: width, set: true}

		text := box + " " + c.Label()
		if i == s.cursor {
			text = s.theme.Primary.Render(text)
		}
		lines = append(lines, marker+text)
	}

	lines = append(lines, "")
	lines = append(lines, s.theme.Footer.Render("[↑/↓] move  [space] toggle  [esc] back"))
	return strings.Join(lines, "\n")
}

// HitBackButton reports whether a click at (col, row) lands on the
// title-row [Back] button — mirrors Missions.HitBackButton so the App's
// mouse cascade can treat "left the screen" uniformly across screens.
func (s *SettingsScreen) HitBackButton(col, row int) bool {
	return s.backBtn.Hit(col, row)
}
