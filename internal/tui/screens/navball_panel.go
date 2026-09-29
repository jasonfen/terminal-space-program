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
	NavballControlSAS                   // toggle InstantSAS (MANUAL slew <-> AUTO instant)
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
// ([MODE] + RCS), then a disk with a vertical stack of eight SAS buttons
// hugging the far left. The disk is shorter than the button stack at the
// Design Size, so it is centred vertically. The disk and panel scale with
// canvas height (navballGeometry, ADR 0051 W5); the constants below are
// the Design Size values and the parts that never scale.
const (
	// Design Size disk (140x40, canvas 37 rows). Doubled from the original
	// 12x6: the small disk made markers hard to read and the 1-cell glyph
	// buttons hard to click. Cells are about 2:1, so cols = 2 x rows keeps
	// the disk round.
	navballBaseDiskRows = 12
	navballBaseDiskCols = 2 * navballBaseDiskRows
	navballLabelW       = 3                     // label field, e.g. "PRO" / "T- "
	navballBtnW         = 1 + 1 + navballLabelW // glyph + sep + label = 5
	navballBtnRows      = 2                     // each SAS button is at least 2 rows tall
	navballGlyphColW    = navballBtnW + 1       // + 1 gutter to the disk = 6
	navballBaseBodyRows = 8 * navballBtnRows    // 8 buttons x 2 rows = 16
	// Rows of body the panel keeps beyond the disk: at the Design Size the
	// 16-row button stack around a 12-row disk.
	navballBodyExtra = navballBaseBodyRows - navballBaseDiskRows // 4
	// Border (2) + toggle row (1).
	navballChromeRows = 3
)

// navballGeom is the navball panel's size at one canvas height.
type navballGeom struct {
	diskCols, diskRows int
	bodyRows           int // rows below the toggle row, inside the border
	innerW             int
	panelW, panelH     int // outer size, border included
	diskRegionW        int
	diskTopPad         int
}

// navballFullRightStackRows is the height NAVIGATION + TARGET occupy at
// their maximum line counts (the Full Empty readings setting, both boxes
// drawn with every row), borders included, stacked from canvas row 0 with
// chipGap between them. The navball is budgeted against this and not the
// live boxes, so pressing t or changing Empty readings never resizes it.
func navballFullRightStackRows() int {
	return (navigationBoxMaxLines + 2) + chipGap + (targetBoxMaxLines + 2)
}

// navballMaxPanelRows is the tallest panel that still sits below the full
// right stack. canvasRows is canvas rows, not terminal rows. The panel is
// lifted one row off the bottom (the "view:" label row, see
// composeNavballOverlay).
func navballMaxPanelRows(canvasRows int) int {
	return canvasRows - 1 - navballFullRightStackRows()
}

// navballGeometry sizes the panel for a canvas canvasRows tall. Unchanged
// (24x12 disk, 34x19 panel) at the Design Size; grows by heightScaledCells
// above it, capped so the panel never reaches the rows NAVIGATION and
// TARGET can occupy.
func navballGeometry(canvasRows int) navballGeom {
	return navballGeometryCapped(canvasRows, navballMaxPanelRows(canvasRows))
}

// navballGeometryCapped is navballGeometry with the panel height cap
// passed in, so tests can drive the cap at heights the real one never
// binds at.
func navballGeometryCapped(canvasRows, maxPanelRows int) navballGeom {
	rows := heightScaledCells(navballBaseDiskRows, canvasRows)
	if maxDisk := maxPanelRows - navballChromeRows - navballBodyExtra; rows > maxDisk {
		rows = maxDisk
	}
	if rows < navballBaseDiskRows {
		rows = navballBaseDiskRows // never below the Design Size disk
	}
	g := navballGeom{diskRows: rows, diskCols: 2 * rows}
	g.bodyRows = rows + navballBodyExtra
	g.innerW = navballGlyphColW + g.diskCols + 2
	g.panelW = g.innerW + 2
	g.panelH = navballChromeRows + g.bodyRows
	g.diskRegionW = g.innerW - navballGlyphColW
	g.diskTopPad = (g.bodyRows - g.diskRows) / 2
	return g
}

// buttonRow reports which SAS button owns body row j, and whether j is the
// button's first row (where its face is drawn). The eight buttons share
// the body rows evenly, so a taller panel keeps one continuous column with
// every row clickable. At the Design Size (16 rows) each button is exactly
// 2 rows, as before.
func (g navballGeom) buttonRow(j int) (button int, first bool) {
	n := len(navballAxisRow)
	button = j * n / g.bodyRows
	first = j == 0 || (j-1)*n/g.bodyRows != button
	return button, first
}

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

// sasTagLabel is the manual-flight attitude-model tag shown between
// [MODE] and RCS. MAN = rate-limited slew (v0.10.0 default,
// instantSAS=false); AUT = legacy instantaneous snap. Kept to 3
// glyphs so the bracketed tag matches the [TGT]/[SURF] visual weight.
// v0.10.0+.
func sasTagLabel(instantSAS bool) string {
	if instantSAS {
		return "AUT"
	}
	return "MAN"
}

// buildNavballPanel renders the framed panel string and returns it
// together with the control layout relative to the panel's own
// top-left (0,0). The caller offsets these by the panel's screen
// position to get absolute hit boxes.
//
// disk is the already-rendered NavballString (navballDiskCols ×
// g.diskRows). mode drives the [MODE] button label; rcsActive
// colours the RCS toggle (Warning when on, Dim when off).
//
// Every assembled line is exactly g.innerW cells wide so the
// caller's splitStyledCells / overlayStyledBlock splice stays
// aligned (the historical right-border-drop invariant).
func (v *OrbitView) buildNavballPanel(g navballGeom, disk string, mode sim.NavMode, instantSAS, rcsActive bool) (string, []navballControlBox) {
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

	// Inner row 0 (panel row 1): [MODE] left, [SAS] centred, RCS
	// right. [MODE] cycles NavMode; [SAS] toggles the manual-flight
	// attitude model (MAN slew / AUT instant — World.InstantSAS);
	// RCS toggles the thruster. The disk speaks for itself, no label.
	modeLabel := "[" + navModeLabel(mode) + "]"
	sasLabel := "[" + sasTagLabel(instantSAS) + "]"
	rcsLabel := "RCS"
	rcsStyle := v.theme.Dim
	if rcsActive {
		rcsStyle = v.theme.Warning
	}
	// MAN (slew) is the v0.10 default → neutral; AUT (instant) is the
	// legacy opt-out → Warning, so the non-default model is never
	// silent (the locked-decision "not silent" requirement).
	sasStyle := btnStyle
	if instantSAS {
		sasStyle = v.theme.Warning
	}

	mw := lipgloss.Width(modeLabel)
	sw := lipgloss.Width(sasLabel)
	rw := lipgloss.Width(rcsLabel)
	// modeLabel hugs inner col 0; rcsLabel ends flush at innerW;
	// sasLabel sits centred between, clamped off both neighbours so
	// the three never collide on a wide [ORBIT] mode label.
	rcsStart := g.innerW - rw
	sasStart := (g.innerW - sw) / 2
	if sasStart < mw+1 {
		sasStart = mw + 1
	}
	if sasStart+sw > rcsStart-1 {
		sasStart = rcsStart - 1 - sw
	}
	gap1 := sasStart - mw
	gap2 := rcsStart - (sasStart + sw)
	if gap1 < 1 {
		gap1 = 1
	}
	if gap2 < 1 {
		gap2 = 1
	}
	boxes = append(boxes,
		navballControlBox{
			id:       NavballControlMode,
			colStart: 1, // +1 left border; mode starts at inner col 0
			colEnd:   1 + mw,
			row:      1, // +1 top border; toggle is panel row 1
		},
		navballControlBox{
			id:       NavballControlSAS,
			colStart: 1 + sasStart,
			colEnd:   1 + sasStart + sw,
			row:      1,
		},
		navballControlBox{
			id:       NavballControlRCS,
			colStart: 1 + rcsStart,
			colEnd:   1 + rcsStart + rw,
			row:      1,
		},
	)
	toggleLine := pad(btnStyle.Render(modeLabel)+
		strings.Repeat(" ", gap1)+sasStyle.Render(sasLabel)+
		strings.Repeat(" ", gap2)+rcsStyle.Render(rcsLabel), g.innerW)
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
	for j := 0; j < g.bodyRows; j++ {
		bi, first := g.buttonRow(j)
		b := navballAxisRow[bi]
		face := strings.Repeat(" ", navballBtnW)
		if first { // top row of the button carries the face
			label := b.label
			if len(label) < navballLabelW {
				label += strings.Repeat(" ", navballLabelW-len(label))
			}
			face = lipgloss.NewStyle().Foreground(b.color).Render(string(b.glyph)) +
				" " + btnStyle.Render(label) // 1 + 1 + navballLabelW = navballBtnW
		}
		region := strings.Repeat(" ", g.diskRegionW)
		if di := j - g.diskTopPad; di >= 0 && di < g.diskRows && di < len(diskLines) {
			region = center(diskLines[di], g.diskRegionW)
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
func navballPanelDisk(g navballGeom, w *sim.World, subLat, subLon float64) string {
	return render.NavballString(g.diskCols, g.diskRows, subLat, subLon, w.NavballMarkers())
}
