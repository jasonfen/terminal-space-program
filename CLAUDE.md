# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Terminal-native orbital-mechanics rocket simulator (a KSP-in-the-terminal),
shipped as a single static Go binary. Bubble Tea TUI on top of a
patched-conic physics core. Go 1.24+, no CGO.

## Commands

```bash
go build ./cmd/terminal-space-program        # build the binary
./terminal-space-program                     # run it
./terminal-space-program --version           # version + commit

go test ./... -race -count=1                 # full suite (matches CI)
go test ./internal/sim -run TestWarp         # single package / test (regex)
go vet ./...                                 # CI runs this before tests
```

CI (`.github/workflows/test.yml`) runs `go vet ./...` then
`go test ./... -race -count=1` on every push/PR — keep both green.
Releases are tag-driven via GoReleaser (`v*` SemVer tags only;
four-part `vX.Y.Z.N` tags are checkpoint markers and do **not** trigger
a release).

There is no Makefile and no lint config beyond `go vet`; the codebase
is test-heavy (almost every `.go` has a sibling `_test.go`) — add tests
alongside changes.

## Architecture

The dependency flow is one-directional: **physics → orbital → planner /
spacecraft → sim → tui**. Lower layers never import upward.

- **`internal/physics`** — integrators and force models, stateless and
  unit-tested in isolation: symplectic `verlet` (free flight), `rk4`
  (active thrust), `kepler_step` (warp-locked analytic propagation),
  `drag`, `soi` (sphere-of-influence transitions), `surface` (ground
  contact), `state` (position/velocity state vector).
- **`internal/orbital`** — Keplerian element conversions (`kepler`),
  the `Calculator` for body ephemerides, reference-frame transforms
  (`frame` — ecliptic vs. body-equatorial), apsis/node `events`.
- **`internal/bodies`** — body catalog + multi-system data, loaded from
  embedded `systems/*.json` plus user overlays. `LoadAllWithWarnings`
  is the entrypoint; user files in `$XDG_CONFIG_HOME` win on
  `systemName` match. Save files carry a `body_catalog_hash` so a save
  rejects if the catalog changed.
- **`internal/planner`** — burn/transfer math: `lambert` (Stumpff
  universal-variable, Curtis Alg 5.2), `hohmann`, `porkchop`,
  `inclination`, `moon_escape`, `finiteburn` (iterate-for-target),
  `predictor` (projected post-burn orbit, frame-rebased per node).
- **`internal/spacecraft`** — vessel state: `Spacecraft`, `Stages`
  (player-managed decouple chain), `thrust`, `rcs`, `slew`
  (rate-limited attitude), `maneuver` nodes, `loadouts` (the launch
  catalog), `target`.
- **`internal/sim`** — the simulation orchestrator. **`world.go`**
  (`World`) is the central mutable state: loaded systems, the craft
  slate (`Crafts []*Spacecraft` + `ActiveCraftIdx`), `Clock`, `Focus`,
  `Target` (unified body/craft aim slot), `NavMode`, `ViewMode`,
  `Missions`. `tick.go` drives the Bubble Tea physics loop via
  `TickMsg`/`TickCmd`. Other files are feature slices over `World`
  (`maneuver.go` is the largest — node firing/integration; plus
  `staging`, `docking`, `landed`, `navball`, `nav`, `predict`,
  `warp`, `spawn`, `target`).
- **`internal/save`** — versioned JSON envelope (`SchemaVersion = 6`,
  at `$XDG_STATE_HOME/terminal-space-program/save.json`). Accepts
  `[1, SchemaVersion]`; older envelopes auto-migrate via typed
  `save_migrate_v*` functions. Bump the version + add a migration
  whenever persisted state shape changes.
- **`internal/tui`** — Bubble Tea root. `app.go` (`App`) is the root
  `tea.Model`: owns `world`, theme, keymap, and the active screen.
  **Screens read from the shared world; they don't mutate it** —
  state changes go through `sim` methods. `input.go` is the keymap.
  `tui/screens/*` are screen sub-models (orbit / maneuver / porkchop /
  bodyinfo / missions / menu / spawn / help); `tui/widgets/canvas.go`
  is the drawille braille renderer.
- **`internal/render`** — per-pixel body textures + the navball;
  pure pixel/color math, view-aware projection with axial tilts.

## Conventions

- **Integrator switch**: free flight uses Verlet (symplectic); an
  active burn switches to RK4 with rocket-equation mass loss. Warp is
  clamped to ≤10× during burns and ramps down approaching a planted
  node so the integrator can't alias past it. Preserve these clamps
  when touching warp or burn code.
- **Frames matter**: body-bound orbits read Keplerian elements in the
  primary's *equatorial* frame; heliocentric orbits stay
  ecliptic-relative. Don't mix them — `internal/orbital/frame.go` is
  the boundary.
- The in-game `?` help overlay is the source of truth for keybindings;
  `README.md` mirrors it. Update both when changing input.
- `docs/state-of-game.md` is the running per-version changelog/backlog;
  `docs/vX.Y-plan.md` are the cycle plans. Read the current plan before
  starting feature work — scope for a cycle is committed there.
- Version strings live in `internal/version`; don't hand-edit (set via
  ldflags at release).
