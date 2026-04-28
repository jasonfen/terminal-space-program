package screens

import (
	"strings"
)

// Menu is the splash / pause menu surfaced when the player presses Esc
// on the orbit (home) screen. Three actions: save, load, quit
// (autosave). Replaces the v0.6.3 inline "Quit and save? [y/N]"
// footer prompt with a centered modal that doubles as a "what can I
// do from here" entry point. v0.7.3.3+.
type Menu struct {
	theme Theme

	// backColStart / backColEnd track the [Back] click-target column
	// range, recomputed on every Render so terminal-resize doesn't
	// stale the hit-test. v0.7.4+.
	backColStart, backColEnd int
}

func NewMenu(th Theme) *Menu { return &Menu{theme: th} }

// MenuAction enumerates the menu's outcomes. Returned by HandleKey
// so the caller (App.Update) can perform the actual save / load /
// quit dispatch — the screen stays decoupled from sim and save
// packages.
type MenuAction int

const (
	MenuActionNone   MenuAction = iota // unhandled key
	MenuActionCancel                   // esc — return to orbit
	MenuActionSave
	MenuActionLoad
	MenuActionQuit
)

// HandleKey maps a raw key string to a MenuAction. Called from
// App.Update when active == screenMenu. Lower- and upper-case both
// match (player's caps-lock state shouldn't matter).
func (Menu) HandleKey(s string) MenuAction {
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
	return MenuActionNone
}

// Render returns the centered menu modal. width is the terminal
// width — used to right-align the [Back] button on row 0.
func (m *Menu) Render(width int) string {
	const titleText = "terminal-space-program"
	const backLabel = "[Back]"
	var b strings.Builder
	pad := width - len([]rune(titleText)) - len([]rune(backLabel))
	if pad < 1 {
		pad = 1
	}
	m.backColStart = len([]rune(titleText)) + pad
	m.backColEnd = m.backColStart + len([]rune(backLabel))
	b.WriteString(m.theme.Title.Render(titleText))
	b.WriteString(strings.Repeat(" ", pad))
	b.WriteString(m.theme.Primary.Render(backLabel))
	b.WriteString("\n")
	b.WriteString(m.theme.Dim.Render("─── menu ───"))
	b.WriteString("\n\n")

	rows := [][2]string{
		{"s", "save game"},
		{"l", "load game"},
		{"q", "quit (autosave)"},
	}
	for _, r := range rows {
		b.WriteString("  ")
		b.WriteString(m.theme.Primary.Render("[" + r[0] + "]"))
		b.WriteString("  ")
		b.WriteString(r[1])
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Footer.Render("[esc] cancel — back to orbit"))
	return b.String()
}

// HitBackButton reports whether a click at (col, row) lands on the
// title-row [Back] button. v0.7.4+.
func (m *Menu) HitBackButton(col, row int) bool {
	return row == 0 && col >= m.backColStart && col < m.backColEnd
}
