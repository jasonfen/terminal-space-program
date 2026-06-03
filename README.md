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

## Inspiration

A love letter to **Kerbal Space Program**, reimagined for the terminal:
patched-conic orbital mechanics, finite burns with rocket-equation mass loss,
Hohmann and Lambert transfers, porkchop plots, staging, docking, and
pad-to-LEO launches — all rendered with braille-canvas graphics and a
keyboard-driven HUD, no GUI required.

The architectural foundation was lifted (with MIT attribution) from
[furan917/go-solar-system](https://github.com/furan917/go-solar-system). See
[NOTICE.md](NOTICE.md) for the full acknowledgments list.

## Install

```bash
# Linux x86_64
curl -L https://github.com/jasonfen/terminal-space-program/releases/latest/download/terminal-space-program-linux-amd64.tar.gz | tar xz
./terminal-space-program
```

Replace `linux-amd64` with `linux-arm64`, `darwin-amd64`, `darwin-arm64`, or
`windows-amd64` (use the `.zip` variant on Windows).

No Go toolchain, no libc dance. `CGO_ENABLED=0` static binaries.

### Build from source

```bash
git clone https://github.com/jasonfen/terminal-space-program
cd terminal-space-program
go build ./cmd/terminal-space-program
./terminal-space-program
```

Requires Go 1.24+ (bubbletea dependency chain).

## Learn more

- **[Controls & flight guide](docs/controls.md)** — quick tour, surface-launch
  walkthrough, and the full keybinding reference.
- **[Version history](docs/version-history.md)** — per-cycle changelog.
- **[Running changelog / backlog](docs/state-of-game.md)** — per-version detail.

## License

MIT. See [LICENSE](LICENSE).
