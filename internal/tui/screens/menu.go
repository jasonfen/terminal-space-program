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

// Render returns the centered menu modal.
func (m *Menu) Render() string {
	title := m.theme.Title.Render("terminal-space-program")
	var b strings.Builder
	b.WriteString(title)
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
