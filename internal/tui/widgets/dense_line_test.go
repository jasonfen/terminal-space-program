package widgets

import (
	"sort"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// taggedPixels collects the pixel coords tagged with the given colour, the
// same white-box store ringDottedPixels reads.
func taggedPixels(c *Canvas, color lipgloss.Color) [][2]int {
	var px [][2]int
	for coord, tag := range c.pixelTags {
		if tag.Color == color {
			px = append(px, coord)
		}
	}
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
