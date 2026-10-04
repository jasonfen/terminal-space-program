package screens

import (
	"fmt"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
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
	for i, br := range s.rowBtns {
		if br.Hit(col, row) {
			s.cursor = i
			return s.toggleAt(i)
		}
	}
	return SettingsActionNone, ""
}

// settingsBody flattens the screen's body — the chips / gameplay / saves
// sections, each a pinnable widgets.WindowLine header followed by its rows
// (blank lines and descriptions included, exactly as Render used to emit
// them inline) — into one windowed-list input. rowSelectable is
// index-aligned with the returned lines: rowSelectable[i] is the cursor
// index (into settings.AllChips + the gameplay/saves rows) that line i
// represents, or -1 for a header/blank/description line. cursorLine is the
// line index the current cursor (s.cursor) sits on.
func (s *SettingsScreen) settingsBody(prefs settings.Settings) (lines []widgets.WindowLine, rowSelectable []int, cursorLine int) {
	add := func(text string, isHeader bool, selectable int) {
		lines = append(lines, widgets.WindowLine{Text: text, IsHeader: isHeader})
		rowSelectable = append(rowSelectable, selectable)
		if selectable >= 0 && selectable == s.cursor {
			cursorLine = len(lines) - 1
		}
	}

	add(s.theme.Dim.Render("─── chips ───"), true, -1)
	add("", false, -1)
	add(s.theme.Dim.Render("  Default visibility of each orbit-screen chip."), false, -1)
	add("", false, -1)
	for i, c := range settings.AllChips {
		marker := "  "
		if i == s.cursor {
			marker = "> "
		}
		box := "[ ]"
		if prefs.ChipEnabled(c) {
			box = "[x]"
		}
		text := box + " " + c.Label()
		if i == s.cursor {
			text = s.theme.Primary.Render(text)
		}
		add(marker+text, false, i)
	}

	// Gameplay section: the two built-in mission programs (ADR 0025 §2 /
	// v0.21 Slice 7). Flight School (Tutorial) is on unless switched off
	// here (#425); the Challenge ladder stays opt-in.
	add("", false, -1)
	add(s.theme.Dim.Render("─── gameplay ───"), true, -1)
	add("", false, -1)
	add(s.theme.Dim.Render("  Flight School is on by default. Challenge ladder is opt-in."), false, -1)
	add("", false, -1)
	gameplay := []struct {
		label string
		on    bool
	}{
		{"Tutorial", prefs.TutorialOn()},
		{"Challenge ladder", prefs.ChallengesEnabled},
	}
	for j, g := range gameplay {
		idx := len(settings.AllChips) + j
		marker := "  "
		if idx == s.cursor {
			marker = "> "
		}
		box := "[ ]"
		if g.on {
			box = "[x]"
		}
		text := box + " " + g.label
		if idx == s.cursor {
			text = s.theme.Primary.Render(text)
		}
		add(marker+text, false, idx)
	}

	// Saves section: the periodic-autosave interval (v0.26 S4 / ADR 0033
	// §E). A value row rather than a checkbox — space/enter cycles it
	// through settings.AutosaveIntervalSteps; 0 renders as "off" (the
	// on-quit autosave still fires regardless).
	add("", false, -1)
	add(s.theme.Dim.Render("─── saves ───"), true, -1)
	add("", false, -1)
	add(s.theme.Dim.Render("  Periodic autosave into the rotating ring. Off keeps quit-autosave only."), false, -1)
	add("", false, -1)
	{
		idx := len(settings.AllChips) + gameplayRows
		marker := "  "
		if idx == s.cursor {
			marker = "> "
		}
		text := "Autosave interval: ‹" + autosaveIntervalLabel(prefs.AutosaveIntervalMinutes()) + "›"
		if idx == s.cursor {
			text = s.theme.Primary.Render(text)
		}
		add(marker+text, false, idx)
	}

	// Display section: Empty readings (ADR 0051 W6, #482), a value row
	// like autosave: space/enter cycles Full / Tidy / Compact.
	add("", false, -1)
	add(s.theme.Dim.Render("─── display ───"), true, -1)
	add("", false, -1)
	add(s.theme.Dim.Render("  Full: every row. Tidy: no empty TARGET. Compact: no trailing dash rows."), false, -1)
	add("", false, -1)
	{
		idx := len(settings.AllChips) + gameplayRows + savesRows
		marker := "  "
		if idx == s.cursor {
			marker = "> "
		}
		text := "Empty readings: ‹" + prefs.EmptyReadingsMode().Label() + "›"
		if idx == s.cursor {
			text = s.theme.Primary.Render(text)
		}
		add(marker+text, false, idx)
	}

	return lines, rowSelectable, cursorLine
}

// Render returns the settings screen for the given visibility state.
// width is the terminal width — used to right-align [Back] on row 0 the
// same way the menu / missions screens do, and to size the full-row click
// targets. height is the terminal height (#373 / ADR 0046): the body (the
// chips / gameplay / saves sections) windows itself around the cursor so
// the title, the active section's header, and the cursor stay on screen;
// height<=0 disables the windowing clamp (shows the whole body) — handy
// for tests and any caller with no height budget. The on/off box for each
// Chip reads prefs.ChipEnabled.
func (s *SettingsScreen) Render(prefs settings.Settings, width, height int) string {
	var lines []string

	body, rowSelectable, cursorLine := s.settingsBody(prefs)

	// blank(1) + footer(1) — every line Render emits outside the
	// windowed body.
	const fixedLines = 2
	budget := 0 // widgets.Window treats <=0 as "show everything"
	if height > 0 {
		// The window is sized from the terminal, not a fixed row cap: at
		// the 140x40 design size the whole body fits, so no row (the
		// display section's Empty readings, last of all) hides behind
		// scrolling. A shorter terminal still windows around the cursor.
		budget = height - fixedLines
		if len(body) > budget {
			// Windowing: widgets.Window adds up to 3 pin/"more" lines on
			// top of budget; reserve them so the markers survive the
			// safety net below and the player can see rows are hidden.
			budget -= 3
		}
		if budget < 1 {
			budget = 1
		}
	}
	rendered := widgets.Window(body, cursorLine, budget)
	// Safety net: widgets.Window's pinned header + "N more" markers can add
	// up to 3 lines on top of budget (documented on Window). Drop those
	// marker/pin lines first — cheapest to lose — before ever touching a
	// content row, so the cursor's own row is the last thing trimmed.
	// Mirrors spawn.go's Render.
	if height > 0 {
		if over := fixedLines + len(rendered) - height; over > 0 {
			trimmed := rendered[:0]
			dropped := 0
			for _, r := range rendered {
				if dropped < over && r.Kind != widgets.LineContent {
					dropped++
					continue
				}
				trimmed = append(trimmed, r)
			}
			rendered = trimmed
		}
	}

	// rowBtns for off-window rows stay unset (the zero buttonRange), so a
	// click can never land on a row that isn't drawn — mirrors saves.go's
	// windowed-list click-target contract.
	s.rowBtns = make([]buttonRange, len(settings.AllChips)+gameplayRows+savesRows+displayRows)
	for _, r := range rendered {
		if r.Kind == widgets.LineContent {
			if sel := rowSelectable[r.Index]; sel >= 0 {
				s.rowBtns[sel] = buttonRange{row: len(lines), colStart: 0, colEnd: width, set: true}
			}
		}
		lines = append(lines, r.Text)
	}

	lines = append(lines, "")
	lines = append(lines, s.theme.Footer.Render("[↑/↓] move  [space] toggle  [esc] back"))
	return strings.Join(lines, "\n")
}

// autosaveIntervalLabel renders an interval-in-minutes as the Settings
// row's value text: "off" for 0, "N min" otherwise.
func autosaveIntervalLabel(min int) string {
	if min <= 0 {
		return "off"
	}
	return fmt.Sprintf("%d min", min)
}
