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
// v0.7.4+ adds clickable controls. The list state shows three labelled
// buttons; clicking any transitions into a confirm sub-state with
// [Yes] / [No] buttons. Yes triggers the corresponding MenuAction;
// No returns to the list. The keyboard path keeps the legacy direct
// flow (s/l/q execute immediately) so muscle memory still works —
// the confirm gate is for the mouse path only.
type Menu struct {
	theme Theme
	mode  menuMode

	// Click-target ranges, recomputed each Render so terminal-resize
	// doesn't stale the hit-tests. Each is (row, colStart, colEnd).
	backBtn buttonRange
	saveBtn buttonRange
	loadBtn buttonRange
	quitBtn buttonRange
	yesBtn  buttonRange
	noBtn   buttonRange
}

// menuMode tracks which sub-screen the menu is showing.
type menuMode int

const (
	menuModeList menuMode = iota
	menuModeConfirmSave
	menuModeConfirmLoad
	menuModeConfirmQuit
)

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

// Reset returns the menu to its top-level list state. Called by the
// App when transitioning into screenMenu so the screen always opens
// in the action-list view, not whatever confirm state was last
// dismissed.
func (m *Menu) Reset() {
	m.mode = menuModeList
	m.saveBtn.set = false
	m.loadBtn.set = false
	m.quitBtn.set = false
	m.yesBtn.set = false
	m.noBtn.set = false
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
	MenuActionQuit
)

// HandleKey maps a raw key string to a MenuAction. Lower- and
// upper-case both match. The keyboard path skips the click-only
// confirm gate when in the list state — typing "s" still saves
// immediately, matching v0.7.3.3 muscle memory. In a confirm state
// the keys narrow to y / n / enter / esc.
func (m *Menu) HandleKey(s string) MenuAction {
	switch m.mode {
	case menuModeList:
		switch s {
		case "s", "S":
			return MenuActionSave
		case "l", "L":
			return MenuActionLoad
		case "q", "Q":
			return MenuActionQuit
		case "esc":
			return MenuActionCancel
		}
	default:
		switch s {
		case "y", "Y", "enter":
			action := m.confirmAction()
			m.mode = menuModeList
			return action
		case "n", "N":
			m.mode = menuModeList
			return MenuActionNone
		case "esc":
			// Esc from a confirm step backs out to the list rather
			// than escaping all the way to orbit — gives a way to
			// undo a misclick on Save / Load / Quit.
			m.mode = menuModeList
			return MenuActionNone
		}
	}
	return MenuActionNone
}

// HandleClick maps a (col, row) click to a MenuAction. List-state
// clicks on Save / Load / Quit transition into the corresponding
// confirm sub-state and return MenuActionNone (the action only fires
// when the player confirms with Yes). Confirm-state Yes returns the
// stored action; No returns to the list.
func (m *Menu) HandleClick(col, row int) MenuAction {
	if m.backBtn.Hit(col, row) {
		m.mode = menuModeList
		return MenuActionCancel
	}
	switch m.mode {
	case menuModeList:
		switch {
		case m.saveBtn.Hit(col, row):
			m.mode = menuModeConfirmSave
		case m.loadBtn.Hit(col, row):
			m.mode = menuModeConfirmLoad
		case m.quitBtn.Hit(col, row):
			m.mode = menuModeConfirmQuit
		}
	default:
		switch {
		case m.yesBtn.Hit(col, row):
			action := m.confirmAction()
			m.mode = menuModeList
			return action
		case m.noBtn.Hit(col, row):
			m.mode = menuModeList
		}
	}
	return MenuActionNone
}

// confirmAction returns the MenuAction matching the current confirm
// sub-state. Used by both the keyboard "y" / enter path and the
// mouse [Yes] click path.
func (m *Menu) confirmAction() MenuAction {
	switch m.mode {
	case menuModeConfirmSave:
		return MenuActionSave
	case menuModeConfirmLoad:
		return MenuActionLoad
	case menuModeConfirmQuit:
		return MenuActionQuit
	}
	return MenuActionNone
}

// Render returns the menu screen for the current mode. width is the
// terminal width — used to right-align the [Back] button on row 0
// the same way the orbit-screen title bar does.
func (m *Menu) Render(width int) string {
	var lines []string

	// Row 0: title + right-aligned [Back] button.
	const titleText = "terminal-space-program"
	const backLabel = "[Back]"
	pad := width - len([]rune(titleText)) - len([]rune(backLabel))
	if pad < 1 {
		pad = 1
	}
	backCol := len([]rune(titleText)) + pad
	m.backBtn = buttonRange{
		row:      0,
		colStart: backCol,
		colEnd:   backCol + len([]rune(backLabel)),
		set:      true,
	}
	lines = append(lines, m.theme.Title.Render(titleText)+
		strings.Repeat(" ", pad)+
		m.theme.Primary.Render(backLabel))

	// Reset confirm-button visibility — only the active mode sets them.
	m.saveBtn.set = false
	m.loadBtn.set = false
	m.quitBtn.set = false
	m.yesBtn.set = false
	m.noBtn.set = false

	// rowOffset is the count of rows already in `lines` (the title row).
	// The mode-specific helpers record buttonRange.row in absolute
	// terms, so they need to know how many rows precede their output.
	rowOffset := len(lines)
	switch m.mode {
	case menuModeList:
		lines = append(lines, m.renderList(rowOffset)...)
	case menuModeConfirmSave:
		lines = append(lines, m.renderConfirm(rowOffset, "Save current game to disk?", "save")...)
	case menuModeConfirmLoad:
		lines = append(lines, m.renderConfirm(rowOffset, "Load saved game and discard current state?", "load")...)
	case menuModeConfirmQuit:
		lines = append(lines, m.renderConfirm(rowOffset, "Quit (autosaves on exit)?", "quit")...)
	}

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

	type entry struct {
		key, label string
		btn        *buttonRange
	}
	rows := []entry{
		{"s", "[Save Game]", &m.saveBtn},
		{"l", "[Load Game]", &m.loadBtn},
		{"q", "[Quit]", &m.quitBtn},
	}
	for _, r := range rows {
		const indent = "  "
		colStart := len([]rune(indent))
		colEnd := colStart + len([]rune(r.label))
		*r.btn = buttonRange{
			row:      rowOffset + len(lines),
			colStart: colStart,
			colEnd:   colEnd,
			set:      true,
		}
		shortcut := m.theme.Dim.Render("  (" + r.key + ")")
		lines = append(lines, indent+m.theme.Primary.Render(r.label)+shortcut)
	}

	lines = append(lines, "")
	lines = append(lines, m.theme.Footer.Render("[esc] back to orbit · keyboard: s/l/q"))
	return lines
}

// renderConfirm composes a confirm sub-screen body asking the player
// to confirm the given action. label is the lowercase verb (save /
// load / quit) shown in the divider header. Records [Yes] and [No]
// click-target ranges in absolute coordinates given rowOffset.
func (m *Menu) renderConfirm(rowOffset int, prompt, label string) []string {
	var lines []string
	lines = append(lines, m.theme.Dim.Render("─── confirm "+label+" ───"))
	lines = append(lines, "")
	lines = append(lines, "  "+prompt)
	lines = append(lines, "")

	const indent = "  "
	const yesLabel = "[Yes]"
	const noLabel = "[No]"
	const gap = "   "
	yesCol := len([]rune(indent))
	noCol := yesCol + len([]rune(yesLabel)) + len([]rune(gap))
	row := rowOffset + len(lines)
	m.yesBtn = buttonRange{row: row, colStart: yesCol, colEnd: yesCol + len([]rune(yesLabel)), set: true}
	m.noBtn = buttonRange{row: row, colStart: noCol, colEnd: noCol + len([]rune(noLabel)), set: true}
	lines = append(lines,
		indent+m.theme.Primary.Render(yesLabel)+gap+m.theme.Primary.Render(noLabel))

	lines = append(lines, "")
	lines = append(lines, m.theme.Footer.Render("[y]es / [n]o / [esc] cancel"))
	return lines
}

// HitBackButton reports whether a click at (col, row) lands on the
// title-row [Back] button. Kept as a public method for App.Update's
// mouse cascade — though HandleClick also handles [Back], having a
// dedicated check lets the dispatcher differentiate "left the menu"
// vs "clicked something inside it." v0.7.4+.
func (m *Menu) HitBackButton(col, row int) bool {
	return m.backBtn.Hit(col, row)
}
