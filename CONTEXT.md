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
not per-Vessel. Independent of Target.
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
_Avoid_: Vector mode, Heading, Direction.

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
its **launch latitude** and **launch longitude** (stored at spawn time
as `LaunchLatDeg` / `LaunchLonDeg`) using the Primary's current rotation
phase. Position rides the Primary's body-fixed frame; velocity is set
to ω × r each tick so a future un-Landed transition releases the
Vessel with full surface co-rotation velocity — the ~465 m/s eastward
boost at Earth's equator. Player concept: the Vessel is *parked on
the ground, moving with the ground*.

Currently set only by a **Launchpad** spawn; a Vessel that arrives at
the surface via aerobraking (see **Surface Contact**) does **not**
become Landed. Cleared by engine ignition — either a Manual Burn
(player presses `b`) or a planted Maneuver Node firing on schedule.
The clearing transition releases the Vessel into normal integration
with the surface co-rotation velocity it had at the moment of ignition.
_Avoid_: On the pad (loses generality — Landed is a runtime state,
not a place; future powered-landing modes would also be Landed), Parked,
Surface Park, Grounded.

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
sub-step puts |R| below the Primary's mean radius — typically an
**Aerobraking** pass that dipped too low, an intentional **Re-entry**,
or an uncontrolled descent. The clamp projects R back to the surface
along r̂ and zeros V; the Vessel stays in normal integration with
**Landed** unchanged (still false). Without this clamp the gravity
singularity at r → 0 would slingshot the Vessel back out at huge
velocity.

A consequence of the "zero V, don't set Landed" semantics: a
post-contact Vessel sits motionless in inertial space while the
Primary rotates underneath — visually drifting west along the
ground. Each subsequent tick gravity pulls it back below the
surface and Surface Contact fires again; the Vessel is perpetually
re-clamped.

This is a placeholder, not the intended final model — see
**Touchdown / Crash** for the differentiated outcomes the simulator
should produce once landing semantics ship.
_Avoid_: Crash (reserved for the intended hard-landing outcome —
see below), Ground hit, Impact (acceptable casual prose, but
Surface Contact is the canonical term for this physics event).

**Touchdown** / **Crash**:
The two intended outcomes of a Vessel arriving at a Body's surface,
distinguished by kinematic state at the moment of contact:

- **Touchdown** — a controlled arrival within velocity and
  orientation tolerances. Should produce a **Landed** Vessel that
  co-rotates with the ground, preserves fuel and stage state, and
  can re-ignite for liftoff (the same end-state a **Launchpad** spawn
  creates, but earned through controlled descent rather than spawned
  in).
- **Crash** — an uncontrolled arrival outside those tolerances —
  excess descent velocity, off-vertical attitude, or other failure
  predicate. Should produce a destroyed or disabled Vessel.

Not yet differentiated in code. Today every surface arrival — soft
or hard — routes through **Surface Contact**, which zeros V without
distinguishing the two cases. The destruction model that would back
**Crash** is deferred (the v0.8.5 surface doc punted it to "v0.9+";
not yet shipped as of v0.10.5+). When that model lands, the
**Landed** entry above should drop its "set only by a Launchpad spawn"
scope cap.
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
_Avoid_: Coordinate system (broader concept), Basis.

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
