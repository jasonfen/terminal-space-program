package screens

import (
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// makeActiveEquatorial puts the active vessel on the pre-#566 seed orbit:
// the same 500 km circle at phase 0, but in Earth's equatorial plane. The
// default seed is now inclined (spacecraft.SeedInclinationDeg), so a test
// about equatorial geometry names that plane here instead of inheriting
// whatever the seed is.
func makeActiveEquatorial(w *sim.World) {
	c := w.ActiveCraft()
	eq := spacecraft.NewInLEOAtInclination(c.Primary, 0, 0)
	c.State.R, c.State.V = eq.State.R, eq.State.V
}
