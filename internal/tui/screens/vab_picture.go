package screens

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// The VAB picture (#596, grill GS Q1): the rocket being built, drawn by the
// same composer the pad rocket uses (ComposeLaunchSprite + the engine bell),
// rasterised to braille cells in a box under the stage list. Nothing about
// the rocket's shape is decided here: a stage's rows, width and colour come
// from the Stage (catalog-authored, or derived from mass and fuel chemistry
// for a stage built from parts, see spacecraft/sprite_derive.go), so the VAB
// and the launch view cannot disagree.

// Picture box sizing at the VAB: shown only when the whole stage list still
// fits above it.
const (
	vabPictureMinBox  = 10 // box rows, edges and title included
	vabPictureMaxBox  = 26
	vabVehicleMinBox  = 14 // the stage list never windows below this to make room
	pictureLabelGap   = 2  // blank cells between the rocket and its stage labels
	pictureLabelWidth = 24
)

// brailleBit maps (sub-row, sub-col) inside a cell to its braille dot bit.
var brailleBit = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

// stackPictureSprite is the rocket as the launch view would compose it, in an
// upright identity basis at one sub-pixel per unit: the stack, then the
// engine bell below the first stage.
func stackPictureSprite(stages []spacecraft.Stage) []SpritePixel {
	cmd := orbital.Vec3{Y: 1}
	basis := widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Y: 1}}
	px := ComposeLaunchSprite(stages, cmd, basis, 1)
	if px == nil {
		return nil
	}
	return append(px, ComposeEngineBell(coreStages(stages), cmd, basis, 1)...)
}

// renderStackPicture rasterises stages (bottom first) into h lines of w
// cells: the rocket stands on the bottom row, centred left of its stage
// labels, squeezed vertically (never in width) only when it is taller than
// the box.
// labels[i] names stage i; focus is the stage to mark with a pointer (-1 for
// none).
func renderStackPicture(th Theme, stages []spacecraft.Stage, labels []string, focus, w, h int) []string {
	blank := strings.Repeat(" ", max(w, 0))
	out := make([]string, max(h, 0))
	for i := range out {
		out[i] = blank
	}
	px := stackPictureSprite(stages)
	if len(px) == 0 || w < 8 || h < 2 {
		if len(px) == 0 && h >= 1 && w >= 8 {
			out[0] = th.Dim.Render(padCells("  nothing to draw yet", w))
		}
		return out
	}

	yMin, yMax := math.Inf(1), math.Inf(-1)
	for _, p := range px {
		y := p.OffsetWorld.Y
		yMin, yMax = math.Min(yMin, y), math.Max(yMax, y)
	}
	totalPx := yMax - yMin + 1
	k := math.Max(1, totalPx/float64(4*h)) // vertical squeeze

	// Rocket column: leave room for the labels on the right.
	labelRoom := 0
	if w >= 30 {
		labelRoom = pictureLabelGap + min(pictureLabelWidth, w/3)
	}
	cx := (w - labelRoom) / 2 // centre cell of the rocket
	type cell struct {
		bits   rune
		counts map[lipgloss.Color]int
	}
	cells := map[[2]int]*cell{}
	for _, p := range px {
		ix := int(math.Floor(p.OffsetWorld.X + float64(2*cx))) // widths are never scaled
		iy := int(math.Floor((p.OffsetWorld.Y - yMin) / k))
		if ix < 0 || ix >= 2*w || iy < 0 || iy >= 4*h {
			continue
		}
		key := [2]int{ix / 2, iy / 4}
		c := cells[key]
		if c == nil {
			c = &cell{counts: map[lipgloss.Color]int{}}
			cells[key] = c
		}
		c.bits |= brailleBit[3-iy%4][ix%2]
		c.counts[p.Color]++
	}
	lines := make([][]string, h) // [row from top][col]
	for r := range lines {
		lines[r] = make([]string, w)
		for c := range lines[r] {
			lines[r][c] = " "
		}
	}
	for key, c := range cells {
		best, bestN := lipgloss.Color(""), -1
		for col, n := range c.counts {
			if n > bestN || (n == bestN && col < best) {
				best, bestN = col, n
			}
		}
		row := h - 1 - key[1]
		lines[row][key[0]] = lipgloss.NewStyle().Foreground(best).Render(string(rune(0x2800) + c.bits))
	}

	// Stage labels to the right of the rocket, at each stage's middle row.
	if labelRoom > 0 && len(labels) > 0 {
		spans := map[int][2]int{}
		for _, r := range launchStackLayout(stages) {
			if r.kind != rectBody && r.kind != rectSide {
				continue
			}
			spans[r.stage] = [2]int{r.rowBase, r.rowBase + r.rows}
		}
		labelX := cx + 4
		if maxW := composedStackMaxWidth(stages); maxW > 0 {
			labelX = cx + maxW/2 + pictureLabelGap + 1
		}
		used := map[int]bool{}
		place := func(i int) {
			sp, ok := spans[i]
			if !ok || i >= len(labels) {
				return
			}
			// Spans are relative to the stack bottom (row 0); pixels to yMin,
			// which is negative when the bell hangs below stage 0.
			mid := (float64(sp[0]+sp[1])/2 - yMin) / k
			row := h - 1 - int(mid)/4
			if row < 0 {
				row = 0
			}
			if row >= h {
				row = h - 1
			}
			if used[row] && i != focus {
				return
			}
			used[row] = true
			text := fmt.Sprintf("  S%d %s", i+1, labels[i])
			style := th.Dim
			if i == focus {
				text = fmt.Sprintf("▸ S%d %s", i+1, labels[i])
				style = th.Primary
			}
			text = truncWidth(text, w-labelX)
			col := labelX
			for _, r := range []rune(text) {
				if col >= w {
					break
				}
				lines[row][col] = style.Render(string(r))
				col++
			}
		}
		for i := range stages {
			if i != focus {
				place(i)
			}
		}
		place(focus)
	}
	for r := range lines {
		out[r] = strings.Join(lines[r], "")
	}
	return out
}

// pictureStageName is the short name the picture gives stage i:
// a catalog block's name, or the fuel chemistry of a stage built from parts.
func (v *VAB) pictureStageName(i int) string {
	vs := v.stages[i]
	if vs.isCatalog() {
		if m, ok := spacecraft.StageCatalog[vs.catalogPartID]; ok {
			return m.Name
		}
		return "catalog"
	}
	if ft := v.stageFuelType(vs.components); ft != "" {
		return ft
	}
	return "—"
}

// renderPicture is the PICTURE box's content: exactly h lines of w cells.
func (v *VAB) renderPicture(w, h int) []string {
	stages := v.resolvedStages()
	labels := make([]string, len(v.stages))
	for i := range v.stages {
		labels[i] = v.pictureStageName(i)
	}
	focus := -1
	if len(v.stages) > 0 {
		focus = clampI(v.stageIdx, 0, len(v.stages)-1)
	}
	return renderStackPicture(v.theme, stages, labels, focus, w, h)
}
