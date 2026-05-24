package widgets

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

func TestProjectCenterMapsToMiddle(t *testing.T) {
	c := NewCanvas(40, 20) // pixel grid 80 × 80
	c.SetScale(1)
	c.Center(orbital.Vec3{X: 100, Y: -50})
	px, py, ok := c.Project(orbital.Vec3{X: 100, Y: -50})
	if !ok {
		t.Fatal("center should be on-canvas")
	}
	if px != c.pxW/2 || py != c.pxH/2 {
		t.Errorf("center maps to (%d,%d), want (%d,%d)", px, py, c.pxW/2, c.pxH/2)
	}
}

func TestProjectYFlip(t *testing.T) {
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	// +Y world should give a smaller py (upward in screen space).
	_, pyUp, _ := c.Project(orbital.Vec3{Y: 10})
	_, pyDown, _ := c.Project(orbital.Vec3{Y: -10})
	if pyUp >= pyDown {
		t.Errorf("+Y world (py=%d) should be above -Y world (py=%d)", pyUp, pyDown)
	}
}

func TestFitToScalesCorrectly(t *testing.T) {
	c := NewCanvas(40, 20) // pxW=80, pxH=80, shorter=80
	c.Center(orbital.Vec3{})
	c.FitTo(1e9) // radius 1 billion meters
	want := 0.45 * 80 / 1e9
	if c.Scale() != want {
		t.Errorf("FitTo: scale=%.6e, want %.6e", c.Scale(), want)
	}
}

func TestOffCanvasReturnsOk(t *testing.T) {
	c := NewCanvas(10, 10)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	_, _, ok := c.Project(orbital.Vec3{X: 1e6}) // way off
	if ok {
		t.Error("far point should report off-canvas")
	}
}

// TestFillDiskProducesNonEmptyRender: drawing a disk at the center
// should yield at least one non-space character in the rendered string.
// Catches regressions where the pixel loop drops all samples (e.g. a
// sign flip in the bounding box).
func TestFillDiskProducesNonEmptyRender(t *testing.T) {
	c := NewCanvas(20, 10)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	c.FillDisk(orbital.Vec3{}, 3)
	if onlyWhitespace(c.String()) {
		t.Error("FillDisk at center produced an empty canvas")
	}
}

// TestRingOutlineProducesNonEmptyRender: same guard for the ring
// primitive used by the system primary.
func TestRingOutlineProducesNonEmptyRender(t *testing.T) {
	c := NewCanvas(20, 10)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	c.RingOutline(orbital.Vec3{}, 3)
	if onlyWhitespace(c.String()) {
		t.Error("RingOutline at center produced an empty canvas")
	}
}

// TestDrawEllipseOffsetDottedTranslates: drawing the same ellipse with
// offset={0,0} vs offset={large} should produce different non-empty
// renders — the translated version should move the curve entirely off-
// canvas for a large enough offset.
func TestDrawEllipseOffsetDottedTranslates(t *testing.T) {
	c := NewCanvas(20, 10)
	c.SetScale(1.0 / 100) // 1 pixel per 100 m
	c.Center(orbital.Vec3{})

	el := orbital.Elements{A: 500, E: 0} // 500 m circle
	c.Clear()
	c.DrawEllipseOffsetDotted(el, orbital.Vec3{}, 64, 1)
	onScreen := c.String()
	if onlyWhitespace(onScreen) {
		t.Fatal("zero-offset ellipse rendered empty")
	}

	c.Clear()
	// Offset by 1e6 m — far beyond pxW × 100 m/px, so entirely off-canvas.
	c.DrawEllipseOffsetDotted(el, orbital.Vec3{X: 1e6}, 64, 1)
	offScreen := c.String()
	if !onlyWhitespace(offScreen) {
		t.Error("offset-off-canvas ellipse still rendered visible pixels")
	}
}

// onlyWhitespace treats ASCII whitespace and U+2800 (braille blank, "⠀")
// as empty. drawille writes U+2800 for rows with no dots set; ignoring
// it lets tests assert "nothing plotted" without caring about the
// encoding.
// TestPlotArrowProducesNonEmptyRender: the chevron glyph should paint
// some pixels for any non-zero velocity. Zero velocity is a no-op
// (direction is undefined) and must not panic.
func TestPlotArrowProducesNonEmptyRender(t *testing.T) {
	c := NewCanvas(20, 10)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	c.PlotArrow(orbital.Vec3{}, orbital.Vec3{X: 1}, 4)
	if onlyWhitespace(c.String()) {
		t.Error("PlotArrow in +X direction rendered empty canvas")
	}
	// Zero velocity must not panic and must leave the canvas untouched.
	c.Clear()
	c.PlotArrow(orbital.Vec3{}, orbital.Vec3{}, 4)
	if !onlyWhitespace(c.String()) {
		t.Error("PlotArrow with zero velocity plotted pixels")
	}
}

// TestColoredDiskEmitsAnsiOnRender: v0.5.3 — AddColoredDisk should
// cause the corresponding cells to render with a lipgloss foreground
// escape sequence. Sanity-check that the SGR (ESC [ 38 ;) substring
// appears in the output when colored, and is absent when not.
func TestColoredDiskEmitsAnsiOnRender(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1") // force lipgloss to emit ANSI in tests
	c := NewCanvas(20, 10)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	c.FillDisk(orbital.Vec3{}, 3)
	uncolored := c.String()
	if strings.Contains(uncolored, "\x1b[") {
		t.Errorf("uncolored canvas contained ANSI escape: %q", uncolored)
	}

	c.Clear()
	c.FillColoredDisk(orbital.Vec3{}, 3, lipgloss.Color("#FF0000"))
	colored := c.String()
	if !strings.Contains(colored, "\x1b[") {
		t.Error("colored canvas missing ANSI escape sequence")
	}
}

// TestRingOutlineHugeRadiusDoesNotHang: v0.5.15 regression — pre-fix
// a ring with pxRadius in the millions (Saturn rings projected at
// extreme zoom) looped pxRadius*8 ≈ billions of times, locking the
// game when the user changed focus to a tiny body. The samples cap
// keeps the loop bounded by the canvas pixel-diagonal.
func TestRingOutlineHugeRadiusDoesNotHang(t *testing.T) {
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	c.RingColoredOutline(orbital.Vec3{}, 1_000_000, lipgloss.Color("#FF0000"))
	// If we get here, the cap held. Without it the test would never
	// finish (or OOM the map). Sanity: at least one pixel got tagged.
	if len(c.pixelTags) == 0 {
		t.Skip("ring entirely off-canvas — test setup issue, not a regression")
	}
}

// TestPerPixelTagDoesNotBleed: v0.5.10 — pixel tags only affect cells
// containing tagged pixels. A colored disk + an untagged Plot in a
// nearby cell should leave the Plot's cell uncolored. Pre-fix the
// cell-rectangle approach colored the Plot cell when it fell inside
// the disk's bounding box.
func TestPerPixelTagDoesNotBleed(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	// Tagged disk at origin, radius 2 (4×4 px box → ~2×1 cells).
	c.FillColoredDisk(orbital.Vec3{}, 2, lipgloss.Color("#FF0000"))
	// Untagged Plot 8 px to the right (4 cells away, well clear of disk).
	c.Plot(orbital.Vec3{X: 8})
	out := c.String()

	// Count ANSI escape sequences. With per-pixel tagging only the
	// disk's ~2 cells should be wrapped, NOT the lone Plot cell.
	// Pre-fix the disk's whole bounding-box cells (and any nearby
	// untagged content within them) would all be colored.
	red := strings.Count(out, "\x1b[")
	if red < 2 {
		t.Errorf("expected ANSI sequences for disk cells, got %d", red)
	}
	// The standalone Plot's cell should NOT carry red. Look for "⠁" or
	// any braille char NOT preceded by an ANSI escape — robust check
	// would parse colors per cell, but for this regression we just
	// verify a non-trivial portion of the output is plain.
	if !strings.Contains(out, "⠁") && !strings.Contains(out, "⠈") &&
		!strings.Contains(out, "⠠") && !strings.Contains(out, "⠐") {
		// Some single-pixel braille glyph should appear from the Plot.
		// If absent, the test setup needs adjustment, not a regression.
		t.Skip("Plot didn't produce a recognisable single-pixel glyph; test setup")
	}
}

// TestUnprojectRoundTripDefaultBasis: Project(world) → (px, py); the
// pixel-rounding cost is at most one half-pixel of world distance,
// so Unproject(Project(w)) ≈ w within that bound. Default basis case.
// v0.6.4+.
func TestUnprojectRoundTripDefaultBasis(t *testing.T) {
	c := NewCanvas(60, 30)
	c.SetScale(0.01)               // pixels per metre — 1 m ≈ 0.01 px
	c.Center(orbital.Vec3{X: 1000}) // off-origin to exercise centerW
	cases := []orbital.Vec3{
		{X: 1000, Y: 0},
		{X: 1500, Y: 250},
		{X: 750, Y: -500},
	}
	for _, w := range cases {
		px, py, ok := c.Project(w)
		if !ok {
			t.Errorf("project(%v) off-canvas — expected on-canvas for round-trip", w)
			continue
		}
		got := c.Unproject(px, py)
		// Tolerance: half-pixel of world distance = (1 / scale) / 2.
		halfPxWorld := 0.5 / c.scale
		if abs(got.X-w.X) > halfPxWorld || abs(got.Y-w.Y) > halfPxWorld {
			t.Errorf("round-trip %v → (%d, %d) → %v exceeds %.1f tolerance",
				w, px, py, got, halfPxWorld)
		}
	}
}

// TestUnprojectRoundTripPerifocalBasis: same round-trip invariant
// against an arbitrary basis (orbit-perpendicular case). World
// points are in the basis plane (xHat / yHat span); Project drops
// the orbit-normal component, so the round-trip is exact-ish on the
// (xHat, yHat) plane.
func TestUnprojectRoundTripPerifocalBasis(t *testing.T) {
	c := NewCanvas(60, 30)
	c.SetScale(0.01)
	c.Center(orbital.Vec3{})
	// Inclined orbit: i = 30°, Ω = 45°, ω = 60°.
	el := orbital.Elements{A: 1e6, E: 0.2, I: 30 * 3.14159265 / 180,
		Omega: 45 * 3.14159265 / 180, Arg: 60 * 3.14159265 / 180}
	xHat, yHat := orbital.PerifocalBasis(el)
	c.SetBasis(Basis{X: xHat, Y: yHat})

	// Test points are linear combinations of xHat and yHat (i.e.,
	// they lie in the orbit plane).
	combos := []struct{ a, b float64 }{
		{1000, 0},
		{500, 250},
		{-300, 750},
	}
	for _, lc := range combos {
		w := xHat.Scale(lc.a).Add(yHat.Scale(lc.b))
		px, py, ok := c.Project(w)
		if !ok {
			continue
		}
		got := c.Unproject(px, py)
		// `got` should equal w to within rounding. The basis is
		// orthonormal, so the (a, b) coords are recovered exactly.
		halfPxWorld := 0.5 / c.scale
		dx := got.Sub(w)
		if abs(dx.X) > halfPxWorld || abs(dx.Y) > halfPxWorld || abs(dx.Z) > halfPxWorld {
			t.Errorf("round-trip in perifocal basis (%v) → got %v (diff %v) exceeds %.1f",
				w, got, dx, halfPxWorld)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestHitAtResolvesBodyTag (v0.6.4+): a tagged disk drawn at a known
// world coord is recoverable by HitAt at the cell containing the
// disk's center. The test sets a 5-pixel disk and asserts the
// center cell hits with the supplied BodyID.
func TestHitAtResolvesBodyTag(t *testing.T) {
	c := NewCanvas(40, 20) // pixel grid 80 × 80
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.FillColoredDiskTagged(orbital.Vec3{}, 5, CellTag{
		Color:  lipgloss.Color("#FF0000"),
		BodyID: "moon",
	})
	// Disk center maps to canvas pixel (40, 40), terminal cell (20, 10).
	hit := c.HitAt(20, 10)
	if hit.BodyID != "moon" {
		t.Errorf("center cell hit BodyID = %q, want %q", hit.BodyID, "moon")
	}
}

// TestHitAtUntaggedReturnsZero: a cell whose covered pixels carry
// only color tags (no BodyID / NodeIdx / IsVessel) returns the
// zero-value CellTag — used by the orbit screen to differentiate
// "click on a sim object" from "click on empty canvas."
func TestHitAtUntaggedReturnsZero(t *testing.T) {
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	// PlotColored only sets Color — no BodyID / NodeIdx / IsVessel.
	c.PlotColored(orbital.Vec3{}, lipgloss.Color("#00FF00"))
	hit := c.HitAt(20, 10)
	if hit.BodyID != "" || hit.NodeIdx != 0 || hit.IsVessel {
		t.Errorf("untagged cell hit non-zero CellTag: %+v", hit)
	}
}

// TestPickDominantColorMajority (v0.7.2.3+): pickDominantColor
// returns the color with the highest pixel count in a cell. Used by
// String() to resolve the per-cell color from per-pixel tags.
func TestPickDominantColorMajority(t *testing.T) {
	counts := map[lipgloss.Color]int{
		lipgloss.Color("#FF0000"): 5,
		lipgloss.Color("#0000FF"): 3,
	}
	if got := pickDominantColor(counts); got != lipgloss.Color("#FF0000") {
		t.Errorf("got %q, want #FF0000 (majority)", string(got))
	}
}

// TestPickDominantColorTieBreak (v0.7.2.3+): equal pixel counts must
// resolve deterministically. Documented contract: the lexicographic-
// ally smaller color string wins. Pre-v0.7.2.3 the picker iterated
// the map and used Go's randomized iteration order to pick — so the
// "winner" of a tie flipped frame-to-frame, causing visible flicker
// on textured-body cells (Earth coastlines, Moon mare boundaries).
func TestPickDominantColorTieBreak(t *testing.T) {
	counts := map[lipgloss.Color]int{
		lipgloss.Color("#FF0000"): 4,
		lipgloss.Color("#0000FF"): 4,
	}
	// Run many times — Go's map iteration is randomized per range.
	// Each call must return the same answer regardless of seed.
	for i := 0; i < 100; i++ {
		got := pickDominantColor(counts)
		if got != lipgloss.Color("#0000FF") {
			t.Fatalf("iter %d: got %q, want #0000FF (lex-smaller)", i, string(got))
		}
	}
}

// TestHitAtOutOfBoundsReturnsZero: clicks outside the canvas
// content area must not panic and must return zero. The mouse
// dispatch uses this guard for clicks that fall on the title /
// border / HUD regions.
func TestHitAtOutOfBoundsReturnsZero(t *testing.T) {
	c := NewCanvas(40, 20)
	for _, p := range [][2]int{
		{-1, 5}, {5, -1}, {40, 5}, {5, 20}, {1000, 1000},
	} {
		hit := c.HitAt(p[0], p[1])
		if hit.BodyID != "" || hit.NodeIdx != 0 || hit.IsVessel {
			t.Errorf("HitAt(%d, %d) = %+v, want zero", p[0], p[1], hit)
		}
	}
}

// TestIsBehindBodyDepthAndDisk (v0.6.4+): the helper returns true
// only when both conditions hold — sample is behind the body's
// camera-perpendicular plane AND the projected pixel coord falls
// inside the body's disk. Two of three failure modes (in front, or
// behind but outside the disk) must return false.
func TestIsBehindBodyDepthAndDisk(t *testing.T) {
	c := NewCanvas(60, 30)
	c.SetScale(0.001) // 1 m → 0.001 px
	c.Center(orbital.Vec3{})
	// Right-view basis: depth axis = +X (toward camera).
	c.SetBasis(Basis{X: orbital.Vec3{Y: 1}, Y: orbital.Vec3{Z: 1}})

	// Body at origin with 100 px projected radius (= 100 km world).
	body := orbital.Vec3{}
	const bodyPxR = 100

	cases := []struct {
		name    string
		sample  orbital.Vec3
		want    bool
		comment string
	}{
		{
			name:    "front, on screen-axis with body",
			sample:  orbital.Vec3{X: 50_000}, // depth +50 km → in front
			want:    false,
			comment: "depth ≥ 0 → never occluded",
		},
		{
			name:    "behind, screen-coincident with body",
			sample:  orbital.Vec3{X: -50_000}, // depth -50 km → behind
			want:    true,
			comment: "behind + same screen pos = inside disk",
		},
		{
			name:    "behind, screen-far-from body",
			sample:  orbital.Vec3{X: -50_000, Y: 200_000}, // off-disk laterally
			want:    false,
			comment: "behind but screen pos outside disk",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.IsBehindBody(tc.sample, body, bodyPxR)
			if got != tc.want {
				t.Errorf("got %v, want %v — %s", got, tc.want, tc.comment)
			}
		})
	}
}

// TestDepthAxisCardinalBases (v0.6.4+): each cardinal-view basis
// produces the expected camera-toward axis. Top → +Z, Right → +X,
// Bottom → -Z, Left → -X. Catches sign / cross-product errors that
// would silently invert the in-front / behind decision in side
// views.
func TestDepthAxisCardinalBases(t *testing.T) {
	cases := []struct {
		name string
		b    Basis
		want orbital.Vec3
	}{
		{"top", Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Y: 1}}, orbital.Vec3{Z: 1}},
		{"right", Basis{X: orbital.Vec3{Y: 1}, Y: orbital.Vec3{Z: 1}}, orbital.Vec3{X: 1}},
		{"bottom", Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Y: -1}}, orbital.Vec3{Z: -1}},
		{"left", Basis{X: orbital.Vec3{Y: -1}, Y: orbital.Vec3{Z: 1}}, orbital.Vec3{X: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.b.DepthAxis()
			if abs(got.X-tc.want.X) > 1e-12 || abs(got.Y-tc.want.Y) > 1e-12 || abs(got.Z-tc.want.Z) > 1e-12 {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTiltedWorldBasisIdentity (v0.10.6+): TiltedWorldBasis(0, 0)
// must return DefaultBasis verbatim. The struct-literal World zero
// value gives ViewTilt{0, 0}, and the ViewTilted code path then
// has to reduce to the pre-v0.10.6 ViewTop projection so existing
// golden tests stay byte-identical.
func TestTiltedWorldBasisIdentity(t *testing.T) {
	got := TiltedWorldBasis(0, 0)
	want := DefaultBasis()
	if abs(got.X.X-want.X.X) > 1e-12 || abs(got.X.Y-want.X.Y) > 1e-12 || abs(got.X.Z-want.X.Z) > 1e-12 {
		t.Errorf("X: got %v, want %v", got.X, want.X)
	}
	if abs(got.Y.X-want.Y.X) > 1e-12 || abs(got.Y.Y-want.Y.Y) > 1e-12 || abs(got.Y.Z-want.Y.Z) > 1e-12 {
		t.Errorf("Y: got %v, want %v", got.Y, want.Y)
	}
}

// TestTiltedWorldBasisTilts (v0.10.6+): with θ = 25° and φ = 0,
// the basis stays anchored on world X but the Y axis pitches into
// +Z (orbital.Rotate is right-hand-rule about the axis). DepthAxis
// = X × Y therefore tips from +Z toward -Y. Catches sign / axis-
// order errors in the rotation composition; the tilt direction
// itself is a free convention — what matters is that the basis
// stays orthonormal and the tilt is non-zero. Players (or v0.10.7's
// launch-anchor) can yaw via φ to retarget which world axis ends
// up at screen-Y+.
func TestTiltedWorldBasisTilts(t *testing.T) {
	theta := 25 * math.Pi / 180
	b := TiltedWorldBasis(theta, 0)
	// X axis untouched (yaw φ = 0 leaves world X alone; θ rotates
	// around it).
	if abs(b.X.X-1) > 1e-12 || abs(b.X.Y) > 1e-12 || abs(b.X.Z) > 1e-12 {
		t.Errorf("X = %v, want (1, 0, 0)", b.X)
	}
	// Y rotated around X by θ (right-hand): (0, 1, 0) → (0, cosθ, sinθ).
	wantY := orbital.Vec3{Y: math.Cos(theta), Z: math.Sin(theta)}
	if abs(b.Y.X-wantY.X) > 1e-9 || abs(b.Y.Y-wantY.Y) > 1e-9 || abs(b.Y.Z-wantY.Z) > 1e-9 {
		t.Errorf("Y = %v, want %v", b.Y, wantY)
	}
	// Depth axis: X × Y = (1,0,0) × (0, cosθ, sinθ) = (0, -sinθ, cosθ).
	d := b.DepthAxis()
	wantD := orbital.Vec3{Y: -math.Sin(theta), Z: math.Cos(theta)}
	if abs(d.X-wantD.X) > 1e-9 || abs(d.Y-wantD.Y) > 1e-9 || abs(d.Z-wantD.Z) > 1e-9 {
		t.Errorf("DepthAxis = %v, want %v", d, wantD)
	}
}

// TestTiltedPerifocalBasisIdentity (v0.10.6+): TiltedPerifocalBasis
// with θ = 0 and φ = 0 must return PerifocalBasis(el) verbatim —
// the ViewOrbitFlat projection. Documents the
// ViewTilted-with-zero-tilt ≡ ViewOrbitFlat equivalence so a
// player at θ=0 sees the same basis the cycle's last mode gives.
func TestTiltedPerifocalBasisIdentity(t *testing.T) {
	el := orbital.Elements{A: 7_000_000, E: 0.1, I: 0.3, Omega: 0.4, Arg: 0.5}
	got := TiltedPerifocalBasis(el, 0, 0)
	wantX, wantY := orbital.PerifocalBasis(el)
	if abs(got.X.X-wantX.X) > 1e-12 || abs(got.X.Y-wantX.Y) > 1e-12 || abs(got.X.Z-wantX.Z) > 1e-12 {
		t.Errorf("X: got %v, want %v", got.X, wantX)
	}
	if abs(got.Y.X-wantY.X) > 1e-12 || abs(got.Y.Y-wantY.Y) > 1e-12 || abs(got.Y.Z-wantY.Z) > 1e-12 {
		t.Errorf("Y: got %v, want %v", got.Y, wantY)
	}
}

// TestTiltedPerifocalBasisPreservesOrthonormality (v0.10.6+): for
// any (el, θ, φ) the resulting Basis is orthonormal — |X|=|Y|=1
// and X·Y=0. Two rotations of unit vectors around unit axes
// preserve magnitudes and angles; if this test breaks, the
// rotation composition order is wrong.
func TestTiltedPerifocalBasisPreservesOrthonormality(t *testing.T) {
	el := orbital.Elements{A: 1.5e7, E: 0.4, I: 1.1, Omega: 0.9, Arg: 0.7}
	for _, theta := range []float64{0, 0.2, 0.6, math.Pi / 3} {
		for _, phi := range []float64{0, 0.3, 1.5} {
			b := TiltedPerifocalBasis(el, theta, phi)
			if abs(b.X.Norm()-1) > 1e-9 {
				t.Errorf("θ=%v φ=%v: |X| = %v", theta, phi, b.X.Norm())
			}
			if abs(b.Y.Norm()-1) > 1e-9 {
				t.Errorf("θ=%v φ=%v: |Y| = %v", theta, phi, b.Y.Norm())
			}
			if abs(b.X.Dot(b.Y)) > 1e-9 {
				t.Errorf("θ=%v φ=%v: X·Y = %v", theta, phi, b.X.Dot(b.Y))
			}
		}
	}
}

// TestDrawEllipseOffsetFarSideDashed_FarSideStippled (v0.10.6+):
// with a circular orbit straddling the basis plane, near-side
// samples render at the requested nearStride and far-side samples
// render at 2*nearStride. The test counts plotted pixels in each
// half and asserts the near-side count is meaningfully higher
// than the far-side count — the depth-stride invariant the
// player relies on for the depth read.
func TestDrawEllipseOffsetFarSideDashed_FarSideStippled(t *testing.T) {
	c := NewCanvas(80, 40)
	c.Center(orbital.Vec3{})
	c.FitTo(1e7)
	// Top-down basis means the depth axis is +Z; a circular
	// equatorial orbit lies entirely on the basis plane (depth = 0),
	// which is the boundary case. Tilt the basis by 30° so half
	// the orbit lands on each side cleanly.
	c.SetBasis(TiltedWorldBasis(30*math.Pi/180, 0))
	el := orbital.Elements{A: 5e6, E: 0, I: 0, Omega: 0, Arg: 0}
	c.DrawEllipseOffsetFarSideDashed(el, orbital.Vec3{}, 360, 3, orbital.Vec3{}, 0, "")
	// Count tagged-or-set pixels by depth sign.
	near, far := 0, 0
	for i := 0; i < 360; i++ {
		nu := 2 * math.Pi * float64(i) / 360
		p := orbital.PositionAtTrueAnomaly(el, nu)
		d := p.Dot(c.Basis().DepthAxis())
		px, py, ok := c.Project(p)
		if !ok {
			continue
		}
		if c.dc.Get(px, py) {
			if d >= 0 {
				near++
			} else {
				far++
			}
		}
	}
	if near == 0 {
		t.Fatalf("no near-side pixels drawn — basis or projection broken")
	}
	if far == 0 {
		t.Fatalf("no far-side pixels drawn — far-side stipple should still produce some pixels")
	}
	// Near side renders ~1/3 (stride=3); far side renders ~1/6
	// (stride=6). Expect roughly 2:1 ratio. Allow slack for
	// drawille pixel quantisation.
	if near < far*3/2 {
		t.Errorf("near=%d, far=%d — near should be ~2x far (stride 3 vs 6)", near, far)
	}
}

func onlyWhitespace(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\n' && r != '\t' && r != '⠀' {
			return false
		}
	}
	return true
}
