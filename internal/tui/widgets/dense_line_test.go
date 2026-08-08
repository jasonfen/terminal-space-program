package widgets

import (
	"math"
	"sort"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// taggedPixels collects the pixel coords tagged with the given colour, the
// same white-box store ringDottedPixels reads.
func taggedPixels(c *Canvas, color lipgloss.Color) [][2]int {
	var px [][2]int
	c.pixelTags.each(func(x, y int, tag CellTag) {
		if tag.Color == color {
			px = append(px, [2]int{x, y})
		}
	})
	return px
}

// TestPlotDenseLineFillsGap: two world points that project 80 px apart get
// the gap between them filled with a near-contiguous run of dots (step=1),
// rather than just two endpoint pixels — the zoom-constant densification of
// ADR 0023 C. Every dot lies on the (horizontal) segment.
func TestPlotDenseLineFillsGap(t *testing.T) {
	c := NewCanvas(60, 30) // pixel grid 120 × 120
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	color := lipgloss.Color("#33AAFF")
	a := orbital.Vec3{X: -40}
	b := orbital.Vec3{X: 40}
	// Sanity: the endpoints project 80 px apart, so without gap-fill only two
	// pixels would be set.
	ax, ay, _ := c.Project(a)
	bx, by, _ := c.Project(b)
	if ay != by || bx-ax != 80 {
		t.Fatalf("test setup: expected an 80px horizontal segment, got (%d,%d)→(%d,%d)", ax, ay, bx, by)
	}

	c.PlotDenseLineColored(a, b, color, 1)

	dots := taggedPixels(c, color)
	if len(dots) < 70 {
		t.Fatalf("step=1 set only %d pixels over an 80px gap — not densely filled", len(dots))
	}
	xs := make([]int, 0, len(dots))
	for _, d := range dots {
		if d[1] != ay {
			t.Errorf("dot (%d,%d) off the y=%d segment line", d[0], d[1], ay)
		}
		if d[0] < ax || d[0] > bx {
			t.Errorf("dot x=%d outside the segment [%d,%d]", d[0], ax, bx)
		}
		xs = append(xs, d[0])
	}
	// Near-contiguous: no gap larger than the step between consecutive dots.
	sort.Ints(xs)
	for i := 1; i < len(xs); i++ {
		if g := xs[i] - xs[i-1]; g > 1 {
			t.Errorf("gap of %d px between dots at x=%d and x=%d — not contiguous", g, xs[i-1], xs[i])
		}
	}
}

// TestPlotDenseLineStepDashes: step=2 sets roughly half as many pixels as
// step=1 over the same segment — the dashed home-SOI texture vs the solid
// foreign-SOI fill.
func TestPlotDenseLineStepDashes(t *testing.T) {
	color := lipgloss.Color("#33AAFF")
	a := orbital.Vec3{X: -40}
	b := orbital.Vec3{X: 40}

	solid := NewCanvas(60, 30)
	solid.SetScale(1)
	solid.Center(orbital.Vec3{})
	solid.Clear()
	solid.PlotDenseLineColored(a, b, color, 1)
	nSolid := len(taggedPixels(solid, color))

	dashed := NewCanvas(60, 30)
	dashed.SetScale(1)
	dashed.Center(orbital.Vec3{})
	dashed.Clear()
	dashed.PlotDenseLineColored(a, b, color, 2)
	nDashed := len(taggedPixels(dashed, color))

	if nSolid == 0 || nDashed == 0 {
		t.Fatalf("empty lines: solid=%d dashed=%d", nSolid, nDashed)
	}
	if nDashed >= nSolid {
		t.Errorf("dashed (step=2) set %d pixels vs solid %d — not visibly dashed", nDashed, nSolid)
	}
	if want := nSolid / 2; nDashed < want-5 || nDashed > want+5 {
		t.Errorf("dashed set %d pixels, want ≈%d (half of solid)", nDashed, want)
	}
}

// TestPlotDenseLineLongChordClippedAndBridged: a chord longer than the
// canvas (here an on-canvas point to one far off-canvas) is CLIPPED to the
// viewport and its visible run drawn — ADR 0042 §3 replacing ADR 0023's
// long-chord refusal, which used to drop such a pair back to endpoint dots
// and so dissolved a zoomed-in trajectory into dust. Iteration stays bounded
// by the canvas, not by the projected distance.
func TestPlotDenseLineLongChordClippedAndBridged(t *testing.T) {
	c := NewCanvas(60, 30) // pixel grid 120 × 120
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	color := lipgloss.Color("#33AAFF")
	a := orbital.Vec3{}          // canvas centre, on-canvas
	b := orbital.Vec3{X: 100000} // far off the right edge
	c.PlotDenseLineColored(a, b, color, 1)

	dots := taggedPixels(c, color)
	cx, cy, _ := c.Project(a)
	if len(dots) < 50 {
		t.Fatalf("long chord set %d pixels, want the visible run (~%d, centre→right edge)", len(dots), 120-cx)
	}
	for _, d := range dots {
		if d[1] != cy {
			t.Errorf("pixel %v off the y=%d line", d, cy)
		}
		if d[0] < cx || d[0] >= 120 {
			t.Errorf("pixel %v outside the visible run [%d,119]", d, cx)
		}
	}
}

// TestPlotDenseLineForcedBridgesLongChord: the forced variant (for genuine
// straight sightlines — a CommNet relay link) DOES bridge a chord longer than
// the canvas, drawing the visible run all the way toward a far off-screen
// endpoint — the case the guarded variant deliberately refuses (compare
// TestPlotDenseLineLongChordNotBridged).
func TestPlotDenseLineForcedBridgesLongChord(t *testing.T) {
	c := NewCanvas(60, 30) // 120 × 120 px, centre (60,60)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	color := lipgloss.Color("#34E2D0")
	a := orbital.Vec3{}          // centre, on-canvas
	b := orbital.Vec3{X: 100000} // far off the right edge
	c.PlotDenseLineForcedColored(a, b, color, 1)

	px := taggedPixels(c, color)
	cx, cy, _ := c.Project(a) // (60,60)
	if len(px) < 50 {
		t.Fatalf("forced long chord set %d pixels, want the visible run (~%d, centre→right edge)", len(px), 120-cx)
	}
	for _, p := range px {
		if p[1] != cy {
			t.Errorf("pixel %v off the y=%d line", p, cy)
		}
		if p[0] < cx || p[0] >= 120 {
			t.Errorf("pixel %v outside the visible run [%d,119]", p, cx)
		}
	}
}

// TestPlotDenseLineForcedClipsStraddle: a forced chord with BOTH endpoints
// off-canvas on opposite sides (the guarded variant draws nothing — both
// endpoint dots are off-screen) draws its full visible run, clipped to the
// canvas, with bounded iteration.
func TestPlotDenseLineForcedClipsStraddle(t *testing.T) {
	c := NewCanvas(60, 30) // 120 × 120 px, centre (60,60)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	color := lipgloss.Color("#34E2D0")
	a := orbital.Vec3{X: -100} // → pixel x = -40 (off-left)
	b := orbital.Vec3{X: 100}  // → pixel x = 160 (off-right)
	c.PlotDenseLineForcedColored(a, b, color, 1)

	px := taggedPixels(c, color)
	if len(px) < 100 {
		t.Errorf("forced straddle set %d pixels, want ~120 (the full visible width)", len(px))
	}
	for _, p := range px {
		if p[0] < 0 || p[0] >= 120 || p[1] < 0 || p[1] >= 120 {
			t.Errorf("pixel %v outside canvas bounds (clip failed)", p)
		}
	}
}

// TestPlotDenseLineOffCanvasSkipped: a chord lying wholly off one edge sets
// nothing and returns promptly (the same-off-edge guard), so a zoomed-in
// leg's off-screen samples cost nothing.
func TestPlotDenseLineOffCanvasSkipped(t *testing.T) {
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	color := lipgloss.Color("#33AAFF")
	// Both points far to the right of the 80px-wide canvas.
	c.PlotDenseLineColored(orbital.Vec3{X: 1e6}, orbital.Vec3{X: 1.0001e6}, color, 1)
	if n := len(taggedPixels(c, color)); n != 0 {
		t.Errorf("off-canvas chord set %d pixels, want 0", n)
	}
}

// nonFinitePoints enumerates the ways a world point can stop being a
// point. Each is fed as the FAR endpoint of an otherwise ordinary chord
// whose near endpoint sits dead centre on the canvas.
var nonFinitePoints = []struct {
	name string
	p    orbital.Vec3
}{
	{"NaN x", orbital.Vec3{X: math.NaN()}},
	{"NaN y", orbital.Vec3{Y: math.NaN()}},
	{"+Inf x", orbital.Vec3{X: math.Inf(1)}},
	{"-Inf y", orbital.Vec3{Y: math.Inf(-1)}},
	{"NaN both", orbital.Vec3{X: math.NaN(), Y: math.NaN()}},
}

// TestNonFinitePointsDrawNothing is the review regression for the freeze
// the ADR 0042 §3 float rewrite opened. Liang-Barsky cannot reject a NaN
// endpoint on its own — every comparison against NaN is false, so all four
// of clipSegmentToCanvas's return-false branches are skipped and it used to
// report ok=true with NaN endpoints. The walkers then read segLenPx's 1e9
// NaN clamp as a real clipped length and iterated a BILLION times: a single
// PlotDensePolylineColored call measured at ~18 seconds, i.e. the whole TUI
// wedged, not one dropped line.
//
// Asserted as "no ink" rather than "returns quickly": a wall-clock bound
// would be a flaky way to state a correctness property, and zero pixels is
// the honest requirement — a non-finite point has no place on the canvas.
// (The loop bound follows: nothing can iterate a billion times and still
// set no pixels.)
func TestNonFinitePointsDrawNothing(t *testing.T) {
	color := lipgloss.Color("#33AAFF")
	centre := orbital.Vec3{}
	for _, bad := range nonFinitePoints {
		t.Run(bad.name, func(t *testing.T) {
			for _, draw := range []struct {
				name string
				fn   func(c *Canvas)
			}{
				{"PlotDenseLineColored", func(c *Canvas) {
					c.PlotDenseLineColored(centre, bad.p, color, 1)
				}},
				{"PlotDenseLineForcedColored", func(c *Canvas) {
					c.PlotDenseLineForcedColored(centre, bad.p, color, 1)
				}},
				{"PlotDensePolylineColored", func(c *Canvas) {
					c.PlotDensePolylineColored([]orbital.Vec3{centre, bad.p, centre}, color, 1)
				}},
				{"PlotPolylineClass/Planned", func(c *Canvas) {
					c.PlotPolylineClass([]orbital.Vec3{centre, bad.p, centre}, color, ClassPlanned)
				}},
				{"PlotPolylineClass/Real", func(c *Canvas) {
					c.PlotPolylineClass([]orbital.Vec3{centre, bad.p, centre}, color, ClassReal)
				}},
			} {
				c := NewCanvas(60, 30)
				c.SetScale(1)
				c.Center(orbital.Vec3{})
				c.Clear()
				draw.fn(c)
				if n := len(taggedPixels(c, color)); n != 0 {
					t.Errorf("%s inked %d pixels for a %s endpoint, want 0", draw.name, n, bad.name)
				}
			}
		})
	}
}

// TestNonFiniteEndpointKeepsDashPhase: a bad sample must not silently
// shift the cadence of the rest of the curve either. A non-finite chord
// contributes no arc length, so the dots after it land exactly where they
// would have with the bad point simply absent.
func TestNonFiniteEndpointKeepsDashPhase(t *testing.T) {
	color := lipgloss.Color("#33AAFF")
	run := func(pts []orbital.Vec3) [][2]int {
		c := NewCanvas(60, 30)
		c.SetScale(1)
		c.Center(orbital.Vec3{})
		c.Clear()
		c.PlotDensePolylineColored(pts, color, 3)
		px := taggedPixels(c, color)
		sort.Slice(px, func(i, j int) bool {
			if px[i][0] != px[j][0] {
				return px[i][0] < px[j][0]
			}
			return px[i][1] < px[j][1]
		})
		return px
	}
	clean := run([]orbital.Vec3{{X: -40}, {X: 40}})
	// The same visible chord with a NaN excursion spliced into the middle:
	// the NaN legs draw nothing and advance nothing, so what remains is the
	// two half-chords at the same phase they had in `clean`.
	spliced := run([]orbital.Vec3{{X: -40}, {X: math.NaN()}, {X: -40}, {X: 40}})
	if len(clean) == 0 {
		t.Fatal("precondition: the clean chord inked nothing")
	}
	if len(spliced) != len(clean) {
		t.Fatalf("NaN excursion changed the dot count: %d vs %d", len(spliced), len(clean))
	}
	for i := range clean {
		if spliced[i] != clean[i] {
			t.Fatalf("dot %d moved to %v (want %v) — a NaN sample shifted the dash phase", i, spliced[i], clean[i])
		}
	}
}

// TestClipSegmentToCanvasRefusesNonFinite pins the guard at the level it
// actually lives, so a future caller reaching for the clipper directly
// inherits the same protection.
func TestClipSegmentToCanvasRefusesNonFinite(t *testing.T) {
	cases := [][4]float64{
		{math.NaN(), 0, 10, 10},
		{0, math.NaN(), 10, 10},
		{0, 0, math.NaN(), 10},
		{0, 0, 10, math.NaN()},
		{math.Inf(1), 0, 10, 10},
		{0, 0, math.Inf(-1), 10},
	}
	for _, c := range cases {
		if _, _, _, _, ok := clipSegmentToCanvas(c[0], c[1], c[2], c[3], 120, 120); ok {
			t.Errorf("clipSegmentToCanvas%v = ok, want refused", c)
		}
	}
	// A merely enormous (but finite) coordinate still clips normally — the
	// guard must not become a reason to drop a real off-screen chord.
	if _, _, _, _, ok := clipSegmentToCanvas(60, 60, 1e18, 60, 120, 120); !ok {
		t.Error("a huge finite endpoint was refused; only non-finite ones should be")
	}
}
