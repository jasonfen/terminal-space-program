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
│ │                                     │ │   LEO-1       │ │
│ │                                     │ │   alt: 200 km │ │
│ └─────────────────────────────────────┘ │   v: 7.78 km/s│ │
│ [q] quit [s] system [m] burn [?] help   └───────────────┘ │
└───────────────────────────────────────────────────────────┘
```

## Install

Latest release: **v0.5.8**.

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

You spawn in low Earth orbit at 200 km altitude. The left panel is the
canvas — Sun at the center, planets on their actual orbits, your
spacecraft as a small cluster. The right HUD shows clock, vessel state,
selected body, planted nodes, and (when relevant) the Hohmann preview to
the selected target. Time-warp with `.` / `,` to watch planets move; pause
with `0` or space.

To make something happen:

1. Press `←`/`→` to scroll the cursor through bodies. Pick Mars.
2. Press `P` to plant a Hohmann transfer — two nodes appear on the canvas
   (one in Earth's frame, one in Mars's), the HUD lists them with their
   Δv and time-to-fire.
3. Time-warp forward. Watch the departure node fire when its trigger time
   hits. Your trajectory unrolls past Earth's SOI, the predictor switches
   frames, and the curve bends sunward as it should.
4. The arrival node fires near Mars. (Phasing isn't enforced in v0.3.1 —
   the sandbox assumes Mars is where you need it. Real launch-window
   selection comes with the porkchop plot in v0.3.2.)

For finite engine burns instead of instant Δv, press `m` to open the
planner — set mode, Δv, and a non-zero duration. The integrator switches
from Verlet (energy-conserving free flight) to RK4 (handles the non-
conservative thrust force) and ticks the burn across multiple frames with
mass loss tracked from the rocket equation.

## Keybindings

### Global

| Key | Action |
|---|---|
| `q`, `Ctrl+C` | Quit |
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
| `n` | Plan a default node (T+5min, prograde, 200 m/s) |
| `N` | Clear all planned nodes |
| `H` | **Auto-plant Hohmann transfer to selected body** (v0.3.1; rebound from `P` in v0.5.8). For moon targets uses intra-primary geocentric Hohmann (v0.5.7). |
| `P` | **Porkchop plot for selected body** (v0.3.3; rebound from `k` in v0.5.8); `Enter` on a cell plants that transfer (v0.4.1). Inter-primary only — moon targets show a banner redirecting to `H`. |
| `R` | **Refine plan** — re-Lambert from live state, plant mid-course correction + update arrival (v0.4.1) |
| `S` / `L` | Save / load game (v0.4.0) |
| `m` | Open maneuver planner |

### Maneuver planner (`m`)

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Cycle field focus (mode → Δv → duration) |
| `←` / `→` | Cycle direction mode (when mode field is focused) |
| digits / backspace | Edit Δv or duration value |
| `Enter` | Commit burn |
| `Esc` | Cancel and back to orbit view |

A duration of `0` plants an impulsive burn (instant Δv). A non-zero
duration starts a finite burn that runs for up to that many seconds, or
until the requested Δv is delivered, whichever first.

### Porkchop plot (`P`)

| Key | Action |
|---|---|
| `←` / `→` | Departure-day cursor |
| `↑` / `↓` | Time-of-flight cursor |
| `Enter` | Plant Lambert transfer for selected cell (v0.4.1) |
| `Esc` | Back to orbit view |

Cursor opens snapped to the minimum-Δv cell. `·` glyphs mark cells
where Lambert didn't converge — `Enter` on those is a no-op.

## Features (v0.5.8)

- **Adaptive body sizing.** `BodyPixelRadius` now switches to true-
  scale rendering when the body's projected radius would be ≥ 4 px,
  capped at 64 px so the Sun can't fill the canvas at extreme zoom.
  Below the threshold it falls back to the existing tier buckets so
  bodies stay visible at system-wide zoom (where true scale would be
  sub-pixel). Practical effect: Earth fills its real radius on
  FocusCraft, so a periapsis marker inside the rendered disk reads
  visually as "you're going to hit the surface" — pre-fix, the 2 px
  tier disk hid sub-orbital periapsis behind a token glyph.
- **Periapsis-below-surface warning.** VESSEL HUD block renders
  `PERIAPSIS BELOW SURFACE` in the Alert style whenever the craft's
  computed periapsis altitude goes negative, regardless of zoom level.

## Features (v0.3.5)

- **Punchier default engine.** `Thrust` on `NewInLEO` bumped from 1 kN
  to 10 kN. 200 m/s `n` burn now takes 20 s sim-time instead of 200 s;
  3.6 km/s Mars departure ≈ 6 min sim-time instead of 1 hr.
- **Apoapsis / periapsis markers.** The orbit renderer now plants two
  filled disks on the current orbit — larger disk at apoapsis (ν=π),
  smaller at periapsis (ν=0). Low-eccentricity orbits (e < 0.1) are
  near-circular in *shape* even when the apo/peri altitudes differ by
  hundreds of km; the markers show the two extremes at a glance
  without requiring the player to read altitudes off the HUD.
- **`n` default Δv** bumped 50 → 200 m/s so a single press produces a
  visibly-offset orbit (e ≈ 0.05) at default FocusCraft zoom.

## Features (v0.3.4)

- **Finite burns by default for planted nodes.** Both `P` (auto-plant
  Hohmann) and `n` (default-node) now set `Duration = Δv × mass /
  thrust` so the burn runs through the RK4+mass-flow integrator that
  landed in v0.2.1 — no more "burn appears instantaneous" surprise.
  Manual entry in the maneuver planner still accepts `duration = 0`
  for impulsive testing.
- **Equatorial orbit rendering fix.** `ElementsFromState` now computes
  argument of periapsis directly from the eccentricity-vector angle
  (`atan2(eVec.Y, eVec.X)`) when the node vector is degenerate
  (equatorial orbit, `i ≈ 0`). Before the fix, post-burn periapsis
  rotation wasn't reflected in the rendered ellipse because ω stayed
  pinned at 0 for all equatorial orbits.
- **Directional vessel glyph.** The spacecraft now renders as a
  chevron (">"-style arrow) rotated into its velocity direction
  rather than an 8-dot cross. Reads as "I'm going this way" at a
  glance without having to parse the orbit curve.

## Features (v0.3.3)

- **Porkchop plot** (`k`). Press `k` on a selected target to open a
  Δv heatmap gridded over departure day (0–365) and time of flight
  (100–400 days). Each cell shows the total budget (departure Δv +
  capture Δv) for a Lambert-derived Hohmann-style transfer starting
  that day with that TOF. Intensity ramp `█▓▒░ ` from cheapest to most
  expensive; `·` marks non-converged / infeasible cells. The cursor
  (`←/→` dep, `↑/↓` tof) snaps to the minimum-Δv cell on open and
  reads out the selected cell's total. Uses synthetic planar-circular
  approximations of body orbits for the ephemeris so textbook Hohmann
  alignment lands a cell that matches PlanHohmannTransfer within ~15%.
- **Multi-revolution Lambert** (`LambertSolveRev(..., nRev int)`).
  For N≥1, the universal-variables z-bracket starts at (2πN)². Single
  branch per N (lower-z side); min-energy / multi-branch selection is
  a v0.4 polish item if needed.

## Features (v0.3.2)

- **Perceived body size.** Planets and moons render as filled disks
  sized by physical-radius tier (moon / terrestrial / gas giant /
  star) rather than single dots. The system primary gets a hollow ring
  with a filled center to distinguish it from the planets that orbit it.
  Sizes are bucketed for readability — even the Sun would be a sub-
  pixel speck at Sol-wide zoom if rendered to true scale.
- **Vessel orbit path.** The craft's *current* Keplerian orbit ellipse
  is drawn live on the canvas (dotted, stride 3) so the player can see
  their trajectory at a glance without mentally re-deriving it from a
  velocity vector. Renders in the craft's home primary frame,
  translated into the system frame so it sits alongside planet orbits.
  Hyperbolic escape trajectories are still shown via the SOI-segmented
  preview from the maneuver planner.

## Features (v0.3.1)

- **Auto-plant Hohmann transfer** (`P`). Select a target body, press one
  key, two nodes plant: a geocentric departure burn at parking-orbit
  periapsis (raises apoapsis past Earth's SOI), and a destination-frame
  arrival burn (drops into low capture orbit). Patched-conic Δv math
  matches Curtis Example 8.3 within 5% for Earth → Mars.
- **Multi-frame nodes.** Each `ManeuverNode` carries a `PrimaryID` tag
  identifying which body's frame the burn was planned in. The orbit-view
  glyph cluster grows for foreign-frame nodes so the player can see at a
  glance which leg is which on auto-planted transfers.
- **Frame-aware `PostBurnState`**. Returns the post-burn state plus the
  ID of the primary that frame is relative to — critical for nodes that
  fire after the trajectory crosses an SOI boundary.

## Features (v0.3.0)

- **Lambert solver**. `planner.LambertSolve(r1, r2, dt, mu)` reproduces
  Curtis Example 5.2 within 0.5%; round-trip via Verlet returns to r2
  within integrator tolerance. Single-rev prograde only — multi-rev
  branches and explicit retrograde handling deferred to v0.3.2.
- **SOI-aware predictor**. When a sub-step crosses a sphere-of-influence
  boundary, the state is rebased to the new primary's frame and μ
  switches for subsequent steps. The closing point of each outgoing
  segment lands at the actual crossing — no time gap, join continuous
  in inertial coords. Resolves the LEO reference-frame trap that v0.2's
  predictor punted on (predicted post-escape trajectories were
  geometrically wrong even though their coloring was correct).

## Features (v0.2.1)

- **Finite-duration burns**. `Spacecraft.Thrust` (1 kN default) drives
  per-sub-step engine acceleration via an RK4 integrator path that
  handles the non-conservative force cleanly (Verlet would silently
  drift). Mass flow `dm/dt = -Thrust/(Isp·g0)` debits fuel each tick.
  Burn ends on Δv delivered, fuel exhausted, or duration elapsed.
- **Active-burn HUD**. Orbit screen renders a `BURN ACTIVE` block while
  a burn is in flight (mode, Δv-to-go, T-remaining). Time-warp clamps
  to ≤10× during a burn so the integrator keeps temporal resolution on
  the burn window.

## Features (v0.2)

- **View focus / camera follow.** `f`/`F` cycles the camera target
  across the system primary, every body, and the spacecraft (Sol only);
  `g` resets to the system view.
- **Maneuver nodes.** `n` plants a node on the current orbit; `N`
  clears all pending nodes. Nodes render on-canvas at their projected
  inertial position and list in the HUD; firing pops them automatically.
- **SOI-segmented trajectory viz.** The predicted post-burn trajectory
  is partitioned by dominant SOI. Home-SOI samples render dashed,
  foreign-SOI samples solid so capture arcs read visually distinct.
- **Hohmann preview HUD.** When the cursor-selected body has orbital
  data, the SELECTED block renders reference heliocentric Δv1 / Δv2 /
  transfer time. Earth → Mars matches Curtis §6.2 within 10%.

## Features (v0.1)

- **Viewer.** Physically-plausible renderings of Sol, Alpha Centauri,
  TRAPPIST-1, and Kepler-452. Planets move under time warp; orbit lines
  from the same Kepler math that places the planets.
- **Spacecraft.** One vessel in low Earth orbit, propagated with
  velocity-Verlet on a patched-conic two-body model. Energy-conservation
  verified under 1% drift across 1000 orbits (we ship at ~1e-7%).
- **Burns.** Impulsive burns with prograde / retrograde / normal± /
  radial± modes, rocket-equation fuel accounting, live shadow-trajectory
  preview in the planner.
- **Time warp.** Six discrete steps (1× → 100000×) with integrator-aware
  clamping so sim-time can't outrun numerical stability.
- **Single binary.** 5-target GoReleaser matrix (linux+darwin amd64/arm64,
  windows amd64), `CGO_ENABLED=0`, `-ldflags "-s -w"`.

### Deferred to v0.5+

- **Explicit retrograde-flag** for Lambert (today both branches work,
  but the selection is driven by the bracket starting point, not a
  caller hint).
- **Inclination-change planner** for out-of-plane corrections.
- **Vessel position history trail** (distinct from current orbit
  ellipse).
- **Zoom-level LOD** for body size and orbit density.
- **Multi-system spacecraft**, **N-body perturbations**,
  **config-file custom systems**, **mouse**.

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
