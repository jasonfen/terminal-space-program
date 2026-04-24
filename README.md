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

Latest release: **v0.2.0**.

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

## Keybindings

| Key | Action |
|---|---|
| `q`, `Ctrl+C` | Quit |
| `?` | Toggle help overlay |
| `Esc` | Back / close |
| `→` / `l` | Next body |
| `←` / `h` | Previous body |
| `s` | Switch system (Sol → Alpha Cen → TRAPPIST-1 → Kepler-452) |
| `i` | Body info |
| `+` / `-` | Zoom in / out |
| `f` / `F` | Cycle camera focus forward / backward (system → bodies → craft) |
| `g` | Reset camera focus to system |
| `.` | Warp up (1× … 100000×) |
| `,` | Warp down |
| `0`, `Space` | Pause / resume |
| `m` | Open maneuver planner |
| `n` | Plan a maneuver node |
| `N` | Clear all planned nodes |
| `Tab` (in planner) | Cycle direction mode |
| `Enter` (in planner) | Commit burn |
| `Esc` (in planner) | Cancel burn |

## Features (v0.2)

Slice-1 of the v0.2 scope — "maneuver planning, closed loop."

- **View focus / camera follow.** `f`/`F` cycles the camera target across
  the system primary, every body, and the spacecraft (Sol only); `g`
  resets to the system view. Focus keeps moving targets centered without
  refitting the zoom on every frame.
- **Maneuver nodes.** `n` plants a node on the current orbit; `N` clears
  all pending nodes. Nodes are rendered on-canvas at their projected
  inertial position and listed in the HUD. When sim-time reaches a
  node's trigger time, its impulsive burn fires and the node pops.
- **SOI-segmented trajectory viz.** The predicted post-burn trajectory
  is partitioned by dominant sphere-of-influence. Samples inside the
  craft's home SOI render stride-2 dashed; samples that cross into a
  foreign SOI render stride-1 solid so capture arcs read visually
  distinct from cruise.
- **Hohmann preview HUD.** When the cursor-selected body has orbital
  data, the SELECTED block renders reference heliocentric Δv1 / Δv2 /
  transfer time computed off the system primary's GM and the craft's
  current inertial radius. Earth → Mars lands within 10% of the
  Curtis §6.2 textbook values.

## Features (v0.1)

- **Viewer.** Physically-plausible renderings of Sol, Alpha Centauri,
  TRAPPIST-1, and Kepler-452. Planets move under time warp; orbit lines
  drawn from the same Kepler math that places the planets.
- **Spacecraft.** One vessel in low Earth orbit, propagated with
  velocity-Verlet on a patched-conic two-body model. Energy-conservation
  verified under 1% drift across 1000 orbits (we ship at ~1e-7%).
- **Burns.** Impulsive burns with prograde / retrograde / normal± /
  radial± modes, rocket-equation fuel accounting, and live shadow-
  trajectory preview in the maneuver planner.
- **Time warp.** Six discrete steps (1× → 100000×) with integrator-
  aware clamping so sim-time can't outrun numerical stability.
- **Single binary.** 5-target GoReleaser matrix (linux+darwin amd64/arm64,
  windows amd64), `CGO_ENABLED=0`, `-ldflags "-s -w"`.

### Deferred to v0.3+

Finite-duration burns (impulsive only through v0.2), Lambert targeting
with auto-plant nodes in the correct SOI frame, multi-system spacecraft,
save/load, N-body perturbations, config-file custom systems, mouse
support.

## Implementation plan

Full design doc: [`docs/plan.md`](docs/plan.md). Summary:

- 4-phase physics progression (viewer → Verlet propagation → impulsive
  burns → maneuver library).
- Bubble Tea root model with screen-level sub-models (orbit / bodyinfo /
  maneuver / help).
- GoReleaser single-workflow CI; release artifacts on tag push.

## Credits

Architectural foundation lifted (with MIT attribution) from
[furan917/go-solar-system](https://github.com/furan917/go-solar-system).
See [NOTICE.md](NOTICE.md) for the full acknowledgments list.

## License

MIT. See [LICENSE](LICENSE).
