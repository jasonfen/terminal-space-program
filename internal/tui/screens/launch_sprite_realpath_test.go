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
//
// This checks COLUMN consistency only (every row of a given nominal
// column lands on the same canvas px). It does NOT catch a row-gap
// aliasing where the intended column is right but rows in between two
// samples are simply dark — see
// TestRealLaunchView_SII_EveryRowInSpanIsLit for that.
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

// TestRealLaunchView_SII_EveryRowInSpanIsLit is the row-gap counterpart
// to TestRealLaunchView_SIC_ColumnsAreSolid: it doesn't just check that
// samples land on a consistent column, it checks that EVERY sub-pixel
// ROW within the stage's vertical span has a lit pixel in the stage's
// column band — the exact property that failed on the after2 capture
// (rows 3-16, cols 70-73 alternating a fully-lit cell row with a sparse
// one: ComposeLaunchSprite samples every vesselSubPixelM = 1.5 m, but
// the chase-cam's actual sub-pixel pitch on the pad is FINER than that,
// so a single point per sample left every other sub-pixel row dark).
//
// Checked on S-II (Stages[1]), not S-IC: S-IC's base sits right at pad
// level, close enough to the ground-band fill (issue #424 item 5) that
// a naive "is ANY pixel lit in this row" check could pass by
// coincidence from the UNRELATED ground fill rather than the sprite
// itself. S-II sits a full S-IC (24 rows x 1.5 m = 36 m) above that,
// comfortably clear of the ground band, so a lit pixel there can only
// be the rocket.
func TestRealLaunchView_SII_EveryRowInSpanIsLit(t *testing.T) {
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

	sii := c.Stages[1]
	if sii.Name != "S-II" {
		t.Fatalf("Stages[1] = %q, want S-II", sii.Name)
	}
	siiColor := stageSpriteColor(sii)

	// Compose the FULL stack (not S-II alone) so S-II's samples land at
	// the same rowOffset the real render placed them at — S-IC's rows
	// plus whatever taper/separator rows sit between S-IC and S-II.
	full := ComposeLaunchSprite(c.Stages, cmd, basis, vesselSubPixelM)

	var siiSamples []SpritePixel
	for _, p := range full {
		if p.Color == siiColor {
			siiSamples = append(siiSamples, p)
		}
	}
	if len(siiSamples) == 0 {
		t.Fatal("no S-II-coloured samples found in the composed stack")
	}

	minPx, maxPx, minPy, maxPy := 1<<30, -(1 << 30), 1<<30, -(1 << 30)
	for _, p := range siiSamples {
		px, py, ok := v.canvas.ProjectAnchored(anchorWorld, p.OffsetWorld)
		if !ok {
			t.Fatalf("S-II sample projected off-canvas (px=%d py=%d)", px, py)
		}
		if px < minPx {
			minPx = px
		}
		if px > maxPx {
			maxPx = px
		}
		if py < minPy {
			minPy = py
		}
		if py > maxPy {
			maxPy = py
		}
	}
	t.Logf("S-II real column band: px[%d..%d] py[%d..%d] (%d samples)", minPx, maxPx, minPy, maxPy, len(siiSamples))

	darkRows := 0
	for py := minPy; py <= maxPy; py++ {
		lit := false
		for px := minPx; px <= maxPx; px++ {
			if v.canvas.SubPixelSet(px, py) {
				lit = true
				break
			}
		}
		if !lit {
			darkRows++
			t.Errorf("REAL PATH row py=%d is completely DARK across S-II's own column band (px %d..%d) — a stripe of the #424 follow-up row-gap aliasing", py, minPx, maxPx)
		}
	}
	if darkRows == 0 {
		t.Logf("every sub-pixel row in S-II's span (py %d..%d) has a lit pixel — no row-gap striping on the real chase-cam path", minPy, maxPy)
	}
}
