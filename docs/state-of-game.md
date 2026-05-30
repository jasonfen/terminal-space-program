# terminal-space-program — state of game

<!--
  meta:
    snapshot_version: v0.12.2 (Slice 5 follow-on — Projected Orbit
      Kepler-step fidelity (#66) + Line-of-Nodes split rendezvous
      (#67); v0.12 cycle in progress, Slices 2/3 + flame polish remain)
    snapshot_date: 2026-05-29
    revised_date: 2026-05-29 (header refreshed from the stale v0.9.6
      snapshot: v0.10–v0.12 cycle summaries folded in, released-
      versions table extended through v0.12.2; origin/main sole branch)
    archive: docs/state-of-game-archive.md
  Read the archive for the full v0.7.6-baseline-plus-v0.8-additions
  detail this rewrite condensed. This file is the canonical
  "what's the game today / where is it going" reference. Per-feature
  detail for v0.10+ lives in the cycle plans (docs/v0.1x-plan.md) +
  ADRs (docs/adr/); this file keeps the entries + the snapshot.
-->

> Snapshot at **v0.12.2** (May 2026). Three cycles have shipped to
> `main` since v0.9.6:
>
> - **v0.10** (May 19–23) — planner + maneuver tooling: rate-limited
>   slew (attitude), the staging chain, rendezvous tooling, true
>   plane-match / inclination burns, multi-rev porkchop, and a
>   perspective-tilt orbit view.
> - **v0.11** (May 24–27) — the launch chase-cam (`ViewLaunch`), the
>   crashed/landed lifecycle ([ADR 0004](adr/0004-crashed-landed-lifecycle.md)),
>   and Lander silhouette + soft-landing polish.
> - **v0.12** (May 29, *in progress*) — numbered craft slots + a
>   cleanup bundle (v0.12.0); the combined plane-shift + Hohmann
>   dual-strategy intra-primary transfer (v0.12.1,
>   [ADR 0005](adr/0005-combined-plane-shift-hohmann-via-lambert.md));
>   and the **Slice 5 follow-on** (v0.12.2,
>   [ADR 0006](adr/0006-intra-primary-transfer-arrival-and-predictor-fidelity.md)):
>   analytic-Kepler Projected-Orbit fidelity (GH #66) and a
>   Line-of-Nodes split that actually rendezvous with an inclined
>   Luna (GH #67).
>
> **Remaining in v0.12:** parachutes (Slice 3, ADR 0008), two-colour
> flame polish (Slice 4 leftover), and the Moon-frame lunar-capture
> inclination trim (GH #68, deferred from v0.12.2). The 2-stage Lander
> + surface staging (Slice 2, ADR 0007) shipped in **v0.12.3**. Pre-v0.8
> per-feature detail preserved at
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
craft fleet has since grown past that modest start — a player-
managed staging chain (v0.9.1), craft types + docking (v0.8), a
Lander with landing legs + hypergolic flame (v0.11.5), and
numbered craft slots (v0.12.0).

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
Mars/Jupiter. v0.8 — "multi-craft polish" — grew well past the
headline: RCS / monoprop, multi-craft slate, craft types, docking,
atmospheric drag, sim-time rotation with view-aware projection,
body-equatorial Keplerian frame, adaptive warp clamps, finite-burn
iterate-for-target. v0.9 ("the craft fleet grows up") added the
unified target slot, the staging chain, ground-launch + ascent
ergonomics, rendezvous tooling, the navball, and solar lighting +
eclipses. v0.10 deepened the planner (slew, true plane-match +
inclination burns, multi-rev porkchop) and the orbit view
(perspective tilt). v0.11 added the launch chase-cam and the
crashed/landed lifecycle (ADR 0004) with Lander / soft-landing
polish. v0.12 (in progress) brought numbered craft slots, the
combined plane-shift + Hohmann dual-strategy transfer (ADR 0005),
and the Line-of-Nodes split + analytic-Kepler predictor fidelity
(ADR 0006).

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
| [v0.12](v0.12-plan.md) | 2026-05-29 | ✓ | **Cycle in progress.** Numbered craft slots + cleanup (v0.12.0); combined plane-shift + Hohmann dual-strategy transfer (v0.12.1, ADR 0005); Slice 5 follow-on — Line-of-Nodes split rendezvous + analytic-Kepler Projected-Orbit fidelity (v0.12.2, ADR 0006, GH #66/#67). Remaining: lander 2-stage (ADR 0007), parachutes (ADR 0008), flame polish, GH #68. |
| [v0.11](v0.11-plan.md) | 2026-05-24 → 27 | ✓ | Launch chase-cam (`ViewLaunch`, v0.11.0); crashed/landed lifecycle (ADR 0004); Lander silhouette + soft-landing polish (v0.11.2–.5). |
| [v0.10](v0.10-plan.md) | 2026-05-19 → 23 | ✓ | Planner + maneuver tooling — rate-limited slew, staging chain, rendezvous tooling, true plane-match/inclination burns, predictor adaptive sampling, multi-rev porkchop, perspective-tilt orbit view, launch-anchor. |
| [v0.9.6](#v096) | 2026-05-17 | ✓ | Solar lighting + day/night terminator + eclipses, plus a navball overhaul (flicker root-cause fix + KSP-style framed panel). Closes the v0.9 cycle. |
| [v0.9.5](#v095) | 2026-05-15 | ✓ | Navball — bottom-right HUD attitude indicator. Braille-rendered sphere, classic-ADI horizon split, per-mode SAS/target/maneuver-node markers, NavSurface compass ticks. Merged to `main` (730705d); playtest signoff in progress. |
| [v0.9.4](#v094) | 2026-05-07 | ✓ | Ascent ergonomics — predictive ap/pe/Δv→circ in LAUNCH HUD, ORBIT READY callout, NavSurface auto-snap on launchpad spawn, single-key `C` plants circularize-at-apoapsis. Closes the v0.9.2 WIP friction without an autopilot. Merged via PR #53. |
| [v0.9.3](#v093) | 2026-05-06 | ✓ | Rendezvous tooling — target-relative SAS modes (`BurnTarget*`), TCA / CA / DOCK READY in TARGET HUD, KSP-style NavMode cycle (`;`), `m`-form integration with `next closest approach` trigger event. Merged via PR #52. |
| [v0.9.2](#v092) | 2026-05-05 | ✓ | Ground-launch primitives — launchpad spawn, surface-frame SAS, pitch trim, LAUNCH HUD. Shipped via PR #51; v0.9.4 ascent ergonomics closed the manual-ascent friction. |
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

### v0.9.6
<!-- llm-parse: version=v0.9.6 status=shipped date=2026-05-17 theme=solar-lighting+navball-overhaul merge=32e8d03 -->

**Solar lighting + day/night terminator + eclipses, plus a navball
overhaul.** Two strands landed in the v0.9.6 line on `main`
(merge `32e8d03`): the research-first lighting backlog item, and an
unplanned navball polish pass triggered by a marker-flicker bug
report that turned into a full KSP-style redesign.

**Solar lighting** (`internal/render/lighting.go`,
`internal/render/eclipse.go` + tests). Sub-solar-point per body
per tick → `cos(angle to sun)` shading with a day/night
terminator; eclipses fall out of the same geometry. Originally
earmarked v0.9.6 (research: ANSI 24-bit per-cell mixing); merged
from `v0.9.6-lighting` (78639ea).

**Navball overhaul** (`internal/render/navball.go`,
`internal/tui/screens/navball_panel.go`,
`internal/tui/screens/orbit.go`, `internal/tui/app.go`):
- **Flicker root-caused & fixed.** Three layered float-precision
  bugs: sub-observer jitter (sticky 2° great-circle dead-band on
  `OrbitView`), off-disk markers culled at the limb (clamp to rim
  instead), and — the real culprit — orbit-normal markers sitting
  exactly on the limb where `z>0` picked front/back from noise
  (limb dead-zone `|z| ≤ limbFrontEpsZ` ⇒ stable). Plus a
  multi-rune SGR splice bug that dropped the panel's right border.
- **Relocated + redesigned.** Out of the HUD column into an
  opaque rounded-border panel composited bottom-right over the
  canvas (ANSI-aware `overlayStyledBlock` / `splitStyledCells`).
  KSP-style: no "NAVBALL" label, a `[MODE]`+`RCS` toggle row, and
  eight 2-row labeled SAS buttons (`⊕ PRO` … `◌ T-`, incl. target
  ±) as a vertical column; disk doubled to 24×12. Clicks wired to
  the NavMode cycle + SAS-hold/RCS via `HitNavballControl` /
  `dispatchNavballControl`.
- **HUD trim + ball retune.** Dropped the SYSTEM + SELECTED HUD
  blocks (system name still in the title bar); ball palette moved
  from classic-ADI orange toward KSP blue/tan with a bright
  horizon line. `view:` readout moved to the bottom-left corner.

**Shipped on `main`** (`32e8d03`) 2026-05-17 — build / vet / full
test suite green at merge; interactive playtest pending. All
feature branches cleaned up afterwards; `origin/main` is the sole
branch.

### v0.9.5
<!-- llm-parse: version=v0.9.5 status=shipped date=2026-05-15 theme=navball branch=v0.9.5-navball merge=730705d -->

**Navball — bottom-right HUD attitude indicator.** A KSP-style
attitude ball clamped into the orbit-screen HUD: nothing else in
the HUD showed where the craft's nose points relative to the
orbit / target / surface frame. Visualization-only — the `;`
NavMode cycle controls it consumes already shipped in v0.9.3, so
this slice paints the picture v0.9.3 wired the controls for.

**Shipped on `v0.9.5-navball`, merged to `main` via `--no-ff`
(730705d) + pushed origin 2026-05-15. Playtest signoff in
progress** (build / vet / full test suite green at merge; no
interactive playthrough yet).

- **Braille sphere** (`internal/render/navball.go`): 12×6-cell /
  24×24-dot genuinely-circular disk with classic-ADI sky-blue /
  orange horizon split, limb outline + horizon band, center
  reticle, and a subtle 30° parallel + meridian grid (re-added
  after the early flicker-driven removal once sub-observer was
  quantized to 1°).
- **Markers** (`internal/sim/navball.go`): driven through
  `ResolveAttitudeIntent` + `BurnDirectionWithTarget` so every
  glyph sits exactly where its axis key would aim. Six SAS
  cardinals per mode (prograde / retrograde / normal± / radial±);
  NavTarget swaps radial± to ◉ / ◌ at the line-to-target;
  maneuver-node markers (◎) per planted node in the per-leg
  trajectory colour; N/E/S/W compass ticks in NavSurface.
  Back-hemisphere markers paint dimmed (Faint); front wins at
  coincident cells.
- **HUD wiring** (`internal/tui/screens/orbit.go`): clamped into
  the bottom-right HUD column, marker set + horizon plane chosen
  from `World.NavMode` directly — no new key binding or save
  field (all shipped in v0.9.3). Also dropped the stale "coming
  v0.9.3" toasts on `H`/`I` when a craft target is set.

**LOC.** ~1300 including tests — well under the 3×-rendering-
snowball ~700 plan estimate; the v0.8.5 `SubObserverPointDeg` +
per-pixel sphere pipeline reuse held cleanly, so the renderer-
reuse sizing risk did not materialise.

**Cycle status.** Superseded by v0.9.6 above — solar
lighting+eclipses landed as the v0.9.6 pick, closing the v0.9
cycle. v0.10 planning is underway.

### v0.9.4
<!-- llm-parse: version=v0.9.4 status=shipped date=2026-05-07 theme=ascent-ergonomics branch=claude/improve-launch-rendezvous-BJj0Y pr=53 -->

**Ascent ergonomics — closes the v0.9.2 ground-launch loop.** The
v0.9.2 retrospective flagged "manual ascent to LEO unreliable" as
the gating friction. v0.9.4 transplants the v0.9.3 rendezvous
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
guidance, not primitives. v0.9.4's live-guidance HUD makes the same
v0.9.2 primitives playable. Open question #7 (launch gravity-turn
assist) is resolved in favour of option (a) (live HUD overlay) over
option (b) (autopilot).

### v0.9.3
<!-- llm-parse: version=v0.9.3 status=shipped date=2026-05-06 theme=rendezvous branch=v0.9.3-rendezvous pr=52 -->

Rendezvous tooling (manual-first) shipped on
`origin/v0.9.3-rendezvous`. All four target-relative SAS modes
(`BurnTargetPrograde` / `BurnTargetRetrograde` / `BurnTarget` /
`BurnAntiTarget`); `planner.NextClosestApproach` with live TCA / CA
/ DOCK READY readouts in TARGET HUD; KSP-style NavMode cycle (`;`)
that reroutes the same six SAS axis keys per frame
(Orbit/Surface/Target); `m`-form integration with the
`next closest approach` trigger event + `ManeuverNode.TargetCraftIdx`
captured-at-plant + save round-trip. **Folded into the v0.9.4
working branch** so ascent ergonomics can build on the NavMode
auto-snap pattern.

### v0.9.2
<!-- llm-parse: version=v0.9.2 status=shipped date=2026-05-05 theme=ground-launch branch=v0.9.2-ground-launch pr=51 -->

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
promoted to a v0.9.6+ slice candidate.

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
  Promoted to a v0.9.6+ slice candidate. Two options: (a) target
  pitch-vs-altitude HUD overlay (lightweight, leaves flying
  manual), or (b) autopilot toggle that drives throttle + attitude
  along a baked Saturn V profile.
- **Pitch trim fine resolution** — 10° is reasonable for initial
  pitch-over but mid-ascent fine-tuning at 1° resolution would help.
  Open question: should `>` / `<` repeat-step or take a Δ argument?
- **Cross-view rotation parity in orbit-flat** — the current fix
  makes the Landed craft co-rotate with surface texture in the
  default top view, but orbit-flat falls back to a static basis.
  Texture pipeline parity across views deferred to v0.9.6+.

**Sizing.** Plan called for ~400 LOC + 2× heuristic = ~800. Landed
at ~600 production + ~250 tests = ~850 total across the v0.9.2
branch (close to plan, with the unplanned add-ons — surface-frame
SAS, pitch trim, per-stage BC, body-fixed↔world transforms,
Landed integration — accounting for ~250 LOC of the production
total).

**Status decision.** Slice ships **unmerged on the
`v0.9.2-ground-launch` branch / PR #51** until either the gravity-
turn assist lands or the user accepts the WIP state with eyes open.
Cycle order does not change — v0.9.3 (rendezvous) and v0.9.5
(navball) operate on already-orbiting craft and are unblocked by
this WIP status. The v0.9.2 primitives are foundation that the
v0.9.4 ascent ergonomics slice layered on top of, not throwaway.

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
<!-- llm-parse: id=rendezvous status=partially-shipped target=v0.9-foundations+v0.10.2-actionability -->
🟡 **partially shipped — foundations v0.9.3, actionability v0.10.2; manual loop unchanged.** Five sequenced steps from the original spec:

1. **Target-craft selection** — ✅ v0.9.0 (`World.Target` unified slot) + v0.9.3 (per-craft target binding survives active-craft switches).
2. **Target-relative prograde / retrograde burn modes** — ✅ v0.9.3 (`BurnTargetPrograde` / `BurnTargetRetrograde` / `BurnTarget` / `BurnAntiTarget`).
3. **Prograde-to-target nudge from a phasing orbit** — ✅ v0.10.2 (`World.PlanRendezvousNudge` / `K` keystroke; Lambert intercept ⇒ project onto eight velocity-frame axes ⇒ verify via `NextClosestApproach`; TARGET HUD shows achievable CA + Δv readouts).
4. **Null v_rel at predicted closest approach** — ✅ v0.9.3 (`m`-form `next closest approach` trigger event + `BurnTargetRetrograde` planted node). Manual step — no auto-plant variant.
5. **Iterate until in DOCK READY range** — ✅ player loop using the above; the v0.10.2 advisory recomputes per frame so the recommendation updates after each burn.

**Remaining open** (deferred, not gating): a single `World.PlanRendezvous` auto-plant that chains steps 3 + 4 in one keystroke. v0.10.2's `K` covers step 3 only; the player handles step 4 manually through the `m` form. The remaining auto-plant is a candidate v0.11+ slice rider once the slice-3 actionability has playtest signal.

### Mission scripting / editor
<!-- llm-parse: id=mission-scripting status=rolled-back target=v0.9 attempted=v0.8.7 -->
⚠ **rolled back — needs design pass before re-attempt**. A draft Option-B implementation landed (commit `4159a31`) and was reverted (`e806dd3`) because eight design decision points (engine pick, modder UX flow, error feedback, schema versioning, cross-craft predicate scope, mass/propellant fields, ceiling-vs-floor expectation, editor surface, sandboxing) collapsed into "expr-lang/expr is lighter, ship it" without their own pass.

The reverted artifacts are git history, not a starting point. **Do not re-implement without the design pass.** Full retrospective + decision-point list in [`state-of-game-archive.md` §6 *Mission scripting / editor*](state-of-game-archive.md). Suggested sequencing: (1) write the modder-UX target end-to-end, (2) pick the engine in service of that UX, (3) reference v0.8.7-attempt artifacts only for implementation shape, (4) implement.

### Multi-rev porkchop UI
<!-- llm-parse: id=multi-rev-porkchop status=in-progress target=v0.10.5 -->
🛠 **in progress · v0.10.5** (branch `v0.10.5-porkchop`). The three-cycle-deferred carry-over has shipped on branch — the porkchop screen gains an `o` transfer-options sub-menu that exposes nRev (0–3), prograde/retrograde, and the new short/long branch picker. The TOF axis auto-scales by `(nRev+1)` so multi-rev cells live in a sensible TOF window. The options bundle (`sim.TransferOptions{NRev, Retrograde, LongBranch}`) plumbs through `World.PorkchopGrid` → `World.PlanTransferAt` so a planted Δv matches the cell's scored Δv for any options combo. Specced in `docs/v0.10-plan.md` §v0.10.5. Pairs with the [Lambert short/long branch picker (below)](#lambert-shortlong-branch-picker) — both shipped in the same slice.

### Lambert short/long branch picker
<!-- llm-parse: id=lambert-short-long status=in-progress target=v0.10.5 -->
🛠 **in progress · v0.10.5 · pairs with multi-rev porkchop**. `LambertSolveRev` now takes a trailing `longBranch bool` (after `retrograde bool`). For nRev≥1 the two roots flanking the minimum-energy critical z map to short (lower z, more eccentric, lower-TOF) and long (higher z, higher-TOF) branches; the flag seeds Newton from the appropriate side and confines steps to the rev band so it can't leap across the critical z. For nRev=0 the flag is ignored (single branch). Plumbed through `PlanLambertTransfer` + `PorkchopGrid` + the new porkchop sub-menu (`b` toggle). Shipped on branch `v0.10.5-porkchop`.

### Wider cross-SOI PlanTransfer
<!-- llm-parse: id=cross-soi-transfer status=backlog target=v0.9 -->
🧊 **backlog · target v0.9**. v0.5.7's `PlanIntraPrimaryHohmann` covers same-parent (LEO → Luna); v0.6.3 covers moon → parent. The remaining direction — heliocentric → moon-of-other-planet (Phobos from a Mars approach, a Galilean from a Jupiter cruise) — needs a real patched-conic capture pass through both SOIs.

### Combined plane-shift + Hohmann
<!-- llm-parse: id=plane-shift-hohmann status=shipped target=v0.12 weight=XL adr=0005 slice=v0.12-slice-5 -->
✓ **shipped · v0.12 (Slice 5) · XL**. Grilled 2026-05-28 (`/grill-with-docs`); design frozen in [`docs/v0.12-plan.md`](v0.12-plan.md) §Slice 5 + [`docs/adr/0005`](adr/0005-combined-plane-shift-hohmann-via-lambert.md). Shipped: a **fused single-rev Lambert** (`planner.PlanIntraPrimaryFused`) handles the combined departure (eccentricity + raise + plane change fall out for free); a new **`BurnVector`** fixed-inertial burn mode carries the 3D departure Δv (round-trips through save, alongside a fix to the latent `PlaneChangeRad` save gap); `sim.PlanTransfer`'s intra-primary path computes **both** the combined fused transfer and a split (coplanar raise + plane change at the slow transfer apoapsis), auto-picks the cheaper, and flashes both Δv totals. The split wins for inclined targets (an equatorial LEO sits ~25° off Luna's plane), keeping LEO→Luna playable (~3.3 km/s vs ~13.8 for the combined). The `HohmannDepartureWarning` guard was re-pointed to a preview-accuracy note (the obsolete "match plane [I], circularize, then [H]" advice is retired); `I`/`PlanPlaneMatch`/`PlanInclinationChange` stay as standalone tools. Scope held to intra-primary (heliocentric prograde-fudge retained). **Deferred to a follow-on:** the predictor-fidelity densification around the encounter window (ADR 0005 decision 5). **Historical spec below.** Lambert solver constrained on post-capture inclination so departure geometry lands prograde at the destination instead of the current "match ecliptic, hope arrival inclination is OK" pattern. The v0.8-plan.md retrospective explicitly flags this as **substantial** — the binding technical work is the constrained Lambert variant (root-find on inclination as well as time-of-flight), not the UI plumbing. Pairs naturally with the [capture-direction toggle](#capture-direction-toggle) since both touch arrival-side geometry.

**Why now committed (2026-05-19, from v0.10.1 playtest):** the
playtest confirmed the user-visible symptom this fixes — the `H`
auto-plant "only calculates properly from a 0° inclination,
circular orbit." Root cause is two coupled assumptions in
`planner.PlanIntraPrimaryHohmann` / `HohmannTransfer` +
`sim.PlanTransfer`: (1) `rPark := craft.State.R.Norm()` is fed in
as a *circular* radius (`vDepCirc = √(µ/rPark)`), so an eccentric
departure orbit gets the wrong Δv and the burn isn't placed at
periapsis; (2) phasing is a flat `atan2(R.Y, R.X)` with **no
plane-change term at all**, so an inclined parking orbit can't
reach the target's plane. A non-blocking advisory guard shipped
in v0.10.1 (`sim.HohmannDepartureWarning` / `hohmannGuardDetail`,
surfaced on `H` and in `HohmannPreview.Warn`) so the symptom is
no longer silent, but it does **not** change the math.

**Committed scope for the real fix (the L slice):**
- **Eccentric-aware departure** — derive the parking orbit's
  Keplerian elements, place the departure impulse at periapsis (or
  the optimal true anomaly), and size Δv off the *actual* speed
  there, not `√(µ/r)`. Removes assumption (1); independently
  useful even coplanar.
- **Plane change folded into the transfer** — the constrained
  Lambert variant: root-find on the inclination/RAAN match as well
  as time-of-flight so the transfer arc leaves the craft's plane
  and arrives in the target's, replacing the separate `I`-then-`H`
  dance. Removes assumption (2).
- **Δv split policy** — combined plane+raise at departure vs. a
  cheaper plane change at the higher-altitude node; expose enough
  in the preview/HUD that the player sees the trade.
- **Guard retirement** — once the planner is plane/eccentricity
  aware the v0.10.1 advisory becomes a (much rarer) true-error
  case; downgrade or remove it.
- **Test surface** — eccentric coplanar departure matches an
  independent two-body propagation; inclined departure arrives in
  the target plane within tolerance; `I`-then-`H` and the combined
  path converge to the same orbit.

Sized **L / substantial** (real transfer math, not UI plumbing);
**not** in v0.10 — targeted at v0.11 (its own slice), pairs with
the [capture-direction toggle](#capture-direction-toggle) and
[wider cross-SOI PlanTransfer](#wider-cross-soi-plantransfer).

### Inclination burn: true plane change
<!-- llm-parse: id=inclination-true-plane-change status=in-progress target=v0.10.4 trigger=playtest -->
🛠 **in progress · v0.10.4** (branch `v0.10.4-inclination-burn`). Playtest-triggered: a target-Moon → `I` → `H` run missed the lunar encounter. **Three bugs, all fixed:** (1) the `I` auto-plant planted a pure `BurnNormalPlus`/`Minus` node — but a pure orbit-normal burn adds Δv perpendicular to velocity, so `|v_new| = √(|v|²+|Δv|²) > |v|`: it speeds the craft up (orbit becomes eccentric) and under-rotates the plane. Fix: new `BurnPlaneChange` mode + signed `PlaneChangeRad` on `ManeuverNode`/`ActiveBurn`; `spacecraft.planeChangeDirection` rotates the horizontal velocity about the radial axis, preserving `|v|`. (2) `I` matched only the inclination *magnitude* (`PlaneMatchInclination`) — equal inclination, different Ω, still 25–39° apart. Fix: `sim.PlanPlaneMatch` rotates the orbit to fully *coincide* with the target body's plane (geometric θ about the mutual node line). (3) `orbital.TimeToNodeCrossing` was degenerate for circular orbits (used ν + ω) → fired the burn at the wrong point; now uses the argument of latitude. No save migration (omitempty fields). Scope-committed in `docs/v0.10-plan.md` §v0.10.4. **Follow-up:** the predicted-trajectory renderer integrates long transfer legs too coarsely to *draw* the Moon encounter (the maneuver is correct; the dashed line isn't) — a v0.10.3-area predictor-fidelity fix. Distinct from the L-tier [combined plane-shift + Hohmann](#combined-plane-shift--hohmann) (v0.11).

### Capture-direction toggle
<!-- llm-parse: id=capture-direction-toggle status=backlog target=v0.9 -->
🧊 **backlog**. Today's auto-Hohmann arrival burn is retrograde-in-source-frame. A "capture prograde-around-target" mode would burn differently and trade ~50–100 m/s for the right-direction capture.

### Drag-to-edit nodes
<!-- llm-parse: id=drag-to-edit status=deferred -->
⏸ **deferred · playtest-triggered**. v0.6.4 deliberately picked click-to-edit-replace as the model; v0.8.6 retrospective held the line. Drag-to-scrub Δv / fire-time directly on a planted-node marker is the alternative model — KSP players reach for it on muscle memory. Reopen this slice when (and only when) playtest feedback reports click-to-edit-replace as actually friction; until then the simpler interaction wins.

### Predictor adaptive sampling
<!-- llm-parse: id=predictor-adaptive-sampling status=in-progress target=v0.10.3 carry-over=v0.5-v0.6-v0.7-v0.8 -->
🛠 **in progress · v0.10.3** (branch `v0.10.3-predictor-sampling`). The flat 96-sample-per-leg budget collapsed the predicted dashed orbit to a smear whenever a leg's horizon spanned many revolutions (routine at high warp, with a node planted dozens of orbits ahead). Fix: `adaptiveSampleCount(horizon, period)` in `internal/sim/predict.go` — ~96 points per orbital period the horizon spans, clamped to `[96, 720]`; `PredictedLeg.Samples` carries the per-leg budget; `tui/screens/orbit.go` drops its hardcoded 96. A v0.10.4 playtest exposed a second predictor-fidelity bug folded into this slice: the integrator *sub-step* cap was still `period/100` (~8000 s for an Earth→Moon transfer ellipse), too coarse to resolve a lunar SOI encounter — the dashed line flew off to a bogus heliocentric escape instead of drawing the encounter. Fixed via `predictMaxSubStep` = `min(period/100, 120 s)`. Scope-committed in `docs/v0.10-plan.md` §v0.10.3. Was a three-cycle carry-over (flagged in `v0.5-release-notes.md`, `v0.7-plan.md`, `v0.8-plan.md` backlogs, escalated to `integration-design.md` §10); the **v0.8.4** time-aware `propagateStateWithPrimary` foundation meant no infrastructure work was needed. **Superseded for fidelity in v0.12.2 (GH #66, PR #69):** the `predictMaxSubStep`=120 s cap still drifted ~46 000 km on the e≈0.96 LEO→Luna coast (fixed-step Verlet truncation); coast legs (both `PredictedSegmentsFrom` and the sibling `propagateStateWithPrimary`) now propagate with analytic `KeplerStep` — exact, no drift. Adaptive sampling stays; the 120 s cap remains only for SOI/surface-check granularity.

### Solar lighting + day/night terminator + eclipses
<!-- llm-parse: id=lighting-terminator-eclipses status=shipped version=v0.9.6 merge=32e8d03 -->
✅ **shipped v0.9.6** (`internal/render/lighting.go` + `eclipse.go` + tests; merged from `v0.9.6-lighting` 78639ea → `main` 32e8d03). Sub-solar-point per body per tick → `cos(angle to sun)` shading + day/night terminator; eclipses fall out of the same geometry. Was the research-first v0.9.6 pick that closed the v0.9 cycle.

### Staging chain
<!-- llm-parse: id=staging-chain status=in-progress target=v0.10.1 -->
🛠 **in progress · v0.10.1** (branch `v0.10.1-staging`). Basic manual chain shipped v0.9.1 (`space` decouples `Stages[0]`, Saturn-V loadout, STAGES HUD). The L expansion — multi-tier loadouts (Apollo-Stack with mid-stage Lander payload separation) + a named stage catalog + a spawn-form stack configurator — is scope-committed in `docs/v0.10-plan.md` §v0.10.1. Unblocks (c) multi-rev porkchop and the rendezvous tooling slice (more craft → more practical scenarios).

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
<!-- llm-parse: id=numbered-craft-slots status=shipped shipped-in=v0.12.0 -->
✓ **shipped in v0.12.0** (Slice 4(c)). Number-row `1`..`9` jumps straight to craft index 0..8 via `World.SwitchToCraftIdx` (no wrap, no-op on an empty slot or a same-craft press; mirrors `CycleActiveCraft`'s `StopManualBurn`). `CraftSlot` keymap binding in `input.go`; handler in `app.go` next to `[`/`]`. `0` stays Pause. `?` overlay + README updated. Complements the v0.8.1 `[`/`]` cycle once fleets grow past 4. **Historical scope below.** v0.8.1 ships `[`/`]` cycle + click-select on per-craft glyphs (v0.8.2). Numbered hotkeys deferred until saves routinely have >4 craft and the cycle key gets unwieldy; gating was UX, not code.

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

### Rate-limited attitude (manual flight)
<!-- llm-parse: id=slew-attitude status=shipped shipped-in=v0.10.0 commit=5c596b6 pair=attitude-save decision=lead-compensated -->
✓ **shipped in v0.10.0** (`5c596b6` "rate-limited attitude (slew) + attitude save-persistence"; roll DOF added in follow-on `4a01dc8`). All committed scope landed: `Spacecraft.CurrentAttitudeDir` + per-loadout `SlewRateDegPerSec` (default 15°/s), `SlewToward` wired into `World.Tick` (`world.go:835`, gated on `!InstantSAS`), the `k`-key `ToggleInstantSAS` manual/auto gate, lead-compensated planted nodes (`maneuver.go:1399` + rendezvous lead), cosine-loss thrust along `CurrentAttitudeDir`, navball animates the slew, and the save round-trip at schema v6 (`current_attitude_dir`). *(Backlog entry was stale — never flipped when the code landed in v0.10.0; corrected 2026-05-28 during the v0.12 Slice 1 grill.)* **Historical scope below.** Today the nose is recomputed from `AttitudeMode` every tick, so an SAS heading change snaps instantly. Add a `Spacecraft.CurrentAttitudeDir` world-unit-vector that integrates toward the commanded `BurnDirectionWithTarget(AttitudeMode)` at a capped per-loadout `SlewRate` (deg/s) in **sim-time** (warp-scaled dt; effectively instant-relative-to-tick at high warp — accept + note). A new manual-flight **mode** gates it; instant "magic SAS" stays the default. `stepThrust` + the navball sub-observer read `CurrentAttitudeDir` instead of recomputing, so the navball animates for free and burning before alignment bleeds Δv to cosine loss — the intended consequence. RCS pulses stay instant (discrete nudges). **Planted maneuver nodes: lead-compensated — decided.** The craft auto-starts slewing `slew_angle / SlewRate` before T0 so it's aligned at ignition, preserving planted-node accuracy + its test surface; a punishing "naive, lose the Δv" toggle is a deferred later add. Net-new axis-angle rotate util (~30 LOC — no quaternion/Rodrigues in tree). Weight **M**, sim-only (no rendering-sizing risk). Regression surface: `burn_direction` / `navball` / `maneuver` tests.

### Attitude-mode save persistence
<!-- llm-parse: id=attitude-save status=shipped shipped-in=v0.10.0 commit=5c596b6 pair=slew-attitude -->
✓ **shipped in v0.10.0** (`5c596b6`, folded into the slew slice as planned). `CurrentAttitudeDir` round-trips through save/load via the schema-v6 additive `current_attitude_dir` field (`save.go:176` + write/read at 426/613); legacy saves with the field absent re-init via the `SlewToward` zero-vector snap guard. *(Backlog entry was stale — corrected 2026-05-28.)* **Historical scope below.** Held at "keep ephemeral — planted nodes are the persistence layer" through v0.9. The slew slice makes attitude *physically load-bearing* (a craft can be caught mid-slew), so `AttitudeMode` + `CurrentAttitudeDir` must round-trip through save/load or a reload teleports the nose. Promoted from deferred and folded into the slew slice as the logical partner — same subsystem, small rider (save-schema field bump + restore).

### Mass/propellant fields in mission EvalContext
<!-- llm-parse: id=mission-eval-resources status=backlog target=alongside-mission-scripting -->
🧊 **backlog**. `EvalContext` doesn't carry fuel / monoprop / dv_budget today, so the rolled-back v0.8.7 expression env had those zeroed. Trivial threading from `sim.World.Tick`; pairs with the mission-scripting design pass.

### Parachutes for atmospheric descent + recovery
<!-- llm-parse: id=parachutes status=backlog target=v0.12 origin=adr-0004 -->
🧊 **backlog · target v0.12 · ADR-worthy when it lands**. A non-engine path to soft Touchdown — a deployed parachute on a Vessel without `CanSoftLand` qualifies for the surface-arrival predicate via aerodynamic deceleration. Touches drag (deploy-time `BallisticCoefficient` bump), spacecraft state (parachute deploy flag, deployed-vs-torn states), control modes (deploy keystroke gated on dynamic pressure + altitude). The v0.11.4 Crashed / Soft-Landed lifecycle (ADR 0004) explicitly preserved this hook so capsule-class vessels — historically the natural parachute users — get a non-`CanSoftLand` route to surviving entry. Sources: `docs/adr/0004-crashed-landed-lifecycle.md` §214; `docs/v0.11-plan.md` §Deferred line 950.

### Stippled ground-fill variant
<!-- llm-parse: id=stippled-ground-fill status=backlog target=v0.12 origin=v0.11.4 -->
🧊 **backlog · target v0.12 · render polish**. Originally v0.11.4 scope; deferred — the flat surface fill shipped in v0.11.0 has held up across the v0.11.0–.4 playtest cycle and no felt "this looks flat" pain has surfaced. The render primitive (per-cell density / per-pixel dot pattern instead of solid fill) reuses the LUT body-fixed overlay machinery — no new integrator work, purely a `ViewLaunch` `drawSurfaceFill` rewrite. Reopen if the launch ground texture starts feeling visually flat after v0.11.5's silhouette polish lands. Source: `docs/v0.11-plan.md` §Deferred line 956.

### Polar-launch fallback hardening
<!-- llm-parse: id=polar-launch-fallback status=backlog target=v0.12 origin=v0.11.0 -->
🧊 **backlog · target v0.12 · low priority unless playtest exposes**. ViewLaunch Slice 1 (v0.11.0) ships a body-world-`+X` fallback for the surface-east basis when launching near a pole (where the canonical "east" tangent degenerates). v0.11.0–.4 playtest never surfaced a problem. Reopen if a polar-launch scenario — e.g., a Moon south-pole site repurposed as a launchpad — exposes a chase-cam rotation defect. Source: `docs/v0.11-plan.md` §Deferred line 960.

### Dedicated ViewLanding ViewMode
<!-- llm-parse: id=view-landing-mode status=backlog target=v0.12 origin=adr-0004 -->
🧊 **backlog · target v0.12 · gates on landing-HUD signal divergence**. v0.11.4 considered (and rejected) a dedicated landing-mode `ViewMode` in favour of "ViewLaunch covers both via shift+v manual jump" — ADR 0004 §γ rejected on feature-creep grounds. Reopen when landing-context HUD reads diverge from launch-context reads enough that one ViewMode can't honestly serve both. **Watch signal:** the v0.11.4-followup DESCENT HUD block (`internal/tui/screens/orbit.go` `shouldShowDescentHUD`) is the first landing-specific instrument introduced since the ADR — the threshold for splitting is more landing-specific rows that don't make sense in a launch context. Sources: `docs/adr/0004-crashed-landed-lifecycle.md` §222; `docs/v0.11-plan.md` §Deferred line 963.

### General vessel-removal action
<!-- llm-parse: id=vessel-removal-general status=backlog target=v0.12 origin=adr-0004 -->
🧊 **backlog · target v0.12 · UX scope distinct from Crashed-end-flight**. v0.11.4's end-flight `[E]` action removes *Crashed* vessels only. Removing *live* vessels — an orbital-cleanup tool, a sandbox-mode "abandon this stuck booster" — is a separate UX scope. Different confirmation flow (the player is deliberately discarding a working vessel), different consequences (no wreckage frame, no lifecycle moment), different mental model (deliberate cleanup vs. accepting destruction). Banked here so `[E]` doesn't accidentally generalise without a real UX pass. Sources: `docs/adr/0004-crashed-landed-lifecycle.md` §219; `docs/v0.11-plan.md` §Deferred line 968.

### Vestigial Surface Contact bucket delete
<!-- llm-parse: id=surface-contact-vestigial-delete status=shipped shipped-in=v0.12.0 origin=adr-0004 -->
✓ **shipped in v0.12.0** (Slice 4(d)). Deleted the vestigial third outcome (`outcomeNone`) from the `surfaceArrivalOutcome` enum in `internal/sim/lifecycle.go` — `classifySurfaceArrival` only ever returned Landed or Crashed, so the zero-V/neither-flag bucket was dead. Verified before deleting: the enum value had no callers beyond its declaration, and `TestImpactorTrajectoryHitsSurfacePredicate` already pins that every contact resolves to one of {Landed, Crashed}. CONTEXT.md "Surface Contact" glossary + the "Landed" Flagged-ambiguities entry updated to two outcomes (the latter was also stale pre-v0.11.4 — reconciled to ADR 0004 shipped reality). **Historical scope below.** After v0.11.4 every surface contact resolves to either Landed (predicate qualifies: `CanSoftLand` + `|V| < V_CRIT` + nose alignment) or Crashed (anything else). The third "Surface Contact" placeholder bucket is reachable only theoretically; v0.11.4 kept it as a defensive fallback. Delete after the v0.11.x playtest cycle confirms no qualifying-but-not-Crashed contacts emerge. Source: `docs/adr/0004-crashed-landed-lifecycle.md` §197.

### 2-stage Lander (Descent + Ascent split)
<!-- llm-parse: id=lander-2-stage status=shipped shipped-in=v0.12.3 target=v0.12 weight=M-L origin=v0.11.5-grill adr=0007 -->
✅ **shipped · v0.12.3 · ADR 0007**. Split into `lander-descent` + `lander-ascent` with a surface-staging event; the abandoned descent stage is a Landed passive Vessel, and an explicit per-loadout Decouple Plan (`[1,1,1,2]` for the Apollo Stack) extracts the 2-stage LM as one craft. Full arc playtest-verified (descent → surface stage → ascent → CSM rendezvous). _Original backlog note:_ The real Apollo LM was two vehicles: an ascent stage that launched *from* the descent stage on the surface, leaving the descent stage as discarded hardware on the lunar surface. v0.11.5's Lander silhouette polish (per-stage width, landing legs, engine bell, hypergolic flame) keeps the Lander as a single catalog stage. A v0.12+ catalog enrichment would split it into two stages (`lander-descent` + `lander-ascent`) and add a **surface staging event** — decouple the ascent stage from the descent stage *while Landed*, leaving the descent stage as a Landed wreck on the surface and the ascent stage as the player's active craft for return-to-lunar-orbit. This is a *gameplay* extension, not a visual one: the player gains a new lifecycle moment between Touchdown and Re-launch. The v0.11.5 1-stage Lander's visual differentiation (legs + bell + hypergolic flame, per `CONTEXT.md` "Launch Sprite") already pulls its weight for v0.11.5; the split is about replicating the authentic Apollo flow (Eagle's descent stage left on the Moon, ascent stage returns to dock with the CSM). Source: `docs/v0.11-plan.md` §Deferred (added during the v0.11.5 grill).

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
<!-- llm-parse: id=gravity-turn-assist status=resolved target=v0.9.4 reopened-from=v0.9-plan-decision-7 -->
✓ **resolved in v0.9.4** with neither (a) nor (b). The two options
on the table at v0.9.2 retrospective were (a) target pitch-vs-
altitude HUD overlay or (b) autopilot toggle. v0.9.4 transplanted
v0.9.3's rendezvous design language onto launch instead — live
predictive numbers (ap, pe, Δv→circ) + threshold callout (ORBIT
READY) + frame auto-routing (NavSurface auto-snap on launchpad
spawn) + single-key circularize (`C`). The KSP recipe (tip 10°,
hold surface-prograde, ride the gravity turn) was already
realisable with v0.9.2 primitives + v0.9.3 NavMode; what was
missing was the live KSP-style instruments to fly it by. Adding
those instruments closes the loop without the autopilot route.

#### Cross-view rotation parity in orbit-flat
<!-- llm-parse: id=cross-view-rotation-parity status=open-question target=v0.9.6-plus -->
📐 **open · v0.9.6+ polish**. v0.9.2 fixes Landed-craft visual
position to match the renderer's tilted-axis sub-observer point in
the default top view, but orbit-flat falls back to a static basis
because the perifocal frame co-rotates with the body for Landed
craft. Cross-view consistency in the texture pipeline (so a
launchpad spawn lines up the same way regardless of view) is
deferred polish.

#### Pitch trim fine resolution
<!-- llm-parse: id=pitch-trim-fine-resolution status=open-question target=v0.9.6-plus -->
📐 **open · v0.9.6+ polish**. v0.9.2.1 bumped pitch trim step from
5° → 10° because the original required 6+ key presses for an initial
pitch-over. 10° is reasonable for the first few degrees but mid-
ascent fine-tuning at 1° resolution would help. Should `>` / `<`
repeat-accelerate (hold-to-tilt-faster), expose a numeric input, or
take a Δ argument? Pick at v0.9.6+ if the gravity-turn assist
doesn't subsume manual trim entirely.

---

## Upcoming — v0.9 cycle plans

<!-- llm-parse: cycle=v0.9 status=in-progress -->

**Cycle theme: "the craft fleet grows up."** Plan committed at
[`docs/v0.9-plan.md`](v0.9-plan.md); first two slices (v0.9.0
targeting + v0.9.1 staging) shipped 2026-05-05. v0.9.2 ground-
launch primitives shipped on PR #51, then closed out by v0.9.4
ascent ergonomics (live LAUNCH HUD instruments + ORBIT READY +
NavSurface auto-snap + `C` plants circularize) — pad-to-LEO is
playable. v0.9.3 rendezvous and v0.9.5 navball remain on the slate;
both operate on already-orbiting craft.

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
| 4 | [Combined plane-shift + Hohmann](#combined-plane-shift--hohmann) | L | 🧊 spec-committed (v0.11) | Eccentric-aware departure + plane change folded into the transfer (constrained Lambert). v0.10.1 shipped a non-blocking advisory guard only; full spec committed. |
| 5 | [Rendezvous tooling](#rendezvous-tooling) | M | 🧊 backlog | Target-craft selection + target-relative burn modes + null-v_rel at closest approach + iteration. Pairs with multi-craft fleet from (1). |
| 6 | [Solar lighting + terminator + eclipses](#solar-lighting--daynight-terminator--eclipses) | M | ✅ shipped v0.9.6 | Landed `internal/render/lighting.go`+`eclipse.go`; closed the v0.9 cycle (merge 32e8d03). |
| 7 | [Predictor adaptive sampling](#predictor-adaptive-sampling) | M | 🛠 in progress v0.10.3 | Three-cycle carry-over; foundation shipped v0.8.4. Adaptive per-leg sample budget (`adaptiveSampleCount`, `[96,720]`). |
| 8 | [Multi-rev porkchop UI](#multi-rev-porkchop-ui) + [Lambert short/long picker](#lambert-shortlong-branch-picker) | S | 🛠 in progress v0.10.5 | UI for `LambertSolveRev` (nRev + retrograde + short/long) via the porkchop `o` sub-menu + `TransferOptions` plumbing. Branch `v0.10.5-porkchop`. |
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
