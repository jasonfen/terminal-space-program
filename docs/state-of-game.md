# terminal-space-program — state of game

*Snapshot at v0.7.4 (April 2026) — v0.6 cycle complete; v0.7 cycle
through v0.7.4 shipped (modding chain v0.7.0–v0.7.2.3 + manual flight
v0.7.3 + Esc-on-home menu polish v0.7.3.1–.3 + inclination-change
planner v0.7.4 + a HUD compaction pass that moved the view indicator
into the canvas, paired VESSEL/PROPELLANT and SYSTEM/SELECTED, and
relocated MISSION into a dedicated [Missions] screen reachable from a
title-bar button). Updated at each minor / patch boundary.*

`docs/plan.md` is the original architecture / phase plan. This doc complements it
with a "what plays today, what's queued next" view organised around player-facing
features and the version sequence that delivers them.
`docs/v0.5-release-notes.md` covers the v0.5.0 → v0.5.15 release series patch
by patch — this doc is the snapshot, those are the release notes.

---

## 1. What works today (v0.6.6)

### Physics
- Two-body patched-conic propagation with **SOI-aware** state transitions.
  Crossing a sphere of influence (outward or inward) rebases the state into the
  new primary's frame and switches μ for subsequent steps.
- Symplectic **Verlet** integrator for free flight (energy-conserving within
  ~1e-7 % over 1000 orbits at LEO).
- **RK4** integrator on the active-burn path so non-conservative thrust forces
  integrate cleanly. Verlet stays on the inactive-burn path.
- **Stumpff-universal-variables Lambert solver** (Curtis Algorithm 5.2). Single-
  rev plus multi-rev branches — `LambertSolveRev(..., nRev)`. Single branch
  per N (lower-z side of the minimum-energy critical point).
- **Hohmann transfer** math: classical two-impulse for circular-to-circular.
- **Patched-conic v∞ → Δv** identity for departure / capture burns.
- **Time warp** clamps to ≤10× during an active burn so the integrator never
  outruns the burn window's temporal resolution. Outside burns the clamp is
  the (1024 sub-steps × period/100) / base-step guard from v0.1.

### Spacecraft & burns
- **Finite-duration burns**. `Spacecraft.Thrust = 108 kN` default (RL-10C-3); a node's
  `Duration` field controls integration. Mass flow `dm/dt = -Thrust/(Isp·g0)`
  debits fuel each sub-step; burn ends on Δv delivered, fuel exhausted, or
  duration elapsed (whichever first).
- **Six burn modes**: prograde, retrograde, normal±, radial±. Direction is
  recomputed each sub-step from live (r, v) so held-prograde follows the
  rotating velocity frame.
- Default vessel ("S-IVB-1", v0.5.10+): 11000 kg dry + 40000 kg fuel,
  Isp 421 s, Thrust 1023 kN (J-2 vacuum). Δv budget ≈ 6.3 km/s,
  comfortable for a Luna round trip. Spawns in a 500 km circular
  prograde LEO (v0.6.1+; was 200 km), inclination 0°. NewWorld sets
  `Focus = FocusCraft` so the camera is on the ship from tick 0.

### Planning
- **Manual planner** (`m`): three-field form (mode / fire-at / Δv).
  `Tab` cycles fields, `←/→` cycles modes or trigger events when those
  fields are focused, Δv > budget warns. Burn duration is **derived**
  from Δv via the rocket-equation form `t = (m₀/ṁ)·(1 − exp(−Δv/(Isp·g₀)))`
  in `spacecraft.BurnTimeForDV` (v0.6.5+) — pre-v0.6.5 the form
  exposed Δv AND duration as independent inputs, but at fixed thrust +
  mass the two are the same dial. The auto-plant Hohmann +
  RefinePlan paths route through the same call so player- and
  auto-planted burns size identically. The planner shows a live
  PROJECTED ORBIT block with apo/peri/AN/DN of the resulting orbit;
  the canvas dashed shadow trajectory and the form readout both feed
  off `World.PreviewBurnState`, which propagates to the fire-at event
  point before applying Δv (v0.6.1) — so a prograde burn at next-apo
  previews the perigee-rise circularization, not a spurious second
  apoapsis growth. Zero-thrust craft fall back to impulsive
  (`BurnTimeForDV` returns 0); the legacy code path is preserved
  through the API even though the form no longer surfaces it as an
  input. v0.6.0+ form, v0.6.1 readout, v0.6.5 input simplification.
- **Event-relative trigger nodes** (v0.6.0): `fire at` field selects
  Absolute T+ or one of `next peri / next apo / next AN / next DN`.
  Lazy-freeze resolver in `World.Tick` computes `TriggerTime` from the
  live orbit on the first tick after plant, then freezes — past that
  point dispatch is identical to absolute-time nodes. Equatorial /
  hyperbolic / unreachable inputs leave the node unresolved; the
  resolver retries each tick.
- **`n` quick-plant**: T+5 min prograde 200 m/s, finite (sized to thrust).
- **`H` auto-plant Hohmann transfer**: select target body, one keystroke
  plants two finite nodes (geocentric departure + destination-frame arrival)
  with `Duration = Δv × mass / thrust`. Frame-aware via `ManeuverNode.PrimaryID`.
  v0.6.2: the departure Δv is refined through `planner.IterateForTarget`,
  a Newton solver that adjusts commanded Δv against a finite-burn
  RK4 integration of the burn until delivered apoapsis matches the
  Hohmann target. For high-TWR loadouts (S-IVB-1) the impulsive
  guess is already < 0.1 % off so the iterator converges in 1-2
  steps — effectively a no-op. For low-TWR loadouts (revived ICPS,
  future ion stages) it catches multi-percent gravity-rotation
  losses the impulsive math misses. Iteration failure silently
  falls back to the impulsive Δv.
- **`P` porkchop plot**: ASCII heatmap over departure-day × time-of-flight,
  intensity ramp `█▓▒░ ` cheap → expensive. Cursor navigates cells, snaps to
  min-Δv on open. **Enter on a feasible cell plants that Lambert-based
  transfer** (v0.4.1) via `World.PlanTransferAt`.
- **`R` refine plan** (v0.4.1): re-runs Lambert from the craft's live
  heliocentric state to the pending arrival node's target at the
  existing arrival time; plants a prograde / retrograde mid-course
  correction burn sized to `|v1_lambert − v_craft|` and replaces the
  arrival burn's Δv with the refined `|v2_lambert − v_target|` capture.
  Correction mode picked by alignment of the Δv vector with current
  velocity (scalar-along-velocity; full vector corrections stay
  deferred).

### Rendering (orbit canvas)
- **Adaptive body sizing**: bodies render at true scale when `radius × scale
  ≥ 4 px`, capped at 64 px; otherwise tier buckets (1 small / 2 terrestrial
  / 4 gas giant / 6 star). System primary is a hollow ring + filled center
  to distinguish from planets.
- **Textured body disks** (v0.7.2.1+): when `r ≥ 12 px`, Earth and
  Moon render per-pixel via `Canvas.FillTexturedDiskTagged` +
  `render.{Earth,Moon}PixelColor`. Orthographic projection from
  (dx, dy) to (lat, lon) drives an ellipse-table lookup classifying
  each pixel — Earth: cloud / land / ocean; Moon: bright crater
  ray / mare / highland (canonical near-side layout). The body-
  identity glyph (●) is suppressed when the texture is active so
  it doesn't blot the surface detail. Static — no rotation tied to
  sim time. `render.TextureFor(b, pxRadius)` is the dispatch hook
  for future bodies (Mars caps, Jupiter banding, etc.).
- **Vessel orbit ellipse**: live Keplerian orbit drawn dotted (stride 3)
  in the craft's primary frame, in `ColorCurrentOrbit` pale slate
  (v0.6.1; was white — distinct from any body palette). Hyperbolic /
  degenerate orbits skipped (the SOI-segmented preview covers those).
  v0.6.1: ellipse hidden when `apoapsis × scale < minOrbitPixels` so
  the orbit doesn't render as a one-cell blob over the parent body
  at heliocentric zoom.
- **Apo / peri markers**: filled disks at ν=0 (peri, 2 px) and ν=π (apo,
  3 px) so low-eccentricity orbits still show their two extremes at a
  glance. Hidden when the orbit ellipse is suppressed.
- **Vessel marker**: 5-pixel chevron oriented along velocity when the
  orbit ellipse is visible; swaps to a single bright `ColorCraftMarker`
  disk at sub-orbit zoom (v0.6.1) so the craft reads as a recognisable
  pixel rather than a sprawling chevron over the parent body.
- **Per-leg colored trajectory preview** (v0.6.1): each planted node's
  post-burn orbit renders in its own color from a 4-cycle palette
  (cyan / mint / amber / pink). Node-marker clusters take the same
  color so the (marker, post-burn-orbit) pair reads as a matched
  group at a glance. Each leg's window runs from its node's burn
  centre to the next node (or one full period if last). Frame-rebase:
  legs planted in destination frames (Hohmann arrival in Mars frame)
  predict from there instead of being skipped. Suppressed during
  active burns (live state mutates each integrator step).
- **Camera focus** (`f`/`F`/`g`): cycles system-wide / each body / craft.
  FocusCraft auto-fits to ~3× current altitude. v0.6.1: NewWorld
  spawns with `Focus = FocusCraft`.

### HUD
- Clock + warp + paused indicator.
- Focus block: focused-target name + permanent **VIEW** sub-line
  showing the active projection (top / right / bottom / left /
  orbit-flat) — replaces the v0.6.4 toast that flashed for 2 s on
  `v` press (v0.6.5+).
- Vessel block: name, primary, altitude, velocity, apoapsis, periapsis,
  inclination, plus **PERIAPSIS BELOW SURFACE** alert when periapsis altitude
  goes negative.
- Propellant: fuel, total mass, Δv budget remaining (rocket equation).
- Active-burn block (when in flight): mode, Δv-to-go, T-remaining.
- Planned nodes: list with mode / Δv / time-to-fire / impulsive vs
  finite tag. Unresolved event-relative nodes show their trigger label
  ("next peri") instead of T+ until the resolver fires (v0.6.0+).
- **Projected orbit** (v0.6.1, shown when ≥1 resolved node and no
  active burn): apo / peri / AN / DN of the chained post-burn orbit
  via `World.PredictedFinalOrbit`. Rebases into each node's intended
  PrimaryID before applying its Δv, so a Hohmann arrival's projected
  orbit reports as Mars-frame, not Sol-frame. Suppressed during
  active burns to avoid flailing values as live state mutates.
- **Mission** block (v0.6.5+, shown when at least one mission is
  loaded): active mission name + status, or "N/total complete" once
  every loaded mission reaches a terminal state. The first
  `InProgress` mission in `World.Missions` is surfaced via
  `World.ActiveMission`.
- Selected body: name, type, semimajor axis, eccentricity, period, plus
  Hohmann preview when applicable.

### Missions (v0.6.5)
- `internal/missions` package: typed predicate machine over the
  spacecraft's (primary, state, sim-time) tuple. Three predicate
  kinds dispatched on `Mission.Type`:
  - `circularize` — craft is in the named primary's frame, orbit is
    bound, eccentricity ≤ cap, semimajor axis within ±tol of
    `radius + altitude_m`.
  - `orbit_insertion` — craft is in the named primary's frame on a
    bound orbit (e < 1).
  - `soi_flyby` — any tick where craft's current primary ID matches
    the named body.
- Three-state machine (`InProgress` → `Passed` | `Failed`) with sticky
  terminal states; `Mission.Evaluate(EvalContext)` is idempotent on
  Passed/Failed so the per-tick caller blindly walks the slice.
- Embedded starter catalog (`internal/missions/missions.json` via
  `go:embed`): "Circularize at 1000 km LEO" (e ≤ 0.005, ±5% on `a`),
  "Luna orbit insertion" (bound orbit around moon), "Mars SOI
  flyby". `missions.DefaultCatalog()` returns the parsed catalog.
- `World.Missions` seeded at `NewWorld`, evaluated each Tick after
  `executeDueNodes` so a burn that completes a circularization
  passes on the same tick the burn ends.
- Save schema v2 → v3 with `Payload.Missions []missions.Mission`
  (omitempty). v1/v2 saves wire-out nil and get the embedded
  catalog seeded fresh in `worldFromPayload` — older saves gain
  the feature transparently. v3 saves round-trip status verbatim.

### Body hierarchy & moons (v0.5.0)
- `bodies.Body.ParentID` enables arbitrary-depth `parent → child` refs.
  Empty ParentID = top-level body (orbits the system primary).
- `BodyPosition` recurses: moon position = parent's inertial position
  + moon's position relative to parent.
- `bodyInertialVelocity` recurses: moon's inertial velocity = parent's
  inertial velocity + moon's velocity relative to parent.
- `physics.FindPrimary` uses each body's actual parent for SOI sizing
  (Luna→Earth, Phobos→Mars), so nested-SOI walks pick the innermost
  containing body correctly. Also reaches into the warp-lock chunk
  cap so foreign-SOI proximity stays accurate post-hierarchy.
- Moon catalog: Luna, Phobos, Deimos, the four Galilean (Io, Europa,
  Ganymede, Callisto), Titan, Enceladus — single-moon (Earth) and
  multi-moon (Jupiter, Saturn) primaries both exercised.
- Transfer planning to/from moons is **shipped both directions**:
  same-primary intra-primary transfers (craft in LEO → Luna, both
  around Earth) ship in v0.5.7 via `planner.PlanIntraPrimaryHohmann`,
  with v0.5.9 adding phase correction so the craft actually
  rendezvous with the target. The reverse — craft inside a moon's
  SOI returning to its parent (Luna → Earth) — ships in v0.6.3 via
  `planner.PlanMoonEscape`: bound transfer ellipse with apolune at
  the moon's SOI radius, zero-Δv frame marker at SOI exit, player
  plants their own circularization once they see the post-escape
  parent-frame trajectory. The departure burn reuses v0.6.2's
  `IterateForTarget` for finite-burn refinement. Wider inter-SOI
  capture (heliocentric → moon-of-other-planet, Phobos from Mars,
  Titan from heliocentric) is **not** in v0.6 scope.

### Systems loaded
- **Sol** (playable — craft spawns here).
- **Alpha Centauri**, **TRAPPIST-1**, **Kepler-452** (viewable; craft does
  not yet move between systems).
- **User overlay** (v0.7.0+): JSON files in
  `$XDG_CONFIG_HOME/terminal-space-program/systems/*.json` merge with
  the embedded set via `bodies.LoadAllWithWarnings`. User files win on
  `systemName` match (e.g. dropping a `sol.json` replaces the embedded
  Sol entirely); otherwise they append. Body-info screen tags the
  source so the player can tell which catalog a body came from.
  Malformed user files print a warning to stderr at startup and are
  skipped; embedded systems always load.

### Distribution
- **GoReleaser** matrix: linux + darwin amd64/arm64, windows amd64.
- `CGO_ENABLED=0`, `-ldflags "-s -w"` static binaries.
- CI: `go test ./...` on every PR.

---

## 2. Backlog

### From v0.3 testing — small polish items
- **Lambert multi-branch selection**. Today the multi-rev path returns the
  first root the bracket finds (lower-z side); a per-N "cheap" / "long" flag
  would expose both. Useful when porkchop multi-rev lands.
- **Explicit retrograde flag for `LambertSolve`** *(claimed by v0.7.5)*.
  Today direction is driven by the bracket starting point; a caller
  hint would be cleaner.

### Larger queued features
- **Realistic finite-burn intra-primary auto-plant** (v0.6 target).
  v0.5.10's S-IVB-1 default + finite-burn return drops gravity-rotation
  loss to <0.1% for the Earth → Luna profile, but the underlying
  numerical-iteration planner is still v0.6 work for less-favorable
  thrust profiles (low-TWR stages, longer burns). The "right"
  implementation: finite-burn-aware planner that integrates a candidate
  Δv, measures resulting apoapsis, iterates Newton-style until it hits
  target. Composes naturally with the v0.6 burn-at-next scheduler.
  Closes the remaining ~21% apoapsis over-shoot from asymmetric burn
  termination once it lands.
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
- **Multi-craft control selector**. Today the sim exposes one craft
  (`World.Craft`); UI features that depend on "the active orbit" — focus,
  the maneuver planner's PROJECTED ORBIT, v0.6.4's orbit-perpendicular
  view-mode basis — implicitly point at it. When multiple craft come
  online, those features need a craft-control selector key (e.g. `[`/`]`
  cycle, or click-to-select on the orbit canvas) so "active craft" is
  unambiguous and per-screen. Surfaces during v0.6.4 view-mode work as a
  flagged future requirement.
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
- **Inclination-change planner** *(claimed by v0.7.4)*. Today's burn
  modes don't expose a clean out-of-plane corrector. Adds a third burn
  at the line-of-nodes.
- **N-body perturbations**. The sim is strict patched-conic; Lagrangian
  points and three-body trajectories aren't representable.
- **Custom systems via config file** *(claimed by v0.7.0)*.
  `bodies/systems.go` is hardcoded; a JSON overlay loader at
  `$XDG_CONFIG_HOME/.../systems/*.json` unlocks modding.
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

### Polish / quality
- **Race-detector CI**. Currently no `-race` because the local environment
  doesn't have cgo; CI could enable it with `CGO_ENABLED=1`.
- **Throttle control** *(claimed by v0.7.3)*. Engine is on/off — adding
  a 0–100 % `Throttle` field on `Spacecraft` (with `z/x` keys + ±10 %
  step on Shift) plus a per-node throttle override in the `m` form
  makes finite burns more flexible. Pairs with hold-to-burn manual
  flight in the same slice.
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
| v0.7.5 | | Explicit retrograde flag for `LambertSolve` + `LambertSolveRev` + `PorkchopGrid` (unblocks multi-rev porkchop in v0.8+) |
| v0.8+ | Open *(speculative)* | Multiplayer implementation, multi-rev porkchop, multi-craft selector, N-body, multi-system spacecraft, mission editor/scripting, optional drag, maneuver node drag-to-edit |

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

We're now at the **v0.6 ship boundary**, with v0.7 (modding + manual
flight + planner polish) the next cycle. Slice breakdown lives in
`docs/v0.7-plan.md`. The questions framed for the v0.5 → v0.6 boundary
all resolved during v0.6 implementation (see §4); what carries forward,
plus what v0.6 newly opened, framed for v0.7 scoping:

- **`Body` schema additions for the v0.7 external loader** *(now in
  scope)*. v0.7.0 lands the `$XDG_CONFIG_HOME/.../systems/*.json`
  overlay loader; v0.7.1 promotes the `internal/render/palette.go`
  table to per-body `Color` field. Whether `Rings` / `Glyph` overrides
  ride per-body JSON or stay hardcoded in `render.BodyRings` /
  `render.GlyphFor` is a v0.7.0 / v0.7.1 implementation question.
- **Inter-SOI `PlanTransfer()` capture** *(partially answered, still
  open beyond v0.7)*. v0.5.7's `PlanIntraPrimaryHohmann` covers
  same-parent transfers (LEO → Luna), v0.5.9 added phase correction,
  v0.6.3 added the moon → parent return direction (Luna → Earth).
  **Remaining beyond v0.7**: fully cross-primary capture —
  heliocentric cruise → Mars-orbit insertion through Phobos's SOI, or
  heliocentric → a Galilean satellite. Needs a real patched-conic
  capture pass; deferred to v0.8+.
- **SOI-exit HUD surfacing** *(newly open from v0.6.3)*. The
  parent-frame zero-Δv arrival marker that `PlanMoonEscape` plants
  today only shows up as a "next-frame" trigger; a more explicit HUD
  callout for the SOI transition would help users plan their
  parent-frame circularization. Slice it into v0.7 polish or leave for
  v0.8 — open.
- **Caller-facing `IterateForTarget` toggle** *(newly open from
  v0.6.2)*. The Newton-iterating finite-burn solver lives behind
  `H` auto-plant only. Whether the `m` form should expose a "plan
  with finite-burn iteration" toggle for player-planted burns is
  a v0.7.3 / v0.7.4 polish question.
- **Predictor adaptive sampling at high warp** *(carried from
  `docs/integration-design.md` §10)*. The fixed 96-sample horizon
  collapses to a smear at 10000× warp on LEO orbits. Adaptive
  sampling (sample density inversely proportional to warp) is the
  obvious fix; not yet sliced. Likely v0.8+ unless v0.7 surfaces
  it as a player-visible regression.
- **Theme override granularity** *(newly open for v0.7.2)*. Whether
  per-body color overrides belong in the same `theme.json` as
  UI-tier overrides (the v0.7.2 plan), or in a sibling
  `bodies.json` overlay that can also tweak orbital parameters.
  Resolve before v0.7.2 implementation.
- **Multi-craft selector vs multiplayer** *(newly open from v0.6.6
  design spike, open question #1)*. The MP design doc flagged
  craft-control-selector sequencing as a question. Resolve before
  committing to a multi-craft v0.7 / v0.8 slice.
- **Body rendering — rotation, lighting, eclipses** *(newly open
  from v0.7.2.1 / v0.7.2.2)*. Earth + Moon now have per-pixel
  textures but they're fully lit, statically oriented, and don't
  occlude each other. Four follow-ons sketched in §2 ("Body
  rendering — open scope"): (1) surface rotation tied to sim time,
  (2) more features per body (Mars caps, Jupiter banding, etc.),
  (3) solar lighting + day/night terminator, (4) eclipses as
  render artifacts. Sequencing: do (1) / (3) belong as v0.7.2.x
  polish patches, or v0.8+? (2) can trickle one-body-per-patch
  like v0.5.11 / v0.5.12 did. (4) gates on (3).
- **Monopropellant / RCS mode** *(newly open from v0.7.3 manual-
  flight UX)*. Future system for sub-m/s precision burns —
  micro orbit adjustments, encounter refinement, docking. Sketch
  in §2: separate propellant pool (`Spacecraft.Monoprop`),
  separate thruster profile (~50 N, Isp ~220 s, total Δv ~6 m/s),
  mode toggle (`r`?), pulse model (tap = ~100 ms burst). Open
  design questions: docking model (state-transition vs full
  proximity ops), encounter precision target, sequencing relative
  to multi-craft work, pulse quantum tuning. Likely v0.8 territory
  — pairs with the multi-craft + multiplayer threads since
  docking implies two craft.

Update this doc on each minor/patch boundary so the snapshot stays current.
