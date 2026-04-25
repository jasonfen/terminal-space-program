# terminal-space-program — state of game

*Snapshot at v0.5.15 (April 2026) — v0.5 cycle closed; v0.6 in scoping.
Updated at each minor / patch boundary.*

`docs/plan.md` is the original architecture / phase plan. This doc complements it
with a "what plays today, what's queued next" view organised around player-facing
features and the version sequence that delivers them.
`docs/v0.5-release-notes.md` covers the v0.5.0 → v0.5.15 release series patch
by patch — this doc is the snapshot, those are the release notes.

---

## 1. What works today (v0.5.15)

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
- Default vessel ("ICPS-1"): 3500 kg dry + 25000 kg fuel, Isp 462 s, Thrust 108 kN, spawned
  in 200 km LEO.

### Planning
- **Manual planner** (`m`): three-field form (mode / Δv / duration). `Tab`
  cycles fields, `←/→` cycles modes when mode field is focused, Δv > budget
  warns, duration 0 = impulsive, > 0 = finite.
- **`n` quick-plant**: T+5 min prograde 200 m/s, finite (sized to thrust).
- **`H` auto-plant Hohmann transfer**: select target body, one keystroke
  plants two finite nodes (geocentric departure + destination-frame arrival)
  with `Duration = Δv × mass / thrust`. Frame-aware via `ManeuverNode.PrimaryID`.
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
- **Vessel orbit ellipse**: live Keplerian orbit drawn dotted (stride 3) in
  the craft's primary frame, translated into the system frame for cross-frame
  rendering. Hyperbolic / degenerate orbits skipped (the SOI-segmented
  trajectory preview covers those).
- **Apo / peri markers**: filled disks at ν=0 (peri, 2 px) and ν=π (apo, 3
  px) so low-eccentricity orbits — visually near-circular — still show their
  two extremes at a glance.
- **Vessel arrow glyph**: chevron rotated into the velocity direction.
- **SOI-segmented trajectory preview**: post-burn trajectory partitioned by
  the dominant primary at each sample. Inside-home-SOI uses stride-2 dashed,
  foreign SOI uses stride-1 solid. Continuity fixed at boundary crossings.
- **Camera focus** (`f`/`F`/`g`): cycles system-wide / each body / craft.
  FocusCraft auto-fits to ~3× current altitude.

### HUD
- Clock + warp + paused indicator.
- Vessel block: name, primary, altitude, velocity, apoapsis, periapsis,
  inclination, plus **PERIAPSIS BELOW SURFACE** alert when periapsis altitude
  goes negative.
- Propellant: fuel, total mass, Δv budget remaining (rocket equation).
- Active-burn block (when in flight): mode, Δv-to-go, T-remaining.
- Planned nodes: list with mode / Δv / time-to-fire / impulsive vs finite tag.
- Selected body: name, type, semimajor axis, eccentricity, period, plus
  Hohmann preview when applicable.

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
- Transfer planning to/from moons is **partially shipped**: same-primary
  intra-primary transfers (craft in LEO → Luna, both around Earth) ship
  in v0.5.7 via `planner.PlanIntraPrimaryHohmann`, with v0.5.9 adding
  phase correction so the craft actually rendezvous with the target.
  Full inter-SOI patched-conic capture (Earth → Mars-orbit insertion
  through Phobos's SOI etc.) still slips to v0.6 alongside the planner
  UX work.

### Systems loaded
- **Sol** (playable — craft spawns here).
- **Alpha Centauri**, **TRAPPIST-1**, **Kepler-452** (viewable; craft does
  not yet move between systems).

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
- **Explicit retrograde flag for `LambertSolve`**. Today direction is driven
  by the bracket starting point; a caller hint would be cleaner.

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
- **Mission system / objectives** (v0.6 target, see §3). Starter objectives
  such as "achieve a 1000 km circular orbit," "intercept Mars within Δv X."
- **Burn-at-next scheduler** (v0.6 target). Event-relative maneuver nodes:
  fire at next periapsis / apoapsis / ascending node / descending node.
  Foundation for richer triggers (at-SOI-exit, at-fuel<X). v0.6 also
  extends `PlanTransfer()` to handle the inter-SOI capture case (e.g.
  Earth → a moon of Mars, or a Galilean satellite from heliocentric
  cruise) — v0.5.7's intra-primary path covers same-parent transfers
  but the cross-primary capture pass is still missing.
- **Multi-system spacecraft**. The craft is currently locked to Sol. Allowing
  it to enter Alpha Cen / TRAPPIST / Kepler unlocks the system-cycle UX.
  Requires interstellar transfer math (or deus-ex-machina jump for now).
- **Inclination-change planner**. Today's burn modes don't expose a clean
  out-of-plane corrector. Adds a third burn at the line-of-nodes.
- **N-body perturbations**. The sim is strict patched-conic; Lagrangian
  points and three-body trajectories aren't representable.
- **Custom systems via config file**. `bodies/systems.go` is hardcoded; a
  TOML or JSON loader unlocks modding.
- **Mission editor / scripting** (long-tail). Once basic missions exist,
  expose a config format so users can author custom objectives without
  touching Go.
- **Optional simple atmospheric drag model** (opt-in, off by default). Toy
  drag below ~150 km to enable reentry / aerobraking gameplay; patched-conic
  two-body stays the primary integrator. Previously on "excluded forever",
  softened here — only *realistic* multi-species drag stays out of scope.
- **Mouse support** (v0.6 target, see §3). Currently keyboard-only;
  v0.6 adds click-to-focus on bodies, click-to-plant on porkchop cells,
  and drag-to-edit on planted maneuver nodes.

### Visual / UX targets
- **Maneuver node editing**. Nodes are plant-once today; adding drag/scrub
  on a planted node (adjust Δv, duration, fire-time in place) would make
  the planner iterative rather than plant-and-replace. v0.6's mouse work
  delivers the input plumbing; the broader edit-in-place flow can land
  later.
- **HUD typography polish (residual)**. v0.5.13 added section dividers,
  the active-burn ● indicator, and ⚠ alerts; remaining work is alignment
  inside dense panels (vessel, propellant, planned-nodes blocks) and
  consistent column widths across sections.
- **Body details on the canvas (beyond rings + glyphs)**. v0.5.11 ships
  Saturn rings; v0.5.12 ships per-type identity glyphs. Remaining
  texture hints — day/night terminator for Earth, polar caps for Mars,
  Jupiter banding, additional ringed-body data (Uranus, Neptune) —
  would push body identity further.
- **Active-burn flame animation**. The arrow glyph could pulse / extend
  while a burn is firing.

### Polish / quality
- **Race-detector CI**. Currently no `-race` because the local environment
  doesn't have cgo; CI could enable it with `CGO_ENABLED=1`.
- **Throttle control**. Engine is on/off — adding a 0–100 % throttle field
  would make finite burns more flexible.
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
| **v0.6** | **(next) Planner UX + missions + MP design** | Burn-at-next scheduler, mission scaffold, multiplayer design-doc spike, mouse support |
| v0.7 | Custom systems + modding *(speculative)* | Config-file body loader; promote color theme to user-configurable |
| v0.8+ | Open *(speculative)* | N-body, multi-system spacecraft, multi-rev porkchop, mission editor/scripting, optional drag, maneuver node editing, multiplayer implementation |

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

v0.6 bundles four related strands: richer maneuver planning (event-relative
nodes + mouse), the mission scaffold that slipped out of v0.4, and the
first written exploration of what multiplayer would look like.

- **Burn-at-next scheduler.** Today maneuver nodes use absolute T+ time
  only. Add an event-relative mode: "fire at next periapsis / apoapsis /
  ascending node / descending node" computed from live orbit. Foundation
  for richer triggers later ("at SOI exit", "when fuel < X"). Planner UX
  is **hybrid**: common modes (T+ absolute, next peri, next apo) live in
  the existing `m` form via a new `fire at` field; advanced triggers
  (AN / DN, SOI-exit, fuel < X) sit behind a secondary event-trigger
  picker reachable from `m`. Keeps the simple flow one form; gives the
  advanced triggers a place to grow into without bloating `m`.
- **Mouse support.** Promoted from "speculative v0.8+" because v0.6's
  planner UX work is mouse-shaped: porkchop cells want click-to-plant
  (composes with v0.4.1 Enter-to-plant), planted maneuver nodes want
  drag-to-edit, and bodies want click-to-focus as a faster path than
  cycling `f` / `F`. Bubble Tea exposes mouse events natively
  (`tea.MouseMsg`); scope is click + drag + wheel-zoom on the orbit
  canvas, click on HUD panels and porkchop cells. Keyboard remains
  primary — every mouse action has a key equivalent. About 150–250
  LOC + tests. Note: full *maneuver node editing* (in-place Δv /
  duration / fire-time scrub with rich UX) stays in §2 backlog —
  v0.6's mouse work delivers the input plumbing and a basic drag
  interaction; the broader edit-in-place flow can land later.
- **Mission scaffold.** `missions.go` with a couple of starter objectives
  ("circularize at apoapsis," "visit Mars SOI"). Pure pass/fail check
  against current state. Mission state persists via the v0.4.0 save format
  (hence the `version` field shipping in v0.4). About 200 LOC + a
  `missions.json` data file.
- **Multiplayer design-doc spike.** Short `docs/multiplayer-design.md`
  scoped to **networking + persistence**: transport choice, authority
  model, what "shared sandbox" means for time warp, *and* a persistence /
  save-slot story for shared sessions (host-authoritative snapshot vs.
  per-player envelope, conflict resolution on rejoin, how the v0.4 save
  schema accommodates a `session` block). The persistence half is
  explicitly in scope so the v0.4 save envelope can be future-proofed
  deliberately rather than retrofitted. Implementation roadmap (which
  release ships networking, the v0.6.x → v0.9 path) stays **out of scope**
  for the v0.6 spike — the doc records constraints, not a schedule.
  Writing the design now lets v0.6 save-format decisions (and any v0.7
  schema work) stay open to shared-session use cases without committing
  code. Multiplayer leaves the "excluded forever" list because of this.

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

5. **Burn-scheduler UX.** Hybrid. Common triggers (T+ absolute, next
   peri, next apo) live inside `m` via a new `fire at` field; advanced
   triggers (AN / DN, SOI-exit, fuel < X) sit behind a secondary picker
   reachable from `m`.
6. **Multiplayer design scope.** Networking + persistence. The spike
   covers transport / authority / time-warp **and** a persistence /
   save-slot story for shared sessions, so the v0.4 save envelope is
   future-proofed deliberately. Implementation roadmap is explicitly
   *not* in scope for the v0.6 doc — constraints only, no schedule.

---

## 5. Open questions for the next cycle

We're now at the **v0.5 ship boundary**, with v0.6 (planner UX + missions
+ multiplayer design spike) the next cycle. The questions surfaced for
the v0.4-boundary list partially resolved during v0.5; what's left,
plus what v0.5 newly opened, framed for v0.6 scoping:

- **`Body` schema for the v0.7 config-file loader** *(still open)*. What
  the v0.5 hierarchy needs to expose — beyond promoting the palette
  table to per-body JSON — for a clean external loader. Surfaces when
  v0.7 starts; v0.6 should avoid schema decisions that paint into a
  corner.
- **Inter-SOI `PlanTransfer()` capture** *(partially answered)*. v0.5.7's
  `PlanIntraPrimaryHohmann` covers same-parent transfers (LEO → Luna)
  and v0.5.9 added phase correction. **Remaining**: cross-primary
  capture — Earth heliocentric cruise → Mars-orbit insertion that
  passes through (or rendezvous with) one of Mars's moons; or
  heliocentric → a Galilean satellite. Needs a real patched-conic
  capture pass, not just a shared-parent assumption flip. v0.6 work.
- **Mission persistence shape** *(still open)*. Whether v0.6 missions
  persist inside the v0.4 save's `payload` block or alongside it as a
  sibling (e.g. `{"version":1, ..., "payload":{...},
  "missions":{...}}`) — affects save-schema ergonomics more than
  correctness, but worth deciding before missions land.
- **Integration-design event-loop landing** *(newly open)*.
  `docs/integration-design.md` (the v0.4.2–v0.4.4 contract) sketches an
  event-driven outer loop unifying integrator and predictor; that work
  stayed deferred so the v0.5 series could focus on moons + visuals.
  Open: does it land *inside* v0.6 alongside the burn-at-next
  scheduler (they touch the same control loop, so co-shipping is
  natural), or stay its own follow-on after v0.6's planner UX is
  bedded in?

Update this doc on each minor/patch boundary so the snapshot stays current.
