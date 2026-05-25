package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// TestComposeLaunchSprite_VerticalClimbSingleStage is the tracer:
// one stage with LaunchSpriteRowsPx = 4 emits a 2-wide × 4-tall
// rectangle of braille sub-pixels (8 pixels), all with positive
// screen.Y offset (stack extends UP from the vessel anchor at
// vertical climb).
func TestComposeLaunchSprite_VerticalClimbSingleStage(t *testing.T) {
	stage := spacecraft.Stage{
		LaunchSpriteRowsPx: 4,
		Color:              "#FFD93D",
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{stage}, cmd, basis, 1.0)

	if got, want := len(pixels), 4*spriteWidthPx; got != want {
		t.Fatalf("got %d pixels, want %d (rows %d × width %d)", got, want, 4, spriteWidthPx)
	}
	for i, p := range pixels {
		if string(p.Color) != "#FFD93D" {
			t.Errorf("pixel %d color %q, want %q", i, string(p.Color), "#FFD93D")
		}
		if y := p.OffsetWorld.Dot(basis.Y); y < 0 {
			t.Errorf("pixel %d screen.Y=%v; expect ≥ 0 (stack extends up from vessel anchor)", i, y)
		}
	}
}

// TestComposeLaunchSprite_MultiStageStacksBottomToTop verifies the
// Stages[0]=bottom convention: every pixel from the bottom stage
// sits at lower screen.Y than every pixel from the top stage.
func TestComposeLaunchSprite_MultiStageStacksBottomToTop(t *testing.T) {
	bottom := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#FF0000"}
	top := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#00FF00"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{bottom, top}, cmd, basis, 1.0)

	if got, want := len(pixels), 2*4*spriteWidthPx; got != want {
		t.Fatalf("got %d pixels, want %d", got, want)
	}

	maxBottomY := -1e18
	minTopY := 1e18
	for _, p := range pixels {
		y := p.OffsetWorld.Dot(basis.Y)
		switch string(p.Color) {
		case "#FF0000":
			if y > maxBottomY {
				maxBottomY = y
			}
		case "#00FF00":
			if y < minTopY {
				minTopY = y
			}
		default:
			t.Errorf("unexpected color %q on pixel", string(p.Color))
		}
	}
	if !(minTopY > maxBottomY) {
		t.Errorf("top-stage pixels must be above bottom-stage pixels; maxBottomY=%v minTopY=%v", maxBottomY, minTopY)
	}
}

// TestComposeLaunchSprite_FortyFiveDegreePitchTiltsStack verifies the
// gravity-turn lean: with cmdWorld at 45° between hAxis and localUp,
// the top of the stack gains screen.X offset in the lean direction
// while still rising in screen.Y. Braille pixels are direction-
// agnostic so this works smoothly at any pitch — no glyph rotation.
func TestComposeLaunchSprite_FortyFiveDegreePitchTiltsStack(t *testing.T) {
	bottom := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#FF0000"}
	top := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#00FF00"}
	cmd := orbital.Vec3{X: 1, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{bottom, top}, cmd, basis, 1.0)

	var bottomXSum, bottomYSum, topXSum, topYSum float64
	var bottomN, topN int
	for _, p := range pixels {
		x := p.OffsetWorld.Dot(basis.X)
		y := p.OffsetWorld.Dot(basis.Y)
		switch string(p.Color) {
		case "#FF0000":
			bottomXSum += x
			bottomYSum += y
			bottomN++
		case "#00FF00":
			topXSum += x
			topYSum += y
			topN++
		}
	}
	bottomCentreX := bottomXSum / float64(bottomN)
	bottomCentreY := bottomYSum / float64(bottomN)
	topCentreX := topXSum / float64(topN)
	topCentreY := topYSum / float64(topN)

	if !(topCentreY > bottomCentreY) {
		t.Errorf("top stage must rise above bottom; bottomY=%v topY=%v", bottomCentreY, topCentreY)
	}
	if !(topCentreX > bottomCentreX) {
		t.Errorf("top stage must lean toward hAxis (cmd has +hAxis component); bottomX=%v topX=%v", bottomCentreX, topCentreX)
	}
	// At 45° pitch the stack-screen direction is equal in X and Y,
	// so the centre-to-centre delta along X equals the delta along Y.
	dx := topCentreX - bottomCentreX
	dy := topCentreY - bottomCentreY
	if math.Abs(dx-dy) > 1e-9 {
		t.Errorf("at 45° pitch dx == dy expected; dx=%v dy=%v", dx, dy)
	}
}

// TestComposeLaunchSprite_Col0LeftOfCol1 pins the screen handedness:
// for a 2-px-wide sprite at vertical climb, the col 0 pixels sit to
// the LEFT of the col 1 pixels in screen. Pinned because the
// width-axis sign was flipped in v0.11.3 first commit (cap mirror
// bug); the braille rewrite preserves the corrected handedness.
func TestComposeLaunchSprite_Col0LeftOfCol1(t *testing.T) {
	stage := spacecraft.Stage{
		LaunchSpriteRowsPx: 1, // single row to isolate width effect
		Color:              "#FFD93D",
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{stage}, cmd, basis, 1.0)
	if got := len(pixels); got != spriteWidthPx {
		t.Fatalf("got %d pixels for 1 row, want %d", got, spriteWidthPx)
	}
	// Pixels should span the screen.X range symmetrically: minX
	// must be negative (col 0 is left of centre), maxX positive
	// (col spriteWidthPx-1 is right of centre).
	minX, maxX := 1e18, -1e18
	for _, p := range pixels {
		x := p.OffsetWorld.Dot(basis.X)
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
	}
	if !(minX < 0 && maxX > 0) {
		t.Errorf("expected pixels straddling screen.X=0 (col 0 left, col N-1 right); minX=%v maxX=%v", minX, maxX)
	}
}

// TestComposeLaunchSprite_ZeroRowsSkipped verifies a stage with
// LaunchSpriteRowsPx = 0 emits no pixels (the "no sprite" sentinel).
func TestComposeLaunchSprite_ZeroRowsSkipped(t *testing.T) {
	stage := spacecraft.Stage{LaunchSpriteRowsPx: 0, Color: "#FFD93D"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{stage}, cmd, basis, 1.0)
	if pixels != nil {
		t.Errorf("got %d pixels, want nil (LaunchSpriteRowsPx == 0)", len(pixels))
	}
}

// TestComposeLaunchSprite_NoSpriteReturnsNil verifies the fallback
// contract: when no stage carries a non-zero LaunchSpriteRowsPx the
// function returns nil so the render path falls back to the
// vessel-level single Glyph.
func TestComposeLaunchSprite_NoSpriteReturnsNil(t *testing.T) {
	stages := []spacecraft.Stage{
		{LaunchSpriteRowsPx: 0, Color: "#FFD93D"},
		{LaunchSpriteRowsPx: 0, Color: "#FFD93D"},
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite(stages, cmd, basis, 1.0)
	if pixels != nil {
		t.Errorf("got %d pixels, want nil (no stage has LaunchSpriteRowsPx)", len(pixels))
	}
}

// TestComposeFlame_MidThrottleEmitsMidLength verifies the flame
// length-binning: throttle 0.5 falls in the middle bin and emits
// 8 sub-pixel rows (× spriteWidthPx wide). All pixels positioned
// below the vessel anchor at vertical climb.
func TestComposeFlame_MidThrottleEmitsMidLength(t *testing.T) {
	stage := spacecraft.Stage{LaunchSpriteRowsPx: 4}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.5, 0)
	if got, want := len(pixels), 8*spriteWidthPx; got != want {
		t.Fatalf("got %d pixels, want %d (mid bin = 8 rows × %d wide)", got, want, spriteWidthPx)
	}
	for i, p := range pixels {
		if y := p.OffsetWorld.Dot(basis.Y); y >= 0 {
			t.Errorf("flame pixel %d screen.Y=%v; expect < 0 (below vessel along -cmd)", i, y)
		}
	}
}

// TestComposeFlame_ZeroThrottleReturnsNil verifies the no-burn case:
// throttle 0 emits no flame pixels.
func TestComposeFlame_ZeroThrottleReturnsNil(t *testing.T) {
	stage := spacecraft.Stage{LaunchSpriteRowsPx: 4}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0, 0)
	if pixels != nil {
		t.Errorf("got %d pixels at throttle 0, want nil", len(pixels))
	}
}

// TestComposeFlame_FrameSwapShiftsPixels verifies the 2-frame pulse:
// frame A and frame B at the same throttle emit pixels at different
// screen.Y positions (frame B is shifted 1 px further from the
// engine base), so the braille dot pattern in each cell visibly
// repaints between frames.
func TestComposeFlame_FrameSwapShiftsPixels(t *testing.T) {
	stage := spacecraft.Stage{LaunchSpriteRowsPx: 4}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	frameA := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.9, 0)
	frameB := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.9, 1)
	if len(frameA) == 0 || len(frameB) == 0 {
		t.Fatalf("expected non-empty frames; A=%d B=%d", len(frameA), len(frameB))
	}
	// Same pixel index in each frame must differ in screen.Y (frame
	// B is shifted by 1 sub-pixel along -cmd, i.e. more negative Y).
	for i := range frameA {
		yA := frameA[i].OffsetWorld.Dot(basis.Y)
		yB := frameB[i].OffsetWorld.Dot(basis.Y)
		if !(yB < yA) {
			t.Errorf("frame B pixel %d must be lower (more negative Y) than frame A; yA=%v yB=%v", i, yA, yB)
		}
	}
}
