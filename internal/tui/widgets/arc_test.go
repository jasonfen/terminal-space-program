package widgets

import (
	"math"
	"sort"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// arcColor is the tag colour the adaptive-sampling tests draw with, so
// taggedPixels can pull exactly the curve's ink back out.
const arcColor = lipgloss.Color("#33AAFF")

// litRows / litPixels are the row coverage and pixel set of a drawn curve.
func litRows(c *Canvas, color lipgloss.Color) int {
	rows := map[int]bool{}
	for _, p := range taggedPixels(c, color) {
		rows[p[1]] = true
	}
	return len(rows)
}

// zoomedCanvas frames a circular orbit of radius R, then multiplies the fit
// scale by `zoom` — the same baseScale × userZoom composition the orbit
// screen applies. The canvas centre sits ON the orbit (its periapsis), which
// is where the vessel would be, so zooming in magnifies a real piece of the
// track rather than empty space.
func zoomedCanvas(cols, rows int, r, zoom float64) *Canvas {
	c := NewCanvas(cols, rows)
	c.Center(orbital.Vec3{X: r})
	c.FitTo(r)
	c.ZoomBy(zoom)
	c.Clear()
	return c
}

// TestEllipseStaysConnectedWhenZoomedIn is the headline of ADR 0042 §3: a
// trajectory must read as a curve at any zoom. The shipped fixed-360-sample
// stride loop spread its samples further apart the further the player zoomed
// in, until the ellipse was a scatter of dots ("zoom-in turns to dust"). With
// arc-length sampling the visible arc stays a connected line — asserted as
// "the curve reaches nearly every pixel row it crosses" — at 1×, 40× and
// 1000× the framing scale, on an 80×24 terminal.
func TestEllipseStaysConnectedWhenZoomedIn(t *testing.T) {
	const r = 1e7
	el := orbital.Elements{A: r}
	for _, zoom := range []float64{1, 40, 1000, 100000} {
		c := zoomedCanvas(80, 24, r, zoom) // 160 × 96 px
		c.DrawEllipseOffsetFarSideDashed(el, orbital.Vec3{}, 360, 1, orbital.Vec3{}, 0, arcColor)
		if got := litRows(c, arcColor); got < 80 {
			t.Errorf("zoom %.0f×: arc lit %d of 96 pixel rows — the curve broke up into dots", zoom, got)
		}
	}
}

// TestEllipseDotSpacingIsZoomInvariant: the spacing argument is a distance in
// canvas pixels along the arc, so the same ellipse at wildly different zooms
// inks at the same on-screen cadence. At high zoom the visible arc is a
// near-vertical line through the canvas centre, so consecutive dots ordered
// by row are consecutive along the arc.
func TestEllipseDotSpacingIsZoomInvariant(t *testing.T) {
	const (
		r       = 1e7
		spacing = 4
	)
	el := orbital.Elements{A: r}
	for _, zoom := range []float64{200, 5000, 250000} {
		c := zoomedCanvas(80, 24, r, zoom)
		c.DrawEllipseOffsetFarSideDashed(el, orbital.Vec3{}, 360, spacing, orbital.Vec3{}, 0, arcColor)
		ys := make([]int, 0, 64)
		for _, p := range taggedPixels(c, arcColor) {
			ys = append(ys, p[1])
		}
		if len(ys) < 10 {
			t.Fatalf("zoom %.0f×: only %d dots on the visible arc", zoom, len(ys))
		}
		sort.Ints(ys)
		for i := 1; i < len(ys); i++ {
			if gap := ys[i] - ys[i-1]; gap > spacing+2 {
				t.Errorf("zoom %.0f×: %d px gap between dots at y=%d and y=%d, want ≈%d",
					zoom, gap, ys[i-1], ys[i], spacing)
				break
			}
		}
		// And not solid: a finely flattened curve must still honour the
		// cadence across chord boundaries rather than plotting every chord's
		// own start pixel.
		if len(ys) > 96/spacing*2 {
			t.Errorf("zoom %.0f×: %d dots over ~96 px of arc at spacing %d — cadence lost between chords",
				zoom, len(ys), spacing)
		}
	}
}

// TestEllipseSpacingArgumentScalesInk: doubling the spacing roughly halves
// the ink, at a fixed zoom — the depth stipple (far side = 2× spacing) and
// the per-line-role density levers both rest on this.
func TestEllipseSpacingArgumentScalesInk(t *testing.T) {
	const r = 1e7
	el := orbital.Elements{A: r}
	count := func(spacing int) int {
		c := zoomedCanvas(200, 60, r, 1)
		c.DrawEllipseOffsetFarSideDashed(el, orbital.Vec3{}, 360, spacing, orbital.Vec3{}, 0, arcColor)
		return len(taggedPixels(c, arcColor))
	}
	dense, sparse := count(2), count(4)
	if dense == 0 || sparse == 0 {
		t.Fatalf("no ink: spacing2=%d spacing4=%d", dense, sparse)
	}
	if ratio := float64(dense) / float64(sparse); ratio < 1.6 || ratio > 2.4 {
		t.Errorf("spacing 2 vs 4 inked %d vs %d pixels (ratio %.2f), want ≈2", dense, sparse, ratio)
	}
}

// TestEllipseOffCanvasCostsNothing: a curve that can't reach the viewport is
// pruned rather than sampled — the perf side of the adaptive policy. An
// orbit far off-screen must set no pixels at all.
func TestEllipseOffCanvasDrawsNothing(t *testing.T) {
	c := NewCanvas(80, 24)
	c.SetScale(1)
	c.Center(orbital.Vec3{X: 1e9}) // a gigapixel away from the orbit
	c.Clear()
	c.DrawEllipseOffsetFarSideDashed(orbital.Elements{A: 1e4}, orbital.Vec3{}, 360, 2, orbital.Vec3{}, 0, arcColor)
	if n := len(taggedPixels(c, arcColor)); n != 0 {
		t.Errorf("off-canvas ellipse set %d pixels, want 0", n)
	}
}

// TestEllipseEccentricSamplingCoversApoapsis: sampling steps in eccentric
// anomaly, not true anomaly. Uniform ν starves the apoapsis half of an
// eccentric orbit (where the tangent turns 1/(1−e) times faster per radian
// of ν), which is exactly where a transfer ellipse spends its time. Frame the
// apoapsis of an e=0.9 orbit and require the arc to read as a curve there.
func TestEllipseEccentricSamplingCoversApoapsis(t *testing.T) {
	el := orbital.Elements{A: 1e7, E: 0.9}
	apo := orbital.PositionAtTrueAnomaly(el, math.Pi)
	c := NewCanvas(80, 24)
	c.Center(apo)
	c.SetScale(96 * 0.45 / (0.2 * el.A)) // zoomed well inside the orbit
	c.Clear()
	c.DrawEllipseOffsetFarSideDashed(el, orbital.Vec3{}, 360, 1, orbital.Vec3{}, 0, arcColor)
	if got := litRows(c, arcColor); got < 40 {
		t.Errorf("apoapsis arc of an e=0.9 orbit lit %d pixel rows — sampling starves the slow half", got)
	}
}

// TestClipSegmentToCanvas covers the three cases the chord bridging depends
// on: wholly inside (unchanged), crossing the viewport with both endpoints
// outside (clipped to the visible run), and wholly outside (rejected).
func TestClipSegmentToCanvas(t *testing.T) {
	const w, h = 160, 96
	tests := []struct {
		name               string
		ax, ay, bx, by     float64
		wantOK             bool
		wx0, wy0, wx1, wy1 float64
	}{
		{"inside", 10, 10, 100, 50, true, 10, 10, 100, 50},
		{"crossing", -500, 48, 900, 48, true, 0, 48, 159, 48},
		{"half in", 80, 48, 1e6, 48, true, 80, 48, 159, 48},
		{"outside right", 500, 10, 900, 50, false, 0, 0, 0, 0},
		{"outside above", 10, -900, 100, -500, false, 0, 0, 0, 0},
	}
	for _, tc := range tests {
		x0, y0, x1, y1, ok := clipSegmentToCanvas(tc.ax, tc.ay, tc.bx, tc.by, w, h)
		if ok != tc.wantOK {
			t.Errorf("%s: ok=%v, want %v", tc.name, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if math.Abs(x0-tc.wx0) > 0.5 || math.Abs(y0-tc.wy0) > 0.5 ||
			math.Abs(x1-tc.wx1) > 0.5 || math.Abs(y1-tc.wy1) > 0.5 {
			t.Errorf("%s: clipped to (%.1f,%.1f)→(%.1f,%.1f), want (%.1f,%.1f)→(%.1f,%.1f)",
				tc.name, x0, y0, x1, y1, tc.wx0, tc.wy0, tc.wx1, tc.wy1)
		}
	}
}

// TestPlotDensePolylineCarriesDashPhase: the dot cadence is measured along
// the whole polyline, not restarted at every sample. A run of points one
// pixel apart at step 4 must ink about a quarter of them — restarting the
// phase per segment would ink every one and the "dashed" leg would come out
// solid.
func TestPlotDensePolylineCarriesDashPhase(t *testing.T) {
	c := NewCanvas(60, 30) // 120 × 120 px
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	pts := make([]orbital.Vec3, 0, 81)
	for i := -40; i <= 40; i++ {
		pts = append(pts, orbital.Vec3{X: float64(i)})
	}
	c.PlotDensePolylineColored(pts, arcColor, 4)

	n := len(taggedPixels(c, arcColor))
	if n < 15 || n > 26 {
		t.Errorf("81-point polyline at step 4 inked %d pixels, want ≈21 (the phase must survive the joins)", n)
	}
}

// TestPlotDensePolylineSinglePoint: a one-sample segment still plots its dot
// (the degenerate case the leg draw path hands over at extreme zoom).
func TestPlotDensePolylineSinglePoint(t *testing.T) {
	c := NewCanvas(60, 30)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	c.PlotDensePolylineColored([]orbital.Vec3{{}}, arcColor, 2)
	if n := len(taggedPixels(c, arcColor)); n != 1 {
		t.Errorf("single-point polyline set %d pixels, want 1", n)
	}
}

// TestRingDottedKeepsSpacingAtExtremeRadius: the SOI ring is a screen-space
// circle, and the shipped primitive sampled the WHOLE circumference under a
// 4×(pxW+pxH) cap — so at a zoomed-in radius the samples fell hundreds of
// pixels apart and the visible arc vanished. Restricting the sweep to the
// window the canvas subtends keeps the commanded dot spacing at any radius.
func TestRingDottedKeepsSpacingAtExtremeRadius(t *testing.T) {
	const pxRadius = 200000
	c := NewCanvas(80, 24) // 160 × 96 px
	c.SetScale(1)
	// Put the ring's centre far to the left so the circle sweeps through the
	// canvas: centre x = 80 − pxRadius, radius pxRadius → the ring crosses
	// x ≈ 80.
	c.Center(orbital.Vec3{X: pxRadius})
	c.Clear()
	c.RingDottedColored(orbital.Vec3{}, pxRadius, arcColor)

	dots := taggedPixels(c, arcColor)
	if len(dots) < 15 {
		t.Fatalf("ring at radius %d px inked %d dots on canvas — the visible arc went sparse", pxRadius, len(dots))
	}
	ys := make([]int, 0, len(dots))
	for _, d := range dots {
		ys = append(ys, d[1])
	}
	sort.Ints(ys)
	for i := 1; i < len(ys); i++ {
		if gap := ys[i] - ys[i-1]; gap > ringDotSpacingPx+2 {
			t.Errorf("%d px gap between ring dots at y=%d and y=%d, want ≈%d", gap, ys[i-1], ys[i], ringDotSpacingPx)
			break
		}
	}
}

// TestRingDottedWhollyOffCanvas: a circle that can't reach the viewport is
// rejected by the angular window rather than swept.
func TestRingDottedWhollyOffCanvas(t *testing.T) {
	c := NewCanvas(80, 24)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	// Centre on canvas, radius far beyond the far corner.
	c.RingDottedColored(orbital.Vec3{}, 900_000, arcColor)
	if n := len(taggedPixels(c, arcColor)); n != 0 {
		t.Errorf("ring wholly outside the canvas set %d pixels, want 0", n)
	}
}

// TestVisibleAngleWindowBoundsTheSweep: the window a canvas subtends from a
// distant centre is narrow, which is what makes the ring cost O(visible arc)
// instead of O(circumference).
func TestVisibleAngleWindowBoundsTheSweep(t *testing.T) {
	// Centre far to the left of a 160 × 96 canvas.
	_, span, ok := visibleAngleWindow(-100000, 48, 100000, 160, 96)
	if !ok {
		t.Fatal("window rejected a circle that crosses the canvas")
	}
	if span > 0.01 {
		t.Errorf("span %.4f rad from 100 000 px away — the sweep is not being bounded", span)
	}
	// Centre inside the canvas: every direction can matter.
	if _, span, ok := visibleAngleWindow(80, 48, 40, 160, 96); !ok || math.Abs(span-2*math.Pi) > 1e-9 {
		t.Errorf("centre inside canvas: span=%.4f ok=%v, want 2π", span, ok)
	}
}
