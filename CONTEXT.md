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
