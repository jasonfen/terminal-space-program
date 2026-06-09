<a href="https://www.buymeacoffee.com/jasonfen"><img src="https://cdn.buymeacoffee.com/buttons/v2/default-yellow.png" alt="Buy Me A Coffee" height="24" width="85"></a>

# Terminal Space Program

Terminal-native orbital-mechanics rocket simulator. A take on Kerbal Space
Program that lives in your terminal, distributed as a single static Go binary.

## Inspiration

I love **Kerbal Space Program**, I love **TUI Applications**. I decided the two should be married for when I'm bored and have a terminal available.

## The Game

<img align="right" width="250" src="media/orbit-rendering.gif" alt="Orbit rendering in the launch / landing chase-cam view">

By default, you spawn in an Apollo-style SIV-B in a 500km circular orbit. Switch targets to Moon (press t to switch and T to clear). Plant a Hohmann transfer + inclination change (press H). Or, fly it all manually. See **[Controls & flight guide](docs/controls.md)** for a quick tour, a launch walkthrough, and the full list of keys.

Plan transfers between planets and moons, fly your rocket off the pad and into orbit
by hand, rendezvous and dock, stage away spent boosters, and bring a capsule
home under parachute — all drawn with braille-canvas graphics and driven from
the keyboard. No mouse required, no GUI, just a single binary in your terminal.

Under the hood it's a real orbital-mechanics sim: gravity, fuel, atmospheric drag, and timing all
matter, the way they do in real life. Unlike KSP (without mods), the default game renders our solar system. Launches are hard, they take 7.5km/s for LEO - just like real life. The moon is inclined, the earth is tilted on its axis. To match the real solar systems, there are real life vessels with accurate loadouts of thrust.

Recently introduced: a familiar 1/10th-scale system named Lumen; home to a familiar planet, Kernel, with two moons, Cursor and Glyph - and a nearby red planet, Rust. A Lumen-specific vessel is scaled to that environment. It's a little rough appearance-wise as no textures have ported over to that system, but vessels fly and hit familiar Delta-V marks for launch, Cursor (Mun) transfers, etc. From a default game launch press TAB to cycle through the available solar systems until you reach Lumen. Press F to cycle to the planet Kernel, then press N to spawn a craft there.

Each vessel is bound for its lifetime to the system it spawns in — the simulator flies every vessel against its own system and the camera follows your *active* vessel's system, so a craft parked in Sol keeps orbiting while you fly in Lumen (TAB stays a browse-only camera toggle). Adding Lumen also changed the body catalog, so the on-disk save format moved to v8; older saves auto-migrate on load.

The visual foundation was lifted (with MIT attribution) from
[furan917/go-solar-system](https://github.com/furan917/go-solar-system). See
[NOTICE.md](NOTICE.md) for the full acknowledgments list.

## Install

### Homebrew (macOS / Linux)

```bash
brew install --cask jasonfen/tap/terminal-space-program
```

### Scoop (Windows)

```powershell
scoop bucket add jasonfen https://github.com/jasonfen/scoop-bucket
scoop install terminal-space-program
```

### Direct download

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
