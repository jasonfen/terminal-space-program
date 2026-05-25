package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// TestComposeLaunchSprite_VerticalClimbSingleStage is the tracer:
// one 2-row × 2-col stage with vertical-climb basis emits 4 cells
// stacked along the basis Y direction (localUp), with the bottom row
// anchored at the vessel (screen.Y == 0) and the top row above it.
func TestComposeLaunchSprite_VerticalClimbSingleStage(t *testing.T) {
	stage := spacecraft.Stage{
		LaunchSprite: "║║\n║║",
		Color:        "#FFD93D",
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	cells := ComposeLaunchSprite([]spacecraft.Stage{stage}, cmd, basis, 1.0)

	if got := len(cells); got != 4 {
		t.Fatalf("got %d cells, want 4 (2 rows × 2 cols)", got)
	}

	for i, c := range cells {
		if c.Glyph != '║' {
			t.Errorf("cell %d: glyph %q, want '║'", i, c.Glyph)
		}
		if string(c.Color) != "#FFD93D" {
			t.Errorf("cell %d: color %q, want %q", i, string(c.Color), "#FFD93D")
		}
	}

	yCounts := map[float64]int{}
	for _, c := range cells {
		y := c.OffsetWorld.Dot(basis.Y)
		yCounts[y]++
	}
	if len(yCounts) != 2 {
		t.Fatalf("got %d distinct screen.Y values, want 2 (one row each); yCounts=%v", len(yCounts), yCounts)
	}
	for y, n := range yCounts {
		if n != 2 {
			t.Errorf("screen.Y=%v has %d cells, want 2 (2-col stack)", y, n)
		}
	}
	if _, ok := yCounts[0]; !ok {
		t.Errorf("expected a row at screen.Y=0 (bottom-of-stack anchored at vessel); yCounts=%v", yCounts)
	}
}

// TestComposeLaunchSprite_MultiStageStacksBottomToTop verifies the
// Stages[0]=bottom convention: at vertical climb, every cell from the
// bottom stage sits at lower screen.Y than every cell from the top
// stage.
func TestComposeLaunchSprite_MultiStageStacksBottomToTop(t *testing.T) {
	bottom := spacecraft.Stage{
		LaunchSprite: "║║",
		Color:        "#FFD93D",
	}
	top := spacecraft.Stage{
		LaunchSprite: "▓▓",
		Color:        "#FFD93D",
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	cells := ComposeLaunchSprite([]spacecraft.Stage{bottom, top}, cmd, basis, 1.0)
	if got := len(cells); got != 4 {
		t.Fatalf("got %d cells, want 4 (one 2-col row per stage)", got)
	}

	maxBottomY := -1.0
	minTopY := 1e18
	for _, c := range cells {
		y := c.OffsetWorld.Dot(basis.Y)
		switch c.Glyph {
		case '║':
			if y > maxBottomY {
				maxBottomY = y
			}
		case '▓':
			if y < minTopY {
				minTopY = y
			}
		default:
			t.Errorf("unexpected glyph %q", c.Glyph)
		}
	}
	if !(minTopY > maxBottomY) {
		t.Errorf("top-stage cells must be above bottom-stage cells; maxBottomY=%v minTopY=%v", maxBottomY, minTopY)
	}
}

// TestComposeLaunchSprite_FortyFiveDegreePitchTiltsStack verifies the
// gravity-turn lean: when cmdWorld has a horizontal component, the
// top stage's cells gain screen.X offset in the lean direction.
//
// Setup: cmd at 45° between hAxis and localUp; stack axis projects
// into screen as (√2/2, √2/2). Bottom stage stays near the anchor,
// top stage shifts both up *and* sideways.
func TestComposeLaunchSprite_FortyFiveDegreePitchTiltsStack(t *testing.T) {
	bottom := spacecraft.Stage{LaunchSprite: "║║", Color: "#FFD93D"}
	top := spacecraft.Stage{LaunchSprite: "▓▓", Color: "#FFD93D"}
	cmd := orbital.Vec3{X: 1, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	cells := ComposeLaunchSprite([]spacecraft.Stage{bottom, top}, cmd, basis, 1.0)
	if got := len(cells); got != 4 {
		t.Fatalf("got %d cells, want 4", got)
	}

	var bottomXSum, bottomYSum, topXSum, topYSum float64
	var bottomN, topN int
	for _, c := range cells {
		x := c.OffsetWorld.Dot(basis.X)
		y := c.OffsetWorld.Dot(basis.Y)
		switch c.Glyph {
		case '║':
			bottomXSum += x
			bottomYSum += y
			bottomN++
		case '▓':
			topXSum += x
			topYSum += y
			topN++
		}
	}
	bottomCentreX := bottomXSum / float64(bottomN)
	bottomCentreY := bottomYSum / float64(bottomN)
	topCentreX := topXSum / float64(topN)
	topCentreY := topYSum / float64(topN)

	// Top stage above bottom.
	if !(topCentreY > bottomCentreY) {
		t.Errorf("top stage must be higher than bottom; bottomY=%v topY=%v", bottomCentreY, topCentreY)
	}
	// Top stage offset toward +hAxis (the gravity-turn lean direction).
	if !(topCentreX > bottomCentreX) {
		t.Errorf("top stage must lean toward hAxis (cmd has +hAxis component); bottomX=%v topX=%v", bottomCentreX, topCentreX)
	}
	// Lean magnitude: at 45° pitch, the stack-axis projection in
	// screen is equal in X and Y, so topCentreX - bottomCentreX ≈
	// topCentreY - bottomCentreY within float noise.
	dx := topCentreX - bottomCentreX
	dy := topCentreY - bottomCentreY
	if math.Abs(dx-dy) > 1e-9 {
		t.Errorf("at 45° pitch dx == dy expected; dx=%v dy=%v", dx, dy)
	}
}

// TestComposeLaunchSprite_NullRuneSkipped verifies the LUT-precedent
// convention that a '\x00' (zero rune) in a sprite cell means "no
// glyph at this position" — used for sparse outlines like the LUT's
// crown row.
func TestComposeLaunchSprite_NullRuneSkipped(t *testing.T) {
	stage := spacecraft.Stage{
		LaunchSprite: "║\x00",
		Color:        "#FFD93D",
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	cells := ComposeLaunchSprite([]spacecraft.Stage{stage}, cmd, basis, 1.0)
	if got := len(cells); got != 1 {
		t.Fatalf("got %d cells, want 1 (col 1 was \\x00, should be skipped)", got)
	}
	if cells[0].Glyph != '║' {
		t.Errorf("cell glyph %q, want '║'", cells[0].Glyph)
	}
}

// TestComposeLaunchSprite_NoSpriteReturnsNil verifies the fallback
// contract: when no stage in the vessel has LaunchSprite set, the
// function returns nil so the render path falls back to the
// vessel-level single Glyph.
func TestComposeLaunchSprite_NoSpriteReturnsNil(t *testing.T) {
	stages := []spacecraft.Stage{
		{LaunchSprite: "", Color: "#FFD93D"},
		{LaunchSprite: "", Color: "#FFD93D"},
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	cells := ComposeLaunchSprite(stages, cmd, basis, 1.0)
	if cells != nil {
		t.Errorf("got %d cells, want nil (no stage has LaunchSprite set)", len(cells))
	}
}

// TestComposeFlame_MidThrottleEmitsTwoRows is the flame tracer:
// throttle 0.5 falls in the middle bin and emits 2 rows × 2 cols = 4
// cells, all positioned below the vessel anchor (negative screen.Y at
// vertical climb, the -cmd direction).
func TestComposeFlame_MidThrottleEmitsTwoRows(t *testing.T) {
	stage := spacecraft.Stage{LaunchSprite: "║║"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	cells := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.5, 0)
	if got := len(cells); got != 4 {
		t.Fatalf("got %d cells, want 4 (throttle 0.5 = mid bin = 2 rows × 2 cols)", got)
	}
	for i, c := range cells {
		y := c.OffsetWorld.Dot(basis.Y)
		if y >= 0 {
			t.Errorf("flame cell %d at screen.Y=%v; expect < 0 (below vessel along -cmd)", i, y)
		}
	}
}

// TestComposeFlame_ZeroThrottleReturnsNil verifies the no-burn case:
// throttle 0 emits no flame cells (the engine isn't firing).
func TestComposeFlame_ZeroThrottleReturnsNil(t *testing.T) {
	stage := spacecraft.Stage{LaunchSprite: "║║"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	cells := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0, 0)
	if cells != nil {
		t.Errorf("got %d cells at throttle 0, want nil", len(cells))
	}
}

// TestComposeFlame_FrameSwapDiffersInGlyph verifies the 2-frame pulse:
// at high throttle, frame 0's top row uses '▓' and frame 1's top row
// uses '█'. The caller drives frameIdx from wall-clock time.
func TestComposeFlame_FrameSwapDiffersInGlyph(t *testing.T) {
	stage := spacecraft.Stage{LaunchSprite: "║║"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	frameA := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.9, 0)
	frameB := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.9, 1)
	if len(frameA) != 6 || len(frameB) != 6 {
		t.Fatalf("expected 6 cells per frame at high throttle; got A=%d B=%d", len(frameA), len(frameB))
	}
	// Pick the topmost (closest to engine) flame row by max screen.Y.
	topGlyph := func(cs []SpriteCell) rune {
		maxY := -1e18
		var g rune
		for _, c := range cs {
			y := c.OffsetWorld.Dot(basis.Y)
			if y > maxY {
				maxY = y
				g = c.Glyph
			}
		}
		return g
	}
	if got := topGlyph(frameA); got != '▓' {
		t.Errorf("frame A top glyph %q, want '▓'", got)
	}
	if got := topGlyph(frameB); got != '█' {
		t.Errorf("frame B top glyph %q, want '█'", got)
	}
}
