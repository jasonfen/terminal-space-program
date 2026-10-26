# Controls & flight guide

How to fly the thing, then every key grouped by what it does. The in-game
`F1` overlay is the quick reference; this page is the same list with a little
more explanation. Multiplayer has its own guide in
[multiplayer.md](multiplayer.md).

**Terminal size:** the game is designed for a 140×40 terminal; that's the
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
  - [Camera and views](#camera-and-views)
  - [Time](#time)
  - [Manual flight](#manual-flight)
  - [Targets and the map cursor](#targets-and-the-map-cursor)
  - [Planning burns](#planning-burns)
  - [Vessels, staging, and docking](#vessels-staging-and-docking)
  - [Multiplayer](#multiplayer)
  - [Mouse](#mouse)
- [Readout glossary](#readout-glossary)
- [Screens](#screens)
  - [Spawn form (`n`)](#spawn-form-n)
  - [Maneuver planner (`m`)](#maneuver-planner-m)
  - [Porkchop plot (`P`)](#porkchop-plot-p)
  - [Rendezvous Planner (`K`)](#rendezvous-planner-k)
  - [Vehicle Assembly Building](#vehicle-assembly-building-esc--build-vab)
  - [Missions ladder (`M`)](#missions-ladder-m)
  - [Saves](#saves-esc--save-game--load-game)

## Quick tour

You start as a vessel called **Apollo TLI-1** in a 500 km circular orbit
around Earth, tilted 51.6° to the equator (so a launch from the KSC pad can
meet it), moving prograde (the direction of travel). It is the S-IVB with the
Lunar Module and the CSM still on top, the way a Saturn V stack sits after
trans-lunar injection; its docking port is the Command Module's nose. Press
`space` once to drop the S-IVB, then `D` for the Apollo transposition. (The
bare S-IVB is still in the spawn form and on the command line as `S-IVB-1`.) The left panel is the map:
the Sun (or whatever you've focused on) in the middle, planets on their real
orbits, and your vessel as a little chevron pointing the way it's moving. The
right-hand panel is your readout: the clock, what you're looking at, fuel and
attitude, any burns you've planned, and a preview of where they'll put you.

Speed time up or slow it down with `.` and `,` to watch the planets move;
pause with `0`.

To go somewhere, say the Moon:

1. Press `t` to pick a **target**. Keep tapping until the target readout shows
   the Moon. (`h` / `l` move a separate map cursor used by the body info
   screen; they don't set your travel target. `shift` + the arrow
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
3. Above 10 km, pitch east with `W` (Flight School's turn) and hold it
   through the climb: it points the rocket along your motion relative to the
   ground. To start bending over sooner, tap `→` a couple of times to tip
   ~5° toward your heading each (east, unless you trimmed it with `↑`/`↓`); the rocket starts building sideways speed.
4. As your ground speed passes ~100 m/s, keep `W` on so the rocket tracks its
   own motion and gravity rounds the climb into orbit for you. If you tipped
   it with `→`, press `|` to clear the pitch trim.
5. When the first stage runs dry, press `space` to drop it. You keep flying the upper
   stage and the stage list advances. Keep burning, drop the next stage, then
   the last one.
6. Watch the projected high point climb. When it clears Earth's orbit floor
   (175 km, the atmosphere's top plus a safety margin) the **● ORBIT
   READY** note appears on NAVIGATION's title. That's your cue to cut the
   throttle (`x`) and coast.
7. Press `C` to plan a circularising burn at the top of your arc.
8. Coast up to that point. The planned burn fires on its own and rounds out
   your orbit. Once your low point also clears the floor you're safely in
   orbit, and the LAUNCH readout tracks the gap along the way.

The whole thing runs on numbers, not memorised pitch tables: the high point
climbs as you burn, ORBIT READY tells you when to stop, `C` sets up the last
burn, and the LAUNCH readout closes the gap. If a stage flames out early, the
readout shows you why before you watch it fall back.

## Keyboard layout

The keys below are written for **QWERTY**. If you play on a **QWERTZ** keyboard
(where the physical `Y` and `Z` keys are swapped), open `Esc → Keyboard layout` and
switch the layout to QWERTZ. Letter bindings then stay under the same finger as
on QWERTY, and the in-game help overlay (`F1`) relabels itself to match your
keycaps. **Only the letters move** (the swap is `Y` and `Z`, nothing else).
Punctuation does not: `+` `-` `,` `.` `;` `<` `>` `{` `}` `|` `/` `[` `]` are
matched by the character they type, so they sit wherever your keyboard puts that
character, which may not be under the same finger as on QWERTY. The choice is
saved to your config and applies to every game. AZERTY, Dvorak, and free
per-key remapping aren't supported yet.

On the Keyboard layout screen `←` / `→`, `Space` or `Enter` cycle the layout and `Esc` goes back. On Settings, `↑` / `↓` pick a row, `Space` or `Enter` flips or cycles it (`Enter` does the same as `Space`), and `Esc` goes back to the pause menu.

## Keybindings

### Global

| Key | Action |
|---|---|
| `Esc` | Back to the screen that opened you: a screen opened from the pause menu (saves, settings, keyboard layout, help, build) returns to the menu, one opened by a key (missions, spawn, body info, session) returns to the map. On the main view, open the pause menu: a card over the dimmed map with save / load / build / settings / keyboard layout / help / quit. While the card is open the clock is stopped (the flight bar reads `PAUSED`); closing it hands the clock back as it was. `↑`/`↓` pick a row (it is highlighted) and `Enter` opens it; the letter beside each row (`s` `l` `b` `t` `k` `h` `q`) is a shortcut. A second `Esc` from the menu flies |
| `Ctrl+C` | Quit: Save before quitting? `y` saves and quits, `n` asks Are you sure? (`y` quits without saving, `n` goes back to the first card), `esc` goes back to where you were. The pause menu's Quit row asks the same two cards |
| A card | A question over the dimmed map (the quit prompt, `E`, `D`, `Y`, `F9`). `↑`/`↓` move the `▸`, `Enter` picks that row, `y` picks the first row, `n` or `Esc` is the way out. An action row asks once more on the same card before it acts, and there `y` says yes and `n` or `Esc` says no; the way out acts at once; the two quit cards act at once on every row. A click on a row picks it; a click anywhere else does nothing. The clock keeps running under the card |
| `F1` or `?` | Toggle the help overlay. It opens on an index: "Your first flight" first, then every section with a note on when you would want it. `1`-`9` jump to a page (from a form's own page a jump also drops the way back, so `Esc` then goes to the index), `↑`/`↓` + `Enter` reach the rest, `Esc` goes back to the index (and closes from the index), `F1` closes from anywhere. Long pages scroll with `↑`/`↓`, `PgUp`/`PgDn`, `Home`/`End`. `?` opens the same overlay as `F1` everywhere `F1` does. From a form (spawn, VAB, roster, planner, porkchop, missions, saves, settings, keyboard layout, the pause menu) it opens on that screen's own page; from body info and the map it opens the index, and `F1` or `Esc` puts you back exactly where you were: same field, any half-typed number or name kept. Inside a name or number box only `F1` opens it (`?` is a typed character there) |
| `Enter` | Does the highlighted or typed thing; each screen's table says what. On the map it does nothing, except while `j` inspects |
| `F2` | Declutter: hide all eight instrument boxes, the navball and the Centerline scope together for a clean look at the orbit. Press again to restore. ENGINE and PROPELLANT stay up while an engine is lit, so fuel and a live burn are never hidden mid-burn |
| `` ` `` | **Boss key**: instantly swap the screen for a convincing fake developer shell. Type `exit`, `logout`, or `Ctrl+D` to come back where you left off. Left out of the `F1` overlay on purpose |
| `Tab` | Switch star system (Sol first, then alphabetical: Alpha Centauri, Kepler-452, Lumen, TRAPPIST-1). A camera toggle only; vessels stay in the system they spawned in |
| `i` | Body info screen for the body under the map cursor |
| `M` | Mission ladder: program and objective progress, the active mission's checklist, and locked rungs with what unlocks them. Same as the `[Missions]` button. Flight School (the tutorial) is **on unless switched off**; the Challenge ladder stays opt-in; enable it in `[Menu]` → Settings |
| `O` | Session roster (multiplayer). See [Multiplayer](#multiplayer) |

### Camera and views

| Key | Action |
|---|---|
| `shift+↑` `shift+↓` `shift+←` `shift+→` | Pan the map. The camera keeps following your focus, just displaced; `g` or any refocus snaps it back. The plain arrows are the flight trims (see Manual flight), in every flight view; a menu, the mission ladder, body info or the Rendezvous Planner picker keeps them while it is open |
| `+` / `-` | Zoom in / out. Zoom is remembered per focus |
| `f` / `F` | Cycle what the camera follows, forward / back (whole system → each body → your vessel). Focusing a body your trajectory passes fits the camera to its sphere of influence so the capture curve fills the canvas. Also the way back from spectating another player |
| `g` | Snap a pan back onto what you're following; focus and zoom stay as they are |
| `alt+g` | Reset the camera to the whole system (clears any pan) |
| `v` | Cycle the projection: Tilted (default, a 3D-style perspective) → Top → Right → Bottom → Left → Orbit-flat. Two rows above the `view:` label read which way north points (`N ↑` up the screen, `N ⊙` toward you, `N ⊗` away; in Tilted and Orbit-flat a degree figure, `N ↑ 42°`, is how far north tilts out of the screen toward or away from you, 0° being flat on the screen, and it is not the orbit ring's `plane` angle) and how open your orbit ring looks (`plane ◠ 67° open`: 0° `─` is a flat line, 90° `○` a full circle); the maneuver planner shows the same cue. The camera re-frames once when focus, view, or system changes and otherwise stays where you put it |
| `>` / `<` | Tilt the 3D view up / down (tilted view only). Camera only: a missed shift can move the view, never the vessel |
| `{` / `}` | Yaw the 3D view left / right in 5° steps, wrapping (tilted view only) |
| `o` | **Proximity view** for the last kilometres of a rendezvous: the target vessel sits dead centre, its direction of travel runs right, the planet is below, so you read your drift the way the physics works. Needs a vessel target; a `CLOSE RANGE` chip reminds you once within 35 km. The side picture takes the full width, with a dotted corridor running 50 m out along the target's port axis; at the 140x40 design size the [Centerline scope](#docking) sits in its top-right corner, over the navball's twin slot. Below that size the scope is dropped. Both vessels' docking ports are marked `○` (theirs in target green, yours in your vessel's colour), a port line joins them with its length, and the chip gains `port:`, `misalign:`, `lateral:` and `hold:` rows (see [Docking](#docking)). The `range:` row reads one decimal under 10 m (`range: 1.0 m`) and whole metres from 10 m. The plain arrows still trim your vessel here; the PROXIMITY chip's `trim:` row and the bottom legend show it. Press again to return to the map as you left it |
| `V` | **Launch / surface view**: chase-cam on your active vessel with a curved horizon, pad marker, and breadcrumb trail, scaled tight at liftoff and pulled back high up. Lifting off routes you here automatically. Press again to return to the map |
| `j` | **Inspect**: each press steps a bright highlight onto the next thing drawn (bodies, vessels, other players' ghosts, planned burns, the closest-approach `✕`) and names it in a chip. One press past the last item clears it; `Esc` clears immediately. Clicking any orbit line or marker jumps the highlight there |
| `Enter` | While inspecting: make the highlighted thing your target and clear the highlight. Works on vessels and other players, not just bodies. Things that can't be a target say so |

### Time

| Key | Action |
|---|---|
| `0` | Pause / resume |
| `.` / `,` | Speed time up / down, to 100,000×. Eases off automatically around a burn, and is held to 10× within 50 m of another vessel (see [Docking](#docking)) |
| `G` | Auto-warp to the next burn (any vessel's): warps to 30 s before it fires, ramps down, and hands you 1× to watch it arm. On the pad with a target, it warps to 30 s before the launch window on the `plan:` row instead (the best moment, when there is no exact pass) and turns your heading to the one the row names, so you only have to light the engine. Inside the last 30 s there is nothing left to warp, so `G` just turns the heading to the window's and says so (`heading 045° for the launch window`). `|` still clears both trims. `↑` / `↓` afterwards are yours. `.` / `,` or the `[»Burn]` button cancels it. It chases burns, not a commanded raise (`L`) |
| `/` | Cancel warp: straight back to 1× from any level, and cancel auto-warp or a rendezvous warp if one is running |
| warp caps | 10× while an engine burns, or within 50 m of another vessel (so two hulls cannot skip through each other); 1× while the dock magnet pulls; a commanded raise (`L`) never caps warp |

During a multiplayer rendezvous warp the manual warp keys and `G` are inert
and `/` is the only cancel; in the terminal phase `,` and `.` become the
copilot's brake. See [multiplayer.md](multiplayer.md#rendezvous-warp).

### Manual flight

| Key | Action |
|---|---|
| `c` | Throttle target: all / core / boosters. The throttle keys below move the selected throttle; the flash names it (`throttle: core`). A vessel with one throttle answers `one throttle` and nothing changes. The target resets to all when the boosters drop |
| `z` / `x` | Selected throttle to full / cut to zero |
| `Z` / `X` | Selected throttle up / down 10% |
| `alt+z` / `alt+x` | Selected throttle up / down 1% (the fine step, like the alt+arrow nose trims; `alt+Z` / `alt+X` work too) |
| `b` | Light / cut the main engine (needs throttle above zero). Lit, it cuts every liquid engine whatever the target, never a lit solid |
| `w` / `s` | Point prograde / retrograde (with / against your motion) |
| `a` / `d` | Point normal+ / normal- (perpendicular to your orbit) |
| `q` / `e` | Point radial+ / radial- (away from / toward the body) |
| `W` / `S` | Point along / against your ground speed (velocity relative to the spinning atmosphere). Use this for the launch gravity turn |
| `→` / `←` | Tip the nose 5° toward / back from your heading on top of whatever the autopilot is doing (hold to ramp); in the Port frame, 1° right / left across the port face instead (see [Docking](#docking)). Works the same in the map, the `V` launch view, the Proximity view (its PROXIMITY chip shows the trim) and every projection. A menu, the mission ladder, body info or the Rendezvous Planner picker takes the arrows while it is open |
| `↑` / `↓` | Nudge your commanded launch heading 5° toward north / south off due east (hold to ramp), on the pad or mid-ascent; in the Port frame, tip the nose 1° up / down across the port face instead |
| `alt+←` / `alt+→` | Fine pitch trim: 1° back from / toward your heading (alt makes any trim fine); in the Port frame 0.25° left / right across the port face |
| `alt+b` / `alt+f` | The same fine pitch trim, for Terminal.app and Ghostty, which send Option+← / Option+→ as these two keys |
| `alt+↑` / `alt+↓` | Fine heading trim: 1° toward north / south, so you can command the exact heading the pad window names on NAVIGATION's `plan:` row; in the Port frame 0.25° up / down across the port face |
| `alt` | The Alt key on Windows and Linux (WezTerm and most terminals send it as-is); Option on a Mac, see the next row |
| `alt` on a Mac | Ghostty sends Option as alt as-is. Terminal.app and iTerm2 need Option sent as Meta: Terminal.app, Profiles > Keyboard > "Use Option as Meta key"; iTerm2, Profiles > Keys > Left Option key = Esc+ |
| `\|` | Clear the manual tip and the commanded heading (both trims back to 0); in the Port frame, clear the port-face trims |
| `;` | Autopilot reference: Surface → Orbit → Target → Port (Target needs a targeted vessel; Port needs that vessel, yours or a friend's, to have a docking port; reaching Port turns nothing, the flash reads `nav: PORT`). It also switches itself: Surface → Orbit climbing through the upper atmosphere (Earth 51 km), Orbit → Surface coming back down. Press `;` to override; it is kept until the next crossing. Target and Port are only ever chosen by hand: they never switch themselves, even right beside a target vessel |
| `k` | Steering style: smooth turning (default) or instant snap |
| `r` | Switch between the main engine and RCS thrusters |
| `p` | RCS pulse step: cycle the per-press Δv (0.1 → 0.01 → 0.001 m/s) |

The pointing keys only aim the vessel; `b` is what fires the engine. In RCS
mode those same keys also fire one small thruster pulse per press (hold for a
steady stream). Each pulse is 0.1 m/s by default; `p` steps it down for fine
work like nulling an orbital period to the second (see the
[constellation deployment guide](constellation-deploy.md)). The readouts show
which engine is armed, the pulse size, RCS fuel, and how much Δv it's worth.

In the **Port** frame (`;` offers it once your target has a docking port, and
the navball button reads `[PORT]`) the same six keys point at the other
vessel's port instead of your orbit: with RCS armed `q` pushes you in toward
the port along its axis and `e` pushes you out; `w` / `s` push up / down
across the port's face and `a` / `d` push left / right. With the main engine
selected `q` holds your nose on the port axis (`hold: Port+ (PORT)`, the
docking attitude) and `e` holds it away. The arrow keys turn the nose across
the port face on top of that hold: `↑` / `↓` tip it up / down and `←` / `→`
left / right, 1° a press (`alt` 0.25°), and `|` clears them; the PROXIMITY
chip's `trim:` row reads the lean, `trim: 1° up  2° left (port)`. The frame
drops back to Orbit if you change target or the target loses its port;
XPrograde with it.

### Targets and the map cursor

Two different things point at bodies. The **target** (`t` / `T`) is what you
plan trips to and what the target readout describes. The **map cursor**
(`h` / `l`) feeds the body info screen (`i`) and
does not affect your travel target. On the body info screen itself, `t`
targets the body shown (not the next one in the cycle), after which `H`
(transfer) and `P` (porkchop) plan to it; the star and the body you orbit
refuse in one phrase.

| Key | Action |
|---|---|
| `t` / `T` | Pick / clear your target. `t` cycles nearest-first: the moons of whatever you're currently orbiting, then other vessels, then the rest of the system's bodies outward (each followed by its own moons), then none; a released batch is one stop (its nearest member); `j` then `enter` picks a particular one. Your own primary is never offered: from LEO the first press is the Moon; from lunar orbit it's Earth. `T` clears (no reverse cycle) |
| `l` / `h` | Move the map cursor to the next / previous body |

### Planning burns

| Key | Action |
|---|---|
| `m` | Open the [maneuver planner](#maneuver-planner-m) |
| `H` | Plan a transfer to your target. Refuses while any burn is planted (clear it in `m` first) and for a moon of another planet. To a moon of your current planet it works out two routes and plans the cheaper, showing both costs. To another planet it plans a Hohmann transfer. To a moon's parent planet it plans an escape |
| `I` | Plan a burn to match your target's orbital tilt (or to level out to the equator with no target) |
| `C` | Plan a circularising burn at the top of your orbit; pairs with the ORBIT READY cue on launch. Refused if the top of your orbit is inside the atmosphere or you're on an escape trajectory |
| `K` | Close in on a target vessel. Close and near-matched → plants a small nudge directly, using the closest-approach numbers in the target readout. Too far apart in phase → opens the [Rendezvous Planner](#rendezvous-planner-k) instead of refusing. Your planes differ → names `I` instead of planting anything. Needs a vessel target sharing your planet |
| `P` | [Porkchop plot](#porkchop-plot-p) to your `t` target planet. Refuses with no target, a vessel target, or a body in your own system (your planet, its moons, or the planet your moon orbits): those are `H` trips |
| `R` | Refine the plan: recompute the transfer from where you are now and update the arrival |

### Vessels, staging, and docking

| Key | Action |
|---|---|
| `n` | Open the [spawn form](#spawn-form-n) |
| `[` / `]` | Switch which vessel you're flying |
| `1`–`9` | Jump straight to vessel N (nothing happens if the slot is empty) |
| `space` | Drop the bottom stage (only if there's more than one), instantly, on the pad too; the STAGES box names it (`▸ S-IC (1/3) [space]`) and a flash confirms what dropped. On a bare capsule with a parachute this arms the chute instead; it opens on its own in the atmosphere. A Dragon drops its trunk first, then the next press arms the chute. Refused while lit solid boosters burn (`boosters still burning`); a booster stage (the SLS solids) drops as two vessels, `<vessel>-booster1` and `-booster2` |
| `Y` | Deploy the top carried payload as its own vessel (satellite, probe, station) while you keep flying the carrier. Asks first (there is no undo). Press again for the next one |
| `D` | Apollo transposition: flip the Service Module to the front to do the flying, leaving the Lunar Module as a nose payload (then `U` to release it). Asks first (there is no undo) |
| `U` | Undock a docked vessel into its parts and switch you to a released piece. Each piece comes back where its hull was drawn, port to port a row or two apart (1.5 to 3 m, by how tall the two stages at the seam are), and parts along the seam at 0.15 m/s. On a cross-player stack it releases your partner's vessel through the dock ledger, connected or not (an absent partner's vessel is parked as a **Parcel** and delivered, safed, when they next connect) |
| `U` (batch) | On a carrier with two or more payloads, `U` asks `Release all 24 Starlink?` and releases the whole batch at once. The pieces fan out sideways like a spread deck of cards, a few cm/s apart (the card prints the step between neighbours and the top speed), the carrier recoils the other way, and you keep flying the carrier. They are named `Starlink-1`, `Starlink-2` and so on and join the end of the vessel list. Refused with `release: stack along the velocity; hold radial (q) first` when the stack points within 10° of the velocity line, forward or backward. For one orbit the batch's own pieces do not hold the 10x warp limit on each other |
| `batch` | U releases a stack of payloads at once; [t] steps over it as one stop. About 30 vessels per player keeps the map quick |
| `L` | Orbit altitude. Type an altitude in km (it starts at the last one you typed) and `Enter`; the card prints the Δv, the propellant and the time at full thrust, and asks. An electric vessel (a Starlink) then raises or lowers its own orbit by a spiral, by itself, at any warp; ENGINE shows `▸ raise → 550 km  now 412 km  T-4d06h` and `● RAISE`, and it finishes with `raise complete, 550 km`. `L` again while it climbs asks `Stop the raise?` and leaves it coasting where it is. On a released batch (the carrier or any member) `L` asks `Spread 24 Starlink at 550 km?` instead and solves every satellite's own start, so they sit evenly round the orbit when the last arrives: the card says when the first and last start and what each climb costs, and a member's card also offers `raise this one`, which takes it out of the schedule. A `SPREAD` notice counts `arrived`, `climbing` and `waiting` and names the next start. `L` on the carrier while the spread runs asks `Stop the spread?`: the waiting satellites are dropped, the climbing ones finish. Also refused for a spread: `spread: not in one plane` (a member more than 0.5° off the others' plane), `spread: Starlink-3 already at 550 km` (within 1 km), `spread: too slow from this orbit (87 d)` (over 60 days), `spread: not on one orbit` Refused: `raise: thrust too high for a spiral` (a chemical engine), `raise: no engine`, `raise: orbit not round`, `raise: below the atmosphere`, `raise: not free` (landed, docked or mid-burn) |
| `u` | Hand over cargo. While docked to a stack that has a **Cargo Hold**, every cargo load your vessel carries moves into the hold, at once, one way (nothing comes back). The flash states the mass (`3.307 t cargo handed over`), or the state that stopped it: `not docked`, `no hold aboard`, `no cargo aboard`. Lowercase `u`; `U` is undock. Only the vessel's owner hands over its cargo: docked to a friend's vessel, your `u` is asked of their seat and the answer shows on your session chips |
| `alt+u` | Re-arm docking (Option+u on a Mac with Option as Meta, see MANUAL FLIGHT). After an undock a pair is held apart until their ports are more than 5 m apart or ten minutes pass (the PROXIMITY port row says `latched` while it holds), even if you fly back into the docking gate. `alt+u` clears that hold now: with a vessel targeted, just that pair; otherwise every hold on your active vessel. A pair already holding the gate starts the magnet on the next tick |
| `J` | Transfer control of a cross-player docked stack to the guest docked to it. Multiplayer only; see [multiplayer.md](multiplayer.md#docking-across-players) |
| `E` | End the flight: remove a crashed vessel (asks first) |
| `F5` / `F9` | Quicksave / quickload (`F9` asks first: a card, `Enter` picks the highlighted row) to a fixed quick slot, separate from named saves. See [Saves](#saves-esc--save-game--load-game) |

#### Docking

Docking is flown, port to port. Nothing fuses at a distance or by drifting
past: you bring your vessel's docking port to the other vessel's port,
slowly and straight, and the last half metre closes by itself.

- **Who has a port.** One docking port sits on the nose, the end away from the
  engine, of crewed modules and capsules, ascent stages, tugs, keepers,
  relays, stations and the Probe Sat. Launch vehicles, bare upper stages
  (the S-IVB, the ICPS), comsats, relay nodes and science probes have none
  and cannot dock. A lone vessel's port is its top stage's. A docked
  stack has no free port: the seam uses both noses and the far end is the
  partner's engine, so nothing docks there, and each piece gets its port back
  when the other leaves (a carrier with payload aboard likewise). Any port mates with any port. Targeting a port-less
  vessel in the Proximity view reads `no port`.
- **The gate.** A dock needs all three at once: the two ports within 0.5 m,
  relative speed under 0.1 m/s (port to port: a long stack turning with the
  orbit swings its port, and that counts; a pair holding still in the picture reads
  its gap times the orbit rate, about 0.03 m/s at 25 m), and the two noses within 4° of
  pointing at each other. The Proximity view shows them as the `port:`, `rel speed:` and
  `misalign:` rows, each with a `(need < ...)` reminder while you are inside
  50 m and that term fails; `misalign:` is the angle between the two axes,
  zero whenever the noses are parallel however far off the line you sit.
  `lateral:` splits the miss into up/down and left/right across the port.
  `hold:` names the attitude you are holding, as GUIDANCE's `hold:` row does. The port line and the Centerline ring go solid green when the gate
  holds. The 50 m ring is where the close picture starts, no longer a dock.
- **The Centerline scope.** At 140x40 a framed panel in the Proximity view's
  top-right corner looks out of your own docking port along your nose. Its
  title row reads `CENTERLINE  4 m      port 3.33 m`: the disc's width in
  metres, then the port range. Your port is a dotted crosshair whose arms run
  to the disc's rim, with a tick at up, right, down and left. Their port is a
  ring 0.5 m in radius (the gate's port term), drawn where it is across your
  face: up and down, left and right, the same two numbers as `lateral:`. A
  `w` pulse (push up) moves the ring down the disc, because you went up.
  Inside the ring a `+` (the standoff cross) sits off the ring's centre by
  your nose's lean off parallel to their port axis, the two components the
  navball's `pitch` / `yaw` row reads; it reaches the ring's edge at exactly
  the 4° the gate allows, so a cross inside the ring means the alignment term
  holds. The ring is a `○` glyph under 3 cells across and carries no cross
  under 5. The disc ranges itself: its width steps through 2, 4, 8, 16, 32
  and 64 m, out when their port would leave the disc and back in when it
  would fit the next step down with room, never flickering. Beyond 64 m the
  `○` sits on the rim in the port's direction and `lateral:` carries the
  metres. `+` / `-` zoom the side picture only; the scope has no zoom, and
  the scale bar under the side picture is its own. Both hulls are drawn
  end-on as dotted circles: theirs, of their width, around the ring; yours,
  of your width, around the crosshair. Circles apart mean you pass clear
  moving along their axis; overlapping circles mean you will bump. When your
  nose leans, your outline stretches into a stadium toward where the bottom
  of your stack projects (stack length times the sine of the lean). The
  outlines are clipped at the rim and never change the step. Once you have
  overshot, so their port is behind the plane through yours, the ring
  becomes `◌` at any scale, the cross is hidden, and the title's range reads
  negative (`port -4.12 m`); the hulls stay. The PROXIMITY chip's `port:` row
  stays unsigned.
- **The magnet.** The tick the gate holds, a five-second pull starts. It moves
  the vessel you are flying (the active one) the last half metre onto the
  port axis and turns its nose square; warp drops to 1×, and your RCS pulses
  and attitude keys are ignored until it finishes (the `port:` row reads
  `capturing`). If the other vessel leaves the gate meanwhile, by turning or
  pulsing away, the magnet lets go and nothing fuses. Completing the pull is
  the dock: the composite stays where the lead vessel's hull was, with the
  partner drawn nose to the seam above it.
- **The PORT frame.** `;` reaches `[PORT]` once your target has a port. There
  `q` / `e` push in / out along the port's axis, `w` / `s` up / down and
  `a` / `d` left / right across its face (RCS armed), and the main-engine `q`
  hold (`Port+`) puts your nose on the port axis, the docking attitude
  (`hold: Port+ (PORT)` on the chip). Reaching `[PORT]` turns nothing on any
  vessel; the flash reads `nav: PORT`. The navball's text row reads
  `pitch` / `yaw`: how far your nose is above or below, and right or left of,
  parallel to their port axis (the same two components `misalign:` combines).
  The arrow keys act on that same pair: `↑` / `↓` tip your nose 1° up / down
  across the port face and `←` / `→` 1° left / right (`alt` makes it 0.25°),
  on top of the hold and while the letters keep sliding you; the standoff
  cross moves with them and `|` clears them. Only one trim pair is live at a
  time: reaching `[PORT]` clears the surface trims, leaving it (or changing
  target) clears the port-face trims, and the port-face trims are not saved.
  The speed readout is then relative to the target and tagged `(port)`.
  `p` steps the pulse down to 0.01 or 0.001 m/s for the last metres.
- **A bad approach bumps.** Hulls that touch outside the gate (too fast, too
  crooked, or not at the ports) bounce: the pair pushes apart at half the
  closing speed, never under 0.05 m/s, and the bottom edge reads
  `bumped <vessel> at <speed>; re-approach under 0.10 m/s`. Nothing breaks.
  Hulls are flat-ended, so nose against nose only touches when the ends
  meet. A hull is as wide as its widest part's published diameter (a Dragon
  4.0 m, an RCS Tug 1.0 m, an S-IVB 6.6 m), so a slim tug can pass close by a
  wide station. Landed vessels and two debris stages never bump. Within 50 m of
  another vessel's hull (hull to hull, not stack bottom to stack bottom) warp is held to 10×, the cap a burn imposes, so hulls cannot
  skip through each other between ticks.
- **Undocking.** `U` parts a composite along its seam: each piece comes back
  where its hull was drawn, port to port a row or two apart (1.5 to 3 m), opening at 0.15 m/s.
  The pair cannot re-dock until it clears 100 m or ten minutes pass; `alt+u`
  clears that hold now.
- **Alongside spawn.** The spawn form's `alongside` position puts the new
  vessel 25 m port to port ahead of your nose, pointing back at you, at your
  velocity: a straight, slow approach, outside the gate, with no bump.
- **Cargo.** A vessel with a **Cargo Hold** (a structure component, 100 kg, built
  in the VAB) receives cargo; nothing else does. Docked to one, press `u` and
  every cargo load your vessel carries moves into the hold, once, one way.
  Undock and each vessel leaves with what its stages hold. A hand-over changes
  where the mass sits, not how much there is. Across players the Dragon's owner
  presses `u`, whoever approached.
- **Across players.** Docking to a friend's vessel is the same skill: the
  approacher flies it, their hull and port are drawn from what they last
  reported, and `;` reaches `[PORT]` against their vessel too. A friend whose
  report carries no hull reads `port: unknown` and cannot be captured. See
  [multiplayer.md](multiplayer.md#docking-to-a-friend).

### Multiplayer

Full mechanics (hosting, independent time, rendezvous warp, cross-player
docking, chat) are in [multiplayer.md](multiplayer.md). The keys:

| Key | Where | Action |
|---|---|---|
| `O` | flight view | Open the **session roster**: one row per player with online state, last-known system, vessel count, and time offset vs you |
| `~` | flight view | **Chat**. `enter` broadcasts, `esc` bails; a leading `@handle` sends a private line, `tab` completes the handle, and a typo refuses to send. Messages show as chips for ~30 s; nothing is stored |
| `J` | flight view | Transfer control of a docked stack to the guest docked to it |
| `t` | roster | Target a player's vessel (expands to a picker when they have several in your system; `esc` backs out) |
| `v` | roster | Spectate: fit the camera to their ghost orbit and follow it. `f` returns to your own vessel |
| `s` | roster | Sync-warp forward to a player's time (forward only; a player behind you syncs to you) |
| `w` | roster | Agree to a rendezvous warp with them: commits to an encounter when one's found (a planted node's own arrival, or the current course inside 4 h), otherwise arms with no plan yet; they answer `y` on their main screen |
| `y` | flight view | Accept a rendezvous warp |
| `/` | flight view | Cancel a rendezvous warp (either side) |
| `h` | roster | Host: start hosting from single-player in place, or stop hosting (stop asks first: a card; it drops every guest cleanly and keeps progress) |
| `d` | roster | Host or admin: disconnect the selected player now. They reconnect at once with the same key (asks first: a card) |
| `i` / `r` | roster | Host or admin: mint / revoke an invite code. In the invite box `Enter` keeps the typed handle and mints, `Esc` cancels |
| `x` | roster | Host or admin: kick the selected player (asks first: a card with `kick`, `kick and delete vessels` and `cancel`). They are dropped now and need a new invite to return; their vessels stay saved unless you pick `kick and delete vessels`, which backs them up first |
| `p` | roster | Host only: promote the selected player to admin, or demote them |
| `F4` | roster | Host or admin: restart the server. Everyone is warned and drained with progress saved; on a self-updating host it adopts a newer release. Asks first: a card |

The roster's KEYS box shows the keys that ask first (`d` disconnect, `x` kick, `F4` restart server, `h` stop hosting) on their own amber row. On a card, `↑`/`↓` + `Enter` pick the highlighted row, `y` says yes, `n` or `Esc` says no, and a click on a row picks it; picking an action row asks once more before it acts.

Flight controls are inactive on the roster; only the keys listed apply.

### Mouse

Click only; no dragging, no scroll-to-zoom.

| Click | Action |
|---|---|
| `[»Burn]` (top-right) | Toggle auto-warp to the next burn, or on the pad to the launch window (same as `G`). Shows `[■Burn]` while running, dimmed with nothing to warp to |
| `[Menu]` (top-right) | Save / load / build / settings / keyboard layout / help / quit menu |
| Settings → **Empty readings** | What the instrument boxes do with boxes and rows that have nothing to say. **Full**: every box and row always drawn, dashes and all, TARGET included. **Tidy** (default): no TARGET box while nothing is targeted, and NAVIGATION drops its trailing dash rows while TARGET is absent. **Compact**: Tidy, plus every box drops its trailing dash rows. Rows fold from the bottom of a box only; box widths never change |
| `[Missions]` (top-right) | Mission ladder (same as `M`). Flight School is on unless switched off; the Challenge ladder stays opt-in; enable it in `[Menu]` → Settings |
| A body | Follow it with the camera, and inspect it |
| A vessel | Follow it with the camera, and inspect it |
| A planned burn | Open the planner for that burn (fire time kept), and inspect it |
| An orbit line | Inspect it: the line flares and a chip names whose it is (`Enter` targets, `Esc` clears). Your **own** orbit line instead plants a burn there |
| Empty space | Open the planner with a new burn at the nearest point on your orbit |
| A readout panel | Open body info |
| A porkchop cell | Move the cursor there (then `Enter` to plan it) |

## Readout glossary

Labels on the HUD are one plain word each, but a handful of codes are
short enough to earn their keep. This is the same block the in-game `F1`
overlay carries, so you can look one up without leaving the game.

| Code | Meaning |
|---|---|
| `fpa` | Flight path angle: velocity above / below the local horizontal |
| `Q` | Dynamic pressure: aerodynamic stress on the airframe, in kPa |
| `TCA` | Time of closest approach, in a rendezvous or flyby |
| `TWR` | Thrust to weight ratio: engines' thrust over vessel weight |
| `Ap` / `Pe` | Apoapsis / periapsis: height above the surface, like every altitude in the game |
| `Δv` / `Δincl` | Delta-v / relative inclination to a target's orbital plane |
| `T-` / `T+` | Countdown convention: `T-` counts down to an event, `T+` counts up since it |
| `incl (min N°)` | On the pad: the inclination your commanded heading yields, and the Inclination Floor (your launch latitude, unsigned) it can't go below |
| `depart` | The angle between the orbit you'd reach and the plane the world beneath you travels in: the plane you leave along |
| `Ap ↑` / `↓` | Apoapsis trend: climbing / falling; no glyph while it's steady |
| `→` | Becomes: between a current value and a planned or resulting one, wherever it appears |
| `⚠` (on a node) | The planned burn exceeds the stage's Δv budget |
| `plan` | The world the planned burn's numbers are measured from, plus its node angles |
| `pro` / `retro` | Prograde or retrograde: which way the orbit runs, tagged on `incl:` |
| `(ORBIT/SURF/TGT)` | The frame a held direction or `nav:` reads in: orbit, surface, target or PORT (the aimed docking port's frame, see [Docking](#docking)); one spelling everywhere it's named |
| `amber (SURF)` | GUIDANCE `hold:` amber frame word differs from `nav:`; it keeps the frame it was pressed in, on purpose |
| `[MAN]` / `[AUT]` | The navball's attitude tag: MAN turns the nose at a rate-limited slew; AUT (amber) snaps it instantly. Click it to switch |
| `RCS` | The navball's toggle: amber when the RCS thrusters, not the main engine, are armed (`r` does the same) |
| `rcs` | Monoprop's own delta-v, from the RCS thrusters rather than the main engine |
| `cargo / hold` | cargo is what the vessel carries to deliver; hold is what its Cargo Hold has received (`u` hands cargo over while docked). A dash means none; 0 kg is an empty hold that can receive |
| `raise / lower` | ENGINE's node row while an orbit order runs: the target altitude, the altitude now and `T-` to arrival; `starts in` while it waits. The title carries `● RAISE` on that vessel |
| `boosters / core` | ENGINE's badge and throttle label: boosters are the stage lit together with the stage above it, core is that stage above. The badge carries the group's burn clock (`T-` to its burnout, `DRY` once empty, with its throttle if liquid); the throttle label reads `core:` or `boosters:` for the throttle `c` selected |
| `◉ ● ○ ◇` | STAGES pips, bottom stage first: `◉` lit (burns next; every stage of a booster group is lit), `●` fuelled and waiting, `○` spent, `◇` no tank (a pod or payload) |
| `speed` | Speed in the navball frame, named (surface) / (orbit) / (target) / (port), alongside `vert:` and `horiz:` |
| `(max N)` | The same figure at full throttle, when it differs from the current one |
| `heading:` / `hdg` | GUIDANCE `heading:` is the bearing you have commanded; the navball's `pitch` and `hdg` are measured, where the nose points now (they agree on the pad, and differ while the nose slews). In ORBIT the ball's rungs count degrees out of the orbit plane, not above the horizon. `trim:` is how far ←/→ lean the nose down or up off the hold, so `15° down` on a level prograde nose reads `pitch -15°`; → leans toward your heading, which lowers a prograde nose and raises a retrograde one |
| `● ORBIT READY [C]` | Apoapsis has cleared this world's orbit floor: press `C` to plant the circularising burn |

## Map markers

Each marker on the map is one glyph; the colour says what kind it is. This is the same block the in-game `F1` overlay carries.

| Marker | Meaning |
|---|---|
| `➤` | your vessel, in yellow (or your theme's craftmarker colour); other vessels and ghosts wear their own |
| `green` | the vessel you have targeted, and its orbit (`t` picks a target) |
| `Δ` | a planned burn, in the colour of the dashed orbit after it (blue, mint, amber, pink); click to edit |
| `▲` | apoapsis: the highest point of the orbit |
| `▼` | periapsis: the lowest point of the orbit |
| `◇` | ascending node: your orbit crosses your target's orbital plane going north (needs a target) |
| `◆` | descending node: the same crossing going south of the target's plane |
| `⊕` | perilune: the closest point of your pass by another body |
| `✕` | closest approach: one mark on your path, one on the target's, where you pass nearest |
| `▷` | entry: your path crosses into a body's sphere of influence |
| `◁` | exit: your path crosses back out of it |
| `⊗` | impact: where your trajectory meets the ground (surface view) |
| `dim / red` | a dimmed marker is on the path without your burn; a red one is below the surface |

## Screens

### Spawn form (`n`)

Choose a vessel, where to start (orbit or launchpad), which body, altitude,
inclination, and direction. `F1` here opens the SPAWN FORM help page and `F1` / `Esc` brings you back with the field and any typed number kept.

| Key | Action |
|---|---|
| `Tab` / `Shift+Tab` | Next / previous field |
| `←` / `→` | Change the field; on ALTITUDE and INCLINATION step to the next stop |
| `Enter` | Spawn the vessel; on ALTITUDE or INCLINATION the first `Enter` opens a box to type the number |
| `Enter` (box open) | Keep the typed number; the next `Enter` spawns (the form says so beside the number) |
| `Esc` (box open) | Put the old number back; `Esc` again leaves the form |
| `0-9` / `Backspace` | In the box: type / erase the number (INCLINATION also takes one decimal point) |
| `Esc` | Leave the form without spawning anything |
| `f` | Show every system's vessels / only this system's |
| `a` / `x` / `d` | Custom stack: add the picked module / remove the top stage / cycle the dock seam |

- **Vessels** are grouped by category and filtered to the current system's
  scale class by default; `f` inside the form toggles the filter to show every
  system's vessels. Pick **Custom…** to build a quick stack from whole
  modules, or one of your **saved designs** (listed after Custom…) from the
  [VAB](#vehicle-assembly-building-esc--build-vab).
- **Inclination** (orbit position only, a row under POSITION) is typed the
  same way as altitude, with a decimal point allowed: `Enter` opens the box,
  `Enter` keeps it, `Esc` discards. Outside the box `←` / `→` step 1°. It
  runs 0° to 180°; above 90° is retrograde. It starts at 51.6°, the default
  vessel's orbit and plane, so a default spawn is a partner you can rendezvous
  with without a plane change. Tab order is vessel, position, inclination,
  parent, altitude, direction.
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
| `c` | Refuses: clear-all is `Ctrl+K` now, not this key |
| `H` `I` `C` `K` `P` `R` | Run that one-key planner (see [Planning burns](#planning-burns)) right here, then return to the map. Only fires when a text field (Δv / throttle) doesn't have focus |

The **Plan Cursor** is the highlighted row in PLANNED NODES: `↑`/`↓` move it,
the header names which node it's on ("BURN PLAN — node 2 of 3"), and
PROJECTED ORBIT always shows the orbit *after that node*, never a leftover
draft from an earlier edit. The list ends in a blank **+ new node** row; with
the cursor there, PROJECTED ORBIT shows the form's own draft instead, labelled
as such. A node whose Δv is more than the vessel can currently afford still
plants (you may be about to refuel, stage, or dock a tug), but its row
carries a `⚠ exceeds budget by …` marker so it never comes as a surprise
(the map's ENGINE box shows the same thing as a bare `⚠` on its title row, beside the `+N [m]` queued count,
with the words in the F1 readout glossary).

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

### Rendezvous Planner (`K`)

A notice on the map, not a separate screen (it holds keyboard focus while
open, so it never shrinks or gets tucked away). It opens when `K`'s small
nudge isn't enough to close on your target (too far apart in phase) but your
planes already match. Opening it drops warp to 1x (the clock keeps running).
Pick the Rendezvous Orbit: their orbit, yours, or the crossing (you coast to
the point where the two orbits meet and burn there); then pick a lap count on
the **Lap Ladder** for that orbit, more laps for less Δv but a longer
wait. Every row shows its own `burn T-` countdown (about 5 minutes out by
default, so the row stays valid while you read it) and its `wait` from now;
Enter plants exactly the row you read, and `G` afterwards warps to the burn.
A row whose burn time passes before you press Enter refuses; press `K` again (it works inside the planner) to re-solve the rows from now. Unaffordable or unsafe rows still show, dimmed, with the reason,
rather than being hidden.

Round orbits of different sizes (say you at 500 km and your partner at 700 km) work too: one burn at the row's burn time puts you on a transfer that reaches their altitude, and the row times the rendezvous at that crossing, the lap count later. Raising burns prograde and lowering burns retrograde (lowering rows are often unsafe, since the transfer dips below your orbit). A stretched orbit on either side is refused with `circularize [C] first`. Orbits within about 0.01% of each other's radius count as the same size.

Once you plant a row, the TARGET box's last line reads the plan (`plan:` the arrival countdown, `miss:` the predicted separation) instead of the 4 hour closest-approach search, from planting until the planned arrival passes (also after the burn has fired).

| Key | Action |
|---|---|
| `←` / `→` | Walk the Rendezvous Orbit: their orbit / your orbit / the crossing |
| `↑` / `↓` | Walk the Lap Ladder |
| `Enter` | Plant the highlighted row's burn |
| `K` | Re-solve every row from now (after a row expired) |
| `Esc` | Close without planting |

While the picker is open it holds the plain arrows, so the flight trims and the pan are unavailable until you plant or press `Esc`.

"Your orbit" computes a plan for your *target*, not you, since this
session has no way to plant a node on someone else's vessel, planting there
is a later slice; the row still shows what that burn would cost.

### Vehicle Assembly Building (`Esc → Build (VAB)`)

Design a custom vehicle from fine parts and save it to launch later. You
compose **components** (engines, fuel tanks, command cores, antennas,
structure) into stages, stack the stages, and read a live **Δv / TWR / mass**
panel as you go. Saved designs are global across games and show up in the
spawn form (`n`) alongside the built-in vessels.

Under the stage list a **PICTURE** box draws the rocket you are building, with
the stage under the cursor marked (`▸ S2`). It is the same drawing the pad shows
in the launch view. A stage you built from parts is drawn too: its height and
width come from its mass (a heavier stage is a taller block) and its colour from
its fuel (kerolox terracotta, hydrolox ice blue, hypergolic khaki, solid mauve,
no propellant grey); two neighbours of one fuel are shaded apart. On a short
terminal the picture is dropped so the stage list and the key hints keep their
rows.

Editing happens **in place** on the vehicle rows, the same idiom as the
maneuver form. The VAB opens focused on the vehicle column with the cursor on
a fresh stage's `engine` placeholder, and `←` / `→` cycles that row's part.
You rarely need the palette: `←` / `→` to pick an engine, `↓` and `←` / `→`
for a tank, `+` / `-` for the count, done.

| Key | Action |
|---|---|
| `Tab` | Other column (palette ↔ vehicle) |
| `↑` / `↓` | Move the cursor in the active column |
| `PgUp` / `PgDn` | Jump section: previous / next tier or kind section in the palette, or previous / next stage in the vehicle column |
| `/` | Find: type part of a name (or an ID) and the palette narrows as you type; `Esc` clears it and puts the whole list back, `Enter` closes the find row and keeps the filter. While it is open every key types, so `?` and the global keys do not fire |
| `←` / `→` | Change part: swap the selected row's part within its kind (engines on an engine row, tanks on a tank row). On an empty stage the placeholder rows cycle from nothing through the catalog. No-op on a stage header or in the palette |
| `a` | Add the selected part, subassembly or component: a catalog part becomes a new whole stage directly above the cursor's stage and the cursor moves onto it; a subassembly lands whole with its marks (a payload block with a payload seam under it); a component joins the current stage |
| `n` | Start a new empty stage on top |
| `x` | Remove the component group under the cursor, or the whole stage when on a stage header or the stage is empty / a catalog part |
| `+` / `-` | Count: raise / lower the count of the component group under the cursor (`×N`) |
| `[` / `]` | Move the cursor's stage down / up in the stack |
| `y` | Copy stage: duplicate the stage under the cursor, marks included (not the booster mark, there is one booster group per vessel) |
| `Enter` | **Open part into pieces** (crack open): the catalog part under the cursor (a stage header) becomes editable components, no undo key, so you can start from an S-IVB or a Falcon booster and tweak it. A flash shows the Δv change; parts with no decomposition stay whole |
| `d` | **Payload from here up** (dock seam): toggle a seam below the stage. Everything above the seam becomes a nose payload released with `Y` (deploy, one at a time) or `U` (undock all) instead of staging; mark several seams for several payloads |
| `c` | **Drops with stage below** (fuse): toggle a fused decouple. The stage drops together with the group below it on one staging press. Refused on a booster stage (`boosters drop alone`) |
| `b` | **Lights with stage above**: toggle the booster mark. This stage lights together with the stage above it on `b` in flight and is the vessel's boosters; the header reads `· lights with S2`, and Σ Δv and TWR follow the pair. Refused with `nothing above` on the top stage, `one booster group` under another mark, and `boosters drop alone` on a stage in a `c` group. The mark saves with the design and with a subassembly |
| `t` | **Δv goal**: set a session **Σ Δv target**. The stats strip reads `current / target (delta)`, and with a tank row selected a hint shows how many of that tank close the gap (`+2 → Σ ≈ 9280 ✓`) or that it's unreachable |
| `s` | Name and save the design |
| `S` | Save from here up: the cursor's stage and everything above it, named, as a **subassembly**. The card shows the name, the role (`←` / `→` flips payload or stage; payload when the bottom stage was marked `d`) and the stages saved with their marks. It joins the palette's `· subassembly ·` section; `Comsat`, `LM` and `CSM+LM` ship there |
| `o` | Open a saved design, one of your subassemblies, or a built-in vessel to start from. The list has three groups, `YOUR DESIGNS`, `SUBASSEMBLIES` and `BUILT IN` (every catalog vessel, in catalog order), and scrolls with the cursor. `Enter` loads the highlighted row; a built-in opens unsaved (the design line reads `(unsaved)`, payload seams in place) and `s` saves it under a new name. `x` opens a card, `Delete design 'name'?` or `Delete subassembly 'name'?`, on `keep it` (`delete` asks once more before it acts); on a built-in it says `built-in vessels can't be deleted` |
| `Enter` in a box | Name box: keep the typed name and save. Δv goal box: keep the typed Σ Δv. S card: keep the typed name and save the subassembly; a name already taken asks first (`replace "name"?`), and asks once more. `Esc` cancels any of them |
| `Esc` | Back to the map |

Rules the VAB enforces:

- A stage holds **one fuel chemistry**. The engine row leads: cycling the
  engine with `←` / `→` can cross chemistries (the stage is briefly flagged),
  and the tank rows then cycle only the new chemistry, so one `←` / `→` per
  tank repairs the mix. Adding a mismatched part with `a` is rejected outright.
- **Cargo Hold** is a structure component (100 kg). A stage that holds one reads
  `· hold` on its header and `hold: yes` in INSPECT; a vessel with a hold
  anywhere in its stack can receive cargo handed over with `u`.
- Multiple engines in one stage combine honestly: thrust adds, effective Isp
  is the thrust-weighted blend.
- Soft warnings flag the usual snags (no engine, no command source, lift-off
  TWR below 1) but never block saving. A design with no command source gets a
  probe core on spawn.

Designs are stored as portable files under
`$XDG_CONFIG_HOME/terminal-space-program/designs/` (typically
`~/.config/terminal-space-program/designs/`); copy one into the sibling
`loadouts/` overlay dir to share it as a mod.

### Missions ladder (`M`)

The active mission (if any) shows as a checklist card on top; below it, two
headed lists, **Flight School** and **Challenges**, each with their own
progress count. A locked rung is dimmed with what unlocks it; an available
rung reads bright; completed and failed rungs are marked. Each program
carries a one-key row: `turn on` in place of its list while it's off,
`turn off` under its list while it's on. Flight School is on by default;
if you'd rather fly unguided, `M`, `1`, `Esc` switches it off for good
(the same toggle lives in Settings).

| Key | Action |
|---|---|
| `1` | Turn Flight School on / off |
| `2` | Turn the Challenge ladder on / off |
| `Esc` | Back to the map |

### Saves (`Esc → [Save Game]` / `[Load Game]`)

One browser for every save: your named saves plus the managed quicksave
(`F5`) and the three rotating autosaves, newest first, with when it was
saved, the in-game date, and the vessel you were flying. `[Save Game]` opens
it in save mode (with a `＋ New save…` row), `[Load Game]` in load mode.

| Key | Action |
|---|---|
| `↑` / `↓` | Move the save cursor (click a row to select it; click again to activate) |
| `Enter` | Load mode: load the save (a card asks first). Save mode: `＋ New save…` prompts for a name (prefilled with vessel + in-game day); an existing named save asks to overwrite (a card) |
| `Enter` in the name box | Keep the typed name and save; `Esc` cancels |
| A card | Opens on the way out (`cancel` / `keep it`): `↑`/`↓` + `Enter` pick a row, `y` the first, `n` or `Esc` the way out; an action row asks once more. A click on a row picks it |
| `d` | Delete the highlighted save (a card asks first; works on quicksave and autosaves too) |
| `r` | Rename a named save. Quicksave and autosave slots are managed and can't be renamed or overwritten by hand |
| `Esc` | Back |
