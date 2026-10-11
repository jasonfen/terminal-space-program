package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/render"
)

// The atmosphere halo ring is Backdrop (review #582 M2-S18; the v0.51.4
// row: it "no longer cuts a dark gap into the planet's edge"). Drawn as
// strong ink the ring wins the cells it shares with the planet's limb and
// moats the limb's dots. Zoomed in on a low orbit, the shipped ring owns 53
// cells; with the tag off it owns 65.
func TestAtmosphereHaloRingIsBackdrop(t *testing.T) {
	w := lowOrbitTargetingSeed(t)
	earth := w.ActiveCraft().Primary
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	for i := 0; i < 6; i++ {
		v.ZoomIn()
	}
	v.Render(w, 0, DesignWidth, DesignHeight)
	haze := v.canvas.CountColor(render.AtmosphereHazeColor(earth))
	if haze == 0 {
		t.Fatal("setup: no halo ring on the canvas")

	}
	if haze > 59 {
		t.Errorf("the halo ring owns %d cells, want <= 59: it is winning cells off the planet's limb (not Backdrop)", haze)
	}
}
