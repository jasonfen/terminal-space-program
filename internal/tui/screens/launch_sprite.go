package screens

import (
	"math"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// SpritePixel is one braille sub-pixel of a composed launch sprite —
// a world-frame offset from the vessel anchor (`c.State.R + bodyPos`)
// plus the color the render path should PlotColored at that position.
// No glyph: braille dots are direction-agnostic, so a tilted rocket
// renders smoothly at any pitch without per-glyph rotation
// (v0.11.3 playtest pivot from ASCII glyphs to braille pixels — see
// docs/v0.11-plan.md "Resolved at slice-open").
type SpritePixel struct {
	OffsetWorld orbital.Vec3
	Color       lipgloss.Color
}

// spriteWidthPx is the width of every composed-rocket sprite, in
// braille sub-pixels (1 cell = 2 sub-pixel cols, so 2 px = 1 cell
// wide). Constant across stages — per-stage identity comes from
// height + color, not silhouette width. Narrow is intentional:
// playtest showed wider rockets smear at gravity-turn angles.
const spriteWidthPx = 2

// ComposeLaunchSprite builds the composed-from-stages rocket sprite
// as a list of braille sub-pixels. Stages stack bottom-to-top from
// Stages[0] along the projection of cmdWorld into the chase-cam
// basis (so a gravity-turned rocket leans visibly); each stage
// contributes a `spriteWidthPx × Stage.LaunchSpriteRowsPx` filled
// rectangle of pixels in the stage's catalog color. Returns nil
// when no stage has a non-zero LaunchSpriteRowsPx — caller falls
// back to the vessel's single Glyph render.
//
// Each "pixel" is one braille sub-cell dot (`scaleMPerPx` metres
// across); the canvas's PlotColored accumulates dots per cell and
// renders the resulting braille char.
func ComposeLaunchSprite(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64) []SpritePixel {
	pxSize := scaleMPerPx // one braille sub-pixel = scaleMPerPx world metres
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	// Width axis: perpendicular to stack in screen, rotated −90° so
	// col 0 sits LEFT of col 1 (pinned by
	// TestComposeLaunchSprite_Col0LeftOfCol1).
	widthX, widthY := stackY, -stackX

	var pixels []SpritePixel
	rowOffset := 0
	for _, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		color := lipgloss.Color(s.Color)
		for r := 0; r < s.LaunchSpriteRowsPx; r++ {
			rowAbove := float64(r + rowOffset)
			for col := 0; col < spriteWidthPx; col++ {
				xPx := float64(col) - float64(spriteWidthPx-1)/2.0
				screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
				screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
				offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
				pixels = append(pixels, SpritePixel{
					OffsetWorld: offset,
					Color:       color,
				})
			}
		}
		rowOffset += s.LaunchSpriteRowsPx
	}
	if len(pixels) == 0 {
		return nil
	}
	return pixels
}

// ComposeFlame builds exhaust-flame pixels extending below Stages[0]'s
// base along the -cmdWorld direction. Same `spriteWidthPx` width as
// the rocket sprite. Length-binned by throttle into 3 bins:
//
//   - throttle ≤ 0 or no stages: returns nil.
//   - 0 < throttle ≤ 1/3: 4 sub-pixel rows (1 cell tall).
//   - 1/3 < throttle ≤ 2/3: 8 sub-pixel rows.
//   - 2/3 < throttle:      12 sub-pixel rows.
//
// frameIdx selects one of two pulse offsets — frame B shifts the
// flame down by 1 px so the dot pattern within each cell visibly
// changes between frames at the ~100 ms wall-clock cadence.
// Colour: amber `render.ColorWarning`.
func ComposeFlame(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64, throttle float64, frameIdx int) []SpritePixel {
	if throttle <= 0 || len(stages) == 0 {
		return nil
	}
	var nPx int
	switch {
	case throttle <= 1.0/3.0:
		nPx = 4
	case throttle <= 2.0/3.0:
		nPx = 8
	default:
		nPx = 12
	}

	pxSize := scaleMPerPx
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	widthX, widthY := stackY, -stackX

	// Frame B shifts flame 1 px further from engine base so the cells
	// repaint their braille dot pattern between frames. With nPx
	// already in 4-sub-pixel buckets the 1-px frame shift visibly
	// changes the dot density in each cell at the 100 ms cadence.
	frameShift := 0.0
	if frameIdx%2 == 1 {
		frameShift = 1.0
	}

	pixels := make([]SpritePixel, 0, nPx*spriteWidthPx)
	for row := 0; row < nPx; row++ {
		// row 0 is the topmost flame pixel just below Stages[0]'s
		// base (rowAbove = -1); higher row index = further down
		// (more negative rowAbove).
		rowAbove := -1.0 - float64(row) - frameShift
		for col := 0; col < spriteWidthPx; col++ {
			xPx := float64(col) - float64(spriteWidthPx-1)/2.0
			screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
			screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
			offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
			pixels = append(pixels, SpritePixel{
				OffsetWorld: offset,
				Color:       render.ColorWarning,
			})
		}
	}
	return pixels
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
