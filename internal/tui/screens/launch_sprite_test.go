package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
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

	if got, want := len(pixels), 4*defaultSpriteWidthPx; got != want {
		t.Fatalf("got %d pixels, want %d (rows %d × width %d)", got, want, 4, defaultSpriteWidthPx)
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

	if got, want := len(pixels), 2*4*defaultSpriteWidthPx; got != want {
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
	if got := len(pixels); got != defaultSpriteWidthPx {
		t.Fatalf("got %d pixels for 1 row, want %d", got, defaultSpriteWidthPx)
	}
	// Pixels should span the screen.X range symmetrically: minX
	// must be negative (col 0 is left of centre), maxX positive
	// (col defaultSpriteWidthPx-1 is right of centre).
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
// 8 sub-pixel rows (× defaultSpriteWidthPx wide). All pixels positioned
// below the vessel anchor at vertical climb.
func TestComposeFlame_MidThrottleEmitsMidLength(t *testing.T) {
	stage := spacecraft.Stage{LaunchSpriteRowsPx: 4}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.5, 0, 0)
	if got, want := len(pixels), 8*defaultSpriteWidthPx; got != want {
		t.Fatalf("got %d pixels, want %d (mid bin = 8 rows × %d wide)", got, want, defaultSpriteWidthPx)
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
	pixels := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0, 0, 0)
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
	frameA := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.9, 0, 0)
	frameB := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.9, 1, 0)
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

// TestComposeLaunchSpriteUsesPerStageWidth verifies sub-scope 1 of
// v0.11.5 silhouette polish: each stage paints a rectangle at its own
// Stage.LaunchSpriteWidthPx width. A 4-wide stage emits 4 pixels per
// row; a 2-wide stage emits 2 per row; a mixed-width stack composes
// both honestly.
func TestComposeLaunchSpriteUsesPerStageWidth(t *testing.T) {
	wide := spacecraft.Stage{LaunchSpriteRowsPx: 2, LaunchSpriteWidthPx: 4, Color: "#FF0000"}
	narrow := spacecraft.Stage{LaunchSpriteRowsPx: 2, LaunchSpriteWidthPx: 2, Color: "#00FF00"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{wide, narrow}, cmd, basis, 1.0)
	var wideN, narrowN int
	for _, p := range pixels {
		switch string(p.Color) {
		case "#FF0000":
			wideN++
		case "#00FF00":
			narrowN++
		}
	}
	if got, want := wideN, 2*4; got != want {
		t.Errorf("wide stage pixels = %d, want %d (2 rows × 4 wide)", got, want)
	}
	if got, want := narrowN, 2*2; got != want {
		t.Errorf("narrow stage pixels = %d, want %d (2 rows × 2 wide)", got, want)
	}
}

// TestInterStageTaperFiresAboveThreshold (v0.11.5 sub-scope 2):
// two stages both ≥ taperThreshold rows with different widths produce
// a synthetic 1-row band at width round((lower + upper) / 2) in the
// lower stage's colour, sitting between the stage bodies in the stack.
func TestInterStageTaperFiresAboveThreshold(t *testing.T) {
	lower := spacecraft.Stage{LaunchSpriteRowsPx: 10, LaunchSpriteWidthPx: 4, Color: "#FF0000"}
	upper := spacecraft.Stage{LaunchSpriteRowsPx: 10, LaunchSpriteWidthPx: 2, Color: "#00FF00"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{lower, upper}, cmd, basis, 1.0)
	// Expected counts: lower body = 10×4 = 40, upper body = 10×2 = 20,
	// taper row = round((4+2)/2) = 3 pixels in lower colour.
	const wantTaper = 3
	wantTotal := 10*4 + 10*2 + wantTaper
	if got := len(pixels); got != wantTotal {
		t.Fatalf("got %d total pixels, want %d (40 lower + 20 upper + %d taper)", got, wantTotal, wantTaper)
	}
	// Count lower-colour pixels and find the highest-y lower pixel —
	// that should be a taper-row pixel sitting above the lower body's
	// top.
	lowerN := 0
	maxLowerY := -1e18
	for _, p := range pixels {
		if string(p.Color) == "#FF0000" {
			lowerN++
			if y := p.OffsetWorld.Dot(basis.Y); y > maxLowerY {
				maxLowerY = y
			}
		}
	}
	if got, want := lowerN, 10*4+wantTaper; got != want {
		t.Errorf("lower-colour pixels = %d, want %d (body + taper)", got, want)
	}
	// Lower body's top row is at y = 9; taper row at y = 10. So the
	// max lower-colour y should be at index 10 (the taper row).
	if !(maxLowerY > 9.5) {
		t.Errorf("taper must sit ABOVE the lower stage body; maxLowerY=%v, expect > 9.5", maxLowerY)
	}
}

// TestInterStageTaperSuppressedBelowThreshold (v0.11.5 sub-scope 2):
// when one or both stages fall below taperThreshold rows the boundary
// hard-steps — no synthetic row is inserted.
func TestInterStageTaperSuppressedBelowThreshold(t *testing.T) {
	lower := spacecraft.Stage{LaunchSpriteRowsPx: 5, LaunchSpriteWidthPx: 4, Color: "#FF0000"} // 5 < threshold 6
	upper := spacecraft.Stage{LaunchSpriteRowsPx: 6, LaunchSpriteWidthPx: 2, Color: "#00FF00"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{lower, upper}, cmd, basis, 1.0)
	want := 5*4 + 6*2 // no taper row
	if got := len(pixels); got != want {
		t.Errorf("got %d pixels, want %d (no taper when lower < threshold)", got, want)
	}
}

// TestInterStageTaperColourIsLowerStage (v0.11.5 sub-scope 2):
// the synthetic taper row paints in the LOWER stage's colour, so the
// stack reads "stage transitions UP to a narrower top".
func TestInterStageTaperColourIsLowerStage(t *testing.T) {
	lower := spacecraft.Stage{LaunchSpriteRowsPx: 8, LaunchSpriteWidthPx: 4, Color: "#FF0000"}
	upper := spacecraft.Stage{LaunchSpriteRowsPx: 8, LaunchSpriteWidthPx: 2, Color: "#00FF00"}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{lower, upper}, cmd, basis, 1.0)
	// Find pixels with y == 8 (the row immediately above lower's top
	// row at y=7) — those are taper pixels and must all be lower's red.
	for _, p := range pixels {
		y := p.OffsetWorld.Dot(basis.Y)
		if math.Abs(y-8) < 0.01 {
			if string(p.Color) != "#FF0000" {
				t.Errorf("taper-row pixel at y=8 has colour %q, want lower stage %q", string(p.Color), "#FF0000")
			}
		}
	}
}

// TestEngineBellFlaresBottomStage (v0.11.5 sub-scope 3): a thrust-
// bearing bottom stage with width 2 + rows ≥ 4 emits a 1-row bell
// flare at width 4 in the stage's colour, sitting between the stage
// body and the flame.
func TestEngineBellFlaresBottomStage(t *testing.T) {
	stage := spacecraft.Stage{
		LaunchSpriteRowsPx:  6,
		LaunchSpriteWidthPx: 2,
		Color:               "#FF0000",
		Thrust:              1e6,
	}
	if got, want := EngineBellWidth([]spacecraft.Stage{stage}), 4; got != want {
		t.Errorf("EngineBellWidth = %d, want %d", got, want)
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	bell := ComposeEngineBell([]spacecraft.Stage{stage}, cmd, basis, 1.0)
	if got, want := len(bell), 4; got != want {
		t.Fatalf("bell pixel count = %d, want %d (1 row × 4 wide)", got, want)
	}
	for _, p := range bell {
		if string(p.Color) != "#FF0000" {
			t.Errorf("bell pixel colour = %q, want stage colour %q", string(p.Color), "#FF0000")
		}
		if y := p.OffsetWorld.Dot(basis.Y); !(y < 0) {
			t.Errorf("bell pixel must sit below stage base (y < 0); got %v", y)
		}
	}
}

// TestEngineBellSuppressedNoThrust (v0.11.5 sub-scope 3): a pure-
// monoprop bottom stage (Thrust == 0) gets no bell — phantom nozzles
// don't sprout from RCS tugs.
func TestEngineBellSuppressedNoThrust(t *testing.T) {
	stage := spacecraft.Stage{
		LaunchSpriteRowsPx:  6,
		LaunchSpriteWidthPx: 2,
		Color:               "#FF87D7",
		Thrust:              0,
	}
	if got := EngineBellWidth([]spacecraft.Stage{stage}); got != 0 {
		t.Errorf("EngineBellWidth = %d, want 0 (no thrust ⇒ no bell)", got)
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	if bell := ComposeEngineBell([]spacecraft.Stage{stage}, cmd, basis, 1.0); bell != nil {
		t.Errorf("got %d bell pixels for thrust=0 stage, want nil", len(bell))
	}
}

// TestFlameColorMatchesFuelType (v0.11.5 sub-scope 4): each FuelType
// value maps to its catalog-locked palette hex; the bottom stage's
// FuelType drives every flame pixel's colour.
func TestFlameColorMatchesFuelType(t *testing.T) {
	cases := []struct {
		fuel string
		want string
	}{
		{spacecraft.FuelTypeKerolox, "#FF7A1F"},
		{spacecraft.FuelTypeHydrolox, "#BFEFFF"},
		{spacecraft.FuelTypeHypergolic, "#FFD96A"},
		{spacecraft.FuelTypeSolid, "#FF4500"},
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	for _, c := range cases {
		stage := spacecraft.Stage{LaunchSpriteRowsPx: 4, FuelType: c.fuel}
		pixels := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.5, 0, 0)
		if len(pixels) == 0 {
			t.Errorf("fuel=%q: expected flame pixels, got none", c.fuel)
			continue
		}
		for _, p := range pixels {
			if string(p.Color) != c.want {
				t.Errorf("fuel=%q: pixel colour = %q, want %q", c.fuel, string(p.Color), c.want)
				break
			}
		}
	}
}

// TestFlameFallsBackToWarningOnUnsetFuelType (v0.11.5 sub-scope 4):
// stages without a FuelType (pre-v0.11.5 saves, RCS-tug, custom
// stacks) keep amber `render.ColorWarning` flame — no regression.
func TestFlameFallsBackToWarningOnUnsetFuelType(t *testing.T) {
	stage := spacecraft.Stage{LaunchSpriteRowsPx: 4} // FuelType unset
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.5, 0, 0)
	if len(pixels) == 0 {
		t.Fatal("expected flame pixels, got none")
	}
	for _, p := range pixels {
		if p.Color != render.ColorWarning {
			t.Errorf("unset FuelType: pixel colour = %q, want render.ColorWarning %q", string(p.Color), string(render.ColorWarning))
			break
		}
	}
}

// TestFlameInheritsBellWidth (v0.11.5 sub-scope 3): when bellWidth > 0
// the flame paints at that width (not the stage's own width), so the
// exhaust visibly emerges from the nozzle aperture rather than the
// narrower stage body.
func TestFlameInheritsBellWidth(t *testing.T) {
	stage := spacecraft.Stage{
		LaunchSpriteRowsPx:  6,
		LaunchSpriteWidthPx: 2,
		Color:               "#FF0000",
		Thrust:              1e6,
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	bellWidth := EngineBellWidth([]spacecraft.Stage{stage})
	flame := ComposeFlame([]spacecraft.Stage{stage}, cmd, basis, 1.0, 0.5, 0, bellWidth)
	// Mid throttle ⇒ 8 rows. Width inherits bellWidth (= 4).
	if got, want := len(flame), 8*bellWidth; got != want {
		t.Errorf("flame pixel count = %d, want %d (8 rows × %d wide bell)", got, want, bellWidth)
	}
}

// TestLanderRendersLegsWhenBottomStage (v0.11.5 sub-scope 6): a
// Stages[0] with HasLegs=true emits ≥ 6 sub-pixels splayed about the
// stack axis, with the leg dots straddling the body's width corners
// outward and below the stage base.
func TestLanderRendersLegsWhenBottomStage(t *testing.T) {
	lander := spacecraft.Stage{
		LaunchSpriteRowsPx:  5,
		LaunchSpriteWidthPx: 3,
		Color:               "#5FFF87",
		LaunchSpriteHasLegs: true,
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	legs := ComposeLegs([]spacecraft.Stage{lander}, cmd, basis, 1.0)
	if got := len(legs); got < 6 {
		t.Fatalf("expected ≥ 6 leg sub-pixels, got %d", got)
	}
	var leftN, rightN int
	for _, p := range legs {
		x := p.OffsetWorld.Dot(basis.X)
		y := p.OffsetWorld.Dot(basis.Y)
		if !(y < 0) {
			t.Errorf("leg pixel must sit BELOW stage base (y < 0); got %v", y)
		}
		if x < 0 {
			leftN++
		} else if x > 0 {
			rightN++
		}
		if string(p.Color) != "#5FFF87" {
			t.Errorf("leg colour = %q, want stage colour %q", string(p.Color), "#5FFF87")
		}
	}
	if leftN == 0 || rightN == 0 {
		t.Errorf("legs must straddle stack axis; leftN=%d rightN=%d", leftN, rightN)
	}
	if leftN != rightN {
		t.Errorf("legs must be symmetric about stack axis; leftN=%d rightN=%d", leftN, rightN)
	}
}

// TestLegsSuppressedAboveStagesZero (v0.11.5 sub-scope 6): a stage
// with HasLegs=true that is NOT at Stages[0] gets no legs — only the
// bottom stage's HasLegs flag matters.
func TestLegsSuppressedAboveStagesZero(t *testing.T) {
	booster := spacecraft.Stage{LaunchSpriteRowsPx: 8, LaunchSpriteWidthPx: 4, Color: "#FF8C42"}
	lander := spacecraft.Stage{
		LaunchSpriteRowsPx:  5,
		LaunchSpriteWidthPx: 3,
		Color:               "#5FFF87",
		LaunchSpriteHasLegs: true, // set on upper stage — should be ignored
	}
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	if legs := ComposeLegs([]spacecraft.Stage{booster, lander}, cmd, basis, 1.0); legs != nil {
		t.Errorf("got %d leg pixels for HasLegs upper stage, want nil", len(legs))
	}
}

// TestLegsRotateWithStackDir (v0.11.5 sub-scope 6): under a 45° gravity-
// turn the leg sub-pixels offset along the rotated width-dir basis, not
// the original world basis — so legs lean with the rocket. Verified by
// comparing the same leg's position at vertical vs 45°-tilted cmdWorld.
func TestLegsRotateWithStackDir(t *testing.T) {
	lander := spacecraft.Stage{
		LaunchSpriteRowsPx:  5,
		LaunchSpriteWidthPx: 3,
		Color:               "#5FFF87",
		LaunchSpriteHasLegs: true,
	}
	cmdUp := orbital.Vec3{X: 0, Y: 0, Z: 1}
	cmdTilt := orbital.Vec3{X: 1, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	up := ComposeLegs([]spacecraft.Stage{lander}, cmdUp, basis, 1.0)
	tilt := ComposeLegs([]spacecraft.Stage{lander}, cmdTilt, basis, 1.0)
	if len(up) == 0 || len(tilt) != len(up) {
		t.Fatalf("expected same leg count for both attitudes; up=%d tilt=%d", len(up), len(tilt))
	}
	// At vertical, all leg sub-pixels sit below the corner (y < 0).
	// At 45° tilt, the stack axis swings to (X+Y)/√2 so leg sub-pixels
	// pick up positive-X offset relative to their vertical positions.
	var dxSum float64
	for i := range up {
		dxSum += tilt[i].OffsetWorld.Dot(basis.X) - up[i].OffsetWorld.Dot(basis.X)
	}
	if !(dxSum != 0) {
		t.Errorf("legs failed to rotate with stack direction (dxSum=%v)", dxSum)
	}
}

// TestComposeLaunchSpriteDefaultsToTwoWhenWidthZero verifies the
// pre-v0.11.5 behaviour fallback: a stage with LaunchSpriteWidthPx == 0
// renders at defaultSpriteWidthPx (= 2), so un-catalogued and pre-
// v0.11.5 saves keep their original 2-wide rectangle.
func TestComposeLaunchSpriteDefaultsToTwoWhenWidthZero(t *testing.T) {
	stage := spacecraft.Stage{LaunchSpriteRowsPx: 3, Color: "#FFD93D"} // width unset = 0
	cmd := orbital.Vec3{X: 0, Y: 0, Z: 1}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{stage}, cmd, basis, 1.0)
	if got, want := len(pixels), 3*defaultSpriteWidthPx; got != want {
		t.Errorf("got %d pixels, want %d (3 rows × default %d wide)", got, want, defaultSpriteWidthPx)
	}
}
