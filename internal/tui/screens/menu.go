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

	// Click-target ranges, recomputed each Render so terminal-resize
	// doesn't stale the hit-tests. Each is (row, colStart, colEnd).
	backBtn     buttonRange
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

// HandleKey maps a raw key string to a MenuAction. Lower- and
// upper-case both match. Every row — including Quit — fires its action
// directly; #474 moved quit's confirm off this screen entirely and
// onto the single app-level quit prompt (App.applyMenuAction arms it
// via a.quitConfirm), the same one ctrl+c raises, so there's one
// question and one wording wherever the player leaves from.
func (m *Menu) HandleKey(s string) MenuAction {
	switch s {
	case "s", "S":
		return MenuActionSave
	case "l", "L":
		return MenuActionLoad
	case "b", "B":
		return MenuActionVAB
	case "t", "T":
		return MenuActionSettings
	case "c", "C":
		return MenuActionControls
	case "h", "H":
		return MenuActionHelp
	case "q", "Q":
		return MenuActionQuit
	case "esc":
		return MenuActionCancel
	}
	return MenuActionNone
}

// HandleClick maps a (col, row) click to a MenuAction. Every row fires
// its action directly on click — #474 removed Quit's mouse-only
// [Yes]/[No] confirm sub-state along with the rest of the menu's own
// confirm machinery; the click-confirm gate save/load/quit go through
// now lives on the single app-level quit prompt instead.
func (m *Menu) HandleClick(col, row int) MenuAction {
	if m.backBtn.Hit(col, row) {
		return MenuActionCancel
	}
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

	type entry struct {
		key, label string
		btn        *buttonRange
	}
	rows := []entry{
		{"s", "[Save Game]", &m.saveBtn},
		{"l", "[Load Game]", &m.loadBtn},
		{"b", "[Build (VAB)]", &m.vabBtn},
		{"t", "[Settings]", &m.settingsBtn},
		// #425: renamed from "[Controls]" — that label read like the
		// keybinding list, but it's only the QWERTY/QWERTZ picker.
		{"c", "[Keyboard layout]", &m.controlsBtn},
		// #425: a real pointer to the F1 overlay, the actual keybinding
		// list — previously the menu's only mention of it was a
		// parenthetical inside the Controls screen's own prose.
		{"h", "[Help (F1)]", &m.helpBtn},
		{"q", "[Quit]", &m.quitBtn},
	}
	for i, r := range rows {
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
		// Blank row between buttons so they're easier to click
		// individually — the v0.7.4 list was tight enough that
		// adjacent rows blurred under thumbs / trackpads.
		if i < len(rows)-1 {
			lines = append(lines, "")
		}
	}

	lines = append(lines, "")
	lines = append(lines, m.theme.Footer.Render("[esc] back to orbit · keyboard: s/l/b/t/c/h/q"))
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
