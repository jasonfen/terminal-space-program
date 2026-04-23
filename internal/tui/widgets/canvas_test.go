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
