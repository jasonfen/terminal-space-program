package screens

import (
	"strings"
)

// Menu is the splash / pause menu surfaced when the player presses Esc
// on the orbit (home) screen. Three actions: save, load, quit
// (autosave). Replaces the v0.6.3 inline "Quit and save? [y/N]"
// footer prompt with a centered modal that doubles as a "what can I
// do from here" entry point. v0.7.3.3+.
//
// v0.7.4+ added clickable controls; v0.26 (ADR 0033 §F) rehomed
// Save / Load onto the Saves screen, which owns its own destructive
// confirms (§H). #474 removed Quit's own click/keyboard confirm
// sub-state (menuModeConfirmQuit, [Yes]/[No]): the row now fires
// MenuActionQuit directly, same as every other row, and the caller
// (App.applyMenuAction) arms the single app-level quit prompt that
// ctrl+c also raises — one question, one wording, wherever the player
// leaves from.
type Menu struct {
	theme Theme

	// cursor is the highlighted row (ADR 0052 decision 6): up/down move it,
	// enter fires it. Indexes menuRows. Reset returns it to the first row.
	cursor int

	// Click-target ranges, recomputed each Render so terminal-resize
	// doesn't stale the hit-tests. Each is (row, colStart, colEnd).
	saveBtn     buttonRange
	loadBtn     buttonRange
	vabBtn      buttonRange
	settingsBtn buttonRange
	controlsBtn buttonRange
	helpBtn     buttonRange // #425: "Help (F1)" row, opens the F1 overlay
	quitBtn     buttonRange
}

// buttonRange records a clickable label's row + column span. set=false
// means the button isn't rendered in the current mode (Hit returns
// false unconditionally).
type buttonRange struct {
	row, colStart, colEnd int
	set                   bool
}

func (br buttonRange) Hit(col, row int) bool {
	return br.set && row == br.row && col >= br.colStart && col < br.colEnd
}

func NewMenu(th Theme) *Menu { return &Menu{theme: th} }

// Reset clears every button's click-target visibility. Called by the
// App when transitioning into screenMenu so a stale hit-test from the
// previous time the menu was open can't linger.
func (m *Menu) Reset() {
	m.cursor = 0
	m.saveBtn.set = false
	m.loadBtn.set = false
	m.vabBtn.set = false
	m.settingsBtn.set = false
	m.controlsBtn.set = false
	m.helpBtn.set = false
	m.quitBtn.set = false
}

// MenuAction enumerates the menu's outcomes. Returned by HandleKey
// and HandleClick so the caller (App.Update) can perform the actual
// save / load / quit dispatch — the screen stays decoupled from sim
// and save packages.
type MenuAction int

const (
	MenuActionNone   MenuAction = iota // unhandled key / click
	MenuActionCancel                   // esc / [Back] — return to orbit
	MenuActionSave
	MenuActionLoad
	MenuActionVAB      // v0.24 / ADR 0029: open the Vehicle Assembly (VAB) screen
	MenuActionSettings // v0.13: open the per-Chip visibility screen
	MenuActionControls // ADR 0022: open the keyboard-layout screen (labelled "[Keyboard layout]" on the row, #425)
	MenuActionHelp     // #425: open the F1 help overlay — a real pointer, not just the Controls row's parenthetical
	MenuActionQuit
)

// menuRow is one pause-menu row: its shortcut letter, label and action.
type menuRow struct {
	key, label string
	action     MenuAction
}

// menuRows is the row order, top to bottom. The letters are shortcuts that
// sit beside each row; up/down + enter reach the same actions (ADR 0052
// decision 6). Keyboard layout moved from c to k so no letter is a
// near-miss for the flight keys around it.
var menuRows = []menuRow{
	{"s", "[Save Game]", MenuActionSave},
	{"l", "[Load Game]", MenuActionLoad},
	{"b", "[Build (VAB)]", MenuActionVAB},
	{"t", "[Settings]", MenuActionSettings},
	// #425: renamed from "[Controls]": that label read like the keybinding
	// list, but it's only the QWERTY/QWERTZ picker.
	{"k", "[Keyboard layout]", MenuActionControls},
	// #425: a real pointer to the F1 overlay, the actual keybinding list.
	{"h", "[Help (F1)]", MenuActionHelp},
	{"q", "[Quit]", MenuActionQuit},
}

// HandleKey maps a raw key string to a MenuAction. Lower- and
// upper-case both match. Every row — including Quit — fires its action
// directly; #474 moved quit's confirm off this screen entirely and
// onto the single app-level quit prompt (App.applyMenuAction arms it
// via a.quitConfirm), the same one ctrl+c raises, so there's one
// question and one wording wherever the player leaves from.
func (m *Menu) HandleKey(s string) MenuAction {
	switch s {
	case "esc":
		return MenuActionCancel
	case "up":
		m.cursor = (m.cursor - 1 + len(menuRows)) % len(menuRows)
		return MenuActionNone
	case "down":
		m.cursor = (m.cursor + 1) % len(menuRows)
		return MenuActionNone
	case "enter":
		return menuRows[m.cursor].action
	}
	for _, r := range menuRows {
		if s == r.key || s == strings.ToUpper(r.key) {
			return r.action
		}
	}
	return MenuActionNone
}

// HandleClick maps a (col, row) click to a MenuAction. Every row fires
// its action directly on click — #474 removed Quit's mouse-only
// [Yes]/[No] confirm sub-state along with the rest of the menu's own
// confirm machinery; the click-confirm gate save/load/quit go through
// now lives on the single app-level quit prompt instead.
func (m *Menu) HandleClick(col, row int) MenuAction {
	switch {
	case m.vabBtn.Hit(col, row):
		return MenuActionVAB
	case m.settingsBtn.Hit(col, row):
		return MenuActionSettings
	case m.controlsBtn.Hit(col, row):
		return MenuActionControls
	case m.helpBtn.Hit(col, row):
		return MenuActionHelp
	case m.saveBtn.Hit(col, row):
		// v0.26 (ADR 0033 §F): Save / Load open the Saves screen, which
		// owns its own destructive confirms (§H).
		return MenuActionSave
	case m.loadBtn.Hit(col, row):
		return MenuActionLoad
	case m.quitBtn.Hit(col, row):
		return MenuActionQuit
	}
	return MenuActionNone
}

// Render returns the menu screen for the current mode. width is the
// terminal width — used to right-align the [Back] button on row 0
// the same way the orbit-screen title bar does.
func (m *Menu) Render(width int) string {
	var lines []string

	// rowOffset is the count of rows already in `lines` (the title row).
	// renderList records buttonRange.row in absolute terms, so it needs
	// to know how many rows precede its output.
	rowOffset := len(lines)
	lines = append(lines, m.renderList(rowOffset)...)

	return strings.Join(lines, "\n")
}

// renderList composes the action-list mode body (everything below
// the title row). Records the row + column span of each clickable
// button so HandleClick can hit-test them. rowOffset is the number
// of rows already rendered above this output (currently the title
// row) so button rows are stored in absolute terms.
func (m *Menu) renderList(rowOffset int) []string {
	var lines []string
	lines = append(lines, m.theme.Dim.Render("─── menu ───"))
	lines = append(lines, "")

	btns := []*buttonRange{&m.saveBtn, &m.loadBtn, &m.vabBtn, &m.settingsBtn,
		&m.controlsBtn, &m.helpBtn, &m.quitBtn}
	rows := menuRows
	for i, r := range rows {
		// The highlighted row carries a ▸ in the indent; the label column
		// does not move, so click ranges are identical for every row.
		indent := "  "
		if i == m.cursor {
			indent = m.theme.Primary.Render("▸") + " "
		}
		colStart := 2
		colEnd := colStart + len([]rune(r.label))
		*btns[i] = buttonRange{
			row:      rowOffset + len(lines),
			colStart: colStart,
			colEnd:   colEnd,
			set:      true,
		}
		shortcut := m.theme.Dim.Render("  (" + r.key + ")")
		lines = append(lines, indent+m.theme.Primary.Render(r.label)+shortcut)
		// Blank row between buttons so they're easier to click
		// individually — the v0.7.4 list was tight enough that
		// adjacent rows blurred under thumbs / trackpads.
		if i < len(rows)-1 {
			lines = append(lines, "")
		}
	}

	lines = append(lines, "")
	lines = append(lines, m.theme.Footer.Render("[↑/↓] pick · [enter] open · [esc] back to orbit"))
	return lines
}
