package widgets

import (
	"math"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// ringDottedPixels collects the pixel coords RingDottedColored tagged with
// the given colour — white-box over pixelTags, the same store CountColor
// aggregates.
func ringDottedPixels(c *Canvas, color lipgloss.Color) [][2]int {
	var px [][2]int
	for coord, tag := range c.pixelTags {
		if tag.Color == color {
			px = append(px, coord)
		}
	}
	return px
}

// TestRingDottedColoredDotsLieOnCircle: every dot sits at the requested
// pixel radius from the projected center (±1.5 px rounding), and the dots
// cover all four quadrants — a ring, not an arc or a smear.
func TestRingDottedColoredDotsLieOnCircle(t *testing.T) {
	c := NewCanvas(60, 30) // pixel grid 120 × 120
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()

	const pxRadius = 40
	color := lipgloss.Color("#602A72")
	c.RingDottedColored(orbital.Vec3{}, pxRadius, color)

	dots := ringDottedPixels(c, color)
	if len(dots) == 0 {
		t.Fatal("RingDottedColored set no pixels")
	}
	cx, cy, _ := c.Project(orbital.Vec3{})
	quadrants := map[[2]bool]bool{}
	for _, d := range dots {
		dx, dy := float64(d[0]-cx), float64(d[1]-cy)
		r := math.Hypot(dx, dy)
		if math.Abs(r-pxRadius) > 1.5 {
			t.Errorf("dot (%d,%d) sits %.1f px from center, want %d ±1.5", d[0], d[1], r, pxRadius)
		}
		quadrants[[2]bool{dx >= 0, dy >= 0}] = true
	}
	if len(quadrants) < 4 {
		t.Errorf("dots cover only %d quadrants, want 4 — not a full ring", len(quadrants))
	}
}

// TestRingDottedColoredIsSparse: the dotted ring sets roughly one pixel
// per ringDotSpacingPx of circumference — visibly sparser than the solid
// ring primitive at the same radius, so the boundary reads as a quiet
// dotted cue rather than solid ink.
func TestRingDottedColoredIsSparse(t *testing.T) {
	const pxRadius = 40
	color := lipgloss.Color("#602A72")

	dotted := NewCanvas(60, 30)
	dotted.SetScale(1)
	dotted.Center(orbital.Vec3{})
	dotted.Clear()
	dotted.RingDottedColored(orbital.Vec3{}, pxRadius, color)
	nDotted := len(ringDottedPixels(dotted, color))

	solid := NewCanvas(60, 30)
	solid.SetScale(1)
	solid.Center(orbital.Vec3{})
	solid.Clear()
	solid.RingColoredOutline(orbital.Vec3{}, pxRadius, color)
	nSolid := len(ringDottedPixels(solid, color))

	if nDotted == 0 || nSolid == 0 {
		t.Fatalf("empty rings: dotted=%d solid=%d", nDotted, nSolid)
	}
	if nDotted*2 >= nSolid {
		t.Errorf("dotted ring has %d pixels vs solid %d — not visibly dotted", nDotted, nSolid)
	}
	// And roughly the commanded density: one dot per ~ringDotSpacingPx of
	// circumference (rounding collapses a few neighbours).
	want := 2 * math.Pi * pxRadius / ringDotSpacingPx
	if float64(nDotted) < want*0.6 || float64(nDotted) > want*1.4 {
		t.Errorf("dotted ring has %d pixels, want ≈%.0f (±40%%)", nDotted, want)
	}
}

// TestRingDottedColoredHugeRadiusDoesNotHang mirrors the v0.5.15 guard on
// the other ring primitives: an extreme-zoom radius must stay bounded by
// the 4×(pxW+pxH) sample cap instead of looping per circumference pixel.
// If the cap regresses, this test never finishes — same sentinel style as
// TestRingOutlineHugeRadiusDoesNotHang.
func TestRingDottedColoredHugeRadiusDoesNotHang(t *testing.T) {
	c := NewCanvas(40, 20)
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	c.Clear()
	c.RingDottedColored(orbital.Vec3{}, 500_000_000, lipgloss.Color("#602A72"))
}
