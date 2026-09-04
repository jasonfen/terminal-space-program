# Controls & flight guide

How to fly the thing, then every key grouped by what it does. The in-game
`F1` overlay is the quick reference; this page is the same list with a little
more explanation. Multiplayer has its own guide in
[multiplayer.md](multiplayer.md).

**Terminal size:** the game is designed for a 140×40 terminal — that's the
size every screen is laid out and reviewed at. It runs down to a hard floor
of 104×24 (below that it refuses to render and shows a "terminal too small"
screen instead); between the floor and the design size, HUD chips shrink to
a compact form and, if a side of the screen still can't fit, collapse
behind a one-row `▸ +N hidden` marker rather than overlap each other.

**Contents**

- [Quick tour](#quick-tour): your first trip to the Moon
- [Surface launches](#surface-launches): flying a Saturn V off the pad
- [Keyboard layout](#keyboard-layout): QWERTZ support
- [Keybindings](#keybindings)
  - [Global](#global)
  - [Time](#time)
  - [Camera and views](#camera-and-views)
  - [Targets and the map cursor](#targets-and-the-map-cursor)
  - [Planning burns](#planning-burns)
  - [Vessels, staging, and docking](#vessels-staging-and-docking)
  - [Manual flight](#manual-flight)
  - [Multiplayer](#multiplayer)
  - [Mouse](#mouse)
- [Screens](#screens)
  - [Spawn form (`n`)](#spawn-form-n)
  - [Saves](#saves-esc--save-game--load-game)
  - [Maneuver planner (`m`)](#maneuver-planner-m)
  - [Porkchop plot (`P`)](#porkchop-plot-p)
  - [Meeting Planner (`K`)](#meeting-planner-k)
  - [Vehicle Assembly Building](#vehicle-assembly-building-esc--build-vab)

## Quick tour

You start as a vessel called **S-IVB-1** in a 500 km circular orbit around
Earth, moving prograde (the direction of travel). The left panel is the map:
the Sun (or whatever you've focused on) in the middle, planets on their real
orbits, and your vessel as a little chevron pointing the way it's moving. The
right-hand panel is your readout: the clock, what you're looking at, fuel and
attitude, any burns you've planned, and a preview of where they'll put you.

Speed time up or slow it down with `.` and `,` to watch the planets move;
pause with `0`.

To go somewhere, say the Moon:

1. Press `t` to pick a **target**. Keep tapping until the target readout shows
   the Moon. (`h` / `l` move a separate map cursor used by the body info
   screen and the porkchop plot; they don't set your travel target. The arrow
   keys **pan** the map.)
2. Press `H` to plan the trip. Because the Moon orbits the same planet you do,
   the planner works out two ways to get there, plans the cheaper one, and
   flashes both fuel costs, something like
   `combined 4.12 / split 3.95 km/s → planted split`. Two burn markers appear:
   one to leave your orbit and one to capture at the Moon, each showing its Δv
   (the speed change it costs) and a countdown.
3. Speed time up. From a fresh game the first burn is only a few hours out,
   because a new game starts you shortly before the Moon lines up with your
   orbital plane, the cheapest moment to leave. Deeper into a game, or after
   loading a save, it can sit further out, since it always waits for that next
   line-up. When the countdown hits zero the burn fires on its own; time warp
   eases off around a burn so nothing gets skipped. Your path stretches out
   toward the Moon, still curving under Earth's gravity.
4. The second burn fires near the Moon and drops you into a low orbit around
   it. (The porkchop plot, `P`, is for trips to other planets. For a moon of
   your own planet, `H` is the way.)

Prefer to set up a burn by hand? Press `m` for the planner. Choose a direction
(prograde, retrograde, normal, or radial), choose when it fires (a specific
time, or the next high/low point of your orbit, or the next time you cross the
target's orbital plane), set how much Δv you want, and pick a throttle. The
burn's length is worked out for you. A live preview shows the orbit you'll end
up with as you tweak the numbers. Press `Enter` to lock it in.

For hands-on, stick-and-throttle flying: `z` to throttle up, the
`w` `s` `a` `d` `q` `e` keys to point the vessel, then `b` to light the engine.

## Surface launches

You can also spawn a Saturn V on the launchpad and fly it to orbit by hand,
just like KSP: tip the rocket slightly east, let your speed build, switch the
autopilot to follow your velocity, and let gravity bend you over into a
gravity turn. The launch readout shows your projected high point, low point,
and the Δv still needed to circularise, so you can fly by watching the numbers.

A good first attempt:

1. Press `n` to open the spawn form. Set position to **launchpad**, pick a
   latitude (28.6° N is Cape Canaveral), choose **Saturn V**, and press
   `Enter`. The autopilot comes up pointing straight up and already set to
   follow your motion relative to the ground, so `w` does the right thing from
   the start.
2. `z` for full throttle, `b` to light the engine. The first stage lifts off
   at a thrust-to-weight ratio of about 1.24.
3. Around 3 km up, tap `>` a couple of times to tip ~5° east each. The rocket
   starts building sideways speed.
4. As your ground speed passes ~100 m/s, press `w` to have the autopilot point
   along your velocity, and `?` to clear the manual tip. From here the rocket
   tracks its own motion and gravity rounds the climb into orbit for you.
5. Press `space` to drop the empty first stage. You keep flying the upper
   stage and the stage list advances. Keep burning, drop the next stage, then
   the last one.
6. Watch the projected high point climb. When it passes 200 km the **● ORBIT
   READY** note appears. That's your cue to cut the throttle (`x`) and coast.
7. Press `C` to plan a circularising burn at the top of your arc.
8. Coast up to that point. The planned burn fires on its own and rounds out
   your orbit. Once your low point also clears 200 km you're safely in orbit,
   and the LAUNCH readout tracks the gap along the way.

The whole thing runs on numbers, not memorised pitch tables: the high point
climbs as you burn, ORBIT READY tells you when to stop, `C` sets up the last
burn, and the LAUNCH readout closes the gap. If a stage flames out early, the
readout shows you why before you watch it fall back.

## Keyboard layout

The keys below are written for **QWERTY**. If you play on a **QWERTZ** keyboard
(where the physical `Y` and `Z` keys are swapped), open `Esc → [Controls]` and
switch the layout to QWERTZ. Every binding then stays under the same finger as
on QWERTY, and the in-game help overlay (`F1`) relabels itself to match your
keycaps. The choice is saved to your config and applies to every game. AZERTY,
Dvorak, and free per-key remapping aren't supported yet.

## Keybindings

### Global

| Key | Action |
|---|---|
| `Esc` | Back; on the main view, open the save / load / build / settings / controls / quit menu |
| `Ctrl+C` | Quit immediately |
| `F1` or `?` | Toggle the help overlay (scroll with `↑`/`↓`, `PgUp`/`PgDn`, `Home`/`End`). `?` opens the same overlay as `F1` everywhere `F1` does |
| `F2` | Declutter: hide the corner chips and the navball for a clean look at the orbit. Press again to restore. The telemetry column stays |
| `` ` `` | **Boss key**: instantly swap the screen for a convincing fake developer shell. Type `exit`, `logout`, or `Ctrl+D` to come back where you left off. Left out of the `F1` overlay on purpose |
| `Tab` | Switch star system (Sol first, then alphabetical: Alpha Centauri, Kepler-452, Lumen, TRAPPIST-1). A camera toggle only; vessels stay in the system they spawned in |
| `i` | Body info screen for the body under the map cursor |
| `M` | Mission ladder: program and objective progress, the active mission's checklist, and locked rungs with what unlocks them. Same as the `[Missions]` button. Flight School (the tutorial) is **on unless switched off**; the Challenge ladder stays opt-in — enable it in `[Menu]` → Settings |
| `O` | Session roster (multiplayer). See [Multiplayer](#multiplayer) |

### Time

| Key | Action |
|---|---|
| `0` | Pause / resume |
| `.` / `,` | Speed time up / down, to 100,000×. Eases off automatically around a burn |
| `G` | Auto-warp to the next burn (any vessel's): warps to 30 s before it fires, ramps down, and hands you 1× to watch it arm. `.` / `,` or the `[»Burn]` button cancels it |
| `/` | Cancel warp: straight back to 1× from any level, and cancel auto-warp or a rendezvous warp if one is running |

During a multiplayer rendezvous warp the manual warp keys and `G` are inert
and `/` is the only cancel; in the terminal phase `,` and `.` become the
copilot's brake. See [multiplayer.md](multiplayer.md#rendezvous-warp).

### Camera and views

| Key | Action |
|---|---|
| `↑` `↓` `←` `→` | Pan the map. The camera keeps following your focus, just displaced; `g` or any refocus snaps it back |
| `+` / `-` | Zoom in / out. Zoom is remembered per focus |
| `f` / `F` | Cycle what the camera follows, forward / back (whole system → each body → your vessel). Focusing a body your trajectory passes fits the camera to its sphere of influence so the capture curve fills the canvas. Also the way back from spectating another player |
| `g` | Reset the camera to the whole system (clears any pan) |
| `v` | Cycle the projection: tilted (default, a 3D-style perspective) → top → right → bottom → left → flat. The camera re-frames once when focus, view, or system changes and otherwise stays where you put it |
| `shift+↑` / `shift+↓` | Tilt the 3D view up / down (tilted view only) |
| `shift+←` / `shift+→` | Yaw the 3D view left / right in 5° steps, wrapping (tilted view only) |
| `o` | **Proximity view** for the last kilometres of a rendezvous: the target vessel sits dead centre, its direction of travel runs right, the planet is below, so you read your drift the way the physics works. Needs a vessel target; a `CLOSE RANGE` chip reminds you once within 35 km. Press again to return to the map as you left it |
| `V` | **Launch / surface view**: chase-cam on your active vessel with a curved horizon, pad marker, and breadcrumb trail, scaled tight at liftoff and pulled back high up. Lifting off routes you here automatically; a `DESCENDING` chip reminds you when you're headed for the ground. Press again to return to the map |
| `j` | **Inspect**: each press steps a bright highlight onto the next thing drawn (bodies, vessels, other players' ghosts, planned burns, the closest-approach `✕`) and names it in a chip. One press past the last item clears it; `Esc` clears immediately. Clicking any orbit line or marker jumps the highlight there |
| `Enter` | While inspecting: make the highlighted thing your target and clear the highlight. Works on vessels and other players, not just bodies. Things that can't be a target say so |

### Targets and the map cursor

Two different things point at bodies. The **target** (`t` / `T`) is what you
plan trips to and what the target readout describes. The **map cursor**
(`h` / `l`) feeds the body info screen (`i`) and the porkchop plot (`P`) and
does not affect your travel target.

| Key | Action |
|---|---|
| `t` / `T` | Pick / clear your target. `t` cycles nearest-first: the moons of whatever you're currently orbiting, then other vessels, then the rest of the system's bodies outward (each followed by its own moons), then none. Your own primary is never offered — from LEO the first press is the Moon; from lunar orbit it's Earth. `T` clears (no reverse cycle) |
| `l` / `h` | Move the map cursor to the next / previous body |

### Planning burns

| Key | Action |
|---|---|
| `m` | Open the [maneuver planner](#maneuver-planner-m) |
| `H` | Plan a transfer to your target. To a moon of your current planet it works out two routes and plans the cheaper, showing both costs. To another planet it plans a Hohmann transfer. To a moon's parent planet it plans an escape |
| `I` | Plan a burn to match your target's orbital tilt (or to level out to the equator with no target) |
| `C` | Plan a circularising burn at the top of your orbit; pairs with the ORBIT READY cue on launch. Refused if the top of your orbit is inside the atmosphere or you're on an escape trajectory |
| `K` | Close in on a target vessel. Close and near-matched → plants a small nudge directly, using the closest-approach numbers in the target readout. Too far apart in phase → opens the [Meeting Planner](#meeting-planner-k) instead of refusing. Your planes differ → names `I` instead of planting anything. Needs a vessel target sharing your planet |
| `P` | [Porkchop plot](#porkchop-plot-p) for the body under the map cursor (not your `t` target). Other planets only; moon targets point you back to `H` |
| `R` | Refine the plan: recompute the transfer from where you are now and update the arrival |

### Vessels, staging, and docking

| Key | Action |
|---|---|
| `n` | Open the [spawn form](#spawn-form-n) |
| `[` / `]` | Switch which vessel you're flying |
| `1`–`9` | Jump straight to vessel N (nothing happens if the slot is empty) |
| `space` | Drop the bottom stage (only if there's more than one). On a bare capsule with a parachute this arms the chute instead; it opens on its own in the atmosphere |
| `Y` | Deploy the top carried payload as its own vessel (satellite, probe, station) while you keep flying the carrier. Press again for the next one |
| `D` | Apollo transposition: flip the Service Module to the front to do the flying, leaving the Lunar Module as a nose payload (then `U` to release it) |
| `U` | Undock a docked vessel into its parts and switch you to a released piece. On a cross-player stack it releases your partner's vessel through the dock ledger, connected or not (an absent partner's vessel is parked as a **Parcel** and delivered, safed, when they next connect) |
| `c` | Re-arm docking. After an undock a pair is held apart until you clear 100 m or ten minutes pass, even if you drift back inside docking range. `c` clears that hold now: with a vessel targeted, just that pair; otherwise every hold on your active vessel. A pair already close and slow docks on the next tick |
| `J` | Transfer control of a cross-player docked stack to the guest riding in it. Multiplayer only; see [multiplayer.md](multiplayer.md#docking-across-players) |
| `E` | End the flight: remove a crashed vessel after a `y` / `n` confirm |
| `F5` / `F9` | Quicksave / quickload to a fixed quick slot, separate from named saves. See [Saves](#saves-esc--save-game--load-game) |

### Manual flight

| Key | Action |
|---|---|
| `z` / `x` | Throttle to full / cut to zero |
| `Z` / `X` | Throttle up / down 10% |
| `b` | Light / cut the main engine (needs throttle above zero) |
| `w` / `s` | Point prograde / retrograde (with / against your motion) |
| `a` / `d` | Point normal+ / normal- (perpendicular to your orbit) |
| `q` / `e` | Point radial+ / radial- (away from / toward the body) |
| `W` / `S` | Point along / against your ground speed (velocity relative to the spinning atmosphere). Use this for the launch gravity turn |
| `>` / `<` | Tip the nose 5° east / west on top of whatever the autopilot is doing (hold to ramp) |
| `\|` | Clear the manual tip (pitch trim back to 0) |
| `;` | Autopilot reference: Orbit → Surface → Target (skips Target when none is set) |
| `k` | Steering style: smooth turning (default) or instant snap |
| `r` | Switch between the main engine and RCS thrusters |
| `p` | RCS pulse step: cycle the per-press Δv (0.1 → 0.01 → 0.001 m/s) |

The pointing keys only aim the vessel; `b` is what fires the engine. In RCS
mode those same keys also fire one small thruster pulse per press (hold for a
steady stream). Each pulse is 0.1 m/s by default; `p` steps it down for fine
work like nulling an orbital period to the second (see the
[constellation deployment guide](constellation-deploy.md)). The readouts show
which engine is armed, the pulse size, RCS fuel, and how much Δv it's worth.

### Multiplayer

Full mechanics (hosting, independent time, rendezvous warp, cross-player
docking, chat) are in [multiplayer.md](multiplayer.md). The keys:

| Key | Where | Action |
|---|---|---|
| `O` | flight view | Open the **session roster**: one row per player with online state, last-known system, vessel count, and time offset vs you |
| `~` | flight view | **Chat**. `enter` broadcasts, `esc` bails; a leading `@handle` sends a private line, `tab` completes the handle, and a typo refuses to send. Messages show as chips for ~30 s; nothing is stored |
| `J` | flight view | Transfer control of a docked stack to the guest riding in it |
| `t` | roster | Target a player's vessel (expands to a picker when they have several in your system; `esc` backs out) |
| `v` | roster | Spectate: fit the camera to their ghost orbit and follow it. `f` returns to your own vessel |
| `s` | roster | Sync-warp forward to a player's time (forward only; a player behind you syncs to you) |
| `w` | roster | Agree to a rendezvous warp with them — commits to an encounter when one's found (a planted node's own arrival, or the current course inside 4 h), otherwise arms with no plan yet; they answer `y` on their main screen |
| `y` | flight view | Accept a rendezvous warp |
| `/` | flight view | Cancel a rendezvous warp (either side) |
| `h` | roster | Host: start hosting from single-player in place, or stop hosting (drops guests, keeps progress) |
| `i` / `r` | roster | Host or admin: mint / revoke an invite code |
| `x` | roster | Host or admin: remove the selected player |
| `p` | roster | Host only: promote the selected player to admin, or demote them |
| `F4` | roster | Host or admin: restart the server. Everyone is warned and drained with progress saved; on a self-updating host it adopts a newer release |

Flight controls are inactive on the roster; only the keys listed apply.

### Mouse

Click only; no dragging, no scroll-to-zoom.

| Click | Action |
|---|---|
| `[»Burn]` (top-right) | Toggle auto-warp to the next burn (same as `G`). Shows `[■Burn]` while running, dimmed with no burn planned |
| `[Menu]` (top-right) | Save / load / settings / controls / quit menu |
| `[Missions]` (top-right) | Mission ladder (same as `M`). Off by default; enable a program in `[Menu]` → Settings |
| A body | Follow it with the camera, and inspect it |
| A vessel | Follow it with the camera, and inspect it |
| A planned burn | Open the planner for that burn (fire time kept), and inspect it |
| An orbit line | Inspect it: the line flares and a chip names whose it is (`Enter` targets, `Esc` clears). Your **own** orbit line instead plants a burn there |
| Empty space | Open the planner with a new burn at the nearest point on your orbit |
| A readout panel | Open body info |
| A porkchop cell | Move the cursor there (then `Enter` to plan it) |

## Screens

### Spawn form (`n`)

Choose a vessel, where to start (orbit or launchpad), which body, altitude,
and direction.

- **Vessels** are grouped by category and filtered to the current system's
  scale class by default; `f` inside the form toggles the filter to show every
  system's vessels. Pick **Custom…** to build a quick stack from whole
  modules, or one of your **saved designs** (listed after Custom…) from the
  [VAB](#vehicle-assembly-building-esc--build-vab).
- **Altitude** is typed, not picked from a list. `Enter` opens the field for
  digits, `Enter` again keeps the number, `Esc` discards what you typed
  without closing the form, and only once you're back out does `Enter` launch.
- Outside the box, `←` / `→` step through altitudes derived from the body
  itself: a floor a safe margin above its atmosphere (or 25 km with none), a
  couple of round waypoints, geostationary where the body has one, and a
  ceiling before you'd lose its gravity's grip. They hold at either end
  instead of wrapping. Type past either end and the number is moved back in
  range with a line saying by how much and why.
- A body too small to hold any orbit (Phobos, Deimos) still lists as a place
  to land, not to orbit: `Enter` is dead there while POSITION is `orbit`.

### Saves (`Esc → [Save Game]` / `[Load Game]`)

One browser for every save: your named saves plus the managed quicksave
(`F5`) and the three rotating autosaves, newest first, with when it was
saved, the in-game date, and the vessel you were flying. `[Save Game]` opens
it in save mode (with a `＋ New save…` row), `[Load Game]` in load mode.

| Key | Action |
|---|---|
| `↑` / `↓` | Move the save cursor (click a row to select it; click again to activate) |
| `Enter` | Load mode: load the save (asks first). Save mode: `＋ New save…` prompts for a name (prefilled with vessel + in-game day); an existing named save asks to overwrite |
| `d` | Delete the highlighted save (asks first; works on quicksave and autosaves too) |
| `r` | Rename a named save. Quicksave and autosave slots are managed and can't be renamed or overwritten by hand |
| `Esc` | Back |

### Maneuver planner (`m`)

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Move between fields (direction → when → Δv → throttle → refine) |
| `←` / `→` | Change the field you're on (direction / when / refine) |
| `↑` / `↓` | Move the **Plan Cursor** through PLANNED NODES (independent of `Tab`) |
| `Space` | Toggle the refine option when you're on that field |
| digits / backspace | Type a Δv or throttle value |
| `Enter` | Cursor on a node you haven't loaded yet: load it for editing. Otherwise: lock in the burn |
| `Esc` | Cancel and go back |
| `Ctrl+D` | Delete the node under the Plan Cursor |
| `Ctrl+K` | Clear every planned burn for this vessel |
| `c` | Refuses — clear-all is `Ctrl+K` now, not this key |
| `H` `I` `C` `K` `P` `R` | Run that one-key planner (see [Planning burns](#planning-burns)) right here, then return to the map. Only fires when a text field (Δv / throttle) doesn't have focus |

The **Plan Cursor** is the highlighted row in PLANNED NODES: `↑`/`↓` move it,
the header names which node it's on ("BURN PLAN — node 2 of 3"), and
PROJECTED ORBIT always shows the orbit *after that node* — never a leftover
draft from an earlier edit. The list ends in a blank **+ new node** row; with
the cursor there, PROJECTED ORBIT shows the form's own draft instead, labelled
as such. A node whose Δv is more than the vessel can currently afford still
plants — you may be about to refuel, stage, or dock a tug — but its row (and
the map's NODES chip) carries a `⚠ exceeds budget by …` marker so it never
comes as a surprise.

Below PLANNED NODES, **QUICK PLANS** lists the same six one-key planners as
the map; whichever ones aren't legal right now (no target, no vessel target,
no planted transfer, …) are dimmed with the reason instead of hidden.

- The Δv you enter sets the burn's length automatically; vessels with no
  engine fall back to an instant nudge.
- **When** fires at a set time or at the next high point, low point, or
  orbital-plane crossing.
- **Throttle** is saved per burn, so changing your live throttle while
  coasting won't slow a burn you've already planned.
- **Refine** spends a little extra Δv to make up for the fuel lost steering
  and fighting gravity during a long burn, so you end up where an instant burn
  of the same size would have put you. Leave it off for short burns; turn it
  on for low-thrust vessels or big burns.
- The preview updates the resulting orbit as you edit.
- The Δv budget line shows what the whole plan leaves you, not just what the
  vessel has: `Δv budget: 6129 m/s (2217 after plan)`.

### Porkchop plot (`P`)

| Key | Action |
|---|---|
| `←` / `→` | Move the departure-day cursor |
| `↑` / `↓` | Move the travel-time cursor |
| `Enter` | Plan the transfer for the selected cell |
| `o` | Transfer options: number of laps, prograde/retrograde, short/long path. Closing re-draws the grid |
| Click a cell | Move the cursor there (then `Enter` to plan) |
| `Esc` | Back to the map |

The cursor opens on the cheapest cell. A `·` marks cells where no transfer was
found; `Enter` does nothing there.

### Meeting Planner (`K`)

A chip on the map, not a separate screen — it opens when `K`'s small nudge
isn't enough to close on your target (too far apart in phase) but your
planes already match. Pick where to meet: on their orbit, on yours, or at
the natural crossing of your current courses; then pick a lap count on the
**Lap Ladder** for that Meeting Place — more laps, less Δv, longer wait.
Unaffordable or unsafe rows still show, dimmed, with the reason, rather than
being hidden.

| Key | Action |
|---|---|
| `←` / `→` | Walk the Meeting Place: their orbit / your orbit / the crossing |
| `↑` / `↓` | Walk the Lap Ladder |
| `Enter` | Plant the highlighted row's burn |
| `Esc` | Close without planting |

"Meet on your orbit" computes a plan for your *target*, not you — since this
session has no way to plant a node on someone else's vessel, planting there
is a later slice; the row still shows what that burn would cost.

### Vehicle Assembly Building (`Esc → [Build (VAB)]`)

Design a custom vehicle from fine parts and save it to launch later. You
compose **components** (engines, fuel tanks, command cores, antennas,
structure) into stages, stack the stages, and read a live **Δv / TWR / mass**
panel as you go. Saved designs are global across games and show up in the
spawn form (`n`) alongside the built-in vessels.

Editing happens **in place** on the vehicle rows, the same idiom as the
maneuver form. The VAB opens focused on the vehicle column with the cursor on
a fresh stage's `engine` placeholder, and `←` / `→` cycles that row's part.
You rarely need the palette: `←` / `→` to pick an engine, `↓` and `←` / `→`
for a tank, `+` / `-` for the count, done.

| Key | Action |
|---|---|
| `Tab` | Switch the active column (palette ↔ vehicle) |
| `↑` / `↓` | Move the cursor in the active column |
| `PgUp` / `PgDn` | Previous / next kind section in the palette, or previous / next stage in the vehicle column |
| `←` / `→` | Swap the selected row's part within its kind (engines on an engine row, tanks on a tank row). On an empty stage the placeholder rows cycle from nothing through the catalog. No-op on a stage header or in the palette |
| `a` | Add the selected component to the current stage, or add a catalog part as a new whole stage |
| `n` | Start a new empty stage on top |
| `x` | Remove the component group under the cursor, or the whole stage when on a stage header or the stage is empty / a catalog part |
| `+` / `-` | Increase / decrease the count of the component group under the cursor (`×N`) |
| `[` / `]` | Move the cursor's stage down / up in the stack |
| `y` | Duplicate the stage under the cursor |
| `Enter` | **Crack open** the catalog part under the cursor (a stage header) into editable components, so you can start from an S-IVB or a Falcon booster and tweak it. A flash shows the Δv change; parts with no decomposition stay whole |
| `d` | Toggle a **dock seam** below the stage. Everything above the seam becomes a nose payload released with `U` instead of staging; mark several seams for several payloads |
| `c` | Toggle a **fused decouple**: the stage drops together with the group below it on one staging press |
| `t` | Set a session **Σ Δv target**. The stats strip reads `current / target (delta)`, and with a tank row selected a hint shows how many of that tank close the gap (`+2 → Σ ≈ 9280 ✓`) or that it's unreachable |
| `s` | Name and save the design |
| `o` | Open a saved design to edit (`x` deletes the highlighted one) |
| `Esc` | Back to the map |

Rules the VAB enforces:

- A stage holds **one fuel chemistry**. The engine row leads: cycling the
  engine with `←` / `→` can cross chemistries (the stage is briefly flagged),
  and the tank rows then cycle only the new chemistry, so one `←` / `→` per
  tank repairs the mix. Adding a mismatched part with `a` is rejected outright.
- Multiple engines in one stage combine honestly: thrust adds, effective Isp
  is the thrust-weighted blend.
- Soft warnings flag the usual snags (no engine, no command source, lift-off
  TWR below 1) but never block saving. A design with no command source gets a
  probe core on spawn.

Designs are stored as portable files under
`$XDG_CONFIG_HOME/terminal-space-program/designs/` (typically
`~/.config/terminal-space-program/designs/`); copy one into the sibling
`loadouts/` overlay dir to share it as a mod.
