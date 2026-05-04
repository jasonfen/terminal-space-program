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

// SpawnSpec describes a craft to spawn. v0.8.2 ships these axes:
//   - LoadoutID:    propulsion archetype. Empty → round-robin via
//                   nextLoadoutID().
//   - ParentBodyID: which body to orbit. Empty → active craft's
//                   current primary.
//   - AltitudeM:    altitude above the parent's mean radius (m).
//                   Zero → 500 km default.
//   - Retrograde:   spawn going retrograde rather than prograde.
//   - Alongside:    spawn within the docking gate of the active
//                   craft, matching its velocity. Overrides
//                   ParentBodyID + AltitudeM + Retrograde.
//                   (v0.8.3+ — for docking testing.)
//
// Future patches may add inclination, a phase-angle offset, etc.
type SpawnSpec struct {
	LoadoutID    string
	ParentBodyID string
	AltitudeM    float64
	Retrograde   bool
	Alongside    bool
}

// SpawnCraft adds a new craft to the slate using the given spec.
// The new craft is placed in a circular orbit at the requested
// altitude, 90° around the parent body from the active craft's
// position (or +Y if no offset is meaningful). After spawn the new
// craft becomes active. v0.8.2+.
func (w *World) SpawnCraft(spec SpawnSpec) (*spacecraft.Spacecraft, error) {
	active := w.ActiveCraft()
	if active == nil {
		return nil, errNoActiveCraftToCopy
	}

	id := spec.LoadoutID
	if id == "" {
		id = w.nextLoadoutID()
	}
	c := spacecraft.NewFromLoadout(id)
	c.Name = w.nextCraftName(c.Name)

	if spec.Alongside {
		// v0.8.3+: place the new craft inside the docking gate of
		// the active craft, matching its velocity. The 25 m offset
		// is half the docking distance — close enough that one or
		// two RCS taps null residuals to dock, far enough that the
		// craft don't immediately auto-fuse before the player can
		// see the spawn.
		const offsetM = 25.0
		c.Primary = active.Primary
		c.State = physics.StateVector{
			R: active.State.R.Add(orbital.Vec3{X: offsetM}),
			V: active.State.V,
			M: c.TotalMass(),
		}
		w.Crafts = append(w.Crafts, c)
		w.ActiveCraftIdx = len(w.Crafts) - 1
		w.StopManualBurn()
		return c, nil
	}

	primary := active.Primary
	if spec.ParentBodyID != "" {
		sys := w.System()
		if b := sys.FindBody(spec.ParentBodyID); b != nil {
			primary = *b
		}
	}

	alt := spec.AltitudeM
	if alt <= 0 {
		alt = 500e3
	}
	mu := primary.GravitationalParameter()
	r := primary.RadiusMeters() + alt
	v := math.Sqrt(mu / r)
	if spec.Retrograde {
		v = -v
	}

	// v0.8.6+: spawn into the primary's equatorial plane (ECI / MCI
	// convention) rather than the world ecliptic. Body-frame +Y
	// position with prograde velocity along -X — rotates into world
	// coords applying the body's axial tilt so an Earth-orbit spawn
	// passes over the equator (Ecuador), not over the world XY plane
	// (which crosses Earth at ~23°N). The +Y / -X orientation
	// preserves the pre-v0.8.6 90° offset from the default LEO craft
	// (which sits at body-frame +X).
	frame := orbital.ReferenceFrameForPrimary(primary)
	rBody := orbital.Vec3{Y: r}
	vBody := orbital.Vec3{X: -v}
	c.Primary = primary
	c.State = physics.StateVector{
		R: frame.ToWorld(rBody),
		V: frame.ToWorld(vBody),
		M: c.TotalMass(),
	}
	w.Crafts = append(w.Crafts, c)
	w.ActiveCraftIdx = len(w.Crafts) - 1
	w.StopManualBurn()
	return c, nil
}

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

// SpawnSisterCraft is the auto-pick variant of SpawnCraft: it
// delegates with an empty SpawnSpec, which round-robins the
// loadout cycle. Kept as a convenience for tests / the v0.8.1
// rapid-spawn behaviour. v0.8.2+: the orbit screen routes
// through a SpawnCraft form for explicit loadout pick.
func (w *World) SpawnSisterCraft() (*spacecraft.Spacecraft, error) {
	return w.SpawnCraft(SpawnSpec{})
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
