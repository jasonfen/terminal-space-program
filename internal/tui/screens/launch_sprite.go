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

// defaultSpriteWidthPx is the fallback width used by stages whose
// LaunchSpriteWidthPx is zero (un-catalogued stages, pre-v0.11.5 saves).
// 1 cell = 2 sub-pixel cols, so 2 px = 1 cell wide — the pre-v0.11.5
// universal constant. Per-stage width comes from Stage.LaunchSpriteWidthPx
// (v0.11.5+).
const defaultSpriteWidthPx = 2

// Flame palette (v0.11.5 sub-scope 4). Bottom-stage FuelType drives
// the flame tint; throttle still drives flame length (length-binned
// in ComposeFlame). Unset/empty FuelType falls back to amber
// ColorWarning so un-catalogued and pre-v0.11.5 stages keep their
// original colour. Hex values eyeballed against the existing catalog
// stage colours so e.g. hypergolic doesn't clash with S-IVB's
// `#FFD93D` body fill.
var fuelTypeFlameColor = map[string]lipgloss.Color{
	spacecraft.FuelTypeKerolox:    lipgloss.Color("#FF7A1F"), // orange (F-1, Merlin)
	spacecraft.FuelTypeHydrolox:   lipgloss.Color("#BFEFFF"), // pale cyan (J-2, RS-25, RL-10)
	spacecraft.FuelTypeHypergolic: lipgloss.Color("#FFD96A"), // yellow-amber (LM, SPS)
	spacecraft.FuelTypeSolid:      lipgloss.Color("#FF4500"), // orange-red (SLS SRB)
}

// flameColorForFuelType returns the flame tint for the given FuelType,
// falling back to render.ColorWarning when unset or unknown. v0.11.5.
func flameColorForFuelType(fuel string) lipgloss.Color {
	if c, ok := fuelTypeFlameColor[fuel]; ok {
		return c
	}
	return render.ColorWarning
}

// stageSpriteWidthPx resolves the width a stage should render at, with
// the unset-zero fallback to defaultSpriteWidthPx baked in. v0.11.5+.
func stageSpriteWidthPx(s spacecraft.Stage) int {
	if s.LaunchSpriteWidthPx <= 0 {
		return defaultSpriteWidthPx
	}
	return s.LaunchSpriteWidthPx
}

// taperThreshold (v0.11.5) is the minimum LaunchSpriteRowsPx on BOTH
// adjacent stages for an inter-stage boundary to grow a synthetic
// 1-row taper. Below the threshold the boundary hard-steps — the
// catalog author opts in by sizing stages tall enough that taper is
// the natural read of their transition.
const taperThreshold = 6

// Engine-bell gates (v0.11.5 sub-scope 3). The bell is a synthetic
// 1-row flare between Stages[0]'s base and the flame, persistent
// regardless of throttle — it represents nozzle hardware. Bell width
// = min(stages[0].width + 2, bellMaxWidth). Suppressed for tiny
// stages or pure-monoprop bottom stages so an RCS-tug doesn't sprout
// a phantom main-engine nozzle.
const (
	bellMinStageWidth = 2 // Stages[0] LaunchSpriteWidthPx ≥ this for bell
	bellMinStageRows  = 4 // Stages[0] LaunchSpriteRowsPx ≥ this for bell
	bellExtraWidth    = 2 // bell width = stage width + this (clamped)
	bellMaxWidth      = 7 // hard cap so a 5-wide booster doesn't get a 7-wide bell
)

// EngineBellWidth returns the bell width (in sub-pixels) for the
// given stack, or 0 when the bell is suppressed by the gates above.
// v0.11.5 sub-scope 3.
func EngineBellWidth(stages []spacecraft.Stage) int {
	if len(stages) == 0 {
		return 0
	}
	s := stages[0]
	if s.Thrust <= 0 {
		return 0
	}
	if s.LaunchSpriteRowsPx < bellMinStageRows {
		return 0
	}
	width := stageSpriteWidthPx(s)
	if width < bellMinStageWidth {
		return 0
	}
	w := width + bellExtraWidth
	if w > bellMaxWidth {
		w = bellMaxWidth
	}
	return w
}

// Landing-leg geometry constants (v0.11.5 sub-scope 6). Legs splay
// outward by legSpreadX sub-pixels along the width axis and downward
// by legSpreadY sub-pixels along the negative stack axis from each
// of Stages[0]'s bottom corners. Mirrored about the stack axis so a
// width-3 stage gets symmetric legs from (xPx=-1, row=0) and
// (xPx=+1, row=0). The foot pad is one extra sub-pixel beyond the
// leg's foot to give a visible base.
const (
	legSpreadX = 2 // outward in width axis (sub-pixels)
	legSpreadY = 3 // downward in stack axis (sub-pixels)
	legNDots   = 4 // interpolated dots along each leg, foot inclusive
)

// ComposeLegs paints landing-leg sub-pixels for Stages[0] when
// LaunchSpriteHasLegs is set, returning nil otherwise. Legs sit
// around the engine bell — bell occupies the centre, legs splay to
// the sides — so the exhaust visually fires through the gap between
// them. Painted in Stages[0].Color, geometry defined in the
// (stack-dir, width-dir) basis so legs rotate with the rocket
// through gravity turns. v0.11.5 sub-scope 6.
func ComposeLegs(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64) []SpritePixel {
	if len(stages) == 0 || !stages[0].LaunchSpriteHasLegs {
		return nil
	}
	s := stages[0]
	width := stageSpriteWidthPx(s)
	pxSize := scaleMPerPx
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	widthX, widthY := stackY, -stackX
	color := lipgloss.Color(s.Color)

	emit := func(rowAbove, xPx float64) SpritePixel {
		screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
		screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
		offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
		return SpritePixel{OffsetWorld: offset, Color: color}
	}

	pixels := make([]SpritePixel, 0, 2*(legNDots+1))
	for _, sign := range [...]float64{-1, +1} {
		xCorner := sign * float64(width-1) / 2.0
		// Leg dots: nDots evenly spaced along the leg from the corner
		// (d=1) out to the foot (d=legNDots).
		for d := 1; d <= legNDots; d++ {
			frac := float64(d) / float64(legNDots)
			xPx := xCorner + sign*float64(legSpreadX)*frac
			rowAbove := -float64(legSpreadY) * frac
			pixels = append(pixels, emit(rowAbove, xPx))
		}
		// Foot pad: 1 extra sub-pixel further outward at the foot's
		// row level — gives a visible 2-px foot rather than a leg
		// tapering to a single point.
		footX := xCorner + sign*float64(legSpreadX+1)
		footY := -float64(legSpreadY)
		pixels = append(pixels, emit(footY, footX))
	}
	return pixels
}

// ComposeEngineBell paints the 1-row engine-bell flare below
// Stages[0]'s base in Stages[0].Color, at width EngineBellWidth(stages).
// Returns nil when the bell is suppressed. v0.11.5 sub-scope 3.
func ComposeEngineBell(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64) []SpritePixel {
	bw := EngineBellWidth(stages)
	if bw == 0 {
		return nil
	}
	pxSize := scaleMPerPx
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	widthX, widthY := stackY, -stackX
	color := lipgloss.Color(stages[0].Color)
	rowAbove := -1.0 // sits 1 row below the stage's base
	pixels := make([]SpritePixel, 0, bw)
	for col := 0; col < bw; col++ {
		xPx := float64(col) - float64(bw-1)/2.0
		screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
		screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
		offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
		pixels = append(pixels, SpritePixel{
			OffsetWorld: offset,
			Color:       color,
		})
	}
	return pixels
}

// ComposeLaunchSprite builds the composed-from-stages rocket sprite
// as a list of braille sub-pixels. Stages stack bottom-to-top from
// Stages[0] along the projection of cmdWorld into the chase-cam
// basis (so a gravity-turned rocket leans visibly); each stage
// contributes a `width × Stage.LaunchSpriteRowsPx` filled
// rectangle of pixels in the stage's catalog color, where width
// resolves per-stage from Stage.LaunchSpriteWidthPx (v0.11.5+;
// zero falls back to defaultSpriteWidthPx). Returns nil when no
// stage has a non-zero LaunchSpriteRowsPx — caller falls back to
// the vessel's single Glyph render.
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
	emitRect := func(rowsBase int, rows, width int, color lipgloss.Color) {
		for r := 0; r < rows; r++ {
			rowAbove := float64(r + rowsBase)
			for col := 0; col < width; col++ {
				xPx := float64(col) - float64(width-1)/2.0
				screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
				screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
				offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
				pixels = append(pixels, SpritePixel{
					OffsetWorld: offset,
					Color:       color,
				})
			}
		}
	}
	for i, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		width := stageSpriteWidthPx(s)
		color := lipgloss.Color(s.Color)
		emitRect(rowOffset, s.LaunchSpriteRowsPx, width, color)
		rowOffset += s.LaunchSpriteRowsPx
		// Inter-stage taper (v0.11.5 sub-scope 2): when both adjacent
		// stages have LaunchSpriteRowsPx ≥ taperThreshold, paint a
		// synthetic 1-row band in the lower stage's colour at width
		// round((lower.width + upper.width) / 2). Suppressed at the
		// top of the stack (no upper neighbour). The taper rows do
		// NOT alter Stage.LaunchSpriteRowsPx — they extend the
		// composed stack height by 1 row per boundary.
		if i+1 < len(stages) {
			up := stages[i+1]
			if up.LaunchSpriteRowsPx >= taperThreshold && s.LaunchSpriteRowsPx >= taperThreshold {
				upWidth := stageSpriteWidthPx(up)
				taperWidth := (width + upWidth + 1) / 2 // round half-up
				emitRect(rowOffset, 1, taperWidth, color)
				rowOffset++
			}
		}
	}
	if len(pixels) == 0 {
		return nil
	}
	return pixels
}

// ComposeFlame builds exhaust-flame pixels extending below Stages[0]
// along the -cmdWorld direction. Width and starting row depend on
// whether the engine bell is rendered:
//
//   - bellWidth > 0: flame uses bellWidth and starts at rowAbove = -2
//     (the bell occupies rowAbove = -1, so exhaust emerges from
//     beneath the nozzle).
//   - bellWidth == 0: flame uses Stages[0]'s resolved width and starts
//     at rowAbove = -1 (pre-v0.11.5 behaviour for un-belled stacks).
//
// Length-binned by throttle into 3 bins:
//
//   - throttle ≤ 0 or no stages: returns nil.
//   - 0 < throttle ≤ 1/3: 4 sub-pixel rows (1 cell tall).
//   - 1/3 < throttle ≤ 2/3: 8 sub-pixel rows.
//   - 2/3 < throttle:      12 sub-pixel rows.
//
// frameIdx selects one of two pulse offsets — frame B shifts the
// flame down by 1 px so the dot pattern within each cell visibly
// changes between frames at the ~100 ms wall-clock cadence.
// Colour: amber `render.ColorWarning` (v0.11.5 sub-scope 4 swaps in
// fuel-type tints — see ComposeFlameTinted).
func ComposeFlame(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64, throttle float64, frameIdx int, bellWidth int) []SpritePixel {
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
	flameWidth := bellWidth
	topRow := -2.0 // bell sits at -1; flame starts at -2
	if flameWidth == 0 {
		flameWidth = stageSpriteWidthPx(stages[0])
		topRow = -1.0
	}

	// Frame B shifts flame 1 px further from engine base so the cells
	// repaint their braille dot pattern between frames. With nPx
	// already in 4-sub-pixel buckets the 1-px frame shift visibly
	// changes the dot density in each cell at the 100 ms cadence.
	frameShift := 0.0
	if frameIdx%2 == 1 {
		frameShift = 1.0
	}

	flameColor := flameColorForFuelType(stages[0].FuelType)
	pixels := make([]SpritePixel, 0, nPx*flameWidth)
	for row := 0; row < nPx; row++ {
		rowAbove := topRow - float64(row) - frameShift
		for col := 0; col < flameWidth; col++ {
			xPx := float64(col) - float64(flameWidth-1)/2.0
			screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
			screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
			offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
			pixels = append(pixels, SpritePixel{
				OffsetWorld: offset,
				Color:       flameColor,
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
