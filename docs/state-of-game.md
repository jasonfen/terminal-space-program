# terminal-space-program — state of game

<!--
  meta:
    snapshot_version: v0.9.1 (with v0.9.2 ground-launch primitives in
      flight on branch — work-in-progress)
    snapshot_date: 2026-05-05
    revised_date: 2026-05-05 (v0.9.2 ground-launch slice feature-
      complete on `v0.9.2-ground-launch` branch / PR #51 — manual
      ascent to LEO unreliable, ships unmerged as WIP)
    archive: docs/state-of-game-archive.md
  Read the archive for the full v0.7.6-baseline-plus-v0.8-additions
  detail this rewrite condensed. This file is the canonical
  "what's the game today / where is it going" reference.
-->

> Snapshot at **v0.9.1** (May 2026), with **v0.9.2 ground-launch
> primitives in flight on branch** as work-in-progress (manual ascent
> unreliable — see [v0.9.2 (work-in-progress)](#v092-work-in-progress)).
> Predecessor doc with full per-feature detail preserved at
> [`docs/state-of-game-archive.md`](state-of-game-archive.md).

---

## What the game is

**terminal-space-program** is a terminal-native orbital-mechanics
rocket simulator — a take on Kerbal Space Program that lives in
your shell, distributed as a single static Go binary
(`CGO_ENABLED=0`, `~10 MB`, no toolchain dependencies).

It plays as a real two-body patched-conic sim with SOI-aware
state transitions, finite-burn integration, time-warp, planet
rotation in sim-time, and atmospheric drag. The Sol system is
playable; Alpha Centauri / TRAPPIST-1 / Kepler-452 are viewable.
You spawn in a 500 km LEO around Earth and can plant Hohmann
transfers, manual maneuvers, plane changes, and dock with sister
craft. The renderer is per-pixel braille on lipgloss with
view-aware orthographic projection of textured body disks.

The headline aesthetic is "Apollo-era nominal trajectory" — the
default vessel is a Saturn V S-IVB stage with a J-2 engine and
~6.3 km/s of Δv, sized so a Luna round trip is comfortable and a
Mars Hohmann is *barely* reachable on a good launch window. The
craft fleet up through v0.9.0 is intentionally modest; staging
slices later in the v0.9 cycle will grow it.

## Where it came from

Initial sketches as a learn-Go-by-building project — orbit-
mechanics math is a tractable problem with bounded scope and a
clear test surface. The early cycles (v0.1–v0.4) built the
two-body integrator, Lambert solver, save/load, and SOI rebasing.
v0.5 grew the body hierarchy (moons, rings, multi-system catalog)
and visual polish. v0.6 added the planner UX (event-relative
nodes, predicted-orbit HUD, finite-burn iterator, mission
scaffold, click-only mouse, multiplayer design spike). v0.7
filled in modding (theme + system overlays), manual flight (WASDQE
attitude, throttle keys), inclination planner, and textured
Mars/Jupiter. v0.8 — the current cycle — was branded "multi-craft
polish" and grew well past the headline: RCS / monoprop, multi-
craft slate, craft types, docking, atmospheric drag, sim-time
rotation with view-aware projection, body-equatorial Keplerian
frame, adaptive warp clamps, finite-burn iterate-for-target.

The codebase still tracks Apollo-era reality more than KSP-style
fantasy — atmospheric scale heights, axial tilts, sidereal periods,
GMs are pulled from public catalogs; the mission profiles
(circularize at 1000 km LEO, Luna orbit insertion, Mars SOI
flyby) match real spacecraft work.

## Status legend

<!-- llm-parse: status_legend -->

| Symbol | Meaning |
|---|---|
| ✓ | **shipped** — code on main, binary release published |
| 🚧 | **in progress** — work started, not yet tagged |
| 📐 | **planning** — design doc in flight, no code |
| 🧊 | **backlog** — accepted concept, no design pass |
| ⚠ | **rolled back** — attempted, reverted; needs redesign before re-attempt |
| ⏸ | **deferred** — explicit skip; reopen later |

---

## Released versions

<!-- llm-parse: releases_index -->

| Version | Date | Status | Theme |
|---|---|---|---|
| [v0.9.5 (in flight)](#v095-in-flight) | 2026-05-07 | 🚧 | Ascent ergonomics — predictive ap/pe/Δv→circ in LAUNCH HUD, ORBIT READY callout, NavSurface auto-snap on launchpad spawn, single-key `C` plants circularize-at-apoapsis. Closes the v0.9.2 WIP friction without an autopilot. |
| [v0.9.3 (awaiting playtest)](#v093-awaiting-playtest) | 2026-05-06 | 🚧 | Rendezvous tooling — target-relative SAS modes (`BurnTarget*`), TCA / CA / DOCK READY in TARGET HUD, KSP-style NavMode cycle (`;`), `m`-form integration with `next closest approach` trigger event. |
| [v0.9.2 (WIP, superseded by v0.9.5)](#v092-work-in-progress) | 2026-05-05 | 🚧 | Ground-launch primitives — launchpad spawn, surface-frame SAS, pitch trim, LAUNCH HUD. Manual ascent unreliable; v0.9.5 ascent ergonomics adds the live guidance that closes the loop. |
| [v0.9.1](#v091) | 2026-05-05 | ✓ | KSP-style staging chain — Saturn-V 3-stage loadout, `space` decouples bottom stage |
| [v0.9.0](#v090) | 2026-05-05 | ✓ | unified `World.Target` slot — first slice of "the craft fleet grows up" cycle |
| [v0.8.6](#v086) | 2026-05-04 | ✓ | controls polish + body-equatorial frame + adaptive warp clamps + iterate-for-target |
| [v0.8.5](#v085) | 2026-05-03 | ✓ | sim-time planet rotation + view-aware projection + textured-bodies trickle |
| [v0.8.4](#v084) | 2026-05-03 | ✓ | atmospheric drag |
| [v0.8.3](#v083) | 2026-04-30 | ✓ | docking + undocking |
| [v0.8.2](#v082) | 2026-04-30 | ✓ | craft types + spawn form + capture preview |
| [v0.8.1](#v081) | 2026-04-29 | ✓ | multi-craft foundation |
| [v0.8.0](#v080) | 2026-04-29 | ✓ | RCS / monopropellant mode |
| [v0.7](#v07) | 2026-04 | ✓ | modding + manual flight + planner polish |
| [v0.6](#v06) | 2026-04 | ✓ | planner UX + missions + multiplayer design |
| [v0.5](#v05) | 2026-04 | ✓ | moons + visual enhancement |
| [v0.4](#v04) | 2026-04 | ✓ | save / load + mid-course corrections |
| [v0.3](#v03) | 2026-04 | ✓ | porkchop + Lambert + finite burns |
| [v0.2](#v02) | 2026-04 | ✓ | finite burns + maneuver planner |
| [v0.1](#v01) | 2026-04 | ✓ | two-body propagator + SOI |

### v0.9.5 (in flight)
<!-- llm-parse: version=v0.9.5 status=in-progress date=2026-05-07 theme=ascent-ergonomics branch=claude/improve-launch-rendezvous-BJj0Y -->

**Ascent ergonomics — closes the v0.9.2 ground-launch loop.** The
v0.9.2 retrospective flagged "manual ascent to LEO unreliable" as
the gating friction. v0.9.5 transplants the v0.9.3 rendezvous
design language onto launch: live predictive numbers in the LAUNCH
HUD that the player can fly by (TCA/CA → ap/Δv→circ), a
threshold-callout (DOCK READY → ORBIT READY), and frame-aware
default routing (NavTarget auto-snap → NavSurface auto-snap on
launchpad spawn). No autopilot, no pitch table — KSP-style: tip the
rocket 10° east, hold surface-prograde, watch ap climb, plant the
circularisation node.

**Shipped on `claude/improve-launch-rendezvous-BJj0Y`.**

- **Live ascent prediction** in LAUNCH HUD
  (`internal/tui/screens/orbit.go:1158-1268`): `ap` (with
  `(climbing) / (falling) / (steady)` trend tag,
  finite-differenced from last frame), `pe`, `t_to_apo`,
  `Δv→circ`. Mirrors v0.9.3's TARGET HUD signed closing-rate
  pattern. Cached per-craft on `OrbitView.ascentTrendCraft` so
  cycling crafts re-baselines cleanly.
- **ORBIT READY callout** (`internal/tui/screens/orbit.go:1255-1267`):
  fires when apoapsis crosses the 200 km mission floor — the
  actionable threshold ("coast & plant `C`"), not the mission-pass
  threshold (which is per-frame transient). Renders in the same
  bold green (`#3DDC84`) as v0.9.3's DOCK READY for visual
  symmetry.
- **NavSurface auto-snap on launchpad spawn** (`internal/sim/spawn.go:213-229`):
  mirrors v0.9.3's `reconcileNavMode` pattern. Idempotent on
  NavSurface; only lifts NavOrbit. Lowercase `w` now means
  surface-prograde out of the box on launch.
- **`C` plants circularize-at-apoapsis** (`internal/sim/maneuver.go:790-867`,
  `internal/tui/input.go:166`, `internal/tui/app.go:528-547`):
  `World.PlanCircularizeAtApoapsis` computes the impulsive Δv from
  vis-viva (`sqrt(mu/r_apo) - sqrt(mu·(2/r_apo − 1/a))`) and plants
  a `BurnPrograde / TriggerNextApo` node. Errors when apoapsis is
  below the atmosphere cutoff (with a flash explaining the gate).
- **Mission progress in LAUNCH HUD** (`internal/tui/screens/orbit.go:1791-1816`):
  surfaces `pe X km / 200 km target` whenever a circularize_from_pad
  mission is in flight, so the player has one number to chase.

**LOC.** ~470 production + ~280 tests. Targets / sub-targets land
within the 2× HUD-snowball heuristic envelope (~500 plan / ~750
worst-case).

**v0.9.2 retrospective resolution.** The v0.9.2 unmerged-on-branch
WIP status is closed by this slice — the friction the v0.9.2
retrospective flagged ("manual ascent to LEO unreliable") was
guidance, not primitives. v0.9.5's live-guidance HUD makes the same
v0.9.2 primitives playable. Open question #7 (launch gravity-turn
assist) is resolved in favour of option (a) (live HUD overlay) over
option (b) (autopilot).

### v0.9.3 (awaiting playtest)
<!-- llm-parse: version=v0.9.3 status=in-progress date=2026-05-06 theme=rendezvous branch=v0.9.3-rendezvous -->

Rendezvous tooling (manual-first) shipped on
`origin/v0.9.3-rendezvous`. All four target-relative SAS modes
(`BurnTargetPrograde` / `BurnTargetRetrograde` / `BurnTarget` /
`BurnAntiTarget`); `planner.NextClosestApproach` with live TCA / CA
/ DOCK READY readouts in TARGET HUD; KSP-style NavMode cycle (`;`)
that reroutes the same six SAS axis keys per frame
(Orbit/Surface/Target); `m`-form integration with the
`next closest approach` trigger event + `ManeuverNode.TargetCraftIdx`
captured-at-plant + save round-trip. **Folded into the v0.9.5
working branch** so ascent ergonomics can build on the NavMode
auto-snap pattern.

### v0.9.2 (work-in-progress, superseded by v0.9.5)
<!-- llm-parse: version=v0.9.2 status=in-progress date=2026-05-05 theme=ground-launch branch=v0.9.2-ground-launch pr=51 -->

**Ground-launch primitives — feature-complete on branch, manual
ascent to LEO unreliable, ships unmerged.** Third slice of the v0.9
"craft fleet grows up" cycle. Adds the ability to spawn a craft on
the surface of a rotating Earth and fly it to orbit by hand, using
the v0.9.1 staging chain to drop spent stages along the way.

The primitives all work; the **flying experience does not** yet read
as ready-for-primetime. A representative attempt with the suggested
gravity-turn profile (vertical to ~3-5 km, trim east 20-30°, switch
to surface-prograde once v_horiz > 500 m/s, stage on fuel exhaustion)
regularly drains S-IVB with periapsis still negative. The slice is
preserved on branch / PR #51 as the canonical reference; gravity-turn
assist (target pitch-vs-altitude overlay or autopilot toggle) is
promoted to a v0.9.5+ slice candidate.

**Primitives shipped on branch.**

- **Launchpad spawn** (`internal/sim/spawn.go`): `SpawnSpec.Launchpad
  bool` + `SpawnSpec.Latitude float64`. When `Launchpad=true`, craft
  spawns at altitude 0 on the surface at the named latitude (presets:
  0° equator, 28.6° KSC = Cape Canaveral, 45.6° Baikonur, 62.8°
  Plesetsk, 90° pole), with surface-co-rotation velocity (ω × r) and
  `Landed=true` so the integrator bypasses Verlet free-flight while
  the craft is on the pad.
- **Body-fixed↔world coordinate transforms** (`internal/render/rotation.go`):
  `BodyFixedToWorld(b, latDeg, lonDeg, simTime)` Snyder forward
  projection through the renderer's tilted-axis sub-observer point;
  `BodySpinOmegaWorld(b)` returns the spin-axis-aligned angular
  velocity vector. Fixes a class of bugs where launchpad spawn
  visualised in the wrong location because spawn geometry and
  texture geometry diverged.
- **Landed-state integration** (`internal/sim/landed.go`):
  `integrateLanded` updates position to track surface rotation and
  re-derives velocity from ω × r each tick. Prevents warping a pad-
  bound craft from launching it into a free-flight trajectory.
- **Surface-frame SAS modes** (`internal/spacecraft/burn_direction.go`):
  `BurnSurfacePrograde` / `BurnSurfaceRetrograde` resolve direction
  from the surface-relative velocity (v - ω × r). Pre-launch (v_surf=0)
  the modes return zero — caller treats as "no defined direction"
  no-op, the burn is a no-op until the craft is moving relative to
  the ground. Bound to `W` / `S` (capitalised).
- **Pitch trim** (`internal/spacecraft/burn_direction.go`):
  `Spacecraft.PitchTrim float64` — a player-set ± rotation about the
  local-north axis applied on top of the SAS mode's natural direction.
  `>` / `<` step ±10° east / west; `\` resets. `ApplyPitchTrim`
  rotates dir using a (east, up, north) local frame decomposition.
  v0.9.2.1+: step bumped from 5° → 10° because the original required
  6+ presses to get the gravity turn going on a Saturn V.
- **LAUNCH HUD block** (`internal/tui/screens/orbit.go`): visible
  while the craft has not achieved a stable orbit (periapsis < primary
  radius) AND altitude < atmosphere cutoff. Shows altitude AGL, v_vert,
  v_horiz (surface-relative), TWR (active stage thrust / current mass
  / surface gravity), current SAS mode, current pitch trim.
- **Per-stage `BallisticCoefficient`** (`internal/spacecraft/stage.go`,
  `loadouts.go`): real Saturn V cross-sections × C_D / wet mass — S-IC
  ≈ 8e-6, S-II ≈ 2.5e-5, S-IVB ≈ 6.25e-5 m²/kg. Pre-v0.9.2.1 the
  default 0.01 was 1250× too high, making sea-level drag dominate
  the launch. `Spacecraft.EffectiveBallisticCoefficient` prefers the
  bottom stage's BC, falling back to the spacecraft-level default.
- **Landed crafts default to BurnRadialOut** (`internal/sim/spawn.go`):
  so `b` ignites pointing up instead of along surface co-rotation
  velocity. Prevents the surprise of a vertical-pad craft trying to
  burn east at TWR < 1.
- **`saturn-v-pad-to-leo` mission** (`internal/missions/missions.json`):
  new `TypeCircularizeFromPad` predicate, passes when craft is in
  Earth's frame, bound orbit (e<1), periapsis above the floor (200 km).
  Looser than `TypeCircularize` (no apoapsis tolerance) so the success
  condition is reachable from a manual ascent.

**v0.9.0/v0.9.1-flow continuity.** v0.9.0's `World.Target` slot and
v0.9.1's `space`-staging keystroke + Saturn V loadout both remain
fully functional. The launchpad branch is additive — pre-v0.9.2 spawn
flows (orbit, alongside) are unchanged.

**What's NOT in the slice (deferred / open).**

- **Gravity-turn assist** — the open question that the slice's
  manual-only decision deferred is now confirmed friction.
  Promoted to a v0.9.5+ slice candidate. Two options: (a) target
  pitch-vs-altitude HUD overlay (lightweight, leaves flying
  manual), or (b) autopilot toggle that drives throttle + attitude
  along a baked Saturn V profile.
- **Pitch trim fine resolution** — 10° is reasonable for initial
  pitch-over but mid-ascent fine-tuning at 1° resolution would help.
  Open question: should `>` / `<` repeat-step or take a Δ argument?
- **Cross-view rotation parity in orbit-flat** — the current fix
  makes the Landed craft co-rotate with surface texture in the
  default top view, but orbit-flat falls back to a static basis.
  Texture pipeline parity across views deferred to v0.9.5+.

**Sizing.** Plan called for ~400 LOC + 2× heuristic = ~800. Landed
at ~600 production + ~250 tests = ~850 total across the v0.9.2
branch (close to plan, with the unplanned add-ons — surface-frame
SAS, pitch trim, per-stage BC, body-fixed↔world transforms,
Landed integration — accounting for ~250 LOC of the production
total).

**Status decision.** Slice ships **unmerged on the
`v0.9.2-ground-launch` branch / PR #51** until either the gravity-
turn assist lands or the user accepts the WIP state with eyes open.
Cycle order does not change — v0.9.3 (rendezvous) and v0.9.4
(navball) operate on already-orbiting craft and are unblocked by
this WIP status. The v0.9.2 primitives are foundation that the
gravity-turn assist will layer on top of, not throwaway.

### v0.9.1
<!-- llm-parse: version=v0.9.1 status=shipped date=2026-05-05 theme=staging-chain -->

**KSP-style player-managed staging chain.** Second slice of the
v0.9 "craft fleet grows up" cycle. Adds multi-stage launch
vehicles + the `space` decouple keystroke + the Saturn-V loadout.

- **Stage source-of-truth** (`internal/spacecraft/stage.go`): new
  `Stage` struct (DryMass / FuelMass / FuelCapacity / Thrust /
  Isp / MonopropMass / MonopropCap / RCSThrust / RCSIsp /
  LoadoutID / Name / Glyph / Color). `Spacecraft.Stages []Stage`
  is authoritative; the historical flat fields become derived
  shadow-mirror values refreshed by `SyncFields`. Convention:
  `Stages[0]` = bottom (currently-firing engine, first to be
  jettisoned); `Stages[len-1]` = top (core payload).
- **BurnFuel / BurnMonoprop helpers**: write sites route through
  `c.BurnFuel(amount)` / `c.BurnMonoprop(amount)` which mutate
  `Stages[0]` and `SyncFields` together. Pre-v0.9.1 wrote to flat
  fields directly; with Stages now authoritative, those would
  leak.
- **Saturn-V 3-stage loadout** (`internal/spacecraft/loadouts.go`):
  S-IC booster (35,100 kN @ 263s, sea-level Isp; TWR > 1 sea
  level), S-II sustainer (5,140 kN @ 421s vacuum), S-IVB
  insertion (1,023 kN @ 421s — same shape as the standalone
  S-IVB-1). Existing 4 loadouts wrap into single-stage
  `Stages: [{...}]`.
- **`World.StageActive`** (`internal/sim/staging.go`): pops
  `Stages[0]`, spawns it as a passive Spacecraft at the active
  craft's exact inertial state (residual fuel + monoprop on the
  jettison), active idx stays on the upper chain. Errors:
  `ErrStageOnlyOne` (refuses to drop the only stage),
  `ErrStageNoCraft`, `ErrStageEmpty`.
- **`space` keystroke** (`internal/tui/input.go`): retired from
  Pause (now `0` only) and routed to `StageActive`. The maneuver
  form's iterate-toggle (v0.8.6.3) still owns `space` because its
  key path intercepts before `app.go`.
- **STAGES HUD block** (`internal/tui/screens/orbit.go`):
  per-stage thrust / Isp / fuel% with bottom highlighted in
  Warning. Hidden for single-stage craft (existing PROPELLANT
  block already covers them).
- **Composite-craft post-docking**
  (`internal/sim/docking.go`): composite Stages =
  `lead.Stages ++ partner.Stages` (appended on top — undocking
  peels the partner off as a unit). New `CompositeEngineSummary`
  helper exposes the pooled view (sum thrust, mass-weighted Isp)
  per scoping #4 for downstream consumers.
- **Save schema v5 → v6** (`internal/save/save.go` +
  `save_migrate_v5_to_v6.go`): `Craft.Stages` added (omitempty);
  flat fields stay on the wire for back-compat. Pre-v6 saves
  migrate by wrapping the v5 flat fields into a single-element
  Stages slice; FuelCapacity defaults to live Fuel (v5 had no
  pristine-capacity record).

**Plan deviations.**

- The plan's literal text said "computed accessors that delegate
  to the top stage" (methods). Implemented as **derived shadow-
  mirror with SyncFields** instead, because converting ~30 read
  sites to method calls had ~3× the diff with no functional gain.
  The Stages-as-truth invariant is preserved.
- Plan said "active engine reads from `Stages[len-1]` (top
  stage)" but every other detail (Saturn-V "TWR>1 at sea level on
  stage 1"; `StageActive` "pops `Stages[0]`") makes it clear
  bottom = firing. Implemented as `Stages[0]` = bottom = firing.
  The plan text was a typo; the Saturn-V tuning is the truth.

**Sizing.** Plan called for ~700 LOC + tests + corpus. Landed at
~500 production + ~300 tests = ~800 total. Close to estimate;
matches the v0.8 retrospective heuristic that isolated planner /
sim-internals slices (no rendering or frame-convention churn)
land near plan.

### v0.9.0
<!-- llm-parse: version=v0.9.0 status=shipped date=2026-05-05 theme=targeting-slot -->

**Unified `World.Target` slot.** Foundation slice of the v0.9 "craft
fleet grows up" cycle. Replaces the implicit body cursor that
planted-Hohmann (`H`) and plane-match (`I`) read pre-v0.9 with a
single explicit slot every planner consumes.

- **Target shape** (`internal/sim/target.go`): `TargetKind` enum
  (`TargetNone` / `TargetBody` / `TargetCraft` / `TargetSite`
  reserved) + `Target` struct (`Kind`, `BodyIdx`, `CraftIdx`).
  Mirrors the existing `Focus` pattern from
  `internal/sim/focus.go`.
- **World API** (`internal/sim/world.go`, `target.go`):
  `World.Target` field; `SetTargetBody` / `SetTargetCraft` /
  `ClearTarget` / `CycleTarget` (forward / backward) helpers.
  Cycle order: bodies in active system (idx 1 .. n-1) →
  non-active sibling craft → none → repeat. Out-of-range or
  self-targeting clears.
- **Resolver**: `World.TargetState() (orbital.Vec3State, ok bool)`
  — returns the target's heliocentric (or primary-frame, when
  active craft is body-bound) state for downstream consumers.
- **Planner consumers**: `H` planted Hohmann and `I` plane-match
  now read `World.Target` instead of the cursor. `TargetCraft` on
  `H` or `I` flashes "needs v0.9.3" — exit door wired for the
  rendezvous slice.
- **Cursor retained**: `App.selectedBody` still drives body-info
  (`i`), porkchop (`P`), and the SELECTED HUD block. Targeting
  is the planner-input concept; the cursor stays UX scaffolding
  for read-only screens.
- **Keys** (`internal/tui/input.go`): `t` cycles target, `T`
  clears.
- **TARGET HUD block** (`internal/tui/screens/orbit.go`): hidden
  when `Target.Kind == TargetNone`. For `TargetBody`: name,
  body-equatorial Δi vs active craft, closest-encounter range.
  For `TargetCraft`: name + role, current range, |v_rel|.
  Extension to closest-approach time + distance is a v0.9.3 hook.
- **Save** (`internal/save/save.go`): `*Target` pointer added to
  v5 payload with `omitempty` — zero-value (`TargetNone`) means
  no JSON field, no schema bump from v5. Pre-v0.9.0 v5 saves
  load with `World.Target = Target{}`.

**Sizing.** Plan called for ~250 LOC + 2× heuristic (planner /
HUD touches) → ~500. Landed at ~280 production + ~200 tests = 8
files, +662/-22 LOC. Under estimate — no rendering snowball this
slice. Confirms the v0.8 retrospective pattern: isolated
planner / sim-internals slices stay close to estimate; the 2–3×
heuristic applies to rendering / frame-convention / planner-UX
slices specifically.

### v0.8.6
<!-- llm-parse: version=v0.8.6 status=shipped date=2026-05-04 theme=controls-polish-and-frames -->

**Controls polish bag** that grew unplanned add-ons.

- **(a) Keymap pass.** `S`/`L` save/load → `F5`/`F9` (KSP-style); drop the global `N` ClearNodes binding (case-collided with `n` SpawnCraft); per-node `ctrl+d` delete + `ctrl+k` clear-all in the maneuver form. New `World.DeleteNode` sim API.
- **(b) IterateForTarget toggle in `m` form.** 5th cycled field after throttle. `space` or `←/→` toggles. When on, app routes commanded Δv through `World.IterateBurnDV(mode, dv)` before plant — `planner.IterateForTarget` Newton-iterates against an RK4 finite-burn simulation so post-burn apsides match the impulsive target. Skipped for Normal±. *(Shipped in patch v0.8.6.3.)*
- **(d) Adaptive warp clamps.** Three new caps layered on top of the pre-existing burn-active 10× cap:
  - **Throttle-change cap** — 10× for 1 sim-second after `Throttle` changes.
  - **Upcoming-node approach cap** — continuous predictive ramp-down: `maxWarp = secondsUntilNode / (10 × BaseStep)`, floored at 1×. Prevents 100,000× warp from skipping a 30-s-out node in one tick. *(Shipped in patch v0.8.6.2.)*
- **Body-equatorial Keplerian frame** *(unplanned add-on, shipped in patch v0.8.6.1)*. Inclination, Ω, ω for body-bound orbits read in the primary's equatorial frame (ECI for Earth, MCI for Mars, etc.) — the operational mission-planning convention. A 0° Earth orbit physically passes over the equator (Ecuador), not over the world ecliptic plane that intersected Earth at ~23°N because of the 23.44° axial tilt. `orbital.BodyFrame` + `ReferenceFrameForPrimary` (identity for Sun, body-equatorial for everything else). `PlaneMatchInclination` converts a target's heliocentric plane into the primary's frame. Heliocentric orbits stay ecliptic-relative (standard astronomical convention).
- **Orbit-flat low-warp jitter fix** *(part of v0.8.6.1)*. ω snapped to 0 for circular orbits (`eMag < 1e-6`) so `PerifocalBasis` stays stable per frame. Defensive pole-on guard added in `SubObserverPointDeg`.
- **CI: four-part patch tags excluded from goreleaser** — `vX.Y.Z.N` checkpoint markers don't fail the workflow.
- **(c) backlogged** — multi-rev porkchop UI keys deferred until staging slices grow craft fleet enough that multi-rev / retrograde transfers are practically valuable.

### v0.8.5
<!-- llm-parse: version=v0.8.5 status=shipped theme=sim-time-rotation-textures -->

**Sim-time planet rotation + tidal-lock perspectives + textured-bodies trickle + view-aware projection.**

- Rotation core: `bodies.CelestialBody.TidallyLocked` + `AxialTilt` + `AxialAzimuth` fields. `render.SubObserverPointDeg(b, simTime, camDir, primMer)` returns (subLat, subLon) at the visible disk centre. Tidally-locked moons track parent's direction (Luna's near-side faces Earth always); free bodies use sidereal rotation. `Clock.RotationTime` advances at `min(warp, 10000×) × BaseStep` so high-warp doesn't blur surfaces.
- View-aware projection (Snyder §20 inverse-orthographic with arbitrary sub-observer point). ViewTop on tilted Earth reveals the Arctic; Uranus rolls pole-on along its orbit; Saturn's polar hex stays at +78°N regardless of view; ViewOrbitFlat picks up the canvas's depth axis.
- Polygon-rasterised 144×72 Earth grid (~50 polys × 10–20 verts: continents + key islands incl. UK/Iceland/Italy/Madagascar/Cuba/Hispaniola/Sumatra/Java/Borneo/Sulawesi/New Guinea/Philippines/Tasmania/NZ + deserts + polar ice). Biome-shaded land (tropical / temperate / boreal) by `|lat|`; atmospheric blue-marble limb tint at r²>0.92 over non-ice. Replaces the v0.7.6 ellipse-table.
- Far-side / polar Moon detail (Mare Orientale, Moscoviense, Ingenii, South Pole-Aitken basin, far-side / polar craters).
- Tilted Saturn ring system: C / B / Cassini Division gap / A / F bands sampled in body equatorial plane and projected through `Canvas.RingTiltedOutline` so foreshortening reads correctly per view (~89% top, ~45% side).
- Textured Sun (limb-darkened solar disk + sunspots + corona halo replaces the v0.7.x crosshair); Galileans (Io paterae, Europa lineae, Ganymede dark regiones, Callisto crater rays); Uranus (subtle banding); Neptune (banded + Great Dark Spot).
- Terminal moons (no children) zoom to 8× radius on focus so surface texture is visible by default.
- Save schema: TidallyLocked + AxialTilt + AxialAzimuth bump CatalogHash; v0.8.4 saves reject on first v0.8.5 load.

### v0.8.4
<!-- llm-parse: version=v0.8.4 status=shipped theme=atmospheric-drag -->

**Atmospheric drag.**

- `bodies.Atmosphere` data model + Earth/Mars values: exponential ρ(h) with 8500 m / 11100 m scale heights, 100 km / 80 km cutoffs.
- Drag-aware Verlet (`physics.StepVerletWithAccel`) wired into live integrator + `propagateStateWithPrimary` + `PredictedSegmentsFrom` + `stepThrust`. Co-rotating air via `v_rel = v − ω × r`.
- Kepler-warp-lock retreat below atmospheric cutoff (analytic propagation breaks under drag).
- `Spacecraft.BallisticCoefficient` (default 0.01 m²/kg).
- `physics.ClampToSurface` on aerobrake impact.
- Time-aware `propagateStateWithPrimary` (foundation work) unlocks exact CAPTURE PREVIEW inclination for typical Hohmanns.
- Visual: faint haze ring at `cutoff + scale-height` in `atm.Color`. Body pixel cap raised to 512; body disk grows to canvas reach so altitude-0 reads as surface. Zoom cap for landed craft.

### v0.8.3
<!-- llm-parse: version=v0.8.3 status=shipped theme=docking -->

**Docking + undocking.**

- Proximity-gated `DockCrafts` at <50 m and <0.1 m/s relative velocity. Mass-weighted centroid + momentum-conserving fuse. Summed propellant pools.
- `DockedComponents` preserves original-craft identities through fusion so `U` undock can split back along original boundaries with proportional propellant.
- RENDEZVOUS HUD (live range / |v_rel| / DOCK READY indicator).
- Spawn form `POSITION = alongside active` for inside-gate testing.
- Engine-firing flame visual + per-thruster RCS puff visuals replace the v0.8.0 placeholder.

Real rendezvous tooling (target-craft Hohmann + phasing) flagged for v0.9 [(see backlog)](#rendezvous-tooling).

### v0.8.2
<!-- llm-parse: version=v0.8.2 status=shipped theme=craft-types -->

**Craft types + spawn form + capture preview.**

- Four loadouts with distinct propulsion + glyph + colour: S-IVB-1 ▲ yellow (J-2 1023 kN, Isp 421 s), ICPS ◆ blue (RL10C 108 kN, Isp 462 s), RCS-tug ● pink (monoprop-only), Lander ▼ mint (throttleable descent).
- Full `n` spawn form: loadout / parent body / altitude / direction.
- Clickable HUD NODES rows (open the planner pre-loaded for that node).
- CAPTURE PREVIEW HUD: predicted approach speed + qualitative direction (prograde / retrograde) for Hohmann arrivals.
- Equatorial inclination match (`I` with no body cursor → 0°).
- Per-craft canvas glyphs + colours so multi-craft slates read at a glance.

### v0.8.1
<!-- llm-parse: version=v0.8.1 status=shipped theme=multi-craft -->

**Multi-craft foundation.**

- `World.Crafts []*Spacecraft` + `ActiveCraftIdx`; `[`/`]` cycles active craft; `n` spawns sister craft 90° around primary.
- `ManeuverNode` / `ActiveBurn` / `ManualBurn` / `TriggerEvent` / `AttitudeMode` / `EngineMode` lifted from `internal/sim` to `internal/spacecraft` (sim re-exports as type aliases) so each craft owns its own state without an import cycle.
- Save schema v4 → v5 with `Craft *Craft` → `Crafts []*Craft` migration; pre-v5 saves auto-migrate.
- HUD's NODES + BURNS blocks list all-craft state simultaneously.

### v0.8.0
<!-- llm-parse: version=v0.8.0 status=shipped theme=rcs-monoprop -->

**RCS / monopropellant precision-thruster mode.**

- `Spacecraft.MonopropMass` + `MonopropFuel` + `RCSThrust` + `RCSIsp` (typically ~720 kg / ~50 N / ~220 s for the S-IVB-1 base).
- `EngineMode` toggle (`r` key) routing `b` / attitude keys through a 0.1 m/s pulse pool (~30 m/s on default S-IVB-1).
- Per-thruster RCS-puff visual placeholder (replaced by per-thruster glyph trail in v0.8.3).

### v0.7
<!-- llm-parse: version=v0.7 status=shipped theme=modding-manual-flight-polish rolled_up=true -->

Rolled up: **modding chain (v0.7.0–v0.7.2.3) + manual flight (v0.7.3) + Esc-on-home menu polish (v0.7.3.1–.3) + inclination-change planner (v0.7.4) + HUD compaction + retrograde-flag Lambert (v0.7.5) + textured Mars/Jupiter / per-node throttle / SOI HUD (v0.7.6).**

Highlights:

- **Modding** (v0.7.0–.2): system overlay (`$XDG_CONFIG_HOME/.../systems/*.json`), per-body palette, `theme.json` UI + body colour overrides.
- **Manual flight** (v0.7.3): `Spacecraft.Throttle` + `World.ManualBurn` + `World.AttitudeMode`, WASDQE attitude keys, `z`/`x` throttle, warp clamp ≤10× during burns.
- **Inclination planner** (v0.7.4): `planner.PlanInclinationChange` plants single normal±-burn at next AN/DN; `I` keybinding; PROJECTED ORBIT inclination line; HUD compaction (VESSEL+PROPELLANT and SYSTEM+SELECTED side-by-side); `[Missions]` + `[Menu]` title-bar buttons; Hohmann-preview frame fix for moon targets.
- **Retrograde Lambert** (v0.7.5): `LambertSolveRev(..., nRev, retrograde)` plumbed through `LambertSolve` / `PlanLambertTransfer` / `PorkchopGrid`. Library-only; UI toggle deferred.
- **Textured Mars + Jupiter** (v0.7.6): Syrtis Major / Solis Lacus / polar caps; 10-band Jovian zones/belts + GRS. Per-node throttle (schema v3 → v4). FRAME TRANSITION HUD section.

### v0.6
<!-- llm-parse: version=v0.6 status=shipped theme=planner-ux-missions-mp-design rolled_up=true -->

**Planner UX + missions + multiplayer design.**

- **Burn-at-next scheduler** (v0.6.0): event-relative trigger nodes (`next peri / next apo / next AN / next DN`); lazy-freeze resolver in `World.Tick`.
- **Predicted-orbit HUD** (v0.6.1): apo / peri / AN / DN of chained post-burn orbit, frame-rebased per node's PrimaryID. Per-leg colored trajectory preview (4-cycle palette). Vessel chevron + apo/peri markers.
- **Finite-burn iterator** (v0.6.2): `planner.IterateForTarget` Newton-iterates commanded Δv against RK4 finite-burn simulation. Used internally by Hohmann auto-plant.
- **Moon → parent escape transfer** (v0.6.3): `planner.PlanMoonEscape` (bound transfer ellipse with apolune at moon's SOI radius, zero-Δv frame marker at SOI exit).
- **Click-only mouse + 5-way views** (v0.6.4): mouse hit-test for body / vessel / nodes / canvas / HUD; ViewTop / Right / Bottom / Left / OrbitFlat.
- **Mission scaffold** (v0.6.5): `internal/missions` package with three predicate kinds (`circularize` / `orbit_insertion` / `soi_flyby`), embedded starter catalog, schema v2 → v3.
- **Multiplayer design spike** (v0.6.6): `docs/multiplayer-design.md` (transport, authority, persistence, open questions). Pure prose; no code change.

### v0.5
<!-- llm-parse: version=v0.5 status=shipped theme=moons-visual rolled_up=true -->

**Moons + visual enhancement.**

- **Body hierarchy** (v0.5.0): `bodies.Body.ParentID` for arbitrary-depth refs; recursive `BodyPosition` / `bodyInertialVelocity`; `physics.FindPrimary` walks the tree for SOI sizing. Moon catalog: Luna, Phobos, Deimos, Galileans, Titan, Enceladus.
- **Intra-primary Hohmann** (v0.5.7) + **phase correction** (v0.5.9): `planner.PlanIntraPrimaryHohmann` for same-parent transfers (LEO → Luna).
- **S-IVB-1 default** (v0.5.10): J-2 stage replaces RL10C-class; ~110 s TLI burn at default thrust drops gravity-rotation loss to <0.1 %.
- **Visual polish** (v0.5.11–.15): tilted Saturn rings (face-on), per-body glyphs, HUD dividers, porkchop labels, ring sample cap.

### v0.4
<!-- llm-parse: version=v0.4 status=shipped theme=save-load rolled_up=true -->

**Save / load + mid-course corrections.**

- **Save / load** (v0.4.0): JSON state file at `$XDG_STATE_HOME/.../save.json`, `body_catalog_hash` header for save-compatibility checks, `S` / `L` keys (replaced by F5/F9 in v0.8.6), autosave on quit.
- **Mid-course refine** (v0.4.1): `R` re-Lamberts from live state to pending arrival; plants prograde / retrograde correction.
- **SOI-fix + warp-lock + SOI subdivide** (v0.4.2–.4): nested SOI walk, analytic Kepler fast-path during high warp, chunk subdivision so high-warp orbits don't skip foreign SOIs.

### v0.3
<!-- llm-parse: version=v0.3 status=shipped theme=lambert-porkchop rolled_up=true -->

**Porkchop + Lambert + finite burns.**

Lambert solver (Stumpff universal variables, Curtis Algorithm 5.2). Auto-plant Hohmann + porkchop heatmap (ASCII intensity ramp). Finite-burn integration. Adaptive body sizing with tier fallback.

### v0.2
<!-- llm-parse: version=v0.2 status=shipped theme=maneuver-planner rolled_up=true -->

**Finite burns + maneuver planner.**

`m` planner form (mode / fire-at / Δv); RK4 on burn, Verlet on free flight; rocket-equation duration. Six burn modes (prograde / retrograde / normal± / radial±). Quick-plant `n`.

### v0.1
<!-- llm-parse: version=v0.1 status=shipped theme=foundations rolled_up=true -->

**Two-body propagator + SOI.**

Patched-conic two-body propagation; SOI-aware state transitions; symplectic Verlet integrator; warp clamp to 1024 sub-steps × period/100. Sol system loaded; LEO craft spawn; orbit canvas with bodies / vessel / focus cycling; basic HUD.

---

## Backlog

<!-- llm-parse: backlog_root -->

Concepts accepted but not yet sliced. Each item carries a status
tag plus the major slice / cycle it would naturally pair with.
Items here are **not under active development** unless noted.

### Rendezvous tooling
<!-- llm-parse: id=rendezvous status=backlog target=v0.9 -->
🧊 **backlog · target v0.9**. Real rendezvous flow that v0.8.3 docking left as a "spawn alongside / thumb-fly" cheat. Five sequenced steps: target-craft selection → target-relative prograde / retrograde burn modes → prograde-to-target nudge from a phasing orbit → null v_rel at predicted closest approach → iterate until in DOCK READY range. Foundations needed: `World.TargetCraftIdx`, `BurnTargetPrograde` / `BurnTargetRetrograde` modes (h-direction-from-relative-velocity), `planner.NextClosestApproach`, `World.PlanRendezvous` auto-plant, TARGET HUD block. Pair with staging slices that grow the craft fleet.

### Mission scripting / editor
<!-- llm-parse: id=mission-scripting status=rolled-back target=v0.9 attempted=v0.8.7 -->
⚠ **rolled back — needs design pass before re-attempt**. A draft Option-B implementation landed (commit `4159a31`) and was reverted (`e806dd3`) because eight design decision points (engine pick, modder UX flow, error feedback, schema versioning, cross-craft predicate scope, mass/propellant fields, ceiling-vs-floor expectation, editor surface, sandboxing) collapsed into "expr-lang/expr is lighter, ship it" without their own pass.

The reverted artifacts are git history, not a starting point. **Do not re-implement without the design pass.** Full retrospective + decision-point list in [`state-of-game-archive.md` §6 *Mission scripting / editor*](state-of-game-archive.md). Suggested sequencing: (1) write the modder-UX target end-to-end, (2) pick the engine in service of that UX, (3) reference v0.8.7-attempt artifacts only for implementation shape, (4) implement.

### Multi-rev porkchop UI
<!-- llm-parse: id=multi-rev-porkchop status=deferred target=v0.9 -->
⏸ **deferred from v0.8.6 (c) → v0.9**. `LambertSolveRev` + retrograde flag have been library-ready since v0.7.5; UI not sliced. Defer until staging slices grow craft fleet — current chemical S-IVB-1-class fleet always picks nRev=0 prograde, so UI gives no leverage until that changes. Pairs with the Lambert short/long branch picker [(below)](#lambert-shortlong-branch-picker).

### Lambert short/long branch picker
<!-- llm-parse: id=lambert-short-long status=backlog target=v0.9-with-multi-rev -->
🧊 **backlog · pairs with multi-rev porkchop**. Today `LambertSolveRev` returns the first root the bracket finds (lower-z side); a per-N "short" / "long" flag would expose both branches per rev count. Library-only LOC (~30) — the surface is plumbing through `PlanLambertTransfer` + `PorkchopGrid` + a UI control. Travels with multi-rev porkchop because both expose nRev≥1 branches that don't exist on the nRev=0 path.

### Wider cross-SOI PlanTransfer
<!-- llm-parse: id=cross-soi-transfer status=backlog target=v0.9 -->
🧊 **backlog · target v0.9**. v0.5.7's `PlanIntraPrimaryHohmann` covers same-parent (LEO → Luna); v0.6.3 covers moon → parent. The remaining direction — heliocentric → moon-of-other-planet (Phobos from a Mars approach, a Galilean from a Jupiter cruise) — needs a real patched-conic capture pass through both SOIs.

### Combined plane-shift + Hohmann
<!-- llm-parse: id=plane-shift-hohmann status=backlog target=v0.9 weight=L -->
🧊 **backlog · target v0.9 · substantial**. Lambert solver constrained on post-capture inclination so departure geometry lands prograde at the destination instead of the current "match ecliptic, hope arrival inclination is OK" pattern. The v0.8-plan.md retrospective explicitly flags this as **substantial** — the binding technical work is the constrained Lambert variant (root-find on inclination as well as time-of-flight), not the UI plumbing. Pairs naturally with the [capture-direction toggle](#capture-direction-toggle) since both touch arrival-side geometry.

### Capture-direction toggle
<!-- llm-parse: id=capture-direction-toggle status=backlog target=v0.9 -->
🧊 **backlog**. Today's auto-Hohmann arrival burn is retrograde-in-source-frame. A "capture prograde-around-target" mode would burn differently and trade ~50–100 m/s for the right-direction capture.

### Drag-to-edit nodes
<!-- llm-parse: id=drag-to-edit status=deferred -->
⏸ **deferred · playtest-triggered**. v0.6.4 deliberately picked click-to-edit-replace as the model; v0.8.6 retrospective held the line. Drag-to-scrub Δv / fire-time directly on a planted-node marker is the alternative model — KSP players reach for it on muscle memory. Reopen this slice when (and only when) playtest feedback reports click-to-edit-replace as actually friction; until then the simpler interaction wins.

### Predictor adaptive sampling
<!-- llm-parse: id=predictor-adaptive-sampling status=backlog carry-over=v0.5-v0.6-v0.7-v0.8 -->
🧊 **backlog · three-cycle carry-over**. Fixed 96-sample horizon collapses to a smear at 10000× warp on LEO orbits. Adaptive sampling (sample density ∝ orbit period / sim-time horizon) is the obvious fix. Flagged in `v0.5-release-notes.md` deferred list, escalated to `integration-design.md` §10 open question, re-flagged in `v0.7-plan.md` and `v0.8-plan.md` backlogs without shipping in any cycle. **Foundation shipped at v0.8.4** (time-aware `propagateStateWithPrimary` for drag-aware predictor coherence) — the integration is now possible without further infrastructure work, just not done. ~150–200 LOC.

### Solar lighting + day/night terminator + eclipses
<!-- llm-parse: id=lighting-terminator-eclipses status=backlog target=v0.9 -->
🧊 **backlog · target v0.9 (research-first)**. Sub-solar-point per body per tick → `cos(angle to sun)` shading; eclipses fall out for free if lighting lands. Research item: investigate canvas-level ANSI 24-bit per-cell mixing as a `lipgloss` workaround before slicing.

### Staging chain
<!-- llm-parse: id=staging-chain status=backlog target=v0.9 -->
🧊 **backlog · target v0.9**. Ground launch → orbit → ICPS / S-IVB / lander staging chain so the craft fleet has more than one tier of capability. Unblocks (c) multi-rev porkchop and the rendezvous tooling slice (more craft → more practical scenarios).

### Multiplayer implementation
<!-- llm-parse: id=multiplayer status=planning target=v0.9-stretch -->
📐 **planning** *(`docs/multiplayer-design.md` v0.6.6)*. WebSocket transport, host-authoritative authority, warp-arbitration rule. **Prerequisite (multi-craft foundation) was satisfied at v0.8.1 — the architectural blocker is gone.** Three open scoping questions carry forward from the v0.6.6 spike: (1) multi-craft selector ordering vs MP land sequencing; (2) warp arbitration rule generalisation to 3+ peers (current rule is host-veto, fine for 2 but ambiguous beyond); (3) per-player vs shared mission state (does each connected player see their own mission slate, or one shared catalog?). Not slated for v0.9 directly but the design doc is current and the foundations are in.

### N-body perturbations
<!-- llm-parse: id=n-body status=backlog target=v0.10+ -->
🧊 **backlog · target v0.10+**. Lagrange points, J2, third-body acceleration. Major architectural change — Kepler-warp-lock fast path retreats to RK4 + Verlet.

### Multi-system spacecraft
<!-- llm-parse: id=multi-system-craft status=backlog target=v0.10+ -->
🧊 **backlog · target v0.10+**. Two paths sketched: (A) real interstellar transfer math (~50,000 yr at chemical Δv, needs a propulsion abstraction), or (B) deus-ex jump drive. Today the craft is locked to Sol; `CycleSystem` only changes the camera.

### Theme-file hot-reload
<!-- llm-parse: id=theme-hot-reload status=deferred -->
⏸ **deferred**. ~200 LOC of fsnotify watching `theme.json` so palette tweaks land without restarting. `LoadTheme` is already idempotent (v0.7.2) so the runtime side is cheap; the cost is the watcher setup + a debounce. Never surfaced as a v0.8 playtest pain — reopen if a modder hits it iterating on a per-body palette.

### Numbered craft slots (1–9)
<!-- llm-parse: id=numbered-craft-slots status=backlog target=v0.9-when-fleet-grows -->
🧊 **backlog · gates on craft-fleet growth**. v0.8.1 ships `[`/`]` cycle + click-select on per-craft glyphs (v0.8.2). Numbered hotkeys (`1`..`9` jump to craft N) deferred until saves routinely have >4 craft and the cycle key gets unwieldy. Trivial keystroke + `World.SwitchToCraftIdx` wrapper; gating is UX, not code.

### `bodies.json` sibling overlay
<!-- llm-parse: id=body-overlay status=backlog -->
🧊 **backlog**. Per-body overrides without redefining the whole system. Pairs with mission-scripting design pass (both touch the catalog-loader pattern).

### `Rings` / `Glyph` JSON overrides
<!-- llm-parse: id=rings-glyph-json status=backlog -->
🧊 **backlog**. v0.7.1 put `Color` into `bodies.CelestialBody`; whether `Rings` and `Glyph` follow as JSON-driven fields is open.

### Race-detector CI
<!-- llm-parse: id=race-detector status=deferred -->
⏸ **deferred**. Currently no `-race` because the local toolchain doesn't ship cgo; CI could enable with `CGO_ENABLED=1`.

### High-fidelity Earth raster
<!-- llm-parse: id=earth-raster status=backlog -->
🧊 **backlog**. NOAA ETOPO1 land/sea mask embedded via `go:embed` would slot into the same `earthGrid` storage with a different generator. Today's polygon raster (~50 polys) is good enough at typical disk px-radii; this is post-v0.8.5 polish.

### Attitude-mode save persistence
<!-- llm-parse: id=attitude-save status=deferred -->
⏸ **deferred**. Decision held at "keep ephemeral" through v0.8 — planted nodes are the persistence layer. Reopen if mid-coast-load resetting attitude is annoying in playtest.

### Mass/propellant fields in mission EvalContext
<!-- llm-parse: id=mission-eval-resources status=backlog target=alongside-mission-scripting -->
🧊 **backlog**. `EvalContext` doesn't carry fuel / monoprop / dv_budget today, so the rolled-back v0.8.7 expression env had those zeroed. Trivial threading from `sim.World.Tick`; pairs with the mission-scripting design pass.

### Open scoping questions
<!-- llm-parse: backlog_section=open-questions -->

These are unresolved scoping questions that don't yet have an
implementation slice attached. Each gates a v0.9-or-later
decision. Carried forward from the v0.x-plan docs because they
never resolved; flagged here so tonight's planning session can
take a position on them.

#### Spawn-form persistence
<!-- llm-parse: id=spawn-form-persistence status=open-question target=v0.9-polish -->
📐 **open**. Should the `n`-keystroke spawn dialog remember the last-used craft type / fuel / orbit, or default-fresh every open? Today: default-fresh. Trivial to add a `World.LastSpawnSpec` field that prefills the form. No design discussion to-date.

#### Docking visual feedback
<!-- llm-parse: id=docking-visual-feedback status=open-question target=v0.9-polish -->
📐 **open**. Today's `DockCrafts` fuses two craft into one silently — the player learns it happened from the HUD's RENDEZVOUS block disappearing. Should there be a flash / glyph swap / sound (terminal beep) on fusion? Carries to undocking too. No design discussion.

#### Staging continuity
<!-- llm-parse: id=staging-continuity status=open-question target=gates-v0.9-staging -->
📐 **open · gates the v0.9 staging-chain slice**. When a stage is shed, does the player keep controlling the *upper* craft (KSP default — that's where the payload goes) or get prompted? KSP-style implicit-upper makes lander missions natural; explicit prompt is safer for surprise scenarios. Pre-v0.9 staging slice should pick a default and document it.

#### Composite-craft mass distribution post-docking
<!-- llm-parse: id=composite-mass-post-docking status=open-question target=gates-v0.9-staging -->
📐 **open · gates the v0.9 staging-chain slice**. Today's `DockCrafts` picks the active partner's engine for the composite. What happens when two main-engine craft dock — pool both engines (sum thrust, average Isp by mass)? Pick highest TWR? Player-select via prompt? Becomes especially relevant once staging chain creates multi-stack vehicles where the docked partner *is* the upper stage's engine.

#### Atmosphere co-rotation at high altitude
<!-- llm-parse: id=atmosphere-corotation-high-alt status=open-question target=v0.9-if-playtest-shows -->
📐 **open · low priority unless playtest exposes**. v0.8.4 has the atmosphere co-rotating with the body via `ω × r`. At high altitude (above ~100 km on Earth, where ground-level corotation breaks down in reality), the model is approximate. Reopen if it shows up in a playtest as a noticeable orbit decay error.

#### Launch gravity-turn assist
<!-- llm-parse: id=gravity-turn-assist status=resolved target=v0.9.5 reopened-from=v0.9-plan-decision-7 -->
✓ **resolved in v0.9.5** with neither (a) nor (b). The two options
on the table at v0.9.2 retrospective were (a) target pitch-vs-
altitude HUD overlay or (b) autopilot toggle. v0.9.5 transplanted
v0.9.3's rendezvous design language onto launch instead — live
predictive numbers (ap, pe, Δv→circ) + threshold callout (ORBIT
READY) + frame auto-routing (NavSurface auto-snap on launchpad
spawn) + single-key circularize (`C`). The KSP recipe (tip 10°,
hold surface-prograde, ride the gravity turn) was already
realisable with v0.9.2 primitives + v0.9.3 NavMode; what was
missing was the live KSP-style instruments to fly it by. Adding
those instruments closes the loop without the autopilot route.

#### Cross-view rotation parity in orbit-flat
<!-- llm-parse: id=cross-view-rotation-parity status=open-question target=v0.9.5-plus -->
📐 **open · v0.9.5+ polish**. v0.9.2 fixes Landed-craft visual
position to match the renderer's tilted-axis sub-observer point in
the default top view, but orbit-flat falls back to a static basis
because the perifocal frame co-rotates with the body for Landed
craft. Cross-view consistency in the texture pipeline (so a
launchpad spawn lines up the same way regardless of view) is
deferred polish.

#### Pitch trim fine resolution
<!-- llm-parse: id=pitch-trim-fine-resolution status=open-question target=v0.9.5-plus -->
📐 **open · v0.9.5+ polish**. v0.9.2.1 bumped pitch trim step from
5° → 10° because the original required 6+ key presses for an initial
pitch-over. 10° is reasonable for the first few degrees but mid-
ascent fine-tuning at 1° resolution would help. Should `>` / `<`
repeat-accelerate (hold-to-tilt-faster), expose a numeric input, or
take a Δ argument? Pick at v0.9.5+ if the gravity-turn assist
doesn't subsume manual trim entirely.

---

## Upcoming — v0.9 cycle plans

<!-- llm-parse: cycle=v0.9 status=in-progress -->

**Cycle theme: "the craft fleet grows up."** Plan committed at
[`docs/v0.9-plan.md`](v0.9-plan.md); first two slices (v0.9.0
targeting + v0.9.1 staging) shipped 2026-05-05. **v0.9.2 ground-
launch primitives are feature-complete on branch / PR #51 but ship
as work-in-progress** — manual ascent to LEO is unreliable without a
gravity-turn assist (see [v0.9.2 entry](#v092-work-in-progress)).
Cycle order is unchanged; v0.9.3 (rendezvous) and v0.9.4 (navball)
operate on already-orbiting craft and are unblocked by the .2 WIP
status. A v0.9.5+ ergonomic-pass slice (gravity-turn assist) is
promoted from "reopen if friction" to a committed candidate.

The v0.8 cycle delivered multi-craft capability and the precision
tooling (RCS, docking, drag, body-equatorial frame, adaptive warp)
to support it — but the *fleet itself* is still one chemical
S-IVB-class stage and three minor variants. v0.9 grows the fleet
(staging) and the operational tooling that becomes useful once you
have multiple capability tiers in flight (rendezvous, mission
scripting properly designed — the latter deferred to v0.10).

### Sizing note from the v0.8 retrospective
<!-- llm-parse: planning_caveat=v0.8-scope-creep -->

Visual / polish / frame-convention slices in v0.8 consistently
grew **2–3× past their original LOC estimates**. v0.8.5 was
scoped as "lon0 in textures" and shipped view-aware projection +
tilted Saturn rings + polygon Earth grid + textured Sun +
Galileans + Uranus / Neptune banding + terminal-moon focus
zoom + warp-clamp on rotation. v0.8.6 was scoped as "controls
polish, ~250 LOC" and shipped the keymap pass + iterate-for-
target toggle + body-equatorial Keplerian frame for body-bound
orbits + adaptive warp clamps (throttle + upcoming-node
predictive ramp-down) + orbit-flat ω-snap + pole-on guard, at
~600 LOC actual.

Pattern: slices that **touch rendering, frame conventions, or
planner UX** snowball — each piece reveals the next assumption.
Slices that touch **isolated planner / sim internals** (e.g.
`PlanInclinationChange`, `IterateForTarget`, individual residual
functions) tracked closer to estimate.

Apply the 2–3× heuristic to v0.9 sizing for any slice in the L
or M tier that touches rendering / frames / planner UX. Slices
in the S tier (isolated UI surfaces, library plumbing) are
typically size-stable.

### Provisional slice candidates

In rough priority / dependency order, with weight estimates per
the legend below. **None of these are committed slices** —
they're planning-mode candidates pending an explicit v0.9 plan
doc.

**Weight tiers**:

- **L** (large) — substantial new architecture or multi-cycle
  dependency. Plan generously — 1+ weeks of focused work.
- **M** (medium) — bounded but non-trivial; ~150–300 LOC + tests
  + design discussion. 2–4 days.
- **S** (small) — bounded UI / polish; ~50–150 LOC. < 1 day each
  but often clusters into a slice.

| Order | Slice | Weight | Status | Notes |
|---|---|---|---|---|
| 1 | [Staging chain](#staging-chain) | L | 🧊 backlog | Ground launch → LEO → ICPS → lander chain. Composes multi-stage staging + atmosphere + launch mechanics. Unblocks practical use of (2)–(4). Open Qs: [staging continuity](#staging-continuity), [composite-craft mass distribution](#composite-craft-mass-distribution-post-docking) gate this. |
| 2 | [Mission scripting](#mission-scripting--editor) | L | ⚠ rolled back | **Design-pass first** (eight decision points), then re-implement. Reference v0.8.7-attempt artifacts only for implementation shape. Block 1: write the modder-UX target end-to-end. |
| 3 | [Wider cross-SOI PlanTransfer](#wider-cross-soi-plantransfer) | L | 🧊 backlog | Heliocentric → moon-of-other-planet patched-conic capture. Substantial new transfer math. |
| 4 | [Combined plane-shift + Hohmann](#combined-plane-shift--hohmann) | L | 🧊 backlog | Lambert constrained on post-capture inclination. Substantial — root-find on inclination + time-of-flight. |
| 5 | [Rendezvous tooling](#rendezvous-tooling) | M | 🧊 backlog | Target-craft selection + target-relative burn modes + null-v_rel at closest approach + iteration. Pairs with multi-craft fleet from (1). |
| 6 | [Solar lighting + terminator + eclipses](#solar-lighting--daynight-terminator--eclipses) | M | 🧊 backlog | Research-first — ANSI 24-bit canvas mixing investigation precedes slicing. **Apply 2–3× sizing heuristic — touches rendering.** |
| 7 | [Predictor adaptive sampling](#predictor-adaptive-sampling) | M | 🧊 backlog | Three-cycle carry-over; foundation shipped v0.8.4. ~150–200 LOC. |
| 8 | [Multi-rev porkchop UI](#multi-rev-porkchop-ui) + [Lambert short/long picker](#lambert-shortlong-branch-picker) | S | ⏸ deferred | UI for `LambertSolveRev` (nRev + retrograde + short/long). Library-ready since v0.7.5. Useful once (1) staging grows the fleet. |
| 9 | [Capture-direction toggle](#capture-direction-toggle) | S | 🧊 backlog | "Capture prograde-around-target" mode for auto-Hohmann arrival. Trades ~50–100 m/s for the right-direction capture. |
| 10 | [Theme-file hot-reload](#theme-file-hot-reload) | S | ⏸ deferred | ~200 LOC fsnotify watcher. Reopen if a modder hits theme-iteration friction. |
| 11 | Polish open questions ([spawn-form persistence](#spawn-form-persistence), [docking visual feedback](#docking-visual-feedback), [numbered craft slots](#numbered-craft-slots-19)) | S | 📐 / 🧊 | Bundle-of-small-stuff candidates if cycle bandwidth allows. |

### Pre-cycle checklist

Before opening `docs/v0.9-plan.md`:

1. **Resolve mission-scripting design pass** (or explicitly defer
   it to v0.10). The retrospective in
   [`state-of-game-archive.md`](state-of-game-archive.md) §6
   *Mission scripting / editor* lists eight decision points; the
   pass should produce a position on each.
2. **Decide staging-chain shape**: KSP-style player-managed
   staging (sequential decouples on a stack), or auto-managed
   (planner plants stage events alongside burn nodes)?
3. **Decide rendezvous-tooling scope**: full target-relative
   modes + auto-plant, or just the target-cycle infrastructure
   (modes/UI deferred to a follow-up)?
4. **Confirm v0.8.7 stays vacant.** The tag is reserved by the
   rolled-back attempt and currently unused. If a small patch
   slice fits between v0.8.6 and v0.9, it'd take v0.8.7 — but
   anything tagged v0.8.7 should not be the mission-scripting
   work it was originally claimed for.

### Out of scope for v0.9

- Multiplayer implementation (architecture spike from v0.6.6 still stands; but cycle bandwidth doesn't fit).
- N-body perturbations.
- Multi-system spacecraft.

---

## Reference

- Per-cycle planning docs: `docs/v0.6-plan.md`, `docs/v0.7-plan.md`, `docs/v0.8-plan.md`. (`v0.9-plan.md` to be opened.)
- Original architecture / phase plan: `docs/plan.md`.
- Multiplayer design spike: `docs/multiplayer-design.md`.
- Integration / numerical stability: `docs/integration-design.md`.
- Per-cycle release notes: `docs/v0.5-release-notes.md`.
- Full historical detail (predecessor of this doc, ~1,200 lines): [`docs/state-of-game-archive.md`](state-of-game-archive.md).

<!-- llm-parse: end-of-doc -->
