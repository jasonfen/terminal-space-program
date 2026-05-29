# Terminal Space Program

A terminal-native orbital-mechanics rocket simulator: a single Go binary running
a Bubble Tea TUI over a patched-conic physics core. This glossary defines the
domain language shared by the simulation, the TUI, and the planner.

## Language

**Vessel**:
A craft in the simulation — the thing flying through space, performing burns,
and being targeted by the camera. Each Vessel has its own state (position,
velocity, mass, stages, target binding, nav mode). The slate of Vessels is
held by `World.Crafts`; exactly one is the Active Vessel at any time.
_Avoid_: Spacecraft, Craft, Ship, Rocket. The Go type is currently named
`Spacecraft` and field shorthands say `Craft` (`Crafts`, `ActiveCraftIdx`,
`FocusCraft`); these are legacy and should drift toward `Vessel`.

### Vessel construction & lifecycle

**Loadout**:
A template/recipe for a Vessel — `ID`, `Name`, `Role`, `Glyph`, `Color`,
and a list of Stages. The catalog (`internal/spacecraft/loadouts.go`) of
designs the player picks from at spawn time; `NewFromLoadout` instantiates
a Vessel from one. Loadouts also carry per-vehicle tuning like
`SlewRateDegPerSec`.
_Avoid_: Template, Blueprint, Design.

**Stage**:
One decoupleable propulsion module on a Vessel. Convention: `Stages[0]` is
the *bottom* stage (the currently-firing engine, the next to be jettisoned)
and `Stages[len-1]` is the *top* stage (the player's "core" — the only one
left after every lower stage has been decoupled). Carries dry mass, fuel
mass/capacity, thrust, Isp, and ballistic coefficient. **Staging** is the
act of jettisoning `Stages[0]`; the popped stage spawns as its own passive
Vessel. Single-stage Vessels can't be staged (no-op + status flash).
_Avoid_: Booster (one kind of stage, not all stages), Tank, Section.

**Launch Sprite**:
The per-Stage **braille-pixel silhouette** rendered by the ViewLaunch
chase-cam, conveying a Vessel's stack composition during launch. Each
Stage paints a (`LaunchSpriteWidthPx` × `LaunchSpriteRowsPx`) filled
rectangle of braille dots via `PlotColored` in the Stage's catalog
color, both dimensions in sub-pixel (= half-cell) units. Stages stack
bottom-to-top from `Stages[0]` (lowest in screen, firing engine) to
`Stages[len-1]` (payload, highest) along the Vessel's
`CurrentAttitudeDir` projected into the chase-cam basis — so a
gravity-turned rocket leans smoothly at any pitch (braille dots are
direction-agnostic, so no glyph-rotation problem). Per-Stage identity
reads from color + height + width; the LUT's body-fixed
`SetCellOverlay` glyphs are cleared at rocket-pixel cells so the
braille dots show through at the pad.

Stage convention: `LaunchSpriteRowsPx` / `LaunchSpriteWidthPx` are
*stylised* (chosen so the stack reads well at typical pad-launch
zoom), not derived from real metres. Zero `LaunchSpriteRowsPx` means
"no sprite, fall back to the Vessel-level `Glyph`." Zero
`LaunchSpriteWidthPx` falls back to a 2-sub-pixel default
(the pre-v0.11.5 universal width). Practical width range [1, 5].

**Engine Bell**: a synthetic single-row flare painted between
`Stages[0]`'s bottom edge and the flame, at width `min(stage.width +
2, 7)`, in the stage's color. Renders whenever `Stages[0]` has
`launchSpriteWidthPx ≥ 2`, `launchSpriteRowsPx ≥ 4`, and `Thrust > 0`
(a pure-monoprop RCS-tug as bottom stage gets no bell). Inferred from
geometry, not authored — no catalog field. The bell is *hardware*, so
it renders regardless of throttle; the flame attaches below it and
inherits the bell's width so exhaust visibly emerges from the nozzle.

**Inter-stage Taper**: an optional synthetic 1-row taper between
adjacent stages of different `LaunchSpriteWidthPx`, painted at
`round((lower.width + upper.width) / 2)` in the lower stage's color.
Renders only when *both* adjacent stages have
`LaunchSpriteRowsPx ≥ 6` (the `taperThreshold`) — short stages hard-
step. The catalog author controls whether the overall envelope reads
as "stepped rocket" or smooths toward a triangle by tuning which
stage boundaries match in width (= no taper) vs differ.

**Landing Legs**: a Lander-class hardware feature painted as two
diagonal lines of sub-pixels splaying outward and downward from
`Stages[0]`'s bottom corners to foot-pad positions roughly level with
or just past the flame tip. Mirrored about the stack axis; constants
`legSpreadX = 2` sub-pixels outward and `legSpreadY = 3` sub-pixels
downward give the iconic LM splay. Renders only when `Stages[0]` has
`LaunchSpriteHasLegs == true` (opt-in catalog flag). Painted in the
stage's color (legs = descent-stage hardware). Defined in the
`(stack-dir, width-dir)` basis so the legs lean with the rocket
during gravity-turns, preserving the direction-agnostic invariant.

Below `Stages[0]`'s engine bell a **flame** of the same braille
primitive extends along `-CurrentAttitudeDir`, length-binned by
Throttle (4 / 8 / 12 sub-pixels), pulsed by a wall-clock 100 ms frame
shift. Flame color is looked up from `Stages[0].FuelType` via a
fixed palette (see [[#maneuver--thrust|Fuel Type]]); empty / unknown
FuelType falls back to amber `render.ColorWarning` for backward
compatibility with un-catalogued stages. The flame renders only while
the Vessel has an active `ManualBurn` or `ActiveBurn` — pad-spawn
loadout-default Throttle=1.0 alone does not paint flame.

**RCS Puff**: small bright-white dots painted at recent RCS-pulse
positions in both ViewLaunch and OrbitView. Two-shade (bright-white
origin dot near the craft, dim-grey tip dot farther along the
exhaust direction) so the puff visibly points away from the craft.
Source: `World.RCSPuffs()` ring buffer; renders for any vessel,
fades out by age-fraction. The white-vs-FuelType-coloured-flame
contrast encodes "RCS = small cold puff, main engine = hot coloured
plume" without an explicit HUD readout.

History: the v0.11.3 first cut used multi-line ASCII glyphs
(`╔╗ ║║ ▓▓ ╚╝`); the playtest showed box-drawing characters smear at
gravity-turn angles (the 2-col-wide width axis runs perpendicular to
a near-horizontal stack and the cells split across terminal rows).
The braille pivot replaced the ASCII string with `LaunchSpriteRowsPx
int` mid-cycle (v0.11.3). v0.11.5 added per-stage width, taper, bell,
fuel-type flame colour, white RCS puffs, and Lander landing-legs as a
bundled silhouette-polish slice.
_Avoid_: Sprite (bare — collides with the launch-tower LUT sprite and
the body-texture sprites; qualify as Launch Sprite in launch-render
contexts), Stage art, Rocket art.

**Active Vessel**:
The single Vessel the player is currently flying — indexed by
`World.ActiveCraftIdx`. Only the Active Vessel responds to controls
(throttle, attitude, staging), and most TUI screens default their bindings
to it. Other Vessels in the slate are *passive*: still propagated by the
integrator, still rendered, still subject to maneuver nodes that fire on
schedule — but inert to live input until the player switches active.
_Avoid_: Current craft, Selected vessel (Selection means Cursor), Player
vessel.

### Selection & view

These three are easily confused but independent. The Camera (Focus) can be on
one thing while the active Vessel's Target is on another, and the Cursor is
ephemeral browsing state that only becomes a Target on commit.

**Focus**:
What the OrbitView camera is centered on — `FocusSystem` (heliocentric overview),
`FocusCraft` (active Vessel), or `FocusBody` (a body). World-level view state,
not per-Vessel. Independent of Target, and independent of
[[#view--projection|ViewMode]] — Focus picks *what* the camera centres on; ViewMode
picks *which projection* it uses.
_Avoid_: Camera, View (informal synonyms — `Focus` is the canonical name).

**Cursor**:
The body the player is currently hovering in the OrbitView browse list,
before committing it. Ephemeral TUI state (`selectedIdx`). Becomes a Target
when the player commits it; otherwise has no effect on simulation or planner.
_Avoid_: Selection, Highlight.

**Target**:
What the active Vessel has committed as its aim slot — consumed by the
maneuver planner, navball, and inclination/transfer planners. Each Vessel
stores its own Target (`TargetNone` / `TargetBody` / `TargetCraft` /
`TargetSite`), so per-Vessel target binding persists across active-Vessel
switches. `TargetNone` is the zero value and means consumers fall back to
their kind-less defaults.
_Avoid_: Aim, Lock, Selection.

**NavMode**:
The orientation the navball displays directions in. Three values:
**NavOrbit** (frame of the active Vessel's current orbit), **NavSurface**
(rotating frame of the Primary's surface — local up / horizon), **NavTarget**
(target-relative frame, only meaningful when a TargetCraft Target is bound).
Unlike Focus and Target, NavMode lives on `World`, not on the Vessel — it's
a known scope cap (each Vessel does not yet remember its own NavMode across
active-Vessel switches); revisit if that asymmetry starts to bite.
_Avoid_: Navball mode, Nav frame.

### Maneuver & thrust

The plan is the **Maneuver Node**; the execution is the **Burn**; the visual
preview of what the node will do is the **Projected Orbit**. These three are
distinct lifecycle stages of the same intent.

**Maneuver Node**:
A planted future intent — delta-v vector, trigger event, optional target
binding, burn mode. The plan, not the execution. Player plants, edits, and
deletes nodes; the simulator fires them when the trigger event arrives.
_Avoid_: Burn (a Node is not a Burn until it fires), Plan, Waypoint.

**Burn**:
The execution of thrust: the engine is firing, the integrator has switched
from Verlet to RK4, and mass is decreasing per the rocket equation. Two
sub-modes exist in code — *Active Burn* (a Maneuver Node firing on schedule)
and *Manual Burn* (the player throttling directly without a node) — but
both are Burns. Warp is clamped during a Burn.
_Avoid_: Maneuver (that's the plan), Thrust (an instantaneous force, not
the firing episode), Ignition (only the start of a Burn).

**Projected Orbit**:
The orbit the Vessel will be on after a planted Maneuver Node fires —
computed in the post-burn reference frame and rendered as a dashed track
on the OrbitView. Updates live as the player edits the Node.
_Avoid_: Predictor (that's the internal package name, not the player-facing
artifact), Preview Orbit, Predicted Orbit.

**Burn Mode**:
The direction selector for a Maneuver Node or Manual Burn. Two families:

- *Self-relative* — direction is a function of the Vessel's own state.
  Six modes: **Prograde** / **Retrograde** (along ±velocity), **NormalPlus** /
  **NormalMinus** (along ±orbit-normal h), **RadialOut** / **RadialIn**
  (away from / toward Primary).
- *Target-relative* — direction is a function of the Vessel's state and a
  bound TargetCraft (target binding captured at plant time, snapshotted
  for finite burns). The conceptual model is the full 6×2 product —
  every self-relative mode has a target-relative twin computed against
  closing kinematics. Currently implemented: **TargetPrograde** / **TargetRetrograde**
  (along ± closing velocity) and **Target** (toward target). The other
  three (TargetNormalPlus/Minus, TargetRadialOut/In) are not yet
  implemented but would round out the product.

Target-relative modes are unselectable when no TargetCraft is bound.

A third, narrow family sits outside the self/target product:

- *Fixed-inertial* — **BurnVector** (v0.12+), a plant-only mode that
  fires along a unit direction captured in the inertial frame *at
  plant time* and held fixed (it does not re-derive from live state
  like the others). It exists to carry an arbitrary 3D departure Δv —
  specifically the fused-Lambert combined-transfer departure (see
  Transfer Plan), where the impulse folds a plane change into the
  raise and so points along no self-relative axis. The Vessel slews
  to the captured direction like any planted node.

_Avoid_: Vector mode (now ambiguous with BurnVector — say "Burn Mode"
for the selector, "BurnVector" for the fixed-inertial value), Heading,
Direction.

**Trigger Event**:
The condition that fires a Maneuver Node. Six options:

- *Absolute* — fires at a wall-clock-equivalent T+ time set at plant.
- *NextPeri* / *NextApo* — fires at the next periapsis / apoapsis crossing.
- *NextAN* / *NextDN* — fires at the next Ascending / Descending Node.
- *NextClosestApproach* — fires at the next closest approach to a bound
  TargetCraft. Only selectable when `World.Target.Kind == TargetCraft` at
  plant time; uses the same target snapshot mechanism as target-relative
  Burn Modes.

Non-Absolute events are lazy-resolved on the first tick after plant
(frozen once resolved so the Node can't chase a moving target time).
_Avoid_: Trigger condition, Firing rule.

**Trigger Time**:
The burn-**CENTER** moment recorded on a Maneuver Node — the planner's
intended firing point, not the burn start. For an Impulsive Burn
(Duration = 0) center == start == TriggerTime. For a Finite Burn the
integrator actually starts engine ignition at TriggerTime − Duration/2,
so the burn is centered on TriggerTime. Resolving a non-Absolute
Trigger Event writes the resolved instant into TriggerTime.
_Avoid_: BurnTime, FireTime (both read as "start of burn" — wrong by
half a Duration).

**Impulsive Burn** / **Finite Burn**:
Two execution modes for a Maneuver Node, selected by `Duration`:

- *Impulsive Burn* — `Duration == 0`. Instantaneous Δv applied at
  Trigger Time. The legacy v0.1 path; still used for planning math
  where finite-burn cosine loss is negligible.
- *Finite Burn* — `Duration > 0`. Sustained engine fire lasting up to
  Duration, or until DV is delivered, whichever first. RK4 propagation
  with rocket-equation mass loss. Warp is clamped to ≤10× during a
  Finite Burn.

A Manual Burn (player throttling without a Node) is always finite in
the same sense — the integrator runs the same RK4 path — but doesn't
carry a Duration field because the player decides when to cut off.
_Avoid_: Instant burn, Continuous burn.

**Fuel Type**:
The propellant-chemistry family of a Stage's main engine. Enumerable
and small: **Kerolox** (RP-1 / LOX — F-1, Merlin), **Hydrolox** (LH2 /
LOX — J-2, RS-25, RL-10), **Hypergolic** (Aerozine 50 / N2O4 — LM
descent, SPS), **Solid** (APCP — SLS SRB). Stored on the Stage as
catalog data, not derived from physical numbers. Drives the Launch
Sprite's flame colour via a fixed palette (kerolox = orange, hydrolox
= pale cyan, hypergolic = yellow-amber, solid = orange-red), so a
Saturn V's S-IC reads orange while its hydrolox upper stages read
pale-cyan — visible mid-flight stage character without HUD reads.
Distinct from [[#attitude--rcs|Monopropellant]], which names the RCS
propellant family rather than a main-engine fuel.
_Avoid_: Propellant Class (verbose), Engine Type (engine = hardware,
fuel-type = chemistry), Fuel (bare — means "main-engine propellant
mass" elsewhere in the codebase).

### Bodies & systems

**Body**:
A thing in space with gravity — the umbrella noun covering stars, planets,
and moons. The physics core treats all Bodies uniformly (mass, orbital
elements, SOI radius); Planet vs Moon distinction lives in the catalog
layer and drives planner selection, not the integrator.
_Avoid_: CelestialBody (the Go type name — verbose, leaks the package
boundary into prose).

**Planet**:
A Body whose Primary is the System's star. Carries a `Moons []Moon` list.
Interplanetary transfers between Planets use Hohmann / porkchop / Lambert
planners from heliocentric orbit.
_Avoid_: World (overloaded — `sim.World` is the simulator state, not a
planet).

**Moon**:
A Body whose Primary is a Planet (encoded as `AroundPlanet` on the
catalog entry). Encounters with Moons use the dedicated `moon_escape`
planner, not the heliocentric transfer planners — the math and the UI
flow are different.
_Avoid_: Satellite (ambiguous with player-launched vessels), Subplanet.

**Primary**:
The Body that another Body or Vessel orbits — the gravitational parent
in the SOI hierarchy. Relative, not absolute: the Sun is Kerbin's
Primary, Kerbin is Mun's Primary. Used to name reference frames
("rebase to the destination's primary frame") and to walk the
patched-conic SOI tree.
_Avoid_: Parent (ambiguous with scene-graph parents), Center,
Barycenter (the System's index-0 body may be a star, not a barycenter).

**System**:
A named collection of Bodies orbiting a common Primary — Sol, Kerbol,
or a user-supplied overlay. Loaded from embedded `systems/*.json` plus
user files in `$XDG_CONFIG_HOME`; user files win on `systemName`
collision. Save files carry a `body_catalog_hash` so loading a save
under a different System catalog fails fast.
_Avoid_: Galaxy, Universe, World.

**Sphere of Influence (SOI)**:
The spherical region around a Body inside which that Body's gravity
dominates and the patched-conic approximation treats it as the sole
attractor. The SOI boundary is where the integrator hands off from
one Primary to another (`internal/physics/soi`). Player-visible: the
HUD announces entries and exits, and warp clamps near boundaries.
_Avoid_: Hill sphere (mathematically distinct), Gravity well.

### Launch & landing

How a Vessel sits on (and gets put on) the surface of a Body. The
runtime state is **Landed**; the spawn-time placement that creates a
Landed Vessel is **Launchpad**; the physics fallback when an aerobraking
Vessel penetrates the surface is **Surface Contact** (which, despite
appearances, does *not* set Landed — see Flagged ambiguities).

**Landed**:
A Vessel state in which the integrator bypasses gravity, drag, and
thrust, and instead recomputes the Vessel's position each tick from
its body-fixed touchdown coordinates (`LandedLatDeg` / `LandedLonDeg`
when non-zero — for soft-landed vessels — falling back to
`LaunchLatDeg` / `LaunchLonDeg`, the original spawn site) using the
Primary's current rotation phase. Position rides the Primary's
body-fixed frame; velocity is set to ω × r each tick so a future
un-Landed transition releases the Vessel with full surface co-rotation
velocity — the ~465 m/s eastward boost at Earth's equator. Player
concept: the Vessel is *parked on the ground, moving with the ground*.

Set by:
- A **Launchpad** spawn (also sets [[#launchpad|OnPad]] true).
- A controlled-descent **Touchdown** — a Vessel with `CanSoftLand`
  arriving at the surface with `|V| < V_CRIT` and nose-aligned with
  local-vertical satisfies the touchdown predicate at the
  `physics.ClampToSurface` site and becomes Landed without the OnPad
  flag (v0.11.4+).

Cleared by engine ignition — either a Manual Burn (player presses
`b`) or a planted Maneuver Node firing on schedule. The clearing
transition releases the Vessel into normal integration with the
surface co-rotation velocity it had at the moment of ignition.
_Avoid_: On the pad (loses generality — Landed is a runtime state,
not a place; soft-landed Vessels are Landed too), Parked, Surface
Park, Grounded.

**OnPad**:
A flag on a Vessel that's *currently* sitting at its original
Launchpad spawn awaiting first ignition. Set true by a **Launchpad**
spawn, cleared on the first `Landed=false` transition (i.e., the
moment the rocket first leaves the pad). Distinct from **Landed**:
a soft-landed Vessel that returned from flight is Landed=true,
OnPad=false. Used to gate the [[#view--projection|ViewLaunch]] auto-
route handler, which fires only on `OnPad && Landed-transition` —
soft-land touchdowns don't drag the player into a chase-cam view.
v0.11.4+.
_Avoid_: PreLaunch, AwaitingIgnition (verbose), Fresh.

**Crashed**:
A terminal Vessel state caused by a destructive surface arrival —
contact velocity above `V_CRIT`, off-vertical attitude, or a Vessel
without `CanSoftLand` regardless of how gently it touched. Set at
the `physics.ClampToSurface` site when the Touchdown predicate fails.
Integration is skipped (no gravity, no drag, no slew); rendering uses
the composed-stage sprite in `ColorDim` with no engine flame.
Crashed Vessels persist in the slate as visible wreckage at the
impact point until the player invokes **End Flight** (`E` key,
confirm prompt) to remove them from the world.

Player concept: the Vessel is destroyed; the wreckage is a marker,
not a controllable craft. Auto-switch active to the next Vessel in
the slate on end-flight; falls back to no active Vessel if the
wreckage was the only one. v0.11.4+.
_Avoid_: Destroyed (used in casual prose but not the canonical
state name), Killed, Dead, Wreckage (the *visible artefact* of a
Crashed Vessel, not the state itself).

**Launchpad**:
A Vessel spawn variant that places the new Vessel on the surface of a
Body at altitude 0, co-rotating with the ground. Distinct from the
default *orbital* spawn (drops the Vessel into a circular orbit at
altitude) and the *alongside* spawn (clones the active Vessel's state
with a small offset for docking practice). Selected from the spawn
form's position-mode toggle (orbit / alongside / launchpad).

Side effects of a Launchpad spawn, all baked into the spawn path:

- Sets the Vessel to **Landed** so the integrator begins the body-fixed
  bypass on the first tick.
- Initializes attitude to **RadialOut** (straight up — vertical ascent)
  so pressing `b` ignites pointing skyward; the prior default of
  Prograde points along surface co-rotation velocity (eastward along
  the ground), which would slide the Vessel sideways rather than
  lift it off.
- Auto-snaps [[#attitude--rcs|NavMode]] from NavOrbit to NavSurface
  (idempotent on NavSurface; never lifts NavTarget here because the
  active-Vessel switch has already downgraded NavTarget when the
  new Vessel has no bound TargetCraft).

Where on the Body the Vessel is placed is a separate decision — see
**Launch Site**.
_Avoid_: Pad spawn, Surface spawn (ambiguous with future non-launchpad
surface placements), Ground spawn.

**Launch Site**:
A (latitude, longitude) pair on a Body that a **Launchpad** spawn
places the Vessel at. The spawn form ships a preset cycle — default
is "KSC" (28.6083°N, -80.604°E east of the Body's prime meridian),
matching Kennedy Space Center's LC-39A. Latitude is degrees north
positive; longitude is degrees east positive of the Body's
**pseudo-Greenwich** prime meridian (the longitude line that aligns
with world +X at simTime = 0). Without an explicit longitude offset
a Vessel's spawn longitude would depend only on sim time (the Body's
rotation phase), so successive launches at different sim times would
spawn at different points; with it, KSC stays at KSC regardless of
the clock.

Currently a UI-layer preset list (the spawn form), not a first-class
named entity in the simulation — once spawned, the Vessel carries only
the resolved `LaunchLatDeg` / `LaunchLonDeg` numbers, not the site
name.
_Avoid_: Spawn site, Launch coordinates, Pad location, KSC (a specific
Launch Site, not the general term).

**Surface Contact**:
The physics event that fires inside the integrator when a Vessel's
sub-step puts |R| below the Primary's mean radius. As of v0.11.4 the
clamp site evaluates the **Touchdown** predicate (`CanSoftLand`
catalog gate + `|V_impact| < V_CRIT` + nose-alignment > NOSE_TOL)
and routes to one of three outcomes:

- **Touchdown** — predicate satisfied → Vessel becomes **Landed**
  at the impact (lat, lon).
- **Crash** — predicate fails on velocity / attitude / capability →
  Vessel becomes **Crashed**.
- **Fallback (vestigial)** — predicate doesn't qualify but the
  Vessel isn't destroyed either (e.g., a non-`CanSoftLand` Vessel
  that grazed the surface gently). R is projected back to the
  surface, V zeroed, no lifecycle flag set. The pre-v0.11.4
  placeholder behaviour; expected to disappear in v0.12+ once
  playtest confirms every contact qualifies for one of the two
  modelled outcomes.

Without the clamp the gravity singularity at r → 0 would slingshot
the Vessel back out at huge velocity.
_Avoid_: Crash (use **Crashed** for the runtime state; **Surface
Contact** is the physics event that decides which outcome fires),
Ground hit, Impact (acceptable casual prose, but Surface Contact
is the canonical term for the integrator event).

**Touchdown** / **Crash**:
The two outcomes of a Vessel arriving at a Body's surface,
distinguished by the kinematic + capability predicate evaluated at
the **Surface Contact** site:

- **Touchdown** — a controlled arrival within velocity and
  orientation tolerances, by a Vessel designed to land. Predicate:
  `vessel.CanSoftLand && |V_impact| < V_CRIT && nose·local_up >
  NOSE_TOL`. Produces a **Landed** Vessel that co-rotates with the
  ground, preserves fuel and stage state, and can re-ignite for
  liftoff (the same end-state a **Launchpad** spawn creates, but
  earned through controlled descent rather than spawned in).
- **Crash** — an arrival outside those tolerances — excess descent
  velocity, off-vertical attitude, or a Vessel without
  `CanSoftLand` regardless of how gently it touched. Produces a
  **Crashed** Vessel (terminal state, removed via End Flight).

Differentiated in code as of v0.11.4 (see
[`docs/adr/0004-crashed-landed-lifecycle.md`](adr/0004-crashed-landed-lifecycle.md)).
Constants `V_CRIT = 10 m/s`, `NOSE_TOL = 0.7` (≈ 45° from
local-vertical); both retunable.
_Avoid_: Soft landing / Hard landing (longer; "Landing" alone
overloads with the cluster heading), Successful landing / Unsuccessful
landing (asymmetric and verbose).

### Atmosphere & drag

How a Body's gaseous envelope decelerates a Vessel moving through it.
Cluster anchors: **Atmosphere** is the per-Body model; **Drag** is the
force it produces; **Ballistic Coefficient** is the per-Vessel
tunability. The codebase's BC is the *inverse* of aerospace-standard
BC — see Flagged ambiguities.

**Atmosphere**:
A per-Body exponential-density model of its gaseous envelope —
optional, absent for vacuum Bodies (most Moons, asteroids). Parametrised
by **Scale Height**, **Surface Density**, and **Cutoff Altitude**;
density falls off exponentially with altitude up to the cutoff, then is
treated as identically zero (a hard edge, not a smooth taper). Drives
**Drag** on Vessels passing through it and the haze tint the renderer
paints near the limb.
_Avoid_: Air (informal — exception: **Air-Relative Velocity**), Atmo
(in prose), Sky.

**Atmospheric Density** (ρ):
Mass per unit volume of the **Atmosphere** at a given altitude,
computed from the Body's model: ρ(h) = ρ₀ · exp(-h / H), zero above
**Cutoff Altitude**. The multiplicative scalar in the **Drag**
acceleration; halving ρ halves Drag. Reads ρ₀ = **Surface Density** and
H = **Scale Height** from the Body's catalog entry.
_Avoid_: Air density (acceptable casual prose), Density (ambiguous
with body mass density).

**Scale Height** (H):
The altitude increment over which **Atmospheric Density** drops by a
factor of 1/e. Larger H = thicker atmosphere reaching higher; Earth's
H ≈ 8.5 km, so density at 17 km is ~14% of sea level. Per-Body, set in
the catalog.
_Avoid_: H (in prose; reserve for formulas), Density scale.

**Surface Density** (ρ₀):
**Atmospheric Density** at altitude 0 — the anchor of the exponential
profile. Per-Body, set in the catalog. Player-relevant mainly at
liftoff, where it sets the maximum **Drag** a launching Vessel sees.
_Avoid_: Sea-level density (correct on Earth but most Bodies don't have
a sea), Ground density.

**Cutoff Altitude**:
The altitude above a Body at which **Atmospheric Density** is treated as
identically zero — no Drag, no haze contribution. A hard sentinel, not
a smooth taper; physically the atmosphere extends further but the
contribution is negligible. Per-Body, set in the catalog. Player concept:
"above the cutoff" = in vacuum.
_Avoid_: Kármán line (Earth-specific term loaded with historical and
regulatory baggage; the simulator's cutoff is purely numerical), Top
of atmosphere.

**Drag**:
The retarding acceleration on a Vessel moving through a Body's
**Atmosphere**, opposing the **Air-Relative Velocity**:
a = −0.5 · ρ · |v_rel|² · BC · v̂_rel. Magnitude scales with the square
of relative speed and linearly with **Atmospheric Density** and
**Ballistic Coefficient**. Zero outside the atmosphere (vacuum Body,
altitude above **Cutoff Altitude**, or BC ≤ 0).
_Avoid_: Air resistance (correct everyday prose, but Drag is the
canonical term), Friction (wrong — drag is dynamic-pressure, not
contact friction).

**Air-Relative Velocity** (v_rel):
The Vessel's velocity relative to the co-rotating **Atmosphere** —
v_rel = v − ω × r, where ω is the Primary's spin angular-velocity
vector and r is the Vessel's position. The vector **Drag** opposes;
a Vessel sitting motionless in inertial frame at Earth's equator has
|v_rel| ≈ 465 m/s eastward and feels Drag against that flow. For an
orbiting Vessel, |v_rel| is slightly less than inertial speed when
prograde and slightly more when retrograde (Earth's surface rotates
~6% as fast as LEO orbital speed).

**Engineering caveat:** the ω used by Drag is **Z-aligned** (the
atmosphere is modelled as co-rotating about world +Z at one sidereal
period), *not* the tilted spin axis used elsewhere — notably the
**Launchpad**-spawn surface co-rotation velocity, which uses the full
tilted axis. The two ω's agree for zero-tilt Bodies; for tilted ones
(Earth at ~23.4°) they disagree by a few percent in magnitude plus a
Z component. A launchpad-spawned Vessel's spawn velocity (tilted ω × r)
therefore drifts slightly from Drag's "wind" (Z-aligned ω × r); the
residual aerodynamic force at altitude 0 is typically negligible. Drag
keeps the approximation for back-compat with v0.8.4.
_Avoid_: Wind-relative velocity (misleading — the atmosphere co-rotates,
it doesn't blow), Atmospheric velocity (ambiguous — reads as "the
atmosphere's velocity"), Relative velocity (overloaded — encounter math
uses "relative velocity" between two Vessels).

**Ballistic Coefficient** (BC):
The per-Stage tunability of **Drag** — how draggy a Vessel is per unit
dynamic pressure. Defined in this codebase as `BC = C_D · A / m` (units
m²/kg), the multiplicative factor in the Drag acceleration, so **higher
BC means draggier**. This is the *inverse* of the aerospace-standard
BC (`BC = m / (C_D · A)`, kg/m², higher = less draggy because a heavier
projectile sheds speed slower) — see Flagged ambiguities for the
reciprocal-conversion rule when importing values from aerospace tables.

Per-Stage primarily — each **Stage** carries its own BC so a Saturn V
launch can model a heavier draggier boost stage and a sleeker upper
stage without one field on the Vessel. The effective BC the integrator
multiplies is the **bottom stage's** (`Stages[0]`) — the
currently-firing / outermost stage is the one in the flow. Falls back
to a Vessel-level legacy field for saves predating per-Stage BC, then
to the default 0.01 m²/kg (the S-IVB-1 baseline — a "generic launcher"
draggability anchor). A Vessel with `BC ≤ 0` feels no Drag at all.
_Avoid_: Drag coefficient (collides with the aerospace dimensionless
C_D), Drag factor (no inheritance), B-coefficient.

**Aerobraking**:
Using **Drag** deliberately to lose orbital energy — apoapsis reduction
without spending Δv. In the strict sense: a series of shallow periapsis
passes that gradually lower apoapsis over many orbits. Real-mission
technique (Mars Reconnaissance Orbiter spent ~6 months aerobraking
down to its science orbit); KSP players use it for fuel-light planetary
captures.

Drag is drag — the simulator has no dedicated "aerobraking mode." Two
related variants share the same physics, differentiated only by **how
much** Drag is applied, **for how long**, and what **external
adjustments** (periapsis-raise burns, attitude trims) the player makes
between passes:

- **Aerocapture** — a single *deep* pass that converts an arrival
  hyperbola into a captured orbit on first contact. The aggressive
  variant; the entire energy delta happens in one atmospheric arc.
- **Re-entry** — descent through the atmosphere intended to reach the
  surface (controlled or otherwise). The Vessel does not come back out;
  the pass ends in **Surface Contact**.

Pre-condition for any of the three: a low enough periapsis to dip below
**Cutoff Altitude**. None has dedicated planner or HUD support — the
player flies the geometry, the integrator does the rest.
_Avoid_: Aerobrake (verb, fine in prose; the noun is Aerobraking),
Atmospheric braking (correct but verbose).

### Attitude & RCS

How the Vessel points itself, rotates between commanded headings, and
uses its secondary propulsion system. Distinct from Maneuver & thrust:
that section covers *where* the Δv goes; this one covers *how the
nose gets there*.

**Attitude**:
The Vessel's physical nose direction — a unit vector
(`CurrentAttitudeDir`) in the same frame as its state. Distinct from
velocity: a Vessel can coast prograde while pointing radial-out.
Two states matter:

- *Actual attitude* — where the nose actually points right now.
- *Commanded direction* — where the player has told it to point,
  selected via **AttitudeMode** (a [[#maneuver--thrust|Burn Mode]] value
  reused for live manual flight) and recomputed each tick from the
  Vessel's current state.

Slew chases commanded; Cosine Loss is what you pay if the engine
fires before they line up. Persisted in saves so a slew-in-progress
restores correctly across reloads.
_Avoid_: Orientation, Heading, Nose vector, Pointing.

**Slew**:
The act of rotating Attitude toward the commanded direction. Constant
angular velocity in sim-time, capped at the loadout's **Slew Rate**
(deg/s, default 15°/s — ≈12 s for a 180° flip). At very high Warp,
sim-time `dt` per Tick is large enough that the slew completes in
one Tick — the accepted "effectively instant at high warp" behaviour.
Two refinements:

- *Lead-compensated slew* — for a planted Maneuver Node, the slew
  auto-starts `slew_angle / SlewRate` before Trigger Time so the
  Vessel is aligned at ignition. Preserves planted-node Δv accuracy
  without forcing the player to manage timing.
- *InstantSAS* — a player override that snaps Attitude to the
  commanded direction immediately, skipping the slew. The "magic
  SAS" escape hatch.
_Avoid_: Rotate, Reorient.

**Cosine Loss**:
The Δv loss when the engine fires while Attitude is misaligned with
the commanded direction by angle θ — only `cos(θ)` of the integrated
Δv goes along the intended axis; the rest pushes off-axis and is
wasted. The intended consequence of the slew-then-burn model: a
player who starts a Burn before alignment is complete loses real Δv,
and lead-compensated slew on planted nodes is the escape hatch that
preserves planner accuracy.
_Avoid_: Steering loss (correct in aerospace but ambiguous —
"steering loss" also covers gravity-turn losses).

**RCS**:
The Vessel's Reaction Control System — low-thrust monopropellant
thrusters used for fine attitude work and proximity ops, separate
from the main engine. Player engages via **EngineMode** (`EngineMain`
vs `EngineRCS`); planted Maneuver Nodes always use the main engine
regardless of EngineMode. In RCS mode, each attitude key press fires
a discrete pulse delivering a fixed Δv quantum (0.1 m/s in v0.10);
holding a key streams pulses. Fuel comes from a separate
**Monopropellant** ("Monoprop") tank with its own capacity, distinct
from the main-engine fuel on `Stages[0]`.
_Avoid_: Thrusters (ambiguous — main engine also thrusts), Cold-gas
(implementation flavor not enforced), Maneuvering jets.

### Docking & coupling

How two Vessels fuse into one and how that fusion comes apart.
Cluster anchors: **Docking** / **Undocking** are the actions; the
result is a **Composite**; the threshold that fires Docking is the
**Docking Gate**; pre-Docking identities are preserved as **Docked
Components**. "Component" and "Stage" are easily confused — see
Flagged ambiguities.

**Docking** / **Undocking**:
The act of fusing two Vessels into a single **Composite** (Docking)
and the inverse — splitting a Composite back into its pre-Dock
identities (Undocking). Docking fires automatically each tick when
two Vessels sit inside the **Docking Gate** within the same SOI;
Undocking fires on a player keystroke against a Composite.

Docking is **aggregate-preserving**: mass-weighted centroid for the
Composite's position, momentum-conserving combination for velocity,
summed pools for fuel / monoprop / capacities, concatenated roles
("transfer-stage+lander"). The lead partner's identity — name,
glyph, color, planted Maneuver Nodes, active Burn, attitude mode —
survives, and its loadout becomes the Composite's. The other
partner's identity is preserved only as a **Docked Component**
snapshot for future Undocking.

Undocking is **lossy**: prorated fuel / monoprop shares (by pre-Dock
capacity), each restored Vessel placed 35 m per side along the
radial-out axis (outside the Docking Gate so re-fusing is suppressed
on the next tick) with a 0.05 m/s **Spring Release** push for clear
drift. Composite-level state tied to the *aggregate* — planted Nodes,
active Burn, Manual Burn, attitude mode, engine mode — is dropped on
Undocking. The player keeps flying the lead identity through both
transitions: post-Docking on the Composite, post-Undocking on the
first restored component.
_Avoid_: Mate / Demate (NASA-correct but unfamiliar in the sim's
player vernacular), Fuse / Split (close, but Fuse loses the
"controlled rendezvous" connotation Docking carries), Couple /
Decouple (collides with **Stage** decoupling).

**Composite**:
A Vessel formed by **Docking** two or more pre-Dock Vessels. From the
simulator's perspective a Composite is a Vessel like any other —
same type, same `World.Crafts` slot, same integrator path; the
"composite-ness" is a property (`len(DockedComponents) ≥ 2`), not a
separate kind of entity.

Load-bearing structural rule: the Composite's bottom stage
(`Stages[0]`) is **unchanged from the lead partner's pre-Dock
bottom** — the active partner's currently-firing engine. The
partner's stages stack on **top** of the lead's. Two consequences:

- The player keeps firing the same engine they were firing before
  Docking — no mid-flight engine swap.
- The stage-stack ordering matches the natural along-Stage-boundary
  Undocking flow: the partner's stages, appended on top, peel off as
  a unit when Undocking fires.

The composite-as-a-whole pooled engine view (sum-thrust, mass-weighted
Isp across every stage with thrust) is exposed by
`CompositeEngineSummary` for consumers that want the aggregate rather
than the bottom-stage value. Bottom-stage values stay the default for
back-compat with pre-v0.9.1 readers.
_Avoid_: Composite Vessel (acceptable long form when ambiguity
threatens), Stack (collides with the **Stage** stack — the propulsion
ordering), Docked Vessel (emphasises the act, not the state).

**Docking Gate**:
The combined (distance + relative velocity) threshold within which
**Docking** fires. Two values, both required on the same tick within
the same SOI:

- **Docking Distance** — 50 m, the proximity radius. KSP-ish "soft
  capture" range.
- **Docking Velocity** — 0.1 m/s, the relative-velocity ceiling.
  Typical proximity-ops null-residual.

Same-SOI is a hard prerequisite: two Vessels whose *inertial*
positions are within 50 m but whose Primary IDs differ (one in
Earth's SOI, the other in Moon's, near the SOI boundary) do **not**
dock — they wouldn't be near each other in any common frame even if
inertial coordinates suggest otherwise.

The Alongside spawn (the third spawn-form position-mode after orbital
and **Launchpad**) places a new Vessel at 25 m from the active Vessel
with matching velocity — half the Docking Distance, so a single RCS
tap or even free orbital drift closes the gap without precision
approach.
_Avoid_: Docking window (collides with launch / transfer windows,
which are time intervals), Docking envelope (verbose), Docking
threshold (reads as singular, missing the combined nature).

**Docked Component**:
A snapshot of one pre-Docking Vessel's identity preserved on a
**Composite** so a future **Undocking** can restore it. Records
name, loadout, role, glyph, color, dry mass, fuel + monoprop
capacities, and main / RCS engine numbers — the identity-and-shape
fields. **Not** state: position, velocity, planted Maneuver Nodes,
and active Burns are *not* preserved per-component; while joined
the Docked Components all share the Composite's state, and on
Undocking each restored Vessel re-emerges near the Composite's
current state with no inherited Nodes or Burns.

Chained Docking flattens components: a Composite that Docks with
another Composite produces a single flat `DockedComponents` list
covering every original identity, not a nested tree.
_Avoid_: Component (bare, ambiguous — see Flagged ambiguities for
the **Stage** / Component collision), Module (overloaded with stage
modules in aerospace), Sub-vessel.

**Dock Event**:
An HUD-flash record stashed on `World.LastDockEvent` when a Docking
fires, so the screen layer can briefly announce the fuse without
polling. Carries the wall-clock time, the lead and partner slate
indices at the moment of fusion, and the resulting Composite's name.
Single-slot — the most recent event overwrites any unread one; the
HUD consumes-and-clears (sets to nil) once the flash has been
rendered. The notification, not the act.
_Avoid_: Docking notification, Fuse record, Dock notice.

**Spring Release**:
The small relative-velocity kick (0.05 m/s) applied to each restored
Vessel on **Undocking**, along the radial-out axis from the Primary.
Combined with the 35 m-per-side separation, the Spring Release
guarantees the restored Vessels drift apart instead of immediately
re-entering the **Docking Gate** and re-fusing on the next tick.
Magnitude is deliberately tiny — enough to break the gate, small
enough that the orbital math of each restored Vessel is essentially
unchanged.
_Avoid_: Undock push, Separation impulse, Decouple kick.

### Orbital geometry

**Ascending Node**:
The point where the Vessel's orbit pierces the reference plane (the
primary's equator for body-bound orbits, the ecliptic for heliocentric
orbits) moving north. Used by inclination-change planning.
_Avoid_: AN (in prose), "the node going up". Always qualify — bare
"Node" means [[#maneuver--thrust|Maneuver Node]] in this glossary.

**Descending Node**:
The southbound mirror of the Ascending Node — where the orbit pierces
the reference plane going south. Inclination burns happen at one of
these two points.
_Avoid_: DN (in prose), bare "Node".

### Time & warp

**Warp**:
The time-acceleration factor the player selects (1×, 10×, 100×, up to
many orders of magnitude in coast). Distinguish two values:

- *Selected Warp* — what the player picked. The displayed value.
- *Effective Warp* — what the simulator actually applies this tick,
  after clamps: burn cap (max 10× while a Burn is active), SOI
  step-size guard (per-tick step can't exceed a fraction of SOI
  radius — prevents integrator blow-up in short-period orbits), and
  node-approach ramp (warp ramps down as a Maneuver Node trigger
  time nears so the integrator can't alias past it).

Effective ≤ Selected always; when they differ, the HUD reflects the
effective rate. Preserve every clamp when touching warp code — each
exists for a specific failure mode.
_Avoid_: Time acceleration, Speed, Fast-forward.

**Clock**:
The simulator's time source. Holds `BaseStep` (wall-paced base tick
interval, ~0.05 s), the currently Selected Warp, and the accumulating
sim-time. The Clock advances one *Tick* of size `BaseStep × Effective
Warp` each physics step — Tick is the atomic unit, Clock is the noun
other systems read from.
_Avoid_: Timer (different semantics — timers count down), SimTime,
GameTime.

### Transfer planning

The vocabulary for moving a Vessel from one orbit to another — typically
interplanetary, but the same terms apply for intra-Primary transfers
(moon-to-moon, parking-to-elliptical).

**Transfer Plan**:
The two-burn output of an auto-planted transfer: a **Departure** burn
fired at periapsis of the Vessel's Parking Orbit around the origin
Primary, and an **Arrival** burn fired at the destination's SOI /
Capture Orbit radius after a coast. Each burn becomes a Maneuver Node
in its respective Primary's Reference Frame. Carries the coast time
between burns (`TransferDt`).

For the intra-Primary case (e.g. LEO→Luna) the Departure is no longer
a pure-prograde periapsis kick: the planner fuses the plane change
into the transfer via a Lambert solve and picks, by total Δv, between
two shapes — a **combined** transfer (one `BurnVector` Departure that
folds plane + raise together) and a **split** transfer (a near-coplanar
raise plus a `BurnPlaneChange` node at the transfer apoapsis, where the
plane change is cheapest). Both arrive in the target's plane; the HUD
shows both costs. This retires the old `I`-plane-match-then-`H` dance
as a *requirement* (the manual tools remain available).
_Avoid_: Trajectory (the whole flight path; the Plan is the burns).
The planner's internal `TransferNode` is not glossary-worthy — it's a
handoff struct (see Flagged ambiguities).

**Parking Orbit** / **Capture Orbit**:
The bounding circular orbits at each end of a Transfer Plan. The
**Parking Orbit** is the circular orbit around the origin Primary the
Vessel departs *from* (Departure burn at its periapsis). The **Capture
Orbit** is the circular orbit around the destination Primary the
Vessel arrives *into* (Arrival burn drops the hyperbolic approach
onto this circle). Both are specified by radius — the planner uses
them to compute v∞ on both sides of the transfer.
_Avoid_: Initial / final orbit, Source / target orbit.

**Porkchop**:
The 2D Δv grid plotted in the porkchop screen: departure date along
one axis, time-of-flight along the other, total Δv (Departure +
Arrival) as colour at each cell. Cells where the Lambert solver
fails to converge render as "impossible" pixels (NaN). The visual
intuition is that low-Δv regions form pork-chop-shaped contours —
hence the name. v0.10.5+ supports multiple porkchops stacked by N-rev
count and short/long-branch selection.
_Avoid_: Porkchop chart, Δv map (both fine in casual prose;
"Porkchop" alone is the canonical noun).

**Time of Flight (TOF)**:
The coast duration of a transfer — from Departure burn to Arrival
burn. Distinct from Trigger Time (which marks *when* a burn fires).
On the Porkchop, TOF is the vertical axis.
_Avoid_: Transit time, Cruise time, Coast time (Coast Time is fine
prose but TOF is the planner's name).

**Multi-rev Transfer**:
A Lambert solution that completes N ≥ 1 full revolutions around the
Primary before reaching the destination position. Unlocks transfers
at longer TOFs than the single-rev (N=0) solution can express, often
at lower Δv if the geometry aligns. Each N has two Lambert roots
(see Short / Long Branch); the porkchop UI lets the player walk N
and pick a branch. v0.10.5+.
_Avoid_: N-rev (fine in code; "Multi-rev" reads better in prose),
Multi-orbit transfer.

**Short Way** / **Long Way**:
A single-revolution (N=0) Lambert distinction: the transfer arc
either sweeps less than 180° (short way) or more than 180° (long way)
around the Primary, in the prograde sense. The picker is geometric:
`(r1 × r2) · ẑ ≥ 0` selects short way for prograde, reversed for
retrograde. Not exposed in the UI — the solver picks based on
position geometry.
_Avoid_: Short branch (different concept — see Flagged ambiguities).

**Short Branch** / **Long Branch**:
A multi-rev (N ≥ 1) Lambert distinction: for each N there are two
time-of-flight solutions flanking the minimum-energy critical-z
root. **Short Branch** is the lower-z root (more eccentric ellipse);
**Long Branch** is the higher-z root. The v0.10.5 porkchop UI exposes
both as picker options. Meaningless at N = 0 (single root); the flag
is ignored there.
_Avoid_: Short way (different concept — see Flagged ambiguities).

**Lambert**:
The orbital mechanics problem of finding the Keplerian arc connecting
two position vectors in a specified time of flight, around a given
Primary — and the solver that does it. The math engine underneath
Porkchop, single transfers, and inclination matching. Implementation
is Curtis Algorithm 5.2 (universal-variables formulation,
Newton-Raphson on z). `LambertSolve` is the single-rev entry;
`LambertSolveRev` is the multi-rev version with the branch flag.
_Avoid_: Lambert solver (fine; "Lambert" alone names both the problem
and the solver), Lambert arc.

**v∞ (v-infinity)**:
The hyperbolic excess velocity at a Primary's SOI boundary — the
asymptotic speed of a Vessel relative to the Primary, far from it,
on a hyperbolic trajectory. The bridging concept between intra-SOI
patched-conic math and inter-SOI heliocentric Lambert math: the
heliocentric arc tells you v∞ at each end, and the per-Primary
Escape / Capture Burn formulas convert v∞ into a Parking-Orbit Δv.
_Avoid_: Hyperbolic excess speed (correct but verbose), v_inf
(reserve for code/ASCII contexts).

**Escape Burn** / **Capture Burn**:
The Δv that converts between a circular orbit around a Primary and a
hyperbolic trajectory with a specified v∞. **Escape Burn** is at
Parking-Orbit periapsis, prograde, taking circular → hyperbolic
escape. **Capture Burn** is the mirror at Capture-Orbit periapsis,
retrograde, taking hyperbolic approach → circular. By symmetry the
magnitudes match; the directions and Primary contexts differ. These
are the per-Primary halves of the patched-conic Transfer Plan; the
heliocentric Lambert leg sits between them.
_Avoid_: Burn-to-escape, Insertion burn (Insertion is fine prose for
Capture in some contexts but ambiguous with orbit-insertion missions).

### Encounter math

The geometric vocabulary for two Vessels coming together — and the
single-burn advisory pipeline that helps the player make it happen.
"Encounter" here means **craft-to-craft**, not SOI entry — see Flagged
ambiguities.

**Encounter**:
The geometric event of two Vessels coming within close range along
their predicted trajectories, regardless of whether the approach is
purposeful. Characterised by three quantities: **Closest Approach**
(the minimum distance), **Time of Closest Approach** (when that
minimum happens), and **Relative Velocity at Encounter** (how fast
the two are moving past each other at that instant). The HUD's TARGET
block reports all three live for the active Vessel against its bound
TargetCraft. Distinct from **Rendezvous**: an Encounter just *happens*;
a Rendezvous is a Vessel deliberately pursuing one.
_Avoid_: Flyby (implies passing without intent to dock — too narrow),
Pass (vague), Conjunction (overloaded with astronomical alignment).

**Closest Approach** (CA):
The minimum distance between two Vessels along their forward-propagated
trajectories over a finite horizon. Computed by `NextClosestApproach`:
Verlet-propagate both Vessels through the horizon at ~50 samples per
(shorter) period, find the minimum sample, then **parabolically refine**
the sub-grid minimum from its two bracketing samples. Refinement is
load-bearing — without it, the HUD readout snaps to the ~period/50 grid
and jumps by a whole grid step as the true minimum drifts across a
boundary frame-to-frame; with it the readout is continuous in the
inputs and stable enough to judge *"is my approach improving or
worsening?"* The advisory machinery distinguishes two CA values:
**Current CA** (the no-burn answer) and **Achievable CA** (the
post-**Nudge** answer); see **Rendezvous Advisory**.
_Avoid_: Min range, Minimum distance (correct but verbose; CA is the
canonical noun), Approach distance (ambiguous about which moment).

**Time of Closest Approach** (TCA):
The time-from-now at which **Closest Approach** is reached, in seconds.
Returned alongside CA from `NextClosestApproach`; the HUD typically
renders it as a countdown ("CA in 4m 12s"). Parabolic refinement runs
on time as well as distance — the reported TCA is the parabola's
vertex time, not the nearest sample's time, so it stays continuous
frame-to-frame as the true encounter drifts.
_Avoid_: Encounter time, Time to encounter (acceptable casual prose),
CA time (acronym soup).

**Relative Velocity at Encounter** (v_rel):
The velocity vector of one Vessel relative to the other at the
**Time of Closest Approach** — `v_rel = vA − vB` evaluated at TCA.
Magnitude `|v_rel|` is the live HUD readout that tells the player
whether they're closing on or opening away from the encounter, and at
what speed. **Disambiguating prefix:** this is *Encounter* v_rel,
distinct from the **Air-Relative Velocity** used by **Drag** (same
symbol, different physics — see Air-Relative Velocity's `_Avoid_`
line).
_Avoid_: Relative velocity (overloaded — always qualify as "at
Encounter" or "v_rel between Vessels"), Closing velocity (technically
only the range-rate component, not the full vector).

**DOCK READY**:
The HUD signal that fires when the active Vessel is currently inside
the **Docking Gate** against its bound TargetCraft — range < 50 m AND
`|v_rel|` < 0.1 m/s. Same numbers as the Docking Gate; the HUD just
announces the gate condition in advance so the player knows fusion is
imminent on the next tick (without it, the player would only see the
**Composite** *after* the fact, via the **Dock Event** flash). A
*current-state* readout, not a projection — reads off live separation,
not **Closest Approach**.
_Avoid_: Dock soon, Capture imminent, Docking ready (the rendered
label is two words, no "ing").

**Rendezvous**:
The act of a Vessel deliberately pursuing an **Encounter** with another
Vessel — typically to dock with it. The player drives the rendezvous;
the simulator supplies the live readouts (CA, TCA, |v_rel|, DOCK READY)
and the optional **Rendezvous Advisory**. The advisory pipeline plus
the v0.10.0 target-relative **Burn Modes** together comprise the
rendezvous tooling shipped through the v0.10 cycle. Cross-SOI
rendezvous (chaser and target in different SOIs) is out of scope for
the current tooling.
_Avoid_: Approach (too generic — also covers planetary flybys),
Intercept (military / kinetic connotation), Catch (informal only).

**Rendezvous Advisory**:
The single-burn recommendation surfaced by `RecommendRendezvousNudge`
when a useful **Nudge** is available. Carries the Δv magnitude, the
recommended axis (one of the eight velocity-frame axes — six
self-relative **Burn Modes** plus TargetPrograde and TargetRetrograde),
the axis unit vector, the **Current CA** and **Achievable CA**, the
post-burn **Time of Closest Approach**, and the full Lambert ideal Δv
(always ≥ the recommended scalar — the gap shows how much the
axis-projection lost). When no useful Nudge exists, the advisory
returns `Ok=false` with a `Reason` tag the HUD surfaces verbatim
("no improvement available", "burn too large — use H/I/m", "burn drops
periapsis unsafely", etc.). HUD recompute is sim-time-throttled to
500 ms per cache hit — at warp the recompute rate naturally tracks
how fast the trajectories are changing.
_Avoid_: Burn recommendation (generic — covers any planner output),
Rendezvous plan (too strong; an advisory is a *single-burn nudge*,
not a full plan).

**Nudge** / **Rendezvous Nudge**:
A small single-burn Δv along one of the eight velocity-frame axes,
intended to improve **Closest Approach** without re-shaping the orbit.
The output of the **Rendezvous Advisory** pipeline. "Nudge" is the
canonical short form; "Rendezvous Nudge" is the long form when context
needs disambiguation (e.g. distinguishing from a transfer-plan
midcourse correction). Three gates filter what counts as a Nudge:
the **Improvement Floor** (is it worth recommending?), the **Nudge
Ceiling** (is it small enough to call a nudge?), and the
**Orbit-Safety Gate** (does it leave the chaser's orbit intact?).
_Avoid_: Correction (ambiguous with mid-course corrections on a
transfer leg), Trim (collides with **PitchTrim**), Bump.

**Improvement Floor**:
The two-prong gate that decides whether a **Rendezvous Advisory** is
worth surfacing: either **CA improvement ≥ 10%**, or **Δv ≥ 0.5 m/s
AND CA improvement ≥ 100 m absolute**. Either prong qualifies; both
failing means the recommended Nudge isn't materially better than
coasting, so the advisory returns `Ok=false`, `Reason="no improvement
available"`. Two prongs because percent-improvement is meaningful at
long range (1 km → 900 m is a real win) but breaks down at very close
range (100 m → 90 m is also 10% but unimportant when the goal is
sub-50 m); the absolute prong picks up the cases where percent math
collapses.
_Avoid_: Recommendation threshold (vague), Worth-recommending gate.

**Nudge Ceiling**:
The 300 m/s upper bound on Δv that a **Rendezvous Advisory** will
recommend. Above this magnitude the single-burn projection is no
longer a "nudge" — it's a major orbit-shape change that belongs in
the manual planners (H Hohmann, I Inclination, m Maneuver Node), not
in the auto-recommender. Without this ceiling the gate would happily
plant a 1.7 km/s K-burn whenever it improved CA by ≥10%, because the
Lambert lookahead converges on whatever transfer fits T_k even when
the orbits are wildly mismatched. v0.10.3+.
_Avoid_: Max nudge, DV ceiling.

**Orbit-Safety Gate**:
The filter that rejects a candidate **Nudge** if it would compromise
the chaser's orbit — specifically, drop the post-burn periapsis below
`primary + 50 km` or by more than 100 km from the pre-burn value.
Needed because the axis projection in the **Rendezvous Advisory**
pipeline is lossy: the perturbed orbit is *not* the Lambert transfer,
just the chaser's orbit plus a scalar push on one axis, so a large
retrograde or radial-in nudge can nominally "improve CA" while
dropping the chaser's periapsis into the atmosphere. Failure tag
surfaced verbatim: "burn drops periapsis unsafely". v0.10.3+.
_Avoid_: Periapsis floor (only half the rule), Safety check (too
generic).

### Objectives

**Mission**:
A pass/fail objective evaluated against live World state each tick — the
game's only goal layer. Carries a `Type` (which predicate to run),
`Params` (predicate-specific tuning, e.g. `MinPeriapsisAltM`), and a
three-state `Status` machine: **InProgress → Passed | Failed**. Terminal
states (Passed, Failed) are sticky: once set, `Evaluate` is idempotent.
Current Types: **Circularize**, **OrbitInsertion**, **SOIFlyby**,
**CircularizeFromPad**. Missions are seeded from an embedded starter
catalog at world init and round-trip through save. Evaluated only against
the Active Vessel.
_Avoid_: Quest, Objective (Objective is fine in prose for the abstract
concept; reserve Mission for the concrete pass/fail unit), Achievement.

### View & projection

How 3D Vessel / Body / orbit geometry gets squashed onto the 2D
Bubble Tea braille canvas. Cluster anchors: **ViewMode** is the
player-facing enum (which projection the orbit and maneuver screens
use); **Basis** is the engineering noun underneath (the pair of
world-space unit vectors the canvas projects through). Two Basis
families exist: **Cardinal Views** (fixed world-axis pairs for
Top / Right / Bottom / Left) and the **Perifocal Basis** that backs
**Orbit-Flat View**; the **Depth Axis** falls out of either as
`X × Y`. "Basis" collides with [[#engineering-vocabulary|Reference
Frame]] — see Flagged ambiguities.

**ViewMode**:
The world-level projection selector — which canvas projection the
orbit and maneuver screens use to flatten 3D geometry onto the
braille canvas. Five values cycled in this order: **ViewTop** (drop
world Z), **ViewRight** (look from +X), **ViewBottom** (Top with Y
inverted), **ViewLeft** (Right mirrored), **ViewOrbitFlat** (project
onto the active Vessel's orbit plane). Stored on `World.ViewMode`
so the orbit screen and the maneuver-planner mini-canvas share the
same angle without per-screen coordination.

Distinct from [[#selection--view|Focus]] — Focus picks *what* the
camera centres on (a system, a Body, a Vessel); ViewMode picks
*which projection* the camera uses. They compose independently: any
Focus target can be rendered under any ViewMode. The same word
"view" attaches loosely to Focus (camera centring), to ViewMode
(projection), and to the OrbitView screen itself; the canonical
names are Focus / ViewMode / OrbitView respectively.
_Avoid_: View mode (verbose; `ViewMode` is one word in code and the
canonical noun in prose), Camera angle (describes what ViewMode
controls, not what it *is*), Projection mode.

**Cardinal Views**:
The four world-axis ViewModes — **ViewTop**, **ViewRight**,
**ViewBottom**, **ViewLeft** — each a fixed orthographic projection
onto a pair of world axes. ViewTop is the v0.1 default (drop world
Z, plot (X, Y) — equatorial orbits read as ellipses, inclined
orbits foreshorten). ViewRight looks from +X toward origin (canvas
X+ = world Y+, canvas Y+ = world Z+; equatorial orbits read edge-on
as a horizontal line through the Body's silhouette — useful for the
"watch the craft swing around the back of the planet" geometry Top
hides). ViewBottom mirrors Top vertically; ViewLeft mirrors Right
horizontally. The cardinal cycle (Top → Right → Bottom → Left) is a
90°-rotation circuit around the system, with **Orbit-Flat View**
landing afterwards as punctuation before wrapping.
_Avoid_: Cardinal projections (verbose), World-axis views
(descriptive but not canonical), Orthographic views (all ViewModes
are orthographic — this doesn't distinguish them from ViewOrbitFlat).

**Orbit-Flat View** (ViewOrbitFlat):
The fifth ViewMode — projects onto the active Vessel's orbit plane
via the **Perifocal Basis** (x̂, ŷ), so an inclined orbit renders
as a clean ellipse with no foreshortening — the way the geometry
would read if `i = 0`. The view the Cardinal Views can't produce
because they're tied to world axes. Out-of-plane points project to
their in-plane shadow (orbit-normal component dropped).

Falls back to ViewTop's basis whenever the perifocal projection is
undefined or actively unhelpful: no active Vessel, hyperbolic
orbit (e ≥ 1), degenerate orbit (a ≤ 0 or NaN), or the active
Vessel is **Landed** — a Landed Vessel's `(r, v)` describes a
co-rotating orbit that would lock the camera to the Body and freeze
the surface texture; ViewTop fallback lets the player see the
ground turning underneath while parked.
_Avoid_: Perifocal view (correct but jargon-heavy for the
player-facing label), Orbit view (collides with the OrbitView
screen), Flat view (under-specified).

**Basis**:
A pair of world-space unit vectors `(X, Y)` on the `widgets.Canvas`
that maps world coordinates to canvas pixels: `screen.x = (world −
center) · Basis.X`, `screen.y = (world − center) · Basis.Y`. The
projection primitive — every render path picks a Basis and the
canvas does ortho projection from it. The bridge from **ViewMode**
to Basis is `viewBasis(w)` in the orbit screen, which resolves each
ViewMode to a specific Basis (Cardinal Views to fixed world-axis
pairs, Orbit-Flat to the active Vessel's **Perifocal Basis**).
`DefaultBasis()` is the ViewTop pair (X = (1,0,0), Y = (0,1,0)) —
also the fallback any non-Top ViewMode degrades to when its inputs
go bad.

Distinct from [[#engineering-vocabulary|Reference Frame]] —
Reference Frame defines how Keplerian elements `(i, Ω, ω)` are
*interpreted* (Ecliptic vs Body-Equatorial); Basis defines how
*world coordinates* are *projected* to canvas pixels. Same math
word, different layer. See Flagged ambiguities.
_Avoid_: Projection matrix (a Basis is two vectors, not a 4×4),
Camera basis (close, but "Basis" alone is the canonical noun in
this codebase), View basis (confusable with ViewMode).

**Perifocal Basis**:
The orbit-plane unit vectors `(x̂, ŷ)` derived from a Vessel's
Keplerian elements: x̂ points toward periapsis; ŷ is 90° prograde
from x̂ in the orbit plane. Computed by `PerifocalBasis(el)`
(Vallado §2.6 — the first two columns of the perifocal-to-inertial
rotation matrix). Together they span the orbit plane; projecting an
inertial point onto `(x̂, ŷ)` yields its in-plane coordinates.

Used as the canvas **Basis** for **Orbit-Flat View** (where the
result is the foreshortening-free ellipse) and as the math
underneath the maneuver planner's per-node Projected Orbit redraw,
which evaluates positions in the post-burn perifocal frame. The
`p` / `q` axis labels used inside `PerifocalToInertial` name the
same pair — `(x̂, ŷ)` is the player-facing prose form.
_Avoid_: Orbit basis (informal; PerifocalBasis is the conventional
aerospace term), Perifocal frame (the *frame* is the 3D triad in
`internal/orbital/frame.go`; the *Basis* is the 2D pair pulled out
of it).

**Depth Axis**:
The unit vector pointing toward the camera, computed as `Basis.X ×
Basis.Y`. Points with positive `(world − center) · DepthAxis()`
are in front of the basis plane through center; negative is behind.
Cardinal Views derive their depth axes consistently from the cross
product (Top → +Z, Right → +X, etc.). Used by the orbit renderer
for back-of-body occlusion in side views and by the view-aware
body-texture pipeline (v0.8.5.7+), which needs a camera direction
that — under Orbit-Flat — is the active Vessel's orbit-plane normal
rather than a cardinal world axis.
_Avoid_: Camera direction (correct but ambiguous — a camera also
has up and right), Out-of-screen axis, Normal (overloaded with
orbit-normal h in [[#maneuver--thrust|Burn Mode]] math).

### Engineering vocabulary

Terms below are dev/agent-facing — they don't appear in player UI but are
load-bearing in code reviews, ADRs, and bug discussions.

**Reference Frame**:
The orthonormal basis that an orbit's Keplerian elements (i, Ω, ω) are
interpreted relative to. The codebase has two: the Ecliptic Frame for
heliocentric orbits and the Body-Equatorial Frame for body-bound orbits.
**Rule:** body-bound orbits use the Primary's equatorial frame;
heliocentric orbits use the ecliptic frame; never mix them.
`internal/orbital/frame.go` is the boundary — every conversion lives
there. The Maneuver Node predictor frame-rebases per node so a
post-burn orbit reads in the correct frame for its post-burn Primary.
_Avoid_: Coordinate system (broader concept). Note: "Basis" is a
distinct rendering-layer concept — the `widgets.Canvas` projection
pair — see Flagged ambiguities.

**Ecliptic Frame**:
The world inertial frame (`IdentityFrame`): Ex=+X, Ey=+Y, Ez=+Z. The
ecliptic plane coincides with world XY by construction, so heliocentric
orbits express their elements directly without rotation.
_Avoid_: World frame (the renderer's "world space" is the same basis
but a different concept — positions, not orbital elements), Inertial
frame (true but ambiguous — Body-Equatorial is also inertial).

**Body-Equatorial Frame**:
A Reference Frame whose Z-axis is a Body's spin axis (constructed from
its `AxialTilt` and `AxialAzimuth`). Used for orbits bound to that
Body — a 0° inclination orbit lies in the Body's equator, matching how
operational mission planners express elements. Collapses to the
Ecliptic Frame for zero-tilt Bodies.
_Avoid_: Body frame (ambiguous with rotating body-fixed frames used by
surface code), Equatorial frame (ambiguous about which body).

**Propagation Mode**:
The integrator the simulator runs on a Vessel for a given Tick. Three
modes, selected per-Vessel per-Tick:

- *Verlet* — symplectic, used for free flight (coast). Conserves energy
  over long arcs; the default for any Vessel not burning and not warp-
  locked.
- *RK4* — used during an active Burn, with rocket-equation mass loss
  applied each substep. Switched in by the Burn lifecycle.
- *Kepler Step* — analytic propagation used when warp is high enough
  that step-by-step Verlet would alias. Caller (`canKeplerStep`)
  checks SOI distance and burn state before switching; SOI crossings
  are caller's responsibility.

The mode switch is automatic — no glossary term for "switching modes"
because the choice is a derived consequence of (burn? warp? SOI margin?),
not a player or planner decision.
_Avoid_: Integrator (often correct, but ambiguous — every mode is an
integrator), Stepper.

## Example dialogue

A walkthrough of how the vocabulary interacts in a typical planning
conversation, between a player (P) and a dev (D):

> **P:** I planted a Node to circularize at the Mun, but the Projected
> Orbit is way off — it shows me escaping back to Kerbol.
>
> **D:** What's your Trigger Event?
>
> **P:** Next Apo.
>
> **D:** And what's the Node's PrimaryID — Kerbin or Mun?
>
> **P:** Kerbin, I planted it before SOI entry.
>
> **D:** That's the bug. You're inside Mun's Sphere of Influence now, so
> the integrator is propagating in Mun's Body-Equatorial Frame, but the
> Node still thinks its Δv vector is in Kerbin's frame. The Burn fires
> rotated by Kerbin's tilt relative to Mun's spin axis. Replant the Node
> from inside Mun's SOI and the Projected Orbit will line up.
>
> **P:** Why doesn't it auto-rebase?
>
> **D:** The Predictor does — that's why the Projected Orbit redraws
> correctly across SOI transitions. But the Maneuver Node's stored Δv
> vector is fixed at plant time, in the Reference Frame of its
> PrimaryID. Rebasing the *stored* vector is destructive; the player
> might have wanted the original Kerbin-frame intent.
>
> **P:** OK. While I have you — my Vessel won't stage. I press space
> and nothing happens.
>
> **D:** How many Stages?
>
> **P:** Just the one.
>
> **D:** Single-stage Vessels can't be staged — `StageActive` no-ops
> rather than jettisoning your core. That's by design.
>
> **P:** Right. And the Burn is at >10× Warp; can I bump it higher?
>
> **D:** No. Selected Warp goes higher but Effective Warp clamps to 10×
> for the duration of any Finite Burn. The HUD shows Effective.
>
> **P:** And after the Burn, my Mission status — does it auto-complete
> when I hit the right orbit?
>
> **D:** Each Tick the Mission's predicate (Circularize, OrbitInsertion,
> SOIFlyby — yours is OrbitInsertion) evaluates against your Active
> Vessel's state in its Primary's frame. When the predicate passes,
> Status flips Passed and sticks. Only the Active Vessel counts.

## Flagged ambiguities

**"Node"** is overloaded in aerospace and in this codebase:

- **Maneuver Node** — the planted plan (`spacecraft.ManeuverNode`).
- **Ascending Node / Descending Node** — the orbital-geometry concept
  (`orbital/events.go`).

**Resolution:** bare "Node" in prose means **Maneuver Node** (frequency-weighted
default — players plant maneuver nodes constantly; orbital nodes come up only
in inclination contexts). For the orbital concept, always qualify: "Ascending
Node" or "Descending Node". The planner-internal `TransferNode` struct stays
out of prose entirely — call it a "planner-layer plan" if it must be named.

**"Short"** is overloaded in Lambert transfer math:

- **Short Way / Long Way** — a *single-rev* (N=0) geometric distinction:
  does the transfer arc sweep less or more than 180° around the Primary.
  The solver picks based on position geometry; not a player choice.
- **Short Branch / Long Branch** — a *multi-rev* (N≥1) solver-root
  distinction: which of two TOF roots flanking the minimum-energy
  critical-z to converge on. The v0.10.5 porkchop UI exposes this as
  a picker.

**Resolution:** never use bare "short" in transfer-planning prose. Always
qualify as "short way" (single-rev geometry) or "short branch" (multi-rev
root). Same for "long". When in doubt, consider whether N=0 or N≥1: if
N=0 it's way; if N≥1 it's branch.

**"Landed"** (and the looser "landing", "landed at", "sitting on the
surface") collapses three operationally distinct things:

- **Landed** (the runtime state) — the integrator-bypass mode that
  co-rotates the Vessel with the ground (`Spacecraft.Landed = true`).
  Today reachable only via a **Launchpad** spawn.
- **Surface Contact** (the physics event) — an aerobraking Vessel
  penetrates the surface; the clamp zeros V and projects R back to the
  radius, but **does not set Landed**. The Vessel sits motionless in
  inertial space and drifts west across the ground as the Primary
  rotates underneath, with the clamp re-firing every tick.
- **Touchdown** (the intended future state) — a controlled descent
  that *should* set Landed once landing semantics ship. Not yet in
  code; today, every controlled descent collapses into a Surface
  Contact at zero V.

**Resolution:** capital-L **Landed** in prose means the runtime state —
the co-rotating integrator-bypass mode set only by Launchpad spawn
today. For a Vessel that has arrived at the surface via Surface Contact,
say "post-contact" or "on the surface but not Landed" — never bare
"landed." The asymmetry matters for save state (Landed Vessels persist
`LaunchLatDeg` / `LaunchLonDeg`; post-contact Vessels don't), HUD
readout, and any future per-state behaviour like re-ignition liftoff.
When in doubt: a Landed Vessel co-rotates with the ground; a
post-contact Vessel drifts west.

**"Ballistic Coefficient"** uses two reciprocal conventions across
domains:

- **This codebase**: `BC = C_D · A / m` (units m²/kg). Higher BC =
  draggier. The factor the integrator multiplies in
  `a = −0.5 · ρ · |v_rel|² · BC · v̂_rel`. Per **Stage**; default
  0.01 m²/kg (S-IVB-1).
- **Aerospace literature / KSP / standard textbooks**: `BC = m / (C_D · A)`
  (units kg/m²). Higher BC = less draggy — a heavy bullet has a high BC
  and barely slows.

**Resolution:** a BC quoted from any aerospace source must be **inverted
and unit-converted** (kg/m² → m²/kg, i.e. take the reciprocal) before
being entered as a Stage's `BallisticCoefficient` field. The codebase's
choice is deliberate — it names what the integrator actually multiplies,
not what the literature defines — but the consequence is that "high BC"
in code review means the opposite of "high BC" in an aerospace paper.
Always confirm which convention the speaker is using before discussing
specific values.

**"Component"** collides with **Stage** as a word for "part of a Vessel":

- **Stage** — one element of a Vessel's decoupleable propulsion stack
  (`Stages[i]`). Carries dry mass, fuel, thrust, Isp, ballistic
  coefficient. Indexed bottom-up: `Stages[0]` is the currently-firing
  engine. Player concept: *one fire-and-jettison unit of the rocket*.
- **Docked Component** — one element of a **Composite**'s
  `DockedComponents`. Carries identity-and-shape fields (name, loadout,
  dry mass, capacities, engine numbers) but **not** state, Maneuver
  Nodes, or Burns. Used by **Undocking** to restore the pre-Dock
  Vessels. Player concept: *the original ship I docked in*.

Both are "parts of a Vessel" but at different abstraction levels. A
Composite has both: its **Stages** are the concatenated propulsion
stack (lead's + partner's, appended on top), its **Docked Components**
are the original Vessel identities it can decompose back into. Docking
does **not** add one Component per Stage — it adds *one Component per
pre-Dock Vessel*, even if that Vessel had multiple stages.

**Resolution:** "Stage" is the unambiguous term for the propulsion unit.
Bare "component" in code-review prose should be qualified as "Docked
Component" when ambiguity threatens. A diagnostic: if you see
`len(c.Stages) == 3 && len(c.DockedComponents) == 2`, that's a Composite
of two pre-Dock Vessels whose stage counts sum to 3 (probably 1+2 or
2+1).

**"Encounter"** has two distinct meanings split across domains:

- **Craft-to-craft Encounter** (this codebase) — the geometric event
  of two Vessels coming within close range, characterised by **Closest
  Approach**, **Time of Closest Approach**, and **Relative Velocity at
  Encounter**. The `NextClosestApproach` function and the HUD's TARGET
  block use this meaning.
- **SOI encounter** (aerospace literature, KSP) — a Vessel entering
  another Body's sphere of influence (e.g. "Apollo's lunar encounter").
  The codebase calls these *SOI entry / exit* or *SOI transition*,
  not "encounters" — they happen on the patched-conic boundary, not
  between Vessels.

**Resolution:** in this codebase, bare "encounter" means *craft-to-craft*.
For the patched-conic event, always qualify as "SOI entry" or "SOI
transition". Aerospace-fluent readers should re-orient on first read:
the rendezvous tooling lives under "encounter math," not the SOI
tooling.

**"Basis"** has two distinct meanings split across rendering and
orbital-math layers:

- **Canvas Basis** (this codebase, `widgets.Basis`) — the `(X, Y)`
  pair of world-space unit vectors that the `widgets.Canvas` projects
  world coordinates through to land on canvas pixels. A 2D projection
  primitive on the rendering layer; lives in `internal/tui/widgets/canvas.go`.
- **Reference Frame** (this codebase, `orbital.Frame`; standard aerospace
  usage of "basis") — the orthonormal triad that Keplerian elements
  `(i, Ω, ω)` are interpreted relative to. A 3D coordinate definition on
  the orbital-math layer; lives in `internal/orbital/frame.go`.

**Resolution:** bare "Basis" in code-review prose means the *Canvas Basis*
— the rendering-layer projection primitive. For the orbital-element
interpretation context, use "Reference Frame," "Ecliptic Frame," or
"Body-Equatorial Frame." The two never substitute for each other: a
Canvas Basis is two vectors plus a projection rule; a Reference Frame
is the triad those vectors live in (or, more precisely, the triad
the orbital elements being projected were interpreted in). The
**Perifocal Basis** is the canvas-Basis flavour pulled out of the
perifocal *frame* — same word collision in miniature, and the entry
above names it carefully.
