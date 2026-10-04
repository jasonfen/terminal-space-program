package screens

import (
	"fmt"


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
	// can't stale the hit-tests. rowBtns is index-aligned with settings.AllChips, each spanning its full row.
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
	SettingsActionNone               SettingsAction = iota // unhandled key / click
	SettingsActionCancel                                   // esc / [Back] — return to orbit
	SettingsActionToggle                                   // flip the returned Chip's visibility
	SettingsActionToggleTutorial                           // flip the tutorial mission program (Slice 7)
	SettingsActionToggleChallenges                         // flip the challenge mission program (Slice 7)
	SettingsActionCycleAutosave                            // cycle the autosave interval (v0.26 S4 / ADR 0033 §E)
	SettingsActionCycleEmptyReadings                       // cycle Full / Tidy / Compact (ADR 0051 W6, #482)
)

// gameplayRows is the number of non-chip toggle rows (Tutorial, Challenges)
// rendered below the chips (v0.21 Slice 7). They occupy cursor indices
// [len(AllChips), len(AllChips)+gameplayRows).
const gameplayRows = 2

// savesRows is the number of saves-section rows below the gameplay
// toggles — currently just the autosave-interval cycler (v0.26 S4). It
// occupies cursor index len(AllChips)+gameplayRows.
const savesRows = 1

// displayRows is the display-section row count below saves: the Empty
// readings cycler (ADR 0051 W6). It occupies cursor index
// len(AllChips)+gameplayRows+savesRows.
const displayRows = 1

// HandleKey maps a raw key string to a SettingsAction. Up/down (and
// k/j) move the cursor with wrap-around; space / enter toggles the
// highlighted Chip; esc backs out to orbit. On a toggle the returned
// Chip is the highlighted one; for every other action the Chip is the
// zero value (callers switch on the action first).
func (s *SettingsScreen) HandleKey(key string) (SettingsAction, settings.Chip) {
	n := len(settings.AllChips) + gameplayRows + savesRows + displayRows
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
		return s.toggleAt(s.cursor)
	case "esc":
		return SettingsActionCancel, ""
	}
	return SettingsActionNone, ""
}

// toggleAt maps a row index to its toggle action: the first len(AllChips) rows
// are chip toggles, the two rows below are the Tutorial and Challenges program
// toggles (v0.21 Slice 7), and the last row cycles the autosave interval
// (v0.26 S4).
func (s *SettingsScreen) toggleAt(i int) (SettingsAction, settings.Chip) {
	switch i {
	case len(settings.AllChips):
		return SettingsActionToggleTutorial, ""
	case len(settings.AllChips) + 1:
		return SettingsActionToggleChallenges, ""
	case len(settings.AllChips) + gameplayRows:
		return SettingsActionCycleAutosave, ""
	case len(settings.AllChips) + gameplayRows + savesRows:
		return SettingsActionCycleEmptyReadings, ""
	default:
		if i >= 0 && i < len(settings.AllChips) {
			return SettingsActionToggle, settings.AllChips[i]
		}
	}
	return SettingsActionNone, ""
}

// HandleClick maps a (col, row) click to a SettingsAction. A click on
// the title-row [Back] cancels; a click anywhere on a Chip row moves the
// cursor there and toggles it (rows are full-width click targets so a
// thumb doesn't have to land on the box). Anything else is a no-op.
func (s *SettingsScreen) HandleClick(col, row int) (SettingsAction, settings.Chip) {
	col, row = col-frameInset, row-frameInset // frame-relative -> body
	for i, br := range s.rowBtns {
		if br.Hit(col, row) {
			s.cursor = i
			return s.toggleAt(i)
		}
	}
	return SettingsActionNone, ""
}

// settingsLegend is the key legend on the frame's bottom edge.
const settingsLegend = "[↑/↓] move · [space] toggle · [esc] back"

// cursorMark is the one list cursor (B11 / G9 Q5): ▸ on the row the cursor
// is on, two blanks on every other row.
func (s *SettingsScreen) cursorMark(on bool) string {
	if on {
		return s.theme.Primary.Render("▸") + " "
	}
	return "  "
}

// Render returns the settings screen inside the shared form frame (B11 /
// G9 Q4): CHIPS on the left, GAMEPLAY / SAVES / DISPLAY stacked on the
// right, each a titled box, the legend on the bottom edge. width x height
// is the whole framed block (the App's Title Row sits above it). The on/off
// box for each Chip reads prefs.ChipEnabled. Click ranges are recorded in
// body coordinates (the frame's border is frameInset on each side).
func (s *SettingsScreen) Render(prefs settings.Settings, width, height int) string {
	inner := width - 2*frameInset
	lw := clampI(inner*45/100, 30, 62)
	rw := inner - lw - 1
	s.rowBtns = make([]buttonRange, len(settings.AllChips)+gameplayRows+savesRows+displayRows)

	// row builds one selectable row and records its click range: bodyRow is
	// the row's absolute body row, [colStart, colEnd) the box it sits in.
	row := func(idx, bodyRow, colStart, colEnd int, text string) string {
		s.rowBtns[idx] = buttonRange{row: bodyRow, colStart: colStart, colEnd: colEnd, set: true}
		if idx == s.cursor {
			text = s.theme.Primary.Render(text)
		}
		return s.cursorMark(idx == s.cursor) + text
	}
	checkbox := func(on bool, label string) string {
		if on {
			return "[x] " + label
		}
		return "[ ] " + label
	}
	// desc word-wraps a box's description to the box (it used to truncate
	// with "…", cutting SAVES and DISPLAY off mid-word at 140x40). Rows
	// below it are placed by len(lines), so click targets follow the wrap.
	desc := func(text string, w int) []string {
		var out []string
		for _, ln := range wrapText(text, w-6) {
			out = append(out, "  "+s.theme.Dim.Render(ln))
		}
		return out
	}

	// CHIPS (left). Lines start at body row 2 (top edge, title).
	var chips []string
	chips = append(append(chips, desc("Default visibility of each orbit-screen chip.", lw)...), "")
	// When the chip list outgrows the box (a short terminal, or more
	// Chips than the floor holds), window it around the cursor with
	// "more" markers instead of letting formFrame clip it silently
	// (review #555 L3). height<=0 means no budget: show them all.
	first, last := 0, len(settings.AllChips)
	if room := height - 2 - 3 - len(chips); height > 0 && len(settings.AllChips) > room {
		vis := room - 2 // a marker row above and below
		if vis < 1 {
			vis = 1
		}
		first = 0
		if s.cursor < len(settings.AllChips) {
			first = clampI(s.cursor-vis/2, 0, len(settings.AllChips)-vis)
		}
		last = first + vis
		marker := func(on bool, glyph string, n int) string {
			if !on {
				return ""
			}
			return "  " + s.theme.Dim.Render(fmt.Sprintf("%s %d more", glyph, n))
		}
		chips = append(chips, marker(first > 0, "▲", first))
		for i := first; i < last; i++ {
			c := settings.AllChips[i]
			chips = append(chips, row(i, 2+len(chips), 0, lw, checkbox(prefs.ChipEnabled(c), c.Label())))
		}
		chips = append(chips, marker(last < len(settings.AllChips), "▼", len(settings.AllChips)-last))
	} else {
		for i, c := range settings.AllChips {
			chips = append(chips, row(i, 2+len(chips), 0, lw, checkbox(prefs.ChipEnabled(c), c.Label())))
		}
	}
	left := formBox(s.theme, "CHIPS", chips, lw)

	// Right column, stacked. y tracks the next box's top body row.
	var right []string
	y := 0
	stack := func(title string, lines []string) {
		box := formBox(s.theme, title, lines, rw)
		right = append(right, box...)
		y += len(box)
	}
	// GAMEPLAY: the two built-in mission programs (ADR 0025 §2 / v0.21
	// Slice 7). Flight School (Tutorial) is on unless switched off here
	// (#425); the Challenge ladder stays opt-in.
	{
		var ls []string
		ls = append(append(ls, desc("Flight School is on by default. Challenge ladder is opt-in.", rw)...), "")
		ls = append(ls, row(len(settings.AllChips), y+2+len(ls), lw+1, lw+1+rw, checkbox(prefs.TutorialOn(), "Tutorial")))
		ls = append(ls, row(len(settings.AllChips)+1, y+2+len(ls), lw+1, lw+1+rw, checkbox(prefs.ChallengesEnabled, "Challenge ladder")))
		stack("GAMEPLAY", ls)
	}
	// SAVES: the periodic-autosave interval (v0.26 S4 / ADR 0033 §E). A
	// value row rather than a checkbox: space/enter cycles it through
	// settings.AutosaveIntervalSteps; 0 renders as "off" (the on-quit
	// autosave still fires regardless).
	{
		var ls []string
		ls = append(append(ls, desc("Periodic autosave into the rotating ring. Off keeps quit-autosave only.", rw)...), "")
		ls = append(ls, row(len(settings.AllChips)+gameplayRows, y+2+len(ls), lw+1, lw+1+rw,
			"Autosave interval: ‹ "+autosaveIntervalLabel(prefs.AutosaveIntervalMinutes())+" ›"))
		stack("SAVES", ls)
	}
	// DISPLAY: Empty readings (ADR 0051 W6, #482), a value row like
	// autosave: space/enter cycles Full / Tidy / Compact.
	{
		var ls []string
		ls = append(append(ls, desc("Full: every row. Tidy: no empty TARGET. Compact: no trailing dash rows.", rw)...), "")
		ls = append(ls, row(len(settings.AllChips)+gameplayRows+savesRows, y+2+len(ls), lw+1, lw+1+rw,
			"Empty readings: ‹ "+prefs.EmptyReadingsMode().Label()+" ›"))
		stack("DISPLAY", ls)
	}

	return formFrame(s.theme, joinBoxes(left, right, lw, 1), width, height, s.theme.Footer.Render(settingsLegend))
}

// autosaveIntervalLabel renders an interval-in-minutes as the Settings
// row's value text: "off" for 0, "N min" otherwise.
func autosaveIntervalLabel(min int) string {
	if min <= 0 {
		return "off"
	}
	return fmt.Sprintf("%d min", min)
}
