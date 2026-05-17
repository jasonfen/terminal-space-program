package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// navball_panel.go — the framed, KSP-style navball overlay for the
// orbit view. v0.9.6-polish moved the navball out of the HUD column
// into a rounded-border panel composited bottom-right over the
// canvas; the redesign drops the redundant "NAVBALL" label, adds a
// top [MODE]/RCS toggle row, and stacks the eight SAS controls
// (prograde/retrograde, normal±, radial±, target±) as a vertical
// glyph column down the left, mirroring KSP's SAS icon stack.
//
// The panel is opaque (it occludes the map slice behind it). Click
// dispatch is wired app-side (see HitNavballControl /
// dispatchNavballControl).

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
	NavballControlRCS         // toggle EngineMain <-> EngineRCS
	NavballControlTargetPlus  // hold toward target (BurnTarget)
	NavballControlTargetMinus // hold away from target (BurnAntiTarget)
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

// navball panel geometry (KSP-style). A compact top toggle row
// ([MODE] + RCS), then a 24×12 disk with a vertical stack of eight
// 2-row "<glyph> LABEL" SAS buttons hugging the far left. The disk
// is shorter than the button stack, so it's centred vertically.
const (
	// Doubled from the original 12×6 — the small disk made markers
	// hard to read and the 1-cell glyph buttons hard to click.
	navballDiskCols  = 24
	navballDiskRows  = 12
	navballLabelW    = 3                                      // label field, e.g. "PRO" / "T- "
	navballBtnW      = 1 + 1 + navballLabelW                  // glyph + sep + label = 5
	navballBtnRows   = 2                                      // each SAS button is 2 rows tall
	navballGlyphColW = navballBtnW + 1                        // + 1 gutter to the disk = 6
	navballBodyRows  = 8 * navballBtnRows                     // 8 buttons × 2 rows = 16
	navballInnerW    = navballGlyphColW + navballDiskCols + 2 // = 32
	// Panel outer size = inner + 1-cell rounded border each side.
	navballPanelW      = navballInnerW + 2                // = 34
	navballPanelH      = 1 + navballBodyRows + 2          // toggle + body + border = 19
	navballDiskRegionW = navballInnerW - navballGlyphColW // = 26
	navballDiskTopPad  = (navballBodyRows - navballDiskRows) / 2
)

// axisButton is one vertical SAS button: a marker glyph + a short
// text label. The glyph mirrors the on-ball marker (KSP convention)
// in that marker's colour so the column reads as the same icon
// family as the disk; the label makes it legible, since a lone
// glyph can't be enlarged in a fixed-cell terminal.
type axisButton struct {
	id    NavballControlID
	glyph rune
	label string
	color lipgloss.Color
}

// navballAxisRow is the fixed top→bottom ordering of the SAS button
// column. Eight buttons: prograde / retrograde, normal ±, radial ±,
// target ±. Glyphs + colours come from the shared sim/render
// constants so the buttons and the disk markers can't drift apart.
var navballAxisRow = []axisButton{
	{NavballControlPrograde, sim.NavballGlyphPrograde, "PRO", render.ColorNavballMarkerPrograde},
	{NavballControlRetrograde, sim.NavballGlyphRetrograde, "RET", render.ColorNavballMarkerPrograde},
	{NavballControlNormalPlus, sim.NavballGlyphNormalPlus, "N+", render.ColorNavballMarkerNormal},
	{NavballControlNormalMinus, sim.NavballGlyphNormalMinus, "N-", render.ColorNavballMarkerNormal},
	{NavballControlRadialOut, sim.NavballGlyphRadialOut, "R+", render.ColorNavballMarkerRadial},
	{NavballControlRadialIn, sim.NavballGlyphRadialIn, "R-", render.ColorNavballMarkerRadial},
	{NavballControlTargetPlus, sim.NavballGlyphTarget, "T+", render.ColorNavballMarkerTarget},
	{NavballControlTargetMinus, sim.NavballGlyphAntiTarget, "T-", render.ColorNavballMarkerTarget},
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
// navballDiskRows). mode drives the [MODE] button label; rcsActive
// colours the RCS toggle (Warning when on, Dim when off).
//
// Every assembled line is exactly navballInnerW cells wide so the
// caller's splitStyledCells / overlayStyledBlock splice stays
// aligned (the historical right-border-drop invariant).
func (v *OrbitView) buildNavballPanel(disk string, mode sim.NavMode, rcsActive bool) (string, []navballControlBox) {
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

	// Inner row 0 (panel row 1): [MODE] left, RCS right. No label —
	// the disk speaks for itself.
	modeLabel := "[" + navModeLabel(mode) + "]"
	rcsLabel := "RCS"
	rcsStyle := v.theme.Dim
	if rcsActive {
		rcsStyle = v.theme.Warning
	}
	gap := navballInnerW - lipgloss.Width(modeLabel) - lipgloss.Width(rcsLabel)
	if gap < 1 {
		gap = 1
	}
	boxes = append(boxes,
		navballControlBox{
			id:       NavballControlMode,
			colStart: 1, // +1 left border; mode starts at inner col 0
			colEnd:   1 + lipgloss.Width(modeLabel),
			row:      1, // +1 top border; toggle is panel row 1
		},
		navballControlBox{
			id:       NavballControlRCS,
			colStart: 1 + lipgloss.Width(modeLabel) + gap,
			colEnd:   1 + lipgloss.Width(modeLabel) + gap + lipgloss.Width(rcsLabel),
			row:      1,
		},
	)
	toggleLine := pad(btnStyle.Render(modeLabel)+
		strings.Repeat(" ", gap)+rcsStyle.Render(rcsLabel), navballInnerW)
	lines := []string{toggleLine}

	// Body: navballBodyRows rows. Left column = a stack of SAS
	// buttons, each navballBtnRows tall (a big click target) showing
	// "<glyph> <LABEL>"; then a 1-cell gutter; then the disk region.
	// The disk is centred vertically within the taller button stack;
	// off-disk body rows still carry their button so the column is
	// continuous. The button face is drawn on the first row of each
	// pair, the rest blank — but every row of the pair gets a hit
	// box (same id) so the whole 2-row block is clickable.
	diskLines := strings.Split(disk, "\n")
	for j := 0; j < navballBodyRows; j++ {
		bi := j / navballBtnRows
		b := navballAxisRow[bi]
		face := strings.Repeat(" ", navballBtnW)
		if j%navballBtnRows == 0 { // top row of the pair carries the face
			label := b.label
			if len(label) < navballLabelW {
				label += strings.Repeat(" ", navballLabelW-len(label))
			}
			face = lipgloss.NewStyle().Foreground(b.color).Render(string(b.glyph)) +
				" " + btnStyle.Render(label) // 1 + 1 + navballLabelW = navballBtnW
		}
		region := strings.Repeat(" ", navballDiskRegionW)
		if di := j - navballDiskTopPad; di >= 0 && di < navballDiskRows && di < len(diskLines) {
			region = center(diskLines[di], navballDiskRegionW)
		}
		lines = append(lines, face+" "+region) // btnW + gutter + regionW = innerW
		boxes = append(boxes, navballControlBox{
			id:       b.id,
			colStart: 1,               // +1 left border; face at inner col 0
			colEnd:   1 + navballBtnW, // full button width is clickable
			row:      j + 2,           // +1 top border, +1 toggle row
		})
	}

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
