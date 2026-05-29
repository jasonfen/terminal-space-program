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

## Install

Latest release: **v0.9.1** — staging chain. KSP-style player-managed sequential decouples; `space` drops the bottom stage and spawns it as a passive cycle-able craft, with the active vehicle continuing on the upper-stage chain. Saturn-V loadout (S-IC + S-II + S-IVB, TWR > 1 at sea level) ships alongside the four single-stage loadouts, which wrap into a one-element `Stages: [{...}]` shim with no behavior change. New STAGES HUD block, `Spacecraft.Stages` source-of-truth for dry mass / propellant / engine numbers, save schema v5 → v6 with typed migration. Followed v0.9.0 (unified `World.Target` slot, `t` / `T` cycle / clear, TARGET HUD).

**v0.9.4 ascent ergonomics in flight.** Ground launch primitives
landed in v0.9.2 (`n` → POSITION → launchpad spawns a Saturn V at
altitude 0 on a rotating Earth, with surface-frame SAS, pitch trim,
and a LAUNCH HUD), and v0.9.4 layers KSP-style live guidance on top:
predictive **ap / pe / t-to-apo / Δv→circ** readouts in the LAUNCH
HUD, an **ORBIT READY** callout when apoapsis crosses 200 km,
auto-snap to NavSurface on launchpad spawn (so `w` already means
surface-prograde), and a single-key **`C`** that plants the
circularisation node at next apoapsis. Pad-to-LEO is now a
playable loop — see [Surface launches](#surface-launches) below.

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

You spawn as **S-IVB-1** in a 500 km circular prograde LEO. The left
panel is the orbit canvas — Sun (or whichever body you focus on) at
the center, planets on their actual orbits, your spacecraft as a
small chevron oriented along velocity. The right HUD shows clock,
focus, vessel + propellant state, attitude, planned nodes, projected
post-burn orbit, frame transitions, system info, and a Hohmann preview
to the cursor-selected body. Time-warp with `.` / `,` to watch planets
move; pause with `0` or space.

To make something happen:

1. Press `←`/`→` (or click) to scroll the cursor through bodies. Pick Mars.
2. Press `H` to plant a Hohmann transfer — two finite-burn nodes
   appear (geocentric departure + Mars-frame arrival), each
   color-coded with its predicted post-burn orbit on the canvas,
   listed in the HUD with Δv and time-to-fire.
3. Time-warp forward. The departure node fires at its trigger time;
   warp clamps to ≤10× during the burn so the integrator keeps
   temporal resolution. Your trajectory unrolls past Earth's SOI,
   the predictor switches frames, and the curve bends sunward as
   it should.
4. The arrival node fires near Mars and drops you into a low
   capture orbit. For phasing-aware launch windows, use `P`
   (porkchop) instead of `H`.

For burns by hand, press `m` to open the planner. Pick a mode
(prograde / retrograde / normal± / radial±), choose when it fires
(absolute T+, or *next peri / next apo / next AN / next DN*), set
Δv, and pick a throttle. Burn duration is derived from Δv via the
rocket equation — no separate field. The PROJECTED ORBIT block
previews apo / peri / AN / inclination of the result live as you
edit. Commit with Enter; the integrator switches from Verlet (free
flight) to RK4 (thrust) and ticks the burn across multiple frames
with mass loss tracked from the rocket equation.

For real-time stick (KSP-style), throttle up with `z`, attitude
with `w`/`s`/`a`/`d`/`q`/`e`, then engage with `b`.

### Surface launches

Spawn a Saturn V on the pad and fly it to LEO by hand. The flow
mirrors KSP: tip the rocket 10° east, switch SAS to surface-prograde,
let gravity bend the velocity vector over, watch the LAUNCH HUD's
live ap / pe / Δv→circ shrink as you burn, plant the circularisation
node at apoapsis with `C`, coast and fire. v0.9.4+ wires the live
KSP-style readouts so you can fly by watching numbers.

Suggested sequence for a Saturn V → LEO attempt:

1. `n` opens the spawn form. Cycle `POSITION` to **launchpad**, set
   `LATITUDE` to a preset (28.6° N = Cape Canaveral KSC), pick
   `CRAFT TYPE = Saturn V`, press `Enter`. SAS comes up at radial+
   (vertical) and **NavMode auto-snaps to Surface**, so `w` already
   means surface-prograde — no `;` press needed.
2. `z` (full throttle), `b` (engage). The S-IC lifts vertical at
   TWR ≈ 1.24.
3. At ~3 km altitude, tap `>` once to trim 10° east. The thrust
   vector tilts; horizontal velocity starts climbing.
4. As surface velocity passes ~100 m/s, press `w` for surface-
   prograde SAS and `\` to clear the pitch trim. The craft now
   tracks its own velocity vector — gravity bends it over and the
   gravity turn falls out of the physics.
5. Press `space` to decouple the spent S-IC. The active craft
   stays on the upper stage; STAGES HUD advances. Continue burn
   through S-II, decouple again, then S-IVB.
6. Watch the LAUNCH HUD's `ap:` row climb. When it crosses 200 km,
   the **● ORBIT READY** callout appears — that's the cue to cut
   throttle (`x`) and coast.
7. Press `C` to plant the circularisation node at next apoapsis.
   Status flashes `circularize @ apoapsis (X km) → +Y m/s prograde`
   and the PROJECTED ORBIT block shows the post-burn circle.
8. Coast to apoapsis. The planted node fires automatically and
   raises periapsis to match. Mission `saturn-v-pad-to-leo` passes
   when `pe > 200 km` — the LAUNCH HUD's `mission:` line counts
   down to that threshold along the way.

The whole loop runs on numbers, not memorised pitch tables: ap
climbs while you burn, ORBIT READY tells you when to coast, `C`
plants the burn, mission progress closes the gap. If anything
flames out, the LAUNCH HUD's predictive readouts surface the
"why" before you watch the craft re-enter.

## Keybindings

The in-game `?` overlay is the source of truth; this table mirrors it.

### Global

| Key | Action |
|---|---|
| `Esc` | Back / open splash menu (save / load / quit) on home view |
| `Ctrl+C` | Quit immediately |
| `?` | Toggle help overlay |
| `i` | Body info screen |
| `Tab` | Switch system (Sol → Alpha Cen → TRAPPIST-1 → Kepler-452) |
| `0` | Pause / resume sim (v0.9.1 dropped `space` from this binding — see Orbit view) |
| `.` / `,` | Warp up / down (1× → 100000×; clamped to ≤10× during a burn) |

### Orbit view

| Key | Action |
|---|---|
| `→` / `l` | Cursor: next body |
| `←` / `h` | Cursor: previous body |
| `+` / `-` | Zoom in / out |
| `f` / `F` | Cycle camera focus forward / backward (system → bodies → craft) |
| `g` | Reset camera focus to system |
| `v` | Cycle view (tilted → top → right → bottom → left → orbit-flat). `tilted` (v0.10.6+) is the new default — perspective tilt over the active craft's perifocal basis, with far-side orbit arcs rendered as same-hue stipple for depth read |
| `shift+↑` / `shift+↓` | While in `ViewTilted`, nudge the polar tilt θ ±5° (clamped 0–60°). HUD shows `view: tilted Nº` when off the 25° default. No-op in cardinal / orbit-flat modes (v0.10.6+) |
| `n` | Open spawn form (loadout / position / parent body / altitude / direction). Pick **Custom…** for the v0.10.1+ stack builder: on the STACK field, `←/→` picks a catalog part, `a` adds it on top, `x` removes the top stage |
| `H` | Auto-plant Hohmann transfer to `World.Target` body (intra-primary for moons of the craft's parent; moon → parent escape via bound transfer ellipse). TargetCraft flashes "needs v0.9.3" |
| `I` | Plant inclination match — rotates the orbital plane to `World.Target` body's inclination (or 0° equatorial when target is None). TargetCraft flashes "needs v0.9.3" |
| `C` | Plant circularize burn at next apoapsis (v0.9.4+) — pairs with the LAUNCH HUD's ORBIT READY callout. Errors when apoapsis is below the primary's atmosphere cutoff or the orbit is hyperbolic |
| `K` | Plant rendezvous nudge to target craft (v0.10.2+) — single-burn Lambert intercept projected onto the closest velocity-frame axis. Reads the TARGET HUD's ACH CA / Δv readouts. Errors when there's no craft target, target shares a different primary, already DOCK READY, or no improvement available |
| `t` / `T` | Cycle / clear `World.Target` (non-active sibling craft → bodies in active system → none) |
| `space` | Decouple bottom stage of active craft (multi-stage only; single-stage status-flashes "cannot drop the only remaining stage") (v0.9.1+) |
| `P` | Porkchop plot for selected body; `Enter` on a cell plants that Lambert transfer. Inter-primary only — moon targets show a banner redirecting to `H`. Press `o` inside to open the transfer-options sub-menu (`n` cycles nRev 0–3, `r` toggles prograde/retrograde, `b` toggles short/long branch); `enter`/`o`/`esc` closes and re-solves (v0.10.5+) |
| `R` | Refine plan — re-Lambert from live state, plant mid-course correction + update arrival |
| `m` | Open maneuver planner |
| `F5` / `F9` | Quicksave / quickload (`$XDG_STATE_HOME/terminal-space-program/save.json`) — KSP-style |
| `[` / `]` | Cycle active craft (no-op when only one craft loaded) |
| `1`–`9` | Jump directly to craft N (no-op when that slot is empty) |
| `U` | Undock active composite |

### Manual flight

| Key | Action |
|---|---|
| `z` / `x` | Throttle full / cut |
| `Z` / `X` | Throttle +10 % / -10 % |
| `w` / `s` | Attitude prograde / retrograde |
| `a` / `d` | Attitude normal+ / normal- |
| `q` / `e` | Attitude radial+ / radial- |
| `b` | Engage / cut manual burn (main engine, throttle > 0) |
| `r` | Engine: main / RCS (v0.8.0+) |
| `k` | SAS model: slew / instant — MANUAL (rate-limited, default) vs AUTO (legacy instant snap); navball `[MAN]`/`[AUT]` tag mirrors it (v0.10.0+) |
| `W` / `S` | Attitude surface-prograde / surface-retrograde — track velocity in the rotating-atmosphere frame (v - ω × r). For ascent gravity turn (v0.9.2+) |
| `>` / `<` | Pitch trim ±10° east / west — rotate thrust vector about local-north axis on top of current SAS mode (v0.9.2+) |
| `\` | Reset pitch trim to 0 (v0.9.2+) |

Attitude keys orient only in main mode — pressing `b` is what fires
the engine. In RCS mode the attitude keys *also* fire one 0.1 m/s
monoprop pulse per keypress (held keys produce a sustained pulse
train at the terminal's key-repeat rate). The HUD's ATTITUDE block
shows the armed engine; the PROPELLANT block shows monoprop level
and remaining RCS Δv.

### Mouse

Click-only. No drag, no wheel-zoom.

| Click target | Action |
|---|---|
| `[Menu]` (top-right) | Save / load / quit confirmation menu |
| `[Missions]` (top-right) | Mission list with status glyphs (✓/✗/·) |
| Body | Focus body (same as cursoring onto it with `←` / `→`) |
| Vessel | Focus craft |
| Planted node | Open planner pre-loaded for that node (edit-replace, fire time preserved) |
| Empty canvas | Open planner with a new node staged at the orbit point nearest the click |
| HUD panel | Open body info |
| Porkchop cell | Move cursor to that cell (then `Enter` to plant) |

### Maneuver planner (`m`)

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Cycle field focus (mode → fire-at → Δv → throttle → iterate) |
| `←` / `→` | Cycle the focused cycle field (mode / fire-at / iterate) |
| `Space` | Toggle iterate-for-target when focused on the iterate field |
| digits / backspace | Edit Δv or throttle |
| `Enter` | Commit burn |
| `Esc` | Cancel and back to orbit view |
| `Ctrl+D` | Delete the planted node being edited (no-op when creating new) |
| `c` / `C` | Clear ALL planted nodes for the active craft (`Ctrl+K` still works) |

The form panel lists every planted node for the active craft
(mode / Δv / fire-time countdown), with the node under edit
flagged — so the planner shows the whole schedule, not just the
burn being created.

Δv drives both delivered Δv **and** burn duration via the rocket
equation `t = (m₀/ṁ)·(1 − exp(−Δv/(Isp·g₀)))`. Zero-thrust craft
fall back to impulsive automatically. The fire-at field selects
absolute T+ or one of `next peri / next apo / next AN / next DN`;
event-relative nodes resolve their trigger lazily on the first tick
after plant. The throttle field is per-node (not the live craft
throttle) so adjusting throttle during a coast doesn't slow an
in-flight planted burn. PROJECTED ORBIT readouts apo / peri / AN /
inclination of the resulting orbit live as you edit.

The **iterate** toggle (v0.8.6.3+) routes the commanded Δv through
`planner.IterateForTarget` at plant time — Newton-iterates against
an RK4 finite-burn simulation to refine the Δv up so the post-burn
apsides match what an impulsive Δv at the same value would have
delivered. Compensates gravity-rotation + thrust-vector-rotation
losses on long burns. Off by default (impulsive-target semantics
are good enough for short burns); flip on for low-TWR loadouts or
high-Δv burns where finite-burn loss is measurable. Skipped for
Normal± burns (no apse target — use `I` for plane-rotation Δv).

### Porkchop plot (`P`)

| Key | Action |
|---|---|
| `←` / `→` | Departure-day cursor |
| `↑` / `↓` | Time-of-flight cursor |
| `Enter` | Plant Lambert transfer for selected cell |
| `o` | Open transfer-options sub-menu — `n` cycles nRev (0–3), `r` toggles prograde/retrograde, `b` toggles short/long branch (only meaningful for nRev ≥ 1); `Enter`/`o`/`Esc` closes and re-solves the grid (v0.10.5+) |
| Click cell | Move cursor to that cell (then `Enter` to plant) |
| `Esc` | Back to orbit view |

Cursor opens snapped to the minimum-Δv cell. `·` glyphs mark cells
where Lambert didn't converge — `Enter` on those is a no-op. The TOF
axis range auto-scales by `(nRev + 1)` so multi-rev cells stay inside
the displayed bracket.

## Features

- **Two-body patched-conic propagation** with SOI-aware state
  transitions. Symplectic Verlet for free flight, RK4 for active
  burns. Stumpff-universal-variables Lambert solver (Curtis
  Algorithm 5.2) with an explicit prograde/retrograde flag.
- **Auto-plant transfers**. `H` plants Hohmann (heliocentric,
  intra-primary, or moon-escape — the planner picks based on
  craft + target frames). `P` shows a porkchop heatmap; `Enter`
  on a cell plants the Lambert-derived transfer. `R` re-runs
  Lambert from live state to refine the planted arrival.
- **Burn scheduling**. Maneuver nodes fire at absolute T+ or on
  the next periapsis / apoapsis / AN / DN crossing — the planner
  resolves event-relative nodes lazily against the live orbit.
  Inclination-change planner (`I`) plants a single normal-burn
  to rotate the orbital plane.
- **Per-node throttle**. The maneuver form's throttle field is
  captured at fire-time onto `ActiveBurn`, so adjusting the live
  throttle knob mid-coast doesn't slow a planted burn.
- **Manual flight**. Real-time stick layered on top of the
  planted-node planner. Throttle, six attitude modes, and an
  explicit `b` engage so accidental attitude-key presses can't
  fire the engine.
- **RCS / monopropellant mode** (v0.8.0+). `r` toggles between the
  main engine and a precision-maneuver monoprop thruster pool.
  In RCS mode each attitude keypress fires a fixed 0.1 m/s pulse
  off the monoprop budget (~30 m/s on the default S-IVB-1) — the
  proximity-ops thruster v0.8.3 docking will lean on. Each pulse
  drops a fading puff marker on the canvas (placeholder visual;
  v0.8.2 replaces with per-thruster glyphs).
- **Multi-craft slate** (v0.8.1+). `n` opens the spawn form
  (loadout / parent body / altitude / direction); `[`/`]` cycles
  which craft the player is flying. Each craft owns its own
  planted nodes, in-flight burn, attitude, and engine state —
  burns fire on the craft they were planted for regardless of
  which craft you're currently flying. The HUD's `BURNS` and
  `NODES` blocks list every craft's state simultaneously; clicking
  a node row opens the maneuver planner for edit-replace. Title
  bar shows `CRAFT N/M` chip when more than one craft is loaded.
  Save schema bumped v4 → v5 to nest per-craft state.
- **Craft types** (v0.8.2+). Four loadouts in the launch catalog:
  S-IVB-1 (yellow `▲`, J-2 third stage), ICPS (blue `◆`, RL-10
  low-TWR), RCS-tug (pink `●`, pure monoprop, no main engine),
  Lander (mint `▼`, throttleable descent stage). Each carries
  propulsion + visual differentiation; the orbit canvas renders
  every craft with its loadout glyph + color so they read
  distinctly even at small zoom.
- **Multi-tier stack + configurator** (v0.10.1+). The **Apollo
  Stack** loadout (S-IC → S-II → S-IVB → LM → CSM) flies the full
  mission arc on the v0.9.1 staging chain: decoupling the mid-stage
  LM spawns it as its own controllable craft (payload separation),
  leaving the CSM core to fly the rendezvous / return. The spawn
  form's **Custom…** entry opens a stack builder over a named stage
  catalog (S-IC / S-II / S-IVB / ICPS / SRB / Core / F9 stages /
  Lander / CSM / RCS-tug) — assemble any bottom-to-top stack and
  spawn it. Custom craft round-trip through save with no schema
  bump (per-stage state already persists at v6).
- **Capture preview** (v0.8.2+). Plant a Hohmann to another body
  and the HUD's `CAPTURE PREVIEW` block shows what you'll arrive
  with — relative approach speed and predicted prograde /
  retrograde direction (a prograde Hohmann to Luna naturally
  captures retrograde, ~110° around Luna; the preview surfaces
  this before fire so you're not surprised). Inclination match
  also works from equatorial source orbits.
- **Atmospheric drag** (v0.8.4+). Earth + Mars carry exponential
  atmospheres (`ρ(h) = ρ_0 · exp(-h/H)`) with co-rotating velocity
  reference, wired through both the live integrator (drag-aware
  Verlet) and the predictor — so projected orbits decay realistically
  on atmospheric-skim trajectories. Below the cutoff altitude the
  Kepler warp-lock retreats to Verlet sub-stepping; surface-impact
  craft clamp to the body and stop. A faint haze ring renders at
  cutoff + scale-height around bodies with atmospheres.
- **Docking + undocking** (v0.8.3+). Two craft within 50 m and
  below 0.1 m/s relative velocity sharing a primary frame fuse
  on the next tick — composite at the mass-weighted centroid
  with momentum-conserving velocity, summed fuel + monoprop
  pools, inheriting the active partner's identity (name, glyph,
  color, planted nodes). `U` undocks back into the original
  components, sharing the composite's pools proportionally.
  HUD's `RENDEZVOUS` block surfaces live range + relative
  velocity to the nearest co-orbiting craft so the player can
  RCS-null the residuals; spawn form's `POSITION = alongside
  active` drops a new craft inside the docking gate for
  testing. Engine-firing flame visual + per-thruster RCS puff
  visual replace the v0.8.0 placeholder dot.
- **Predicted post-burn orbit**. PROJECTED ORBIT block on both
  the orbit screen and `m` form chains every planted node, frame-
  rebases per node (so a Hohmann arrival reads in the destination
  frame), and shows apo / peri / AN / inclination of the
  resulting orbit live.
- **Frame transitions**. The HUD surfaces upcoming SOI / frame
  changes implied by the planted-node chain (e.g. the zero-Δv
  arrival marker from a moon-escape).
- **Body hierarchy + moons**. Earth + Luna, Mars + Phobos /
  Deimos, the four Galilean moons, Titan, Enceladus. Recursive
  `BodyPosition` / `bodyInertialVelocity` so SOI math walks the
  hierarchy correctly.
- **Per-pixel body textures** (v0.8.5+). Sun (limb-darkened solar
  disk + sunspots + corona halo), Earth (polygon-rasterised 144×72
  continental mask with biome-shaded land + deserts + ice +
  atmospheric blue-marble limb), Moon (near-side maria + far-side
  Orientale / Moscoviense / Ingenii / South Pole-Aitken basin +
  polar craters), Mars (Syrtis Major / Solis Lacus / polar caps),
  Jupiter (10-band zone/belt + Great Red Spot), Saturn (banded
  cloud + polar hexagon + four-band ring system C / B / Cassini
  Division / A / F), the four Galileans (Io paterae, Europa lineae,
  Ganymede regiones, Callisto crater rays), Uranus (subtle pole-on
  banding from its 98° tilt), Neptune (banded blue + Great Dark
  Spot). Render at body radii ≥ 12 px; below that bodies fall
  back to a colored disk.
- **Sim-time planet rotation** (v0.8.5+). Body axes spin at
  sidereal-rotation rates; tidally-locked moons (Luna, Phobos,
  Deimos, Galileans, Titan, Enceladus) keep the same face pointed
  at their parent regardless of orbit phase. View-aware projection
  combined with axial tilts (Earth 23°, Mars 25°, Saturn 27°,
  Uranus 98° — the iconic roller, etc.) means top / right / left /
  bottom views show genuinely different geometry per body — top
  view of Earth reveals the Arctic, side views show the equator
  with surface features drifting east at the body's rotation rate.
  Rotation rate caps above warp 10000× so the disk stays
  watchable at extreme warp.
- **Tilted ring system** (v0.8.5+). Saturn's rings render in the
  body's equatorial plane and foreshorten correctly per camera
  view: ~89% aspect from top, ~45% aspect from side, edge-on flat
  perpendicular to the tilt direction.
- **Body-equatorial orbital frame** (v0.8.6.1+). Keplerian
  elements (i, Ω, ω) for body-bound orbits read in the primary's
  equatorial frame — ECI for Earth, MCI for Mars, etc. — matching
  the operational mission-planning convention. A 0° inclination
  Earth orbit physically passes over the equator (Ecuador), not
  over the world ecliptic plane (Guatemala). Heliocentric orbits
  stay ecliptic-relative, the standard astronomical convention.
  PlaneMatchInclination resolves "match this body's plane" from
  any orbit (e.g. tilting LEO by ~23° to match Mars's heliocentric
  plane before TLI).
- **Adaptive warp clamps** (v0.8.6.2+). Three layered guards
  prevent the integrator from aliasing a planted burn:
  - Burn-active cap (10× during ActiveBurn / ManualBurn) —
    pre-existing.
  - Throttle-change cap (10× for 1 sim-second after Throttle
    changes) — catches high-warp throttle ramps that alias the
    same way held burns do.
  - Upcoming-node approach cap (continuous: warp ramps down as
    the next planted node nears, reaching 10× at 5 s out) —
    prevents 100,000× warp from skipping past a 30-s-out node
    in a single tick.
- **Missions**. Three predicate kinds (`circularize` /
  `orbit_insertion` / `soi_flyby`) with sticky pass/fail state,
  embedded starter catalog (1000 km LEO circularize, Luna orbit
  insertion, Mars SOI flyby). Reachable via the `[Missions]`
  title-bar button.
- **Persistence**. Save / load to JSON at
  `$XDG_STATE_HOME/terminal-space-program/save.json`. Schema
  v5 round-trips clock, focus, the entire craft slate (each
  craft's RCS pool, planted nodes with per-node throttle,
  in-flight burn, attitude, engine mode), and missions. Pre-v5
  saves auto-migrate (singular Craft → 1-entry slice).
- **Modding**. Custom systems via JSON overlay, per-body color
  via `theme.json` (see *Custom systems* and *Theming* below).
- **Multi-system viewing**. Sol, Alpha Centauri, TRAPPIST-1, and
  Kepler-452. The craft is locked to Sol today; switching
  systems just changes the camera.

## Custom systems

Drop additional system JSON files into
`$XDG_CONFIG_HOME/terminal-space-program/systems/` (or
`~/.config/terminal-space-program/systems/` if `XDG_CONFIG_HOME` is
unset) and they'll merge with the built-in catalog at startup. User
files win on `systemName` match — e.g. dropping a `sol.json` there
replaces the embedded Sol entirely. Otherwise they append. The
body-info screen (`i`) shows `source: embedded | user` so you can
tell which catalog a body came from.

Schema mirrors the embedded files in
`internal/bodies/systems/*.json`. Malformed user files print a
warning to stderr at startup and are skipped — embedded systems
always load. Save files carry a `body_catalog_hash` field, so a
save taken on the embedded catalog rejects on first load after you
add a custom system; that's by design (otherwise body references
across saves could drift silently).

## Theming

Drop a `theme.json` at
`$XDG_CONFIG_HOME/terminal-space-program/theme.json` (or
`~/.config/terminal-space-program/theme.json`) to recolor either the
UI tier or specific bodies:

```json
{
  "ui":     {"alert": "#ff5f5f", "warning": "#ffaf00"},
  "bodies": {"earth": "#3b82f6", "mars":    "#dc2626"}
}
```

Both blocks are optional. UI keys match the lower-cased name of the
package-level `Color*` var (e.g. `alert`, `warning`, `plannednode`,
`trajectory`, `currentorbit`, `craftmarker`, `foreignsoi`, `dim`).
Body keys match each body's `id` from `systems/*.json`. A body
override wins over that body's per-body `color` field; UI overrides
mutate the global tier colors at startup. Malformed `theme.json`
prints a warning to stderr and falls back to defaults.

## Version history

| Cycle | Theme | One-liner |
|---|---|---|
| v0.1 | Foundation | Heliocentric viewer + Verlet integrator + body catalog. |
| v0.2 | Burns | Spacecraft + impulsive burns + finite-burn integrator. |
| v0.3 | Transfers | Lambert solver, porkchop plot, auto-plant Hohmann transfers. |
| v0.4 | Persistence | Save / load with versioned envelope; mid-course refinement. |
| v0.5 | Moons + visuals | Body hierarchy + major moons (Luna, Phobos/Deimos, Galilean, Titan, Enceladus); per-body color, vessel trail, HUD polish. |
| v0.6 | Planner UX + missions | Burn-at-next scheduler, projected-orbit HUD, finite-burn-aware iteration, moon → parent escape, click-only mouse + 5-way views, mission scaffold, multiplayer design spike. |
| v0.7 | Modding + manual flight + textures | External system / theme overlays, manual-flight stick (throttle + attitude), inclination-change planner, retrograde Lambert flag, textured Earth/Moon/Mars/Jupiter, per-node throttle, SOI / frame-transition HUD. |
| v0.8 | Multi-craft polish | RCS / monopropellant precision thruster (v0.8.0). Multi-craft slate with per-craft burns + spawn keystroke + selector + save schema v4→v5 (v0.8.1). Craft types (4 loadouts with glyph/color visuals), full spawn form, clickable HUD nodes, Hohmann capture-preview, equatorial inclination match (v0.8.2). Docking + undocking, RENDEZVOUS HUD, alongside-spawn, engine-firing flame + per-thruster RCS puff visuals (v0.8.3). Atmospheric drag (Earth + Mars exponential atmospheres, drag-aware Verlet wired into live integrator + predictor, surface-clamp on impact, haze halo) (v0.8.4). Sim-time planet rotation, view-aware texture projection with per-body axial tilts, polygon-rasterised Earth grid, far-side / polar Moon detail, tilted Saturn rings with C / B / Cassini Division / A / F bands, textured Sun + Galileans + Uranus + Neptune, tidally-locked moons keeping their iconic face on the parent (v0.8.5). KSP-style F5/F9 quicksave/load + per-node delete/clear, body-equatorial Keplerian frame for body-bound orbits, adaptive warp clamps (throttle-change + upcoming-node predictive ramp-down), finite-burn iterate-for-target toggle in `m` form (v0.8.6). |
| v0.9 | The craft fleet grows up (in progress) | Unified `World.Target` slot (kind ∈ {None, Body, Craft}) replaces the implicit body cursor; `t` / `T` cycle / clear; TARGET HUD block; `H` / `I` planters consume the slot (v0.9.0). KSP-style player-managed staging chain: `space` decouples bottom stage; `Spacecraft.Stages` source-of-truth; Saturn-V 3-stage loadout; STAGES HUD; save schema v5 → v6 (v0.9.1). Ground-launch primitives: launchpad spawn at altitude 0, surface-frame SAS (`W` / `S`), pitch trim (`<` / `>` / `\`), LAUNCH HUD, `saturn-v-pad-to-leo` mission (v0.9.2). Rendezvous tooling: target-relative SAS modes, TCA / CA / DOCK READY, KSP-style NavMode cycle (`;`), `m`-form integration with `next closest approach` trigger event (v0.9.3). Ascent ergonomics: predictive `ap` / `pe` / `Δv→circ` in LAUNCH HUD, ORBIT READY callout, NavSurface auto-snap on launchpad spawn, `C` plants circularize-at-apoapsis (v0.9.4). |

Per-version detail: [`docs/state-of-game.md`](docs/state-of-game.md).
v0.5 release notes: [`docs/v0.5-release-notes.md`](docs/v0.5-release-notes.md).
v0.6 / v0.7 / v0.8 / v0.9 plans: [`docs/v0.6-plan.md`](docs/v0.6-plan.md), [`docs/v0.7-plan.md`](docs/v0.7-plan.md), [`docs/v0.8-plan.md`](docs/v0.8-plan.md), [`docs/v0.9-plan.md`](docs/v0.9-plan.md).

## Future plans

v0.9 — **the craft fleet grows up**. Slice breakdown in
[`docs/v0.9-plan.md`](docs/v0.9-plan.md):

- ~~v0.9.0 — unified `World.Target` slot (kind ∈ {None, Body, Craft}); `t` / `T` cycle / clear; TARGET HUD; `H` / `I` planters read it~~ **shipped.**
- ~~v0.9.1 — staging chain (KSP-style player-managed sequential decouples; `Spacecraft.Stages []Stage`; `space` keystroke; Saturn-V 3-stage loadout; save schema v5 → v6)~~ **shipped.**
- v0.9.2 — ground launch primitives (`SpawnSpec.Launchpad`, surface-co-rotating spawn at altitude 0, LAUNCH HUD, surface-frame SAS, pitch trim). **In flight on branch — work-in-progress.** Orbital insertion routinely undershoots without a gravity-turn assist; treat as an experimental loop pending an ergonomic pass.
- v0.9.3 — rendezvous tooling (target-relative SAS modes, live closest-approach countdown; manual loop is the success metric).
- v0.9.5 navball — KSP-style attitude indicator clamped into the bottom-right HUD; consumes v0.9.3 target-relative burn modes + v0.9.4 NavSurface frame routing.
- v0.9.6+ bandwidth-permitting — multi-rev porkchop UI + Lambert short/long picker, capture-direction toggle, predictor adaptive sampling, solar lighting + terminator + eclipses (research-first), polish bag.

Deferred to v0.10+: multiplayer implementation, interstellar transfer,
N-body perturbations, mission scripting / editor (eight-decision-point
design pass intact), atmospheric heating / structural overstress,
drag-to-edit nodes, theme-file hot-reload, race-detector CI.

Full backlog in [`docs/state-of-game.md`](docs/state-of-game.md).

## Implementation plan

Full design doc: [`docs/plan.md`](docs/plan.md). Summary:

- Phased physics progression (viewer → Verlet → impulsive burns → finite
  burns + RK4 → SOI-aware predictor + Lambert → auto-plant transfers).
- Bubble Tea root model with screen-level sub-models (orbit / bodyinfo /
  maneuver / help / missions / menu).
- GoReleaser single-workflow CI; release artifacts on tag push.

## Credits

Architectural foundation lifted (with MIT attribution) from
[furan917/go-solar-system](https://github.com/furan917/go-solar-system).
See [NOTICE.md](NOTICE.md) for the full acknowledgments list.

## License

MIT. See [LICENSE](LICENSE).
