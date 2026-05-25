package screens

import (
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// SpriteCell is one terminal-cell emit of a composed launch sprite —
// a world-frame offset from the vessel anchor (`c.State.R + bodyPos`)
// plus the glyph and color the render path should plot at that
// position. The render path translates each cell into a PlotColored
// + SetCellOverlay pair on the chase-cam canvas.
type SpriteCell struct {
	OffsetWorld orbital.Vec3
	Glyph       rune
	Color       lipgloss.Color
}

// ComposeLaunchSprite builds the composed-from-stages rocket sprite
// for a vessel, returning a cell list the render path plots relative
// to the vessel's world anchor.
//
// Stages stack bottom-to-top from Stages[0] (lowest in screen) along
// the projection of cmdWorld into the chase-cam basis — so a
// gravity-turned rocket leans visibly while each stage's cells stay
// screen-axis-aligned (Slice 4 grill, 2026-05-25). Per-row world
// stride is `scaleMPerPx · canvasCellPxH` along the stack axis;
// per-column stride is `scaleMPerPx · canvasCellPxW` along the
// perpendicular axis. Returns nil when no stage has LaunchSprite set
// (caller falls back to the vessel's single Glyph).
func ComposeLaunchSprite(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64) []SpriteCell {
	cellHeight := scaleMPerPx * canvasCellPxH
	cellWidth := scaleMPerPx * canvasCellPxW

	// Stack-axis direction in screen = normalised projection of cmd
	// onto (basis.X, basis.Y). Width-axis is perpendicular, rotated
	// +90° in screen so col 0 sits left of stack, col 1 right.
	// Degenerate cmd (along basis depth axis or zero) falls back to
	// pure-vertical stacking.
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	widthX, widthY := -stackY, stackX

	var cells []SpriteCell
	rowOffset := 0
	for _, s := range stages {
		if s.LaunchSprite == "" {
			continue
		}
		lines := strings.Split(s.LaunchSprite, "\n")
		for r, line := range lines {
			rowAbove := float64(len(lines)-1-r) + float64(rowOffset)
			col := 0
			for _, glyph := range line {
				if col >= 2 {
					break
				}
				if glyph != 0 {
					xCells := float64(col) - 0.5
					screenSX := rowAbove*cellHeight*stackX + xCells*cellWidth*widthX
					screenSY := rowAbove*cellHeight*stackY + xCells*cellWidth*widthY
					offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
					cells = append(cells, SpriteCell{
						OffsetWorld: offset,
						Glyph:       glyph,
						Color:       lipgloss.Color(s.Color),
					})
				}
				col++
			}
		}
		rowOffset += len(lines)
	}
	if len(cells) == 0 {
		return nil
	}
	return cells
}

// ComposeFlame builds the exhaust flame cells appended below
// Stages[0]'s base along the -cmdWorld direction (so the flame leans
// alongside the gravity-turned stack). 2 cells wide, length-binned
// by throttle, 2-frame pulse driven by frameIdx (caller derives from
// wall-clock):
//
//   - throttle ≤ 0 or no stages: returns nil.
//   - 0 < throttle ≤ 1/3: 1 row.
//   - 1/3 < throttle ≤ 2/3: 2 rows.
//   - 2/3 < throttle:      3 rows.
//
// Glyph palette (top row brightest, fading down):
//
//   - frame A: ▓▒░
//   - frame B: █▓▒
//
// Color: amber `render.ColorWarning`.
func ComposeFlame(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64, throttle float64, frameIdx int) []SpriteCell {
	if throttle <= 0 || len(stages) == 0 {
		return nil
	}
	var nRows int
	switch {
	case throttle <= 1.0/3.0:
		nRows = 1
	case throttle <= 2.0/3.0:
		nRows = 2
	default:
		nRows = 3
	}

	frameA := [3]rune{'▓', '▒', '░'}
	frameB := [3]rune{'█', '▓', '▒'}
	glyphs := frameA
	if frameIdx%2 == 1 {
		glyphs = frameB
	}

	cellHeight := scaleMPerPx * canvasCellPxH
	cellWidth := scaleMPerPx * canvasCellPxW
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	widthX, widthY := -stackY, stackX

	cells := make([]SpriteCell, 0, nRows*2)
	for row := 0; row < nRows; row++ {
		// row 0 is the topmost flame cell, just below Stages[0]'s
		// base at rowAbove = -1; row 1 at -2; row 2 at -3.
		rowAbove := -float64(row + 1)
		for col := 0; col < 2; col++ {
			xCells := float64(col) - 0.5
			screenSX := rowAbove*cellHeight*stackX + xCells*cellWidth*widthX
			screenSY := rowAbove*cellHeight*stackY + xCells*cellWidth*widthY
			offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
			cells = append(cells, SpriteCell{
				OffsetWorld: offset,
				Glyph:       glyphs[row],
				Color:       render.ColorWarning,
			})
		}
	}
	return cells
}

// stackDirScreen returns the unit-vector projection of cmdWorld into
// the chase-cam basis's (X, Y) screen plane. Falls back to (0, 1) —
// pure vertical stacking — when the projection magnitude is below
// 1e-9 (cmd parallel to the depth axis, or zero).
func stackDirScreen(cmdWorld orbital.Vec3, basis widgets.Basis) (x, y float64) {
	x = cmdWorld.Dot(basis.X)
	y = cmdWorld.Dot(basis.Y)
	mag := math.Sqrt(x*x + y*y)
	if mag < 1e-9 {
		return 0, 1
	}
	return x / mag, y / mag
}
