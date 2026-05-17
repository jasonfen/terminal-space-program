package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// navball_panel.go — the bottom-center, framed navball overlay for
// the orbit view. v0.9.6-polish moved the navball out of the HUD
// column (where it shared vertical budget with TARGET / NODES) into
// a rounded-border panel composited over the canvas, KSP-style.
//
// The panel is opaque: it occludes the slice of map behind it. It is
// laid out with a control strip below the disk so the mode button
// and the prograde / normal± / radial± "press-to-hold" buttons have
// a home — click dispatch is wired app-side (see HitNavballControl).

// NavballControlID identifies a clickable region in the navball
// panel. The zero value navballControlNone means "no hit".
type NavballControlID int

const (
	navballControlNone NavballControlID = iota
	NavballControlMode                  // cycle NavMode (orbit / surface / target)
	NavballControlPrograde
	NavballControlRetrograde
	NavballControlNormalPlus
	NavballControlNormalMinus
	NavballControlRadialOut
	NavballControlRadialIn
)

// navballControlBox is the absolute screen-cell rectangle of one
// clickable control, recorded each render so the app's mouse handler
// can map a click back to an intent. Rows/cols are inclusive-start,
// exclusive-end, in final-frame screen coordinates.
type navballControlBox struct {
	id               NavballControlID
	colStart, colEnd int
	row              int
}

// navball panel geometry. The disk is the canonical 12×6 braille
// block; the inner content is widened to innerW so the control strip
// fits, and the disk is centred within it.
const (
	navballDiskCols = 12
	navballDiskRows = 6
	navballInnerW   = 22 // content width inside the border
	// Panel outer size = inner + 1-cell rounded border each side.
	navballPanelW = navballInnerW + 2
	navballPanelH = navballDiskRows + 4 // header + 6 disk + controls + border(2)
)

// axisButton is one control-strip cell-range, laid out left→right.
type axisButton struct {
	id    NavballControlID
	label string
}

// navballAxisRow is the fixed ordering of the SAS-axis buttons under
// the disk. Kept compact so the whole row fits navballInnerW.
var navballAxisRow = []axisButton{
	{NavballControlPrograde, "PRO"},
	{NavballControlRetrograde, "RET"},
	{NavballControlNormalPlus, "N+"},
	{NavballControlNormalMinus, "N-"},
	{NavballControlRadialOut, "R+"},
	{NavballControlRadialIn, "R-"},
}

func navModeLabel(m sim.NavMode) string {
	switch m {
	case sim.NavSurface:
		return "SURF"
	case sim.NavTarget:
		return "TGT"
	}
	return "ORBIT"
}

// buildNavballPanel renders the framed panel string and returns it
// together with the control layout relative to the panel's own
// top-left (0,0). The caller offsets these by the panel's screen
// position to get absolute hit boxes.
//
// disk is the already-rendered NavballString (navballDiskCols ×
// navballDiskRows). mode drives the [MODE] button label.
func (v *OrbitView) buildNavballPanel(disk string, mode sim.NavMode) (string, []navballControlBox) {
	pad := func(s string, w int) string {
		n := lipgloss.Width(s)
		if n >= w {
			return s
		}
		return s + strings.Repeat(" ", w-n)
	}
	center := func(s string, w int) string {
		n := lipgloss.Width(s)
		if n >= w {
			return s
		}
		left := (w - n) / 2
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", w-n-left)
	}

	var boxes []navballControlBox
	btnStyle := lipgloss.NewStyle().Foreground(v.theme.Primary.GetForeground())

	// Row 0: "NAVBALL" left, [MODE] button right-aligned.
	modeLabel := "[" + navModeLabel(mode) + "]"
	header := "NAVBALL"
	gap := navballInnerW - lipgloss.Width(header) - lipgloss.Width(modeLabel)
	if gap < 1 {
		gap = 1
	}
	// Mode button starts after header+gap, inside the border (+1).
	modeColStart := 1 + lipgloss.Width(header) + gap
	boxes = append(boxes, navballControlBox{
		id:       NavballControlMode,
		colStart: modeColStart,
		colEnd:   modeColStart + lipgloss.Width(modeLabel),
		row:      1, // +1 for the top border row
	})
	headerLine := v.theme.Primary.Render(header) +
		strings.Repeat(" ", gap) + btnStyle.Render(modeLabel)

	lines := []string{pad(headerLine, navballInnerW)}

	for _, dl := range strings.Split(disk, "\n") {
		lines = append(lines, center(dl, navballInnerW))
	}

	// Control strip: axis buttons separated by single spaces, the
	// whole group centred. Box columns are tracked as we lay it out.
	var seg strings.Builder
	col := 0
	type pendingBox struct {
		id   NavballControlID
		s, e int
	}
	var pend []pendingBox
	for i, b := range navballAxisRow {
		if i > 0 {
			seg.WriteString(" ")
			col++
		}
		pend = append(pend, pendingBox{b.id, col, col + len(b.label)})
		seg.WriteString(b.label)
		col += len(b.label)
	}
	stripPlain := seg.String()
	leftPad := (navballInnerW - lipgloss.Width(stripPlain)) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	stripRow := 1 + len(lines) // +1 top border; lines so far = header+disk
	var styledStrip strings.Builder
	styledStrip.WriteString(strings.Repeat(" ", leftPad))
	for i, b := range navballAxisRow {
		if i > 0 {
			styledStrip.WriteString(" ")
		}
		styledStrip.WriteString(btnStyle.Render(b.label))
	}
	for _, p := range pend {
		boxes = append(boxes, navballControlBox{
			id:       p.id,
			colStart: 1 + leftPad + p.s, // +1 left border
			colEnd:   1 + leftPad + p.e,
			row:      stripRow,
		})
	}
	lines = append(lines, pad(styledStrip.String(), navballInnerW))

	content := strings.Join(lines, "\n")
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.theme.Primary.GetForeground()).
		Render(content)
	return panel, boxes
}

// splitStyledCells splits an ANSI-styled line into exactly one
// self-contained string per visible terminal cell, so len(result)
// always equals the line's display width and a cell-boundary splice
// stays aligned. It tracks the active SGR run rather than assuming a
// fixed escape/rune layout: the canvas emits one rune per styled run
// (`CSI…m<rune>CSI0m`) while lipgloss emits whole multi-rune runs
// (`CSI…m<rune><rune>…CSI0m`, e.g. a border edge or "NAVBALL").
// Both collapse to the same per-rune cell here — `activeSGR + rune
// (+ reset if styled)`. An earlier layout-based parser mis-handled
// multi-rune runs (a stray zero-width reset cell per run), inflating
// the count and shoving right-edge cells off the splice — the
// missing panel border bug.
func splitStyledCells(s string) []string {
	rs := []rune(s)
	const sgrReset = "\x1b[0m"
	// readCSI returns the full CSI sequence at rs[i] (ESC '[' … final
	// byte @–~) and whether it's an SGR reset (`ESC[0m` / `ESC[m`).
	readCSI := func(i int) (seq string, next int, isReset bool) {
		j := i + 1
		var b strings.Builder
		b.WriteRune(rs[i])
		if j < len(rs) && rs[j] == '[' {
			b.WriteRune(rs[j])
			j++
		}
		var params strings.Builder
		for j < len(rs) {
			c := rs[j]
			b.WriteRune(c)
			j++
			if c >= '@' && c <= '~' {
				reset := c == 'm' &&
					(params.Len() == 0 || params.String() == "0")
				return b.String(), j, reset
			}
			params.WriteRune(c)
		}
		return b.String(), j, false
	}
	var cells []string
	var activeSGR strings.Builder
	i := 0
	for i < len(rs) {
		if rs[i] == 0x1b {
			seq, j, isReset := readCSI(i)
			if isReset {
				activeSGR.Reset()
			} else {
				activeSGR.WriteString(seq) // accumulate stacked styles
			}
			i = j
			continue
		}
		if activeSGR.Len() == 0 {
			cells = append(cells, string(rs[i]))
		} else {
			cells = append(cells, activeSGR.String()+string(rs[i])+sgrReset)
		}
		i++
	}
	return cells
}

// overlayStyledBlock splices block over base, placing the block's
// top-left at (atRow, atCol) in cell coordinates. base lines are
// assumed to be exactly baseCols cells wide (the canvas pads them).
// Both base and block may carry ANSI styling; the splice is
// cell-aware so styling on either side stays intact. Rows/cols that
// fall outside base are clipped.
func overlayStyledBlock(base []string, block string, atRow, atCol, baseCols int) []string {
	out := make([]string, len(base))
	copy(out, base)
	for r, bl := range strings.Split(block, "\n") {
		ri := atRow + r
		if ri < 0 || ri >= len(out) {
			continue
		}
		baseCells := splitStyledCells(out[ri])
		for len(baseCells) < baseCols {
			baseCells = append(baseCells, " ")
		}
		blockCells := splitStyledCells(bl)
		var b strings.Builder
		for c := 0; c < len(baseCells); c++ {
			oc := c - atCol
			if oc >= 0 && oc < len(blockCells) {
				b.WriteString(blockCells[oc])
			} else {
				b.WriteString(baseCells[c])
			}
		}
		out[ri] = b.String()
	}
	return out
}

// navballPanelMarkers is a thin pass-through kept here so the panel's
// data dependency on sim is colocated with its rendering. It exists
// to make the Render() call site read as panel-scoped.
func navballPanelDisk(w *sim.World, subLat, subLon float64) string {
	return render.NavballString(navballDiskCols, navballDiskRows, subLat, subLon, w.NavballMarkers())
}
