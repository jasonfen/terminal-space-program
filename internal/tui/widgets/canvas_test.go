package widgets

import (
	"testing"

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

func onlyWhitespace(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\n' && r != '\t' && r != '⠀' {
			return false
		}
	}
	return true
}
