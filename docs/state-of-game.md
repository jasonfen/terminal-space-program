# terminal-space-program — state of game

<!--
  meta:
    snapshot_version: v0.8.6
    snapshot_date: 2026-05-04
    archive: docs/state-of-game-archive.md
  Read the archive for the full v0.7.6-baseline-plus-v0.8-additions
  detail this rewrite condensed. This file is the canonical
  "what's the game today / where is it going" reference.
-->

> Snapshot at **v0.8.6** (May 2026). Predecessor doc with full
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
craft fleet up through v0.8.6 is intentionally modest; staging
slices in v0.9+ will grow it.

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
⏸ **deferred from v0.8.6 (c) → v0.9**. `LambertSolveRev` + retrograde flag have been library-ready since v0.7.5; UI not sliced. Defer until staging slices grow craft fleet — current chemical S-IVB-1-class fleet always picks nRev=0 prograde, so UI gives no leverage until that changes. Open scoping question: Lambert short/long branch picker for nRev≥1 travels with this slice.

### Wider cross-SOI PlanTransfer
<!-- llm-parse: id=cross-soi-transfer status=backlog target=v0.9 -->
🧊 **backlog · target v0.9**. v0.5.7's `PlanIntraPrimaryHohmann` covers same-parent (LEO → Luna); v0.6.3 covers moon → parent. The remaining direction — heliocentric → moon-of-other-planet (Phobos from a Mars approach, a Galilean from a Jupiter cruise) — needs a real patched-conic capture pass through both SOIs.

### Combined plane-shift + Hohmann
<!-- llm-parse: id=plane-shift-hohmann status=backlog target=v0.9 -->
🧊 **backlog · target v0.9**. Lambert solver constrained on post-capture inclination so departure geometry lands prograde at the destination. Substantial — needs the Lambert constraint.

### Capture-direction toggle
<!-- llm-parse: id=capture-direction-toggle status=backlog target=v0.9 -->
🧊 **backlog**. Today's auto-Hohmann arrival burn is retrograde-in-source-frame. A "capture prograde-around-target" mode would burn differently and trade ~50–100 m/s for the right-direction capture.

### Drag-to-edit nodes
<!-- llm-parse: id=drag-to-edit status=deferred -->
⏸ **deferred**. v0.6.4 deliberately picked click-to-edit-replace. Reopen if playtest feedback says scrubbing is worth the implementation cost.

### Predictor adaptive sampling
<!-- llm-parse: id=predictor-adaptive-sampling status=backlog -->
🧊 **backlog**. Fixed 96-sample horizon collapses to a smear at 10000× warp on LEO orbits. v0.8.4's time-aware `propagateStateWithPrimary` foundation work unlocks this; the slice itself didn't ship in v0.8.

### Solar lighting + day/night terminator + eclipses
<!-- llm-parse: id=lighting-terminator-eclipses status=backlog target=v0.9 -->
🧊 **backlog · target v0.9 (research-first)**. Sub-solar-point per body per tick → `cos(angle to sun)` shading; eclipses fall out for free if lighting lands. Research item: investigate canvas-level ANSI 24-bit per-cell mixing as a `lipgloss` workaround before slicing.

### Staging chain
<!-- llm-parse: id=staging-chain status=backlog target=v0.9 -->
🧊 **backlog · target v0.9**. Ground launch → orbit → ICPS / S-IVB / lander staging chain so the craft fleet has more than one tier of capability. Unblocks (c) multi-rev porkchop and the rendezvous tooling slice (more craft → more practical scenarios).

### Multiplayer implementation
<!-- llm-parse: id=multiplayer status=planning target=v0.9-stretch -->
📐 **planning** *(`docs/multiplayer-design.md` v0.6.6)*. WebSocket transport, host-authoritative authority, warp-arbitration rule. Not slated for v0.9 directly but the design doc is current.

### N-body perturbations
<!-- llm-parse: id=n-body status=backlog target=v0.10+ -->
🧊 **backlog · target v0.10+**. Lagrange points, J2, third-body acceleration. Major architectural change — Kepler-warp-lock fast path retreats to RK4 + Verlet.

### Multi-system spacecraft
<!-- llm-parse: id=multi-system-craft status=backlog target=v0.10+ -->
🧊 **backlog · target v0.10+**. Two paths sketched: (A) real interstellar transfer math (~50,000 yr at chemical Δv, needs a propulsion abstraction), or (B) deus-ex jump drive. Today the craft is locked to Sol; `CycleSystem` only changes the camera.

### Theme-file hot-reload
<!-- llm-parse: id=theme-hot-reload status=deferred -->
⏸ **deferred**. ~200 LOC of fsnotify; never surfaced as a v0.8 playtest pain.

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

---

## Upcoming — v0.9 cycle plans

<!-- llm-parse: cycle=v0.9 status=planning -->

**Cycle theme (working title): "the craft fleet grows up."**

The v0.8 cycle delivered multi-craft capability and the precision
tooling (RCS, docking, drag, body-equatorial frame, adaptive warp)
to support it — but the *fleet itself* is still one chemical
S-IVB-class stage and three minor variants. v0.9 is provisionally
structured around growing the fleet (staging) and the operational
tooling that becomes useful once you have multiple capability
tiers in flight (rendezvous, multi-rev transfers, mission
scripting properly designed).

### Provisional slice candidates

In rough priority / dependency order. **None of these are
committed slices** — they're planning-mode candidates pending an
explicit v0.9 plan doc.

| Order | Slice | Status | Notes |
|---|---|---|---|
| 1 | [Staging chain](#staging-chain) | 🧊 backlog | Ground launch → LEO → ICPS → lander chain. Unblocks practical use of (2)–(4). |
| 2 | [Rendezvous tooling](#rendezvous-tooling) | 🧊 backlog | Target-craft selection + null-v_rel at closest approach. Pairs with multi-craft fleet from (1). |
| 3 | [Multi-rev porkchop UI](#multi-rev-porkchop-ui) | ⏸ deferred | UI for `LambertSolveRev` (nRev + retrograde + short/long). Library-ready since v0.7.5. |
| 4 | [Mission scripting](#mission-scripting--editor) | ⚠ rolled back | **Design-pass first**, then re-implement. Reference v0.8.7-attempt artifacts only for implementation shape. |
| 5 | [Wider cross-SOI PlanTransfer](#wider-cross-soi-plantransfer) | 🧊 backlog | Heliocentric → moon-of-other-planet patched-conic capture. |
| 6 | [Combined plane-shift + Hohmann](#combined-plane-shift--hohmann) | 🧊 backlog | Lambert constrained on post-capture inclination. |
| 7 | [Solar lighting + terminator + eclipses](#solar-lighting--daynight-terminator--eclipses) | 🧊 backlog | Research-first — ANSI 24-bit canvas mixing investigation precedes slicing. |

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
