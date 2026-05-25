package sim

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

var errNoActiveCraftToCopy = errors.New("spawn: no active craft to copy")

// surfaceSpawnPosVel computes the **primary-relative** position and
// velocity of a point on `primary`'s rotating surface at the given
// (lat, lon). Uses the renderer's body-fixed coordinate system —
// tilted spin axis, J2000 rotation epoch, per-body texture-offset —
// so a spawn at (28.6083°N, -80.604°E) actually lands on the
// rendered Earth's Florida point. v0.9.2+ (lined up to texture
// in the v0.9.2 fix-3 commit).
//
// Returned (R, V) plug straight into `Spacecraft.State` — the
// integrator's per-tick math adds the primary's heliocentric R/V
// itself when transforming to world coords. Don't put helio
// offsets in `c.State`.
//
// Surface co-rotation velocity uses the body's tilted spin
// angular-velocity vector ω = (2π/period)·n_hat — the same one
// `physics.AtmosphereOmega` returns post-v0.11.2 (ADR 0003) so the
// craft's spawn velocity, drag's atmospheric wind, and the landed
// integrator's per-tick R-rotation all agree to FP precision.
//
// Latitude in degrees north positive (clamped to [-90, 90] by the
// caller). Longitude in degrees east positive (real-Earth-style).
func surfaceSpawnPosVel(
	primary bodies.CelestialBody,
	latitudeDeg float64,
	longitudeDeg float64,
	simTime time.Time,
) (orbital.Vec3, orbital.Vec3) {
	radius := primary.RadiusMeters()
	dirRender := render.BodyFixedToWorld(primary, latitudeDeg, longitudeDeg, simTime)
	rRel := orbital.Vec3{
		X: radius * dirRender.X,
		Y: radius * dirRender.Y,
		Z: radius * dirRender.Z,
	}
	omegaRender := render.BodySpinOmegaWorld(primary)
	omega := orbital.Vec3{X: omegaRender.X, Y: omegaRender.Y, Z: omegaRender.Z}
	vRel := omega.Cross(rRel)
	return rRel, vRel
}

// SpawnSpec describes a craft to spawn. v0.8.2 ships these axes:
//   - LoadoutID:    propulsion archetype. Empty → round-robin via
//     nextLoadoutID().
//   - ParentBodyID: which body to orbit. Empty → active craft's
//     current primary.
//   - AltitudeM:    altitude above the parent's mean radius (m).
//     Zero → 500 km default.
//   - Retrograde:   spawn going retrograde rather than prograde.
//   - Alongside:    spawn within the docking gate of the active
//     craft, matching its velocity. Overrides
//     ParentBodyID + AltitudeM + Retrograde.
//     (v0.8.3+ — for docking testing.)
//
// Future patches may add inclination, a phase-angle offset, etc.
type SpawnSpec struct {
	LoadoutID    string
	ParentBodyID string
	AltitudeM    float64
	Retrograde   bool
	Alongside    bool

	// CustomStages (v0.10.1+) is a player-assembled stage list from
	// the spawn-form stack configurator, bottom-first (same
	// convention as Loadout.Stages). When non-empty it builds the
	// craft via spacecraft.NewFromStages and LoadoutID is ignored —
	// a custom stack has no catalog archetype. Empty (the common
	// case) → the LoadoutID path. Placement (orbit / launchpad /
	// alongside) is orthogonal and still applies.
	CustomStages []spacecraft.Stage

	// Launchpad (v0.9.2+): when true, spawn at altitude 0 on the
	// parent body's surface co-moving with the rotating ground at
	// `Latitude` / `LongitudeOffset` (degrees, north / east positive).
	// Velocity is ω × r (surface co-rotation), so a craft on the
	// pad sits stationary relative to the ground and feels the
	// ~465 m/s eastward boost at the equator from Earth's spin.
	// Overrides AltitudeM + Retrograde + Alongside; ParentBodyID
	// still selects which body's surface (default: active craft's
	// current primary).
	Launchpad bool
	// Latitude is the surface latitude in degrees north positive.
	// Sub-zero values pick southern hemisphere; |Latitude| > 90 is
	// clamped. v0.9.2+.
	Latitude float64
	// LongitudeOffset (v0.9.2+) is the surface longitude offset in
	// degrees east relative to the body's prime meridian at
	// simTime=0 — our pseudo-Greenwich convention. A value of
	// -80.604 places the pad at Cape Canaveral's Earth-relative
	// longitude. Without this offset the spawn longitude depends
	// only on sim time (the body's rotation phase), so consecutive
	// launches at different sim times spawn at different points.
	// With it, "Cape Canaveral" lands at Cape Canaveral regardless
	// of sim time. The Landed bypass continues to rotate the pad
	// with the body once spawned.
	LongitudeOffset float64
}

// DefaultLaunchpadLatitude is the spawn latitude the form uses when
// the player opens it without changing the field. v0.9.2+: pinned
// to LC-39A (Kennedy Space Center, the historical Saturn V launch
// pad) at 28.6083°N. Applied at the form layer, not in SpawnCraft,
// so API callers can explicitly spawn at the equator with
// Latitude=0.
const DefaultLaunchpadLatitude = 28.6083

// DefaultLaunchpadLongitudeEast is the spawn longitude offset (deg
// east of pseudo-Greenwich) for the form's default "Cape Canaveral"
// preset. -80.604° matches LC-39A's Earth-relative longitude.
// v0.9.2+.
const DefaultLaunchpadLongitudeEast = -80.604

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

	var c *spacecraft.Spacecraft
	if len(spec.CustomStages) > 0 {
		// v0.10.1+: player-assembled stack from the configurator.
		// Ignores LoadoutID — a custom craft is not a catalog
		// archetype. NewFromStages returns nil only on an empty
		// slice, already excluded by the len() guard.
		c = spacecraft.NewFromStages(spec.CustomStages)
	} else {
		id := spec.LoadoutID
		if id == "" {
			id = w.nextLoadoutID()
		}
		c = spacecraft.NewFromLoadout(id)
	}
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
		w.SetActiveCraftIdx(len(w.Crafts) - 1)
		w.StopManualBurn()
		w.initCraftAttitude(c)
		return c, nil
	}

	primary := active.Primary
	if spec.ParentBodyID != "" {
		sys := w.System()
		if b := sys.FindBody(spec.ParentBodyID); b != nil {
			primary = *b
		}
	}

	if spec.Launchpad {
		// v0.9.2+: surface spawn co-rotating with the primary's
		// ground. ω × r gives the eastward kick (~465 m/s at
		// Earth's equator). AltitudeM / Retrograde are ignored —
		// the launchpad path defines its own (R, V).
		//
		// Latitude is taken as-passed: zero is the equator, not a
		// sentinel. The form's default (DefaultLaunchpadLatitude =
		// 28.6083° KSC LC-39A) is applied at the form layer, so
		// API callers can spawn at the equator with Latitude=0
		// explicitly. LongitudeOffset places the pad at a fixed
		// Earth-relative longitude (degrees east of pseudo-Greenwich).
		latDeg := spec.Latitude
		if latDeg > 90 {
			latDeg = 90
		}
		if latDeg < -90 {
			latDeg = -90
		}
		rRel, vRel := surfaceSpawnPosVel(primary, latDeg, spec.LongitudeOffset, w.Clock.SimTime)
		c.Primary = primary
		c.State = physics.StateVector{
			R: rRel,
			V: vRel,
			M: c.TotalMass(),
		}
		// v0.9.2+: parked on the surface — the integrator bypasses
		// gravity / drag for Landed craft and recomputes R from
		// (LaunchLatDeg, LaunchLonDeg, simTime) each tick. Cleared
		// automatically when the engine ignites.
		c.Landed = true
		c.LaunchLatDeg = latDeg
		c.LaunchLonDeg = spec.LongitudeOffset
		// v0.9.2.1+: default attitude is radial+ (vertical) so
		// pressing `b` on the pad ignites pointing up — the natural
		// "lift off" gesture. Playtest revealed that the default
		// AttitudeMode (BurnPrograde) at the surface points along
		// surface co-rotation velocity (~east, horizontal), which
		// would slide the craft along the ground instead of lifting
		// it. Player can override with any attitude key before
		// engaging.
		c.AttitudeMode = spacecraft.BurnRadialOut
		w.Crafts = append(w.Crafts, c)
		w.SetActiveCraftIdx(len(w.Crafts) - 1)
		w.StopManualBurn()
		// v0.9.4+: auto-snap NavMode to Surface when launchpad spawn
		// becomes active, mirroring v0.9.3's reconcileNavMode pattern
		// (NavMode auto-snaps to match the frame the player will be
		// flying in). Player can still cycle out via `;`. Idempotent
		// on NavSurface; only lifts NavOrbit. NavTarget never reaches
		// here because SetActiveCraftIdx above already ran
		// reconcileNavMode against the new craft's empty target slot
		// and downgraded NavTarget → NavOrbit.
		if w.NavMode == NavOrbit {
			w.NavMode = NavSurface
		}
		w.initCraftAttitude(c)
		return c, nil
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
	w.SetActiveCraftIdx(len(w.Crafts) - 1)
	w.StopManualBurn()
	w.initCraftAttitude(c)
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
