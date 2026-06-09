# Command-line reference

`terminal-space-program` opens, by default, with a vessel in a 500 km low Earth
orbit. The flags below let you boot straight into a different starting
scenario — a system, a body to orbit or launch from, an orbit shape, a launch
site, and a craft. They only shape a **fresh** game; loading a save from the
in-game menu restores that save unchanged.

All flags are optional. With none, the start is exactly as before.

## Quick examples

```bash
# A 100 km circular orbit around the Moon
terminal-space-program --orbit moon --altitude 100km

# The Kern Stack in a 400 km orbit of Kern, in the Lumen system
terminal-space-program --system Lumen --orbit kern --loadout Kern-Stack --altitude 400km

# An ISS-like inclined low Earth orbit
terminal-space-program --orbit earth --altitude 400km --inclination 51.6

# A Saturn V on the pad at Kennedy Space Center
terminal-space-program --launch-site KSC --loadout Saturn-V

# A launchpad at an arbitrary surface point
terminal-space-program --launchpad --lat -34.6 --lon -58.4 --loadout Falcon-9

# Discover the valid names, then quit
terminal-space-program --list-systems
terminal-space-program --list-bodies --system Lumen
terminal-space-program --list-loadouts
terminal-space-program --list-launch-sites
```

## Flags

### Discovery / info (print and exit)

| Flag | Effect |
|---|---|
| `--version`, `-v` | Print the version + build commit. |
| `--list-systems` | List the available star systems. |
| `--list-bodies` | List bodies (their IDs). Honours `--system`; otherwise lists every system. |
| `--list-loadouts` | List the craft loadout IDs. |
| `--list-launch-sites` | List the named launch sites. |

### Where to start

| Flag | Default | Meaning |
|---|---|---|
| `--system NAME` | `Sol` | Star system, by name (`Sol`, `Lumen`, …). Case-insensitive. |
| `--orbit BODY` | `earth` (or the system's first planet) | Body to orbit or launch from, by **ID** or English name (see `--list-bodies`). `--parent` and `--body` are accepted as synonyms. |
| `--loadout NAME` | `S-IVB-1` | Craft loadout ID (`Saturn-V`, `Apollo-Stack`, `Kern-Stack`, …; see `--list-loadouts`). |

A vessel is bound for its lifetime to the system it spawns in, and the camera
follows it — so `--system Lumen` drops you straight into Lumen with the flight
HUD live.

### Orbital placement

Used when **not** launching from a pad. Mutually exclusive with the surface
flags below.

| Flag | Default | Meaning |
|---|---|---|
| `--altitude VAL` | `500km` | Circular-orbit altitude above the body's mean radius. Unit-suffixed: `400km`, `400000m`, or a bare number read as kilometres. |
| `--inclination DEG` | `0` | Orbit inclination in degrees, relative to the body's equator. |
| `--retrograde` | off | Spawn into a retrograde orbit. |

The orbit is always circular; `--inclination` tilts its plane.

### Surface (launchpad) placement

Any of these switches to a surface spawn. Mutually exclusive with the orbital
flags above.

| Flag | Default | Meaning |
|---|---|---|
| `--launchpad` | off | Spawn on the surface instead of in orbit. On its own, uses the KSC default site. |
| `--launch-site NAME` | — | A named site: `Equator`, `KSC`, `Baikonur`, `Plesetsk`, `North-Pole` (see `--list-launch-sites`). Implies `--launchpad`. |
| `--lat DEG`, `--lon DEG` | `0`, `0` | Numeric surface site — degrees north and degrees east of the prime meridian. Implies `--launchpad`. |

Named sites are Earth-oriented; to launch from a pad on another body, give an
explicit `--lat`/`--lon` (and `--orbit BODY` to choose the body).

## Notes & errors

- Combining orbital flags (`--altitude`/`--inclination`/`--retrograde`) with
  surface flags (`--launchpad`/`--launch-site`/`--lat`/`--lon`) is an error.
- `--launch-site` can't be combined with `--lat`/`--lon`.
- An unknown system, body, loadout, or launch site prints a clear error that
  lists the valid values, and exits non-zero.
- Body and loadout names are the catalog **IDs** — run the matching `--list-*`
  flag if a name is rejected (e.g. the Moon's ID is `moon`, Lumen's home planet
  is `kern`).
