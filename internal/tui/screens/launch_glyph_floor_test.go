package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestVesselSpriteBelowCellFloor — v0.16. As the chase-cam autozoom grows
// (larger metres-per-pixel), the fixed-metre launch sprite shrinks until
// the whole silhouette fits inside one terminal cell. Past that point the
// caller must fall back to the glyph rather than render a sub-cell braille
// dot. The floor compares the on-screen footprint
// (rows·vesselSubPixelM / scale braille px) against a cell
// (canvasCellPxW×canvasCellPxH).
func TestVesselSpriteBelowCellFloor(t *testing.T) {
	// A 10-row, 2-wide stack: 10·1.5 = 15 m tall, 2·1.5 = 3 m wide.
	stages := []spacecraft.Stage{
		{LaunchSpriteRowsPx: 10, LaunchSpriteWidthPx: 2, Color: "#FFFFFF"},
	}

	// Tight zoom: 0.5 m/px → 30 px tall × 6 px wide, far above one cell.
	if vesselSpriteBelowCellFloor(stages, 0.5) {
		t.Error("at 0.5 m/px the sprite is 30px tall — should NOT floor to glyph")
	}

	// Far zoom: 10 m/px → height 15/10 = 1.5 px, width 3/10 = 0.3 px, both
	// inside one cell (4×2) → floor to glyph.
	if !vesselSpriteBelowCellFloor(stages, 10.0) {
		t.Error("at 10 m/px the sprite collapses under one cell — should floor to glyph")
	}

	// Exactly at the height boundary: height == canvasCellPxH (4 px),
	// width <= canvasCellPxW. scale where 15/scale == 4 → scale = 3.75.
	// The "<=" floor means at-or-below-one-cell uses the glyph.
	if !vesselSpriteBelowCellFloor(stages, 15.0/float64(canvasCellPxH)) {
		t.Error("at exactly one cell tall, the floor should still prefer the glyph (<=)")
	}

	// No composed sprite (rows == 0): never floors here — the nil-sprite
	// path in drawComposedRocket handles the glyph fallback instead.
	if vesselSpriteBelowCellFloor([]spacecraft.Stage{{LaunchSpriteRowsPx: 0}}, 1000.0) {
		t.Error("a spriteless stack should return false (handled by the nil-sprite path)")
	}

	// Non-positive scale guards against div-by-zero / garbage.
	if vesselSpriteBelowCellFloor(stages, 0) {
		t.Error("non-positive scale should return false, not floor")
	}
}
