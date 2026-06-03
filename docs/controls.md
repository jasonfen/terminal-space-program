# Controls & flight guide

A walkthrough of how to actually fly the thing, followed by the full
keybinding reference. The in-game `?` overlay is the source of truth for
keybindings; the tables below mirror it.

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

1. Press `t` to cycle the **target** (`World.Target`) onto a body — keep
   tapping until the TARGET HUD block reads Mars. (The `←`/`→` arrow keys
   move a separate *selection cursor* used by the body-info screen and the
   porkchop plot — they do **not** set the transfer target.)
2. Press `H` to plant a Hohmann transfer to the target — two finite-burn
   nodes appear (geocentric departure + Mars-frame arrival), each
   color-coded with its predicted post-burn orbit on the canvas,
   listed in the HUD with Δv and time-to-fire.
3. Time-warp forward. The departure node fires at its trigger time;
   warp clamps to ≤10× during the burn so the integrator keeps
   temporal resolution. Your trajectory unrolls past Earth's SOI,
   the predictor switches frames, and the curve bends sunward as
   it should.
4. The arrival node fires near Mars and drops you into a low
   capture orbit. For phasing-aware launch windows, use `P`
   (porkchop) instead of `H` — porkchop targets the body under the
   `←`/`→` selection cursor.

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
| `→` / `l` | Selection cursor: next body. Moves the on-canvas selected-body cross that drives the body-info screen (`i`), the porkchop plot (`P`), and the SELECTED HUD pane. **Not** the transfer target — that's `t` / `T` (since v0.9.0) |
| `←` / `h` | Selection cursor: previous body (see above) |
| `+` / `-` | Zoom in / out |
| `f` / `F` | Cycle camera focus forward / backward (system → bodies → craft) |
| `g` | Reset camera focus to system |
| `v` | Cycle view (tilted → top → right → bottom → left → orbit-flat). `tilted` (v0.10.6+) is the new default — perspective tilt over the active craft's perifocal basis, with far-side orbit arcs rendered as same-hue stipple for depth read |
| `shift+↑` / `shift+↓` | While in `ViewTilted`, nudge the polar tilt θ ±5° (clamped 0–60°). HUD shows `view: tilted Nº` when off the 25° default. No-op in cardinal / orbit-flat modes (v0.10.6+) |
| `n` | Open spawn form (loadout / position / parent body / altitude / direction). Pick **Custom…** for the v0.10.1+ stack builder: on the STACK field, `←/→` picks a catalog part, `a` adds it on top, `x` removes the top stage |
| `H` | Auto-plant transfer to `World.Target` body. Intra-primary (moons of the craft's parent) is a **plane-aware dual strategy** (v0.12.x, ADR 0005): it computes both a *combined* fused-Lambert departure (eccentricity + raise + plane change folded into one `BurnVector` burn) and a *split* (coplanar raise + a plane change at the slow transfer apoapsis, where it's cheapest), plants the cheaper, and flashes both Δv totals. Large departure inclinations (an equatorial LEO sits ~25° off Luna's plane) favour the split; near-coplanar favours the combined. Heliocentric stays patched-conic Hohmann; moon → parent is a bound-escape ellipse. TargetCraft flashes "needs v0.9.3" |
| `I` | Plant inclination match — rotates the orbital plane to `World.Target` body's inclination (or 0° equatorial when target is None). TargetCraft flashes "needs v0.9.3" |
| `C` | Plant circularize burn at next apoapsis (v0.9.4+) — pairs with the LAUNCH HUD's ORBIT READY callout. Errors when apoapsis is below the primary's atmosphere cutoff or the orbit is hyperbolic |
| `K` | Plant rendezvous nudge to target craft (v0.10.2+) — single-burn Lambert intercept projected onto the closest velocity-frame axis. Reads the TARGET HUD's ACH CA / Δv readouts. Errors when there's no craft target, target shares a different primary, already DOCK READY, or no improvement available |
| `t` / `T` | Cycle / clear `World.Target` (non-active sibling craft → bodies in active system → none) |
| `space` | Decouple bottom stage of active craft (multi-stage only; single-stage status-flashes "cannot drop the only remaining stage") (v0.9.1+). On a bare chute-bearing capsule the same press **arms the parachute** instead — it auto-deploys once dynamic pressure builds in the atmosphere (v0.12+, ADR 0008) |
| `P` | Porkchop plot for the body under the `←` / `→` selection cursor (not the `t` target); `Enter` on a cell plants that Lambert transfer. Inter-primary only — moon targets show a banner redirecting to `H`. Press `o` inside to open the transfer-options sub-menu (`n` cycles nRev 0–3, `r` toggles prograde/retrograde, `b` toggles short/long branch); `enter`/`o`/`esc` closes and re-solves (v0.10.5+) |
| `R` | Refine plan — re-Lambert from live state, plant mid-course correction + update arrival |
| `m` | Open maneuver planner |
| `F5` / `F9` | Quicksave / quickload (`~/.local/state/terminal-space-program/save.json`, or `$XDG_STATE_HOME/...` if that var is set) — KSP-style |
| `[` / `]` | Cycle active craft (no-op when only one craft loaded) |
| `1`–`9` | Jump directly to craft N (no-op when that slot is empty) |
| `U` | Undock active composite |
| `D` | Transpose (Apollo): flip the SM to the firing core, the LM becomes a releasable nose payload (then `U` to release) |
| `V` | Jump to launch chase-cam (`ViewLaunch`) focused on the active vessel (v0.11.4+, ADR 0004). Skips the lowercase `v` view cycle |
| `E` | End flight — remove a **Crashed** active vessel from the slate after a `y`/`n` confirm (v0.11.4+, ADR 0004). No-op unless the active vessel is Crashed |

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
| `;` | NavMode cycle: Orbit → Surface → Target (skips Target when no craft target is set) (v0.9.3+) |
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
