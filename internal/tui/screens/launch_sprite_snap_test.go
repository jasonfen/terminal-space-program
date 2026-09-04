package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// angledCmd returns a unit cmdWorld vector tilted degOffVertical
// degrees off basis.Y (screen-vertical) toward basis.X — the probe
// vector every test in this file uses to sweep across the #424
// near-vertical snap threshold.
func angledCmd(degOffVertical float64) orbital.Vec3 {
	rad := degOffVertical * math.Pi / 180
	return orbital.Vec3{X: math.Sin(rad), Y: 0, Z: math.Cos(rad)}
}

// TestSnapNearVertical_WithinToleranceSnapsExact pins the ADR 0048 §4
// tolerance ("a couple of degrees"): 0° and 1.5° off vertical both
// snap to exactly (0, 1).
func TestSnapNearVertical_WithinToleranceSnapsExact(t *testing.T) {
	for _, deg := range []float64{0, 1.5} {
		rad := deg * math.Pi / 180
		x, y := snapNearVertical(math.Sin(rad), math.Cos(rad))
		if x != 0 || y != 1 {
			t.Errorf("at %.1f° off vertical: snapNearVertical = (%v, %v), want (0, 1)", deg, x, y)
		}
	}
}

// TestSnapNearVertical_BeyondToleranceKeepsRawDirection is the other
// half of the same pin: 3° and 10° off vertical are NOT snapped — the
// exact continuous direction passes through unchanged, so a
// gravity-turned vehicle keeps leaning smoothly with no pop at the
// snap boundary (ADR 0048 explicitly rejected a two-renderer switch).
func TestSnapNearVertical_BeyondToleranceKeepsRawDirection(t *testing.T) {
	for _, deg := range []float64{3, 10} {
		rad := deg * math.Pi / 180
		wantX, wantY := math.Sin(rad), math.Cos(rad)
		x, y := snapNearVertical(wantX, wantY)
		if x != wantX || y != wantY {
			t.Errorf("at %.1f° off vertical: snapNearVertical = (%v, %v), want unchanged (%v, %v)", deg, x, y, wantX, wantY)
		}
	}
}

// TestComposeLaunchSprite_NearVerticalColumnsAreSolid reproduces the
// #424 bug directly and proves the fix: at 0° and 1.5° off vertical
// (inside the snap cone — the angles a player would call "the rocket
// is standing on the pad"), rasterising a tall multi-row stage through
// a REAL widgets.Canvas must land every row of the same nominal
// column on the identical canvas sub-pixel column. Before the snap,
// this failed at exactly these angles: floating-point drift in
// screenSX across rows rounded to different absolute columns, which
// is the "scattered, half-filled... changes pattern every row"
// TV-static the issue reported. An empty-sprite pass would be
// vacuous, so this asserts against a real 24-row × 5-wide stage (the
// Saturn V S-IC's authored dimensions) and fails loudly if the pixel
// count doesn't match rows×width.
func TestComposeLaunchSprite_NearVerticalColumnsAreSolid(t *testing.T) {
	const rows = 24
	const width = 5
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	for _, deg := range []float64{0, 1.5} {
		stage := spacecraft.Stage{LaunchSpriteRowsPx: rows, LaunchSpriteWidthPx: width, Color: "#F5EFE0"}
		cmd := angledCmd(deg)
		pixels := ComposeLaunchSprite([]spacecraft.Stage{stage}, cmd, basis, vesselSubPixelM)
		if got, want := len(pixels), rows*width; got != want {
			t.Fatalf("at %.1f°: got %d pixels, want %d (rows×width, single stage — no taper/separator)", deg, got, want)
		}

		c := widgets.NewCanvas(40, 20)
		c.SetBasis(basis)
		c.SetScale(1.0 / launchAutoScale(0, 20))
		c.Center(orbital.Vec3{})

		for col := 0; col < width; col++ {
			wantPx := -1
			for row := 0; row < rows; row++ {
				p := pixels[row*width+col]
				px, _, ok := c.Project(p.OffsetWorld)
				if !ok {
					t.Fatalf("at %.1f° col %d row %d: pixel projected off-canvas", deg, col, row)
				}
				if row == 0 {
					wantPx = px
					continue
				}
				if px != wantPx {
					t.Errorf("at %.1f° off vertical, column %d is NOT solid: row 0 rasterises to canvas column %d but row %d rasterises to %d — this is the #424 aliasing", deg, col, wantPx, row, px)
				}
			}
		}
	}
}

// TestComposeLaunchSprite_OffVerticalKeepsOriginalRasteriser confirms
// attitudes outside the snap cone (3°, 10°) are genuinely untouched:
// column 0's screen.X still DRIFTS row-to-row (the row-dependent term
// snapNearVertical removes when it fires is still present here), and
// stackDirScreen returns the raw normalized projection rather than
// (0, ±1) — the same continuous rasteriser this file has used since
// v0.11.3, so a gravity-turned vehicle keeps rendering exactly as
// before the #424 fix.
func TestComposeLaunchSprite_OffVerticalKeepsOriginalRasteriser(t *testing.T) {
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	const rows = 24
	const width = 5
	for _, deg := range []float64{3, 10} {
		cmd := angledCmd(deg)
		rad := deg * math.Pi / 180
		wantX, wantY := math.Sin(rad), math.Cos(rad)
		gotX, gotY := stackDirScreen(cmd, basis)
		// Two independent paths to the same unit vector (Sin/Cos here
		// vs. cmd.Dot(basis)/mag in stackDirScreen) can differ in the
		// last float64 bit — compare with a tight epsilon rather than
		// exact equality; the point being tested is "not snapped to
		// (0, ±1)", not bit-identical trig.
		const eps = 1e-12
		if math.Abs(gotX-wantX) > eps || math.Abs(gotY-wantY) > eps {
			t.Fatalf("at %.1f°: stackDirScreen = (%v, %v), want the raw unsnapped direction (%v, %v)", deg, gotX, gotY, wantX, wantY)
		}

		stage := spacecraft.Stage{LaunchSpriteRowsPx: rows, LaunchSpriteWidthPx: width, Color: "#F5EFE0"}
		pixels := ComposeLaunchSprite([]spacecraft.Stage{stage}, cmd, basis, vesselSubPixelM)
		x0 := pixels[0*width+0].OffsetWorld.Dot(basis.X)
		xLast := pixels[(rows-1)*width+0].OffsetWorld.Dot(basis.X)
		if x0 == xLast {
			t.Errorf("at %.1f° off vertical, column 0 shows NO row-dependent drift — the snap appears to be firing where it shouldn't", deg)
		}
	}
}

// TestStageSpriteWidthPx_FloorsBelowMinimum pins stageSpriteWidthPx's
// resolution order directly: unset (0) falls back to
// defaultSpriteWidthPx first, THEN both that fallback and any
// authored width below minRenderedStageWidthPx are floored; an
// authored width at or above the floor passes through unchanged.
func TestStageSpriteWidthPx_FloorsBelowMinimum(t *testing.T) {
	cases := []struct{ authored, want int }{
		{0, minRenderedStageWidthPx},
		{1, minRenderedStageWidthPx},
		{2, minRenderedStageWidthPx},
		{3, minRenderedStageWidthPx},
		{4, 4},
		{5, 5},
	}
	for _, c := range cases {
		got := stageSpriteWidthPx(spacecraft.Stage{LaunchSpriteWidthPx: c.authored})
		if got != c.want {
			t.Errorf("stageSpriteWidthPx(authored=%d) = %d, want %d", c.authored, got, c.want)
		}
	}
}

// TestComposeLaunchSprite_SeparatorAtEachBoundary is the direct TDD
// pin for ADR 0048 §4's third requirement: an unconditional dark
// separator row at EVERY boundary between two rendered stages,
// regardless of the (unrelated, height-gated) inter-stage taper.
// Three stages, all below taperThreshold rows, so no taper rows
// appear — every non-body pixel found here is a separator pixel.
func TestComposeLaunchSprite_SeparatorAtEachBoundary(t *testing.T) {
	s1 := spacecraft.Stage{LaunchSpriteRowsPx: 4, LaunchSpriteWidthPx: 5, Color: "#111111"}
	s2 := spacecraft.Stage{LaunchSpriteRowsPx: 4, LaunchSpriteWidthPx: 4, Color: "#222222"}
	s3 := spacecraft.Stage{LaunchSpriteRowsPx: 4, LaunchSpriteWidthPx: 3, Color: "#333333"}
	basis := widgets.Basis{
		X: orbital.Vec3{X: 1, Y: 0, Z: 0},
		Y: orbital.Vec3{X: 0, Y: 0, Z: 1},
	}
	pixels := ComposeLaunchSprite([]spacecraft.Stage{s1, s2, s3}, orbital.Vec3{Z: 1}, basis, 1.0)
	if len(pixels) == 0 {
		t.Fatal("expected sprite pixels, got none")
	}

	sep := string(stageSeparatorColor())
	widthAtY := map[int]int{}
	for _, p := range pixels {
		if string(p.Color) != sep {
			continue
		}
		y := int(math.Round(p.OffsetWorld.Dot(basis.Y)))
		widthAtY[y]++
	}
	if len(widthAtY) != 2 {
		t.Fatalf("got separator rows at %d distinct heights, want 2 (one per boundary); rows=%v", len(widthAtY), widthAtY)
	}
	// Boundary 1 sits right after s1's 4 body rows (y=0..3) at y=4,
	// width = max(s1 5, s2 4) = 5. Boundary 2 sits after s2's 4 body
	// rows (y=5..8) at y=9, width = max(s2 4, s3 3 floored to
	// minRenderedStageWidthPx 4) = 4.
	want := map[int]int{4: 5, 9: 4}
	for y, wantW := range want {
		if got := widthAtY[y]; got != wantW {
			t.Errorf("separator row at y=%d has width %d, want %d (full map: %v)", y, got, wantW, widthAtY)
		}
	}
}
