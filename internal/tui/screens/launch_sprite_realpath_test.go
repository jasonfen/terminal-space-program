package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestRealLaunchView_SIC_ColumnsAreSolid reproduces #424's follow-up
// through the ACTUAL chase-cam path — spawnSaturnVOnPad + real
// World.Tick() calls (so CurrentAttitudeDir is genuinely initialized the
// way the live game initializes it, not a synthetic unit vector) + a
// real LaunchView.Render call — rather than the synthetic basis/cmd
// TestComposeLaunchSprite_NearVerticalColumnsAreSolid used, which passed
// even while the real chase-cam still aliased. Two things distinguish
// this test from that one:
//
//  1. It measures the REAL chase-cam basis's angle off vertical on the
//     pad and logs it, so a future regression that moves the attitude
//     outside the snap cone shows up as a measured number, not a guess.
//  2. It reads back the ACTUAL RENDERED CANVAS BITMAP via
//     Canvas.SubPixelSet — not a re-derived projection — after a real
//     v.Render call, so it can only pass if the live render path is
//     genuinely solid, not if some parallel/idealized computation says
//     it should be.
func TestRealLaunchView_SIC_ColumnsAreSolid(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	for i := 0; i < 5; i++ {
		w.Tick()
	}

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	v.Render(w, 140, 40)

	basis := v.canvas.Basis()
	anchorWorld := v.canvas.CenterWorld()

	cmd := c.CurrentAttitudeDir
	if cmd.Norm() == 0 {
		t.Fatalf("precondition: CurrentAttitudeDir is still the zero vector after 5 ticks — the real init path never ran")
	}
	x := cmd.Dot(basis.X)
	y := cmd.Dot(basis.Y)
	mag := math.Sqrt(x*x + y*y)
	angleDeg := math.Atan2(math.Abs(x), math.Abs(y)) * 180 / math.Pi
	t.Logf("real chase-cam basis at KSC pad: cmd.X=%.9f cmd.Y=%.9f |proj|=%.9f angle-off-vertical=%.6f deg (snap cone = %.1f deg)", x, y, mag, angleDeg, nearVerticalSnapDeg)
	if angleDeg > nearVerticalSnapDeg {
		t.Fatalf("real attitude is %.4f deg off vertical - OUTSIDE the %.1f deg snap cone, so snapNearVertical never engages on the pad; the threshold or the basis is the bug, not the anchor", angleDeg, nearVerticalSnapDeg)
	}

	sic := c.Stages[0]
	if sic.Name != "S-IC" {
		t.Fatalf("Stages[0] = %q, want S-IC", sic.Name)
	}
	rows := sic.LaunchSpriteRowsPx
	width := stageSpriteWidthPx(sic)
	t.Logf("S-IC: rows=%d width=%d anchorWorld=%+v scale=%v", rows, width, anchorWorld, v.canvas.Scale())

	sprite := ComposeLaunchSprite([]spacecraft.Stage{sic}, cmd, basis, vesselSubPixelM)
	if len(sprite) != rows*width {
		t.Fatalf("got %d sprite pixels for S-IC, want %d (rows x width - no taper/separator on a lone stage)", len(sprite), rows*width)
	}

	// Real path: how drawComposedRocket actually plots (PlotColoredAnchored),
	// read back via ProjectAnchored's identical projection math, then
	// cross-checked against the raw drawille bitmap SubPixelSet reads —
	// so this confirms both "the intended column is consistent" AND
	// "the canvas that was actually drawn to agrees."
	badCols := 0
	for col := 0; col < width; col++ {
		wantPx, wantPy := -1, -1
		for row := 0; row < rows; row++ {
			p := sprite[row*width+col]
			px, py, ok := v.canvas.ProjectAnchored(anchorWorld, p.OffsetWorld)
			if !ok {
				t.Fatalf("col %d row %d: pixel projected off-canvas (px=%d py=%d)", col, row, px, py)
			}
			if !v.canvas.SubPixelSet(px, py) {
				t.Errorf("col %d row %d: ProjectAnchored says (px=%d,py=%d) but the real rendered canvas has NO dot there", col, row, px, py)
			}
			if row == 0 {
				wantPx, wantPy = px, py
				continue
			}
			if px != wantPx {
				badCols++
				t.Errorf("REAL PATH column %d is NOT solid: row 0 -> canvas col %d, row %d -> canvas col %d (py %d vs %d) - this is the #424 follow-up aliasing on the actual chase-cam render", col, wantPx, row, px, wantPy, py)
				break
			}
		}
	}
	if badCols == 0 {
		t.Logf("all %d columns solid across %d rows on the real chase-cam path (verified against the actual rendered bitmap)", width, rows)
	}
}
