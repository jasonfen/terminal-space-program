# terminal-space-program

Terminal-native orbital-mechanics rocket simulator. A take on Kerbal Space
Program that lives in your terminal, distributed as a single static Go binary.

```
┌───────────────────────────────────────────────────────────┐
│ terminal-space-program — Sol         [Menu]  [Missions]   │
│ ┌─────────────────────────────────────┐ ┌───────────────┐ │
│ │    · ·                              │ │ CLOCK         │ │
│ │  ·     ·                            │ │   T+2026-04-29│ │
│ │ ·   ⊙   ·       · ⊕ ·               │ │   warp: 100x  │ │
│ │  ·     ·                            │ │               │ │
│ │    · ·                              │ │ VESSEL  PROP  │ │
│ │                                     │ │ S-IVB-1 fuel  │ │
│ │                              view:  │ │ alt 500 26000 │ │
│ └────────────────────────────── orbit ┘ │ v 7.6   Δv 6k │ │
│ [q] quit [s] system [m] burn [?] help   └───────────────┘ │
└───────────────────────────────────────────────────────────┘
```

## Install

Latest release: **v0.8.2**.

```bash
# Linux x86_64
curl -L https://github.com/jasonfen/terminal-space-program/releases/latest/download/terminal-space-program-linux-amd64.tar.gz | tar xz
./terminal-space-program
```

Replace `linux-amd64` with `linux-arm64`, `darwin-amd64`, `darwin-arm64`, or
`windows-amd64` (use the `.zip` variant on Windows).

No Go toolchain, no libc dance. `CGO_ENABLED=0` static binaries.

## Build from source

```bash
git clone https://github.com/jasonfen/terminal-space-program
cd terminal-space-program
go build ./cmd/terminal-space-program
./terminal-space-program
```

Requires Go 1.24+ (bubbletea dependency chain).

## Quick tour

You spawn as **S-IVB-1** in a 500 km circular prograde LEO. The left
panel is the orbit canvas — Sun (or whichever body you focus on) at
the center, planets on their actual orbits, your spacecraft as a
small chevron oriented along velocity. The right HUD shows clock,
focus, vessel + propellant state, attitude, planned nodes, projected
post-burn orbit, frame transitions, system info, and a Hohmann preview
to the cursor-selected body. Time-warp with `.` / `,` to watch planets
move; pause with `0` or space.

To make something happen:

1. Press `←`/`→` (or click) to scroll the cursor through bodies. Pick Mars.
2. Press `H` to plant a Hohmann transfer — two finite-burn nodes
   appear (geocentric departure + Mars-frame arrival), each
   color-coded with its predicted post-burn orbit on the canvas,
   listed in the HUD with Δv and time-to-fire.
3. Time-warp forward. The departure node fires at its trigger time;
   warp clamps to ≤10× during the burn so the integrator keeps
   temporal resolution. Your trajectory unrolls past Earth's SOI,
   the predictor switches frames, and the curve bends sunward as
   it should.
4. The arrival node fires near Mars and drops you into a low
   capture orbit. For phasing-aware launch windows, use `P`
   (porkchop) instead of `H`.

For burns by hand, press `m` to open the planner. Pick a mode
(prograde / retrograde / normal± / radial±), choose when it fires
(absolute T+, or *next peri / next apo / next AN / next DN*), set
Δv, and pick a throttle. Burn duration is derived from Δv via the
rocket equation — no separate field. The PROJECTED ORBIT block
previews apo / peri / AN / inclination of the result live as you
edit. Commit with Enter; the integrator switches from Verlet (free
flight) to RK4 (thrust) and ticks the burn across multiple frames
with mass loss tracked from the rocket equation.

For real-time stick (KSP-style), throttle up with `z`, attitude
with `w`/`s`/`a`/`d`/`q`/`e`, then engage with `b`.

## Keybindings

The in-game `?` overlay is the source of truth; this table mirrors it.

### Global

| Key | Action |
|---|---|
| `Esc` | Back / open splash menu (save / load / quit) on home view |
| `Ctrl+C` | Quit immediately |
| `?` | Toggle help overlay |
| `i` | Body info screen |
| `Tab` | Switch system (Sol → Alpha Cen → TRAPPIST-1 → Kepler-452) |
| `0`, `Space` | Pause / resume sim |
| `.` / `,` | Warp up / down (1× → 100000×; clamped to ≤10× during a burn) |

### Orbit view

| Key | Action |
|---|---|
| `→` / `l` | Cursor: next body |
| `←` / `h` | Cursor: previous body |
| `+` / `-` | Zoom in / out |
| `f` / `F` | Cycle camera focus forward / backward (system → bodies → craft) |
| `g` | Reset camera focus to system |
| `v` | Cycle view (top → right → bottom → left → orbit-flat) |
| `n` | Plan a default node (T+5 min, prograde, 200 m/s, finite-sized) |
| `N` | Clear all planned nodes |
| `H` | Auto-plant Hohmann transfer to selected body (intra-primary for moons of the craft's parent; moon → parent escape via bound transfer ellipse) |
| `I` | Plant inclination match — rotates the orbital plane to the selected body's inclination (or 0° equatorial when no body is selected) |
| `P` | Porkchop plot for selected body; `Enter` on a cell plants that Lambert transfer. Inter-primary only — moon targets show a banner redirecting to `H` |
| `R` | Refine plan — re-Lambert from live state, plant mid-course correction + update arrival |
| `m` | Open maneuver planner |
| `S` / `L` | Save / load game (`$XDG_STATE_HOME/terminal-space-program/save.json`) |

### Manual flight

| Key | Action |
|---|---|
| `z` / `x` | Throttle full / cut |
| `Z` / `X` | Throttle +10 % / -10 % |
| `w` / `s` | Attitude prograde / retrograde |
| `a` / `d` | Attitude normal+ / normal- |
| `q` / `e` | Attitude radial+ / radial- |
| `b` | Engage / cut manual burn (main engine, throttle > 0) |
| `r` | Engine: main / RCS (v0.8.0+) |

Attitude keys orient only in main mode — pressing `b` is what fires
the engine. In RCS mode the attitude keys *also* fire one 0.1 m/s
monoprop pulse per keypress (held keys produce a sustained pulse
train at the terminal's key-repeat rate). The HUD's ATTITUDE block
shows the armed engine; the PROPELLANT block shows monoprop level
and remaining RCS Δv.

### Mouse

Click-only. No drag, no wheel-zoom.

| Click target | Action |
|---|---|
| `[Menu]` (top-right) | Save / load / quit confirmation menu |
| `[Missions]` (top-right) | Mission list with status glyphs (✓/✗/·) |
| Body | Focus body (same as cursoring onto it with `←` / `→`) |
| Vessel | Focus craft |
| Planted node | Open planner pre-loaded for that node (edit-replace, fire time preserved) |
| Empty canvas | Open planner with a new node staged at the orbit point nearest the click |
| HUD panel | Open body info |
| Porkchop cell | Move cursor to that cell (then `Enter` to plant) |

### Maneuver planner (`m`)

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Cycle field focus (mode → fire-at → Δv → throttle) |
| `←` / `→` | Cycle the focused cycle field (mode or fire-at trigger) |
| digits / backspace | Edit Δv or throttle |
| `Enter` | Commit burn |
| `Esc` | Cancel and back to orbit view |

Δv drives both delivered Δv **and** burn duration via the rocket
equation `t = (m₀/ṁ)·(1 − exp(−Δv/(Isp·g₀)))`. Zero-thrust craft
fall back to impulsive automatically. The fire-at field selects
absolute T+ or one of `next peri / next apo / next AN / next DN`;
event-relative nodes resolve their trigger lazily on the first tick
after plant. The throttle field is per-node (not the live craft
throttle) so adjusting throttle during a coast doesn't slow an
in-flight planted burn. PROJECTED ORBIT readouts apo / peri / AN /
inclination of the resulting orbit live as you edit.

### Porkchop plot (`P`)

| Key | Action |
|---|---|
| `←` / `→` | Departure-day cursor |
| `↑` / `↓` | Time-of-flight cursor |
| `Enter` | Plant Lambert transfer for selected cell |
| Click cell | Move cursor to that cell (then `Enter` to plant) |
| `Esc` | Back to orbit view |

Cursor opens snapped to the minimum-Δv cell. `·` glyphs mark cells
where Lambert didn't converge — `Enter` on those is a no-op.

## Features

- **Two-body patched-conic propagation** with SOI-aware state
  transitions. Symplectic Verlet for free flight, RK4 for active
  burns. Stumpff-universal-variables Lambert solver (Curtis
  Algorithm 5.2) with an explicit prograde/retrograde flag.
- **Auto-plant transfers**. `H` plants Hohmann (heliocentric,
  intra-primary, or moon-escape — the planner picks based on
  craft + target frames). `P` shows a porkchop heatmap; `Enter`
  on a cell plants the Lambert-derived transfer. `R` re-runs
  Lambert from live state to refine the planted arrival.
- **Burn scheduling**. Maneuver nodes fire at absolute T+ or on
  the next periapsis / apoapsis / AN / DN crossing — the planner
  resolves event-relative nodes lazily against the live orbit.
  Inclination-change planner (`I`) plants a single normal-burn
  to rotate the orbital plane.
- **Per-node throttle**. The maneuver form's throttle field is
  captured at fire-time onto `ActiveBurn`, so adjusting the live
  throttle knob mid-coast doesn't slow a planted burn.
- **Manual flight**. Real-time stick layered on top of the
  planted-node planner. Throttle, six attitude modes, and an
  explicit `b` engage so accidental attitude-key presses can't
  fire the engine.
- **RCS / monopropellant mode** (v0.8.0+). `r` toggles between the
  main engine and a precision-maneuver monoprop thruster pool.
  In RCS mode each attitude keypress fires a fixed 0.1 m/s pulse
  off the monoprop budget (~30 m/s on the default S-IVB-1) — the
  proximity-ops thruster v0.8.3 docking will lean on. Each pulse
  drops a fading puff marker on the canvas (placeholder visual;
  v0.8.2 replaces with per-thruster glyphs).
- **Multi-craft slate** (v0.8.1+). `n` opens the spawn form
  (loadout / parent body / altitude / direction); `[`/`]` cycles
  which craft the player is flying. Each craft owns its own
  planted nodes, in-flight burn, attitude, and engine state —
  burns fire on the craft they were planted for regardless of
  which craft you're currently flying. The HUD's `BURNS` and
  `NODES` blocks list every craft's state simultaneously; clicking
  a node row opens the maneuver planner for edit-replace. Title
  bar shows `CRAFT N/M` chip when more than one craft is loaded.
  Save schema bumped v4 → v5 to nest per-craft state.
- **Craft types** (v0.8.2+). Four loadouts in the launch catalog:
  S-IVB-1 (yellow `▲`, J-2 third stage), ICPS (blue `◆`, RL-10
  low-TWR), RCS-tug (pink `●`, pure monoprop, no main engine),
  Lander (mint `▼`, throttleable descent stage). Each carries
  propulsion + visual differentiation; the orbit canvas renders
  every craft with its loadout glyph + color so they read
  distinctly even at small zoom.
- **Capture preview** (v0.8.2+). Plant a Hohmann to another body
  and the HUD's `CAPTURE PREVIEW` block shows what you'll arrive
  with — relative approach speed and predicted prograde /
  retrograde direction (a prograde Hohmann to Luna naturally
  captures retrograde, ~110° around Luna; the preview surfaces
  this before fire so you're not surprised). Inclination match
  also works from equatorial source orbits.
- **Predicted post-burn orbit**. PROJECTED ORBIT block on both
  the orbit screen and `m` form chains every planted node, frame-
  rebases per node (so a Hohmann arrival reads in the destination
  frame), and shows apo / peri / AN / inclination of the
  resulting orbit live.
- **Frame transitions**. The HUD surfaces upcoming SOI / frame
  changes implied by the planted-node chain (e.g. the zero-Δv
  arrival marker from a moon-escape).
- **Body hierarchy + moons**. Earth + Luna, Mars + Phobos /
  Deimos, the four Galilean moons, Titan, Enceladus. Recursive
  `BodyPosition` / `bodyInertialVelocity` so SOI math walks the
  hierarchy correctly.
- **Per-pixel body textures**. Earth (continents + polar caps +
  deserts + cloud streaks), Moon (canonical near-side maria +
  bright rayed craters), Mars (Syrtis Major / Solis Lacus /
  polar caps), Jupiter (10-band zone/belt scheme + Great Red
  Spot). Render at body radii ≥ 12 px; below that bodies fall
  back to a colored disk.
- **Missions**. Three predicate kinds (`circularize` /
  `orbit_insertion` / `soi_flyby`) with sticky pass/fail state,
  embedded starter catalog (1000 km LEO circularize, Luna orbit
  insertion, Mars SOI flyby). Reachable via the `[Missions]`
  title-bar button.
- **Persistence**. Save / load to JSON at
  `$XDG_STATE_HOME/terminal-space-program/save.json`. Schema
  v5 round-trips clock, focus, the entire craft slate (each
  craft's RCS pool, planted nodes with per-node throttle,
  in-flight burn, attitude, engine mode), and missions. Pre-v5
  saves auto-migrate (singular Craft → 1-entry slice).
- **Modding**. Custom systems via JSON overlay, per-body color
  via `theme.json` (see *Custom systems* and *Theming* below).
- **Multi-system viewing**. Sol, Alpha Centauri, TRAPPIST-1, and
  Kepler-452. The craft is locked to Sol today; switching
  systems just changes the camera.

## Custom systems

Drop additional system JSON files into
`$XDG_CONFIG_HOME/terminal-space-program/systems/` (or
`~/.config/terminal-space-program/systems/` if `XDG_CONFIG_HOME` is
unset) and they'll merge with the built-in catalog at startup. User
files win on `systemName` match — e.g. dropping a `sol.json` there
replaces the embedded Sol entirely. Otherwise they append. The
body-info screen (`i`) shows `source: embedded | user` so you can
tell which catalog a body came from.

Schema mirrors the embedded files in
`internal/bodies/systems/*.json`. Malformed user files print a
warning to stderr at startup and are skipped — embedded systems
always load. Save files carry a `body_catalog_hash` field, so a
save taken on the embedded catalog rejects on first load after you
add a custom system; that's by design (otherwise body references
across saves could drift silently).

## Theming

Drop a `theme.json` at
`$XDG_CONFIG_HOME/terminal-space-program/theme.json` (or
`~/.config/terminal-space-program/theme.json`) to recolor either the
UI tier or specific bodies:

```json
{
  "ui":     {"alert": "#ff5f5f", "warning": "#ffaf00"},
  "bodies": {"earth": "#3b82f6", "mars":    "#dc2626"}
}
```

Both blocks are optional. UI keys match the lower-cased name of the
package-level `Color*` var (e.g. `alert`, `warning`, `plannednode`,
`trajectory`, `currentorbit`, `craftmarker`, `foreignsoi`, `dim`).
Body keys match each body's `id` from `systems/*.json`. A body
override wins over that body's per-body `color` field; UI overrides
mutate the global tier colors at startup. Malformed `theme.json`
prints a warning to stderr and falls back to defaults.

## Version history

| Cycle | Theme | One-liner |
|---|---|---|
| v0.1 | Foundation | Heliocentric viewer + Verlet integrator + body catalog. |
| v0.2 | Burns | Spacecraft + impulsive burns + finite-burn integrator. |
| v0.3 | Transfers | Lambert solver, porkchop plot, auto-plant Hohmann transfers. |
| v0.4 | Persistence | Save / load with versioned envelope; mid-course refinement. |
| v0.5 | Moons + visuals | Body hierarchy + major moons (Luna, Phobos/Deimos, Galilean, Titan, Enceladus); per-body color, vessel trail, HUD polish. |
| v0.6 | Planner UX + missions | Burn-at-next scheduler, projected-orbit HUD, finite-burn-aware iteration, moon → parent escape, click-only mouse + 5-way views, mission scaffold, multiplayer design spike. |
| v0.7 | Modding + manual flight + textures | External system / theme overlays, manual-flight stick (throttle + attitude), inclination-change planner, retrograde Lambert flag, textured Earth/Moon/Mars/Jupiter, per-node throttle, SOI / frame-transition HUD. |
| v0.8 | Multi-craft polish (in progress) | RCS / monopropellant precision thruster (v0.8.0). Multi-craft slate with per-craft burns + spawn keystroke + selector + save schema v4→v5 (v0.8.1). Craft types (4 loadouts with glyph/color visuals), full spawn form, clickable HUD nodes, Hohmann capture-preview, equatorial inclination match (v0.8.2). Docking, drag, sim-time rotation queued. |

Per-version detail: [`docs/state-of-game.md`](docs/state-of-game.md).
v0.5 release notes: [`docs/v0.5-release-notes.md`](docs/v0.5-release-notes.md).
v0.6 / v0.7 / v0.8 plans: [`docs/v0.6-plan.md`](docs/v0.6-plan.md), [`docs/v0.7-plan.md`](docs/v0.7-plan.md), [`docs/v0.8-plan.md`](docs/v0.8-plan.md).

## Future plans

v0.8 — **multi-craft polish**. Slice breakdown in
[`docs/v0.8-plan.md`](docs/v0.8-plan.md):

- ~~v0.8.0 — RCS / monopropellant mode for sub-m/s precision burns~~ **shipped.**
- ~~v0.8.1 — multi-craft foundation (selector + save schema v4 → v5 + keystroke spawn + per-craft burn state)~~ **shipped.**
- ~~v0.8.2 — craft types (4 loadouts with glyph/color visuals), full spawn form, clickable HUD nodes, capture preview, equatorial inclination match~~ **shipped.**
- v0.8.2 — craft types (propulsion loadouts, roles, visual
  differentiation, engine-firing / RCS-puffing visuals, staging).
- v0.8.3 — docking — state-transition stub.
- v0.8.4 — atmospheric drag (realistic Earth + Mars, drag-aware
  predictor, atmospheric haze rendering).
- v0.8.5 — sim-time planet rotation + tidally-locked perspectives +
  textured-bodies trickle (Saturn, Jovian moons, Uranus/Neptune).
- v0.8.6 — controls polish bag (multi-rev porkchop UI keys,
  `IterateForTarget` toggle in `m` form, throttle-change warp clamp).
- v0.8.7+ stretch — mission scripting / editor.

Deferred to v0.9+: multiplayer implementation, interstellar transfer,
N-body perturbations, solar lighting + day/night terminator + eclipses
(needs ANSI 24-bit canvas mixing research first), atmospheric heating
/ structural overstress, drag-to-edit nodes, theme-file hot-reload,
race-detector CI.

Full backlog in [`docs/state-of-game.md`](docs/state-of-game.md).

## Implementation plan

Full design doc: [`docs/plan.md`](docs/plan.md). Summary:

- Phased physics progression (viewer → Verlet → impulsive burns → finite
  burns + RK4 → SOI-aware predictor + Lambert → auto-plant transfers).
- Bubble Tea root model with screen-level sub-models (orbit / bodyinfo /
  maneuver / help / missions / menu).
- GoReleaser single-workflow CI; release artifacts on tag push.

## Credits

Architectural foundation lifted (with MIT attribution) from
[furan917/go-solar-system](https://github.com/furan917/go-solar-system).
See [NOTICE.md](NOTICE.md) for the full acknowledgments list.

## License

MIT. See [LICENSE](LICENSE).
