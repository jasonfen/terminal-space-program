# terminal-space-program

Terminal-native orbital-mechanics rocket simulator. A take on Kerbal Space
Program that lives in your terminal, distributed as a single static Go binary.

![Orbit rendering in the launch / landing chase-cam view](media/orbit-rendering.gif)

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

A love letter to **Kerbal Space Program**, reimagined for the terminal. Plan
transfers between planets and moons, fly your rocket off the pad and into orbit
by hand, rendezvous and dock, stage away spent boosters, and bring a capsule
home under parachute — all drawn with braille-canvas graphics and driven from
the keyboard. No mouse required, no GUI, just a single binary in your terminal.

Under the hood it's a real orbital-mechanics sim: gravity, fuel, and timing all
matter, the way they do in KSP.

The visual foundation was lifted (with MIT attribution) from
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

Requires Go 1.24 or newer.

## Learn more

- **[Controls & flight guide](docs/controls.md)** — a quick tour, a launch
  walkthrough, and the full list of keys.
- **[Version history](docs/version-history.md)** — what landed in each release.

## License

MIT. See [LICENSE](LICENSE).
