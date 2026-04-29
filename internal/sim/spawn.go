package sim

import (
	"errors"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

var errNoActiveCraftToCopy = errors.New("spawn: no active craft to copy")

// SpawnSisterCraft adds a sister copy of the active craft to the
// slate, offset 90° around the same primary in a 500 km circular
// prograde orbit. After spawn, the new craft becomes the active
// one so the player can immediately fly it.
//
// v0.8.1 ships this as the minimum-viable multi-craft spawn — one
// keystroke (`n`) drops a fresh craft into the slate. The proper
// SpawnSpec form (parent body cycle / altitude knob / prograde
// toggle / craft-type cycle) is a follow-up patch. Returns the
// newly-spawned craft so callers can flash a status message.
func (w *World) SpawnSisterCraft() (*spacecraft.Spacecraft, error) {
	active := w.ActiveCraft()
	if active == nil {
		return nil, errNoActiveCraftToCopy
	}
	primary := active.Primary
	mu := primary.GravitationalParameter()
	r := primary.RadiusMeters() + 500e3
	v := math.Sqrt(mu / r)

	// Offset 90° around the primary from the original — primary
	// position at +X, sister at +Y. Same speed, prograde direction
	// rotates with the position so velocity points at +Y for the
	// original, -X for the sister (still tangential, prograde).
	dry := 11000.0
	fuel := 40000.0
	mp, monoCap, rcsThrust, rcsIsp := spacecraft.DefaultRCSLoadout(dry)
	sister := &spacecraft.Spacecraft{
		Name:             "S-IVB-1",
		DryMass:          dry,
		Fuel:             fuel,
		Isp:              421,
		Thrust:           1023000,
		Throttle:         1.0,
		Monoprop:         mp,
		MonopropCapacity: monoCap,
		RCSThrust:        rcsThrust,
		RCSIsp:           rcsIsp,
		Primary:          primary,
		State: physics.StateVector{
			R: orbital.Vec3{Y: r},
			V: orbital.Vec3{X: -v},
			M: dry + fuel + mp,
		},
	}
	w.Crafts = append(w.Crafts, sister)
	// Active swaps to the new craft so the player can immediately
	// see and fly it. Drop any in-flight manual burn since it was
	// tied to the prior active craft.
	w.ActiveCraftIdx = len(w.Crafts) - 1
	w.StopManualBurn()
	return sister, nil
}
