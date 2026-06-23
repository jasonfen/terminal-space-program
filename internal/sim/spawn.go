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
	// Inclination (v0.17+) tilts the spawned circular orbit off the
	// parent's equator by this many degrees, measured in the body-
	// equatorial frame. Zero (the common case) spawns equatorial,
	// byte-identical to the pre-v0.17 placement. The orbit stays
	// circular; only the plane tilts (ascending node along body-frame
	// +Y, the spawn-position axis). Used by the --inclination CLI flag.
	Inclination float64
	Alongside   bool

	// CustomStages (v0.10.1+) is a player-assembled stage list from
	// the spawn-form stack configurator, bottom-first (same
	// convention as Loadout.Stages). When non-empty it builds the
	// craft via spacecraft.NewFromStages and LoadoutID is ignored —
	// a custom stack has no catalog archetype. Empty (the common
	// case) → the LoadoutID path. Placement (orbit / launchpad /
	// alongside) is orthogonal and still applies.
	CustomStages []spacecraft.Stage

	// NosePayloadPlan (v0.14 / ADR 0011) is the top-release counterpart
	// to a Loadout's bottom-up DecouplePlan: a list of how many
	// contiguous TOP stages of CustomStages form a docked nose payload
	// (released by Undock, not Staging) rather than linear firing-core
	// stages. v0.14 honours a single entry — one nose payload, itself
	// possibly multi-stage (the Apollo LM = [Descent, Ascent]). When set
	// (and CustomStages is non-empty), SpawnCraft splits the stack at the
	// seam, builds the core and payload, and assembles them into a ready
	// docked composite — so a CSM+LM spawns already in the
	// post-transposition shape. Nil/empty ⇒ a plain linear custom craft.
	NosePayloadPlan []int

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
	// The active craft is only required by the Alongside path (which
	// clones its state) and as the *default* parent body. Launchpad and
	// orbit-at-parent spawns place the craft from the spec alone, so
	// they must work from an empty slate — e.g. spawning a fresh vessel
	// after end-flight removed the last one. The nil guard therefore
	// lives on the Alongside branch, not up here.
	active := w.ActiveCraft()

	var c *spacecraft.Spacecraft
	if len(spec.CustomStages) > 0 {
		// v0.10.1+: player-assembled stack from the configurator.
		// Ignores LoadoutID — a custom craft is not a catalog
		// archetype. NewFromStages returns nil only on an empty
		// slice, already excluded by the len() guard.
		//
		// v0.14 / ADR 0011: a NosePayloadPlan marks a contiguous TOP
		// group as a docked nose payload, so the stack spawns as an
		// assembled composite (core firing, payload Undock-able) rather
		// than a linear chain. A malformed plan falls back to linear.
		c = newCustomCraft(spec.CustomStages, spec.NosePayloadPlan)
	} else {
		id := spec.LoadoutID
		if id == "" {
			id = w.nextLoadoutID()
		}
		c = spacecraft.NewFromLoadout(id)
	}
	c.Name = w.nextCraftName(c.Name)

	if spec.Alongside {
		// Alongside clones the active craft's state + system, so it
		// genuinely needs one. (Other paths don't reach here.)
		if active == nil {
			return nil, errNoActiveCraftToCopy
		}
		// v0.8.3+: place the new craft inside the docking gate of
		// the active craft, matching its velocity. The 25 m offset
		// is half the docking distance — close enough that one or
		// two RCS taps null residuals to dock, far enough that the
		// craft don't immediately auto-fuse before the player can
		// see the spawn.
		const offsetM = 25.0
		c.Primary = active.Primary
		// v0.16 / ADR 0015: an Alongside spawn clones the active Vessel's
		// state, so it inherits the *active* Vessel's System (not the
		// viewed one); view-follows-active then keeps them co-framed.
		c.SystemIdx = active.SystemIdx
		c.State = physics.StateVector{
			R: active.State.R.Add(orbital.Vec3{X: offsetM}),
			V: active.State.V,
			M: c.TotalMass(),
		}
		w.stampCraftID(c) // stable identity before the craft enters the slate (ADR 0012)
		w.Crafts = append(w.Crafts, c)
		w.SetActiveCraftIdx(len(w.Crafts) - 1)
		w.StopManualBurn()
		w.focusNewCraft()
		w.initCraftAttitude(c)
		return c, nil
	}

	// Default parent is the active craft's primary; an explicit
	// ParentBodyID overrides. With an empty slate (no active craft) and
	// no/unknown parent id, fall back to the system primary so a
	// post-end-flight spawn still resolves a real body.
	var primary bodies.CelestialBody
	if active != nil {
		primary = active.Primary
	}
	if spec.ParentBodyID != "" {
		sys := w.System()
		if b := sys.FindBody(spec.ParentBodyID); b != nil {
			primary = *b
		}
	}
	if primary.ID == "" {
		if sys := w.System(); len(sys.Bodies) > 0 {
			primary = sys.Bodies[0]
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
		// v0.16 / ADR 0015: bind the new Vessel to the viewed System.
		c.SystemIdx = w.SystemIdx
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
		// v0.11.4+ (ADR 0004): mark this Landed-true vessel as on the
		// launchpad. The ViewLaunch auto-route handler gates on
		// `OnPad && Landed=false→true`, so a fresh launchpad spawn
		// fires the route but a post-flight soft-landed touchdown
		// (OnPad already cleared on liftoff) doesn't rip the player
		// into ViewLaunch mid-landing. Cleared in
		// StartManualBurn / planted-burn-fire alongside Landed.
		c.OnPad = true
		// v0.9.2.1+: default attitude is radial+ (vertical) so
		// pressing `b` on the pad ignites pointing up — the natural
		// "lift off" gesture. Playtest revealed that the default
		// AttitudeMode (BurnPrograde) at the surface points along
		// surface co-rotation velocity (~east, horizontal), which
		// would slide the craft along the ground instead of lifting
		// it. Player can override with any attitude key before
		// engaging.
		c.AttitudeMode = spacecraft.BurnRadialOut
		w.stampCraftID(c) // stable identity before the craft enters the slate (ADR 0012)
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
		w.focusNewCraft()
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
	// v0.17+: tilt the circular orbit off the equator by spec.Inclination.
	// The spawn point (+Y) is the ascending node, so the plane rotates
	// about the +Y axis: the prograde in-plane velocity (-X) tips toward
	// +Z by the inclination angle. At i=0 this is exactly {X: -v} (and the
	// retrograde sign flows through v), so the equatorial path is unchanged.
	inc := spec.Inclination * math.Pi / 180
	vBody := orbital.Vec3{X: -v * math.Cos(inc), Z: v * math.Sin(inc)}
	c.Primary = primary
	// v0.16 / ADR 0015: bind the new Vessel to the viewed System.
	c.SystemIdx = w.SystemIdx
	c.State = physics.StateVector{
		R: frame.ToWorld(rBody),
		V: frame.ToWorld(vBody),
		M: c.TotalMass(),
	}
	w.stampCraftID(c) // stable identity before the craft enters the slate (ADR 0012)
	w.Crafts = append(w.Crafts, c)
	w.SetActiveCraftIdx(len(w.Crafts) - 1)
	w.StopManualBurn()
	w.focusNewCraft()
	w.initCraftAttitude(c)
	return c, nil
}

// focusNewCraft centers the orbit camera on the just-spawned (now active)
// craft, mirroring NewWorld's seed (Focus = FocusCraft). v0.16 / ADR 0015:
// the browse-to-another-System-then-spawn flow reaches the spawn via
// CycleSystem, which leaves Focus at FocusSystem; without this a launchpad
// spawn's exit-ViewLaunch (releaseLaunchSession restores ViewMode but not
// Focus) would land on the bare system map instead of the craft. Guarded on
// CraftVisibleHere so FocusCraft is only set when it will render (it needs
// the active craft's System to be the viewed one — always true post-spawn,
// since SetActiveCraftIdx has just snapped the view to it).
func (w *World) focusNewCraft() {
	if w.CraftVisibleHere() {
		w.Focus = Focus{Kind: FocusCraft}
	}
}

// newCustomCraft builds the Spacecraft for a player-assembled stack.
// With no nose-payload plan it is a plain linear craft (NewFromStages,
// the v0.10.1 behaviour). With an N-entry plan (v0.14 single payload / v0.23
// ADR 0028 C3-1 generalized to N) it splits the top stages off as docked
// nose payloads: each plan entry is a count of contiguous TOP stages forming
// one payload, ordered **top-down** (entry 0 is the topmost payload, e.g.
// `[2,1]` = a 2-stage top payload over a 1-stage payload, over the carrier
// core). The result is a composite whose Stages are the full stack (carrier
// core at the bottom, firing) with DockedComponents recording the core and
// every payload so Undock (docking.go) releases them and Deploy (ADR 0028 C3-2)
// pops the top payload one press at a time. The composite flies as the core,
// the surviving firing vehicle. A malformed plan (any entry ≤ 0, or the
// payloads consuming the whole stack with no core left) degrades to a linear
// craft rather than erroring, so a stray plan can never strand the spawn.
//
// DockedComponents are ordered **bottom-to-top** — core first, then each
// payload from the bottom of the stack up — so the in-order concatenation of
// component stages equals the composite's Stages. Undock peels stages
// sequentially off that order; Deploy pops the **last** (topmost) component.
//
// This reuses dockedComponentFromStages — the same helper Transpose
// (staging.go) uses to wrap the CSM core and LM nose payload — so a
// spawned composite is byte-for-byte the configuration a hand-flown
// dock or the `D` transpose key produces, and rides the identical
// Undock / save-load machinery (no schema bump).
func newCustomCraft(stages []spacecraft.Stage, nosePayloadPlan []int) *spacecraft.Spacecraft {
	c := spacecraft.NewFromStages(stages)
	if c == nil || len(nosePayloadPlan) == 0 {
		return c
	}
	n := len(stages)
	total := 0
	for _, k := range nosePayloadPlan {
		if k <= 0 {
			return c // malformed entry — leave it a linear craft
		}
		total += k
	}
	if total >= n {
		return c // payloads leave no carrier core — malformed, stay linear
	}

	coreStages := append([]spacecraft.Stage(nil), stages[:n-total]...)
	coreName := vehicleNameForStages(coreStages)
	comps := make([]spacecraft.DockedComponent, 0, len(nosePayloadPlan)+1)
	comps = append(comps, dockedComponentFromStages(coreStages, coreName, "custom"))

	// Walk the plan bottom-up: the LAST entry sits just above the core, the
	// FIRST entry is the topmost payload. Peeling upward from the core seam
	// keeps comps in composite-stage order (Undock's sequential peel + Deploy's
	// top-pop both depend on it).
	off := n - total
	for i := len(nosePayloadPlan) - 1; i >= 0; i-- {
		k := nosePayloadPlan[i]
		payloadStages := append([]spacecraft.Stage(nil), stages[off:off+k]...)
		off += k
		comps = append(comps, dockedComponentFromStages(
			payloadStages, vehicleNameForStages(payloadStages), payloadRoleForStages(payloadStages)))
	}
	c.DockedComponents = comps

	// Identity: fly as the core (the surviving firing vehicle), not the
	// top-of-stack payload that NewFromStages defaulted the name/marker to.
	c.Name = coreName
	c.Glyph = coreStages[0].Glyph
	c.Color = coreStages[0].Color
	c.SyncFields()
	return c
}

// vehicleNameForStages picks a friendly name for a contiguous stage
// group used as a docked composite component (ADR 0011). It recognises
// the Apollo halves so the CSM+LM composite reads as "CSM" / "LM"; any
// other group falls back to its surviving-core (top) stage name — the
// same identity rule NewFromStages uses.
func vehicleNameForStages(stages []spacecraft.Stage) string {
	has := func(name string) bool {
		for _, s := range stages {
			if s.Name == name {
				return true
			}
		}
		return false
	}
	switch {
	case has("SM") && has("CM"):
		return "CSM"
	case has("Descent") && has("Ascent"):
		return "LM"
	}
	if top := stages[len(stages)-1].Name; top != "" {
		return top
	}
	return "Custom"
}

// payloadRoleForStages assigns a role to a nose-payload component so the
// undocked craft surfaces sensibly — a soft-land-capable payload (the LM)
// becomes a "lander", matching what Transpose records. v0.14.
func payloadRoleForStages(stages []spacecraft.Stage) string {
	for _, s := range stages {
		if s.CanSoftLand {
			return "lander"
		}
	}
	return "payload"
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
