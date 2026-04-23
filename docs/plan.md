---
title: terminal-space-program — Implementation Plan
author: fenbot (via /ultraplan, 2026-04-23)
owner: pookiebot
date: 2026-04-23
status: v0 scaffold shipped; porting pending Go toolchain install
---
# terminal-space-program — Implementation Plan

> **Editor's note (pookiebot, 2026-04-23):** This plan was authored by fenbot
> and handed off via sidechat msg 2196 under the working name `orbsim`. All
> eight open questions have since been closed by jason:
>
> 1. **Repo name** → `terminal-space-program` (msg 2209).
> 2. **License** → MIT (msg 2211).
> 3. **Fork vs greenfield** → greenfield with `NOTICE.md` crediting furan917
>    and preserved MIT headers on lifted files (msg 2211).
> 4. **Time-warp UX** → discrete steps (msg 2216).
> 5. **Starting scenario** → LEO-only for v0.1 (msg 2216).
> 6. **Initial system filter** → all four systems shipped; spacecraft
>    restricted to Sol for v0.1 (msg 2216).
> 7. **Sim vs game** → sandbox for v0.1; missions deferred to v0.2 (msg 2216).
> 8. **Release cadence** → tag `v0.1.0` at end-of-week-3 regardless of polish
>    (msg 2216).
>
> Fenbot's original prose is preserved below; treat `orbsim` references in the
> body as historical (module path is `github.com/jasonfen/terminal-space-program`).

## Context

Side-project: terminal-native orbital-mechanics rocket simulator distributed as a single static Go binary.
Two reference repos per Jason (sidechat msg 2194):

- **`github.com/furan917/go-solar-system`** — architectural foundation (Calculator interface, CelestialBody model with full Keplerian elements, multi-system data).
- **`github.com/Cladamos/solcl`** — visual/UX target (Bubble Tea MVC, drawille braille rendering, polished feel). `jellyshell` also called out as bubbletea polish reference.

Prior research: `/home/jason/fenbot/handoffs/2026/04/23/solar-sim-research.md`.

Scope anchor: 2–3 week MVP (v0.1). Not a production system. Not KSP. A side-project sim that's fun to fly.

Working name for this doc: **`orbsim`** (placeholder — see Open Questions).

---

## Goals

1. Fly a spacecraft through a physically-plausible solar system in the terminal.
2. Foundation strong enough to grow from "viewer with a dot on it" to "Hohmann transfer planner with time-warp."
3. `curl | tar | ./orbsim` — no Go toolchain, no libc version dance, no CGO surprises.

## Non-Goals (for v0.1)

- N-body gravity between planets (planets stay on fixed Keplerian tracks).
- Full Lambert/rendezvous targeting UI.
- Atmospheric flight, re-entry heating, aerodynamics.
- Multiplayer, save-game versioning, mods.
- Gorgeous 3D projection. Orthographic top-down, with inclination hinted via color/length, is enough for MVP.

---

## Module Layout

Concrete Go package tree. `orbsim/` is repo root.

```
orbsim/
├── cmd/
│   └── orbsim/
│       └── main.go                    # entry point; wires tea.NewProgram
├── internal/
│   ├── bodies/                        # LIFTED from go-solar-system/internal/models
│   │   ├── body.go                    # CelestialBody struct, Keplerian elements
│   │   ├── systems.go                 # Sol, AlphaCen, TRAPPIST-1, Kepler-452 data
│   │   └── constants.go               # G, AU, seconds-per-day, J2000 epoch
│   ├── orbital/                       # LIFTED+EXTENDED from go-solar-system/internal/orbital
│   │   ├── calculator.go              # Calculator interface (unchanged signature)
│   │   ├── solar_system.go            # SolarSystemCalculator (J2000 mean-anomaly propagation)
│   │   ├── generic.go                 # GenericCalculator (exoplanet pseudo-random placement)
│   │   ├── exact.go                   # ExactCalculator
│   │   ├── kepler.go                  # NEW: Newton-Raphson solver for Kepler's equation
│   │   └── frame.go                   # NEW: inertial <-> planet-centric frame transforms
│   ├── physics/                       # NEW — not in either reference
│   │   ├── state.go                   # StateVector{r, v, m}; gravitational accel helpers
│   │   ├── integrator.go              # Integrator interface
│   │   ├── verlet.go                  # Symplectic velocity-Verlet (v0.1 default)
│   │   ├── rk4.go                     # Classical RK4 (for comparison / non-conservative forces)
│   │   └── soi.go                     # Patched-conic: sphere-of-influence tests, primary switching
│   ├── spacecraft/                    # NEW
│   │   ├── spacecraft.go              # Spacecraft struct: state, mass, fuel, Isp, thrust
│   │   ├── thrust.go                  # Directional thrust (prograde/retro/normal/radial/fixed)
│   │   └── maneuver.go                # Burn plan: (t_start, duration, vector, deltaV_est)
│   ├── planner/                       # NEW — Phase 3
│   │   ├── hohmann.go                 # Circular-orbit Hohmann transfer delta-v + burn times
│   │   ├── lambert.go                 # Stub in v0.1; filled in v0.2+
│   │   └── predictor.go               # Trajectory prediction (forward-integrate w/o applying)
│   ├── tui/                           # NEW — inspired by solcl structure, bubbletea idioms
│   │   ├── app.go                     # Root tea.Model; owns sub-models + focus/state machine
│   │   ├── screens/
│   │   │   ├── orbit.go               # Orbit view screen (drawille canvas + HUD)
│   │   │   ├── bodyinfo.go            # Planet/body info panel
│   │   │   ├── maneuver.go            # Maneuver planner screen
│   │   │   └── help.go                # Keybinding overlay
│   │   ├── widgets/
│   │   │   ├── canvas.go              # drawille-go wrapper; world-to-pixel projection
│   │   │   ├── hud.go                 # Lip Gloss status bars (fuel, velocity, alt, ap/pe)
│   │   │   ├── timewarp.go            # Time-warp indicator (1x, 10x, 100x, 1000x, 10000x)
│   │   │   └── menu.go                # Generic list/menu widget
│   │   ├── input.go                   # Key bindings centralized (charmbracelet/bubbles/key)
│   │   ├── theme.go                   # Lip Gloss styles
│   │   └── project.go                 # 3D -> 2D projection helpers (ortho, tilt)
│   ├── sim/                           # NEW — simulation clock + world state
│   │   ├── world.go                   # World: system + spacecraft + sim-time + warp factor
│   │   ├── clock.go                   # Sim-time vs wall-time; warp stepping
│   │   └── tick.go                    # tea.Msg tick loop; dispatches physics + re-render
│   └── version/
│       └── version.go                 # Populated via -ldflags; printed by `--version`
├── assets/                            # embedded via //go:embed
│   └── systems/                       # JSON overrides for system data (optional for v0.1)
├── go.mod
├── go.sum
├── .goreleaser.yaml
├── Makefile
├── README.md
└── LICENSE
```

### What comes from where

| Package | Source | Notes |
|---|---|---|
| `internal/bodies` | Copied from `go-solar-system/internal/models` | Keep the struct shape; drop anything tcell-specific. |
| `internal/orbital/{calculator,solar_system,generic,exact}.go` | Copied from `go-solar-system/internal/orbital` | Interface unchanged; drop display hooks. |
| `internal/orbital/kepler.go` | New (referenced in research as "not in either repo") | ~40 LOC Newton-Raphson. |
| `internal/physics/*` | New | Core contribution. |
| `internal/spacecraft/*` | New | Core contribution. |
| `internal/tui/*` | New, inspired by solcl's `model/model.go` layout and jellyshell polish | Use bubbletea idioms throughout. |
| `drawille-go` | External dep, from solcl's stack | Only for the canvas widget. |
| `tcell/v2` | **Not imported.** | bubbletea owns the terminal; we don't mix. |

### Dependency choices (go.mod)

```
github.com/charmbracelet/bubbletea      v2.x
github.com/charmbracelet/bubbles        v2.x
github.com/charmbracelet/lipgloss       v2.x
github.com/exrook/drawille-go           (or go-drawille fork that's maintained)
golang.org/x/term                       (for size detection fallback)
```

No `tcell`, no CGO-linked libs (SQLite, image/heif, etc.), no `cgo` anywhere. This is load-bearing for single-binary distribution — see "Single-Binary Strategy."

---

## Physics Progression

Four phases. Each phase is runnable end-to-end before the next begins. Phase 0–2 are MVP. Phase 3 is stretch.

### Phase 0 — Viewer parity (solcl look, go-solar-system brain)

**Goal:** Match solcl visually; beat it structurally. No spacecraft yet.

- Port `Calculator` interface and `SolarSystemCalculator` from go-solar-system as-is.
- Compute body positions at sim-time `t` from Keplerian elements (`a, e, i, ω, Ω, M₀`) via mean-anomaly propagation + Newton-Raphson for E (eccentric anomaly).
- Project 3D heliocentric coords to 2D ortho view; render on drawille canvas.
- Bubble Tea model: orbit screen + body-info panel; arrow keys cycle bodies; `s` switches system.
- Time-warp stub: integer multiplier on sim-dt per wall-tick.

**Exit criterion:** Sol, Alpha Cen, TRAPPIST-1, Kepler-452 all render; planet positions match `go-solar-system` within numerical noise at any `t`; UI feels like solcl.

### Phase 1 — Spacecraft as propagated body

**Goal:** A dot obeys gravity.

- `Spacecraft` struct with `StateVector{r, v, mass, fuel}`.
- Patched-conic model: at any `t`, exactly one primary (Sun by default; a planet if spacecraft is inside that planet's SOI).
- **Integrator choice: velocity-Verlet (symplectic).** Justification:
  - Two-body gravity is conservative — symplectic integrators preserve energy over long time horizons; RK4 drifts.
  - Single force eval per step; cheap. Important because time-warp multiplies step count.
  - Simple to implement (~30 LOC) and simple to reason about.
  - RK4 kept in tree for Phase 2 where thrust (non-conservative) is active during a burn — switch integrators inside burns.
- SOI check every `N` steps (cheap); on crossing, rebase state vector to new primary's frame (`orbital/frame.go`).
- Spawn spacecraft in low circular parking orbit around Earth at sim-start.

**Exit criterion:** Uncontrolled spacecraft in a stable circular orbit stays within 1% semi-major-axis drift over 100 simulated orbits.

### Phase 2 — Thrust, burn planning, delta-v

**Goal:** Fly it.

- `thrust.go`: direction modes — prograde, retrograde, normal+, normal-, radial in/out, plus a free-form `(pitch, yaw)` locked to the current velocity frame.
- Instant burn (impulsive) for MVP; finite burn (thrust over duration, mass decreases via rocket equation `Δv = Isp·g·ln(m0/m1)`) as follow-on within Phase 2.
- Manual burn UI: pause sim, dial in direction + delta-v magnitude, preview predicted orbit (`planner/predictor.go` forward-integrates a shadow spacecraft for N steps and draws the trajectory on canvas).
- HUD shows: altitude above primary, orbital velocity, apoapsis, periapsis, inclination, available fuel, estimated remaining delta-v.

**Exit criterion:** User can raise apoapsis from LEO to GEO altitude with a manual prograde burn, time-warp to apoapsis, circularize with a second burn, end in stable GEO.

### Phase 3 — Maneuver library (stretch, may slip past v0.1)

- **Hohmann transfer** (`planner/hohmann.go`): given current circular orbit and target altitude, compute both burn magnitudes and the time to execute burn 2.
- **Porkchop / Lambert solver** (`planner/lambert.go`): stub the interface; leave implementation to v0.2.
- **SOI-crossing predictor**: extend trajectory predictor to detect and visualize planet-SOI captures/escapes.
- **Planned maneuver nodes** on the orbit view (KSP-style): place, drag, and execute.

---

## TUI Design

### State machine (bubbletea root model)

```
                 ┌──────────────┐
    startup ───▶ │  OrbitView   │ ◀─┐
                 └─┬────────────┘   │
                   │ 'i'            │ Esc
                   ▼                │
                 ┌──────────────┐   │
                 │  BodyInfo    │ ──┘
                 └──────────────┘
                   │ 'm' (from OrbitView)
                   ▼
                 ┌──────────────┐
                 │ ManeuverPlan │ ──┐
                 └──────────────┘   │ Esc / Execute
                                    ▼
                             OrbitView (updated)
```

Every screen is its own `tea.Model`, returned via `tea.Cmd` from the root. Messages:

- `tickMsg` — physics step (frequency = `1/warpFactor * baseHz`).
- `keyMsg` — routed by root to active screen first; global bindings (quit, warp up/down, pause) handled by root.
- `burnExecutedMsg` — from planner screen back to world.
- `soiCrossMsg` — emitted by physics; world handles primary switch, HUD updates.

### Screen details

**OrbitView** (`internal/tui/screens/orbit.go`)
- Left ~75%: drawille canvas. Sun at center; current primary highlighted; orbits drawn as dotted ellipses; spacecraft as a distinct glyph; predicted trajectory as a faint second curve.
- Right panel (lipgloss box): mini-HUD (altitude, velocity, ap/pe, fuel, sim-time, warp factor).
- Bottom status bar: key hints.

**BodyInfo** (`internal/tui/screens/bodyinfo.go`)
- Orbital elements table, physical properties, distance from spacecraft, delta-v-to-reach estimate (rough Hohmann lower bound).

**ManeuverPlanner** (`internal/tui/screens/maneuver.go`)
- Form (bubbles/textinput + custom knob widget): direction mode, magnitude (m/s), optional execute-at-time.
- Live trajectory preview on a miniature canvas as the user dials parameters.
- `Enter` commits; `Esc` cancels.

**Help overlay** (`internal/tui/screens/help.go`)
- Modal over current screen. `?` toggles.

### Drawille canvas wrapper (`internal/tui/widgets/canvas.go`)

- Owns a `drawille.Canvas`.
- Exposes `Plot(worldX, worldY float64)` and `Line(x0,y0,x1,y1 float64)` — handles world-to-pixel projection (ortho + zoom + pan).
- Zoom is per-screen state; `+`/`-` keys. Auto-zoom on primary switch to keep SOI visible.

### Time-warp UX

Jason called this out explicitly. KSP-inspired:

- Warp factors: 1×, 10×, 100×, 1000×, 10000×, 100000× (six discrete steps).
- `.` / `,` to step up / down. `0` to pause.
- Warp is clamped during active burns (finite-burn mode) to ≤10× so the integrator stays accurate.
- Warp indicator in top bar with chevrons (`▶ ▶▶ ▶▶▶ …`) like solcl's aesthetic.
- When inside an SOI, cap warp so we don't skip over the SOI with one giant step (step-size guard: `dt * warp < orbital_period/100`).

### Visual polish references

- solcl's clean panel framing → replicate with `lipgloss.Border`.
- jellyshell's contextual help footer → replicate via persistent footer bar on every screen.
- Subtle color palette: primary cyan, warning amber, alert red, dim gray for orbits.

---

## Single-Binary Strategy

The whole point of this doc, per Jason: "simple binary instead of have go as a prereq."

### Build flags

```bash
CGO_ENABLED=0 \
GOOS=${os} GOARCH=${arch} \
go build \
  -trimpath \
  -ldflags="-s -w -X 'orbsim/internal/version.Version=${VERSION}' -X 'orbsim/internal/version.Commit=${SHA}'" \
  -o dist/orbsim-${os}-${arch}${ext} \
  ./cmd/orbsim
```

- `CGO_ENABLED=0` — **non-negotiable.** Any CGO dep kills portable linux (glibc vs musl) and breaks static binaries.
- `-trimpath` — strips build-machine paths; reproducibility.
- `-ldflags="-s -w"` — drops DWARF and symbol table; ~30% smaller binary. Accept that stack traces are less pretty; if that matters in the wild we can ship a debug variant.
- Version injection via `-X` so `orbsim --version` works without embedding a file.

### Target matrix

| GOOS    | GOARCH | Notes |
|---------|--------|-------|
| linux   | amd64  | Primary |
| linux   | arm64  | Jason's laptop / Pi |
| darwin  | amd64  | Intel Macs |
| darwin  | arm64  | Apple Silicon |
| windows | amd64  | Include `.exe` suffix |

Dropping `windows/arm64`, `freebsd/*`, etc. until someone asks. GoReleaser makes adding them trivial later.

### CGO traps to avoid

- **SQLite** — classic CGO offender. If we ever persist save-games, use `modernc.org/sqlite` (pure-Go) or just gob/JSON.
- **Image libs** — irrelevant here (terminal-only), but flagging so future "let's add a splash screen" doesn't sneak `libpng` in.
- **tcell** — pure Go but unused. Confirm via `go mod why` before shipping.
- **Terminfo** — bubbletea uses pure-Go terminfo; safe.

### Release automation

**GoReleaser.** `.goreleaser.yaml` stanza outline:

- `builds:` — one entry with the target matrix above, env `CGO_ENABLED=0`, the ldflags block.
- `archives:` — `.tar.gz` for unix, `.zip` for windows; embed README + LICENSE.
- `checksums:` — SHA256SUMS alongside releases.
- `release:` — GitHub release on tag push.
- **No Homebrew / no nix / no AUR for v0.1.** Raw release artifacts only. Those are adoption problems we don't have yet.

CI: single GitHub Actions workflow triggered on tags, runs `goreleaser release --clean`. One file, one secret (`GITHUB_TOKEN`). Done.

### Install UX

```bash
curl -L https://github.com/jasonfen/orbsim/releases/latest/download/orbsim-linux-amd64.tar.gz | tar xz
./orbsim
```

No package manager, no Go toolchain, no libc dance. That's the bar.

---

## MVP Scope (v0.1)

**Shipping in v0.1 (hard commitment):**

- Phase 0 viewer (all four reference systems).
- Phase 1 spacecraft propagation in Sol only, starting in LEO.
- Phase 2 impulsive burns with prograde/retro/normal/radial + magnitude.
- Trajectory prediction preview in maneuver planner.
- Time warp 1×–10000×.
- `orbit`, `bodyinfo`, `maneuver`, `help` screens.
- Static binaries for the 5-target matrix.
- README with install one-liner and keybinding cheatsheet.

**Explicitly deferred to v0.2+:**

- Finite-duration burns and mass-flow rocket equation (stub the UI, compute impulsive for now).
- Phase 3 maneuver library (Hohmann planner, Lambert, SOI-crossing predictor UI).
- Multi-system spacecraft (spacecraft only exists in Sol for MVP).
- Save/load.
- N-body perturbations.
- Config file / custom systems via JSON.
- Mouse support.
- Replays / flight recorder.

**Timeline against Jason's 2–3 week estimate:**

Rough slicing (calendar-weeks, assuming evenings + weekends):

- Week 1: Port Phase 0 + stand up bubbletea skeleton. Target: runnable viewer, feels like solcl.
- Week 2: Phase 1 physics + spacecraft HUD. Target: orbit a planet, watch it work.
- Week 3: Phase 2 burns + maneuver screen + release pipeline. Target: v0.1 tag, binaries on GitHub.

If any week slips, cut Phase 3 work (already deferred) before cutting polish. A janky sim with no polish gets closed after one session; a polished sim with fewer features gets shown to friends.

---

## Risks & Mitigations

| Risk | Mitigation |
|---|---|
| Numerical drift under high warp | Cap warp by integrator step size; use symplectic Verlet; validate with energy-conservation test. |
| Drawille canvas flicker under rapid ticks | Bubbletea's diff renderer handles this if we return consistent `View()` output; keep HUD strings stable. |
| Frame-transform bugs at SOI crossings | Unit test `frame.go` transforms round-trip; hand-verified scenarios (free-return trajectory should close to within tolerance). |
| Bubbletea v2 API churn | Pin minor version in go.mod; skim CHANGELOG before bumping. |
| "I want an N-body sim" scope creep | This doc is the shield; v0.1 is explicitly two-body + patched-conic. |
| Licensing of go-solar-system code we're lifting | See Open Questions — decide before first commit that contains ported code. |

---

## Testing Strategy (lightweight, this is a side project)

- `internal/orbital/kepler_test.go` — Kepler solver converges for `e ∈ {0, 0.1, 0.5, 0.9}`.
- `internal/orbital/solar_system_test.go` — body positions match known J2000 values within tolerance (lift test vectors from go-solar-system if present).
- `internal/physics/verlet_test.go` — energy conservation over 1000 orbits < 1% drift.
- `internal/physics/soi_test.go` — round-trip frame transforms; SOI boundary detection.
- `internal/planner/hohmann_test.go` — classical textbook problem (LEO→GEO delta-v matches published value).
- No UI tests for MVP. Manual QA via a `scripts/scenarios/` folder of `.txt` key-replay files if we get fancy.

---

## Open Questions for Jason

All closed as of 2026-04-23. Historical text preserved below for context.

1. ~~**Repo name.**~~ **Closed 2026-04-23 (msg 2209):** `terminal-space-program`.
2. ~~**License.**~~ **Closed 2026-04-23 (msg 2211):** MIT.
3. ~~**Fork vs greenfield.**~~ **Closed 2026-04-23 (msg 2211):** greenfield + `NOTICE.md`, MIT headers preserved on lifted files.
4. ~~**Time-warp UX.**~~ **Closed 2026-04-23 (msg 2216):** discrete steps (1×/10×/100×/1000×/10000×/100000×) per the recommendation.
5. ~~**Starting scenario.**~~ **Closed 2026-04-23 (msg 2216):** LEO-only for v0.1.
6. ~~**Initial system filter.**~~ **Closed 2026-04-23 (msg 2216):** ship all four systems (Sol, Alpha Cen, TRAPPIST-1, Kepler-452); spacecraft restricted to Sol for v0.1.
7. ~~**"Sim" vs "game."**~~ **Closed 2026-04-23 (msg 2216):** sandbox for v0.1; missions deferred to a v0.2 design pass.
8. ~~**Release cadence.**~~ **Closed 2026-04-23 (msg 2216):** tag `v0.1.0` at end-of-week-3 regardless of polish level — shipping forces decisions.

---

## Appendix: Load-bearing external references

- go-solar-system: `https://github.com/furan917/go-solar-system`
- solcl: `https://github.com/Cladamos/solcl`
- bubbletea: `https://github.com/charmbracelet/bubbletea`
- drawille-go: pick maintained fork at go.mod time (check `pkg.go.dev` for recent activity)
- Research handoff: `/home/jason/fenbot/handoffs/2026/04/23/solar-sim-research.md`
