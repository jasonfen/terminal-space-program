# terminal-space-program

Terminal-native orbital-mechanics rocket simulator. A take on Kerbal Space
Program that lives in your terminal, distributed as a single static Go binary.

> **Status:** v0 scaffold. Not yet runnable. See [MVP scope](#mvp-scope-v01)
> and the [implementation plan](#implementation-plan).

## Features (v0.1 target)

- Physically-plausible solar system viewer — Sol, Alpha Centauri,
  TRAPPIST-1, Kepler-452.
- Fly a spacecraft through patched-conic two-body physics with
  velocity-Verlet propagation.
- Impulsive burns with prograde / retrograde / normal / radial modes.
- Trajectory prediction preview.
- KSP-style time-warp, up to 10,000×.
- Bubble Tea TUI with drawille canvas rendering.

## Install

```bash
# v0.1 and later — once binaries are published:
curl -L https://github.com/jasonfen/terminal-space-program/releases/latest/download/terminal-space-program-linux-amd64.tar.gz | tar xz
./terminal-space-program
```

No Go toolchain required at install time. The project ships `CGO_ENABLED=0`
static binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and
windows/amd64.

## Build from source

```bash
git clone https://github.com/jasonfen/terminal-space-program
cd terminal-space-program
go build ./cmd/terminal-space-program
```

Requires Go 1.22+.

## MVP scope (v0.1)

- Phase 0 — viewer parity with existing references.
- Phase 1 — spacecraft as a propagated body in Sol.
- Phase 2 — impulsive burns + burn-planner screen + trajectory preview.

Deferred to v0.2+: finite-duration burns, Hohmann / Lambert planner,
multi-system spacecraft, save/load, N-body perturbations.

## Implementation plan

Full design doc: see `docs/plan.md` (landing in a follow-up commit). Summary:

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
