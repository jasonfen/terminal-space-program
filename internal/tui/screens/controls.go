package screens

import (
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
)

// ControlsScreen is the menu-reached input-settings screen (ADR 0022).
// Slice 1 holds a single row — the keyboard-layout selector — cycling
// QWERTY ↔ QWERTZ. It is the designated home for a future full per-action
// remap, hence its own screen rather than a row on the chip-toggle Settings
// screen.
//
// Like Settings and Menu, the screen is decoupled from the sim / settings
// persistence: it owns only the cursor + click ranges and emits a
// ControlsAction the App turns into a real layout change + persist. The
// active layout is passed into Render, so the App's settings stay the single
// source of truth.
type ControlsScreen struct {
	theme  Theme
	cursor int // index into rows; only the layout row exists in slice 1

	backBtn   buttonRange
	layoutBtn buttonRange
}

func NewControlsScreen(th Theme) *ControlsScreen { return &ControlsScreen{theme: th} }

// Reset returns the cursor to the top. Called by the App on entry.
func (c *ControlsScreen) Reset() { c.cursor = 0 }

// ControlsAction enumerates the screen's outcomes. The App performs the
// actual cycle + persist so the screen stays decoupled from settings.Save.
type ControlsAction int

const (
	ControlsActionNone        ControlsAction = iota // unhandled key / click
	ControlsActionCancel                            // esc / [Back] — return to orbit
	ControlsActionCycleLayout                       // advance keyboard layout to the next
)

// HandleKey maps a raw key string to a ControlsAction. Up/down (and k/j)
// move the cursor; space / enter / ← / → cycle the layout on the layout
// row; esc backs out. With one row, up/down are inert but kept so the
// navigation matches the Settings screen.
//
// Note: the App ingest-normalizes keypresses (ADR 0022) before this sees
// them, but every key here is a non-letter (arrows, space, esc) or k/j —
// none are remapped — so the selector behaves identically on any layout.
func (c *ControlsScreen) HandleKey(key string) ControlsAction {
	switch key {
	case "up", "k", "down", "j":
		// One row today; movement is a no-op but reserved.
	case " ", "enter", "left", "right":
		return ControlsActionCycleLayout
	case "esc":
		return ControlsActionCancel
	}
	return ControlsActionNone
}

// HandleClick maps a (col, row) click to a ControlsAction. A click on the
// title-row [Back] cancels; a click anywhere on the layout row cycles it.
func (c *ControlsScreen) HandleClick(col, row int) ControlsAction {
	if c.backBtn.Hit(col, row) {
		return ControlsActionCancel
	}
	if c.layoutBtn.Hit(col, row) {
		return ControlsActionCycleLayout
	}
	return ControlsActionNone
}

// Render returns the controls screen for the active layout. width sizes the
// right-aligned [Back] button and the full-row click target.
func (c *ControlsScreen) Render(layout keylayout.Layout, width int) string {
	var lines []string

	// Row 0: title + right-aligned [Back] button.
	const titleText = "controls"
	const backLabel = "[Back]"
	pad := width - len([]rune(titleText)) - len([]rune(backLabel))
	if pad < 1 {
		pad = 1
	}
	backCol := len([]rune(titleText)) + pad
	c.backBtn = buttonRange{row: 0, colStart: backCol, colEnd: backCol + len([]rune(backLabel)), set: true}
	lines = append(lines, c.theme.Title.Render(titleText)+
		strings.Repeat(" ", pad)+
		c.theme.Primary.Render(backLabel))

	lines = append(lines, c.theme.Dim.Render("─── keyboard ───"))
	lines = append(lines, "")

	// Layout selector row — a full-width click target.
	marker := "> " // single row is always the cursor
	value := "‹ " + keylayout.Label(layout) + " ›"
	c.layoutBtn = buttonRange{row: len(lines), colStart: 0, colEnd: width, set: true}
	lines = append(lines, marker+c.theme.Primary.Render("Keyboard layout: "+value))

	lines = append(lines, "")
	lines = append(lines, c.theme.Dim.Render("  Bindings are authored for QWERTY key positions. QWERTZ"))
	lines = append(lines, c.theme.Dim.Render("  swaps the physical Y and Z keys; selecting it keeps every"))
	lines = append(lines, c.theme.Dim.Render("  binding under the same finger and relabels the help (F1)."))

	lines = append(lines, "")
	lines = append(lines, c.theme.Footer.Render("[←/→ space] change layout  [esc] back"))
	return strings.Join(lines, "\n")
}

// HitBackButton reports whether a click at (col, row) lands on the
// title-row [Back] button — mirrors the other screens so the App mouse
// cascade can treat "left the screen" uniformly.
func (c *ControlsScreen) HitBackButton(col, row int) bool {
	return c.backBtn.Hit(col, row)
}
