package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The maneuver planner draws its planet as Backdrop and its own orbit in
// ColorCurrentOrbit (review #582 M2-S16/S16b). Untagged white, or a planet
// that is not Backdrop, lost every cell the orbit shared with the disk.
func TestManeuverPlannerOrbitShowsOverItsPlanet(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 400e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}

	m := NewManeuver(Theme{})
	m.Render(w, 120, 40, 0)
	earth := render.ColorFor(c.Primary)
	own, planet := m.canvas.CountColor(render.ColorCurrentOrbit), m.canvas.CountColor(earth)
	if planet == 0 {
		t.Fatal("setup: the planner drew no planet")
	}
	if own == 0 {
		t.Fatal("the planner's own orbit is not ColorCurrentOrbit (untagged ink counts as no colour)")
	}
	// Measured: 43 cells. A planet that is not Backdrop wins the shared
	// cells: 25 (review S16); an untagged orbit: 0 (S16b).
	if own < 38 {
		t.Errorf("your orbit shows in %d planner cells, want >= 38: the planet is outvoting it", own)
	}
}
