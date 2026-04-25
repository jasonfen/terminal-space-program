# terminal-space-program — state of game

*Snapshot at v0.4.2 (April 2026). Updated at each minor / patch boundary.*

`docs/plan.md` is the original architecture / phase plan. This doc complements it
with a "what plays today, what's queued next" view organised around player-facing
features and the version sequence that delivers them.

---

## 1. What works today (v0.4.2)

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
- **Finite-duration burns**. `Spacecraft.Thrust = 10 kN` default; a node's
  `Duration` field controls integration. Mass flow `dm/dt = -Thrust/(Isp·g0)`
  debits fuel each sub-step; burn ends on Δv delivered, fuel exhausted, or
  duration elapsed (whichever first).
- **Six burn modes**: prograde, retrograde, normal±, radial±. Direction is
  recomputed each sub-step from live (r, v) so held-prograde follows the
  rotating velocity frame.
- Default vessel: 500 kg dry + 500 kg fuel, Isp 300 s, Thrust 10 kN, spawned
  in 200 km LEO.

### Planning
- **Manual planner** (`m`): three-field form (mode / Δv / duration). `Tab`
  cycles fields, `←/→` cycles modes when mode field is focused, Δv > budget
  warns, duration 0 = impulsive, > 0 = finite.
- **`n` quick-plant**: T+5 min prograde 200 m/s, finite (sized to thrust).
- **`P` auto-plant Hohmann transfer**: select target body, one keystroke
  plants two finite nodes (geocentric departure + destination-frame arrival)
  with `Duration = Δv × mass / thrust`. Frame-aware via `ManeuverNode.PrimaryID`.
- **`k` porkchop plot**: ASCII heatmap over departure-day × time-of-flight,
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
- **Enter-to-plant from porkchop cursor**. Reuses `World.PlanTransfer` once
  it accepts explicit (departure offset, TOF) params. Slated for v0.4.1.
- **Explicit retrograde flag for `LambertSolve`**. Today direction is driven
  by the bracket starting point; a caller hint would be cleaner.

### Larger queued features
- **Save / load** (v0.4.0 target). No state persistence today — close the
  program and your orbit is gone. JSON state file at
  `$XDG_STATE_HOME/terminal-space-program/save.json`, manual `S` / `L`, autosave on quit.
- **Mid-course corrections** (v0.4.1 target). Auto-plant produces a single
  two-impulse plan; no replanning during the coast. A "refine arrival"
  key re-runs Lambert from the live state and updates the planted arrival
  node; paired with porkchop Enter-to-plant so the cursor can feed the
  same path.
- **Mission system / objectives** (v0.6 target, see §3). Starter objectives
  such as "achieve a 1000 km circular orbit," "intercept Mars within Δv X."
- **Burn-at-next scheduler** (v0.6 target). Event-relative maneuver nodes:
  fire at next periapsis / apoapsis / ascending node / descending node.
  Foundation for richer triggers (at-SOI-exit, at-fuel<X). v0.6 also
  extends `PlanTransfer()` to handle Earth → Luna (and any planet → moon)
  transfers — needs an inter-SOI capture pass that today's shared-primary
  assumption skips.
- **Body hierarchy + moons** (v0.5.0 target, see §3). Today's body model
  is flat star → planet. v0.5.0 adds arbitrary-depth `ParentID` refs,
  hierarchical SOI lookup, and recursive `BodyPosition()` so Luna,
  Phobos/Deimos, the Galilean four, Titan, and Enceladus all orbit
  their planets in a single release.
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
- **Color** (v0.5 target). The renderer is monochrome. Hardcoded palette
  first; planets and the sun colored reminiscent of their actual appearance
  (Sol gold-white, Mars rust, Jupiter banded ochre, Neptune deep blue), with
  a temperature-based tint for non-Sol stars (TRAPPIST-1 red dwarf, Alpha
  Cen A yellow). Promoted to user-configurable in v0.7 alongside the
  systems config-file loader.
- **Vessel position trail**. A fading dotted history of where the craft has
  *actually* been, distinct from the current orbit ellipse.
- **Maneuver node editing**. Nodes are plant-once today; adding drag/scrub
  on a planted node (adjust Δv, duration, fire-time in place) would make
  the planner iterative rather than plant-and-replace.
- **HUD typography polish**. Dense lipgloss panels could read cleaner with
  better spacing, dividers, alignment.
- **Body details on the canvas**. Today bodies are filled disks; texture
  hints (poles, rings for Saturn, day/night terminator for Earth) would add
  identity.
- **Better porkchop axis labels**. Current implementation cuts off; needs a
  proper axis-label renderer.
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
| v0.4.2 ✓ | (current) | Per-sub-step SOI check in live integrator (high-warp orbit drift fix) |
| **v0.5** | **Moons + visual enhancement** | Body hierarchy + Luna/Phobos/Deimos/Galilean/Titan/Enceladus (v0.5.0), then color (palette.go, realistic palette), vessel trail, HUD polish, body identity |
| **v0.6** | **Planner UX + missions + MP design** | Burn-at-next scheduler, mission scaffold, multiplayer design-doc spike, mouse support |
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

None open. All v0.4 → v0.6 scoping questions are resolved (see §4
current cycle). The next scoping round naturally re-opens at the v0.4
ship boundary — expected new questions:

- What the v0.5 `Body` schema needs to expose for the v0.7 config-file
  loader (beyond moving the palette table into per-body JSON).
- How `PlanTransfer()` extends to Earth → Luna once the hierarchy is in
  (v0.6 work, surfaces during mid-course-correction integration).
- Whether v0.6 missions persist in the `payload` block or alongside it
  as a sibling (e.g. `{"version":1, ..., "payload":{...},
  "missions":{...}}`) — affects save-schema ergonomics more than
  correctness.

Update this doc on each minor/patch boundary so the snapshot stays current.
