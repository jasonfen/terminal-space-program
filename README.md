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

Latest release: **v0.7.6**.

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
| `b` | Engage / cut manual burn (with throttle > 0) |

Attitude keys orient only — pressing `b` is what fires the engine.
Throttle setting and held attitude both show in the HUD's
ATTITUDE / PROPELLANT blocks.

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
  v4 round-trips clock, focus, craft, planted nodes (with
  per-node throttle), active burn, and missions.
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

Per-version detail: [`docs/state-of-game.md`](docs/state-of-game.md).
v0.5 release notes: [`docs/v0.5-release-notes.md`](docs/v0.5-release-notes.md).
v0.6 / v0.7 plans: [`docs/v0.6-plan.md`](docs/v0.6-plan.md), [`docs/v0.7-plan.md`](docs/v0.7-plan.md).

## Future plans

Speculative; subject to change. Candidates on the v0.8+ deferred list:

- Multi-rev porkchop branches (unblocked by v0.7.5's retrograde flag).
- Multi-craft control selector when multiple vessels come online.
- Multiplayer implementation against the v0.6.6 design.
- N-body perturbations (Lagrange points, three-body trajectories).
- Multi-system spacecraft (interstellar transfer math or deus-ex jump).
- Mission editor / scripting for user-authored objectives.
- Optional simple atmospheric drag for reentry / aerobraking gameplay.
- Drag-to-edit on planted nodes.
- Body-rendering polish (Earth terminator, Mars / Jupiter sim-time
  rotation, eclipses).
- Monopropellant / RCS mode for sub-m/s precision burns.
- Race-detector CI.

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
