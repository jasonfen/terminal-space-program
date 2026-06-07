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
// designdocs/terminal-space-program/v0.11-plan.md "Resolved at slice-open").
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

// flameCoreColor is the hot-core tint painted down the centre of a wide
// enough flame (v0.12 Slice 4). A warm cream — reads as the white-hot
// Mach-diamond core against the fuel-coloured outer plume, but kept a
// step off pure white so it stays distinct from the cold-white RCS puff
// (the white-vs-coloured contrast CONTEXT.md's RCS Puff entry relies on).
const flameCoreColor = lipgloss.Color("#FFEFC2")

// flameCoreWidth returns the sub-pixel width of the hot core for a flame
// row of total width w: zero below 3 (too narrow to resolve a core, so
// the row stays a single fuel colour), else a thin central third. The
// core narrows with the row as the cone tapers toward the tip. v0.12
// Slice 4.
func flameCoreWidth(w int) int {
	if w < 3 {
		return 0
	}
	return (w + 1) / 3
}

// stageSpriteWidthPx resolves the width a stage should render at, with
// the unset-zero fallback to defaultSpriteWidthPx baked in. v0.11.5+.
func stageSpriteWidthPx(s spacecraft.Stage) int {
	if s.LaunchSpriteWidthPx <= 0 {
		return defaultSpriteWidthPx
	}
	return s.LaunchSpriteWidthPx
}

// stageSpriteColor resolves the silhouette colour for a stage:
// LaunchSpriteColor when set, else Color. Decouples slate HUD identity
// (Color) from rocket-body identity (LaunchSpriteColor) so a
// 5-stage Apollo Stack can paint a unified palette without changing
// the per-stage HUD colours. v0.11.5-followup.
func stageSpriteColor(s spacecraft.Stage) lipgloss.Color {
	if s.LaunchSpriteColor != "" {
		return lipgloss.Color(s.LaunchSpriteColor)
	}
	return lipgloss.Color(s.Color)
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

// bellTaperRows is the height (sub-pixel rows) of a flared engine bell.
// v0.11.5 shipped a single flat row; v0.12 Slice 4 tapers the bell over
// 3 rows from the stage throat out to the nozzle mouth for a more
// authentic flare.
const bellTaperRows = 3

// EngineBellRows returns how many sub-pixel rows the engine bell
// occupies below Stages[0]: 0 when suppressed (see EngineBellWidth),
// bellTaperRows when the mouth is at least 2 sub-pixels wider than the
// stage throat (room for a visible flare), else 1 (a flat row — a wide
// stage whose mouth is clamped at bellMaxWidth has no room to taper).
// The flame anchors below this whole stack. v0.12 Slice 4.
func EngineBellRows(stages []spacecraft.Stage) int {
	mouth := EngineBellWidth(stages)
	if mouth == 0 {
		return 0
	}
	throat := stageSpriteWidthPx(stages[0])
	if mouth-throat >= 2 {
		return bellTaperRows
	}
	return 1
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
	color := stageSpriteColor(s)

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

// ComposeEngineBell paints the engine-bell flare below Stages[0]'s base
// in Stages[0]'s colour. v0.12 Slice 4: a multi-row taper (EngineBellRows
// rows) flaring linearly from the stage throat at the top (rowAbove −1,
// nearest the body) out to the mouth width (EngineBellWidth) at the
// bottom (the nozzle exit). A single-row bell (EngineBellRows == 1)
// reproduces the v0.11.5 flat flare at the mouth width. Returns nil when
// the bell is suppressed.
func ComposeEngineBell(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64) []SpritePixel {
	rows := EngineBellRows(stages)
	if rows == 0 {
		return nil
	}
	mouth := EngineBellWidth(stages)
	throat := stageSpriteWidthPx(stages[0])
	pxSize := scaleMPerPx
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	widthX, widthY := stackY, -stackX
	color := stageSpriteColor(stages[0])

	var pixels []SpritePixel
	for r := 0; r < rows; r++ {
		// r = 0 is the top row (rowAbove −1, throat width); r = rows-1 is
		// the bottom (nozzle exit, mouth width). Linear flare in between.
		rowAbove := -1.0 - float64(r)
		w := mouth
		if rows > 1 {
			w = int(math.Round(float64(throat) + float64(mouth-throat)*float64(r)/float64(rows-1)))
		}
		for col := 0; col < w; col++ {
			xPx := float64(col) - float64(w-1)/2.0
			screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
			screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
			offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
			pixels = append(pixels, SpritePixel{
				OffsetWorld: offset,
				Color:       color,
			})
		}
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
		color := stageSpriteColor(s)
		emitRect(rowOffset, s.LaunchSpriteRowsPx, width, color)
		rowOffset += s.LaunchSpriteRowsPx
		// Inter-stage taper (v0.11.5 sub-scope 2): when both adjacent
		// stages have LaunchSpriteRowsPx ≥ taperThreshold, paint a
		// synthetic 1-row band in the lower stage's colour at width
		// round((lower.width + upper.width) / 2). Suppressed at the
		// top of the stack (no upper neighbour). The taper rows do
		// NOT alter Stage.LaunchSpriteRowsPx — they extend the
		// composed stack height by 1 row per boundary.
		if i+1 < len(stages) && taperRowBetween(s, stages[i+1]) > 0 {
			upWidth := stageSpriteWidthPx(stages[i+1])
			taperWidth := (width + upWidth + 1) / 2 // round half-up
			emitRect(rowOffset, 1, taperWidth, color)
			rowOffset++
		}
	}
	if len(pixels) == 0 {
		return nil
	}
	return pixels
}

// Canopy geometry constants (v0.12 Slice 3, ADR 0008). A deployed
// parachute paints a synthetic braille dome above the top stage,
// connected by a short shroud-line gap — reusing the (stack-dir,
// width-dir) basis seam the bell / legs / flame already use so the
// canopy leans with the vessel through any attitude.
const (
	canopyGapRows    = 2  // shroud-line rows between the capsule top and canopy skirt
	canopyRows       = 3  // dome height in sub-pixels
	canopyMinWidth   = 5  // skirt width floor (sub-pixels)
	canopyMaxWidth   = 11 // skirt width cap so a wide stack doesn't get an absurd canopy
	canopyShroudDots = 3  // interpolated dots per shroud line
)

// canopyColor is the canopy tint — a pale off-white that reads as
// fabric against the metal stage bodies and the coloured flame.
const canopyColor = lipgloss.Color("#F5F0E0")

// taperRowBetween returns the number of synthetic inter-stage taper
// rows painted between two vertically-adjacent stages: 1 when both
// clear taperThreshold, else 0. The single source of truth for the
// taper rule, shared by ComposeLaunchSprite (which emits the taper
// band) and composedStackRows (which only needs the height) so the two
// can't drift — if they disagreed, ComposeCanopy would anchor at the
// wrong height. v0.12 Slice 3 (ADR 0008).
func taperRowBetween(lower, upper spacecraft.Stage) int {
	if lower.LaunchSpriteRowsPx >= taperThreshold && upper.LaunchSpriteRowsPx >= taperThreshold {
		return 1
	}
	return 0
}

// composedStackRows returns the total sub-pixel height of the composed
// launch sprite, including inter-stage taper rows — mirrors the
// rowOffset accumulation in ComposeLaunchSprite so ComposeCanopy can
// anchor itself just above the top stage.
func composedStackRows(stages []spacecraft.Stage) int {
	rows := 0
	for i, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		rows += s.LaunchSpriteRowsPx
		if i+1 < len(stages) {
			rows += taperRowBetween(s, stages[i+1])
		}
	}
	return rows
}

// composedStackMaxWidth returns the widest stage body (sub-pixels) among
// the stages that contribute to the composed sprite (LaunchSpriteRowsPx >
// 0), or 0 when none do. Paired with composedStackRows to size the
// sprite's on-screen footprint for the glyph-floor check in
// drawComposedRocket (v0.16). Bell/legs/canopy can extend slightly wider,
// but the body width is what governs whether the silhouette resolves above
// a single cell.
func composedStackMaxWidth(stages []spacecraft.Stage) int {
	maxW := 0
	for _, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		if w := stageSpriteWidthPx(s); w > maxW {
			maxW = w
		}
	}
	return maxW
}

// vesselSpriteBelowCellFloor reports whether the composed launch sprite for
// stages, at the given chase-cam scale (metres per braille pixel — the
// reciprocal of renderScene's canvas SetScale), would render no larger than
// a single terminal cell (canvasCellPxW×canvasCellPxH braille sub-pixels).
// Past that floor the silhouette collapses to one or two lit dots, so the
// caller paints the vessel's glyph instead (v0.16). Returns false when
// there is no composed sprite (rows == 0) or scale is non-positive, leaving
// the existing nil-sprite / guard paths in control. The sub-pixel stride is
// the fixed vesselSubPixelM, so the on-screen footprint is
// rows·vesselSubPixelM / scaleMPerPx braille pixels tall.
func vesselSpriteBelowCellFloor(stages []spacecraft.Stage, scaleMPerPx float64) bool {
	rows := composedStackRows(stages)
	if rows <= 0 || scaleMPerPx <= 0 {
		return false
	}
	pxPerM := 1.0 / scaleMPerPx
	heightPx := float64(rows) * vesselSubPixelM * pxPerM
	widthPx := float64(composedStackMaxWidth(stages)) * vesselSubPixelM * pxPerM
	return heightPx <= canvasCellPxH && widthPx <= canvasCellPxW
}

// ComposeCanopy paints a deployed-parachute canopy above the top stage:
// a 3-row braille dome at canopyWidth, plus two converging shroud lines
// bridging the canopyGapRows gap down to the top stage's shoulders.
// Geometry is defined in the (stack-dir, width-dir) basis so the canopy
// leans with the vessel through gravity turns, like the bell / legs.
// The caller gates this on craft.ChuteState == ChuteDeployed. Returns
// nil when there is no composed stack to sit above. v0.12 Slice 3
// (ADR 0008).
func ComposeCanopy(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64) []SpritePixel {
	totalRows := composedStackRows(stages)
	if totalRows == 0 {
		return nil
	}
	topWidth := stageSpriteWidthPx(stages[len(stages)-1])

	canopyW := topWidth*2 + 3
	if canopyW < canopyMinWidth {
		canopyW = canopyMinWidth
	}
	if canopyW > canopyMaxWidth {
		canopyW = canopyMaxWidth
	}

	pxSize := scaleMPerPx
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	widthX, widthY := stackY, -stackX
	emit := func(rowAbove, xPx float64) SpritePixel {
		screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
		screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
		offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
		return SpritePixel{OffsetWorld: offset, Color: canopyColor}
	}

	baseRow := float64(totalRows + canopyGapRows) // canopy skirt sits here
	var pixels []SpritePixel
	// Dome: narrows toward the top (a rounded canopy) — widest at the
	// skirt (r=0), losing 2 sub-pixels of width per row up.
	for r := 0; r < canopyRows; r++ {
		w := canopyW - 2*r
		if w < 1 {
			w = 1
		}
		rowAbove := baseRow + float64(r)
		for col := 0; col < w; col++ {
			xPx := float64(col) - float64(w-1)/2.0
			pixels = append(pixels, emit(rowAbove, xPx))
		}
	}
	// Shroud lines: two lines from the canopy skirt corners down to the
	// top stage's shoulder corners across the gap.
	skirtX := float64(canopyW-1) / 2.0
	shoulderX := float64(topWidth-1) / 2.0
	for _, sign := range [...]float64{-1, +1} {
		for d := 1; d <= canopyShroudDots; d++ {
			frac := float64(d) / float64(canopyShroudDots+1) // 0<frac<1 along the gap
			rowAbove := baseRow - frac*float64(canopyGapRows)
			xPx := sign * (skirtX + frac*(shoulderX-skirtX))
			pixels = append(pixels, emit(rowAbove, xPx))
		}
	}
	return pixels
}

// flameMaxRows is the cap on flame height in sub-pixel rows
// (v0.11.5-followup). Replaces the pre-followup 12-row max — a tall
// rectangle read as a clunky bar of exhaust; capping at 4 and tapering
// the width (see ComposeFlame) gives a basic-cone silhouette that
// scales by throttle without dominating the canvas.
const flameMaxRows = 4

// ComposeFlame builds exhaust-flame pixels extending below Stages[0]
// along the -cmdWorld direction as a basic cone tapering from the
// nozzle width at the top to half that width at the tip. Width and
// starting row depend on whether the engine bell is rendered:
//
//   - bellWidth > 0: top row paints at bellWidth, starts at
//     rowAbove = -2 (the bell occupies rowAbove = -1).
//   - bellWidth == 0: top row paints at Stages[0]'s resolved width
//     and starts at rowAbove = -1 (un-belled fallback path).
//
// Row count is throttle-binned, capped at flameMaxRows = 4:
//
//   - throttle ≤ 0 or no stages: returns nil.
//   - 0 < throttle ≤ 1/3: 2 sub-pixel rows.
//   - 1/3 < throttle ≤ 2/3: 3 sub-pixel rows.
//   - 2/3 < throttle:      4 sub-pixel rows.
//
// Width tapers linearly across rows from the top (top width) to
// max(1, topWidth/2) at the tip — basic cone shape, centred on the
// stack axis. frameIdx selects one of two pulse offsets — frame B
// shifts the flame 1 sub-pixel further from the engine so the dot
// pattern in each braille cell visibly repaints at the ~100 ms
// wall-clock cadence. Colour comes from Stages[0].FuelType via
// flameColorForFuelType (v0.11.5 sub-scope 4); unset falls back to
// render.ColorWarning.
func ComposeFlame(stages []spacecraft.Stage, cmdWorld orbital.Vec3, basis widgets.Basis, scaleMPerPx float64, throttle float64, frameIdx int, bellWidth int) []SpritePixel {
	if throttle <= 0 || len(stages) == 0 {
		return nil
	}
	var nRows int
	switch {
	case throttle <= 1.0/3.0:
		nRows = 2
	case throttle <= 2.0/3.0:
		nRows = 3
	default:
		nRows = flameMaxRows
	}

	pxSize := scaleMPerPx
	stackX, stackY := stackDirScreen(cmdWorld, basis)
	widthX, widthY := stackY, -stackX
	topWidth := bellWidth
	topRow := -1.0
	if topWidth == 0 {
		// Un-belled: flame attaches directly under the stage base.
		topWidth = stageSpriteWidthPx(stages[0])
	} else {
		// Belled: the flame starts just below the whole bell stack, whose
		// height (1 or bellTaperRows) the bell renderer owns — keep the
		// two in lockstep so the flame doesn't float or overlap the bell.
		topRow = -1.0 - float64(EngineBellRows(stages))
	}
	tipWidth := topWidth / 2
	if tipWidth < 1 {
		tipWidth = 1
	}

	// Frame B shifts flame 1 sub-pixel further from engine base so
	// the cells repaint their braille dot pattern between frames at
	// the 100 ms cadence.
	frameShift := 0.0
	if frameIdx%2 == 1 {
		frameShift = 1.0
	}

	flameColor := flameColorForFuelType(stages[0].FuelType)
	pixels := make([]SpritePixel, 0, nRows*topWidth)
	for r := 0; r < nRows; r++ {
		rowAbove := topRow - float64(r) - frameShift
		var w int
		if nRows <= 1 {
			w = topWidth
		} else {
			frac := float64(r) / float64(nRows-1)
			w = int(math.Round(float64(topWidth)*(1-frac) + float64(tipWidth)*frac))
		}
		if w < 1 {
			w = 1
		}
		// v0.12 Slice 4: two-colour plume — a hot warm-white core down the
		// central flameCoreWidth(w) columns, fuel tint on the edges. The
		// core narrows with the row as the cone tapers, so it reads as a
		// bright spine fading to the tip.
		coreW := flameCoreWidth(w)
		coreLo := (w - coreW) / 2
		for col := 0; col < w; col++ {
			xPx := float64(col) - float64(w-1)/2.0
			screenSX := rowAbove*pxSize*stackX + xPx*pxSize*widthX
			screenSY := rowAbove*pxSize*stackY + xPx*pxSize*widthY
			offset := basis.X.Scale(screenSX).Add(basis.Y.Scale(screenSY))
			color := flameColor
			if coreW > 0 && col >= coreLo && col < coreLo+coreW {
				color = flameCoreColor
			}
			pixels = append(pixels, SpritePixel{
				OffsetWorld: offset,
				Color:       color,
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
