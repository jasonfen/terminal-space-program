# terminal-space-program

Terminal-native orbital-mechanics rocket simulator. A take on Kerbal Space
Program that lives in your terminal, distributed as a single static Go binary.

```
┌───────────────────────────────────────────────────────────┐
│ terminal-space-program — Sol                              │
│ ┌─────────────────────────────────────┐ ┌───────────────┐ │
│ │    · ·                              │ │ CLOCK         │ │
│ │  ·     ·                            │ │   T+2026-04-23│ │
│ │ ·   ⊙   ·       · ⊕ ·               │ │   warp: 100x  │ │
│ │  ·     ·                            │ │               │ │
│ │    · ·                              │ │ VESSEL        │ │
│ │                                     │ │   S-IVB-1     │ │
│ │                                     │ │   alt: 500 km │ │
│ └─────────────────────────────────────┘ │   v: 7.61 km/s│ │
│ [q] quit [s] system [m] burn [?] help   └───────────────┘ │
└───────────────────────────────────────────────────────────┘
```

## Install

Latest release: **v0.6.6**.

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

## Quick tour

You spawn as **S-IVB-1** in a 500 km circular prograde LEO. The left
panel is the canvas — Sun (or whichever body you focus on) at the
center, planets on their actual orbits, your spacecraft as a small
chevron oriented along velocity. The right HUD shows clock, focus +
view, vessel state, propellant budget, planned nodes, projected post-
burn orbit, the active mission, and (when applicable) a Hohmann preview
to the cursor-selected body. Time-warp with `.` / `,` to watch planets
move; pause with `0` or space.

To make something happen:

1. Press `←`/`→` (or click) to scroll the cursor through bodies. Pick Mars.
2. Press `H` to plant a Hohmann transfer — two finite-burn nodes appear
   (geocentric departure + Mars-frame arrival), each color-coded with
   its predicted post-burn orbit on the canvas, listed in the HUD with
   Δv and time-to-fire.
3. Time-warp forward. The departure node fires at its trigger time;
   warp clamps to ≤10× during the burn so the integrator keeps temporal
   resolution. Your trajectory unrolls past Earth's SOI, the predictor
   switches frames, and the curve bends sunward as it should.
4. The arrival node fires near Mars and drops you into a low capture
   orbit. For phasing-aware launch windows, use `P` (porkchop) instead
   of `H`.

For burns by hand, press `m` to open the planner. Pick a mode
(prograde / retrograde / normal± / radial±), choose when it fires
(absolute T+, or *next peri / next apo / next AN / next DN*), and set
Δv. Burn duration is derived from Δv via the rocket equation — no
separate field. The PROJECTED ORBIT block previews apo/peri/AN/DN of
the result live as you edit. Commit with Enter; the integrator switches
from Verlet (free flight) to RK4 (thrust) and ticks the burn across
multiple frames with mass loss tracked from the rocket equation.

## Keybindings

The in-game `?` overlay is the source of truth; this table mirrors it.

### Global

| Key | Action |
|---|---|
| `q` | Quit (confirm prompt + autosave) |
| `Ctrl+C` | Quit immediately |
| `?` | Toggle help overlay |
| `Esc` | Back / close panel |
| `s` | Switch system (Sol → Alpha Cen → TRAPPIST-1 → Kepler-452) |
| `i` | Body info screen |
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
| `P` | Porkchop plot for selected body; `Enter` on a cell plants that Lambert transfer. Inter-primary only — moon targets show a banner redirecting to `H` |
| `R` | Refine plan — re-Lambert from live state, plant mid-course correction + update arrival |
| `S` / `L` | Save / load game (`$XDG_STATE_HOME/terminal-space-program/save.json`) |
| `m` | Open maneuver planner |

### Mouse (orbit canvas)

Click-only. No drag, no wheel-zoom.

| Click target | Action |
|---|---|
| Body | Focus body (same as cursoring onto it with `←` / `→`) |
| Vessel | Focus craft |
| Planted node | Open planner pre-loaded for that node (edit-replace, fire time preserved) |
| Empty canvas | Open planner with a new node staged at the orbit point nearest the click |
| HUD panel | Open body info |

### Maneuver planner (`m`)

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Cycle field focus (mode → fire-at → Δv) |
| `←` / `→` | Cycle the focused cycle field (mode or fire-at trigger) |
| digits / backspace | Edit Δv |
| `Enter` | Commit burn |
| `Esc` | Cancel and back to orbit view |

Δv drives both delivered Δv **and** burn duration via the rocket
equation `t = (m₀/ṁ)·(1 − exp(−Δv/(Isp·g₀)))`. Zero-thrust craft fall
back to impulsive automatically. The fire-at field selects Absolute T+
or one of `next peri / next apo / next AN / next DN`; event-relative
nodes resolve their trigger lazily on the first tick after plant. The
PROJECTED ORBIT block readouts apo / peri / AN / DN of the resulting
orbit live as you edit.

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

## Features (v0.6)

The v0.6 cycle landed planner UX, a mission scaffold, and the
multiplayer design spike:

- **Burn-at-next scheduler** (v0.6.0). Maneuver nodes can fire on
  `next peri / next apo / next AN / next DN` instead of an absolute
  T+; the planner resolves the trigger time lazily on the first tick
  after plant.
- **Predicted post-burn orbit HUD** (v0.6.1). PROJECTED ORBIT block
  on both the orbit screen and `m` form (apo / peri / AN / DN of the
  chained post-burn orbit, frame-rebased per node). Per-leg
  trajectory preview in cyan / mint / amber / pink with matched
  node-marker colors. Default LEO 200 → 500 km; spawn focus = craft.
- **Finite-burn-aware iterative planner** (v0.6.2). Newton iteration
  around the impulsive solver so `H` auto-plant Hohmann hits the
  requested apoapsis even on low-TWR loadouts where gravity-rotation
  losses bite.
- **Moon → parent escape transfer** (v0.6.3). `H` on the parent body
  while inside a moon's SOI plants a bound transfer ellipse with
  apolune at the SOI radius; player circularizes manually post-exit.
- **Click-only mouse + 5-way view modes** (v0.6.4). Click-to-select
  on the orbit canvas (vessel / node / body / empty / HUD priority)
  and porkchop cells; `v` cycles top → right → bottom → left →
  orbit-flat with back-of-body occlusion in side views.
- **Mission scaffold + Δv-only burn input** (v0.6.5). Three predicate
  kinds (`circularize` / `orbit_insertion` / `soi_flyby`) on a sticky
  three-state machine, embedded starter catalog (1000 km LEO
  circularize, Luna orbit insertion, Mars SOI flyby), MISSION HUD
  block. The maneuver form drops the duration field — Δv now drives
  duration via the rocket equation.
- **Multiplayer design spike** (v0.6.6). `docs/multiplayer-design.md`
  — transport (WebSocket-for-MVP), authority (host-authoritative +
  warp-arbitration), and persistence (`Session` inside `Payload`).
  Pure prose; implementation deferred.

Earlier feature history (v0.1 → v0.5.15) lives in
[`docs/state-of-game.md`](docs/state-of-game.md) and
[`docs/v0.5-release-notes.md`](docs/v0.5-release-notes.md).

## Future plans

Speculative scoping; subject to change.

- **v0.7 — modding + manual flight + planner polish.** Six slices:
  external `$XDG_CONFIG_HOME/.../systems/*.json` overlay loader (v0.7.0),
  per-body palette migration (v0.7.1), user `theme.json` overrides
  (v0.7.2), manual flight controls — throttle + hold-to-burn +
  attitude hold (v0.7.3), inclination-change planner (v0.7.4), and an
  explicit retrograde flag for `LambertSolve` (v0.7.5). Slice
  breakdown: [`docs/v0.7-plan.md`](docs/v0.7-plan.md).
- **v0.8+ — open.** Candidates: N-body perturbations (Lagrange points,
  three-body trajectories), multi-system spacecraft (interstellar
  transfer math or deus-ex jump to unlock the system-cycle UX),
  multi-rev porkchop branches, mission editor / scripting for user-
  authored objectives, optional simple atmospheric drag for reentry /
  aerobraking gameplay, drag-to-edit on planted nodes, multiplayer
  implementation against the v0.6.6 design.

Smaller polish items in the queue: explicit retrograde flag for
`LambertSolve`, inclination-change planner, throttle control,
multi-craft control selector when multiple vessels come online,
race-detector CI. Full backlog in
[`docs/state-of-game.md`](docs/state-of-game.md).

## Implementation plan

Full design doc: [`docs/plan.md`](docs/plan.md). Summary:

- Phased physics progression (viewer → Verlet → impulsive burns → finite
  burns + RK4 → SOI-aware predictor + Lambert → auto-plant transfers).
- Bubble Tea root model with screen-level sub-models (orbit / bodyinfo /
  maneuver / help).
- GoReleaser single-workflow CI; release artifacts on tag push.

## Credits

Architectural foundation lifted (with MIT attribution) from
[furan917/go-solar-system](https://github.com/furan917/go-solar-system).
See [NOTICE.md](NOTICE.md) for the full acknowledgments list.

## License

MIT. See [LICENSE](LICENSE).
