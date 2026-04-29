package sim

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

var errNoActiveCraftToCopy = errors.New("spawn: no active craft to copy")

// nextCraftName returns a name for the next spawned craft of the
// given prototype name (e.g. "S-IVB-1"). The trailing `-N` suffix
// is bumped so consecutive spawns become "S-IVB-2", "S-IVB-3", …
// regardless of what the user has named existing crafts. v0.8.1+:
// keeps each spawned vessel visually distinguishable in HUDs.
func (w *World) nextCraftName(proto string) string {
	prefix, _ := splitNumericSuffix(proto)
	highest := 0
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		p, n := splitNumericSuffix(c.Name)
		if p != prefix {
			continue
		}
		if n > highest {
			highest = n
		}
	}
	return fmt.Sprintf("%s-%d", prefix, highest+1)
}

// splitNumericSuffix returns ("S-IVB", 3) for "S-IVB-3", or
// (name, 0) if the name has no `-N` tail.
func splitNumericSuffix(name string) (string, int) {
	idx := strings.LastIndex(name, "-")
	if idx < 0 {
		return name, 0
	}
	suffix := name[idx+1:]
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return name, 0
	}
	return name[:idx], n
}

// SpawnSisterCraft adds a new craft to the slate, offset 90° around
// the same primary as the active craft in a 500 km circular prograde
// orbit. The new craft cycles through `spacecraft.LoadoutOrder` so
// consecutive spawns produce visually-distinct vessels (S-IVB-1 →
// ICPS → RCS-tug → Lander → wraps). After spawn the new craft
// becomes active so the player can immediately fly it.
//
// v0.8.1 shipped a single-loadout sister spawn; v0.8.2 cycles
// through the loadout catalog so multi-craft slates exercise the
// new craft-type variety automatically. The full SpawnSpec form
// (parent-body / altitude / direction / explicit loadout pick)
// lands as a v0.8.2.x follow-up.
func (w *World) SpawnSisterCraft() (*spacecraft.Spacecraft, error) {
	active := w.ActiveCraft()
	if active == nil {
		return nil, errNoActiveCraftToCopy
	}
	primary := active.Primary
	mu := primary.GravitationalParameter()
	r := primary.RadiusMeters() + 500e3
	v := math.Sqrt(mu / r)

	// Offset 90° around the primary — primary at the inertial
	// origin, sister at +Y, prograde points toward -X.
	loadoutID := w.nextLoadoutID()
	sister := spacecraft.NewFromLoadout(loadoutID)
	sister.Name = w.nextCraftName(sister.Name)
	sister.Primary = primary
	sister.State = physics.StateVector{
		R: orbital.Vec3{Y: r},
		V: orbital.Vec3{X: -v},
		M: sister.TotalMass(),
	}
	w.Crafts = append(w.Crafts, sister)
	w.ActiveCraftIdx = len(w.Crafts) - 1
	w.StopManualBurn()
	return sister, nil
}

// nextLoadoutID picks the next loadout in the spawn-rotation —
// cycling through LoadoutOrder so a player who spawns multiple
// craft sees variety. Counts existing loadouts and returns the
// first one not yet flown, falling back to wrap-around when the
// slate has every type.
func (w *World) nextLoadoutID() string {
	used := make(map[string]int, len(spacecraft.LoadoutOrder))
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		id := c.LoadoutID
		if id == "" {
			id = spacecraft.LoadoutSIVB1ID
		}
		used[id]++
	}
	// Prefer un-spawned loadouts; once all four are flown, spawn
	// the least-used (which rotates the cycle on subsequent calls).
	bestID := spacecraft.LoadoutOrder[0]
	bestCount := used[bestID]
	for _, id := range spacecraft.LoadoutOrder {
		if used[id] < bestCount {
			bestID = id
			bestCount = used[id]
		}
	}
	return bestID
}
