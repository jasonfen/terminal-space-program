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
_Avoid_: Template, Blueprint. (**Design** is now a *distinct* noun — a
player-saved VAB vehicle, v0.24 / ADR 0029 — so don't call a built-in Loadout
a Design.)

**Stage**:
One decoupleable propulsion module on a Vessel. Convention: `Stages[0]` is
the *bottom* stage (the currently-firing engine, the next to be jettisoned)
and `Stages[len-1]` is the *top* stage (the player's "core" — the only one
left after every lower stage has been decoupled). Carries dry mass, fuel
mass/capacity, thrust, Isp, and ballistic coefficient. **Staging** is the
act of jettisoning `Stages[0]`; the popped stage spawns as its own passive
Vessel. Single-stage Vessels can't be staged (no-op + status flash).
_Avoid_: Booster (one kind of stage, not all stages), Tank, Section.

**Surface Staging**:
A **Staging** event performed while the Vessel is **Landed** — the
player decouples the bottom stage on the ground rather than in flight.
The canonical case is the 2-stage Lander: with the Vessel sitting on
the surface, jettisoning the descent stage (`Stages[0]`) leaves it as
a **Landed** *passive* Vessel (intentionally-abandoned hardware, not
**Crashed**) at the same surface point, while the ascent stage
(`Stages[1]`, now the new bottom) becomes the player's active core for
return-to-orbit. Distinct from orbital Staging only in spawn placement:
the jettisoned stage is pinned to the surface (co-rotating via the
landed integrator) instead of nudged onto a retrograde inertial offset.
A Landed Vessel is never auto-fused by proximity (the dock check skips
any pair where **either** craft is Landed), so the shed descent stage
and the parked ascent stage don't re-merge — neither while both sit on
the ground nor at the moment the ascent ignites and lifts off while
still co-located with the descent.
_Avoid_: Surface decouple, Ground staging, Lander separation.

**Decouple Plan**:
An optional per-Loadout, bottom-up list of group sizes describing how
many contiguous bottom Stages each **Staging** press releases as a
single craft. Default (absent) is all-ones — one Stage per press, the
historical behaviour. The Apollo Stack declares `[1, 1, 1, 2]` (ADR
0009): drop S-IC, S-II, S-IVB individually to reach the
pre-transposition stack `[Descent, Ascent, SM, CM]`, then the trailing
`2` releases the LM (Descent + Ascent) as a single 2-stage craft. The
plan is copied onto the Vessel at spawn and consumed positionally (each
press pops the next entry's worth of Stages and advances). A released
multi-Stage craft inherits **no** plan, so its own internal boundaries
are ordinary single-Stage separations — the extracted 2-stage LM later
**Surface Stages** its descent Stage alone with no special-casing.
The trailing `2` is what makes the **canonical manual flip** work: it
drops the whole LM as one craft, leaving the `[SM, CM]` core firing the
SPS, ready to slew and re-dock. Staging the LM one Stage at a time
(the bug from the `[1,1,1]` interim) would strand the Descent and split
the lander. The one-shot **Transposition** key (`D`) is the alternative
at the same `[Descent, Ascent, SM, CM]` state: it reorders to `[SM, CM,
Descent, Ascent]` and registers the LM as a docked **nose payload** that
**Undock** releases (clearing the unconsumed trailing `2` so the core
doesn't later pop as a group). The mission survivor is the **Command
Module**, not the fused CSM.
_Avoid_: Decouple group, Staging sequence, Separation script.

**Nose Payload Plan**:
The spawn-time, top-release counterpart to the **Decouple Plan**: a
bottom-up list naming how many contiguous *top* Stages of a custom build
or **Loadout** form each docked **nose payload**, ordered top-down. At
spawn the builder splits the stack at each **Dock Seam** entry, builds
the carrier core and each payload as separate Vessels, and **Docks** them
into a ready **Composite** — so a player-assembled CSM+LM spawns *already*
in the post-**Transposition** shape (SM firing core, LM an **Undock**-able
nose payload), with no flip to fly. Where the Decouple Plan releases
bottom Stages via **Staging**, the Nose Payload Plan pre-assembles the top
group(s) and hands release to **Undock** (in-flight docking composites) or
**Deploy** (carried payloads — v0.23 / ADR 0028). Default (absent) ⇒ a
plain linear Vessel, the historical custom-build behaviour. A multi-entry
plan (e.g. `[1,1,1]`) assembles a **Payload Stack** — a carrier with N
stacked docked payloads; a single-entry plan is byte-identical to v0.14
behaviour.
_Avoid_: Payload plan, Top decouple plan, Reverse staging.

**Dock Seam**:
The marker in the spawn configurator's stack list that designates the
contiguous top group as a docked **nose payload** (the editor-side
expression of the **Nose Payload Plan**). A pre-seamed **CSM+LM** module
pick drops the four Apollo Stages `[SM, CM, Descent, Ascent]` with the
seam set between `CM` and `Descent`, so the spawned Composite fires the
SM and carries the LM as an Undockable payload.
_Avoid_: Payload boundary, Split point, Stack divider.

**Part**:
The normalized, data-authored catalog stage (`internal/spacecraft/catalog.go`,
ADR 0026): one **Part** materializes into one runtime **Stage** via `ToStage`
at spawn, and a **Loadout** references Parts by ID rather than inlining Stage
literals. A Part is **atomic** when its scalar stats (dry mass, fuel, thrust,
Isp, …) are authored inline, or **composed** when it instead declares a
`components` list and its stats are **derived by Aggregation** at load time
(v0.24 / ADR 0029). The two are indistinguishable downstream — both become one
flat Stage — so the runtime never knows which it was; today's whole catalog is
atomic, so the migration to composability cost zero changes. Embedded + user
overlay catalog files supply Parts (user wins on ID).
_Avoid_: Stage (a Part *becomes* a Stage, but a Part is catalog data, a Stage
is runtime state), Component (the finer noun below), Module.

**Component** (v0.24 / ADR 0029):
The finest catalog noun — one level below a **Part**. A composed Part lists
Components by ID; **Aggregation** collapses them into the Part's flat scalars.
Five **Component Kinds** ship: **engine** (thrust / Isp / fuel type), **tank**
(fuel capacity / fuel type), **command-core** (a control point — crewed or
probe — plus optional soft-land / parachute), **antenna** (direct / relay +
range), and **structure** (inert dry mass: adapters, fairings, ballast). The
**VAB** composes vehicles from Components. **NB:** this is a *different* noun
from a **Docked Component** (an element of a **Composite**) — see Flagged
ambiguities.
_Avoid_: Part (a Component is finer — many compose one Part), Docked Component
(unrelated — that's a composite element), Module.

**Aggregation** (v0.24 / ADR 0029):
The load-time pass that derives a composed **Part**'s flat **Stage** scalars
from its **Components**: dry mass and tank capacity are **additive** (Σ);
multiple engines combine honestly — thrust adds (`Thrust = ΣF_i`) and the
effective Isp is the **thrust-weighted** parallel-engine blend `Isp_eff = ΣF_i
/ Σ(F_i / Isp_i)` (exact for a single fuel pool); command-source and antenna
attributes ride up. A stage holds **one fuel chemistry** — a mixed-chemistry
Part is a catalog warning and the VAB rejects it in the editor — which keeps
the single-`FuelMass`-pool runtime and the burn integrator untouched.
_Avoid_: Composition (the act, not the math), Summing (only mass/capacity sum —
Isp is thrust-weighted, not summed).

**Vehicle Assembly (VAB)**:
The in-game builder screen (`internal/tui/screens/vab.go`, v0.24 / ADR 0029),
reached from the pause menu (`Esc → [Build (VAB)]`). The player composes
**Components** into stages, stacks stages into a vehicle, marks **Dock Seams**
(N **Nose Payloads**) and fused decouple groups, and reads a live **Δv / TWR /
mass** panel — then saves the result as a **Design**. Like every screen it
reads shared state and routes mutations elsewhere; here it owns the **Designs
Store** I/O directly (designs are app-managed catalog data, not World state).
Since v0.25 / ADR 0032 the editing model is **In-Place Row Editing** (below):
the screen opens focused on the vehicle column with the cursor on a fresh
stage's **Placeholder Row**, and common edits happen on the rows without
visiting the palette.
_Avoid_: Editor, Configurator (that name belongs to the spawn-form quick stack
builder — coarser, whole-modules-only, not persistent), Workshop.

**In-Place Row Editing** (v0.25 / ADR 0032):
The VAB editing idiom — the maneuver-form pattern (`tab` moves focus, `←/→`
changes the focused field's value) applied to the kind-folded vehicle rows.
`←/→` **swaps the selected row's Component within its kind** (cycle engines on
an engine row, tanks on a tank row); `tab`/`shift+tab` is the sole column
switch. The bag model and **Design** schema are unchanged — only the
interaction over them.
_Avoid_: Slot form (rejected — a strict 1-engine+1-tank form can't hold the
honest multi-engine cluster), Modal picker.

**Chemistry Leader** (v0.25 / ADR 0032):
The rule that resolves the tank↔engine chemistry deadlock during **In-Place
Row Editing**: the **engine row cycles ALL engines** and so *leads* the stage's
fuel chemistry; **fuelled (tank) rows cycle compatible-only**, following the
engine. A chemistry-crossing engine swap lands and leaves the stage
soft-invalid (the **Aggregation** mixed-fuel warning) until each tank row is
re-cycled to the new chemistry — one `←/→` per tank. The `[a]` add path still
rejects mismatches outright.
_Avoid_: Permissive cycling (rejected — tanks would spend most of the cycle
invalid), Whole-stage rechem key.

**Placeholder Row** (v0.25 / ADR 0032):
A synthetic `engine —` / `tank —` prompt row the VAB shows on a stage missing
that propulsion kind, so `←/→` fills it in from nothing through the catalog
with no palette trip. A truly-empty stage shows both; once one propulsion kind
is present the *other* stays prompted; a non-propulsion (structure/core-only)
stage shows none. Modeled as a **vabGroup** with an empty `compID`; not a real
Component until picked.
_Avoid_: Empty slot, Ghost part.

**Crack-Open** + **Vab Seed** (v0.25 / ADR 0032):
Crack-Open is the `enter`-on-a-catalog-stage-header action that converts an
atomic catalog **Part** into its editable seed **Components** in place (seam /
decouple flags ride along), with a flash showing the honest Δv delta. The seed
is a **Vab Seed** — an optional `vab_seed` component-ID list on a **Part** that
is **seed-only and never stat-bearing**: **Aggregation** reads `components`,
never `vab_seed`, so the part keeps its authored scalars and loadouts / budget
evals / golden tests are untouched by construction. The cracked aggregate may
differ from the part; that delta is shown, not hidden.
_Avoid_: Decompose (implies the part's *stats* come from the seed — they never
do), Explode.

**Σ Δv Target** (v0.25 / ADR 0032):
A session-only vehicle-level Δv goal (`t`), not persisted into a **Design**.
The stats strip renders `current / target (delta)`; with a tank row selected a
**hint** shows the count of that tank to add to close the gap (`+2 → Σ ≈ 9280
✓`) or reports it unreachable. Computed against whole-stack Σ (adding a tank to
one stage lowers every stage below it). A hint, deliberately not a solver.
_Avoid_: Optimizer, Solver (there is no engine suggestion or multi-variable
solve).

**Design** (v0.24 / ADR 0029):
A saved custom vehicle built in the **VAB**: a **Loadout** plus the **composed
Parts** it references, serialized as a self-contained catalog fragment. A
Design is **catalog data, not save state** — global across games, no
save-schema bump — and lives in the **Designs Store**. At spawn it resolves
against the live catalog into a flyable Loadout (composed Parts aggregated,
atomic refs resolved, plans carried), so a Design flies identically to a
built-in Loadout, and the spawn form lists Designs alongside the built-ins
(design once, launch many). A first-class noun, distinct from a built-in
**Loadout**.
_Avoid_: Loadout (a Design is a *player-saved* vehicle that resolves to its own
Loadout, outside the built-in catalog namespace), Blueprint, Craft file.

**Designs Store** (v0.24 / ADR 0029):
The app-managed directory of saved **Designs**
(`$XDG_CONFIG_HOME/terminal-space-program/designs/`), kept distinct from the
hand-authored modder overlay (`loadouts/`) so app-written files never override
a built-in by ID collision. The **VAB** owns its writes and deletes. A Design
file is a portable catalog fragment — copy it into the sibling `loadouts/`
overlay to publish it as a mod.
_Avoid_: Save dir (that is the game-save location — the **Saves directory**,
`saves/`), Catalog (the embedded / built-in set).

**Craft Category** (v0.24 / ADR 0031):
A display-only, hash-excluded grouping label on a **Loadout** that clusters
the spawn-form CRAFT TYPE picker under ~6 non-selectable headers: **Launch
Vehicles**, **Crewed Mission Stacks**, **Upper Stages**, **Landers &
Capsules**, **Tugs & Relays**, **Satellites & Payloads**. A trailing
**Custom & Designs** group holds the inline stack builder and saved VAB
**Designs** and is never filtered. Distinct from **Role**, which is
functionally overloaded (drives command-source defaulting) and must not be
repurposed for display. An unknown or absent `category` falls into "Other."
_Avoid_: Role (that is a behaviour-driving field, not a display group),
Group (ambiguous — use Craft Category when referring to the picker grouping).

**Surface-Launch Gate** (v0.24 / ADR 0031):
The physics predicate that determines whether `POSITION = launchpad` is
offered in the spawn form: the selected craft's bottom stage must achieve
**TWR ≥ 1** against the selected parent body's surface gravity. Derived
dynamically — no stored flag — so the gate is body-aware ("Moon yes, Earth
no" for a lander), auto-correct for new craft added to the catalog, and
never out of sync with actual launch capability. When the gate fails, the
launchpad option is skipped or shown as unavailable.
_Avoid_: Launch flag, Launchable badge (static alternatives — rejected
because they can't express body-dependent launch ability).

**System Filter** / **Show-All Toggle** (v0.24 / ADR 0031):
The spawn-form mechanism that hides off-**Scale Class** craft by default
(see **Scale Class**). The filter keys on Scale Class — `real` craft appear
in Sol (and any future Earth-class system); `stripped-back` craft appear in
Lumen (and any future stripped-back system). The `[f]` key is the **show-all
toggle**: when on, every catalog Loadout is listed regardless of scale;
Custom & Designs entries are always exempt (user content is never filtered).
Amends ADR 0014's no-filter rule, preserved as an opt-out, not removed.
_Avoid_: Hard filter (the filter is an opt-out, not a hard gate), Per-system
binding (the filter keys on Scale Class, not a per-loadout system list).

**Lumen Fleet** (v0.24 / ADR 0031):
The full-parity roster of **stripped-back** counterpart Loadouts for the
Lumen system — one role-for-role equivalent of every Sol catalog entry,
named in the **Lumen computing/typography theme** (extending the Kern Stack
precedent: Vector V, Raster Launch System, Packet 9, Buffer, Spool, Socket
Lander, Token Pod, Nudge Tug, Thread Relay, Heartbeat Keeper, Uplink Relay,
Relay Node, Endpoint Station, Scalar Probe, Node Carrier ×3, Scan Pack).
Each vehicle is sized to Lumen's ~3.4 km/s-to-orbit budget and validated by
a Δv eval test. Together with the **Kern Stack** they give Lumen a complete,
flyable fleet visible by default when the spawn form is opened in that
system.
_Avoid_: Lumen analogs (implies copies — these are in-universe vehicles with
their own names), Kern-named variants (the Kern Stack is one vehicle; the
fleet is named in the broader computing-term theme).

**Service Module (SM)**:
The propulsive half of the Apollo command-and-service module: carries the
SPS engine and all its storable propellant. Performs lunar-orbit
insertion (**LOI**), mid-course corrections, and trans-Earth injection
(**TEI** — the Apollo-specific Earth-return burn, distinct from the
general **Moon Return** planner). Has no heat shield and is **never
recovered** — jettisoned
during re-entry prep. After **Transposition** it is the bottom/firing
Stage of the surviving stack, pushing the **Command Module** plus the LM
nose payload.
_Avoid_: CSM (that name fuses SM+CM — keep them distinct), Service stage.

**Command Module (CM)**:
The crew capsule half of the command-and-service module: heat shield,
parachutes, RCS only, **no main engine**. The single piece that returns
and splashes down — the true surviving core of a lunar mission. Modelled
on the existing parachute-recovered **Capsule** (ADR 0008): a passive,
engineless Stage that re-enters under chute after the **Service Module**
is jettisoned.
_Avoid_: Capsule (that's the standalone Loadout; the CM is the Apollo
Stack's instance of the same recovery model), Command stage, Re-entry pod.

**Transposition**:
The post-TLI restructure of the lunar stack that makes the **Service
Module** the firing core with the **Lunar Module** as a releasable
**nose payload** — the in-sim analogue of Apollo's transposition-and-
docking. Happens during trans-lunar coast with **no Burn pending** (the
S-IVB does the *full* TLI solo first, then is discarded), which is why
it cannot be folded into TLI as a single continuous push. Resolves the
"wrong-engine" problem: without it the linear bottom-first chain fires
the LM Descent for LOI; after it the SM fires LOI as the real CSM did.
_Avoid_: Flip, Restage, Dock-around.

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

**Engine Bell**: a synthetic flare painted between `Stages[0]`'s bottom
edge and the flame, in the stage's color, at mouth width
`min(stage.width + 2, 7)`. Renders whenever `Stages[0]` has
`launchSpriteWidthPx ≥ 2`, `launchSpriteRowsPx ≥ 4`, and `Thrust > 0`
(a pure-monoprop RCS-tug as bottom stage gets no bell). Inferred from
geometry, not authored — no catalog field. v0.12 Slice 4 replaced
the v0.11.5 single flat row with a **3-row taper** (`EngineBellRows`)
flaring linearly from the stage *throat* width at the top (nearest the
body) to the *mouth* width at the bottom (the nozzle exit) — a more
authentic nozzle silhouette. A bell with no room to flare (mouth
clamped to the throat width, e.g. a 7-wide stage) stays a single flat
row. The bell is *hardware*, so it renders regardless of throttle; the
flame attaches just below the whole bell stack and inherits the mouth
width so exhaust visibly emerges from the nozzle.

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
primitive extends along `-CurrentAttitudeDir`, a basic cone whose
height is throttle-binned (2 / 3 / 4 sub-pixel rows, capped at
`flameMaxRows`) and whose width tapers from the bell mouth at the top
to half that at the tip, pulsed by a wall-clock 100 ms frame shift.
Flame color is looked up from `Stages[0].FuelType` via a fixed palette
(see [[#maneuver--thrust|Fuel Type]]); empty / unknown FuelType falls
back to amber `render.ColorWarning` for backward compatibility with
un-catalogued stages. v0.12 Slice 4 made the plume **two-colour**: a
hot warm-white core (`flameCoreColor`) down the central
`flameCoreWidth(w)` columns of each row wide enough to resolve one
(≥ 3 sub-pixels), with the fuel tint on the edges — the white-hot
Mach-diamond read. The core is a warm cream, deliberately a step off
the cold pure-white of the [[#attitude--rcs|RCS]] **RCS Puff** so the
white-vs-coloured "RCS = cold puff, main engine = hot plume" contrast
below still holds. The flame renders only while the Vessel has an
active `ManualBurn` or `ActiveBurn` — pad-spawn loadout-default
Throttle=1.0 alone does not paint flame.

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
integrator (each against its own [[#bodies--systems|System]] — see that
entry), still rendered when their System is in view, still subject to
maneuver nodes that fire on schedule — but inert to live input until the
player switches active. The camera follows the Active Vessel's System: a
switch (cycle key, numbered slot, docking / end-flight auto-switch) snaps
the viewed System to the incoming Vessel's, so the Vessel being flown is
always in frame (ADR 0015,
`designdocs/terminal-space-program/adr/0015-vessels-bound-to-system.md`).
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

**Camera Contract**:
The rule that camera fit (center + zoom) is solely the player's: the sim
may fit the canvas only once per **Framing Event**, and ambient sim-state
changes (a SOI Pass appearing, an approach closing) never move the camera
(ADR 0021). Three closed carve-outs answer player-initiated events:
ViewLaunch auto-route on launch, the burn-frozen center during a Burn,
and system-follows-active-vessel on vessel switch.
_Avoid_: Auto-fit policy (names a mechanism, not the rule), Camera lock.

**Framing Event**:
A player action that changes the camera's framing context — a Focus
change, a ViewMode change, or a System switch — and the only occasions
the sim may fit the canvas. The fit *value* may read sim state (focusing
a Body with an active SOI Pass fits to its SOI Ring); the fit *timing*
may not. Manual zoom composes over the fitted base and persists until
the next Framing Event.
_Avoid_: Refit, Auto-frame (the per-frame behavior ADR 0021 retired).

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
flow are different. The moon→parent direction of that planner is the
targeted **Moon Return** (ADR 0013); the legacy `PlanMoonEscape`
function name is retained.
_Avoid_: Satellite (ambiguous with player-launched vessels), Subplanet.

**Primary**:
The Body that another Body or Vessel orbits — the gravitational parent
in the SOI hierarchy. Relative, not absolute: the Sun is Earth's
Primary, Earth is the Moon's Primary. Used to name reference frames
("rebase to the destination's primary frame") and to walk the
patched-conic SOI tree.
_Avoid_: Parent (ambiguous with scene-graph parents), Center,
Barycenter (the System's index-0 body may be a star, not a barycenter).

**System**:
A named collection of Bodies orbiting a common Primary — Sol, Lumen,
or a user-supplied overlay. Loaded from embedded `systems/*.json` plus
user files in `$XDG_CONFIG_HOME`; user files win on `systemName`
collision. Save files carry a `body_catalog_hash` so loading a save
under a different System catalog fails fast.

**Each Vessel is bound to exactly one System, fixed at spawn, for its
lifetime** — there is no interstellar transfer, SOI transitions are always
within one System, and [[#docking--coupling|Docking]] cannot cross Systems
(the same-SOI gate). The simulator integrates each Vessel against *its own*
System regardless of which System the camera is currently viewing, so a
parked Vessel in one System keeps orbiting correctly while the player flies
another in a second System. A Vessel's System is the System that was in view
when it was spawned (an *Alongside* spawn inherits the Active Vessel's System
instead, since it clones that Vessel's state). This lifts the original v0.1
"spacecraft restricted to Sol" cap (ADR 0015,
`designdocs/terminal-space-program/adr/0015-vessels-bound-to-system.md`).
_Avoid_: Galaxy, Universe, World. Home System / Host System (a Vessel does
not travel *between* Systems — the binding is permanent, not a base to
return to).

**Scale Class**:
A coarse size/difficulty tag shared by a System and a Loadout: **real**
(Sol-scale — Earth-class bodies, ~9.4 km/s to orbit) vs **stripped-back**
(Lumen-scale — ~1/10-linear bodies with Earth-like surface gravity,
~3.4 km/s to orbit, modelled on the Kerbal Space Program stock system).
Purely a classification: the integrator derives all dynamics from a
Body's mass and radius, so a System needs no Scale Class to work. The
tag drives the spawn form's **system filter** (v0.24 / ADR 0031) and the
per-craft Δv-to-orbit hint: the spawn picker shows only Loadouts whose
Scale Class matches the active System's by default, so Sol shows the
real fleet and Lumen shows the stripped-back fleet. The **System Filter**
is an opt-out — a show-all toggle (`[f]`) reveals every Loadout and
preserves the spawn-anywhere escape hatch as an explicit choice. (Prior to
ADR 0031, craft were **not** filtered; the old "best for" hint was the only
signal.) **Lumen** is the canonical stripped-back System; the **Kern Stack**
plus the full **Lumen Fleet** (v0.24 / ADR 0031) are its scale-matched
vehicles. Cite ADR 0031 for the filter reversal.
_Avoid_: Difficulty, Tier (overloaded), Realism mode.

**Sphere of Influence (SOI)**:
The spherical region around a Body inside which that Body's gravity
dominates and the patched-conic approximation treats it as the sole
attractor. The SOI boundary is where the integrator hands off from
one Primary to another (`internal/physics/soi`). Player-visible: the
HUD announces entries and exits, and warp clamps near boundaries.
_Avoid_: Hill sphere (mathematically distinct), Gravity well.

**SOI Pass**:
The predicted transit of the active Vessel's *live, unburned* trajectory
through a Body's **Sphere of Influence** — the always-on forecast of
where the current orbit will carry the Vessel *next*, drawn ahead of
arrival regardless of whether a **Target** is set or a Capture Burn is
planted. Detected by forward-propagating the live state across SOI
boundaries (the same SOI-segment predictor the planted-node legs use),
bounded a few **patches** deep. Characterised by its **Perilune** (the
closest approach to the passed Body) and **Time to Perilune**. A SOI Pass
whose Perilune falls below the Body's surface is an **Impact**. Once the
player plants a Capture Burn at Perilune the Pass becomes a closed
Capture Orbit — a different prediction (the node leg), not a SOI Pass.
After SOI entry the Pass persists as the *in-SOI residence* variant
(#157): while the Vessel is inside a non-root Body's SOI on a
trajectory that leaves it, the same picture — ring, Local-to-Body arc,
Perilune + SOI Exit markers, chip — draws around the current Primary
(no Entry marker; that crossing is in the past) and continues past the
exit into the onward path. A captured orbit (bound, apoapsis inside
the SOI) quiets it.
Distinct from **Encounter** (craft-to-craft — see Encounter math) and
from the **Target** slot (a SOI Pass renders whether or not the Body is
targeted). The HUD reports it as the SOI PASS block; on the orbit canvas
the in-SOI arc draws as a **Local-to-Body Arc** inside the Body's
**SOI Ring**, and the **Perilune** point carries a marker (the unified
marker glyph system).
Design: ADR 0019 (`adr/0019-soi-pass-forward-prediction.md`) in the
planning vault; markers per ADR 0020 (`adr/0020-unified-orbital-marker-glyphs.md`);
arc frame + camera per ADR 0021 (`adr/0021-player-owned-camera-local-to-body-arcs.md`).
_Avoid_: Encounter (reserved for craft-to-craft), Intercept / Approach
(Closest Approach is the craft-to-craft term), Conic patch (Patch names
the arc, not the event).

**Perilune**:
The **periapsis** of a SOI Pass measured in the passed Body's frame —
the lowest point of the predicted flyby. "Perilune" is used as the
general body-relative periapsis on any Body (not only Moons), reported as
an altitude above that Body's surface. Computed by the moon-frame
`targetPerilune` helper (hyperbolic elements, `rp = a·(1−e)`); paired
with **Time to Perilune**, the seconds until the Vessel reaches it.
_Avoid_: Periapsis (correct but unqualified — Perilune names the
body-relative apsis of the predicted Pass specifically).

**Local-to-Body Arc**:
How any predicted trajectory segment inside a foreign SOI is drawn:
sampled relative to the encounter Body and anchored at the Body's
*current* position, so the hyperbola wraps the Body's drawn disk and
converges to truth as arrival nears (ADR 0021 — KSP's local-to-body
conic mode). Replaces drawing those samples at their heliocentric
positions, where the Body's own motion smears the arc into an unreadable
streak many times the SOI. Applies to every foreign-SOI segment — live
Pass arc, counterfactual, and planted-node legs — so the pictures at one
Body never disagree. The arc meets the heliocentric transfer leg with a
deliberate break at the SOI boundary, made legible by the **SOI Ring**.
_Avoid_: Rebased arc (mechanism, not the name), Encounter arc
(Encounter is reserved for craft-to-craft), Relative mode (the KSP
setting this deliberately is *not*).

**SOI Ring**:
The dim dotted ring drawn at a Body's parent-relative **Sphere of
Influence** radius while a SOI Pass to it exists — including the
in-SOI escape-residence case (#157): while the active Vessel sits
*inside* a non-root Body's SOI on a trajectory that leaves it
(hyperbolic, or bound with apoapsis at/past the SOI radius), the ring
persists around the current Primary, so SOI entry doesn't switch the
boundary off mid-transit. Gives the **Local-to-Body Arc** its scale
and a boundary to visibly enter and exit on; carries the SOI Entry and
SOI Exit marker glyphs (ADR 0020 family, per ADR 0021). Quiet bodies —
no active Pass, or a captured orbit wholly inside the SOI — draw no
ring.
_Avoid_: SOI circle, Influence boundary (informal).

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
- A **Parachute**-decelerated Touchdown — a Vessel *without*
  `CanSoftLand` whose chute is **deployed** and which arrives with
  `|V| < V_CRIT` (nose-alignment waived) also becomes Landed (v0.12+,
  ADR 0008). The second non-engine route into Landed. See
  [[#parachute|Parachute]].

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
and routes to one of two outcomes:

- **Touchdown** — predicate satisfied → Vessel becomes **Landed**
  at the impact (lat, lon).
- **Crash** — predicate fails on velocity / attitude / capability →
  Vessel becomes **Crashed**. A non-`CanSoftLand` Vessel that grazes
  the surface gently lands here too — it is Crashed, not a third
  state.

The classification is exhaustive: every Surface Contact resolves to
Landed or Crashed. ADR 0004 originally shipped a vestigial third
"fallback" bucket (zero-V, neither flag set) as a defensive
placeholder; v0.11.x playtest confirmed it never occurs in practice
(`TestImpactorTrajectoryHitsSurfacePredicate` pins this), and v0.12.0
deleted it.

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
  orientation tolerances. Two routes qualify, each with its own
  gates: the **engine route** —
  `vessel.CanSoftLand && |V_impact| < V_CRIT && nose·local_up >
  NOSE_TOL` (a Vessel designed to thrust in); and the **chute route**
  (v0.12+, ADR 0008) — `chuteDeployed && |V_impact| < V_CRIT`, which
  **waives** the nose-alignment gate because a [[#parachute|Parachute]]
  is the stabiliser, not the pilot's attitude. Either produces a
  **Landed** Vessel that co-rotates with the ground, preserves fuel and
  stage state, and can re-ignite for liftoff (the same end-state a
  **Launchpad** spawn creates, but earned through controlled descent
  rather than spawned in).
- **Crash** — an arrival outside those tolerances — excess descent
  velocity, off-vertical attitude (engine route), or a Vessel with
  neither `CanSoftLand` nor a deployed chute regardless of how gently
  it touched. Produces a **Crashed** Vessel (terminal state, removed
  via End Flight).

Differentiated in code as of v0.11.4 (see ADR 0004 in the planning vault,
`designdocs/terminal-space-program/adr/0004-crashed-landed-lifecycle.md`);
the chute route added v0.12+ (see ADR 0008,
`designdocs/terminal-space-program/adr/0008-parachutes-atmospheric-descent-recovery.md`).
Constants `V_CRIT = 10 m/s`, `NOSE_TOL = 0.7` (≈ 45° from
local-vertical); both retunable.
_Avoid_: Soft landing / Hard landing (longer; "Landing" alone
overloads with the cluster heading), Successful landing / Unsuccessful
landing (asymmetric and verbose).

**Parachute**:
A capsule-class recovery device giving a Vessel *without*
[[#touchdown--crash|CanSoftLand]] a non-engine route to a soft
**Touchdown** via aerodynamic deceleration (v0.12+, ADR 0008). Two
parts, mirroring the `CanSoftLand` capability/state split:

- *Capability* — a per-**Stage** catalog flag (`HasParachute`),
  synced to a Vessel-level mirror from `Stages[0]` like
  [[#touchdown--crash|CanSoftLand]] and
  [[#ballistic-coefficient-bc|Ballistic Coefficient]]. Rides the
  hardware across a decouple. Today's bearers: the `csm` stage and a
  standalone re-entry capsule loadout.
- *Deploy state* — a runtime enum on the Vessel (beside **Landed** /
  **Crashed**): **Stowed** → **Armed** → **Deployed**, one-way,
  Deployed terminal. There is **no torn / failure state** — the chute
  is forgiving (over-speed tearing was considered and cut).

Lifecycle: the player **arms** the chute through the ordinary
[[#vessel-construction--lifecycle|Stage]] (`space`) action — it is
"just another staging action," not a new key. Because the chute rides
the surviving *top* stage and `space` pops the *bottom*, arming is the
final staging action once the Vessel is its bare chute-bearing stage;
allowed in any conditions, including vacuum. An armed chute
**auto-deploys** the first tick dynamic pressure
`q = 0.5 · ρ · |v_rel|²` reaches `ChuteDeployQMin`. While **Deployed**,
`EffectiveBallisticCoefficient()` returns a fixed `ChuteDeployedBC`
(≈0.3 m²/kg) — an absolute replace (the canopy swamps the capsule's own
drag), so terminal velocity `v_term = √(2g/(ρ·BC))` ≈ 7.4 m/s at Earth
sea level, **mass-independent** and comfortably under `V_CRIT`. Rendered
as a HUD state + descent-rate readout, plus a synthetic braille canopy
above the top stage in [[#launch-sprite|ViewLaunch]].
_Avoid_: Chute (fine in code/prose shorthand; **Parachute** is the
canonical noun), Drogue (a specific reefed-stage chute the model
doesn't have yet), Airbrake (a different drag device).

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
A **deployed** [[#parachute|Parachute]] short-circuits this whole
chain — `EffectiveBallisticCoefficient()` returns a fixed
`ChuteDeployedBC` (≈0.3 m²/kg) outright while the chute is up (v0.12+,
ADR 0008).
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

### Deployable payloads & CommNet

How a Carrier Vessel releases a carried payload (v0.23 / ADR 0028)
and how uncrewed vessels maintain command through the relay network
(v0.23 / ADR 0027). The section covers both cycles because Deploy
depends on CommNet: the payloads that make Deploy useful (relay sats,
ground stations) are CommNet nodes.

**Deployable Payload**:
A **Loadout** (or sub-stack) carried inside a **Composite** and
intended to be released mid-flight as its own **Vessel** using the
**Deploy** verb. A payload is defined entirely by its parts — a relay
antenna makes it a relay node, soft-land legs + a relay antenna make it
a **Ground Station**, a probe core makes it an uncrewed probe. There is
**no Deployable/IsPayload flag** on the Docked Component; the player's
choice of verb (Deploy vs Undock) determines behaviour (ADR 0028
decision 4).
_Avoid_: Satellite (ambiguous with passive Vessels), Cargo (reserved for
inert cargo tonnage, which is deferred).

**Deploy** (verb / action):
The player action that releases the **topmost** nose payload from a
**Carrier** Composite and **keeps the carrier as the active Vessel** —
drop-and-continue. Bound to `Y`. Emits the `deploy` semantic **Action**
(ADR 0025 vocabulary). Distinct from **Undock** (`U`): Undock releases
and *switches* the active Vessel to the released component, and is used
for in-flight docking composites that rendezvoused and docked. Deploy
reuses the same Undock release mechanism (separation push, Spring Release,
new craft ID stamped) — the only code difference is which Vessel stays
active. Deploy is blocked on a Vessel with no carried payload (guard
prompt rather than no-op).
_Avoid_: Undock (distinct verb — different active-craft post-release
behaviour and different tutorial/objective vocabulary), Release (valid
prose, but Deploy is the canonical noun for this specific verb).

**Carrier**:
A **Vessel** (typically a relay tug or upper stage) whose **Nose Payload
Plan** carries one or more **Deployable Payloads**. The Carrier stays
active after each **Deploy** press, allowing a single launch to deploy a
full constellation by pressing `Y` repeatedly in sequence. No special
type or flag — "Carrier" is a descriptive role (see **Role**) for any
Vessel that has a Payload Stack mounted on its nose.
_Avoid_: Tug (the relay-tug loadout is a Carrier, but not all Carriers
are tugs), Mothership (informal).

**Payload Stack**:
A **Composite** assembled at spawn from a multi-entry **Nose Payload
Plan** (e.g. `[1,1,1]` — three single-stage payloads), producing a
carrier core with N stacked docked payloads ordered top-down. The player
deploys them one by one from the top with repeated **Deploy** presses; a
single-entry plan is byte-identical to the v0.14 single-payload behaviour.
Distinct from the **Stage** stack (the propulsion chain on the carrier
core itself).
_Avoid_: Payload manifest (that implies an inert-cargo model; this is
docked-component identity), Multi-payload stack (verbose; Payload Stack
is the canonical noun).

**Role**:
A descriptive UI string on `Spacecraft` and `Loadout` (`"relay-sat"`,
`"science-probe"`, `"ground-station-lander"`, etc.) used only for player
labels and spawn hints — **no behaviour is keyed on it**. Capabilities
come from parts (a relay antenna confers relay behaviour; legs + relay
confer ground-station behaviour), not from the Role string. Role is the
observable shorthand for "what this craft does," not a behavioural
taxonomy. Pre-v0.23 uses of Role (e.g. "Command Module" for the Apollo CM
re-entry role) follow the same rule.
_Avoid_: Craft type (implies a behavioural enum, which this is not),
Vessel class (same problem), Kind (in this context; Kind is already the
Objective family discriminator).

**Relay Sat**:
A descriptive **Role** for an orbiting relay-antenna **Vessel** —
typically one deployed from a **Carrier** via **Deploy**. Its relay
behaviour comes from having `antenna: {kind: relay}` on its probe stage;
once in range and unoccluded it automatically joins the **CommNet** relay
graph as a forwarding node, extending coverage without any player "enable
relay" action. The relay comsat loadout is the canonical starter unit.
_Avoid_: Relay satellite (verbose; Relay Sat is the canonical short form
in prose), Relay drone, Comm sat.

**Ground Station** (player-deployed):
A **Vessel** with a relay antenna that has **Landed** on a Body's surface —
automatically a body-fixed relay node in the **CommNet** graph, with no
"establish station" action required. Distinct from the **DSN ground-station
ring** (catalog stations baked into `ground_stations.json`, always present)
and from a **Launch Site** (a (lat, lon) spawn preset). A player deploys
one by flying a Ground-Station Lander loadout — soft-land legs + a relay
antenna + a probe core on one stage — to a Body's surface and touching
down; once **Landed**, the relay-antenna craft is immediately a body-fixed
relay node, with no staging or "establish station" step. The relay graph
treats Landed Vessels with relay antennas identically to DSN stations —
they are body-fixed (co-rotating) relay nodes.
_Avoid_: DSN (that's the catalog ring), Station (overloaded with space
stations), Network node (engineering term, not player-facing).

**CommNet** (Communications Network):
The relay graph that determines whether an uncrewed **Vessel** can be
commanded. Computed once per tick from all antenna-equipped Vessels plus
the **DSN ground-station ring**; each probe Vessel's reachability is BFS
from that Vessel through relay hops to any ground station. **Crewed**
Vessels are never gated (they always have `Controllable = true`);
uncrewed **probe** Vessels require a relay chain to be commandable.
A **home-telemetry blanket** (v0.24.1) short-circuits the LOS test for a
probe in a low orbit of a body that hosts ground stations: within
`nearHomeRadiiFactor` (1.5) × the primary's radius of its centre (≈ 0.5 R
altitude — ~3,200 km at Earth, ~300 km at Kern), the probe is connected
regardless of occlusion, modelling the dense near-body network. Without it
a low / equatorial orbit can't see the mid-latitude DSN ring at all (the
body occludes every station) and would read NO SIGNAL right after launch.
Implemented in `internal/sim/commnet.go`; exposed on `World.CommGraph`
(v0.23 / ADR 0027).
_Avoid_: Signal network, Radio network, KSP CommNet (same metaphor but a
different implementation — no antennas-combine formula this cycle).

**Command Source**:
The per-**Stage** declaration of how a Vessel gets command authority.
Three values: **crewed** (a crewed pod — always commandable, never relay-
gated; `CommandCrewed`), **probe** (an uncrewed probe core — commandable
only via a **CommNet** relay chain; `CommandProbe`), and none/empty (no
command authority — a propulsion-only stage, debris). A Vessel is
`Controllable` iff any stage is a command source; `Crewed` iff any stage
is `crewed`. Old saves without explicit `command_source` fields get a
default backfill so they remain fully controllable (no migration).
_Avoid_: Pilot, Control authority (verbose), Command capability.

**Antenna** (direct vs relay):
The per-**Stage** communications hardware declaration. Two functional
kinds (besides `none`):
- **Direct** (`AntennaDirect`) — can receive commands from and report to
  a ground station directly, but **cannot forward** traffic for other
  Vessels. Reaches up to the direct-basic tier range.
- **Relay** (`AntennaRelay`) — can use the network *and* forward traffic
  for others, making the Vessel a potential relay hop. The `Controllable`
  check gates forwarding so a dead probe can't silently relay for others.

Both carry an `AntennaRangeM` (metres) that constrains link distance.
Three informal tiers used in catalog comments: **direct-basic**
(LEO-to-geostationary range; Station-Keeper), **relay-cislunar** (Moon
and below; Relay-Tug and Relay Comsat), **deep-space** (Mars-class
distances). These tiers are named conventions in catalog annotations, not
an enum in code.

Every commandable Vessel carries at least one antenna: `EnsureCommandSource`
backfills a **direct-basic** antenna onto any non-debris Vessel that carries
none — probe *and* crewed pod (v0.24). A probe without one would be
permanently uncommandable under **command gating**; a **Crewed** Vessel is
never gated, so for it the basic antenna is presence-only (it shows on the
**CommNet** as a non-forwarding node, and the comms chip stays hidden), but
"all Vessels carry an antenna" keeps the network model uniform.
_Avoid_: Omni vs High-gain (KSP's distinction — this model uses direct
vs relay), Antenna power (the field is called `range_m` in the catalog
JSON, not a watts/power figure).

**DSN ground-station ring**:
The catalog default **CommNet** anchor: a ring of three fixed ground
stations embedded in `ground_stations.json` at ~120° longitude separation,
each with a high station-class relay range (`DefaultGroundStationRangeM`,
5,000,000 km — above the relay-cislunar tier, below deep-space). One ring
sits on **each home body with launch infrastructure** — Earth in Sol (the
real Goldstone / Madrid / Canberra sites) and **Kern** in Lumen (Stdin /
Stdout / Stderr, the three standard streams) — so a Vessel launched in
either system can reach the network (v0.24). The connectivity graph only
includes stations whose body is in the active craft's system, so the rings
never cross-talk. Loaded at world init into `World.GroundStations`.
Player-deployed **Ground Stations** extend a ring; the DSN rings cannot be
removed by the player. User overlays can add or replace stations by `key`.
_Avoid_: DSN ring (bare acronym — "DSN ground-station ring" on first use;
DSN ring fine thereafter), Home network, Default stations.

**Command gating / NO SIGNAL**:
The control-authority enforcement layer (v0.23 / ADR 0027 C2-5). When a
probe Vessel has no **CommNet** connection (`!World.CanCommandCraft(c)`),
all command-mutating World methods (plant/edit/delete node, throttle, stage,
set attitude/NavMode) return early without effect and emit a transient
"NO SIGNAL" status flash. Planning reads (predictions, porkchop), camera,
and warp are **not** gated — the player can still observe and plan, just
not command. Crewed Vessels are never gated.
_Avoid_: Signal loss (implies something broke; gating is intentional
design), Comms blackout (implies a transient — gating is permanent until
connectivity is restored).

**Relay forwarding**:
The property of an **AntennaRelay** node that allows it to pass command
traffic onward to other Vessels in the **CommNet** graph. Only Vessels
that are both `AntennaKind == relay` and `Controllable` can forward; a
dead probe with a relay antenna is a dead end (it can receive but not
forward). A **DSN ground-station ring** station is always a forwarding
endpoint (the source of authority, not a relay hop). Forwarding is
purely topological: the relay graph finds the shortest hop chain from a
probe to any ground station; the relay path is rendered on the orbit
canvas as a styled line (v0.23 ADR 0027 C2-7).
_Avoid_: Retransmit (correct in RF but not player vocabulary), Relay
hop (fine in prose for one segment of the chain; relay forwarding
describes the property).

**Coverage objectives** (`relay_coverage` / `establish_contact`):
Two mission **Kinds** (ADR 0027 / ADR 0025) that evaluate the **CommNet**
graph:
- **`relay_coverage`** — passes when ≥N relay-antenna Vessels in the
  slate all have `HasConnection == true`. Used for constellation
  missions ("deploy and connect 3 relay sats").
- **`establish_contact`** — passes when the active Vessel currently has
  `HasConnection == true`. Instantaneous + pass-and-stick, like
  `reach_altitude`.

Both are slate-wide (they read `World.CommGraph`) and require no per-craft
binding — the relay graph's reachability covers the durable "is the relay
still connected?" check as long as coverage holds.
_Avoid_: Connectivity objective (valid description; `relay_coverage` and
`establish_contact` are the canonical Kind strings), Coverage check.

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

**Auto-Warp**:
A player-engaged time-warp *driver* — the one actor that *raises* Selected
Warp on the player's behalf, as opposed to the clamps, which only lower
Effective Warp. Engaged with a single control (key or a title-bar button),
it accelerates time toward an upcoming Burn, then ramps back down and hands
control to the player a fixed lead — 30 s of sim-time — before that Burn's
start, leaving the sim at 1× so the player can watch the Burn arm and fire.

- It never adds a new way to skip a Burn window: each Tick it simply takes
  the fastest rate the existing Effective-Warp clamps already allow, so the
  node-approach ramp and the step-size guard remain the hard safety net.
- It aims at the soonest Burn (earliest *burn start*) among the Vessels in
  the active Vessel's System as of the moment it was engaged — System-scoped
  so it never warps to an off-screen Burn in another System (ADR 0015) —
  then follows that specific Maneuver Node if its timing shifts. It
  disengages on arrival, if the player touches warp manually, or if that
  Node is removed.

Distinct from the *node-approach ramp* (a clamp that caps Effective Warp
near any Burn so the integrator can't alias past it): the ramp is passive
and applies to every Burn; Auto-Warp actively steers time toward one and
then steps aside.
_Avoid_: Warp-to (verb-y — name the feature), Time-skip, Autopilot
(reserved for attitude/throttle control, never time).

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
into the transfer and picks, by total Δv, between two shapes — a
**combined** transfer (one `BurnVector` Departure, solved by a Lambert
arc, that folds plane + raise together) and a **split** transfer (a
near-coplanar raise plus a `BurnPlaneChange` node placed where the
Vessel crosses the target's orbital plane — the **line of nodes** —
which is both a cheap place to change planes and the *only* place a
plane change leaves the Vessel actually *in* the target's plane).
A correct Transfer Plan must arrive **coplanar** with the target (≈0°
relative inclination) **and at the Capture Orbit radius** — a safe
periapsis (`destination radius + 200 km`), not the target's *centre*.
Coplanar alone is insufficient: a transfer solved to the target's
centre arrives with a sub-surface perilune (a collision) while its
Capture Burn Δv is sized for a periapsis it never reaches. So the
planted arrival is aimed at an **in-plane offset** chosen so the
natural flyby perilune lands at the Capture Orbit radius, **prograde**
around the target (the affordable capture direction — see GH #68): the
combined Departure offsets its Lambert aim point, the split trims the
raise's far apsis off the target's orbit ring by the impact parameter
(GH #159 — pre-fix the split's node-aligned rendezvous dead-centred,
a radial plunge when coplanar). The Δv *comparison* between the two
stays centre-aimed so the offset never flips strategy selection. The
HUD shows both candidate costs. This retires the old
`I`-plane-match-then-`H` dance as a *requirement* (the manual tools
remain available).
_Avoid_: Trajectory (the whole flight path; the Plan is the burns).
The planner's internal `TransferNode` is not glossary-worthy — it's a
handoff struct (see Flagged ambiguities). **Do not** conflate "plane
change at apoapsis" with "plane change at the line of nodes": changing
velocity at apoapsis rotates the orbit but does *not* move the Vessel
into the target's plane unless apoapsis already lies on the line of
nodes — the distinction is load-bearing for arrival (see GH #67).

**Moon Return**:
The moon→parent direction of an auto-planted **Transfer Plan**: a
targeted **Departure** that injects a Vessel from a lunar **Parking
Orbit** onto a parent-frame arc reaching a chosen perigee, with a
zero-Δv **Arrival** marker at that perigee (the player finishes the
capture by hand — aerobrake or powered). Unlike the outbound transfer,
the Departure is a single **BurnVector** sized so the post-SOI
parent-frame orbit's perigee is `parent radius + 200 km`, aimed so v∞
leaves the SOI **retrograde to the moon's orbital motion, in the moon's
orbital plane** — the cheapest controllable target (arrival inclination
≈ the moon's plane). Folding the plane change into the one departure
burn lets a (possibly inclined) Parking Orbit still inject cleanly; and
because the target plane is the moon's *own* plane, there is **no
around-parent phasing wait** — only a short intra-moon wait for the
parking-orbit point that aims v∞ correctly. Replaces the v0.6.3
minimum-escape objective, which spent the least to leave the SOI but
dumped the Vessel in ≈ the moon's own orbit (perigee ~300 000 km),
~590 m/s of avoidable departure waste plus uncontrolled phasing
(ADR 0013, `adr/0013-moon-return-targeted-injection.md`). The legacy
code function is still `PlanMoonEscape`.
_Avoid_: Moon Escape (the retired minimum-escape objective — the planner
now targets a perigee), TEI (that names the Apollo **Service Module**'s
specific Earth-return burn, not this general planner — though a Moon
Return *is* a TEI for the Earth / Luna case).

**Line of Nodes**:
The line where the Vessel's orbital plane intersects the target's
orbital plane — the only two points in the orbit where the Vessel is
*in* the target's plane. A plane change executed here rotates the
Vessel's plane onto the target's while leaving its position on the
shared line, so the post-burn orbit is coplanar with the target. The
**split** Transfer Plan places its `BurnPlaneChange` here (at the
transfer apoapsis *and* on a node — the two must coincide for the
transfer to arrive), which is why the cheap apoapsis plane change also
produces a coplanar arrival. Direction is `n̂_vessel × n̂_target` (the
cross of the two plane normals).
_Avoid_: Ascending/descending node alone (those name the two specific
crossing points; the Line of Nodes is the axis through both), Node
(overloaded — see Maneuver Node and Flagged ambiguities).

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

**Objective**:
The atomic pass/fail predicate evaluated against live World state each
tick — the smallest goal unit. Carries a `Kind` (which predicate to run),
`Params` (kind-specific tuning, e.g. `MinAltitudeM`, `RangeM`), and a
three-state `Status` machine: **InProgress → Passed | Failed** (terminal
states sticky; `Evaluate` idempotent). Objectives come in two **families**:
*state* objectives — instantaneous world-state predicates (Kinds
**ReachAltitude**, **Circularize**, **OrbitInsertion**, **CircularizeFromPad**,
**SOIFlyby**, **LandAtBody**, **Rendezvous**, **Dock**, **ReturnToBody**) —
and *event* objectives (Kind **Event**), which match a semantic **Action**
(below) fired while the Objective was active rather than a world-state
predicate, for teaching controls that leave no world trace. An Objective
may declare opt-in `FailOn` conditions (**crashed**, **out_of_fuel**) — the
only path that produces Failed; declare none and it never fails (retry
forever). Evaluated against the Active Vessel.

**Mission**:
An ordered list of Objectives plus metadata — the player-facing goal
("Reach the Moon" as a checklist of sub-steps). The Mission carries the
sequencing memory (an Objective is evaluated only once every earlier one
has Passed) so each Objective stays memoryless: "Luna landing & return"
is one Mission whose ordered Objectives are `[LandAtBody] → [ReturnToBody]`.
Has its own rolled-up `Status` — Passes when every Objective has Passed,
Fails the moment any Objective Fails. Seeded from an embedded starter
catalog (+ user overlay) at world init; round-trips through save.

**Program**:
A lightweight campaign grouping — *not* a third container type. A Mission
carries a `Program` tag (**tutorial**, **challenge**) plus `Requires` /
`Unlocks` edges to other Missions, so the tutorial and any campaign are
both gated Mission chains. `Requires` gates *evaluation*, not just display:
a Mission whose prerequisites haven't Passed is skipped by the evaluator,
so a later rung can't latch out of order. Each Program is opt-in — off by
default, toggled in Settings.

**Action**:
A semantic gameplay verb the player triggers (e.g. `open_maneuver`,
`stage`, `plan_transfer`), recorded *downward* from the input layer via
`World.RecordAction` once a keybinding resolves to it — never the raw
keystroke, so event Objectives survive rebinding and layout presets.
Events flow `tui → sim → missions`; the `missions` package never imports
upward. The curated set is ~20 gameplay verbs; pure camera / nav / meta
bindings are excluded.

**Player surface**:
The missions screen (`M`, or the `[Missions]` button) is a gated **ladder**
— the active Mission as a checklist card on top, locked rungs shown with
what unlocks them, completed / failed rungs marked. An in-flight
**checklist chip** (ADR 0010) shows the active Mission's current Objective
plus N/M progress, flashes a Failed Mission for ~4 s before advancing, and
surfaces the step's instruction inline for tutorial Missions.
_Avoid_: Quest, Achievement; and don't swap the two core terms — the
inversion is load-bearing (an Objective is one predicate; a Mission bundles
several ordered Objectives). The pre-v0.21 naming, where a single predicate
was itself called a "Mission," is retired.

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
braille canvas. Projections only, per the **Camera Contract** (ADR
0021): a ViewMode never picks the camera's center or zoom. Values
cycled via `v` in this order: **ViewTilted** (the default — 3D-style
perspective using the active Vessel's perifocal basis with a
player-tunable polar tilt and yaw), **ViewTop** (drop world Z),
**ViewRight** (look from +X), **ViewBottom** (Top with Y inverted),
**ViewLeft** (Right mirrored), **ViewOrbitFlat** (project onto the
active Vessel's orbit plane). Plus **ViewLaunch** (auto-routed on pad
launch; exited via manual `v` cycle). The v0.17.3 **ViewTarget** and
v0.18.0 **ViewSOIPass** modes — which also set center and zoom — are
retired by ADR 0021; encounter readability comes from the
**Local-to-Body Arc** plus plain body Focus instead. Stored on
`World.ViewMode` so the orbit screen and the maneuver-planner
mini-canvas share the same angle without per-screen coordination.

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

**Body Texture**:
The per-pixel surface a Body paints inside its disk — continents,
craters, cloud bands, the day/night terminator — as opposed to the
flat single-color disk a texture-less Body renders as. Resolved from
a Body's **Texture Spec** and projected orthographically about the
**Sub-Observer Point** (the lat/lon facing the camera), then darkened
by the day/night **Shade** factor. Gated by a minimum pixel radius:
below it a Body is too few pixels to texture and stays a solid disk,
so textures only resolve as you approach.
_Avoid_: Sprite (reserved for the Vessel's [[#vessel-construction--lifecycle|Launch Sprite]]),
Skin, Map (collides with the OrbitView map).

**Texture Spec**:
A Body's declarative description of its surface, carried in the system
JSON (ADR 0024): a base color plus typed **Feature Kinds**. The same
spec drives every system, including user overlays — surfaces are
content, not code. Cosmetic only: a Texture Spec never affects physics
or Body references and is excluded from the Catalog Hash, so editing
one never rejects a save.
_Avoid_: Texture map (implies a bitmap; the spec is vector/parametric),
Material (graphics-pipeline jargon this game doesn't use).

**Feature Kind**:
One of the typed surface elements a **Texture Spec** lists, each with
its own look: **continents** (filled albedo regions, terrestrial),
**craters** (rimmed, optionally rayed impact marks, airless bodies),
**bands** (latitude cloud sweeps, gas/ice giants), **spots** (discrete
storms like a Great Red Spot), **mask** (a rasterised land/ocean/
desert/ice polygon grid with latitude biomes, Earth-class terrestrials),
and **star** (limb darkening + granulation; a star is the light source
and is exempt from Shade). A Body mixes whichever kinds fit its
**archetype**.
_Avoid_: Feature type (Kind is the canonical noun here), Layer
(implies strict paint order the schema doesn't mandate).

### HUD & overlays

How orbit-screen information is placed: a slim always-on column of core
readouts, compact overlays composited onto the canvas, and a momentary
hide-all gesture. The model and its rejected alternatives are recorded in
ADR 0010 in the planning vault
(`designdocs/terminal-space-program/adr/0010-hud-column-canvas-chips-and-settings.md`).

**HUD**:
The pinned core-telemetry **Chip** of irreducible vessel telemetry (name,
primary, fuel %, Δv budget, throttle, velocity), composited onto the
canvas's top-left corner. It is never hidden by [[#hud--overlays|Declutter]]
(F2 must not hide fuel/Δv mid-burn) and its contents are fixed, not
player-configurable — the one Chip that survives Declutter. Narrowed from the
pre-ADR-0010 sense, where "HUD" meant the whole tall stack of blocks; a
v0.13 playtest then moved it off a right-hand column onto the canvas (see
ADR 0010's amendment) so the orbit map spans the full terminal width.
_Avoid_: Sidebar, right bar, info panel, HUD blocks (the contextual ones
are now [[#hud--overlays|Chips]]).

**Chip**:
A compact (2–4 row) overlay composited onto a corner of the **Canvas**
carrying one contextual readout — Target, Stages, Nodes, Launch, Capture.
Most Chips render only when their Setting is enabled, they are contextually
relevant, and Declutter is off. The current **Orbit** metrics (apo/peri/incl)
are **always-on** (non-toggleable) — a player must never be able to hide them
from the **Settings screen** — though they still vanish under Declutter. The
**Nodes** Chip carries any in-flight **Burn** as its firing head (the active
● Burns readout was folded in here, v0.16); while a Burn is live it
**force-shows** — overriding both its Setting toggle *and* Declutter — so a
live Burn (safety-critical) can never be hidden. With nothing burning the
Nodes Chip honours its toggle and Declutter like any other. The **HUD** core
Chip and a live-Burn Nodes Chip are the only overlays that survive Declutter.
Distinct from the larger **Navball** panel, which is also a canvas overlay
but a fixed instrument.
_Avoid_: Widget, card, badge, HUD block, panel (reserve panel for the
Navball).

**Declutter**:
The momentary "hide all overlays" action (F2) that clears every Chip and
the Navball to expose a clean orbit view. Transient and unsaved — it does
not change the persisted Settings, and it never hides the **HUD** column.
_Avoid_: Hide UI, clean mode, F2 mode, toggle overlays.

**Settings screen**:
The menu-reached screen where the player toggles each toggleable Chip's
default visibility (and future preferences such as units). The always-on
Orbit readout is deliberately not listed; the **Nodes** Chip is listed
(toggleable) but force-shows while a Burn is in flight regardless, so its
firing-head Burn readout can't be switched off. Persisted to a global
`settings.json` under `$XDG_CONFIG_HOME`, separate from the **Theme** —
visibility versus colour are distinct concerns — and independent of any
save game.
_Avoid_: Options, preferences pane, config menu.

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

### Persistence & saves

**Save** (v0.26 / ADR 0033):
A persisted snapshot of the game **World** — a versioned JSON envelope
(header **Meta** + `Payload`) written as one file in the **Saves
directory**. Replaces the pre-v0.26 single fixed `save.json` slot.
_Avoid_: Slot (there is no longer a fixed number of them), Save file
(correct but overloaded — the envelope, not the directory).

**Saves directory** (v0.26 / ADR 0033):
The app-managed folder holding every **Save**,
`$XDG_STATE_HOME/terminal-space-program/saves/`. The directory *is* the
source of truth — the browser lists it by parsing each envelope's **Meta**
header, with no sidecar index to fall out of sync.
_Avoid_: Save dir (fine informally), Slot folder.

**Named save** (v0.26 / ADR 0033):
A **Save** with a player-chosen **Save name**, created via **Save-As** and
never overwritten except by explicit player action on that exact row. The
deliberate lane, as opposed to the managed **Quicksave** / **Autosave**
lanes.
_Avoid_: Manual save (rejected — naming is the distinguishing feature, and
"Named" reads cleanly against Quicksave/Autosave), Profile, Campaign (the
game has no campaign layer — Saves are flat and independent).

**Save name** (v0.26 / ADR 0033):
The human-readable display label stored in a **Save**'s envelope **Meta**.
**Not** unique (two Saves may share a name, told apart by saved-at
timestamp) and **not** the on-disk filename (filenames are opaque; rename is
a pure Meta rewrite). Defaults on Save-As to *active vessel + in-game day*.
_Avoid_: Slug, Filename, Title.

**Quicksave** (v0.26 / ADR 0033):
The single reserved **Save** written by **F5** and loaded instantly by
**F9**; ephemeral, always overwritten in place, never a **Named save**.
_Avoid_: Snapshot, F5 save.

**Autosave** (v0.26 / ADR 0033):
A reserved *rotating* **Save** lane (a ring of 3: `autosave-1/2/3`, oldest
overwritten) written on a real-time interval + on quit. Event-driven
autosave (SOI entry, staging, landing) is a deferred follow-on.
_Avoid_: Backup, Checkpoint (reserved for a possible future event-driven
lane).

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
> **D:** Each Tick the Objectives in your active Mission evaluate in order
> against your Active Vessel's state in its Primary's frame — yours is a
> one-Objective Mission (the Objective's Kind is OrbitInsertion). When the
> Objective passes, its Status flips Passed and sticks, and the Mission
> rolls up to Passed. Only the Active Vessel counts.

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
surface") collapses operationally distinct things — keep them apart:

- **Landed** (the runtime state) — the integrator-bypass mode that
  co-rotates the Vessel with the ground (`Spacecraft.Landed = true`).
  Reachable via a **Launchpad** spawn *or* a **Touchdown** (v0.11.4+,
  ADR 0004).
- **Surface Contact** (the physics event) — the clamp that fires when
  an arriving Vessel penetrates the surface. It zeros V, projects R
  back to the radius, and (v0.11.4+) routes to Landed or Crashed via
  the Touchdown predicate. It is the *event* that decides the outcome,
  not a resting state of its own.
- **Touchdown** (the controlled outcome, v0.11.4+) — a Surface Contact
  by a `CanSoftLand` Vessel within velocity + attitude tolerance →
  sets **Landed**.
- **Crash** (the destructive outcome, v0.11.4+) — any other Surface
  Contact → sets **Crashed** (inert wreckage, removed via `[E]`
  end-flight).

**Resolution:** capital-L **Landed** in prose means the runtime
state — the co-rotating integrator-bypass mode. **Surface Contact** is
the integrator event that decides which outcome fires; **Touchdown**
and **Crash** are those two outcomes (v0.12.0 removed the vestigial
third "neither flag" fallback — see the **Surface Contact** glossary
entry). Never say bare "landed" for a Crashed Vessel. When in doubt: a
Landed Vessel co-rotates with the ground; a Crashed Vessel is inert
wreckage.

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

**"Component"** is a three-way collision — two runtime senses plus, from
v0.24, a catalog one:

- **Stage** — one element of a Vessel's decoupleable propulsion stack
  (`Stages[i]`). Carries dry mass, fuel, thrust, Isp, ballistic
  coefficient. Indexed bottom-up: `Stages[0]` is the currently-firing
  engine. Player concept: *one fire-and-jettison unit of the rocket*.
- **Docked Component** — one element of a **Composite**'s
  `DockedComponents`. Carries identity-and-shape fields (name, loadout,
  dry mass, capacities, engine numbers) but **not** state, Maneuver
  Nodes, or Burns. Used by **Undocking** to restore the pre-Dock
  Vessels. Player concept: *the original ship I docked in*.
- **Component** (catalog, v0.24 / ADR 0029) — the finest *catalog* noun:
  an engine / tank / command-core / antenna / structure that **composes
  into a Part** via **Aggregation**. It is build-time catalog data, not a
  runtime element of a Vessel at all. Player concept: *a part I bolt onto
  a stage in the VAB*.

The first two are "parts of a Vessel" at different abstraction levels; the
third is catalog data one level *below* a Stage (Components → a composed
**Part** → one **Stage**). A Composite has both runtime senses: its
**Stages** are the concatenated propulsion stack (lead's + partner's,
appended on top), its **Docked Components** are the original Vessel
identities it can decompose back into. Docking does **not** add one Docked
Component per Stage — it adds *one per pre-Dock Vessel*, even if that Vessel
had multiple stages.

**Resolution:** "Stage" is the unambiguous term for the runtime propulsion
unit. Qualify bare "component" as **Docked Component** (a composite element)
or **catalog Component** (a VAB build-part) when ambiguity threatens — they
never appear in the same breath, since one is runtime and the other is
build-time. A diagnostic: if you see `len(c.Stages) == 3 &&
len(c.DockedComponents) == 2`, that's a Composite of two pre-Dock Vessels
whose stage counts sum to 3 (probably 1+2 or 2+1).

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
