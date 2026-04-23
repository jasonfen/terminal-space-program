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

Once v0.1.0 is cut and binaries are published:

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
| `.` | Warp up (1× … 100000×) |
| `,` | Warp down |
| `0`, `Space` | Pause / resume |
| `m` | Open maneuver planner |
| `Tab` (in planner) | Cycle direction mode |
| `Enter` (in planner) | Commit burn |
| `Esc` (in planner) | Cancel burn |

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

### Deferred to v0.2+

Finite-duration burns (impulsive only in v0.1), Hohmann / Lambert
planner, multi-system spacecraft, save/load, N-body perturbations,
config-file custom systems, mouse support.

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
