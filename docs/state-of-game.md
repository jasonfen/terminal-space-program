# terminal-space-program — state of game

*Snapshot at v0.8.6 (May 2026) — v0.8 cycle complete (RCS v0.8.0,
multi-craft v0.8.1, craft types + capture preview v0.8.2, docking
v0.8.3, atmospheric drag v0.8.4, sim-time rotation + view-aware
textures v0.8.5, controls polish + body-equatorial frame + adaptive
warp clamps + iterate-for-target toggle v0.8.6). Updated at each
minor / patch boundary; the §1 "what works today" detail trails
the headline by a slice or two.*

`docs/plan.md` is the original architecture / phase plan. This doc complements it
with a "what plays today, what's queued next" view organised around player-facing
features and the version sequence that delivers them.
`docs/v0.5-release-notes.md` covers the v0.5.0 → v0.5.15 release series patch
by patch — this doc is the snapshot, those are the release notes.

---

## 1. What works today (v0.8.6)

### Physics
- Two-body patched-conic propagation with **SOI-aware** state
  transitions. Crossing a sphere of influence rebases state into the
  new primary's frame and switches μ for subsequent steps.
- Symplectic **Verlet** for free flight (energy-conserving within
  ~1e-7 % over 1000 orbits at LEO). **RK4** on the active-burn path so
  non-conservative thrust forces integrate cleanly.
- **Drag-aware Verlet** (v0.8.4+): `physics.StepVerletWithAccel`
  takes an additional acceleration closure so the live integrator,
  `propagateStateWithPrimary`, the predictor, and `stepThrust` all
  pick up atmospheric drag inside Earth + Mars atmospheres without
  divergent code paths. Atmosphere model is exponential
  ρ(h) = SurfaceDensity · exp(−h/ScaleHeight) up to a cutoff
  altitude (Earth: 8500 m / 100 km; Mars: 11100 m / 80 km), with
  `v_rel = v − ω × r` so corotating air dictates drag direction.
  Surface clamp on aerobrake impact (`physics.ClampToSurface`).
- **Body-equatorial reference frame** (v0.8.6.1+) for body-bound
  Keplerian orbits. `orbital.ReferenceFrameForPrimary(primary)`
  returns identity for the Sun (heliocentric / ecliptic) and the
  body-equatorial basis for everything else (ECI for Earth, MCI for
  Mars, etc.). Inclination, Ω, ω are quoted in this frame everywhere
  the player sees them — orbit screen, maneuver projected-orbit,
  capture preview, planner targets. A 0° Earth orbit physically
  passes over the equator (Ecuador), not the world ecliptic plane
  which intersects Earth at ~23°N because of axial tilt.
- **Stumpff-universal-variables Lambert solver** (Curtis Algorithm
  5.2) with explicit retrograde flag and N-rev branches plumbed
  through `LambertSolveRev(..., nRev, retrograde)` — UI surface for
  multi-rev / retrograde transfers is library-ready but not yet
  wired (deferred to v0.9 alongside staging).
- **Hohmann transfer** math + **patched-conic v∞ → Δv** identity
  for departure / capture burns.
- **Planet rotation in sim time**. `bodies.CelestialBody` carries
  `TidallyLocked` + `AxialTilt` + `AxialAzimuth` (v0.8.5+).
  `render.SubObserverPointDeg(b, simTime, camDir, primMer)` returns
  (subLat, subLon) at the visible disk centre; tidally-locked moons
  point their primary-meridian at the parent body, free bodies use
  sidereal rotation. `Clock.RotationTime` advances at
  `min(warp, 10000×) × BaseStep` so high-warp doesn't blur surfaces
  into stripes.

### Time-warp clamping (v0.4.3 → v0.8.6.2)

Three layered guards keep the integrator stable at high warp.
Smallest of all four caps wins.

- **Orbital-period sub-step cap** (v0.4.3 baseline). The Verlet
  integrator runs sub-steps of `period/100`; the warp clamp
  enforces ≤1024 sub-steps per tick, so `maxWarp ≈
  (1024 · period/100) / BaseStep`. Keeps temporal resolution
  proportional to orbital dynamics.
- **Active-burn cap (10×)**. While `ActiveBurn != nil` or
  `ManualBurn != nil` on any craft, warp pins to ≤10× so burn
  completion + thrust integration stays resolved.
- **Throttle-change cap (v0.8.6.2)**. `Spacecraft.LastThrottleChangeAt`
  records sim-time on actual-value changes in `SetThrottle`;
  `clampedWarp` pins to 10× for 1 sim-second after any craft's
  throttle moved. Catches high-warp throttle ramps that alias the
  integrator the same way held burns do.
- **Upcoming-node approach cap (v0.8.6.2)**. `soonestUpcomingNodeIn`
  scans every craft's resolved future TriggerTime; warp ramps
  continuously down as the node nears via
  `maxWarp = secondsUntilNode / (10 × BaseStep)`, floored at 1×. At
  5 s out the cap reaches 10× and dovetails with the active-burn
  cap. Prevents 100,000× warp from skipping a 30-s-out node in a
  single 5000-s tick.

### Spacecraft & burns
- **Multi-craft slate** (v0.8.1+). `World.Crafts []*Spacecraft` +
  `ActiveCraftIdx`; `[`/`]` cycles active craft, `n` opens the spawn
  form. `ManeuverNode` / `ActiveBurn` / `ManualBurn` / `AttitudeMode` /
  `EngineMode` live on each Spacecraft so a planted burn fires on
  the correct vessel regardless of which one the player is flying.
  Save schema v4 → v5 with `Craft *Craft` → `Crafts []*Craft`
  migration; pre-v5 saves auto-migrate (singular Craft → 1-entry
  slice).
- **Craft loadouts** (v0.8.2+). Four launch types in the spawn form,
  each with distinct propulsion + glyph + colour:
  - **S-IVB-1** ▲ yellow — J-2 third stage (1023 kN, Isp 421 s,
    11000 kg dry / 40000 kg fuel). Δv ≈ 6.3 km/s, comfortable for
    Luna round trips. Default first-craft loadout.
  - **ICPS** ◆ blue — RL10C-derived upper stage (108 kN, Isp 462 s);
    lower thrust, more Δv per kg, longer burns where finite-burn
    loss matters.
  - **RCS-tug** ● pink — monoprop-only proximity-ops vessel.
  - **Lander** ▼ mint — throttleable descent stage.
- **Six burn modes**: prograde, retrograde, normal±, radial±.
  Direction recomputed each sub-step from live (r, v) so held-mode
  burns track the rotating frame.
- **RCS / monopropellant mode** (v0.8.0+). `r` toggles between main
  engine and RCS. RCS pool is a separate propellant + thrust + Isp
  triple (typically ~720 kg / ~50 N / ~220 s for the S-IVB-1 base);
  `b`-tap or attitude-key tap delivers a ~0.1 m/s pulse from the
  monoprop tank. ~30 m/s of RCS Δv on the default vessel —
  proximity-ops budget for docking. EngineMode persists per-craft.
- **Per-thruster RCS visual** (v0.8.3+): puff trail along the active
  attitude direction; main-engine flame visual replaces the v0.8.0
  placeholder during a finite burn.

### Docking + undocking (v0.8.3+)

- **Proximity-gated DockCrafts** at <50 m and <0.1 m/s relative
  velocity. Mass-weighted centroid + momentum-conserving fuse
  picks the composite's new (R, V); propellant pools sum across
  components.
- **DockedComponents** preserves the original-craft identities
  through fusion so `U` undock can split back along the original
  partner boundaries with proportional propellant.
- **RENDEZVOUS HUD** lights up when two craft are within docking
  distance: live range / |v_rel| / DOCK READY indicator.
- **Alongside-spawn**: `n` form's "POSITION = alongside active"
  drops a sister craft inside the docking gate at matched velocity
  for proximity-ops practice without a full rendezvous.

### Planning
- **Manual planner** (`m`): five-field form (mode / fire-at / Δv /
  throttle / iterate). `Tab` cycles, `←/→` cycles modes / events /
  iterate, space toggles iterate, digits edit Δv / throttle. Burn
  duration is **derived** from Δv via the rocket-equation form
  `t = (m₀/ṁ)·(1 − exp(−Δv/(Isp·g₀)))` — pre-v0.6.5 the form
  exposed Δv AND duration as independent inputs, but at fixed
  thrust + mass the two are the same dial. Live PROJECTED ORBIT
  block: apo/peri/AN/DN/inclination of the resulting orbit, rebased
  into the burn's target primary frame; the canvas dashed shadow
  trajectory + the form readout both feed off
  `World.PreviewBurnState`, which propagates to the fire-at event
  point before applying Δv.
- **Iterate-for-target toggle** (v0.8.6.3, the form's 5th field).
  Off by default. When on, the app routes the commanded Δv through
  `World.IterateBurnDV(mode, dv)` before plant —
  `planner.IterateForTarget` Newton-iterates against an RK4 finite-
  burn simulation to refine Δv so the post-burn apsides match what
  an impulsive Δv at the same value would have delivered (target
  picked from mode: Prograde/Retrograde → apoapsis, Radial± →
  periapsis, Normal± → no-op). Falls back to commanded Δv on
  iteration failure. Hohmann auto-plant has used this iterator
  internally since v0.6.2; the toggle exposes it for player-planted
  burns.
- **Event-relative trigger nodes** (v0.6.0+): `fire at` selects
  Absolute T+ or `next peri / next apo / next AN / next DN`. Lazy-
  freeze resolver in `World.Tick` computes `TriggerTime` against
  the body-equatorial frame on the first tick after plant, then
  freezes — body-equatorial means AN/DN are the body's actual
  equator crossings, not world-XY. Equatorial / hyperbolic /
  unreachable inputs leave the node unresolved; the resolver
  retries each tick.
- **`F5` quicksave / `F9` quickload** (v0.8.6+, KSP-style); replaces
  the v0.4.0 `S` / `L` keys.
- **`Ctrl+D` / `Ctrl+K`** (in `m` form, v0.8.6+): per-node delete /
  clear-all-nodes. Replaces the global `N` keybinding (case-collided
  with `n` SpawnCraft).
- **`H` auto-plant Hohmann transfer**: select target body, one
  keystroke plants two finite nodes (origin-frame departure +
  destination-frame arrival), each refined through
  `IterateForTarget`. Frame-aware via `ManeuverNode.PrimaryID`.
  Phase correction (v0.5.9) waits for the next launch window so the
  craft actually rendezvous with the target.
- **`P` porkchop plot**: ASCII heatmap over departure-day × time-of-
  flight. Cursor navigates cells, snaps to min-Δv on open. **Enter
  on a feasible cell plants that Lambert transfer**. Single-
  rev / prograde-only today; multi-rev + retrograde UI deferred to
  v0.9.
- **`R` refine plan** (v0.4.1): re-runs Lambert from the craft's
  live heliocentric state to the pending arrival node's target,
  plants a prograde / retrograde mid-course correction, replaces
  the arrival burn's Δv with the refined capture.
- **`I` plane match**. With no body cursor → drop to body-
  equatorial 0°. With a body selected →
  `orbital.PlaneMatchInclination(b, frame)` converts the target's
  heliocentric orbit normal into the primary's reference frame and
  plants a single normal±-burn at the next AN/DN to match. From
  LEO, "match Mars" returns ~23.4° (Earth-tilt-dominated);
  heliocentric collapses to the body's ecliptic-relative i.

### Rendering (orbit canvas)
- **Adaptive body sizing**: bodies render at true scale when
  `radius × scale ≥ 4 px`, capped at 512 px (v0.8.4+ raised from 64
  so atmospheric haze can render at the full disk). Tiered fallback
  for small bodies. System primary uses a hollow ring + filled
  centre to distinguish from planets; v0.8.5+ replaces the Sun's
  ring with a textured disk + corona halo.
- **Per-pixel body textures** (v0.8.5+). At `r ≥ 12 px`, bodies
  render through `Canvas.FillTexturedDiskTagged`. View-aware
  inverse-orthographic projection (Snyder §20) maps screen (dx, dy)
  to body-frame (lat, lon) using the sub-observer point so
  ViewTop on tilted Earth reveals the Arctic, Uranus rolls pole-on
  along its orbit, Saturn's polar hexagon stays at +78°N regardless
  of view, ViewOrbitFlat picks up the canvas's depth axis. Coverage:
  - **Sun**: limb-darkened solar disk + sunspots + corona halo.
  - **Earth**: polygon-rasterised 144×72 continental mask
    (continents + key islands like UK / Iceland / Italy /
    Madagascar / Cuba / Hispaniola / Sumatra / Java / Borneo /
    Sulawesi / New Guinea / Philippines / Tasmania / NZ + deserts +
    polar ice). Biome-shaded land (tropical / temperate / boreal)
    by `|lat|`, atmospheric blue-marble limb tint.
  - **Moon**: canonical near-side maria (Crisium, Tranquillitatis,
    Imbrium, Procellarum, etc.) + bright rayed-crater accents
    (Tycho, Copernicus, Kepler) + far-side / polar detail (Mare
    Orientale, Moscoviense, Ingenii, South Pole-Aitken basin).
    Tidally-locked override always shows near-side.
  - **Mars**: rust base, Syrtis Major / Solis Lacus / Acidalia /
    Mare Cimmerium / Mare Erythraeum dark albedo, Arabia Terra +
    Hellas bright regions, polar caps.
  - **Jupiter**: 10-band SPR/STB/STrZ/SEB/EZ/NEB/NTrZ/NTB/NTZ/NPR
    alternating zones/belts + Great Red Spot.
  - **Saturn**: cloud bands + polar hexagon + tilted four-band
    ring system (C / B / Cassini Division gap / A / F),
    foreshortening per view (~89% top, ~45% side, edge-on
    perpendicular to tilt).
  - **Galileans** (Io / Europa / Ganymede / Callisto), **Uranus**
    (subtle banding), **Neptune** (banded + Great Dark Spot).
- **Vessel orbit ellipse**: live Keplerian orbit drawn dotted
  (stride 3) in the craft's primary frame. Hyperbolic / degenerate
  orbits skipped. Hidden when `apoapsis × scale < minOrbitPixels`
  so heliocentric zoom doesn't render LEO orbits as a one-cell blob
  over the parent body.
- **Apo / peri markers** at ν=0 / π. **Vessel marker**: 5-pixel
  chevron oriented along velocity, swaps to a single bright disk at
  sub-orbit zoom.
- **Per-leg colored trajectory preview** (v0.6.1+). Each planted
  node's post-burn orbit renders in its own colour from a 4-cycle
  palette (cyan / mint / amber / pink); marker clusters take the
  matched colour. Frame-aware: legs planted in destination frames
  predict from there.
- **Per-craft glyph + colour** (v0.8.2+): each loadout has its own
  marker so multi-craft slates read at a glance.
- **Atmospheric haze ring** (v0.8.4+): faint ring at
  `cutoff + scale-height` in `atm.Color` shows where drag becomes
  non-negligible. Body disk grows to canvas reach so altitude-0
  reads as surface; landed craft trigger a zoom cap so altitude-0
  stays visible.
- **Camera focus** (`f`/`F`/`g`): system-wide / each body / craft.
  FocusCraft auto-fits to ~3× current altitude; terminal moons
  (no children orbiting them) zoom to 8× radius on focus so surface
  texture is visible by default.

### HUD
- Clock + warp + paused indicator. **Effective warp** is shown
  alongside the selected warp when the four-cap clamp engages.
- Focus block: focused-target name + permanent **VIEW** sub-line.
- Vessel block (per active craft): name, primary, altitude,
  velocity, apoapsis, periapsis, inclination (body-equatorial),
  plus **PERIAPSIS BELOW SURFACE** alert when periapsis altitude
  goes negative.
- Propellant block: fuel + monoprop, total mass, Δv budget
  remaining (rocket equation).
- Active-burn block (when in flight): mode, Δv-to-go, T-remaining.
- Planned nodes: per-craft list, mode / Δv / time-to-fire / event
  label / impulsive vs finite tag. Multi-craft slates list every
  craft's nodes simultaneously. Clickable rows for direct edit
  (v0.8.2+).
- **Projected orbit** (v0.6.1+, body-equatorial frame v0.8.6.1+):
  apo / peri / AN / DN / inclination of the chained post-burn
  orbit, rebased into each node's intended PrimaryID before
  applying its Δv.
- **CAPTURE PREVIEW** (v0.8.2+): predicted relative approach
  speed + qualitative direction (prograde / retrograde) for
  Hohmann arrivals. v0.8.4's time-aware predictor unlocks exact
  arrival inclination for typical Hohmanns; v0.8.6.1 reads
  inclination in the destination body's equatorial frame.
- **RENDEZVOUS HUD** (v0.8.3+, when ≥2 craft are close): live
  range / |v_rel| / DOCK READY indicator.
- **FRAME TRANSITION** (v0.7.6+): upcoming SOI / frame change for
  the next planted node, via `World.NextFrameTransition`.
- **Mission** block (clickable `[Missions]` button in the title
  bar opens a dedicated screen with status glyphs ✓/✗/·).
- Selected body block: name, type, semimajor axis, eccentricity,
  period, plus Hohmann preview when applicable.

### Missions (v0.6.5+)
- `internal/missions` package: typed predicate machine over the
  spacecraft's (primary, state, sim-time) tuple. Three predicate
  kinds:
  - `circularize` — craft is in the named primary's frame, orbit
    bound, eccentricity ≤ cap, semimajor axis within ±tol of
    `radius + altitude_m`.
  - `orbit_insertion` — craft is in the named primary's frame on a
    bound orbit (e < 1).
  - `soi_flyby` — any tick where the craft's current primary ID
    matches the named body.
- Three-state machine (`InProgress` → `Passed` | `Failed`) with
  sticky terminal states. Embedded starter catalog: "Circularize
  at 1000 km LEO" (e ≤ 0.005, ±5% on `a`), "Luna orbit insertion",
  "Mars SOI flyby". `World.Missions` evaluated each Tick after
  `executeDueNodes` so a circularization passes on the same tick
  the burn ends.

### Body hierarchy & moons (v0.5.0+)
- `bodies.Body.ParentID` enables arbitrary-depth `parent → child`
  refs. Empty ParentID = top-level body (orbits the system primary).
- Recursive position / velocity: moon = parent's inertial position +
  moon's parent-relative position; same for velocity.
- `physics.FindPrimary` uses each body's actual parent for SOI
  sizing so nested-SOI walks pick the innermost containing body
  correctly.
- Moon catalog: Luna, Phobos, Deimos, the four Galileans (Io,
  Europa, Ganymede, Callisto), Titan, Enceladus.
- Transfer planning to/from moons is **shipped both directions**:
  same-primary intra-primary transfers (LEO → Luna, both around
  Earth) via `planner.PlanIntraPrimaryHohmann` (v0.5.7) with phase
  correction (v0.5.9); the reverse — craft inside a moon's SOI
  returning to its parent (Luna → Earth) — via
  `planner.PlanMoonEscape` (v0.6.3): bound transfer ellipse with
  apolune at the moon's SOI radius, zero-Δv frame marker at SOI
  exit, player plants their own circularization. Wider inter-SOI
  capture (heliocentric → moon-of-other-planet) deferred to v0.9.

### Systems loaded
- **Sol** (playable — craft spawns here).
- **Alpha Centauri**, **TRAPPIST-1**, **Kepler-452** (viewable;
  craft does not yet move between systems).
- **User overlay** (v0.7.0+): JSON files in
  `$XDG_CONFIG_HOME/terminal-space-program/systems/*.json` merge
  with the embedded set. User files win on `systemName` match
  (e.g. dropping a `sol.json` replaces the embedded Sol entirely);
  otherwise they append. Body-info screen (`i`) tags the source so
  the player can tell which catalog a body came from. Malformed
  user files print a warning to stderr and are skipped; embedded
  systems always load.
- **Theme overlay** (v0.7.2+): `theme.json` overrides UI palette
  vars + per-body colours. Loaded at startup; hot-reload deferred.

### Persistence + distribution
- **Save / load** (`F5` / `F9`): JSON state file at
  `$XDG_STATE_HOME/terminal-space-program/save.json`. Schema v5
  round-trips clock, focus, the entire craft slate (each craft's
  RCS pool, planted nodes with per-node throttle, in-flight burn,
  attitude, engine mode), and missions. Pre-v5 saves auto-migrate.
  Save header carries a `body_catalog_hash` so saves reject when
  the body catalog changes between sessions.
- **GoReleaser** matrix: linux + darwin amd64/arm64, windows
  amd64. `CGO_ENABLED=0`, `-ldflags "-s -w"` static binaries.
  Four-part patch tags (`vX.Y.Z.N`) bypass the release workflow so
  checkpoint markers don't fail CI; only strict SemVer
  `vMAJOR.MINOR.PATCH` tags trigger a release build.
- **CI**: `go test ./...` on every PR.

---

## 2. Backlog

### v0.8+ candidates

Organised by theme. Each item is a sketch — none of these are
committed slices; specifics shake out in `docs/v0.8-plan.md` when
that gets drafted.

**Networking & multi-instance**

- **Multiplayer implementation** (the v0.6.6 design's MVP).
  WebSocket transport, host-authoritative authority,
  warp-arbitration rule (`warp > 1×` requires both peers'
  active-burn count == 0), `Session` block inside `Payload` at
  schema v4 → v5 when the real implementation lands. Tentative
  picks documented in `docs/multiplayer-design.md`.

**Multi-craft + RCS / docking**

- ~~**Multi-craft control selector**~~ ✓ shipped in v0.8.1
  (`World.Crafts []*Spacecraft`, `[`/`]` cycle, schema v4→v5).
- ~~**Monopropellant / RCS mode**~~ ✓ shipped in v0.8.0.
- ~~**Docking model**~~ ✓ shipped in v0.8.3 (proximity-gated
  DockCrafts at <50 m / <0.1 m/s, mass-weighted centroid +
  momentum-conserving fuse, RENDEZVOUS HUD).

**Planner UI**

- **Multi-rev porkchop UI** *(re-deferred from v0.8.6 (c) →
  v0.9)*. Library ready since v0.7.5; UI not sliced. Pinned to
  v0.9 alongside staging slices — current chemical-stage fleet
  always picks nRev=0 prograde so the UI gives no leverage until
  the craft fleet grows.
- ~~**Caller-facing `IterateForTarget` toggle**~~ ✓ shipped in
  v0.8.6.3 — `iterate` cycle field in the `m` form.
- **Drag-to-edit on planted nodes**. Click-drag a node marker
  on the canvas to scrub Δv / fire-time in place, instead of
  click-to-edit-replace. v0.6.4 deliberately skipped this in
  favour of click-only selection. Still deferred through v0.8.
- **Wider cross-SOI `PlanTransfer`**. v0.6.3 covers moon →
  parent (Luna → Earth); the inverse — heliocentric → moon-of-
  other-planet (Phobos from a Mars approach, a Galilean from a
  Jupiter cruise) — needs a real patched-conic capture pass
  through both SOIs. Did not ship in v0.8.
- **Real rendezvous tooling** *(opened post-v0.8.3 docking;
  promoted to its own §6 subsection)*. Target-craft selection,
  target-relative prograde / retrograde burn modes, null v_rel
  at closest approach, iterative refinement. See §6 *Rendezvous
  tooling* for foundations needed.

**Body rendering polish**

- ~~**Sim-time rotation**~~ ✓ shipped in v0.8.5 with view-aware
  projection (Snyder §20 inverse-orthographic with arbitrary
  sub-observer point); rotation rate capped at 10000× warp via
  `Clock.RotationTime`.
- **Solar lighting + day/night terminator** *(deferred to v0.9)*.
  Sub-solar-point per body per tick → `cos(angle to sun)`
  shading. Research-first: investigate canvas-level ANSI 24-bit
  mixing as a `lipgloss` workaround before slicing.
- **Eclipses** *(deferred to v0.9)*. Falls out of solar lighting
  if it lands.
- ~~**More bodies textured**~~ ✓ shipped in v0.8.5 (Saturn with
  tilted ring system, Galileans, Uranus, Neptune, Sun with
  limb-darkening + sunspots + corona, refined Earth + Moon).

**Multi-system spacecraft**

- **Interstellar transfer math** OR **deus-ex jump drive**.
  Today the craft is locked to Sol; `CycleSystem` only changes
  the camera. The simpler unlock — a ⟨jump⟩ action that warps
  the craft to a target system's primary orbit — gives the
  system-cycle UX without solving the hard cruise problem.

**Modding extensions**

- **`bodies.json` sibling overlay**. v0.7.0's catalog loader
  takes whole-system files; a per-body overlay would let users
  tweak orbital elements / radius / GM for individual bodies
  without redefining the whole system.
- **Theme-file hot-reload**. v0.7.2's `theme.json` loads at
  startup only; watching the file would let players iterate on
  colour without restarting.
- **Mission editor / scripting**. User-authored objectives
  beyond the three predicate kinds. Needs a config format —
  declarative DSL? Embedded expression language (CEL / Lua)?
  Builds on the v0.7.0 catalog loader pattern.

**Physics extensions**

- ~~**Optional simple atmospheric drag**~~ ✓ shipped in v0.8.4
  (exponential ρ(h) for Earth + Mars, drag-aware Verlet,
  surface-clamp on aerobrake impact).
- **N-body perturbations** *(still deferred to v0.9+)*. Lagrange
  points, three-body trajectories. Major architectural change —
  the Kepler-warp-lock fast path can't survive without a
  re-think. See §6 *Foundations beyond v0.8* for the
  infrastructure sketch.
- **Predictor adaptive sampling at high warp**. The fixed
  96-sample horizon collapses to a smear at 10000× warp on LEO
  orbits. Adaptive sampling (density inversely proportional to
  warp) is the obvious fix. Did not ship in v0.8 despite v0.8.4's
  time-aware `propagateStateWithPrimary` foundation work
  unlocking it.

**Tooling**

- **Race-detector CI**. Currently no `-race` because the local
  toolchain doesn't ship cgo; CI could enable it with
  `CGO_ENABLED=1` and a parallel-tests pass.
- **Lambert multi-branch selection**. Today the multi-rev path
  returns the first root the bracket finds; a per-N "short" /
  "long" flag would expose both branches per rev count. Pairs
  with the multi-rev porkchop UI item above.

### From v0.3 testing — small polish items
- **Lambert multi-branch selection**. Today the multi-rev path returns the
  first root the bracket finds (lower-z side); a per-N "cheap" / "long" flag
  would expose both. Useful when porkchop multi-rev lands.
- ~~**Explicit retrograde flag for `LambertSolve`**~~ ✓ shipped in v0.7.5
  (plumbed through `LambertSolve` / `LambertSolveRev` /
  `PlanLambertTransfer` / `PorkchopGrid`). Library-only; UI toggle
  carried into v0.8 candidates above.

### Larger queued features
- ~~**Realistic finite-burn intra-primary auto-plant** (v0.6 target).~~
  Shipped in v0.6.2 as `planner.IterateForTarget` (Newton-iterates
  candidate Δv against an RK4 finite-burn simulation against
  TargetApoapsis / TargetPeriapsis residuals). Surfaces in the
  Hohmann auto-plant from v0.6.3 onward; v0.8.6.3 added the
  caller-facing `iterate (off/on)` toggle in the `m` form.
- ~~**Mission system / objectives** (v0.6.5 target).~~ Shipped in
  v0.6.5 — `internal/missions` package with three predicate kinds
  (circularize / orbit_insertion / soi_flyby), embedded starter
  catalog (1000 km LEO circularize, Luna orbit insertion, Mars SOI
  flyby) loaded via `go:embed`, World.Missions evaluated each Tick,
  save schema v2 → v3, MISSION HUD section. Mission editor /
  scripting (custom user-authored objectives) stays deferred to a
  later cycle.
- ~~**`PlanTransfer()` moon → parent extension** (v0.6.3 target).~~
  Shipped in v0.6.3 (`planner.PlanMoonEscape`, dispatch branch in
  `sim.PlanTransfer` for `target.ID == w.Craft.Primary.ParentID`).
  Wider inter-SOI capture (heliocentric → moon-of-other-planet)
  remains out of scope.
- **Multi-system spacecraft**. The craft is currently locked to Sol. Allowing
  it to enter Alpha Cen / TRAPPIST / Kepler unlocks the system-cycle UX.
  Requires interstellar transfer math (or deus-ex-machina jump for now).
- ~~**Multi-craft control selector**.~~ Shipped in v0.8.1 —
  `World.Crafts []*Spacecraft` + `ActiveCraftIdx`, `[`/`]`
  cycle keys, `n` keystroke spawns sister craft. Per-craft glyphs
  + colors landed in v0.8.2. Click-to-select on canvas is plumbed
  through the v0.8.2 hit-test pass.
- **Monopropellant / RCS mode** *(needs design — likely v0.8)*. Use
  cases: micro orbit adjustments (sub-m/s precision), encounter
  refinement, docking proximity ops. Surface notes from the v0.7.3
  manual-flight UX discussion:
  - **Separate propellant pool.** New `Spacecraft.Monoprop float64`
    (kg) alongside `Fuel`. Real spacecraft carry ~50–200 kg
    hydrazine; TSP could go ~50 kg for the S-IVB-1 default.
  - **Separate thruster profile.** `RCSThrust` ~50 N total (vs.
    main 1023 kN) at `RCSIsp` ~220 s (vs. main 421 s). Low thrust,
    low Isp, total ~6 m/s of Δv across the tank — exactly the
    regime needed for cm/s precision corrections.
  - **Mode toggle** (`r`?). HUD shows which engine is armed; main
    engine and RCS are mutually exclusive at fire time.
  - **Pulse model.** Tap an attitude key in RCS mode = ~100 ms
    monoprop pulse delivering a few cm/s. Lets you nudge orbital
    elements without busting the integrator (continuous low-thrust
    burns at high warp would be a separate problem).
  - **Reuses the existing burn-mode abstraction.** Same six
    directions (prograde / retrograde / normal± / radial±). No new
    direction logic — RCS just dispatches to a different thruster
    record at fire time.
  - **Implication for the v0.7.3 keymap.** The current WASD/QE +
    `b` layout works for both main engine (continuous burn) and
    RCS (per-tap pulse) under a single mode toggle. The `b` engage
    gate is only meaningful in main-engine mode; in RCS mode each
    attitude tap fires a pulse directly (the gate is the toggle to
    RCS in the first place).
  - **Open design questions.**
    1. **Docking model.** "Within X m at relative speed Y" → state
       transition (cheap), or full proximity-ops simulation (much
       more code)? Gates how much RCS infrastructure is worth
       building.
    2. **Encounter precision target.** What's the target Δv-budget
       precision for "encounter refinement"? Drives RCSThrust /
       Isp tuning.
    3. **Sequencing vs multi-craft.** Docking implies two craft.
       Does RCS land before, with, or after the multi-craft work?
       The v0.6.6 multiplayer doc flagged multi-craft sequencing
       as open question #1; this slots into the same conversation.
    4. **Pulse quantum.** Fixed 100 ms? Tunable per-craft? Burst
       count (tap = 1 pulse, hold = N pulses)?
- ~~**Inclination-change planner**~~ ✓ shipped in v0.7.4
  (`planner.PlanInclinationChange` + `World.PlanInclinationChange`
  + `I` keybind; v.Z-based physical AN/DN identification for
  robustness on circular orbits).
- **N-body perturbations**. The sim is strict patched-conic; Lagrangian
  points and three-body trajectories aren't representable.
  Carried into v0.8+ candidates above.
- ~~**Custom systems via config file**~~ ✓ shipped in v0.7.0
  (`$XDG_CONFIG_HOME/.../systems/*.json` overlay via
  `LoadAllWithWarnings`; user-files-win-on-name-match conflict
  policy).
- **Mission editor / scripting** (long-tail). Once basic missions exist,
  expose a config format so users can author custom objectives without
  touching Go.
- **Optional simple atmospheric drag model** (opt-in, off by default). Toy
  drag below ~150 km to enable reentry / aerobraking gameplay; patched-conic
  two-body stays the primary integrator. Previously on "excluded forever",
  softened here — only *realistic* multi-species drag stays out of scope.
- ~~**Mouse support + view-mode switcher** (v0.6.4 target).~~ Shipped
  in v0.6.4 — click-only selection on the orbit screen (vessel →
  node → body → empty-canvas → HUD priority cascade), porkchop
  cells clickable, click-to-edit on planted nodes preserves the
  scheduled fire time and replaces in place, click on empty canvas
  stages a new node at the orbit point nearest the click, plus a
  `v` hot-key cycling Top → Right → Bottom → Left → OrbitFlat with
  back-of-body occlusion in side views. Drag-to-edit, wheel-zoom,
  and free-rotation remain out of scope.

### Visual / UX targets
- **Maneuver node editing**. Nodes are plant-once today; adding drag/scrub
  on a planted node (adjust Δv, duration, fire-time in place) would make
  the planner iterative rather than plant-and-replace. v0.6 delivers
  click-to-select on a planted node (opens the `m` form pre-filled);
  drag-based in-place editing stays deferred.
- **HUD typography polish (residual)**. v0.5.13 added section dividers,
  the active-burn ● indicator, and ⚠ alerts; remaining work is alignment
  inside dense panels (vessel, propellant, planned-nodes blocks) and
  consistent column widths across sections.
- **Body rendering — open scope** *(needs discussion)*. v0.5.11
  shipped Saturn rings; v0.5.12 ships per-type identity glyphs;
  v0.7.2.1 and v0.7.2.2 add per-pixel textures for Earth (continents
  + cloud streaks) and Moon (canonical near-side maria + bright
  rayed-crater accents) via the `render.TextureFor(b, r)` dispatch
  hook + `Canvas.FillTexturedDiskTagged`. Several follow-on
  directions, all sketched and none scoped — flagged for a
  scoping pass before any of them lands:
  1. **Surface rotation tied to sim time.** Today's textures are
     static — Earth's continents and Moon's near-side layout are
     fixed regardless of `Clock.SimTime`. Threading sim time into
     the texture function (advancing `lon0` at sidereal rate —
     15°/hr for Earth; tidally locked Moon would stay fixed) is
     the obvious extension. Open: render-tick rate vs sim-tick rate?
     At high warp the surface would visibly spin — intentional, or
     distracting? Possibly clamped above some warp factor, like the
     cloud / orbit preview already do.
  2. **More features per body.** Mars polar caps + Olympus Mons,
     Jupiter banding + GRS, Saturn cloud bands, Uranus / Neptune as
     more than pale disks. Each is an ellipse-table edit + a switch
     case in `TextureFor`; cumulative LOC is small but visual ROI
     varies per body. Sequencing TBD — Mars first (most player-
     visible during a Hohmann arrival) is the natural pick.
  3. **Solar lighting + day/night terminator.** Today every body
     renders fully lit. Computing the sun-vector at each body's
     center and shading pixels by `cos(angle to sun)` would give a
     proper terminator, dawn/dusk crescents, and lunar-phase
     variation as the player orbits. Adds a (sun-pos, body-pos)
     projection step per pixel — modest cost, big visual payoff.
     Implies a partial-shadow color tier (or per-pixel brightness
     scaling, which lipgloss doesn't directly support — would need
     a per-cell ANSI 24-bit mix).
  4. **Eclipses as render artifacts.** Solar eclipse: Moon's disk
     obscures part of the Sun if the player happens to look at the
     right alignment. Lunar eclipse: Earth's shadow falls on the
     Moon during full-phase syzygy. Falls out almost for free if
     (3) lands; without it, eclipse rendering is a one-off special
     case. Either way, no current code path projects body-on-body
     occlusion — the canvas draws each body independently.

  Sequencing question: do (1) and (3) belong in the v0.7 cycle as
  follow-on patches in the v0.7.2.x line, or are they v0.8 polish
  behind multiplayer / manual flight? (2) is independent and can
  trickle in patch-by-patch (one body per polish slice, like
  v0.5.11 / v0.5.12 did). (4) gates on (3).
- **Active-burn flame animation**. The arrow glyph could pulse / extend
  while a burn is firing.
- **High-fidelity Earth raster (post-v0.8.5)**. v0.8.5.7 ships a
  hand-crafted polygon-rasterised 144×72 (2.5°) land/sea/desert/ice
  mask in `internal/render/earth_grid.go` — recognisable continents
  with key islands (UK, Iceland, Italy, Sicily, Madagascar, Cuba,
  Hispaniola, Sri Lanka, Sumatra, Java, Borneo, Sulawesi, New Guinea,
  Philippines, Tasmania, NZ, Korean peninsula, Japan, etc.). Polygon
  list is intentionally coarse (~50 polys × 10–20 verts each); higher
  fidelity would come from swapping in a public-domain raster
  (NOAA 1° ETOPO1 land/sea mask, ~64 KB embedded via `go:embed`).
  Same `earthCellAt(lat, lon)` lookup signature; only the data
  source changes. License-check the dataset (NOAA / Natural Earth
  are public domain; OSM-derived data has CC BY-SA constraints).
  Biome shading + atmospheric limb tint stay in EarthPixelColor.

### Polish / quality
- **Race-detector CI**. Currently no `-race` because the local environment
  doesn't have cgo; CI could enable it with `CGO_ENABLED=1`.
- ~~**Throttle control**~~ ✓ shipped in v0.7.3 (live throttle
  + manual flight) and v0.7.6 (per-node throttle in the `m`
  form, save schema v3 → v4, ActiveBurn captures throttle at
  fire-time so adjusting the live knob mid-coast doesn't slow
  planted burns).
- **More integration tests** at the World tick level. Most tests are
  unit-scale.

---

## 3. Version frame

| Version | Theme | Headline features |
|---------|-------|-------------------|
| v0.3.6 ✓ | | Adaptive body sizing, peri-below-surface warning |
| v0.4.0 ✓ | Persistence | Save / load with versioned envelope |
| v0.4.1 ✓ | | Porkchop Enter-to-plant + `R`-refine mid-course correction |
| v0.4.2 ✓ | | Per-sub-step SOI check in live integrator (high-warp orbit drift fix) |
| v0.4.3 ✓ | | Warp-lock: analytic Kepler propagation when warp > 1× and no active burn (eliminates Verlet eccentricity drift) |
| v0.4.4 ✓ | | Sub-divided Kepler step: chunks the analytic warp path so foreign SOIs (e.g. Mars during a heliocentric transfer) aren't skipped |
| v0.5.0 ✓ | | Body hierarchy: `ParentID`, recursive `BodyPosition`/`bodyInertialVelocity`, hierarchical `FindPrimary`. Major moons: Luna, Phobos, Deimos, Galilean ×4, Titan, Enceladus |
| v0.5.1 ✓ | | Color palette (`internal/render/palette.go`): hand-picked body colors + temperature-keyed stellar tint + UI-tier (alert / warning / node / trajectory / SOI). Wired into HUD selected-body name + body-info title. Per-cell canvas coloring stays scoped for v0.5.3 body-identity work. |
| v0.5.2 ✓ | | Vessel position trail: 200-sample ring buffer of inertial positions, sampled every 10 sim seconds (warp-independent density). Renders as a fading-stride dot trail on the orbit canvas behind the live ellipse. |
| v0.5.3 ✓ | | Per-cell canvas body coloring: `Canvas.AddColoredDisk` tags each body's cell footprint with its palette color; `String()` emits per-cell ANSI foregrounds. Body-identity glyphs / Saturn rings / HUD dividers / porkchop axis labels stay scoped for v0.5.x+. |
| v0.5.4 ✓ | | Vessel trail stores primary-relative R per sample (was: heliocentric inertial). Trail now follows the craft's apparent orbit around its primary; pre-fix it was a heliocentric trace drifting at Earth's orbital speed (~30 km/s). |
| v0.5.5 ✓ | | `bodyEphemeris` now recurses through the v0.5.0 hierarchy. Pre-fix it returned moon's parent-relative position as if heliocentric, so PorkchopGrid + PlanTransferAt for moon targets quoted nonsense Δv (~380 m/s display, ~25 km/s plant). Fix folds in for both. |
| v0.5.6 ✓ | | Default vessel is now an ICPS-like upper stage: 3500 kg dry + 25000 kg fuel, Isp 462 s (RL-10C-3), thrust 108 kN. Δv ~9.5 km/s — comfortable for a Earth → Luna round trip. Pre-v0.5.6 default (500/500/Isp 300, ~2 km/s) couldn't even reach Luna one-way. |
| v0.5.7 ✓ | | Intra-primary Hohmann (`PlanIntraPrimaryHohmann`): when target shares craft's primary (e.g. Luna, both around Earth), Hohmann plants a geocentric Hohmann (~3.1 km/s TLI + ~0.7 km/s Luna-orbit insertion). Pre-v0.5.7 the heliocentric Hohmann/Lambert path treated Luna's parent-relative semimajor as heliocentric → wildly wrong Δv. Porkchop rejects same-primary targets with a banner redirecting to Hohmann. |
| v0.5.8 ✓ | | Keybind cleanup: `P` → porkchop (was `k`), `H` → Hohmann auto-plant (was `P`). Mnemonic: P/Porkchop, H/Hohmann. |
| v0.5.9 ✓ | | Phase-corrected intra-primary Hohmann: `H` on a moon now waits for the next launch window (Luna leads craft by `π − n_target·T_transfer`) so the craft actually rendezvous with the target instead of arriving at empty apoapsis. Synodic period for LEO+Luna ≈ 89 min so the wait is short. Also fixes porkchop redirect-banner text from `[P]` to `[H]`. |
| v0.5.10 ✓ | | Lunar-mission delivery pass: Tick clamps to finite-burn TriggerTime (no high-warp burn-fire lag); planner pads launch window so centered burns don't fire retroactively; default vessel swapped to S-IVB-1 (J-2 1023 kN, 11000+40000 kg, Δv 6.3 km/s, ~110s TLI); intra-primary auto-plant returns to finite burns; ManeuverNode.TriggerTime is now the burn *center* (engine fires Duration/2 earlier — HUD shows the planner's intended moment). |
| v0.5.11 ✓ | | Saturn rings render — concentric outer ring at the B–A range (92k–137k km from Saturn), drawn in Saturn's palette color when zoom resolves the rings beyond the body disk. `render.BodyRings(id)` extensible for future ringed bodies. Face-on simplification (always concentric circles regardless of view angle). |
| v0.5.12 ✓ | | Body-identity glyph overlays — `Canvas.SetCellOverlay` replaces the cell at a body's center with a Unicode glyph (☉ star / ◉ gas giant / ● terrestrial / ○ moon) so types read distinctly even at small pixel radius. `render.GlyphFor(b)` keys on BodyType + MeanRadius. Skips system primary (already has ring+dot draw). |
| v0.5.13 ✓ | | HUD section dividers — replace blank-line separators with dim `─` rules across the HUD width. Active-burn block gets a leading ● indicator; peri-below-surface alert gets a leading ⚠. Section grouping much more scannable. |
| v0.5.14 ✓ | | Porkchop axis labels — fix the v0.3.3 misalignment (lead-in width mismatch + label overflow past grid edge). Tick line `└────` under the grid, dep-day labels at every 5th column properly centered, dim "dep day" axis title. |
| **v0.5.15 ✓** | **(final v0.5 patch)** | Fix focus-change lockup at extreme zoom — Saturn rings call to `RingColoredOutline` was unbounded; focusing on a tiny body (Phobos SOI ≈ 20 m → scale 1.8 px/m) made the ring project to ~247M pixels and loop billions of times. Cap samples at 4× canvas pixel diagonal in both `RingOutline` and `RingColoredOutline`; skip drawing rings entirely when `outerPx > canvasReach`. |
| **v0.5 ✓** | **Moons + visual enhancement** | Body hierarchy + Luna/Phobos/Deimos/Galilean/Titan/Enceladus (v0.5.0), then color (palette.go, realistic palette), vessel trail, HUD polish, body identity. Cycle closed at v0.5.15 — see `docs/v0.5-release-notes.md`. |
| **v0.6.0 ✓** | | Burn-at-next scheduler — `ManeuverNode.Event` enum (`Absolute / NextPeri / NextApo / NextAN / NextDN`); event-time helpers in `internal/orbital/events.go`; `World.resolveEventNodes` lazy-freeze resolver hooked into Tick before warp-clamp; `m` form gains `fire at` cycle field (focus 0/1/2/3); save schema bumps v1 → v2 with relaxed version check (v1 saves load with `Event = TriggerAbsolute`). |
| **v0.6.1 ✓** | | Predicted post-burn orbit HUD + maneuver UX polish — `orbital.OrbitReadout`, `World.PreviewBurnState` (event-aware shadow trajectory), `World.PredictedFinalOrbit` + `PredictedLegs` (chain through nodes, rebase into each node's intended frame so Hohmann arrival reports as Mars-frame). PROJECTED ORBIT HUD blocks on both the orbit screen and `m` form. Per-leg colored trajectory preview (cyan/mint/amber/pink cycle) with matched node-marker colors. `minOrbitPixels` gate hides sub-pixel ellipses + swaps craft chevron for bright `ColorCraftMarker` disk at large zoom. `ColorCurrentOrbit` → pale slate (distinct from any body palette). Default LEO 200 → 500 km, NewWorld spawns with `Focus = FocusCraft`, default burn duration 0 → 10 s. Active-burn guard suppresses projection while live state mutates. |
| **v0.6.2 ✓** | | Finite-burn-aware iterative planner — `planner.SimulateFiniteBurn` (RK4 + Tsiolkovsky), `planner.IterateForTarget` (Newton iteration on commanded Δv with ±50 % step cap and `ErrFiniteBurnDiverged` fallback), `planner.TargetApoapsis` / `TargetPeriapsis` residual helpers. `sim.refineFiniteDeparture` wires the iterator into `H` auto-plant so Hohmann departures hit the requested apoapsis even on low-TWR loadouts where the impulsive guess loses several percent of energy to gravity-rotation. For S-IVB-1 the iterator converges in 1-2 steps (no-op); for low-TWR profiles it catches errors the impulsive math misses. |
| **v0.6.3 ✓** | | Moon → parent escape transfer — `planner.PlanMoonEscape` (bound transfer ellipse with apolune at the moon's SOI radius, zero-Δv frame marker at SOI exit so the player plants their own circularization in the parent frame). New dispatch branch in `sim.PlanTransfer` for `target.ID == w.Craft.Primary.ParentID`; pre-v0.6.3 this fell through to the heliocentric Hohmann path and quoted nonsense Δv. Departure burn refines through v0.6.2's iterator with `TargetApoapsis(rSOI)`. Maneuver-screen mini-canvas now renders the primary as a sized disk (true-scale `FillColoredDisk`, [3, 64] px clamp) so low-orbit projections read at their real visual scale (the orbit screen's `BodyPixelRadius` would have dropped Luna-class moons to 1 px under its size-tier fallback). `PreviewBurnState` is finite-burn-aware: takes `Duration`, caps delivered Δv via the rocket equation given the duration window, routes through `planner.SimulateFiniteBurn` so the projected orbit matches what the live integrator will actually deliver — pre-v0.6.3 the preview ignored both finite-burn deformation and the duration cap, so a "400 m/s in 10 s" request previewed full 400 m/s while the live burn delivered ~205 m/s. Quit confirm prompt on `q` (footer "Quit and save? [y/N]"); `ctrl+c` stays immediate. Orbit-canvas camera freezes for the duration of an `ActiveBurn` so the live orbit ellipse + apsidal markers + selected-body crosshair morph in place rather than sweeping past as focus-on-craft tracks the burning craft. |
| **v0.6.4 ✓** | | Click-only mouse + 5-way view modes — `Canvas.pixelTags` widened to `CellTag{Color, BodyID, NodeIdx, IsVessel}`; `Canvas.HitAt(col, row)` aggregates the 2×4 pixels per terminal cell. Click cascade on the orbit screen: vessel → node → body → empty-canvas → HUD. Click-to-edit nodes preserves the scheduled fire time and replaces in place via `Maneuver.LoadNode(idx, node)` + `BurnExecutedMsg.{TriggerTime, EditingIdx}`; the form's BURN PLAN header switches to `BURN PLAN — editing node N` and the fire-at line shows an absolute countdown (`T+14m32s` or `next peri (T+3h12m)`). Empty-canvas click → `OrbitView.ProjectToOrbit` finds the orbit ν nearest the click and stages a new node at that point's TriggerTime via `Maneuver.LoadStaged`. Porkchop cells clickable (cursor parity). View-mode `v` cycles Top → Right → Bottom → Left → OrbitFlat; side views render back-of-body occlusion via `Canvas.IsBehindBody` + `Canvas.DrawEllipseOffsetOccluded` so the spacecraft orbit + apo/peri markers + craft glyph hide behind the primary's projected disk; OrbitFlat projects onto the active craft's orbit plane via `orbital.PerifocalBasis` for clean ellipse rendering at any inclination. Maneuver screen reflowed to horizontal `JoinHorizontal` so canvas + form fit side-by-side. Quit confirm prompt on `q` (`ctrl+c` stays immediate). Burn-camera freeze pins canvas center for the duration of an `ActiveBurn`. |
| **v0.6.5 ✓** | | Mission scaffold + burn-input simplification — new `internal/missions` package with three predicate kinds (`circularize` / `orbit_insertion` / `soi_flyby`) on a sticky three-state machine. Embedded `missions.json` starter catalog (1000 km LEO circularize, Luna orbit insertion, Mars SOI flyby) via `go:embed`. `World.Missions` seeded at `NewWorld`, evaluated each Tick after `executeDueNodes`. Save schema v2 → v3 with `Payload.Missions` (omitempty); v1/v2 saves seed the default catalog post-load so older saves gain the feature transparently. Orbit screen HUD gains a `MISSION` section + permanent `VIEW` sub-line under FOCUS (the v0.6.4 view-mode toast is gone). Maneuver planner drops the duration field; Δv now drives both the delivered Δv AND a rocket-equation-derived burn duration via `spacecraft.BurnTimeForDV(dv) = (m₀/ṁ)·(1 − exp(−Δv/(Isp·g₀)))`. Auto-plant Hohmann + RefinePlan paths unified on the same call so player- and auto-planted burns size identically (constant-mass `dv·m/F` scrubbed from five sites in `internal/sim/maneuver.go`). |
| **v0.6.6 ✓** | | Multiplayer design-doc spike — `docs/multiplayer-design.md`, ~1000 words covering Transport (WebSocket-for-MVP, escalate to QUIC if loss demands), Authority model (host-authoritative + warp-arbitration; lockstep as bit-identical-FP fallback), Persistence (`Session` inside `Payload`, schema v3 → v4 at real-slice time), Out of scope, and three open questions on multi-craft sequencing, warp-veto generalisation, and per-player vs shared missions. Pure prose; no code change. Closes the planned v0.6 cycle. |
| **v0.6 ✓** | **Planner UX + missions + MP design** | Burn-at-next scheduler + predicted-orbit HUD + finite-burn-aware planner + moon → parent escape transfer + click-only mouse + 5-way view modes + mission scaffold + burn-input simplification + multiplayer design-doc spike. See `docs/v0.6-plan.md` for slice breakdown. |
| **v0.7.0 ✓** | **Modding + manual flight + planner polish** | External system catalog overlay — `$XDG_CONFIG_HOME/.../systems/*.json` merges with embedded set via new `LoadAllWithWarnings`. `System.Source` (json:"-") tags entries `embedded` / `user`; bodyinfo screen shows the source. Conflict policy: user files win on `systemName`, otherwise append. Malformed user files surface as `LoadWarning` printed to stderr at startup; embedded systems always load. |
| **v0.7.1 ✓** | | Per-body palette migration — `Color string` field on `CelestialBody` (`json:"color,omitempty"`), 18 entries inserted into `sol.json` from the legacy `bodyPalette` table (no visual change). `render.ColorFor` resolution order: (1) `b.Color`, (2) table fallback, (3) `StellarTint`, (4) bodyType default. Catalog hash changed → v0.7.0 saves reject on first load, same UX as v0.5.0's moons schema bump. Legacy table + `TestColorForJSONFieldMatchesPaletteTable` consistency check stay until v0.8 drops the table. |
| **v0.7.2 ✓** | | User theme overrides — `theme.json` (optional `ui` + `bodies` blocks). UI overrides mutate the package-level `Color*` vars in place via `LoadTheme`; body overrides win over the v0.7.1 per-body `Color` field. `uiDefaults` captured at package init keeps `LoadTheme` idempotent. Malformed `theme.json` warns to stderr and falls back to defaults. |
| **v0.7.2.1 ✓** | *(polish patch)* | Textured Earth disk — at `r ≥ 12 px`, Earth renders per-pixel through `Canvas.FillTexturedDiskTagged` + `render.EarthPixelColor`. Orthographic (dx,dy) → (lat,lon) projection feeds an ellipse-table lookup classifying cloud / land / ocean. Body-identity `●` glyph suppressed for textured bodies. Static (no rotation). `render.BodyHasTexture(b, r)` is the dispatch hook for future bodies. |
| **v0.7.2.2 ✓** | *(polish patch)* | Textured Moon disk + dispatch refactor — Moon renders the canonical near-side mare layout (Crisium, Tranquillitatis, Imbrium, Procellarum, etc.) plus four bright rayed-crater accents (Tycho, Copernicus, Kepler, Aristarchus) via `render.MoonPixelColor`. New `BodyTextureMinRadius` (12 px) generalizes the threshold; new `BodyTexture` function type + `TextureFor(b, r)` lookup replace the body-specific switch in `orbit.go`. `EarthTextureMinRadius` retained as alias until v0.8 cleanup. |
| **v0.7.3 ✓** | | Manual flight controls — `Spacecraft.Throttle` field plumbed through `MassFlowRate` / `ThrustAccelFn` / `stepThrust`. `World.ManualBurn` parallels `ActiveBurn` (carries only `StartTime`); `World.AttitudeMode` drives manual-burn direction. Throttle keys `z` / `x` / `Shift+z` / `Shift+x`; attitude keys `w` / `s` / `a` / `d` / `q` / `e` (rebound `s`→`tab` for NextSystem and `q`→`Q` for QuitAsk to free WASD/QE). Warp clamp ≤10× during either burn type; Kepler warp-lock falls back to Verlet. PROPELLANT block adds throttle line; new ATTITUDE block. Per-node throttle override + save schema v3→v4 deferred to follow-up patch. |
| **v0.7.4 ✓** | | Inclination-change planner + HUD compaction pass + Hohmann-preview frame fix. New `planner.PlanInclinationChange` plants a single normal±-burn at the next AN/DN to rotate the orbital plane to a target inclination; Δv = 2·v_horizontal·sin(`Δi`/2) (exact at the node for any closed orbit). Physical AN/DN identification uses the current state's v.Z sign (robust to the ω-degeneracy that plagues circular orbits in events.go's label-based path). New `I` keybinding plants the burn against the selected body's inclination (or 0° equatorial when none) — same select-body-press-key UX as `H`/`P`. Inclination line added to PROJECTED ORBIT readouts (orbit HUD + maneuver form) so AN/DN burns no longer appear to do nothing. **HUD pass**: VESSEL+PROPELLANT side-by-side at half-width each (~6 row recovery); SYSTEM+SELECTED same shape (~3 rows); "view: <mode>" stamped via new `Canvas.SetCellLabel` overlay in the bottom-right corner of the orbit canvas (and mirrored on maneuver mini-canvas) — the FOCUS/VIEW HUD lines are gone; MISSION block removed in favour of a clickable `[Missions]` button in the orbit-screen title bar that opens a dedicated `screens.Missions` list (status glyphs ✓/✗/·); `[Menu]` companion button mirrors the existing Esc-on-home splash menu; menu screen reworked with clickable list buttons + Yes/No confirm sub-screens (keyboard direct path s/l/q preserved); `[Back]` button on menu and missions screens for a click-only return to orbit. **Frame fix**: `World.HohmannPreviewFor` now uses craft-primary GM + parent-relative radii for moon targets — pre-fix a LEO craft's Luna preview quoted Δv1 ≈ 28 km/s / Δv2 ≈ 242 km/s because it was computing a Hohmann from ~150M km (Earth's heliocentric distance) to ~384k km (Luna's parent-relative SMA). Same flavour as the v0.5.7 PlanTransfer fix; never propagated into the preview until now. |
| **v0.7.5 ✓** | | Explicit retrograde flag plumbed through `LambertSolve` / `LambertSolveRev` / `PlanLambertTransfer` / `PorkchopGrid`. Curtis 5.2's branch rule (prograde takes the short way when `(r1 × r2)·ẑ ≥ 0`, else the long way) reverses for retrograde — `flipBranch` logic encodes the swap. Existing call sites (RefinePlan, PlanTransferAt, sim/PorkchopGrid) pass `false` to preserve today's prograde behaviour; library-only with no UI toggle this slice. Unblocks multi-rev porkchop work in v0.8+ which will surface the flag as a porkchop-screen key. |
| **v0.7.6 ✓** | | Textured Mars + Jupiter, refined Earth, per-node throttle, SOI / frame-transition HUD. **Earth pass**: sub-observer longitude moved from 0° (Africa-only) to -30° (Atlantic-centered), continents decomposed into multi-ellipse shapes (N. America + Mexico + Alaska + Florida; Andes spine + Brazilian bulge; Africa with Horn; Eurasia with India + Iberia + SE Asia; Australia + Tasmania; UK + Iceland + Madagascar + Japan), polar ice caps, Sahara/Arabian/Outback desert overlays, additional cloud streaks (storm tracks + ITCZ). **Mars** (`internal/render/mars.go`): rust base, Syrtis Major / Solis Lacus / Acidalia / Mare Cimmerium / Mare Erythraeum dark albedo, Arabia Terra + Hellas bright regions, N/S polar caps. **Jupiter** (`internal/render/jupiter.go`): 10-band SPR/STB/STrZ/SEB/EZ/NEB/NTrZ/NTB/NTZ/NPR alternating zones/belts + Great Red Spot. Shared `continentEllipse` carries a `color` field so each ellipse picks its fill independently. **Per-node throttle**: `ManeuverNode.Throttle` field, save schema v3 → v4 with omitempty round-trip, `EffectiveThrottle` remaps zero (legacy default) to 1.0; integrator captures throttle onto `ActiveBurn` at fire-time so adjusting the live `Craft.Throttle` knob mid-coast no longer slows planted burns; maneuver form gains a `throttle: <input> %` field as a fourth focus stop. **SOI HUD**: `World.NextFrameTransition` walks resolved nodes for the first node with a foreign `PrimaryID`; new `FRAME TRANSITION` HUD section above NODES surfaces the upcoming frame change (catches v0.6.3 moon → parent escape's zero-Δv arrival marker + Hohmann arrival burns). |
| **v0.8.0 ✓** | **Multi-craft polish** | RCS / monopropellant precision-thruster mode — `Spacecraft.MonopropMass` + `MonopropFuel`, `EngineMode` toggle (`r` key) routing `b` / attitude keys through a 0.1 m/s pulse pool (~30 m/s on default S-IVB-1). Per-thruster RCS-puff visual placeholder (replaced in v0.8.3). |
| **v0.8.1 ✓** | | Multi-craft foundation — `World.Crafts []*Spacecraft` + `ActiveCraftIdx`; `[`/`]` cycle, `n` keystroke spawns sister craft 90° around primary; ManeuverNode / ActiveBurn / ManualBurn / TriggerEvent / AttitudeMode / EngineMode lifted from `internal/sim` to `internal/spacecraft` (sim re-exports as type aliases) so each craft owns its own state without an import cycle. Save schema v4 → v5 with `Craft *Craft` → `Crafts []*Craft` migration. HUD's NODES + BURNS blocks list all-craft state simultaneously. |
| **v0.8.2 ✓** | | Craft types + spawn form + capture preview — four loadouts (S-IVB-1 ▲ yellow, ICPS ◆ blue, RCS-tug ● pink, Lander ▼ mint) each with distinct propulsion + glyph + color rendered through the orbit canvas. Full `n` spawn form (loadout / parent body / altitude / direction). Clickable HUD NODES rows. CAPTURE PREVIEW HUD block surfaces predicted relative approach speed + qualitative direction (prograde/retrograde) for Hohmann arrivals. Equatorial inclination match (`I` with no body cursor → 0°). |
| **v0.8.3 ✓** | | Docking + undocking — proximity-gated DockCrafts at <50 m and <0.1 m/s relative velocity, mass-weighted centroid + momentum-conserving fuse, summed propellant pools, identity preservation through DockedComponents. Undock (`U`) splits back along original components with proportional propellant. RENDEZVOUS HUD (live range / |v_rel| / DOCK READY indicator). Spawn form `POSITION = alongside active` for inside-gate testing. Engine-firing flame visual + per-thruster RCS puff visuals replace the v0.8.0 placeholder. |
| **v0.8.4 ✓** | | Atmospheric drag — bodies.Atmosphere data model + Earth/Mars values (exponential ρ(h) with 8500m / 11100m scale heights), drag-aware Verlet (`physics.StepVerletWithAccel`) wired into live integrator + `propagateStateWithPrimary` + `PredictedSegmentsFrom` + `stepThrust`, Kepler warp-lock retreat below atmospheric cutoff, Spacecraft.BallisticCoefficient (default 0.01 m²/kg). Time-aware `propagateStateWithPrimary` (foundation work) unlocks exact CAPTURE PREVIEW inclination for typical Hohmanns. Surface clamp on aerobrake impact (`physics.ClampToSurface`); zoom cap for landed craft so altitude-0 stays visible. Visual: faint haze ring at cutoff+scale-height in atm.Color. |
| **v0.8.5 ✓** | | Sim-time planet rotation + view-aware projection + textured-bodies trickle. Rotation core: `bodies.CelestialBody.TidallyLocked` + `AxialTilt` + `AxialAzimuth` fields; `render.SubObserverPointDeg(b, simTime, camDir, primMer)` returning (subLat, subLon) — free-body uses sidereal rotation, tidally-locked tracks the body→parent vector at simTime so Luna's near-side faces Earth always. `Clock.RotationTime` advances at min(warp, 10000×) so high-warp doesn't blur surfaces. View-aware projection (Snyder §20 inverse-orthographic with arbitrary sub-observer point) means ViewTop on tilted Earth reveals the Arctic, Uranus rolls pole-on along its orbit, Saturn's polar hex stays at +78°N regardless of view; ViewOrbitFlat picks up the canvas's depth axis. Polygon-rasterised 144×72 Earth grid (~50 polys × 10–20 verts: continents + key islands like UK / Iceland / Italy + Sicily / Madagascar / Cuba / Hispaniola / Sumatra / Java / Borneo / Sulawesi / New Guinea / Philippines / Tasmania / NZ + deserts + polar ice) replaces the v0.7.6 ellipse-table approximation; biome-shaded land (tropical / temperate / boreal) by `|lat|`; atmospheric blue-marble limb tint at r²>0.92 over non-ice. Far-side / polar Moon detail (Mare Orientale + Moscoviense + Ingenii + South Pole-Aitken basin + far-side / polar craters); tidally-locked override always shows near-side regardless of canvas view mode. Tilted Saturn ring system: C / B / Cassini Division gap / A / F bands sampled in body equatorial plane and projected through `Canvas.RingTiltedOutline` so foreshortening reads correctly per view (~89% top, ~45% side). Textured Sun (limb-darkened solar disk + sunspots + corona halo replaces the v0.7.x ring + center-dot crosshair), Galileans (Io paterae, Europa lineae, Ganymede dark regiones, Callisto crater rays), Uranus (subtle banding), Neptune (banded + Great Dark Spot). Terminal moons (no children orbiting them) zoom to 8× radius on focus so surface texture is visible by default. Save schema: TidallyLocked + AxialTilt + AxialAzimuth fields bump CatalogHash; v0.8.4 saves reject on first v0.8.5 load. |
| **v0.8.6 ✓** | | Controls polish bag + unplanned add-ons. Keymap pass: Save/Load → F5/F9 (KSP-style), drop the global N ClearNodes binding (case-collided with `n` SpawnCraft), per-node ctrl+d delete + ctrl+k clear-all in the maneuver form, new World.DeleteNode sim API. **IterateForTarget toggle** in the `m` form (5th cycled field; refines commanded Δv via planner.IterateForTarget at plant time so post-burn apsides match the projected orbit; off by default; skipped for Normal±). **Body-equatorial Keplerian frame** for body-bound orbits — i/Ω/ω read in the primary's equatorial frame (ECI for Earth, MCI for Mars, etc.) per operational mission-planning convention; default LEO spawn passes over the equator (Ecuador), not over Guatemala (the world-XY-frame spawn intersected Earth at ~23°N because of the 23.44° axial tilt). PlaneMatchInclination converts a target's heliocentric plane into the primary's frame. Heliocentric orbits stay ecliptic-relative. **Adaptive warp clamps**: throttle-change cap (10× for 1 sim-second after Throttle changes); upcoming-node approach cap (continuous predictive ramp-down — maxWarp = secondsUntilNode / (10 × BaseStep), floored at 1×, prevents 100,000× warp from skipping a 30-s-out node). **Orbit-flat low-warp jitter fix**: ω snapped to 0 for circular orbits (eMag < 1e-6) so PerifocalBasis stays stable per frame; defensive pole-on guard added in SubObserverPointDeg. CI: four-part patch tags (vX.Y.Z.N) excluded from goreleaser so checkpoint markers don't fail the workflow. **Backlogged**: (c) multi-rev porkchop UI keys — defer until staging slices grow craft fleet enough that multi-rev / retrograde transfers are practically valuable. |
| v0.8 ✓ | **Multi-craft polish** | RCS / monopropellant + multi-craft slate + craft types + docking + atmospheric drag + sim-time rotation + view-aware textures + controls polish + body-equatorial frame + adaptive warp clamps + iterate-for-target. See `docs/v0.8-plan.md` for slice breakdown. |
| v0.9+ | Open *(speculative)* | Multiplayer implementation, multi-rev porkchop, ground-launch chain, real rendezvous planner (target-relative prograde/retrograde modes + null-v_rel at closest approach + iterative refinement — see §6 *Rendezvous tooling*), capture-direction toggle, plane-shift + Hohmann combo, N-body perturbations, mission editor/scripting, drag-to-edit nodes, solar lighting / day-night terminator / eclipses, high-fidelity Earth raster (NOAA ETOPO1) |

### v0.4 — save / load + mid-course corrections

The v0.3.x line built up a lot of orbital state — multi-frame nodes, active
burns, focus / zoom prefs, planted transfers — but a session ends and it all
evaporates. Adding state persistence is the gap most likely to break "I want
to come back to this" friction. Missions are deliberately **not** part of
v0.4; they slip to v0.6 so this release stays narrowly scoped to persistence
and closing the porkchop / replan loop.

Concrete v0.4 slices:

- **v0.4.0 — save / load.** JSON state file at
  `$XDG_STATE_HOME/terminal-space-program/save.json`, manual save / load with
  `S` / `L` keys, autosave on quit. Roundtrip the full `World` (clock, focus,
  craft, nodes, active burn). Save schema ships the **richer header from
  day one**: `{"version": 1, "generator": "tsp <semver>", "clock_t0":
  <unix-nanos>, "body_catalog_hash": "<sha256-of-canonical-bodies>",
  "payload": {...}}`. `generator` preserves forensics across versions,
  `clock_t0` separates wall-clock from sim-clock for replay, and
  `body_catalog_hash` rejects saves built against a stale universe
  (e.g. after a systems-JSON edit). Reserves the slot for v0.6's
  `session` block (multiplayer envelope) without a schema bump. About
  250–350 LOC + tests.
- **v0.4.1 — mid-course corrections.** Enter-to-plant from porkchop cursor
  (requires `World.PlanTransfer` to accept explicit departure-offset / TOF
  params) plus a "refine plan" key that re-runs Lambert from the live
  state and updates the planted arrival node. Closes the porkchop loop and
  gives the user a way to correct drift during a coast. About 150–200 LOC
  + tests.

### v0.5 — moons + visual enhancement

v0.5 leads with an architectural prereq — arbitrary-depth body hierarchy
so moons can orbit planets — then ships the visual work on top. Because
the hierarchy work is the expensive part and the moon catalog is mostly
JSON once it lands, v0.5.0 commits to the full major-moon set in one
go: Luna, Phobos/Deimos, the Galilean four, Titan, Enceladus. One
release validates the hierarchy across single-moon (Earth) *and*
multi-moon (Jupiter, Saturn) primaries, and the visual work in v0.5.1+
has a full cast to colour.

- **v0.5.0 — body hierarchy + major moons.** Today the body model is
  flat star → planet (`internal/bodies/body.go:26,39` carry moon
  *metadata* only, not propagated bodies); `FindPrimary()` in
  `internal/physics/soi.go:32–58` only computes SOI against
  `system.Bodies[0]`; `BodyPosition()` in `internal/sim/world.go:90–99`
  propagates Keplerian elements directly relative to the system primary.
  v0.5.0 introduces **arbitrary-depth hierarchy via `ParentID`**: add a
  `ParentID` field to `bodies.Body`, make `BodyPosition()` recursively
  resolve as `BodyPosition(parent) + propagate(elements, parent.μ)`,
  and rewrite `FindPrimary()` as a hierarchical walk (craft → check
  innermost SOI → fall outward). No depth cap in code — two-level is
  all Sol needs today, but sub-moons / ring shepherds cost nothing to
  accommodate at the schema layer and match how `FindPrimary()` already
  wants to recurse. Moon catalog in this release: **Luna + Phobos +
  Deimos + the four Galilean moons (Io, Europa, Ganymede, Callisto) +
  Titan + Enceladus**. The data surface is mostly JSON edits once the
  hierarchy lands; validating across single-moon (Earth) and
  multi-moon (Jupiter, Saturn) primaries in one release flushes out
  hierarchy bugs early. Estimated ~8–12 targeted edits on the physics
  side + ~9 moon entries in `bodies/systems/sol.json`, ~4–6 hours +
  tests. Transfer planning to/from moons is **not** in v0.5.0 scope —
  `PlanTransfer()` (`internal/sim/maneuver.go:70–113`) assumes shared
  primary; Earth → Luna pathing slips to v0.6 alongside the planner UX
  work, where it composes naturally with the burn-at-next scheduler.
- **v0.5.1 — color** — source of truth is a checked-in
  `internal/render/palette.go` constant table keyed by body name/type
  (plus UI-tier constants for alerts / nodes / trajectory). Faster to
  ship than threading a `Color` field through `bodies.Body` + every
  `systems/*.json`; the v0.7 config-file loader will promote the table
  to per-body JSON fields with a one-shot migration then. Palette:
  - Alerts red, warnings yellow, planted nodes cyan, trajectory white,
    foreign-SOI segments magenta (UI/status tier).
  - Bodies colored reminiscent of their actual appearance: Sol gold-white,
    Mercury grey, Venus pale yellow, Earth blue/green, Luna pale grey,
    Mars rust, Phobos / Deimos dark grey, Jupiter banded ochre, Io
    sulfur yellow, Europa off-white, Ganymede tan, Callisto dark tan,
    Saturn pale gold, Titan burnt orange, Enceladus bright white,
    Uranus cyan, Neptune deep blue.
  - Non-Sol stars: temperature-based tint (TRAPPIST-1 red dwarf deep red,
    Alpha Cen A yellow, Kepler-452 sun-like gold-white).
- **v0.5.2 — vessel position trail**. Ring buffer of last N inertial
  positions; render with fading stride.
- **v0.5.3 — body identity + HUD polish.** Different glyph density for
  stars vs gas giants vs terrestrial; rings drawn for ringed bodies
  (Saturn, eventually); HUD section dividers, alignment, clearer status
  indicators; porkchop axis labels rendered correctly.

### v0.6 — planner UX + missions + multiplayer design

Full slice breakdown lives in `docs/v0.6-plan.md`. Summary:

- **v0.6.0 ✓ — burn-at-next scheduler.** Event-relative maneuver
  nodes (`Absolute / NextPeri / NextApo / NextAN / NextDN`) wired as
  a single `fire at` cycle field in the `m` form. Lazy-freeze
  resolver — the event resolves once at the first Tick that yields
  a future trigger time, then `TriggerTime` freezes. Save schema
  bumps v1 → v2 with a `Node.Event` field defaulting `absolute` for
  backwards-compat.
- **v0.6.1 ✓ — predicted post-burn orbit HUD + maneuver UX polish.**
  PROJECTED ORBIT block on the orbit screen (apo/peri/AN/DN of the
  chained post-burn orbit via `World.PredictedFinalOrbit`). Same
  block lives in the `m` form via `World.PreviewBurnState`, which
  propagates to the fire-at event point before applying Δv (so a
  prograde at next-apo correctly previews perigee-rise rather than
  apoapsis growth). Per-leg colored trajectory preview with matched
  node-marker colors. Hohmann arrival rebases into Mars frame so
  the readout reports captured-orbit numbers, not heliocentric.
  `minOrbitPixels` gate hides sub-pixel ellipses; craft chevron
  swaps to a bright disk at large zoom. Default LEO 200 → 500 km;
  spawn focus = craft; default burn duration 10 s.
- **v0.6.2 ✓ — finite-burn-aware iterative planner.** Newton
  iteration around the impulsive solver via `planner.IterateForTarget`
  / `SimulateFiniteBurn`. Wired into intra-primary auto-plant
  (`sim.refineFiniteDeparture`). For S-IVB-1's high-TWR profile the
  iterator converges in 1-2 steps; for low-TWR loadouts it catches
  multi-percent gravity-rotation errors the impulsive math misses.
  `ErrFiniteBurnDiverged` fallback keeps unreachable targets from
  breaking the auto-plant flow.
- **v0.6.3 — moon → parent escape transfer.** New `PlanTransfer` path
  for the case `target == craft.Primary.ParentID` (Luna → Earth).
  Two-impulse: prograde escape burn at moon-orbit periapsis raising
  apolune past moon SOI, then ballistic drop into parent frame. Player
  circularizes manually post-SOI-exit. Wider inter-SOI capture
  (heliocentric → moon-of-other-planet) stays out of v0.6.
- **v0.6.4 — mouse selection.** Click-only. Targets: planet / moon /
  vessel / maneuver node on orbit canvas, porkchop cells, HUD panels.
  No drag, no wheel-zoom. Click on a planted node opens `m` pre-filled;
  click on a porkchop cell opens the planner with that transfer
  pre-loaded; click on the orbit canvas opens the planner with a new
  node staged at the click point.
- **v0.6.5 ✓ — mission scaffold + burn-input simplification.**
  `internal/missions/` package with three predicate kinds (typed via
  `Mission.Type` enum) on a sticky three-state machine; embedded
  starter catalog via `go:embed`. `World.Missions` seeded at
  `NewWorld`, evaluated each Tick. Save schema v2 → v3 with
  permissive load — v1/v2 saves seed the default catalog post-load.
  Orbit HUD gains MISSION + permanent VIEW sub-line. Folded in: the
  maneuver form drops the duration field; Δv-only input via
  `spacecraft.BurnTimeForDV` rocket-equation form, with the
  auto-plant + RefinePlan paths unified on the same call.
- **v0.6.6 ✓ — multiplayer design-doc spike.** `docs/multiplayer-design.md`,
  ~1000 words. Tentative picks: WebSocket-for-MVP transport,
  host-authoritative authority with a warp-arbitration rule (warp
  escalation requires both peers' active-burn count == 0), Session
  inside Payload at schema v3 → v4 when the real implementation
  lands. Implementation roadmap and target release intentionally out
  of scope. Multiplayer leaves the "excluded forever" list because
  of this.

### Excluded forever
- **Full 3D rendering** — terminal native is the project ethos.

Previously on this list: *realistic atmospheric drag* (softened to an
opt-in simple drag model in §2 backlog — realistic multi-species drag is
still out), and *multiplayer / network sync* (demoted to a v0.6 design
spike; shipped implementation deferred but no longer forbidden).

---

## 4. Resolved

### v0.4 cycle

The v0.3.6 edition of this doc closed with four open scoping questions.
They've been answered in the v0.4 planning cycle:

1. **v0.4 theme.** Save / load only. Missions slip to v0.6 so v0.4 stays
   narrowly scoped to persistence.
2. **v0.5 color.** Hardcoded palette now; planets and stars colored
   reminiscent of their real appearance (see §3 v0.5). Theme is promoted
   to user-configurable in v0.7 alongside the systems config-file loader.
3. **Mid-course corrections.** Folded into v0.4.1, paired with porkchop
   Enter-to-plant as a single "close the replan loop" release.
4. **Multi-rev porkchop.** Still deferred — re-evaluate as v0.5.x polish
   once the v0.5 visual work lands.

### Current cycle

All six open questions resolved in one round so the v0.4 → v0.6
implementation work can start from a fixed set of assumptions. Grouped
by target version:

**v0.4 — save format**

1. **Save-schema header.** Richer header from day one:
   `{"version": 1, "generator": "tsp <semver>", "clock_t0": <unix-nanos>,
   "body_catalog_hash": "<sha256>", "payload": {...}}`. Catches the
   universe-drift footgun (edit `systems/sol.json`, old saves reject),
   preserves forensics across versions, and the v0.6 multiplayer spike
   already guarantees we need to extend the envelope — committing the
   slot now beats a v0.5 schema migration. See §3 v0.4.0.

**v0.5 — moons + visuals**

2. **Color palette source of truth.** Checked-in
   `internal/render/palette.go` constant table keyed by body name/type.
   Faster than threading a `Color` field through `bodies.Body` + every
   `systems/*.json`; v0.7 config-file loader promotes it to per-body
   JSON fields with a one-shot migration.
3. **Moon coverage.** v0.5.0 ships **Luna + Phobos/Deimos + four
   Galilean (Io, Europa, Ganymede, Callisto) + Titan + Enceladus**.
   Validates the hierarchy against single-moon *and* multi-moon
   primaries in one release; catalog is mostly JSON once the code lands.
4. **Hierarchy depth.** Arbitrary depth via `Body.ParentID` + recursive
   `BodyPosition()` + hierarchical `FindPrimary()`. No code-level depth
   cap. Sol only needs two levels today, but the schema accommodates
   sub-moons / ring shepherds at zero incremental cost.

**v0.6 — planner UX + MP design**

5. **Burn-scheduler UX.** All five modes (`Absolute / NextPeri / NextApo
   / NextAN / NextDN`) ship as a single `fire at` cycle field in `m`.
   No advanced-trigger picker. Lazy-freeze resolver — event-relative
   nodes resolve `TriggerTime` once at the first Tick that yields a
   future trigger, then freeze. *(Earlier cycle proposed a hybrid with
   a secondary picker; consolidated to one cycle field during v0.6
   scoping — see `docs/v0.6-plan.md`.)*
6. **Cross-SOI `PlanTransfer` scope.** v0.6.3 covers **moon → parent**
   (Luna → Earth) only. Wider inter-SOI capture (heliocentric →
   moon-of-other-planet) stays deferred.
7. **Mission persistence shape.** Inside `Payload.Missions` — schema
   v1 → v2 with permissive load, not a sibling block.
8. **Mouse scope.** Click-only selection (planet / moon / vessel /
   maneuver node / porkchop cell / HUD panel). No drag, no wheel-zoom.
9. **Multiplayer design scope.** Networking + persistence. The spike
   covers transport / authority / time-warp **and** a persistence /
   save-slot story for shared sessions, so the v0.4 save envelope is
   future-proofed deliberately. Implementation roadmap is explicitly
   *not* in scope for the v0.6 doc — constraints only, no schedule.

---

## 5. Open questions for the next cycle

We're now at the **v0.7 ship boundary**, with v0.8 the next cycle.
v0.8 doesn't have a written plan yet — see §2's *v0.8+ candidates*
sub-section for the unsorted backlog. The questions framed for the
v0.6 → v0.7 boundary largely resolved during v0.7 implementation
(theme.json single-file, body rendering progressed via v0.7.6 Mars +
Jupiter, SOI-exit HUD shipped in v0.7.6, retrograde flag in v0.7.5);
what carries forward, plus what v0.7 newly opened, framed for v0.8
scoping:

**Carry-overs from prior boundaries**

- **Inter-SOI `PlanTransfer()` capture** *(carried from v0.6 → v0.7
  → v0.8 → v0.9)*. v0.5.7's `PlanIntraPrimaryHohmann` covers
  same-parent, v0.6.3 covers moon → parent. The remaining direction
  — heliocentric → moon-of-other-planet (Phobos from a Mars
  approach, a Galilean from a Jupiter cruise) — still needs a real
  patched-conic capture pass through both SOIs. Did not ship in
  v0.8.
- ~~**Caller-facing `IterateForTarget` toggle**~~ ✓ shipped in
  v0.8.6.3 — `iterate (off/on)` cycle field in the `m` form,
  `World.IterateBurnDV(mode, dv)` helper.
- **Predictor adaptive sampling at high warp** *(carried from
  `docs/integration-design.md` §10)*. The fixed 96-sample horizon
  collapses to a smear at 10000× warp on LEO orbits. Adaptive
  sampling (sample density ∝ orbit period / sim-time horizon) is
  the obvious fix. Did not ship in v0.8.
- ~~**Multi-craft selector vs multiplayer ordering**~~ ✓ resolved
  in v0.8.1 (multi-craft selector shipped first; MP follows on the
  same `World.Crafts` foundation).
- **Body-rendering polish sequencing**. v0.8.5 shipped sim-time
  rotation + view-aware projection + the textured-bodies trickle
  (Sun, Galileans, Uranus, Neptune, refined Earth/Moon, tilted
  Saturn rings). Solar lighting + terminator + eclipses still
  deferred to v0.9 (research-first item: ANSI 24-bit per-cell
  mixing as a `lipgloss` workaround).
- ~~**Monopropellant / RCS mode**~~ ✓ shipped in v0.8.0; docking
  shipped in v0.8.3 with proximity-gated DockCrafts at <50 m /
  <0.1 m/s.

**Newly opened from v0.7**

- **`World.AttitudeMode` save persistence** *(v0.7.3)*. Today the
  attitude mode is ephemeral stick state — not in saves. A player
  who loads mid-coast finds their attitude reset to prograde.
  Decision held at "keep ephemeral" through v0.8 — planted nodes
  are the persistence layer. Reopen if players report mid-coast-
  load resetting attitude is annoying.
- ~~**Throttle-change warp clamp**~~ ✓ shipped in v0.8.6.2 with
  an unplanned predictive add-on (upcoming-node approach clamp).
- **Theme-file hot-reload** *(v0.7.2)*. Today `theme.json` loads
  once at startup. Watching the file would let players iterate
  without restarting. Cheap (~200 LOC of fsnotify) — still
  deferred; never surfaced as a v0.8 playtest pain point.
- **`bodies.json` sibling overlay** *(v0.7.0 carry-over)*. The
  v0.7.0 catalog loader takes whole-system files; a per-body
  overlay would let users tweak orbital elements / radius / GM
  for individual bodies without redefining the whole system. Still
  deferred; pairs with future mission-scripting work.
- **Multi-rev porkchop UI surface** *(v0.7.5 carry-over,
  re-deferred from v0.8.6 (c))*. Library is ready
  (`LambertSolveRev`, retrograde flag); UI not sliced. Now pinned
  to v0.9 alongside staging slices that grow the craft fleet —
  current chemical S-IVB-1-class fleet always picks nRev=0
  prograde, so the UI gives no leverage until that changes.
- ~~**Sim-time rotation for textured planets**~~ ✓ shipped in
  v0.8.5; high-warp clamp at 10000× via `Clock.RotationTime`.
- **`Rings` / `Glyph` JSON overrides** *(v0.7.0 follow-up)*. v0.7.1
  put `Color` into `bodies.CelestialBody`; whether `Rings` (today
  hardcoded in `render.BodyRings`) and `Glyph` (in
  `render.GlyphFor`) follow as JSON-driven fields is still open.
- **Mission scripting / editor — needs design pass before
  implementation** *(post-v0.8.6, v0.8.7-attempt rolled back)*. A
  draft Option-B implementation went in (commit `4159a31`) and was
  reverted (`e806dd3`) because the design decision points
  (engine pick, modder UX flow, error feedback, schema versioning,
  cross-craft predicate scope, ceiling-vs-floor expectation, editor
  surface, sandboxing) collapsed into "expr is lighter, ship it"
  without their own discussion. The artifacts in git history are
  reference material; do not cherry-pick them as a substitute for
  the design pass. Full retrospective + decision-point list in §6
  *Mission scripting / editor*. Treat as a v0.9-cycle slice with
  a design doc preceding code.

---

## 6. Foundations beyond v0.8

Sketches of the infrastructure that needs to land *before* the
deepest backlog items can ship. Not slice plans — these are
"if we want feature X, here's what we'd have to build first"
notes so future cycles don't get blindsided.

### Multi-craft architecture

Today `World.Craft` is a single pointer; UI features that depend
on "the active orbit" — focus, PROJECTED ORBIT, OrbitFlat view
basis, manual flight — implicitly point at it. Going to multiple
craft requires:

- `World.Crafts []Spacecraft` (or `[]*Spacecraft`) + a
  `World.ActiveCraftIdx` int.
- Every read of `w.Craft` audited and re-pointed via an
  `ActiveCraft()` accessor (or a per-screen craft selector).
- Save schema bump (per-craft state for each entry).
- Selector keybinding (`[`/`]` cycle, click-to-select on canvas).
- HUD: which craft are we looking at? VESSEL block needs a
  craft name, maybe a numbered chip in the title bar.
- Predictor / planner attribution: PROJECTED ORBIT chains
  through *which craft's* node list?
- Multi-craft physics is already supported by the integrator
  (each craft propagates independently in its own primary's
  frame). The bottleneck is the UI / save / planner layers.

This unlocks: docking (two craft), MP (host's craft + remote
peers' craft), formation flying / rendezvous gameplay.

### Mission scripting / editor

Today `internal/missions` has three predicate kinds (`circularize`
/ `orbit_insertion` / `soi_flyby`) hard-coded in Go. A scripting
foundation lets users author objectives without recompiling.

**Two paths sketched** (one rough draft attempted post-v0.8.6 and
rolled back — see retrospective below):

- **Option A: declarative DSL.** Extend the `missions.json`
  schema with chained predicates — "stay within X of body Y for
  T seconds" / "dock with craft Z" / "complete N orbits."
  Lower ceiling, easier to parse, no embedded interpreter.
- **Option B: embedded expression language.** Evaluating against
  an `EvalContext`-derived environment. Higher ceiling —
  arbitrary predicates over (state, time, world snapshot) — but
  adds an interpreter dependency and a sandboxing story.

Either way, a TUI mission editor screen builds on top of this —
"new mission" → fill in a form → preview against the current
world. v0.7.0's catalog-loader pattern (embedded + user files
merged) is the obvious storage model.

**Retrospective: the post-v0.8.6 attempt at Option B.** A draft
implementation went in (commit `4159a31`, reverted in `e806dd3`)
that picked `github.com/expr-lang/expr` as the engine, defined an
11-field environment schema (`primary`, `altitude_m`,
`apoapsis_alt_m`, `periapsis_alt_m`, `eccentricity`,
`inclination_deg`, `velocity_m_s`, `fuel_kg`, `monoprop_kg`,
`dv_budget_m_s`, `sim_time_unix`), shipped a starter
`mars-soft-capture` mission, and tagged v0.8.7. The rollback wasn't
about the code working — it ran clean — but about the design
decision points being collapsed into "it compiles, ship it"
instead of getting their own discussion. The artifacts are still
in git history and can be cherry-picked back once the design pass
below is run.

**Decision points that need a real pass before re-attempting**
(any of these resolved differently changes the implementation
shape, sometimes substantially):

- *Engine pick.* The reverted draft picked **expr-lang/expr** on
  dep-weight (pure Go, no protobuf, ~5 kLOC, zero transitive
  deps). Real candidates with their own tradeoffs:
  - **expr-lang/expr** — terse, MIT, batteries-included for
    boolean predicates. Decent error messages but no IDE / syntax-
    highlighting community.
  - **Google CEL** (`cel-go`) — more rigorous type system,
    Apache-licensed, designed for policy / config expressions.
    Heavier dep tree (protobuf, ~30 transitive deps) but richer
    documentation surface and broader adoption (Kubernetes
    admission policies use it).
  - **Starlark** (`go.starlark.net`) — Python-like, supports
    multi-statement functions for composite missions ("define a
    helper that checks plane match THEN circularization"). Bigger
    surface but the ceiling is real.
  - **Hand-rolled mini-DSL** — ~300 LOC of Go for boolean ops +
    comparisons over named fields. Zero deps, full control over
    error messages, but reinvents the wheel.
- *Modder UX flow.* Where do mission files live? How do
  community-authored mission packs get discovered, downloaded,
  installed, validated? Today's catalog-loader merges
  `$XDG_CONFIG_HOME/.../missions/*.json` with the embedded set;
  is that sufficient, or do we want a one-shot "drop a `.tspmission`
  file on the binary, append to user catalog" UX? An in-game
  mission browser that fetches from a community index?
- *Error feedback when authoring.* Today's loader fails the
  whole catalog on first compile error. For a player iterating on
  their own mission JSON, "your mission won't load and the game
  shows you a stderr line you didn't see" is poor UX. Should bad
  expressions surface as a `Failed`-status mission with the
  compile error in the description? A dedicated `[Errors]` HUD
  block?
- *Schema stability.* Once expressions reference field names,
  renaming a field breaks every catalog that used it. Versioning
  the env schema (`expression_schema_version: 1`) is one path;
  guaranteeing field names with a doc-tested catalog pass is
  another.
- *Cross-craft predicates.* The single-craft env makes "dock
  with craft Z" / "rendezvous within X km of vessel Y" inexpressible.
  Either widen the env (`craft.others[*].state`) or wait for the
  v0.9 rendezvous tooling to define the right craft-targeting
  primitives first.
- *Mass / propellant fields.* The v0.8.7 draft zeroed
  `fuel_kg` / `monoprop_kg` / `dv_budget_m_s` because
  `EvalContext` didn't carry them; expression authors reading
  those fields would have silently always seen 0. Either thread
  them through `EvalContext` (small `sim.World.Tick` diff) or
  document the env as state-only with a follow-up to add
  resources.
- *Ceiling vs floor.* Is the expectation "modders write
  one-line predicates" (Option A or expr) or "modders compose
  multi-step missions with helper functions" (Starlark)? The
  ceiling drives the engine pick more than the dep weight does.
- *Editor surface.* Stretch: a TUI mission editor (form-driven,
  expression syntax-highlighted, "test against current world"
  button). Ships separately or pairs with the engine?
- *Sandboxing.* All three engines above are sandboxed by
  default, but custom-DSL leaves the security story to us. If we
  ever want to fetch community mission packs over the network, the
  sandbox guarantees matter.

**Suggested sequencing.** Treat this as a v0.9-cycle slice with
its own design doc preceding code. Block 1: write down the
modder-UX target end-to-end (download → install → playtest → share
→ debug). Block 2: pick the engine with that UX in mind, not in
isolation. Block 3: reference the v0.8.7-attempt artifacts for
the implementation shape. Block 4: implement.

Do **not** start with the engine pick again.

### Rendezvous tooling

v0.8.3 docking ships proximity-ops + DockCrafts at <50 m / <0.1 m/s,
but there's no planner-side help for *getting* there. Today's flow
is "Hohmann to a moon, then thumb-fly the closing approach with
RCS" — which works for the alongside-spawn test path but doesn't
scale to two craft on independent orbits.

The KSP-canonical flow (and the right v0.9 target):

1. **Target selection.** A "set as target" keybinding on a selected
   craft. Stores `World.TargetCraftIdx` parallel to the existing
   `ActiveCraftIdx`. RENDEZVOUS HUD already computes range / |v_rel|
   for an implicit target; surfacing target as explicit state means
   different craft pairs report different distances.
2. **Target-relative prograde / retrograde markers.** New burn modes
   `BurnTargetPrograde` / `BurnTargetRetrograde` whose direction unit
   is `(v_active − v_target) / |v_active − v_target|` (or its
   negative). Surface as cycle entries in the `m` form alongside
   the existing prograde / retrograde / radial / normal options.
   Attitude-mode WASDQE follows when active.
3. **Prograde-to-target burn from a phasing orbit.** Player matches
   inclination + phase via existing tools (Hohmann, plane-shift),
   then burns target-prograde to nudge the orbit toward an
   intercept. Live HUD shows next-closest-approach distance + Δt
   so the player can iterate burn magnitude until the encounter is
   acceptable.
4. **Null relative velocity at closest approach.** A "kill v_rel"
   maneuver: at closest approach, the required Δv is exactly
   `−(v_active − v_target)` in the active craft's frame. Plant as
   a target-retrograde burn at the predicted time of closest
   approach. After the burn, |v_rel| ≈ 0 — craft sits stationary
   relative to target at whatever the residual range was.
5. **Iterate.** First pass typically leaves residual range
   (10–500 m) and small residual velocity (sub-m/s). Repeat:
   small target-prograde nudge → coast to next closest approach →
   null residual → eventually within RCS-pulse range.

Foundations needed:
- `World.TargetCraftIdx int` + accessor.
- `spacecraft.BurnTargetPrograde / BurnTargetRetrograde` modes,
  thread through `DirectionUnit` with the active craft's
  `(r, v)` and the target's `(r, v)`.
- Closest-approach finder: `planner.NextClosestApproach(active,
  target, mu) (dt, range, vRel)` — Newton-iterate `d/dt (r_a −
  r_t)·(v_a − v_t) = 0` over an orbital-period window.
- Auto-plant entry: `World.PlanRendezvous(targetIdx)` plants a
  pair of nodes (target-prograde nudge + target-retrograde at
  closest approach), iterating until predicted closest-approach
  range is below a threshold (1 km default) or convergence
  fails.
- HUD: TARGET block showing target name + range + |v_rel| + time
  to next closest approach.

This unlocks the canonical Apollo CSM-LM / ISS-approach gameplay
loop without alongside-spawn cheats. Pairs naturally with the
"target craft Hohmann + phasing" line in the v0.8.3 row's deferred
list. Consider sequencing alongside the staging slices that grow
the craft fleet — once players routinely have multiple craft in
flight, the lack of this tooling becomes the binding UX constraint.

### N-body perturbations

The current Kepler-warp-lock fast path (analytic propagation
during high warp when no burn is active) assumes pure two-body
gravity. N-body breaks this — Lagrangian points, J2 perturbation,
third-body acceleration all need numerical integration even at
warp. Foundations:

- **Perturbation accumulator.** Instead of an integrator rewrite,
  layer additional acceleration sources on top of the existing
  patched-conic gravity term. `physics.AccelN(r, primary, sources)`
  with sources containing `J2Coefficient`, `ThirdBody{ID, GM}`,
  etc.
- **Warp-lock retreat.** Disable the analytic Kepler fast path
  for orbits where perturbations are non-negligible (e.g. craft
  in cislunar space — Earth + Moon both contribute meaningfully).
  Falls back to RK4 + Verlet at any warp. Performance regression
  at high warp; needs an adaptive step size.
- **Predictor support.** The 96-sample predictor walks Kepler;
  N-body propagation needs a longer-running per-frame sim. The
  v0.5+ adaptive-sampling work pairs naturally with this — fewer
  samples at high warp means each sample can be a heavier
  numerical step.

This unlocks: Lagrangian-point parking, accurate cislunar
trajectories, J2-corrected molniya orbits.

### Multi-system spacecraft (interstellar)

Two paths:

- **Real interstellar transfer math.** Lambert solver + patched
  conics scale to interstellar distances trivially (the math
  doesn't care), but transfer times are ~50,000 yr at chemical
  Δv. Real solution wants a propulsion abstraction (constant-
  thrust ion drive at GW power, light-sail, etc.) — a serious
  new physics layer.
- **Deus-ex jump drive.** A `⟨jump⟩` action that warps the craft
  to a target system's primary orbit at the cost of some abstract
  "jump energy." Cheap to ship — maybe 100 LOC of UI + a
  `World.JumpToSystem(idx)` that swaps `Craft.Primary` and seeds
  a new orbit. Unlocks the system-cycle UX without solving the
  hard problem.

Recommend the latter for v0.8+ first ship; the real cruise math
can come later if it has a player-facing point.

### Save schema migrations

Every cycle so far has bumped the save schema with omitempty
fields and permissive load (v1 → v2 → v3 → v4). The pattern works
for additive fields but doesn't survive *removed* or *semantically
changed* fields. When that hits:

- Per-version migration functions: `v3to4`, `v4to5`, etc., each
  taking a parsed-but-old `Payload` and returning the next
  version's shape.
- Versioned struct types (`payloadV3`, `payloadV4`) so each
  migration is statically typed.
- Test harness: a corpus of saved games at each schema version
  that must round-trip through current load.

Multiplayer (`Session` block) and N-body (`Perturbations`
block) would both be additive, so the omitempty pattern still
holds. Multi-craft (`Crafts []Craft` replacing `Craft *Craft`)
is the first migration that wants the typed-versioned-payload
machinery.

### Atmospheric drag

Two-body patched-conic stays the primary integrator; drag is an
opt-in velocity-dependent acceleration term. Foundations:

- `bodies.CelestialBody.Atmosphere *AtmosphereModel` field
  (omitempty in JSON). Density model: scale-height exponential,
  reference altitude + density at reference, plus a cutoff
  altitude.
- `physics.DragAccel(r, v, primary)` adds the term; the active-
  burn / manual-burn paths layer it onto thrust + gravity.
- Predictor needs to know about it too — drag-aware orbit
  prediction is a different beast from analytic Kepler. May
  warrant a "drag region" warp clamp similar to the burn clamp.

This is the kind of feature where the *foundation* is small
(maybe 200 LOC) but the *gameplay* implications (reentry
trajectories, aerobraking captures, atmospheric heating?) are
deep. Worth designing the foundation conservatively — get the
drag term landing the right Δv per orbit, defer heating /
aerodynamic forces / atmospheric chemistry indefinitely.

---

Update this doc on each minor / patch boundary so the snapshot stays
current.
