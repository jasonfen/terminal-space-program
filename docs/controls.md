# Controls & flight guide

How to actually fly the thing, then the full list of keys. The in-game `F1`
overlay is the quick reference; the tables below are the same thing with a
little more explanation.

## Quick tour

You start as a craft called **S-IVB-1** in a 500 km circular orbit around
Earth, moving prograde (the direction of travel). The left panel is the map —
the Sun (or whatever you've focused on) in the middle, planets on their real
orbits, and your craft as a little chevron pointing the way it's moving. The
right-hand panel is your readout: the clock, what you're looking at, fuel and
attitude, any burns you've planned, and a preview of where they'll put you.

Speed time up or slow it down with `.` and `,` to watch the planets move;
pause with `0`.

To go somewhere — say, the Moon:

1. Press `t` to pick a **target**. Keep tapping until the target readout shows
   the Moon. (The `←` / `→` arrow keys move a separate map cursor used by the
   body info screen and the porkchop plot — they don't set your travel target.)
2. Press `H` to plan the trip. Because the Moon orbits the same planet you do,
   the planner works out two different ways to get there, plans the cheaper one,
   and flashes both fuel costs — something like
   `combined 4.12 / split 3.95 km/s → planted split`. Two burn markers appear:
   one to leave your orbit and one to capture at the Moon, each showing its Δv
   (the speed change it costs) and a countdown.
3. Speed time up. From a fresh game the first burn is only a few hours out — a
   new game starts you shortly before the Moon lines up with your orbital plane,
   which is the cheapest moment to leave. (Deeper into a game, or after loading a
   save, it can sit further out, since it always waits for that next line-up.)
   When the countdown hits zero the burn fires on its own (time warp eases off
   around a burn so nothing gets skipped). Your path stretches out toward the
   Moon, still curving under Earth's gravity.
4. The second burn fires near the Moon and drops you into a low orbit around it.
   (The porkchop plot, `P`, is for trips to other planets — for a moon of your
   own planet, `H` is the way.)

Prefer to set up a burn by hand? Press `m` for the planner. Choose a direction
(prograde, retrograde, normal, or radial), choose when it fires (a specific
time, or the next high/low point of your orbit, or the next time you cross the
target's orbital plane), set how much Δv you want, and pick a throttle. The
burn's length is worked out for you. A live preview shows the orbit you'll end
up with as you tweak the numbers. Press Enter to lock it in.

For hands-on, stick-and-throttle flying: `z` to throttle up, the
`w` `s` `a` `d` `q` `e` keys to point the craft, then `b` to light the engine.

### Surface launches

You can also spawn a Saturn V on the launchpad and fly it to orbit by hand,
just like KSP: tip the rocket slightly east, let your speed build, switch the
autopilot to follow your velocity, and let gravity bend you over into a
gravity turn. The launch readout shows your projected high point, low point,
and the Δv still needed to circularise, so you can fly by watching the numbers.

A good first attempt:

1. Press `n` to open the spawn form. Set position to **launchpad**, pick a
   latitude (28.6° N is Cape Canaveral), choose **Saturn V**, and press Enter.
   The autopilot comes up pointing straight up and is already set to follow
   your motion relative to the ground, so `w` does the right thing from the
   start.
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
   READY** note appears — that's your cue to cut the throttle (`x`) and coast.
7. Press `C` to plan a circularising burn at the top of your arc.
8. Coast up to that point. The planned burn fires on its own and rounds out
   your orbit — once your low point also clears 200 km you're safely in orbit,
   and the LAUNCH readout tracks the gap along the way.

The whole thing runs on numbers, not memorised pitch tables: the high point
climbs as you burn, ORBIT READY tells you when to stop, `C` sets up the last
burn, and the LAUNCH readout closes the gap. If a stage flames out early, the
readout shows you why before you watch it fall back.

## Keybindings

### Global

| Key | Action |
|---|---|
| `Esc` | Back, or open the save / load / build / settings / controls / quit menu on the main view |
| `Ctrl+C` | Quit immediately |
| `F1` | Toggle the help overlay (scroll it with `↑`/`↓`, `PgUp`/`PgDn`, `Home`/`End`) |
| `` ` `` | **Boss key** — instantly swap the whole screen for a convincing fake developer shell (works from any screen). Type `exit`, `logout`, or `Ctrl+D` to drop back into the game right where you left off. Deliberately left out of the in-game help overlay so it stays discreet |
| `i` | Body info screen |
| `M` | Mission ladder — your program / objective progress, with the active mission's checklist and locked rungs shown with what unlocks them (same screen as the `[Missions]` button). Missions are **off by default** — enable the Tutorial and/or Challenge ladder in `[Menu]` → Settings |
| `Tab` | Switch star system (Sol → Alpha Centauri → TRAPPIST-1 → Kepler-452) |
| `0` | Pause / resume |
| `.` / `,` | Speed time up / down (up to 100,000×; eases off around a burn) |
| `G` | Auto-warp to the next burn — speeds time to 30 seconds before whichever burn fires first (any craft's), ramps back down, and hands you 1× to watch it arm. Tapping `.` / `,` cancels it back to your own warp; click the `[»Burn]` button to do the same |
| `/` | Cancel warp — drop straight back to 1× from any warp level, and cancel auto-warp if it's running |

### Keyboard layout

The keys below are written for **QWERTY**. If you play on a **QWERTZ** keyboard
(where the physical `Y` and `Z` keys are swapped), open `Esc → [Controls]` and
switch the layout to QWERTZ. Every binding then stays under the same finger as
on QWERTY, and the in-game help overlay (`F1`) relabels itself to match your
keycaps. The choice is saved to your config and applies to every game. (AZERTY,
Dvorak, and free per-key remapping aren't supported yet.)

### Map view

| Key | Action |
|---|---|
| `→` / `l` | Move the map cursor to the next body. This cursor feeds the body info screen (`i`) and the porkchop plot (`P`) — it does **not** set your travel target. For that, use `t` / `T` |
| `←` / `h` | Move the map cursor to the previous body |
| `+` / `-` | Zoom in / out |
| `f` / `F` | Cycle what the camera follows, forward / back (whole system → each body → your craft) |
| `g` | Reset the camera to the whole system |
| `v` | Cycle the view (tilted → top → right → bottom → left → flat → launch). Tilted is the default — a 3D-style perspective that shows orbits leaning in space. Views are projections only: the camera re-frames once when you change focus, view, or system, and otherwise stays exactly where you put it — to read an upcoming encounter, focus the body it passes (`f`) and the camera fits to its sphere of influence so the capture curve fills the canvas |
| `shift+↑` / `shift+↓` | Tilt the 3D view up / down (only in the tilted view) |
| `shift+←` / `shift+→` | Yaw the 3D view left / right in 5° steps, wrapping all the way around (only in the tilted view) |
| `F2` | Declutter — hide all overlays (the corner chips and the navball) for a clean look at the orbit. Press again to bring them back. Your core telemetry column stays put |
| `n` | Open the spawn form (craft, where to start, which body, altitude, direction). Pick **Custom…** to build a quick stack from whole modules, or pick one of your **saved designs** (listed after Custom…) built in the VAB (`Esc → [Build (VAB)]`). Craft are grouped by category and filtered to the current system's scale class by default — use `[f]` to see all systems' craft |
| `f` | Inside the spawn form: toggle the **scale-class system filter** — shows all systems' craft when off, hides off-scale craft when on (on by default) |
| `H` | Plan a transfer to your target. To a moon of the planet you're at, it works out two ways to get there and plans the cheaper one, showing you both fuel costs. To another planet, it plans a standard Hohmann transfer. To a moon's parent planet, it plans an escape |
| `I` | Plan a burn to match your target's orbital tilt (or to level out to the equator when nothing is targeted) |
| `C` | Plan a circularising burn at the top of your orbit — pairs with the ORBIT READY cue on launch. Won't work if the top of your orbit is still inside the atmosphere or you're on an escape trajectory |
| `K` | Plan a small nudge to close in on a target craft. Reads the closest-approach numbers in the target readout. Needs a craft target sharing your planet, and only works when there's an improvement to be had |
| `t` / `T` | Pick / clear your target (other craft nearby → bodies in the system → none) |
| `space` | Drop the bottom stage of your craft (only if it has more than one). On a bare capsule with a parachute, this arms the chute instead — it opens on its own once you hit the atmosphere |
| `P` | Porkchop plot for the body under the map cursor (not your `t` target). `Enter` on a cell plans that transfer. For another planet only — moon targets point you back to `H`. Press `o` inside for transfer options |
| `R` | Refine the plan — recompute the transfer from where you are right now and update the arrival |
| `m` | Open the maneuver planner |
| `F5` / `F9` | Quicksave / quickload |
| `[` / `]` | Switch which craft you're flying (when you have more than one) |
| `1`–`9` | Jump straight to craft N (does nothing if that slot is empty) |
| `U` | Undock a docked craft back into its parts |
| `Y` | Deploy the top carried payload as its own craft — releases a satellite/probe/station while you keep flying the carrier (press again to drop the next one). Undock (`U`) instead splits everything and switches you to a released piece |
| `D` | Apollo transposition — flip the Service Module to the front to do the flying, leaving the Lunar Module as a nose payload (then `U` to release it) |
| `V` | Jump to the launch chase-cam, following your active craft |
| `E` | End the flight — remove a crashed craft after a `y` / `n` confirm |

### Manual flight

| Key | Action |
|---|---|
| `z` / `x` | Throttle to full / cut to zero |
| `Z` / `X` | Throttle up / down 10% |
| `w` / `s` | Point prograde / retrograde (with / against your motion) |
| `a` / `d` | Point normal+ / normal- (perpendicular to your orbit) |
| `q` / `e` | Point radial+ / radial- (away from / toward the body) |
| `b` | Light / cut the main engine (needs throttle above zero) |
| `r` | Switch between the main engine and RCS thrusters |
| `k` | Steering style: smooth turning (the default) or instant snap |
| `;` | Switch the autopilot's reference: Orbit → Surface → Target (skips Target when none is set) |
| `W` / `S` | Point along / against your ground speed — matches your velocity relative to the spinning atmosphere. Use this for the launch gravity turn |
| `>` / `<` | Tip the nose 5° east / west on top of whatever the autopilot is doing (hold to ramp) |
| `?` | Clear the manual tip (reset pitch trim to 0) |

The pointing keys only aim the craft — `b` is what actually fires the engine.
In RCS mode those same keys also fire one small thruster pulse per press (hold
a key for a steady stream). The readouts show which engine is armed, how much
RCS fuel you have, and how much Δv it's worth.

### Mouse

Click only — no dragging, no scroll-to-zoom.

| Click | Action |
|---|---|
| `[»Burn]` (top-right) | Toggle auto-warp to the next burn (same as `G`). Shows `[■Burn]` while running, dimmed when no burn is planned |
| `[Menu]` (top-right) | Save / load / settings / controls / quit menu |
| `[Missions]` (top-right) | Mission ladder — active mission checklist on top, the rest of the program below with locked rungs and pass / fail marks (same as `M`). Off by default; enable a program in `[Menu]` → Settings |
| A body | Follow it with the camera (same as cursoring onto it) |
| A craft | Follow it with the camera |
| A planned burn | Open the planner for that burn (its fire time is kept) |
| Empty space | Open the planner with a new burn at the nearest point on your orbit |
| A readout panel | Open body info |
| A porkchop cell | Move the cursor there (then `Enter` to plan it) |

### Maneuver planner (`m`)

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Move between fields (direction → when → Δv → throttle → refine) |
| `←` / `→` | Change the field you're on (direction / when / refine) |
| `Space` | Toggle the refine option when you're on that field |
| digits / backspace | Type a Δv or throttle value |
| `Enter` | Lock in the burn |
| `Esc` | Cancel and go back |
| `Ctrl+D` | Delete the burn you're editing (does nothing when creating a new one) |
| `c` / `C` | Clear every planned burn for this craft |

The panel lists all the burns you've planned for the current craft —
direction, Δv, and a countdown — with the one you're editing marked, so you
can see your whole schedule, not just the burn in front of you.

The Δv you enter sets the burn's length automatically; craft with no engine
fall back to an instant nudge. The "when" field lets you fire at a set time or
at the next high point, low point, or orbital-plane crossing. Throttle is saved
per burn, so changing your live throttle while coasting won't slow down a burn
you've already planned. The preview updates the resulting orbit as you edit.

The **refine** toggle spends a little extra Δv to make up for the fuel lost
steering and fighting gravity during a long burn, so you end up where an
instant burn of the same size would have put you. Leave it off for short burns;
turn it on for low-thrust craft or big burns where the loss is noticeable.

### Porkchop plot (`P`)

| Key | Action |
|---|---|
| `←` / `→` | Move the departure-day cursor |
| `↑` / `↓` | Move the travel-time cursor |
| `Enter` | Plan the transfer for the selected cell |
| `o` | Open transfer options — number of laps, prograde/retrograde, and the short/long path; closing re-draws the grid |
| Click a cell | Move the cursor there (then `Enter` to plan) |
| `Esc` | Back to the map |

The cursor opens on the cheapest cell. A `·` marks cells where no transfer was
found — `Enter` does nothing there.

### Vehicle Assembly (VAB) (`Esc → [Build (VAB)]`)

The VAB is where you design a custom vehicle from fine parts and save it to
launch later. You compose **components** — engines, fuel tanks, command cores,
antennas, and structure — into stages, stack the stages into a vehicle, and
read a live **Δv / TWR / mass** panel as you go. Saved designs are global (they
survive across games) and show up in the spawn form (`n`) alongside the
built-in craft, so you design once and launch many.

| Key | Action |
|---|---|
| `Tab` | Switch the active column (palette ↔ vehicle) |
| `←` / `→` | Switch column — palette on the left, vehicle (stack) on the right |
| `↑` / `↓` | Move the cursor in the active column |
| `PgUp` / `PgDn` | Jump to the previous / next kind section in the palette, or to the previous / next stage in the vehicle column |
| `a` | Add the selected component to the current stage — or, for a catalog part, add it as a new whole stage |
| `n` | Start a new empty stage on top |
| `x` | Remove the component group under the cursor — or the whole stage when the cursor is on a stage header or the stage is empty / a catalog part |
| `+` / `-` | Increase / decrease the count of the component group under the cursor (the `×N` cluster count) |
| `[` / `]` | Move the cursor's stage down / up in the stack (reorder) |
| `y` | Duplicate the stage under the cursor |
| `d` | Toggle a **dock seam** below the stage — everything above the seam becomes a nose payload you release with Undock (`U`) instead of staging. Mark several seams for several payloads |
| `c` | Toggle a **fused decouple** — the stage drops together with the group below it on one staging press |
| `s` | Name and save the design |
| `o` | Open a saved design to edit (`x` deletes the highlighted one) |
| `Esc` | Back to the map |

A stage can hold only **one fuel chemistry** — the VAB won't let you mix, say,
a kerolox engine with a hydrolox tank in the same stage. Multiple engines in
one stage are fine and combine honestly (thrust adds; the effective Isp is the
thrust-weighted blend). Soft warnings flag the usual snags — no engine, no
command source (it defaults to a probe core on spawn), or a lift-off TWR below
1 — but never block you from saving.

Designs are stored as portable files under
`$XDG_CONFIG_HOME/terminal-space-program/designs/` (typically
`~/.config/terminal-space-program/designs/`); copy one into the
`loadouts/` overlay dir next to it to share it as a mod.
