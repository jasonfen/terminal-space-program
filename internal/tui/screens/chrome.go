package screens

import (
	"github.com/charmbracelet/x/ansi"
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

// ----- Form frame and boxes (B11 / G9 Q4) -----
//
// Every form lives inside the same rounded frame the map has, its sections
// are titled rounded boxes (the title on the first inner row, the way the
// ADR 0051 instrument boxes do it), and the key legend rides the frame's
// bottom edge, the band the status flash overlays (overlayBottomBorder).
// Screens build their body as a list of lines and hand it to formFrame;
// the frame adds one cell of border on every side, so a screen's own
// hit-test coordinates are body coordinates and HandleClick takes the
// frame-relative position and subtracts frameInset.

// frameInset is the border thickness a form frame adds on every side.
const frameInset = 1

// padCells pads or clips s to exactly w display cells (ANSI-aware).
func padCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	if gap := w - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// formBox draws a titled rounded box w cells wide (borders included):
// top edge, the title on the first inner row, the lines (each padded or
// clipped to the box), bottom edge. A box needs at least 4 cells.
func formBox(th Theme, title string, lines []string, w int) []string {
	if w < 4 {
		w = 4
	}
	inner := w - 2
	edge := func(l, r string) string {
		return th.Primary.Render(l + strings.Repeat("─", inner) + r)
	}
	side := th.Primary.Render("│")
	out := make([]string, 0, len(lines)+3)
	out = append(out, edge("╭", "╮"))
	out = append(out, side+padCells(th.Primary.Render(title), inner)+side)
	for _, ln := range lines {
		out = append(out, side+padCells(ln, inner)+side)
	}
	out = append(out, edge("╰", "╯"))
	return out
}

// joinColumns puts two line blocks side by side (left padded to lw cells,
// a gap-cell gutter between), padding the shorter block with blanks.
func joinBoxes(left, right []string, lw, gap int) []string {
	n := len(left)
	if len(right) > n {
		n = len(right)
	}
	rw := 0
	for _, r := range right {
		if w := lipgloss.Width(r); w > rw {
			rw = w
		}
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		out = append(out, padCells(l, lw)+strings.Repeat(" ", gap)+padCells(r, rw))
	}
	return out
}

// formFrame wraps body in the shared rounded frame, exactly w cells wide
// and h rows tall (h <= 0: as tall as the body needs): top edge, h-2 body rows (padded or clipped), and a
// bottom edge that carries legend (already styled) as `╰─ legend ───╯`.
func formFrame(th Theme, body []string, w, h int, legend string) string {
	if w < 8 {
		w = 8
	}
	if h <= 0 {
		h = len(body) + 2 // no height budget: show the whole body
	}
	if h < 3 {
		h = 3
	}
	inner := w - 2
	side := th.Primary.Render("│")
	rows := make([]string, 0, h)
	rows = append(rows, th.Primary.Render("╭"+strings.Repeat("─", inner)+"╮"))
	for i := 0; i < h-2; i++ {
		ln := ""
		if i < len(body) {
			ln = body[i]
		}
		rows = append(rows, side+padCells(ln, inner)+side)
	}
	if legend == "" {
		rows = append(rows, th.Primary.Render("╰"+strings.Repeat("─", inner)+"╯"))
	} else {
		label := " " + legend + " "
		room := inner - 1 // one dash lead
		if lipgloss.Width(label) > room {
			label = ansi.Truncate(label, room, "…")
		}
		trail := inner - 1 - lipgloss.Width(label)
		if trail < 0 {
			trail = 0
		}
		rows = append(rows, th.Primary.Render("╰─")+label+th.Primary.Render(strings.Repeat("─", trail)+"╯"))
	}
	return strings.Join(rows, "\n")
}
