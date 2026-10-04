package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/version"
)

// B11 / G9 Q1+Q2: one Title Row on every screen. The left field names the
// game (title case, with the version), then the screen and its context; the
// clock and warp sit in the middle (PAUSED while the clock is stopped); the
// way out is on the right ([Back] on a form, [»Burn] [Menu] [Missions] in
// flight). The right-hand button zone has a fixed width, so the clock stays
// in the same column when the player changes screens.

// wordmark is the game's name as it reads on screen. The slug stays the
// binary and the save directory name.
const wordmark = "Terminal Space Program"

// titleSep separates the Title Row's left-field parts.
const titleSep = " · "

// titleButtonsWidth is the cell width of the flight button set
// ("[»Burn]  [Menu]  [Missions]"). Forms right-align their single [Back]
// inside a zone of the same width.
const titleButtonsWidth = 27

// titleLeft joins the wordmark (with the version) and the screen/context
// parts with the title separator, skipping empty parts.
func titleLeft(parts ...string) string {
	out := wordmark + " " + version.Version
	for _, p := range parts {
		if p != "" {
			out += titleSep + p
		}
	}
	return out
}

// titleButton is one clickable label in the Title Row's right zone.
type titleButton struct {
	label    string
	rendered string
}

// titleSpec describes a Title Row.
type titleSpec struct {
	left string // plain text; rendered in the Title style
	w    *sim.World
	// extraPlain / extraRendered ride after the clock (the flight
	// bar's `[F2 declutter]` tag); empty on every other screen.
	extraPlain, extraRendered string
	buttons                   []titleButton
}

// titleLayout is the rendered row plus each button's display-cell span
// (start inclusive, end exclusive), in the order the buttons were given.
type titleLayout struct {
	row         string
	starts, end []int
}

// renderTitleRow lays out a Title Row across cols cells. Widths are
// measured with lipgloss.Width, never rune counts (» and ■ are
// ambiguous-width). Below the Design Size the row clips from the right
// (ADR 0046 decision 3): the pad never goes under one cell.
func renderTitleRow(th Theme, sp titleSpec, cols int) titleLayout {
	const gap = "  "
	const clockGap = "    "

	clockPlain, clockRendered := "", ""
	pausePlain, pauseRendered := "", ""
	if sp.w != nil {
		clockPlain = "T+" + sp.w.Clock.SimTime.Format("2006-01-02") + "  " + warpField(sp.w)
		clockRendered = th.Primary.Render(clockPlain)
		if sp.w.Clock.Paused {
			pausePlain = "  PAUSED"
			pauseRendered = "  " + th.Warning.Render("PAUSED")
		}
	}

	var plainBtns, renderedBtns []string
	for _, b := range sp.buttons {
		plainBtns = append(plainBtns, b.label)
		renderedBtns = append(renderedBtns, b.rendered)
	}
	btnPlain := strings.Join(plainBtns, gap)
	btnRendered := strings.Join(renderedBtns, gap)
	// Right-align the buttons inside the fixed zone.
	zoneLead := ""
	if d := titleButtonsWidth - lipgloss.Width(btnPlain); d > 0 {
		zoneLead = strings.Repeat(" ", d)
	}

	rightPlain := clockPlain + pausePlain + sp.extraPlain + clockGap + zoneLead + btnPlain
	leftW := lipgloss.Width(sp.left)
	pad := cols - leftW - lipgloss.Width(rightPlain)
	if pad < 1 {
		pad = 1
	}
	btnStart := leftW + pad + lipgloss.Width(clockPlain+pausePlain+sp.extraPlain+clockGap+zoneLead)

	lay := titleLayout{}
	x := btnStart
	for _, b := range sp.buttons {
		lay.starts = append(lay.starts, x)
		x += lipgloss.Width(b.label)
		lay.end = append(lay.end, x)
		x += lipgloss.Width(gap)
	}
	lay.row = th.Title.Render(sp.left) +
		strings.Repeat(" ", pad) +
		clockRendered + pauseRendered + sp.extraRendered +
		clockGap + zoneLead + btnRendered
	return lay
}

// RenderFormTitleRow is the Title Row for every non-flight screen: the
// wordmark, the screen name (and optional context), the clock, and a
// [Back] button. backStart/backEnd are the button's display-cell span on
// row 0 so the App can hit-test it.
func RenderFormTitleRow(th Theme, w *sim.World, screen, context string, cols int) (row string, backStart, backEnd int) {
	lay := renderTitleRow(th, titleSpec{
		left:    titleLeft(screen, context),
		w:       w,
		buttons: []titleButton{{label: "[Back]", rendered: th.Primary.Render("[Back]")}},
	}, cols)
	return lay.row, lay.starts[0], lay.end[0]
}
