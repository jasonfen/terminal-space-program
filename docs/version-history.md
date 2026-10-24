# Version history

Newest first. One headline per release, then the concrete changes and the issues they closed.

### v0.60.3

Saves keep your view and tell you when they fail, docking with friends no longer misreports or retargets, the server holds up under awkward timing, and big fleets render faster (PRs `#775`-`#778`; tag `v0.60.3`).

- **Saves.** A loaded game keeps the default tilted view instead of going flat. A failed autosave flashes `autosave failed: <reason>` once, then stays quiet until a save succeeds (closes `#758`). A save written against another body catalog shows `unreadable (other catalog)` in the save browser, and loading one says `save: written against another body catalog` instead of printing two hashes (`#776`).
- **Docking with friends.** When the guest's `[U]` and the owner's `u` land at the same moment, the undock no longer shows a `release refused` chip afterwards. A guest undocking aims the stack at them, not whatever other vessel the owner is flying. The undock flash counts the pieces this undock produced, not every vessel in the game (closes `#709`; `#777`).
- **Server.** A second terminal connecting on the same new key is refused while the first is in use, instead of both getting in and the second save wiping the first. A shutdown or restart that begins while a player is still connecting no longer risks crashing the host. `--reset-fleet` clears dock records before wiping fleets, so a run that fails partway can't leave records pointing at wiped stacks (`#775`).
- **Big fleets render faster.** The orbit view keeps up to 256 cached orbit curves instead of 64; at 50 vessels a frame draws in about 1.3 ms instead of 3.7 ms (closes `#729`; `#778`).

### v0.60.2

A node planted on the pad waits until it can lift, throttled nodes are predicted as they fly, and two readouts fit their space (PRs `#772`-`#774`; tag `v0.60.2`).

- **A pad node below TWR 1 stays on the pad.** A planted node that fired on the pad lifted the vessel whatever its thrust. It now stays seated and burning until thrust beats weight, as a manual burn already did, then lifts and delivers the node's full Δv (closes `#734`).
- **Throttled nodes are predicted at their throttle.** The predicted orbit after a throttled-down node assumed full thrust, so at half throttle it missed the flown orbit by about 130 m/s. It now matches the burn the executor flies (closes `#691`).
- **PROPELLANT fits its box during a climb.** The `Δv→circ:` cell pushed the row to 57 cells. It drops the unit (the `Δv:` reading to its left already shows m/s) and reads `7502 3m20s` (closes `#746`).
- **The Centerline scope stops growing on very tall terminals.** It now takes the navball's width cap instead of running off the left edge. Orion's launch sprite is pinned in the sprite test (closes `#769`, `#770`).

### v0.60.1

Long vessels and Orion dock at the port gate instead of bumping apart (PR `#771`; tag `v0.60.1`).

- **The docking magnet no longer swings your port out of the gate.** It turned your nose about the bottom of the stack, so on Orion or a long stack (Apollo Stack, Falcon 9 + Dragon, SLS + Orion) the port drifted past 0.5 m mid-pull; the pull let go with the hulls touching and the pair bumped apart. The nose now turns about the port, and the gap only closes (`#771`).

### v0.60.0

SLS flies with Orion on top, and the Centerline view becomes a scope in the Proximity view's top-right corner (PRs `#762`-`#766`, `#768`; tag `v0.60.0`).

- **SLS + Orion.** A new `SLS + Orion` loadout stacks the boosters, core, ICPS, a European Service Module and the crew module, and an `Orion` loadout and `Orion` subassembly fly the capsule alone. The ICPS now carries the Orion stage adapter, so the bare `SLS Block 1`'s ICPS stage has 8,680 m/s, down from 9,501 (closes `#752`).
- **Centerline scope.** In the Proximity view the Centerline now sits as a framed scope in the top-right corner, and the side picture keeps its full width. Your port is a dotted crosshair, theirs a ring, and the disc ranges itself in width steps of 2 to 64 m, printed on its title row with the port range; `+` / `-` zoom the side picture only. Both hulls are drawn end-on as dotted circles, apart when you pass clear and overlapping when you would bump, and a leaning nose stretches yours into a stadium. Once you overshoot, the ring becomes `◌` behind your port and the title's range reads negative (`port -4.12 m`); the chip's `port:` row stays unsigned. `F2` declutter hides the scope with the navball (closes `#757`).

### v0.59.4

Control codes can no longer reach another player's screen, and failed saves and roster edits on the server say so instead of passing silently (PRs `#755`, `#756`; tag `v0.59.4`).

- **Pasted escape codes are stripped from shared text.** Handles, chat, and friends' vessel and port names drop non-printing characters before they are stored or shown, so a pasted `ESC[2J` can no longer clear everyone's screen. Handles already saved with them are cleaned on load without losing anyone's seat, and the handle limit is now 24 columns of display width (`#755`).
- **Server write failures are reported.** A failed disconnect save is logged and the player's exit line says `your latest progress was not saved`; a kick whose roster edit fails is refused with `kick of <name> failed: ...` instead of reporting success; revoke, promote, demote and the pre-restart save are logged and shown to the host the same way (`#756`).

### v0.59.3

Old saves show the current mission text and gates, and dock text names the vessel you are docked to (PRs `#753`, `#754`; tag `v0.59.3`).

- **Missions in old saves refresh on load.** Every Flight School and challenge mission now takes its wording, gates and fail rules from the current catalog, so a save from before the port-gate docking change shows the new hints. Progress carries over: a completed mission stays completed, each step keeps its status, and a step added since the save counts as done only if you already passed a later one (`#753`).
- **Dock text names the vessel.** The DOCKED chip reads `◇ docked to gern's Skylab` instead of `riding in gern's stack`, and the docked, undocked, reclaim and dock-lost rows follow suit. With no vessel name known it reads `gern's stack` (closes `#723`).

### v0.59.2

A vessel with no engine can no longer burn, and high warp on escape and atmospheric legs is much cheaper (PRs `#750`, `#751`; tag `v0.59.2`).

- **No engine, no burn.** A vessel with no thrust used to gain the full Δv of an impulsive node for free. A planted node now cancels with `<vessel>: node cancelled, no engine`, and execute-now flashes `<vessel>: no engine` (`#750`).
- **High warp on hyperbolic and atmospheric legs costs about 12x less per tick** (6.8 ms to 0.56 ms for one vessel at 100000x). The flight path is unchanged: the full body-position refresh still runs whenever the vessel could be crossing a sphere of influence (`#751`).

### v0.59.1

A node planted below full throttle now delivers all of its Δv (PR `#749`; tag `v0.59.1`).

- **A planned burn set below 100% throttle burns for as long as it needs.** Its window was sized for full throttle, so a 200 m/s burn at 50% stopped with about 100 m/s still owed. The planner's burn-time preview now shows the longer time too (closes `#748`).

### v0.59.0

The SLS lights its boosters with its core, and you can build your own vessels with boosters (PRs `#736`-`#742`, `#744`, `#745`, `#747`; ADR 0058; tag `v0.59.0`).

- **`b` lights the SLS's solid boosters and its core together.** The solids burn at full thrust until they're empty and can't be throttled or cut. `space` is refused while they burn (`boosters still burning`). Once they burn out (`boosters burned out`), `space` drops them as two vessels, `<vessel>-booster1` and `-booster2`.
- **Raster-LS works the same way.** Its core carries more propellant so that it outlasts its solids. Total Δv is about unchanged.
- **Liquid boosters get their own throttle.** `c` picks the target: all, core or boosters. The throttle keys and `x` move only the selected one, and the ENGINE row is labelled `core:` or `boosters:`. A vessel with one throttle answers `one throttle`.
- **Re-arm docking moves from `c` to `alt+u`.**
- **ENGINE, PROPELLANT and STAGES read the whole booster group.** ENGINE's title shows the boosters' clock or `DRY`, PROPELLANT shows both tanks (`60% · 91%`), and STAGES marks every lit stage `◉`.
- **In the VAB, `b` marks a stage to light with the stage above it.** The header shows ` · lights with S2`, Σ Δv and TWR follow the mark, and designs and subassemblies keep it.
- **On the pad, the boosters draw beside the core with their own flames.** The SLS core now stands taller than its boosters.
- **Saves move to schema 16.** Older saves load unchanged, and an older build refuses a v16 save.
- **A node burn on a vessel with lit solids hands the solids to a manual burn when the node is done.** The core cuts to 0% and the solids burn to empty, since solids can't be shut off.

### v0.58.1

Lighting a heavy stack on the pad no longer wrecks it, and saves survive a power cut (PRs `#732`, `#733`, `#735`; tag `v0.58.1`).

- **A vessel too heavy to lift off stays on the pad with its engine burning.** It lifts off as soon as thrust beats weight, either when you throttle up or once it has burned off enough propellant. It used to sink into the pad and count as a crash (closes `#716`).
- **A power cut or crash just after a save no longer leaves an empty or cut-off save file.** Saves, settings and the server's `session.json` are flushed to disk before they replace the old copy.
- **Linux packages ship with each release**: `.deb`, `.rpm`, `.apk` and Arch (`.pkg.tar.zst`) for amd64 and arm64, so Linux users don't need Homebrew. The README has install lines.

### v0.58.0

A server crash no longer duplicates or loses a docked vessel (PR `#724`; tag `v0.58.0`).

- **Docks survive a hard crash of the multiplayer host.** The server's dock record now repairs each player's saved game when it is next opened, so after any crash during a dock, undock or handover there is exactly one copy of every vessel. Nothing is shown; the usual dock chips say what changed.
- **The host comes back where they left off.** `--serve` resumes the host's newest autosave at start, so a stack the host owns, guest and all, survives a restart. `--reset-fleet` starts the host fresh as before.
- **While hosting, the host's game autosaves at least every 5 minutes**, like every guest's, even with autosave off. Solo play follows the setting.
- **Staging a stack down to a friend's docked vessel hands that vessel back to them.** It used to leave the dock stuck with undock refused.
- **`--reset-fleet` now clears old docks for good.** A fresh vessel could be mistaken for a stack from before the reset.
- The server's `session.json` moves to version 5; older builds refuse to start on it.

### v0.57.7

Multiplayer dock and restart fixes (PRs `#720`, `#721`; tag `v0.57.7`).

- **A server restart saves the host's own game.** The restart that follows every update used to lose everything the host had flown since the last autosave.
- **Leaving the server can no longer leave one stack with two pilots.** A player's game is now saved before they are shown as gone, so taking control of their empty seat sees their latest stack.
- **A dock can only capture a player who is still connected.** A player who had left could be caught in a dock with no way out.
- **Each player has at most one ride and one stack at a time.** A second dock used to make the dock keys act on either one at random.
- **Release binaries are built with Go 1.26.10**, which carries a crypto/x509 security fix (GO-2026-6809).

### v0.57.6

Three fixes for vessels in other systems, multiplayer reclaims and custom systems (PRs `#717`, `#718`, `#719`; tag `v0.57.6`).

- **A vessel in a system you are not viewing now sees its moons where they are.** It used to place them as if they belonged to the viewed system, so it could fly straight through a moon's sphere of influence.
- **Taking control of a stack that is still in transit to another player is refused** until it arrives. The reclaim used to lift an unrelated vessel from their save and lose the real stack.
- **A custom system file with no bodies is skipped at load, with a warning.** Spawning in one used to write a save that could never load again.

### v0.57.5

Two crashes and three burn and planner bugs fixed (PR `#715`; tag `v0.57.5`).

- **Attitude keys no longer crash the game when you have no vessel.** They now say there is no vessel to point.
- **alt+d and alt+delete no longer crash the game** in the planner's Δv and throttle boxes or the save-name box.
- **Transfers and rendezvous from orbits inclined past 90° take the short way.** The planner used to solve them the long way round, quoting thousands of m/s for a burn that should cost almost nothing.
- **Touching down or crashing ends the burn.** A burn that outlived the landing used to pin warp at 10× and block the next ignition.
- **Staging to a weaker engine mid-burn keeps the burn going** until its Δv is delivered, instead of ending it early and calling it complete.

### v0.57.4

A security update for multiplayer hosts (PR `#712`; tag `v0.57.4`).

- **The `--serve` host is patched against three SSH flaws.** Two let any client that could reach the port hang the server; the third skipped a source-address restriction for non-key logins. The SSH library is updated to the fixed release.
- **Building from source now needs Go 1.26 or newer.** Release binaries are unaffected.

### v0.57.3

A planted Moon transfer no longer costs a full CPU core (PR `#711`; issues `#673`, `#706`; tag `v0.57.3`).

- **The game idles cool with a Moon transfer planted.** The HUD's plan readouts re-walked the whole transfer every frame and recomputed every planet and moon's position at each step of it, about 19 ms a frame, so the game used a full CPU core while you waited for the burn. The walk now checks only the body you are near and its moons, and looks up everything else only when you actually cross into another sphere of influence: about 1.8 ms a frame (`#673`).
- **The simulation tests take under two minutes on CI**, down from about twelve, so fixes ship faster (`#706`).

### v0.57.2

Undocking after staging keeps every piece's parts, and Falcon 9 stages talk to the ground (PR `#708`; issues `#707`, `#683`; tag `v0.57.2`).

- **`U` after staging keeps each piece's real core and antenna.** A Falcon 9 carrying three comsats, staged off its first stage and undocked near the Moon, came apart with every piece stripped to the basic 10,000 km antenna and no relay, so all of them read `⚠ NO SIGNAL: needs a stronger antenna`. The vessel's record of its parts now shrinks with the stack whenever a bottom stage leaves (staging, a decouple group, a dropped Dragon trunk), and saves made before the fix are repaired on load. Each comsat keeps its relay antenna and the second stage reaches the ground through them (`#707`, `#683`).
- **Falcon 9 stages carry a flight computer and a near-Earth antenna**, as the real stages do. A bare first or second stage is controllable and reaches the ground near Earth, and needs a relay at the Moon. Their masses are unchanged: the published empty weights already include that hardware.

### v0.57.1

Moon transfers capture where the HUD says, and quitting asks the plain question (PRs `#700`, `#703`; issues `#699`, `#701`, `#702`; tag `v0.57.1`).

- **`H` to the Moon captures clear.** The mid-course plane change used to fire after you had already crossed into the Moon's sphere of influence, and it chased your velocity while it burned, so a 209 m/s turn delivered about 136 m/s: you hit the Moon or captured into an orbit whose low point was underground, and the capture fired after you had passed. Now `H` plans the plane change to finish about 15 minutes before the Moon's sphere of influence, and the flight flies the encounter the HUD shows (`#701`).
- **Sideways burns hold their direction.** A planted burn with a normal or radial part (a plane change, a radial correction, yours or `H`'s) holds the direction it was planned in for its whole length, as Apollo's planned burns did. Prograde and retrograde burns still follow your velocity as they turn, which keeps long departures efficient. The predictions model the same rule (`#702`).
- **Quitting asks "Save before quitting?"** with Yes / No / Back. Yes saves and quits; No asks "Are you sure?" with No selected, where Yes quits without saving and No goes back; Back returns to where you opened it. A multiplayer guest, whose flight saves automatically, keeps the single Quit? card (`#699`).

### v0.57.0

Every question is a card, the VAB builds anything, docking gets a Centerline view, SpaceX Dragon flies with cargo, and slow-engine Moon transfers arrive (ADRs 0054-0057; PRs `#657`-`#665`, `#669`-`#672`, `#674`-`#682`, `#697`, `#698`; issues `#611`, `#616`, `#633`, `#645`, `#648`, `#655`, `#656`, `#666`, `#667`, `#685`-`#689`; two batch reviews in the planning vault, 1 blocking and 1 should-fix finding fixed before release; save schema 15; tag `v0.57.0`).

- **Every question is a card.** The quit prompt, `E` end flight, `D` transpose, `Y` deploy, `F9` quickload, the saves screen's load / delete / overwrite and the VAB's delete design all ask on a centred card instead of a `[y/n]` line. `↑`/`↓` move the `▸`, `Enter` picks, a click picks a row. Every action asks once more on the same card before it acts: `y` confirms, `n` or `Esc` is the way out (to quit without saving: `n`, then `y`). Cards that destroy or discard data open on the way out (`#633`).
- **The roster drops a friend.** `d` disconnects (they can reconnect), `x` kicks (a new invite to return), and the kick card's `kick and delete vessels` backs up their save byte for byte, deletes it, hands any friend's vessel docked to them back, and leaves their vessel in someone else's stack with that stack's owner. Stop hosting, restart, the host quitting and SIGTERM now drop every guest cleanly with a line saying why; chips read `was kicked by` / `was disconnected by`, and a kicked player's ghost keeps its name (`#616`).
- **The VAB builds anything.** Every catalog part sits in tier sections, the list fills its box, `PgUp`/`PgDn` step sections and `/` finds a part by name. `a` inserts above the cursor's stage, `y` keeps a stage's marks, per-stage Δv stops at a payload seam and Σ names the `(carrier)`. Subassemblies: the Comsat, LM and CSM+LM blocks, `S` saves your own from the cursor up, and a deployed payload is named after its block (`Comsat-1`). A `Docking Port` component puts a port on any stage, and `o` lists `BUILT IN` vessels to start from (they open unsaved and can't be deleted) (`#645`).
- **Saves keep ports per stage (schema 15).** Older saves and vessels parked on a server mid-handover are backfilled from the catalog on load, so nothing loses its port. A vessel that never launched shows no `downrange:` (`#611`).
- **Docking: the Centerline view.** With a port to dock to, Proximity splits: the side view (now with a dotted corridor out along their port axis) and the Centerline view down your own port: your port is the crosshair, theirs the ring, and the standoff cross inside the ring reaches its edge at the 4° gate. The chip reads `misalign:`, `lateral:` and a new `hold:` row; the navball reads `pitch` / `yaw` in PORT. `;` into PORT turns nothing by itself on any vessel (`q` with the main engine holds `Port+`). In PORT the arrows tip the nose 1° across the port face (`alt` 0.25°) and `trim:` reads `(port)` (`#655`).
- **Docking speed is read at the ports.** `rel speed:`, `closing:` and the dock gate include each stack's swing about its tail, so two parked Apollo stacks read about 0.12 m/s and must null it to dock, and a nose still slewing is not dock-ready.
- **SpaceX Dragon, crew and cargo.** Four `Mission Stacks` vessels with the trunk as a stage under the capsule (`space` drops it first); an engineless bottom stage passes thrust up to the first engine above it. Dragon's antenna reaches from anywhere to the Moon, and crewed craft now link through their built-in antenna too (`#656`).
- **Cargo.** A `Cargo Hold` component lets a vessel receive cargo; docked, `u` hands it over (either seat in a cross-player dock), mass moves with it, and PROPELLANT reads `cargo:` / `hold:` on every vessel.
- **Slow-engine Moon transfers arrive.** `H` plans a long burn as a finite burn instead of falling back to the instant-burn figure: the Comsat Carrier's 10-minute departure now reaches the Moon (it missed by 152,000 km). The HUD claims a Moon encounter only when the path reaches the Moon (`#666`, `#667`).
- **ENGINE's TWR row shows the figures only**, no `will not lift` verdict (`#648`).

### v0.56.1

A planned burn's path stays around its planet (PR `#654`; issue `#653`; tag `v0.56.1`).

- **No more wavy line trailing off the orbit map.** A planted burn's path was drawn with the planet's own motion around the Sun mixed in, about 160 Mm per low-orbit lap, so a multi-lap plan (a rendezvous phasing coast, for one) smeared into a helix that read as a dotted wave running off to one side. Every planned leg inside the planet's sphere of influence is now drawn relative to the planet, like the SOI pass arc already was. The burn itself and its numbers were never affected (`#653`).

### v0.56.0

Docking is flown, port to port, you start in the Apollo TLI stack, and the launch sky fills the screen (ADR 0053; PRs `#640`, `#641`, `#642`, `#643`, `#644`, `#646`, `#647`, `#649`, `#650`; issues `#604`, `#639`; cycle review in the planning vault, 2 blocking, 2 should-fix and 2 low findings fixed before release; tag `v0.56.0`).

- **You start in `Apollo TLI-1`.** A new game, `--loadout` with no value and the `--reset-fleet` vessel are the S-IVB with the Lunar Module and the CSM still on top, the way it sits after trans-lunar injection. Its port is the Command Module's nose. `space` drops the S-IVB, `D` then does the Apollo transposition. The bare `S-IVB-1` stays in the spawn form and on `--loadout`. The start vessel is 126.9 t and its S-IVB carries the Apollo post-insertion propellant (75 t, not the bare stage's 40 t), so the stage alone has 3690 m/s: the 3.1 km/s Moon burn fits with about 600 m/s to spare, and the capture burn at the Moon wants the SM (stage with `space` when the S-IVB runs dry). The first-flight and Moon challenges plan affordably from the start.
- **Nothing docks by itself any more.** The 50 m / 0.1 m/s click is gone. A dock needs your port within 0.5 m of theirs, under 0.1 m/s relative, and the two noses within 4° of facing. Then a five-second magnet pulls you the last half metre at 1× warp (your RCS and attitude keys are ignored; it lets go if the other vessel leaves the gate). The composite stays where the lead vessel's hull was, with the partner drawn nose to the seam.
- **Ports.** Crewed modules and capsules, ascent stages, tugs, keepers, relays, stations and the Probe Sat carry one, on the nose; launch vehicles, bare upper stages, comsats, relay nodes and science probes do not and cannot dock.
- **The Proximity view shows them.** A `○` on each hull, a dashed port line that goes solid green when the gate holds, and `port:`, `align:` and `off-axis:` rows with `(need < ...)` reminders. A port-less target reads `no port`.
- **`;` reaches `[PORT]`** when the target has a port: `q` / `e` push in / out along its axis, `w` / `s` up / down and `a` / `d` left / right across its face (RCS armed); the main-engine `q` hold, `Port+`, puts your nose on the port axis. `speed:` reads `(port)` there.
- **A bad approach bumps.** Hulls that touch outside the gate bounce apart at half the closing speed with `bumped <vessel> at <speed>; re-approach under 0.10 m/s` on the bottom edge. Within 50 m of another vessel's hull (measured hull to hull, so a long stack counts from its nose) warp is held to 10×.
- **`U` parts a dock along its seam:** pieces come back where their hulls were, port to port a row or two apart (1.5 to 3 m, by the stages at the seam), opening at 0.15 m/s (was 75 m apart). The re-arm hold still keeps them from re-docking until 100 m or ten minutes, or `c`.
- **Alongside spawns 25 m port to port** ahead of your nose, outside the gate, instead of fusing 0.6 s after Enter. The spawn form row says `alongside (25 m port to port)`.
- **`K` refuses on the port gate.** It said `already in DOCK READY range` inside 50 m at under 0.1 m/s; it now refuses only when the dock gate itself holds (`docking gate holds, let the magnet close it`) and otherwise still answers inside 50 m.
- **The launch sky fills the screen.** On the pad the sky runs from haze blue at the horizon to deep blue overhead and fades to black as you climb out of the air; the ground runs to the bottom of the screen until the horizon drops out of view. Every line, the rocket and the dashed climb path draw on top of it (`#639`).
- **Vessels loaded from a save draw as their rocket again and have their port.** Saves never kept a stage's picture size, so a loaded vessel had a zero-length hull: no port, and no stack picture.
- **F1 and the controls guide:** a new DOCKING page (who has a port, the gate, the magnet, the bump, the PORT frame, `p`, `U`, `c`), the warp caps, the `o`, `U` and `c` rows, `(ORBIT/SURF/TGT)` and `speed` naming PORT.
- **Docking to a friend is flown the same way.** You are the approacher: their hull and port are drawn from what they last reported, the same gate and five-second magnet apply (it moves only you; they are not asked), and the dock is filed with the host when the pull completes. A friend with no reported hull reads `port: unknown` and cannot be captured; a bump off their hull moves only you. Target your friend before you close in: the player doing the approach is the one pulled in, and the other player's vessel rides in the approacher's stack without moving.

### v0.55.0

The VAB draws your rocket, and tidier readouts (PRs `#638`, `#636`; review in the planning vault, 1 medium and 2 low findings fixed before merge; tag `v0.55.0`).

- **The VAB draws your rocket.** A PICTURE box under the stage list draws the stack with each stage labelled by its fuel and the focused stage marked. Stages built from parts are drawn now, in the VAB and on the pad, as a block sized from their mass in their fuel's colour, instead of vanishing (`#596`).
- **Spawn form readouts:** catalog masses in tonnes (`181.0 t`), and PARENT BODY shows the body's gravity and radius on their own line instead of `μ 3.99e+14`.
- **`t` picks the nearest vessel first**, so a vessel 2 km away comes before one 12 km away.
- **F1 explains GUIDANCE's amber frame word** (a hold kept in the frame it was pressed in), and seven help pages no longer repeat their title.

### v0.54.0

Planning that refuses nonsense, help everywhere, and a clearer map: UX cycle 3 Phase 4, wave 2 (PRs `#621`-`#628`; review `#629` fixed in `#631`, `#632`; tag `v0.54.0`).

- **One transfer at a time.** A second `H` while a burn is planted refuses and names it (`transfer: a burn is already planted (...); clear it in [m] first`). Clearing the planner stops a burn that is already firing (`burn stopped: ...`). `H` at another planet's moon refuses and names the planet to target instead (`#587`, `#588`, `#610`).
- **F1 opens on every screen** at that screen's own help page and puts you back exactly where you were, half-typed numbers included. New SPAWN FORM page; F1 says what Enter does on every screen (`#589`).
- **Keys that mean what they say.** Enter never answers a [y/n] question (only `y` is yes), so Enter, Enter can no longer delete a save. The roster marks the keys that ask first; deleting a saved VAB design asks; the VAB key list is in plain words; the spawn form's bottom edge says what Enter and Esc do right now (`#589`).
- **`speed:` names its frame** (`0.00 m/s (surface)` on the pad, `(orbit)` above the air), and the navball frame switches itself between surface and orbit on the way up and down. A held direction keeps its frame and GUIDANCE marks it when it differs (`#601`).
- **A finished rung says so:** the MISSION box keeps a `✓ done: <rung>` row that flashes green when it completes (`#594`).
- **A scale bar on every view:** Proximity, every map projection and the launch view (`#602`).
- **Small bodies and lines over planets.** Every body draws at least a small disk and never smaller than its true size; markers step off small bodies instead of hiding them; small bodies are lit; night sides are brighter; your orbit crosses a planet without the dark border (`#591`, `#580`).
- **Body info and map greys:** apsides on the distance ladder, periapsis / apoapsis for moons, real gravity where the catalog had none, `Sidereal`; planet and moon orbits a lighter grey (`#617`, `#593`).

### v0.53.0

Clearer screens and honest numbers: UX cycle 3 Phase 4, wave 1 (PRs `#605`-`#609`, `#612`, `#613`; review `#614` fixed in `#619`, `#620`; tag `v0.53.0`).

- **The burn planner says what it is doing.** The Δv and throttle rows show focus like the other rows; the footer names only keys that act on the focused row; `next peri` / `next apo` read `(undefined)` on a circular orbit, and a burn planted there waits instead of firing at an arbitrary moment; the PROJECTED ORBIT canvas draws the plan under the cursor, not the draft (`#585`).
- **Numbers that mean something.** A porkchop where nothing solves says `no transfer solves in this window` instead of printing Inf (`#586`). Body info prints mass as `5.972 × 10²⁴ kg`, radius and semimajor axis on the distance ladder, and age as `4.600 Gyr`, with no `e+` (`#598`).
- **The spawn catalog reads as a table:** name, crew, dry mass, fuel and engine each in one column, the description in Dim (`#595`).
- **The VAB palette names the fuel chemistry** on every fuelled row, not by colour alone (`#597`).
- **F1 has a map marker key** (vessel, Target, burns, apsides, nodes, closest approach, sphere-of-influence entry and exit, impact) and explains the navball's `[MAN]` / `[AUT]` and `RCS` tags; QWERTZ wording now says only letters move. Planned burns get their own periwinkle instead of the interface cyan (`#590`, `#592`).
- **STAGES pips carry state:** `◉` the lit stage, `●` fuelled and waiting, `○` spent (the amber of ENGINE's `✕ DRY`), `◇` no tank (`#600`).
- **The key legend survives a flash** on every form: a status flash lands on its own row under the legend (`#599`).

### v0.52.1

Review fixes for v0.51.2-v0.52.0 (PRs `#583`, `#584`; review `#582`; tag `v0.52.1`).

- **The dotted far side of your orbit no longer cuts notches** where it crosses the atmosphere halo, another planet or a moon's orbit line. It now yields like every other dotted line, as v0.51.4 intended.
- **`G` or `[»Burn]` in the last 30 s before a launch window** turns your heading to the window's and says so (`heading 045° for the launch window`), instead of refusing with "no burn planned to warp to". Handy after `|` has cleared the heading `G` set.
- Tests now pin the launch-window warp handing back 1x from any warp rate, the window winning over another vessel's planted burn on the pad, and the planner, halo and moon-orbit lines drawing over planets.

### v0.52.0

A key to undo a pan (PR `#581`; tag `v0.52.0`).

- **`g` snaps a pan back** onto what you're following. Focus and zoom stay as they are. Before, `g` jumped the camera to the whole system and cleared the pan only as a side effect (and not at all when you were already on the whole system).
- **`alt+g` is the whole-system view** (the old `g`). On a Mac, alt is Option: Terminal.app and iTerm2 need Option sent as Alt, as for the other alt keys.

### v0.51.4

Readouts that match the flight, and no wavy line across the planet (PRs `#577`-`#579`; tag `v0.51.4`).

- **`Δv→circ` shows the burn time you actually fly.** It assumed the vessel's mass stays constant through the burn and read long (1m11s for an S-IVB burn that flew 1m01s); it now uses the same rocket-equation time the `C` burn plants.
- **`fuel:` reads as a percentage only** (`100%`, not `100% (2160 t)`); `mass:` beside it still gives the weight.
- **Dotted and dashed lines yield over planets again.** v0.51.3 let them win over a planet's face with a dark moat, which turned the CommNet beam into a wavy teal line across the planet. Solid orbits, vessels and markers still show across planets.
- **The atmosphere halo** no longer cuts a dark gap into the planet's edge.
- Known: solid orbit lines over a planet still cut a blocky dark trench; tracked in `#580`.

### v0.51.3

Orbits you can see against a planet, and an orbit that reads as yours (PR `#576`; tag `v0.51.3`).

- **Orbit lines show across planets** in every view. Each terminal cell takes one colour, and a planet's disk used to outvote a line crossing it, so a targeted vessel's green orbit vanished over Earth. Lines, vessels and markers now win the cells they share with a planet, and sit in a one-dot dark channel, so they read over a body of their own colour (your yellow over Io, green over Kern or Earth's land).
- **Your orbit is your vessel's yellow** (`#FFD93D`) on the map, the launch view and the maneuver screen, instead of a pale slate that read as Earth's atmosphere edge at a low orbit. Yellow is you, green is your target.
- **Launch view:** an orbit's far side hides behind the ground at the planet's true size.
- The map renders a little faster than before (about 6% on an idle frame).

### v0.51.2

Auto-Warp takes you to the launch window, pointed the right way (PRs `#571`-`#575`; tag `v0.51.2`).

- **`G` on the pad warps to the launch window.** With a target, `G` (or `[»Burn]`) warps to 30 s before the window the `plan:` row names (the best moment when there is no exact pass), ramps down and hands back 1x, and turns your heading to the one the row names, so you only light the engine. `↑` / `↓` during the warp are yours.
- **`←` / `→` lean along your heading.** `→` tips the nose toward your commanded heading and `←` back from it. Before, once you had trimmed off due east, `←` tipped the same way as `→`.
- **`trim:` reads up / down.** GUIDANCE and the Proximity view say `15° down` / `15° up` against the hold instead of `15° E` / `15° W`: `→` lowers a nose facing downrange (the pad, a prograde climb) and raises one facing back (a retrograde hold).
- **`;` cycles the nav mode** Surface, Orbit, Target.
- Clicking `[»Burn]` now counts as using Auto-Warp for mission objectives, as `G` does.

### v0.51.1

The Proximity view says what your engines are doing (PRs `#569`, `#570`; tag `v0.51.1`).

- **PROXIMITY box, three new rows.** `throttle:` the main engine's state as in the ENGINE box (`100% idle`, `● FIRING` with its clock, `✕ DRY`) and the Δv it has left; `rcs:` whether RCS is on and the pulse step each translation key fires (`on  pulse 0.01 m/s`, or `off  [r] on, pulse 0.1 m/s`); `monoprop:` the tank and the RCS Δv it buys. Before, RCS showed only as the colour of the navball's button, and the pulse step (`p`) was shown nowhere.

### v0.51.0

A starting orbit you can launch to, and a Target you can see on the way up (PRs `#567`, `#568`; tag `v0.51.0`).

- **The default vessel orbits at 51.6°** (ISS-like, same 500 km circle) instead of over the equator. From the KSC pad (28.6° N) that plane is reachable: targeting it, the pad's `plan:` row counts down to the launch window and names the heading (about 045°), and the planes match at the pass. Before, matching the equatorial default cost about 3.8 km/s of plane change after launch (#566).
- **Spawn form: an inclination row** for "in orbit" spawns, typed like the altitude, 0° to 180° (above 90° is retrograde), default 51.6° in the same plane as the starting vessel, so a default docking partner needs no plane change.
- **Moon transfer from the new orbit:** the `[H]` burn waits whole parking orbits for the departure that lines the arrival up; a new game opens with it minutes away rather than about 4 h, and the flown arrival clears the Moon's surface.
- **Launch view: your Target in green.** The targeted vessel's glyph and its orbit draw in TARGET green, as on the map; before, the target showed as a plain glyph and its orbit wasn't drawn.
- **Multiplayer:** a joining player could land days ahead of a young group's clock; join now lines up in both directions.

### v0.50.3

A quit card, and F1 that says where the alt keys work (PRs `#563`-`#565`; tag `v0.50.3`).

- **Quit card.** ctrl+c, or q on the pause card, opens a centred **Save before quitting?** card over the screen: **Save and quit** (`y`), **Quit without saving** (`n`), **Stay** (`esc`). `↑` / `↓` + `enter` pick; the letter keys still answer directly. A multiplayer guest sees **Quit** and **Stay** only, since their flight saves automatically.
- **alt keys in F1.** alt is the Alt key on Windows and Linux (WezTerm and most terminals send it as-is) and Option on a Mac. Ghostty on a Mac works as-is; Terminal.app and iTerm2 need Option sent as Meta.
- Includes the v0.50.2 fine throttle on option+z / option+x (`alt+z` / `alt+x`, no shift).

### v0.50.2

The fine throttle uses one modifier, like the fine nose trims (PR `#562`; tag `v0.50.2`).

- **option+z / option+x** (`alt+z` / `alt+x`, no shift) step the throttle 1%, matching the `alt+arrow` 1° nose trims. option+shift+z / option+shift+x still work. `Z` / `X` stay 10%; `z` / `x` stay full and cut.

### v0.50.1

Shifted keys work in every terminal (PR `#561`; tag `v0.50.1`).

- **`|`, `Z`, `X` and other shifted keys** did nothing when the terminal reported modified keys in its extended form (xterm modifyOtherKeys or the kitty keyboard protocol, which another program can switch on in a Ghostty tab). The game now reads those forms, so `|` resets both trims and `Z` / `X` step the throttle again, locally and over SSH.

### v0.50.0

One look across the whole game, and a pause menu that pauses (UX cycle 3 slice B11, tracking `#512`; PRs `#553`, `#554`, review fixes `#556`-`#560`; tag `v0.50.0`).

- **One top row everywhere.** Every screen reads `Terminal Space Program` with the version, then the screen and what it is about, the clock and warp in the same column, and the way out on the right: `[»Burn] [Menu] [Missions]` in flight (the launch view too), `[Back]` everywhere else. While paused it reads `warp 1x  PAUSED`. A long row shortens its left side instead of pushing the buttons off screen (#508).
- **Back goes where you came from.** Settings, keyboard layout, saves, help and the VAB opened from the menu return to the menu; a second esc flies.
- **Forms look like the flight screen.** Settings, spawn, saves, missions, the VAB, help, body info and the session screen are titled boxes inside the same frame, with their keys on the bottom edge where flashes land. One cursor `▸`, one "left/right changes this" mark `‹ value ›`, and `➤` only means a vessel.
- **The pause menu pauses.** It is a card over the dimmed map and the clock really stops. A partner's Rendezvous Warp waits until you close it. Quitting from it still autosaves, and saves made from it load the way you were flying.
- **Which way am I looking?** Above `view:` the map says where north points (`N ↑`, `N ⊙` toward you, `N ⊗` away) and how open your orbit ring looks (`plane ◠ 67° open`: 0° is a flat line, 90° a full circle).
- **Fixes.** The maneuver planner's title row shows (it was always one row off screen), the porkchop's `[Back]` fits at 140 columns, spawn rows keep their engine figures, and settings descriptions wrap instead of cutting off.

### v0.49.0

A pad that tells you when to launch, a wide map with names on it, and a navball you can read numbers off (UX cycle 3 wave C, tracking `#512`; PRs `#545`-`#547`, review fixes `#549`-`#552`; tag `v0.49.0`).

- **Launch window on the pad.** With a target set, NAVIGATION's `plan:` row names the next time the pad passes under the target's plane, the heading to fly and, for a vessel, the lead it will have: `window T-11h03m at 045°, lead +25°`. When the pad never passes under it, the row gives the closest approach instead: `window best 9.17° T-2h46m`. TARGET's `lead:` reads live on the pad. Nothing target-shaped draws on the ground any more, and body targets get ◇/◆ plane crossings in orbit (#460).
- **Fine trims.** Hold alt (Option on a Mac, with Option as Meta on) for 1° steps: alt+↑/↓ heading, alt+←/→ pitch (alt+b/f too, which is what Terminal.app and Ghostty send), alt+Z/X throttle 1%. F1 and the controls page say how to turn Option as Meta on.
- **Trim reads as a lean.** GUIDANCE's `trim:` shows which way the nose leans, `15° E` / `5° W`, instead of a signed number.
- **The wide map has names.** At `g` the star and planets are labelled (moons never at that zoom), your Target at every zoom, and a name is dropped rather than drawn over anything. The Sun draws at its true size with no corona over the inner orbits, planet orbits are closer-dotted, and planets keep their own glyph instead of a moon's (#506).
- **Navball.** Numbered 10° pitch rungs, a continuous horizon line, `pitch` / `hdg` of where the nose points now beside `⊕ PRO`, hold markers at the untrimmed direction so the trim shows as a gap, and a pad compass that follows your commanded heading (#507).
- **`I` on the pad** says what to do instead of a garbled refusal.

### v0.48.0

Same keys in every view, a help screen that starts somewhere, descents that tell the truth, and a Rendezvous Planner that plants the row you read and flies where it said (UX cycle 3 wave B, tracking `#512`; PRs `#531`-`#534`, `#536`, `#538`, review fixes `#539`-`#541`; tag `v0.48.0`).

- **Keys.** The arrow keys trim the nose in every flight view (←/→ pitch west/east, ↑/↓ heading north/south); the map pans with shift+arrows; the 3D view tilts with `<` `>` and yaws with `{` `}`. The pause menu takes ↑/↓ + enter, and Keyboard layout moved from `c` to `k`. `D` (transpose) and `Y` (deploy) ask `[y/n]` first. The STAGES row names what `space` will drop (#493).
- **Help.** F1 opens on an index: "Your first flight" first, then every section with a note on when you would want it; `1`-`9` jump to a page (#494).
- **Body info acts on the body it shows.** `t` targets that body, then `H` plans a transfer and `P` opens its porkchop (#495).
- **Descent.** When the lit stage runs dry with the throttle set, the throttle row shows `✕ DRY` and `space` relights the next engine at that throttle. The stop margin names the stage it judges, and green now means that stage can land you, not just stop you (#465, #466).
- **Rendezvous Planner.** Rows aim about five minutes ahead and each shows its own `burn T-…`; opening the planner drops warp to 1x and Enter plants exactly the row you read (`K` re-solves if it has passed). "The crossing" now works. Vessels in different-sized round orbits get rows instead of a dead end, and different shapes point you at `[C]`. The TARGET box shows the plan's arrival and miss until you change course (#416, #418, #407).
- **Burns deliver what they planned.** A planted burn used to over-deliver up to one tick of Δv (about 1 m/s at 1x, 10 m/s at 10x), which sent rendezvous plans hundreds of kilometres wide. Flown rendezvous rows now land within a few hundred metres of plan, and under 2 km in the worst case measured (#537).
- **Saves move to schema 14.** Older saves migrate on load with nothing lost.

### v0.47.0

Which plane: a default Lumen launch starts on Kern's equator, and the Kepler-452 and TRAPPIST-1 planets now orbit in their star's plane (UX cycle 3 wave B slice B5, tracking `#512`; PR `#535`; tag `v0.47.0`).

- **Every existing save is rejected.** The body catalog changed, so a save made before this version will not load ("created under a different body catalog"). Start a new game. On the multiplayer server every fleet was reset.
- **Lumen launches equatorial.** The spawn form's LAUNCHPAD default (and a bare `--launchpad`) in Lumen is now the Equator instead of KSC's 28.6° N, so a plain ascent from Kern reaches Cursor flat instead of in a 32° tilted orbit. Sol and the exoplanet systems keep KSC (#461).
- **Exoplanets are coplanar.** The 11 Kepler-452 and TRAPPIST-1 planets had their sky-plane inclinations (about 89°) read as orbital tilts; they are now 0°. The `depart:` row on an exoplanet pad no longer shows a sweep that never changed, and hides like any other dead row (#497).

### v0.46.0

A UX pass: the screens say what they mean, the VAB reads like the rest of the game, `P` plots to your Target, shed stages stay shed, and Flight School teaches the right key to lift off (UX cycle 3 wave A, tracking `#512`; PRs `#513`-`#518`, `#520`-`#525`, review fixes `#526`-`#530`; tag `v0.46.0`).

- **Readouts that stop when their reason is gone.** A crashed vessel shows no SOI PASS, and `V` out of the launch view says which view you are back on instead of `ORBIT READY` (#464, #468).
- **Title bar and help.** The clock and warp rate read at full brightness, the warp field keeps a fixed width so nothing slides as warp changes, F1's "more below" cue is visible, and F1's `F2` row names what declutter hides (#498, #499, #420).
- **The VAB.** Parts and the inspector are no longer drawn in the disabled grey, build-blocking warnings are amber, the inspector follows the focused column and is a whole box, `s` shows the vehicle you are naming, `esc` returns to the pause menu, and a tall stack scrolls with a stage header always on top (#500, #501).
- **`P` plots to your Target.** With a planet targeted, `P` opens its porkchop; no target, a vessel target or a body in your own system each refuse in one phrase (#502).
- **Saves and spawn rows.** The in-game column shows date and time, autosave numbers follow the list order, quicksave names the file, the system's Δv-to-orbit moves to the spawn list header, and saved designs show mass and thrust (#503, #504).
- **Planner and mission polish.** A fresh plan reads `fire at: now`, the objective counter sits on the mission title, and view names are title-cased (#505).
- **Shed stages stay shed.** Jettisoned stages never dock with each other, after any warp and after a save/load (#467).
- **Flight School lift-off.** The rung says `[b]` to light the engine (space used to drop the fuelled first stage on the pad), and a new rung teaches `space` when the stage runs dry (#519).
- **Rendezvous.** Two vessels phasing on the same eccentric orbit get real Rendezvous Planner rows (#413); in multiplayer, a peer's clock is read current, and its position is propagated from the moment it was reported (#417).
- **Hosting.** `serve promote <handle>`, `serve demote <handle>` and a read-only `serve roster`; the session store is locked across processes so a CLI change cannot lose a running server's write (#242).
- **Saves move to schema 13.** Older saves migrate on load; an existing Flight School: Off the Pad mission is rebuilt with the corrected rungs, keeping the ones you passed.

### v0.45.1

Rendezvous is the word again: the planner, its HUD row, its messages and the docs no longer say "meeting" anywhere (PRs `#491`, `#492`; tag `v0.45.1`).

- **What changed on screen.** The plan row reads `rendezvous: their orbit, 3 laps`, a planted plan flashes `rendezvous burn planted: …`, and the planner's refusals and Lap Ladder reasons read `rendezvous: …` / `no rendezvous solution`. The picker's three choices (their orbit, your orbit, the crossing) are now called the **Rendezvous Orbit**, and the burn it plants the **Rendezvous Burn**.
- **Saves move to schema 12.** Older saves migrate on load with nothing lost, including a planted rendezvous burn and progress on either docking mission.
- **Multiplayer.** The plan a player agreed to travels under new names; a build from before this one is still understood for one release.
- **The two docking missions are renamed.** The Flight School rung is now `Flight School: Docking` and the challenge `Dock Two Vessels`; `w` on a roster row now reads "agree to rendezvous with them".

### v0.45.0

The instrument boxes line up and hold their width for the whole flight, a taller terminal draws the planet and the navball bigger, and a new Settings line decides what empty readings do (closes #482; PRs `#483`, `#484`, `#485`, `#486`, review fixes `#487`, `#488`, `#489`, `#490`; tag `v0.45.0`).

- **Two straight edges down the left, one on the right.** ENGINE, PROPELLANT and GUIDANCE share one width (56), COMMS, STAGES and MISSION a narrower one (46), NAVIGATION and TARGET one more (64). Planting a node, pressing `t` or starting a burn changes numbers, never outlines. A guard test re-derives every width from the readings each box can draw, so a longer reading fails a test rather than poking past the edge.
- **ENGINE's node row is shorter.** It reads `▸ in 12m04s  Retrograde  3120 m/s`: `ignition in` is now `in`, `#1` is gone, `next closest approach` is `next approach`, and Surface and Target modes read `Surf` and `Tgt` on this row only. The over-budget `⚠` and the `+2 [m]` queued count moved up beside the ENGINE title.
- **NO SIGNAL names the fix.** An uncrewed vessel out of contact reads `⚠ NO SIGNAL: needs a relay` or `needs a stronger antenna`. STAGES shortens a long stage name with `…` on big VAB builds, and a mission failure message wraps instead of widening MISSION.
- **Bigger on a taller terminal.** Earth, and any planet with moons, draws larger as the terminal gets taller (about 1.25x at 181x49); the Moon already did. The navball grows with height too (30x15 disk at 181x49), stays bottom-right, and never reaches the rows NAVIGATION and TARGET can use. At 140x40 both are exactly as before.
- **Settings: Empty readings, Full / Tidy / Compact.** Tidy (the default) is the v0.44.3 screen plus NAVIGATION folding its empty `impact:` / `plan:` rows while nothing is targeted. Full always draws every box and row, TARGET included. Compact also drops every box's trailing empty rows. Rows only ever fold from the bottom of a box, and widths are the same under all three. The Settings screen now shows every row at 140x40 without scrolling.
- No save-schema change; the setting lives in `settings.json`.

### v0.44.3

With nothing targeted, the TARGET box is gone rather than sitting there full of dashes, and the map has those rows back (closes #480; PR `#481`; tag `v0.44.3`).

- **No target, no box.** TARGET used to draw its title and four rows of `—` whenever you were not chasing anything. Press `t` and it appears where it always sits, directly under NAVIGATION; clear the target and it goes again. Worth 7 rows and the whole right column's width, measured at 53 columns with it absent against 60 with a vessel targeted.
- **Nothing else moves when it leaves.** NAVIGATION keeps its rows and the navball keeps its place, verified frame against frame. The notice bay notices the extra room and folds one notice fewer.
- **TARGET is the only box that does this**, and deliberately so: it is the last box in the right column, so it can come and go without anything above it shifting. COMMS with no signal, STAGES on a single-stage vessel and MISSION with no mission all keep their slots and their dashes, because the left stack would otherwise slide under you mid-flight. This amends ADR 0051's every-box-always-present rule for TARGET alone.
- A docked vessel has a target, so it keeps its box. Switching TARGET off in Settings still blanks its rows in place rather than removing it, and `F2` declutter is unchanged.
- No save-schema change.

### v0.44.2

The readings that were setting each instrument box's width say the same things in fewer words, so the map gets the columns back (closes #478; PR `#479`; tag `v0.44.2`).

- **ENGINE's node row reads `▸ Prograde 1234 m/s, 5m00s left`** during a burn, and the whole row is amber while the engine is lit. The `▸` and the running countdown already said it was firing, so `Δv` and the word `burning` are gone and the colour carries the cue alongside the glyph.
- **NAVIGATION says which way round on the `incl:` row**, as `incl: 28.61° pro` or `retro`, and the separate `dir:` cell is gone. On a coasting frame that alone takes the box from 67 columns to 53.
- **One spelling per flight frame, everywhere.** `hold:` used to read `Surface Retrograde (SURF)` beside `nav: SURFACE`, naming the same frame three times in one row. It now reads `hold: Retrograde (SURF)` with `nav: SURF`, and the `nav:` flash message matches, so `ORBIT`, `SURF` and `TGT` are the only forms you see.
- **TARGET's approach row reads `range: … close: … rel: …`** with tighter gaps, keeping all three readings, which are the ones you fly the last hundred metres on.
- **PROPELLANT's `rcs Δv:` is now `rcs:`**, since `m/s` already says what kind of number follows. `monoprop:` keeps its full name, because it names a real substance.
- Measured on one coasting frame at 181x49: NAVIGATION 67 to 53 columns, GUIDANCE 56 to 54, and the map gap between the columns 56 to 69. With a vessel targeted, TARGET's own row becomes what sets the right column, so the gain there is smaller: the win depends on what the flight is doing.
- The F1 readout glossary, `docs/controls.md` and `CONTEXT.md` carry the new wording, including the `pro` / `retro` tag and the frame spellings.
- No save-schema change.

### v0.44.1

Leaving the game now asks whether to save instead of writing over your autosave on the way out, and the instrument boxes give a few columns of screen back (closes #474 and #476; PRs `#475`, `#477`; tag `v0.44.1`).

- **Quitting asks.** `ctrl+c` and the pause menu's Quit row both raise one prompt with one wording: `Save before quitting? [y] save and quit  [n] quit without saving  [esc] stay`. `[n]` writes nothing at all, `[esc]` puts you back exactly where you were, including the Saves browser's frozen clock. Before this every exit wrote into the autosave ring, and setting the autosave interval to off did not stop it, so a flight you wanted to forget could evict a snapshot you wanted to keep. A guest in a multiplayer session sees `Quit? your flight saves automatically`, with no `[n]`: the session writes their vessel on disconnect however they leave, so offering the choice there would be a lie.
- **A node burn no longer counts its throttle clock two different ways.** ENGINE's throttle cell showed elapsed time for a manual burn and remaining time for a node burn under the same `● FIRING` glyph. The node row already carries time remaining, so the cell now keeps its clock only for a manual burn's `T+`.
- **Each instrument box sizes its own label column** instead of every box inheriting one shared width from the longest label anywhere on the HUD. GUIDANCE loses 2 columns, TARGET 4, ENGINE, PROPELLANT and NAVIGATION 1 each; the left stack's widest box goes 58 to 56 and the right column 68 to 67. Columns stay pinned per box, so a reading that grows mid-burn still never steps the next column sideways. What sets a box's width is the widest reading it must clear, not its padding, which is why the saving is small and where the next pass has to look.
- No save-schema change.

### v0.44.0

Every flight readout now lives in one of eight fixed boxes that never move or reflow, the same set on the map and in the LAUNCH view, and you can find a launch window against another vessel while the game tells you whether waiting on the pad is worth it (ADR 0050 and ADR 0051; closes #454; PRs `#462`, `#463`, `#469`, `#470`, `#472`, `#473`; tag `v0.44.0`).

- **Eight instrument boxes replace the scattered flight chips**: ENGINE, PROPELLANT, GUIDANCE, NAVIGATION, COMMS, TARGET, STAGES and MISSION. Each keeps its slot for the whole flight; a row that has nothing to say prints `—` instead of the box coming or going, so nothing below it ever jumps. Every row carries two aligned readings where a box would not otherwise fit at 140x40 (`altitude: 0 m  vert: 0.00 m/s`), pinned to fixed label columns so the second reading lines up row to row.
- **The map title names your active vessel**, not just a fleet index: `Sol — S-IVB-1 — focus: S-IVB-1`. The old dedicated VESSEL box is gone; its numbers moved into NAVIGATION and PROPELLANT.
- **`● ORBIT READY [C]` lights on NAVIGATION's title at each world's own Orbit Floor** (the atmosphere's top plus 25 km, or 25 km flat with none), not the old flat 200 km, so Earth reads 175 km and every airless world reads 25 km. The floor's number is never shown, only the cue lighting. The Ap cell also carries a trend arrow now, `↑` climbing or `↓` falling, nothing while steady.
- **Pop-up notices (FRAME TRANSITION, CAPTURE PREVIEW, SOI PASS, VESSEL DESTROYED, DOCKED, chat, session events, and the Rendezvous picker) move into a notice bay** at the bottom middle of the map, between the instrument columns and the navball. They stack upward, newest at the bottom, and fold behind a `▸ +N more` line rather than ever paint over a box.
- **NAVIGATION carries a permanent `plan:` row** once a burn is planted, naming the world the plan is measured against (`plan: Moon encounter  AN 355.0°  DN 175.0°`, or `Earth orbit`, `Earth escape`, `equatorial`), and its Ap, Pe, incl and period cells grow a `→` arrow to the value the plan would leave you with. A queued node that busts the stage's Δv budget shows a bare `⚠` on ENGINE's row; a second and later queued node folds behind `+N [m]` instead of spelling out a count.
- **The LAUNCH view matches the map exactly**, box for box, row for row. The old ATMOSPHERE and DESCENT CORRIDOR chips are gone; the launch strip drops its own `vert:` reading (NAVIGATION's is the only one) and gains the `● ORBIT READY [C]` cue, and climbing through air draws the altitude ladder straight into the horizon picture instead of as a separate box.
- **`F2` declutter now hides all eight boxes together**, except ENGINE and PROPELLANT stay up while an engine is lit, matching the rule that fuel and a live burn are never hidden mid-burn. Settings can also switch off any one box; a box switched off leaves its rows blank in its own slot rather than dropping, so nothing moves.
- **`Δincl:` reads against a targeted vessel**, not only a body, on the pad and in the full TARGET chip. Target a vessel in orbit and warp: the angle opens and closes as the world turns under the pad. It is withheld when the target orbits a different world, and at a pole, where floating-point noise used to leak through and `I` would plan 8 to 13 km/s. A target landed on the surface reads the due-east launch plane and says so: `Δincl: 53.59° (due east)`.
- **A new `depart:` row** on the pad and in NAVIGATION: the angle between your orbit and the plane the world beneath you travels in. On the pad it carries `(best N°)`, the lowest value waiting a rotation can reach. From KSC it sweeps `5.17°..52.05°`; from a Luna pad `23.46°..33.75°`. Where it cannot move, the cell reads `—`, and it has its own line in the F1 readout glossary.
- **Airless pads get a readout.** Standing on Luna, Glyph or any other world without an atmosphere, the pad now shows `heading:`, `incl:` and, with a target set, `Δincl:`. Before this only the 7 worlds with an atmosphere had one.
- **A stable orbit around an airless world stops re-waking the ascent cues.** The ascent arc and nose-prograde stubs used to return once per orbit; "done climbing" now means periapsis above the surface when there is no atmosphere to clear. This is the fix for #454's airless-instrument gap: an airless ascent now reads `Δv→circ:` and a burn time the same way an atmospheric one does.
- **`hold:` names the frame it holds in**, everywhere it appears: `Surface Prograde (SURF)`, `Prograde (ORBIT)`, `Target+ (TGT)`.
- Renamed to match the science: player-facing "Meeting Place" / "Meeting Plan" / "Meeting Planner" text is now "Rendezvous" (the picker title `RENDEZVOUS PLAN`, the F1 section `RENDEZVOUS PLANNER`), and the orbit-size refusal reads `radius outside target's apsides: plan a transfer [H] first`.
- Five review leads independent of this rework are filed as follow-ups, not fixed here: #464 (SOI PASS still renders after a crash), #465 (the stop-margin colour lags the dry tank), #466 (staging ends a manual burn), #467 (shed Apollo stages re-dock in lunar orbit), #468 (`ORBIT READY — returning to tilted` flashes on every `V`).
- No save-schema change.
- Flown on the build before tagging: the vessel window swept `26.38°..80.19°` against a 51.6° target from KSC, inside its predicted envelope, no ascent cue reappeared across eleven samples of a stable lunar orbit, and a live Saturn V ascent lit `● ORBIT READY [C]` at 189 km, planted `C` and `H` together for a four-node stack, and opened `RENDEZVOUS PLAN` against a second vessel, all with no box ever overlapping another.

### v0.43.0

You can aim a launch now. `{` and `}` swing the commanded heading five degrees at a time, on the pad or on the way up, and the pad tells you the inclination that heading actually reaches instead of claiming your latitude is locked (ADR 0049 decisions 8-11; closes #453; PR `#459`; tag `v0.43.0`; save schema 10 to 11).

- Every ascent used to come out due east, so every orbit ended at `i = |latitude|`. Pitch trim only rotates about local north, so there was no way to aim, and the SURFACE chip said `launch lat: 28.6° (locked)`. That was honest about a limitation rather than stating a fact: latitude is a floor, not a value.
- **The commanded heading is an absolute bearing.** `heading: 120°` means steer to 120 and hold there. The nose slews at the usual 15 degrees per second and thrust follows the attitude, so nothing snaps and the orbit bends only as the engine pushes it. `|` reset pitch trim before and now resets both.
- **The pad shows two rows and `(locked)` is gone**: `heading: 090°` and `incl: 28.61° (min 28.61°)`, the inclination that heading yields from this pad, with your latitude as the floor. Aim west and it reads `incl: 151.4°`, a genuine retrograde orbit. An equatorial pad reads `(min 0.00°)`. Nothing is ever locked.
- **Warping on the pad finally changes something you can see.** With a target set, a `Δincl:` row shows the plane angle an ascent lit right now would leave to the target's plane, ticking as you warp. Find the window by warping and watching it fall. A `window: T-Nh` solver that does the search for you is filed as a follow-up.
- Once you are flying, `heading:` moves up beside `trim:` and `incl:` goes back to your live orbital element, so you watch the real number climb toward the one the pad predicted.
- Fixed along the way: the TARGET chip's `Δincl` row was heading-blind while landed and always read due east.
- Two errors in the ADR itself were caught in review. Its stated trim ordering made heading a complete no-op on the pad, since rotating a vertical vector about vertical does nothing, and every existing test missed it by sitting on the same side of that boundary. Its retrograde example paired a due-south heading with a westward inclination.

### v0.42.0

Every number on the flight surfaces now speaks one dialect. Durations, distances, masses, countdowns and labels all route through a single formatter instead of the nine that had drifted apart (ADR 0049 decisions 1-7 and 12; the readout half of #453; PR `#458`; tag `v0.42.0`).

- The 2026-09-02 UX review found seven dialects of number on screen at once. Durations appeared as `1h34m28s`, `4.72h`, `T+366773s`, `00:00:02` and `fin 109s`, from five separate formatters. Six-digit ranges and masses printed unseparated (`371639 km`, `2160000 kg`). The `alt` suffix appeared on exactly one panel. Labels were written as variable names (`v_vert`, `t_to_apo`) or as maths (`|v_rel|`). A below-surface periapsis printed as a plain negative with nothing to mark it. `Δv budget` meant the current stage and never said so.
- **Durations** read two adjacent units and truncate rather than round, so a countdown never claims more time than there is: `45s`, `2m07s`, `4h43m`, `3d05h`. `period:` is the one exception and keeps its seconds, because that is the number a resonant or phasing orbit is tuned against.
- **Countdowns use the launch convention**: `T-` counts down to an event, `T+` counts up since it. This is a real behaviour change. The old formatter printed `T+` for a node in the *future*, the exact inverse of the mission clock sitting on the same screen.
- **Distances climb an SI ladder** at four significant figures: metres below 1 km, then km to 9999 km, Mm to 999.9 Mm, Gm, then AU. The rung is always printed, so a unit change is never silent. Masses go kg then t, thrust is kN everywhere, and the `alt` suffix is gone because every apsis and altitude in the game was already a height above the surface. The F1 help overlay says so once, in a new readout glossary that also explains the six codes that survived (`fpa`, `Q`, `TCA`, `TWR`, `Ap`/`Pe`, `Δv`/`Δincl`).
- **A below-surface periapsis keeps its signed depth and turns Warning-coloured**, label and value together, on every panel that prints one. On an ascent it is the distance still to climb through zero; on a de-orbit it is how committed the descent is. Replacing it with a word would have thrown that away.
- **`Δv budget` became `Δv: <stage> / <vehicle> m/s`**, so a Saturn V finally says whether the whole stack can reach orbit rather than only what the lit stage has left.
- Fixed along the way: every formatter hung forever on an infinite input, which would have frozen the interface with no error; four significant figures was quietly five at each decade boundary; and the CAPTURE PREVIEW chip's rows had been misaligned by a column or two, which no string assertion can see.

### v0.41.1

Two fixes from flying the tagged v0.41.0 build: the engine-lit cue moves off the whole screen onto the VESSEL chip, and Launch View's Auto-Warp button actually does something when you click it (#456; PRs `#455`-`#457`; tag `v0.41.1`).

- #455: v0.41.0's "the whole screen says an engine is lit" cue (a canvas-border color swap plus a title-bar `● BURN` badge) read as too loud in play. Moved onto the VESSEL chip's header instead — the one chip already shared across the orbit map, the Launch View chase-cam, and a landed vessel — sitting next to the throttle row's own `(idle)`/`● FIRING` state. The border and title-bar badge are gone; both canvases are back to a plain border.
- #456: clicking Launch View's `[»Burn]` button navigated to the Missions screen instead of toggling Auto-Warp. Launch View draws its own title bar with its own burn button but never gave it a click target — the mouse handler was reading the orbit map's title-bar button positions instead, left over from whenever the map was last on screen. Launch View now tracks its own button position; clicking the NODES chip on that screen (which was already working) still works too.

### v0.41.0

Burn state stops hiding, the controls stop lying about being careful, and two chase-cam HUD chips stop cycling forever once you've cleared the atmosphere: the throttle row says whether the engine is lit, the whole screen borders red during a burn, a burn fires and finishes with an Event Flash, silent no-ops on `G`/digit vessel slots/tilt-yaw refuse out loud instead, `q` and `F9` both arm a confirm before discarding anything, and the ATMOSPHERE chip plus the ascent arc/attitude stubs finally stand down for good once an orbit's periapsis clears the atmosphere (2026-09-06 grill; item 3 of the ranked UX-review cycle; #427, #445, #446, #449, #451; PRs `#445`-`#448`, `#450`, `#452`; tag `v0.41.0`).

- #447: the VESSEL chip's `throttle:` row appends `(idle)` or `● FIRING`; the map/launch-view border and title bar carry `● BURN` chrome for the whole session while any craft is thrusting; the TARGET chip collapses its predicted-encounter rows to `encounter: recomputing…` during your own burn; a burn firing or finishing now raises an Event Flash (`World.LastBurnFiredEvent`/`LastBurnFinishedEvent`, ADR 0048) naming the node and its Δv; declutter is named in both the canvas footer and the title bar while it's on; the NODES chip's countdown counts to ignition, then to burn-end, instead of stopping partway.
- #445: the Launch View's title bar was missing the `[»Burn]`/`[■Burn]` Auto-Warp button the Orbit View title bar already had — a rendering-only parity fix (`G` already worked from Launch View; the button just wasn't drawn there).
- #446: ten `X` presses from full throttle landed on a `1.3877787807814457e-16` float residual instead of exactly `0`, so `ManualBurn` never cleared and the 10× burn-warp cap stayed engaged with the throttle reading a displayed `0%`. `SetThrottle` now floors anything under a `1e-9` epsilon to exact zero before its cut check.
- #448 (item 3, "refusals and confirms"): `G` with no eligible burn, a digit key for an empty vessel slot, and `shift+↑/↓/←/→` outside the tilted view now refuse out loud instead of silently no-opping; the pause menu's keyboard `q` arms the same y/n confirm the `[Quit]` click always used; `[K]` rendezvous's "no vessel target" refusal now names `[t]` as the fix; the session screen explains how a guest joins, not just how to host; Inspect's Hint Strip line shows `[enter] target · [esc] exit inspect` while a highlight is live.
- #449/#450: the ATMOSPHERE chip flickered back on every orbit even in a stable orbit fully clear of the atmosphere — its gate was instantaneous climb rate, which swings positive on every periapsis→apoapsis half of any real orbit regardless of altitude. Fixed by gating on periapsis altitude (or current altitude, for hyperbolic/degenerate orbits) against the atmosphere's cutoff instead.
- #451/#452: the ascent arc and nose/prograde attitude stubs shared the exact same bug (a separate gate, same climb-rate blindness) and kept cycling once per orbit above the atmosphere after #450 landed; given the same `atmosphereClearedForGood` check. `DescentCorridorFor`, the sign-mirror sibling, was checked directly and found already correct (it requires a real impact forecast within its horizon, which a periapsis-above-ground orbit never produces) — confirmed by instrumenting it rather than assumed. #454 tracks the same symptom's airless-body case (no atmosphere to clear, so the check is a deliberate no-op there) as a follow-up.
- Reversed ADR 0033 §H ("F9 stays instant, no confirm") as part of #448: F9 quickload now arms the same y/n confirm the Saves-browser Load already used, per Jason's explicit call when the conflict with the existing ADR was flagged. Amendment recorded in the ADR itself.
- No save-schema change.

### v0.40.0

The screen stops lying about space, the planner stops lying about your plan, the game starts talking, and the front door is open: chips shrink before they vanish, forms keep their cursor on screen, the maneuver planner gets a Plan Cursor, docking says why it refused, the pad tells you how to light the engine, a crash gets a banner, the Saturn V finally looks like a rocket, Flight School is on from the first frame with a five-rung ladder that ends in a launch and a dock, and `?` opens help (UX review 2026-09-02, phases 1-3; #373, #421, #422, #423, #424, #425, #426, #427, #428; PRs `#434`-`#442`; tag `v0.40.0`).

- #423: the F1 overlay no longer lists `q` as quit (it is radial+ in flight), gains the missing `E` row, fixes the `< / >` and `;` rows, and the body-info footer names `h`/`l` instead of two dead keys. A keymap-coverage test pins every `DefaultKeymap` binding to an overlay row.
- #373: a reusable windowed list keeps the spawn form's CRAFT TYPE cursor, category header and footer on screen at any height (the form is 46-52 rows, taller than any playable terminal); Settings uses the same widget; the VAB footer wraps instead of truncating `[s] save` / `[o] open`.
- #428: PLANNED NODES is a Plan Cursor (`↑`/`↓`, Enter loads, ctrl+d deletes) and PROJECTED ORBIT always projects the cursor node, never a stale draft; an over-budget node plants with a `⚠ exceeds budget by …` marker; clear-all is ctrl+k only; a QUICK PLANS block lists and fires the one-key planners; `H` with no target refuses out loud; the budget line reads `(N after plan)`.
- #422: two named terminal sizes, Playable Floor 104×24 (the gate) and Design Size 140×40 (what the game is laid out for; named in the README, the controls guide and the too-small screen). Below the Design Size chips follow Graceful Shrink: Compact Form before drop, one column per side so stacks never overlap, a `▸ +N hidden` stub where a chip does drop, numbers clipped on the right only. Replaces the clamp that painted a force-shown NODES chip over the ORBIT chip.
- #421: the PROXIMITY chip's `|v_rel|:` row carries the dock gate (`(need < 0.10)`) whenever you're inside 50 m but too fast, keyed on the same predicate the sim refuses on; the dock ring gets a red over-speed state alongside green/amber/dim; spawning "alongside active" announces `docked with S-IVB-1 — now 1 vessel, 2 components` instead of silently welding you in; the ATTITUDE `hold:` row names the hold in the TARGET frame.
- #427: one `flash` helper with one lifetime replaces a dozen hand-set status timers; a `VESSEL DESTROYED — [E] end flight [F9] quickload` Standing Alert persists while crashed and survives Declutter; the pad shows `[b] ignite · [space] stage · [z/x] throttle` until the engine is lit, `engine: ● LIT` sits beside TWR, the launch title bar carries the warp rate, and staging flashes `dropped S-IC — 2 stages left`. No ascent coach by decision.
- #424: the pad sprite is rasterised as filled rectangles at the canvas's own pitch (the old 1.5 m point cloud striped every third row) with anchored plotting to kill sub-pixel rounding ties; stage width floors at four sub-pixels, a dark separator row marks each stage boundary, the near-white stage colours are spread apart across seven multi-stage loadouts, and the flat green slab becomes a horizon band under a two-step sky that thins with altitude. Took three passes: the first two fixed the direction and the anchor while the on-screen rocket stayed striped; the real-path row-solidity test caught it.
- #425: Flight School is on unless you switch it off, for fresh and existing installs alike (Challenges stay opt-in); a fixed dim Hint Strip (`[F1] help · [t] target · [m] plan · [n] new vessel · [./,] warp · [tab] system`) sits on the map's bottom row; `?` opens help everywhere F1 does and pitch-trim reset moves to `|`; `t` cycles nearest-first (the Moon is the first press from LEO, Earth the first from lunar orbit, your own primary never appears); the pause menu's `Controls` becomes `Keyboard layout` beside a real `Help (F1)` row; the F1 overlay puts CAMERA & VIEW, TIME & WARP and MANUAL FLIGHT right after GENERAL and pushes SAVES and MULTIPLAYER to the end.
- #426: Flight School grows to five rungs (Orientation, Plan a Burn, Fly It, Off the Pad, Meet & Dock), each step an objective in its own words; "aim for the Moon" credits only when TARGET reads Moon and "plant a transfer" only a Moon transfer within your Δv budget (event objectives gain `require_*` gates; `ManeuverNode.OverBudget` is the one over-budget predicate shared by the NODES chip, the planner and the evaluator); the ORBIT chip gains an `e:` row so the eccentricity-graded rungs have a number to check; the MISSION chip shows `relays online N/3` during Relay Constellation; the planner carries a one-line `MISSION ▸` reminder on tutorial rungs; the ladder screen splits into FLIGHT SCHOOL and CHALLENGES lists with their own counts, locked rungs wear `⚿`, each program carries a `[1]`/`[2]` row that turns it on or off (so `M`, `1`, `Esc` opts out of Flight School for good; PR `#443`), and finishing a program shows a Sendoff; `Rendezvous & Dock` is a root rung and `Mars Flyby` now needs `Luna Capture`. Choosing or abandoning a rung is deferred (needs persisted state).
- ADR 0046-0048 record the phase 1-2 decisions; phase 3 decisions are in the vault action plan and the `CONTEXT.md` glossary (no ADR, all reversible). 104×24 is explicitly out of scope for fidelity. No save-schema change.

### v0.39.0

Rendezvous becomes a plan you agree to rather than a nudge you repeat: `K` opens a Meeting Planner instead of refusing, `[I]` matches another vessel's plane, and Engage now means "we are going to meet" (#394-#400; PRs `#401`-`#405`, `#408`-`#412`, `#414`-`#415`, `#419`; tag `v0.39.0`).

- #394: one flat 4 h search horizon everywhere; `rendezvousHorizonSeconds` deleted, so `K` scores the same encounter the TARGET chip shows. Closes #290.
- #395: an engaged leader is *paced* toward its partner instead of frozen. Measured over 300 ticks: hold transitions 180 to 0, ticks stuck at dead 0x 30% to 0%. Closes #279.
- #396: a new player joins at the earliest **live** clock, not the frontier, so a long-departed session can't strand them in the past. `--reset-fleet` still uses the max. Closes #247.
- #397: `[I]` learns a vessel mode, deriving the burn from the target's own angular momentum; post-burn normals agree within 0.5 degrees.
- #398: the Meeting Planner: a Meeting Place (their orbit / your orbit) over a Lap Ladder of laps / wait / cost, planting one node.
- #399: `K` is modal, opening the picker on a phase mismatch instead of dead-ending; a plane mismatch is named, never silently fixed.
- #400: Engage forms the agreement before any plan exists, carries the Meeting Place on the wire, and commits to a planted plan's own arrival however far out. Closes #277.
- Two review rounds fixed 16 findings. The second round's two high-severity findings were both introduced by the first round's fixes.
- **The Meeting Planner covers same-size, near-circular pairs.** Different-sized orbits (#407), an eccentric holder (#413), and "the crossing" (#416) all refuse rather than mis-solve. ADR 0039's Shape-Match Gate is therefore **retained**; its removal is #406. No save-schema change.

### v0.38.2

Rendezvous Engage only commits to an encounter you can actually reach, guarded keys say why they refuse, a Relay-Tug is no longer warned off its own mission, and a craft target lock survives a server-restart reconnect (#276, #282, #283, #294; PRs `#390`–`#393`, merges `a448372`/`8c03115`/`d1e9426`/`29ec37c`, tag `v0.38.2`).

- #276: `RendezvousCommit` searches current courses only, or an actually-planted `[K]` nudge (post-burn state, target-relative axes included, bound to the same peer); a refusal restores the prior target.
- #282: shared `App.refuse` gives one-phrase status refusals to the `E`/`K`/`H`/`I`/`C`/`R`/`m`/`P` state guards; `K` still reaches from body-info and missions.
- #283: the spawn form resolves the vessel through `SyncFields`; a genuine relay (relay antenna, controllable) gets a neutral `coverage from here: ~N%` line instead of `⚠ … relays advised`.
- #294: ghost target/node/burn refs persist across reconnect (save schema 9→10); serve-side re-latch with a 45 s presence-gated give-up and a "target lock lost" chip; an unresolved target holds a burn (throttle cut aborts it) and never aims at the planet's centre; cross-owner dock parcels strip refs.
- Six review rounds fixed ~40 findings pre-merge. Older binaries refuse v10 saves; this build still loads v1–v9.

### v0.38.1

Two vessels you just undocked can dock again without a 200 m round trip, and the dock gate ring no longer turns green while the game is refusing to dock (#372; PR `#383`, merge `66960e6`, tag `v0.38.1`).

- Cause: `SeparationPush` parks pairs at 75 m, inside the 100 m `ReArmDistM` latch, so re-docking silently failed.
- New flight-view `c` ("couple") clears the latch for the aimed vessel (or all latches on the active one); docking stays automatic.
- `ProximityDockGateReady` now includes the latch; the ring gains an amber "latched" state, never green while refused.
- `checkDocking` refusals surface once per latch via `raiseLocalReArmRefusal` / `World.LastLocalReArmRefusal`.
- Same-World only (`relay/dock.go` untouched); ADR 0038 §5 amended (a keypress is not silent re-fusion); no save break.

### v0.38.0

A vessel parked on an airless body stops being described as an orbit, the close-zoom map stops twitching, and the descent corridor says when to brake and whether you will stop (#375, #376, #377, #378; PRs `#379`–`#382`, merge `5ecf0b0`, tag `v0.38.0`).

- #375: co-rotation velocity fed `ElementsFromState` a fake ellipse; `craftHasOrbit` now gates ellipse, apsides, TCA/CA and `✕` for landed vessels.
- #375: ORBIT chip shows surface facts (`buildLandedOrbitChip`), TARGET gets `landed at:`, `formatChipKm` kills `-0.0 km`.
- #376: close-zoom scale cap gated on `Altitude() <= 0` not `Landed`, flipping each frame; fix restored the #363 raster cache hit rate.
- #378: surface chase-cam screen-right follows surface-relative velocity (`physics.AirRelativeVelocity`), so braking descents are not mirrored.
- #377: stop margin is now `sim.PredictPoweredStop` + `sim.PredictBurnAt`; corridor gains `burn at` and live `stop margin`. #377/#378 amend ADR 0043.

### v0.37.0

The spawn form's ALTITUDE field becomes a number you type, clamped to a floor and ceiling every body now actually has (ADR 0044, PR `#374`, merge `6c6f3a1`, tag `v0.37.0`).

- `altitudePresets` ladder deleted; `Enter` opens a digits edit box, `←`/`→` walk body-derived Orbit Stops, no wrap.
- Orbit Band: floor = `Atmosphere.CutoffAltitude + 25 km`, ceiling = two-thirds of the way to the SOI; stars have floors only.
- Phobos and Deimos have no legal orbit altitude; still selectable for landing/launchpad, but `Enter` goes dead on `orbit` with a reason.
- One clamp rule (`World.SpawnCraft`/`sim.ClampToOrbitBand`) serves the form and `--altitude`; values are moved and reported, not refused.
- Review fixed nine defects pre-merge (SOI as centre distance, hidden CommNet warning, `capturingText()` gap, 1 m arrow epsilon).
- Interior-stop rule (`1/2/5 × 10ⁿ`) is an ADR amendment; star stand-off hash-excluded like ADR 0024; #373 (form overflow) out of scope.

### v0.36.3

Ring outlines stop being recomputed every frame and remembering what a pixel means gets cheaper; the last residuals of the #367 profile (#369).

- `canvas_ring_cache.go` replays recorded ring pixels (SOI ring, Saturn bands); new Saturn benchmark: −7.7% frame, −38% at deep zoom.
- `pixelTags` is now a dense index grid into a per-frame tag table (~41 ns to ~6 ns per point on a hit); iteration is deterministic.
- Review bug: the "no zero tag" invariant was false (horizon fill writes empty `SurfaceColorHex()` outside Sol); now enforced.
- Review memory: naive grid cost 69 MB over four SSH seats, index+table is 11 MB pointer-free; a linear-scan intern was 6% slower.
- Round-robin numbers: default scene −2.2%, Saturn −7.7% vs pre-#369 main. Display only. Still open: `--serve` render throttle (#370).

### v0.36.2

Orbit lines stop being redrawn from trigonometry every frame, closing the idle-CPU arc that started at #363 (#367).

- After v0.36.1 the hot spot moved to ellipse geometry; the detached prod server (200×50 tmux, zero players) sat at 55–72% of a core.
- Curve geometry memoized per curve, predict-on-change; any sub-pixel change in elements, scale, basis, offset or canvas size busts it.
- Live-ticking measurement: 97.6% hit rate at 1× warp, ~0% above 100×; whole-frame idle render −23% to −26%.
- Review: the cached-vs-truth pan test compared two warmed caches; now cold-referenced and proven to fail against a sabotaged key.
- Disk offset-mask cache from v0.36.1 deleted (within noise, retired a ~640 MB retention case). Display only, no save break.

### v0.36.1

The idle game stops burning a quarter of a core redrawing a planet that is not changing (#363, #364).

- Orbit screen idled at ~27% CPU vs 0.9% on the menu; the cost was re-rasterizing the focused body's disk every 50 ms.
- Disk raster memoized (ADR 0017 predict-on-change) on body, system, pixel radius and angles quantized to 0.02°; rotation still animates.
- Review: screen position was missing from the key, so 90 of 144 zoom/pan configs showed an uncolored planet; hit path now self-heals.
- Review: retained heap (28–81 MB) back to 0.4 MB; off-canvas disks skip allocation, offset cache LRU-capped.
- `Canvas.String()` coalesces styled runs, byte-identical. Idle CPU 22.65% to 18.36%; disk raster 394 µs to 87 µs, 1793 allocs to 1.

### v0.36.0

The map answers "whose line is whose," the camera stops fighting your hands, and the last 35 km of a rendezvous is flown by sight (ADR 0041, 0042, 0043; #346, #347, #348, plus review batch #359/#362).

- Legibility: ADR 0020's markers finally drawn (`✕`, target `▲`/`▼`, `◇`/`◆` nodes); SOLID = real, DASHED = plan, DOTTED = scenery.
- Inspect: `j` steps a flare with a name chip through everything drawn, mouse click does the same, Enter commits as Target.
- Camera: per-focus zoom memory (amends ADR 0021), arrows pan, `h`/`l` browse bodies, arc-length sampling keeps dashes at any zoom.
- Proximity View (`o`): LVLH frame, exact dashed drift path, log range rings, 50 m dock gate ring green under 0.1 m/s, real silhouettes.
- Surface view: `⊗` impact point, descent corridor with MARGIN, ascent cues, max-Q band; `v` cycles six projections, `V`/`o` toggle.
- Review fixed a map-order hit-test tie-break, a `V`-in-Proximity ping-pong, off-canvas burn staging, a NaN guard, a double-signed altitude.

### v0.35.3

The target panel finally says whether you are ahead of or behind your target (#287).

- Range and closing are unsigned along-track: two coplanar craft 13,500 km apart both read `closing 0.00 m/s`; the sign decides the burn.
- Live two-player session had to infer the sign from `t→Ap` trends (after the constant-half-period bug #286, fixed in v0.32.4).
- TARGET chip shows a signed lead angle: `lead: +82° (ahead)`, `-82° (behind)`, `0° (aligned)`, measured in your orbital plane.
- Reads as a blank dash for body targets and across SOI boundaries. Display only, no save break.
- Known limitation: the TARGET chip is dropped entirely at 24 rows (including the 104×24 floor); pre-existing, not a regression.

### v0.35.2

Undocking a stack no longer re-fuses itself on the next tick (#342, #343).

- Cause: fixed total split span (35 m at three, 17.5 m at five) put neighbours inside the 50 m / 0.1 m/s auto-dock gates.
- Easy to reach: a comsat carrier already holds relays as components, so one dock onto it makes three.
- Components now separate by a fixed adjacent gap: 75 m and 0.15 m/s regardless of count.
- #343's closed-ellipse premise did not reproduce: the push displaces position too, so pairs were safe by accident of magnitude.
- Pushes are now along-track so separation grows for good, pinned by a test; local undock gains the re-arm latch ADR 0038 only gave cross-player.

### v0.35.1

Batch-review fixes for v0.35.0: undocking can no longer permanently brick a vessel, and riders get their exits back (#326–#337).

- ADR 0038 re-arm latch refused on either craft ID (blocked both vessels against everyone); now it refuses only the separated pair.
- Latch arming persisted but clearing did not, so stale latches came back on restart; clearing now persists and a 10 min sim-time ceiling retires bad records.
- Being held off is no longer silent: one chip, `docking held off, back 100 m clear of <partner> before docking again`.
- Undock lost the guest's target when they owned a second vessel (mutual targeting ran before adoption); DOCKED block was clipped off 80x24 (#301 again), chips now obey a per-corner budget.
- Also: `[u]` advertised for key `U`, camera follow no longer overrides `f`/`g`/Spectate, handle repair and enrollment fixes, dock-ledger writes retried. No save break.

### v0.35.0

Both players now know when they dock, the absorbed player rides along in the stack, and the ship you get back on undock is safe to fly (ADR 0038, #301, #303, #304, #274, #289, #293).

- Dock chips on both seats, every path; the guest's notice is carried durably and delivered on their next tick, surviving server restarts.
- Guest view follows the stack (owner's handle badged) with a standing DOCKED block: `[J] request control`, `[u] ask to undock`; an empty pilot seat grants control immediately.
- Undock returns a known-inert vessel (throttle zero, main engine, no hold), Kepler-propagated across the subspace gap, with the 75 m / 0.15 m/s push; fixes a silent re-fuse nine seconds after separation.
- Undock disarms auto-dock with that partner until ~100 m clear, and both craft come out targeting each other.
- `K` and `C` replace their own unfired node instead of stacking; NODES chip force-shows with more than one node queued; case-insensitive handle collisions refused and repaired; server restart moves from `u` to `F4`. No save break.

### v0.34.0

The rendezvous planner explains its refusals and reports arrival speed, and the game says "vessel" everywhere it said "craft" (#277, #278, #281, #290, #318).

- `K` names the gate that fired and the remedy (circularize `[C]`, plan a transfer `[H/I/m]`) instead of one generic shrug.
- New shape-mismatch gate: iterating `K` between different-shaped orbits diverges, each burn making the next worse.
- Recommendations report arrival speed (`CA 9 km, arriving ~540 m/s`) as information, not refusal; doctrine is Δv cheap, waiting costly.
- Beyond-window or never-closing coasts get plain-words phasing advice; the RENDEZVOUS chip carries a live CA trend row.
- Player-facing craft to vessel rename; a stopwatch-based multiplayer reconnection test now waits on the real event. No save break.

### v0.33.0

Rendezvous warp keeps its clock all the way to contact, and a docked pair survives a server restart (#275, #280, #302, #305, #307, #308, #309, #310, #311, #312, #313).

- Reaching the committed encounter demotes the agreement instead of ending it; it ends only on dock or explicit cancel, no distance tripwire.
- The initiator flies the clock; the accepter is copilot (brake or cancel, never push); either side's burn holds the pair at the burn cap.
- Lock state is standing text on the RENDEZVOUS chip (partner, seat, rate, `held: fenbot burning`); proximity couples show a TIME LOCK line; chip flap (852 moments in one session) deduped.
- Dock ledger is durable on disk; offline owners get a Parcel delivered, Kepler-propagated, on reconnect; `[J]` refuses with no live pilot and lets a guest reclaim an empty seat.
- Undock after control transfer no longer swaps vehicles; refused undocks explain themselves. Session directory migrates v2 to v3, no player-save break.

### v0.32.4

Four readouts stop lying: circular-orbit apsis timers, roster location, roster craft count, and rendezvous arm labels (#285, #286, #288, #295, #297).

- `t→Ap`/`t→Pe` on an exactly circular orbit printed a constant half-period; the ORBIT chip now shows a blank marker instead (node scheduling untouched).
- Roster LOCATION follows the craft a player is actually flying (wire report gained an active-craft marker; craft switch forces a re-report).
- Craft column shows a blank for players with no live report rather than a confident `0 craft` that read as a wiped fleet.
- Arming a rendezvous names the vessel on both seats (`armed → gern (Relay Tug-1)`); `K` refusals label themselves once. No save break.

### v0.32.3

The rendezvous coast actually hands the ship back at the encounter it flew you to (#298, #299).

- Reaching committed τ inside the 35 km range releases the coast regardless of velocity; the old gate also needed ≤100 m/s, which transfer arrivals (594 m/s live) never meet.
- Velocity term still gates Proximity Co-Warp coupling, so fast arrivals are released but not coupled until the pilot matches velocities.
- Manual engine ignition and RCS pulses release the standing intent (arm and driver), so the coast can't warp against your own match burn.

### v0.32.2

New `--serve --reset-fleet` flag: one-shot server-admin fleet reset at startup.

- Every player's slate is wiped to the one default vessel with full tanks, placed on a shared Earth 500x500 km equatorial ring, spaced at least 50 km apart (above the 42 km decouple gate).
- All subspace clocks aligned to one epoch so nobody needs a Sync; enrollment (handles, roles, invites) untouched.
- Previous saves backed up byte-identical into the session directory's `backup/` subdir first. No save break.

### v0.32.1

Proximity couple gate widened from 10 km to 35 km, decouple hysteresis to 42 km (#291).

- Live two-player playtest passed ~14.9 km at best approach and never coupled; the planner can't reliably beat hundreds of km (#290), so 10 km was reachable only by hand-flying or luck.
- 35 km sits mid-band of where comparable games and real proximity ops start the terminal phase (20–50 km).
- One retune covers both proximity co-warp and the rendezvous-warp τ handoff; the 100 / 120 m/s velocity terms are untouched. No save break.

### v0.32.0

In-game chat arrives, and the CommNet tells you why you're dark (ADR 0035, #227–#229, #221, #231–#232).

- `~` opens a one-line input: enter broadcasts, leading `@handle` sends a private line, tab completes against the online roster; unknown or offline handles refuse to send and keep your draft.
- Sim keeps running while you type; lines live ~30 s as chips, nothing persisted, never CommNet-gated; chat has its own capped ring so it can't evict presence moments.
- Dock/undock/control-handover session moments (kinds 6/7/8) finally render chips.
- Disconnected probe chips say why (`no station in view: relay needed` vs `out of range: stronger antenna needed`), classified against the live network; spawn form flags degraded-band altitudes sampled per parent body.
- Batch review confirmed nine findings, all fixed pre-release (#272/#273); enrollment-side handle uniqueness follows as #274. No save break.

### v0.31.1

Rendezvous Warp coasts now actually run fast, and the arm survives arrival so the pair stays locked through the whole approach (#248–#253; review follow-ups #259–#261).

- Root cause: min-wins coupling clamped each side to the partner's stale reported rate, so two players engaging at 1× stayed at 1× forever; v0.31.0's "2h19m coast" was this bug.
- Engaged coasts are now exempt from min-wins for the armed partner only; both sides derive the same rate from committed τ (cap 1200×), other clamps still apply.
- The arm is a standing intent: reaching τ demotes it to a waypoint and re-derives the next one; ends only on cancel, proximity handoff, or a dead partner session.
- Warp keys and Auto-Warp refuse during an engaged coast (toast points at `/`); pre-couple window explains why a coast can't start, direction-aware for Sync.
- Degrade watchdog scales to the encounter; Away partner shows as a standing line on chips; review caught a width-2 glyph and a `_arm`-suffixed test file that never compiled. No save break.

### v0.31.0

Commitment Reprieve: a session whose player drops keeps simulating only while a partner is waiting on it (ADR 0036, #243).

- Guest games run inside the server process, so a sleeping client did not stop them; with no idle timeout the owner was locked out behind a dead socket.
- A flat reap would strand co-warp partners mid-coast, so sessions outlive the timeout only while holding a Commitment: Engaged Rendezvous Warp or Docked-as-Guest.
- Proximity Co-Warp does not count; a Reprieve ends when the Commitment resolves, hits its cap, or the player reconnects and takes over the unattended session.
- Session screen gains Away; partners get chips on go-quiet and return; reconnecting players get a replay of moments missed. Solo players are still reaped. No save break.

### v0.30.2

Rendezvous Warp coasts no longer collapse when either player picks a top warp step (#244).

- Cause: at 10000× or 100000× one tick advanced further than the two-minute coupling window, so the pair decoupled and the slowest-wins clamp (a tick stale) could not catch it.
- While armed or coasting, warp is bounded so one tick covers at most half the window (1200× at default step); a four-hour encounter still coasts in about twelve seconds.
- Solo play untouched: the bound lifts when there is no shared clock. No save break.

### v0.30.1

Session screen rows stay aligned, and admin keys now say why they refuse.

- Cause of ragged rows: `%-32s` padding counted invisible ANSI bytes on tagged rows; roster and invites now lay out in terminal cells via `lipgloss.Width` with headers and ellipsised overflow.
- Your own row no longer claims `(0 here)`.
- `[p]`, `[x]`, `[u]` flash a reason on every refusal (can't promote yourself, only the host can..., focus is on the wrong pane) instead of silently no-oping.
- Display only; behaviour and server guardrails unchanged. No save break.

### v0.30.0

Hosts can delegate admins, and the server can be restarted from inside the game (#222–#226, #220).

- Authorization was presentation-only; it is now a capability enforced in the handler, so forged guest intents are refused.
- `p` promotes to admin; admins mint (`i`) and revoke (`r`) invites and remove players (`x`) under single-rooted guardrails (no promoting, no removing host/admins/self).
- `u` drains and restarts the server in-band with progress persisted; where adopt tooling exists it reads "restart to adopt vX.Y.Z", otherwise it points at the releases page.
- `t` on a multi-craft player opens a craft picker (`v`/`w` honor it); roster reads `3 craft (1 here)`. No save or session-directory schema break.

### v0.29.1

Fixes every uncrewed craft reading `⚠ NO SIGNAL` after loading a save.

- Cause: the ground-station catalog is transient, and a world rehydrated from a save never rebuilt it, so no relay chain or home blanket could apply.
- Command gating reads the same graph, so throttle, attitude, nodes, and staging silently refused on uncrewed vessels.
- Hit both single-player continue and multiplayer reconnect; present since v0.22.0. Loading under this version fixes existing saves. No save break.

### v0.29.0

Rendezvous Warp: two players rate-lock and coast together to a predicted closest approach across subspaces (ADR 0034 addendum, PR #218).

- `w` on a player's `O` roster row arms toward predicted CA (K-nudge τ or current-course CA); the target gets a persistent prompt and joins with `y`.
- Once both armed, slowest wins, planted burns fire en route, and arrival at τ drops to 1× already Co-Warped for the Proximity handoff.
- τ is held for the coast; a partner's mid-course change raises a degrade chip rather than re-targeting. Either side cancels with `/`.
- Arm state is transient; one additive wire message. No save or session-directory schema break.

### v0.28.0

Contact cycle: proximity co-warp, cross-player docking, in-game hosting, and spectating (ADR 0034).

- Within 10 km and 100 m/s of a synced player your warp couples to theirs (releases at 12 km / 120 m/s); other players' craft draw as ghost orbits.
- `h` on the `O` roster starts/stops the SSH listener without `--serve`; `v` spectates a player, `f` returns to your craft.
- Docking a co-warped ghost fuses a real stack: docker owns it, guest rides as a `DockedComponent` and can `U` undock; `J` transfers control (refused mid-burn).
- Session-directory schema v1 to v2 with typed migration; local `save.json` untouched.

### v0.27.0

Multiplayer over ssh: host a shared world from your own game, with per-player time and ghost craft (ADR 0034).

- `--serve` embeds a wish SSH listener on 23234; `serve invite <handle>` mints one-time codes; a guest's ssh key becomes their identity after a calibration card and enroll flow.
- Each player has their own save envelope server-side; time is per-player (subspaces) and new players join at the frontier.
- Ghost craft render as Kepler orbits at your clock; the `O` Session screen shows offsets, `t` targets a ghost, `s` Sync-warps forward honoring every warp clamp.
- New terminal size floor: below 104×24 a resize prompt replaces rendering. No local save-schema change.

### v0.26.0

Multi-save system: a `saves/` directory of named saves plus quicksave and autosave lanes (ADR 0033).

- `internal/save` gains a `Meta` header, opaque filenames, header-only listing, and reserved lanes that named saves cannot collide with.
- Old `save.json` auto-imports as "Imported YYYY-MM-DD" and is left in place; `F5`/`F9` use the quicksave lane; quit-autosave rotates through a ring of 3.
- Unified Saves screen from `[Save Game]` and `[Load Game]`: `Enter` load/save, `d` delete, `r` rename, `＋ New save…` row; old pause-menu click-confirms removed.
- Periodic autosave (default 5 min) with a Settings row cycling 0/1/5/10/15/30. No save-schema bump.

### v0.25.0

VAB reframe: edit vehicle rows in place, crack open catalog parts, and chase a Σ Δv target (ADR 0032).

- `←/→` (`h`/`l`) swap the selected row's part within its kind; `tab`/`shift+tab` is the only column switch (keymap break from v0.24.0).
- The engine row leads stage chemistry; empty stages show engine and tank placeholder rows so the build loop needs no palette trip.
- `enter` cracks a shipped part into seed components via a new seed-only `vab_seed` catalog field; two bulk tanks added.
- `t` sets a session-only Σ Δv target shown as `current / target (delta)` with a tank-count hint. No save break.

### v0.24.5

New `p` key cycles RCS pulse size for sub-second orbital trim.

- Pulse Δv steps 0.1 to 0.01 to 0.001 m/s; the PROPELLANT chip shows the active step in RCS mode.
- Still an impulsive quantum, not a throttle; default stays 0.1 m/s. No physics or save change.

### v0.24.4

Orbital-period readouts now show seconds (`6h04m21s`) instead of rounding to the minute.

- Lets a comsat phasing orbit be tuned to about 1 s rather than ±30 s; apoapsis/periapsis countdowns keep minute scale.
- New constellation deployment guide (`constellation-deploy.md`) covers the phasing math. Display only.

### v0.24.3

The ORBIT and PROJECTED ORBIT chips now show the full orbital period.

- Period is `2π√(a³/μ)`, shown under the apsis countdowns; the projected value lets you converge a deployment burn on a target period before firing.
- Display only; no physics, save, or keybinding change.

### v0.24.2

Fixes `NO SIGNAL` in low Earth orbit with a CommNet home-telemetry blanket (ADR 0027 amendment).

- Cause: DSN stations at 35–40° latitude are all occluded from a 500 km orbit whose horizon cone is about 22°.
- A controllable craft within 1.5× body radius of a body hosting ground stations is connected regardless of line-of-sight (about 3,200 km at Earth, 300 km at Kern).
- Range, antennas, and deep-space relay behaviour unchanged. No save break.

### v0.24.1

Every vessel carries a basic antenna, and Kern gets its own DSN ring (ADR 0027 amendment).

- Crewed pods now appear on the CommNet as presence-only nodes; they are still never command-gated and the comms chip stays hidden.
- Kern stations Stdin / Stdout / Stderr sit about 120° apart, so Lumen probes can finally reach the network. No save break.

### v0.24.0

In-game Vehicle Assembly Building plus a grouped, filtered spawn form and a full Lumen fleet (ADRs 0029/0030/0031).

- `Esc → [Build (VAB)]` composes components into stages with a glyph schematic, reorder/duplicate/quantity, and live Δv / TWR / mass; designs save under `~/.config/terminal-space-program/designs/`.
- Spawn form (`n`) groups craft by category, tags crewed/uncrewed, gates launchpad spawn on TWR ≥ 1, and filters by scale class (`[f]` reveals all).
- Lumen fleet ships role-for-role counterparts of every Sol loadout with computing-theme names. No save break.

### v0.23.0

New `Y` Deploy verb releases the top carried payload as its own craft while you keep flying the carrier (ADR 0028, PR #186).

- Distinct from Undock, which switches you to the released piece; `NosePayloadPlan` supports N stacked payloads for one-launch constellations.
- Five starter loadouts (Relay Comsat, Ground-Station Lander, Science Probe, Comsat Carrier ×3, Survey Pack); soft-landing a relay auto-joins the CommNet as a ground station. No save break.

### v0.22.3

Long CommNet relay chains now draw all the way to the destination at any zoom (PR #185).

- New canvas primitive draws the beam without the too-long-chord guard that truncated long hops.
- No save break.

### v0.22.2

CommNet ranges now combine KSP-style, so a strong DSN station extends a weak probe's reach (PR #184).

- Flat 20,000 km cap replaced by combinability: `√(rangeₐ · range_b)`.
- Three antenna tiers: direct-basic (LEO/geo), relay-cislunar (Moon and below), deep-space (Mars-class).
- No save break.

### v0.22.1

Probes near DSN stations no longer strobe NO SIGNAL every tick.

- Cause: floating-point normalization error made ground stations flicker into self-occlusion.
- Fix: small surface tolerance in the line-of-sight test; genuine coverage gaps unchanged. No save break.

### v0.22.0

Parts and loadouts are now data (JSON with user overlays), and probes need a comms link to take commands (ADRs 0026/0027, PR #178).

- Every stage and loadout moved from Go literals to embedded JSON; `--list-loadouts` shows the merged catalog.
- CommNet: per-stage command-source (crewed/probe) and antenna (direct/relay), per-tick relay graph, "NO SIGNAL" gating.
- Comms chip and relay-path beam on the orbit map; crewed vessels are never gated. No save break.

### v0.21.0

Missions arrive: an opt-in tutorial-to-challenge ladder gives the sandbox a purpose (ADR 0025, PRs #171–#177).

- Data-authored Objective→Mission→Program model; two objective families (state predicates, semantic-action events).
- Embedded ladder (orient → plan → fly → Luna → Mars), enabled from Settings, off by default.
- Missions screen shows the gated ladder; an in-flight checklist chip tracks the current step.
- Save v8→v9 (soft: old saves load, mission progress resets).

### v0.20.0

Body textures are now authored as data, and user-overlay systems can ship their own (ADR 0024, PRs #167–#170).

- Generic JSON texture engine with typed Feature Kinds (continents, craters, bands, spots, mask, star).
- All 17 Lumen and 12 Sol bodies migrated; net −1215 lines. No save break.

### v0.19.0

Predicted encounter paths read cleanly instead of wobbling (ADR 0023, PR #166).

- Planted-leg foreign-SOI arcs and in-SOI residence arcs sampled analytically from the encounter conic, not fixed steps.
- No save break.

### v0.18.5

QWERTZ keyboards get a layout preset so every binding stays under the same finger (ADR 0022, PR #165).

- Layout selector in `[Controls]` remaps the physical Y↔Z swap.
- Implemented as ingest-normalize + display-translate; keymap stays authored in QWERTY. No save break.

### v0.18.4

The purple post-burn preview no longer vanishes at ignition, and yaw moves to the shift+arrow keys (PRs #162, #163).

- Dashed orbit, SOI-pass arc, and SOI ring are snapshotted on the last coasting frame and held during the burn.
- Yaw moved from `{`/`}` to `shift+←`/`shift+→` to match the tilt keys. No save break.

### v0.18.3

Encounter arcs now draw body-relative and the camera stops fighting you (ADR 0021, PRs #152–#160).

- Foreign-SOI segments anchored to the encounter body's current position: 47.8× SOI smear collapses to 1.0×.
- Camera Contract: fit once per Framing Event, never per frame.
- Also ships SOI rings, entry/exit markers, in-SOI continuation, and a split-capture aim fix. No save break.

### v0.18.2

Manual zoom now sticks during encounter framing (PR #146).

- v0.18.1's framing re-fit every frame, overwriting `+`/`-` on the next tick.
- Canvas scale is now `baseScale × userZoom`; auto-fit owns the base until the next Framing Event. No save break.

### v0.18.1

Focusing a body with an active SOI pass now frames the encounter instead of landing up to 169× too far away (PR #145).

- All three framers center on the predicted encounter and fit to the drawn arc's extent. No save break.

### v0.18.0

Live SOI Pass prediction and a unified marker set land on the orbit map (ADRs 0019/0020, PRs #138–#142).

- Single-glyph markers (▲▼◇◆⊕✕Δ) replace the FillDisk blobs; all craft become `➤`.
- Always-on foreign-SOI arc, Perilune marker, and SOI PASS chip; dual-arc counterfactual when a node is planted.
- No save break.

### v0.17.3

Planet-to-moon transfers now arrive at the Capture Orbit radius instead of aiming at the body's centre (ADR 0018, PR #131).

- Combined transfer aims at an in-plane offset; capture fires at the analytic hyperbolic perilune.
- Live TARGET readout shows approach data for body targets. No save break.

### v0.17.2

The dashed line on lunar transfers stops wobbling (ADR 0017, PR #128).

- Both SOI-aware propagators default to interpolated body positions, bisected crossing times, 120 s coast sub-step cap.
- Predict-on-change cache keeps the dashed line off the per-frame render path. No save break.

### v0.17.1

Diagnostics only: an env-gated harness measures prediction stability across three reference transfers (PR #127).

- No behaviour change.

### v0.17

Boot straight into any scenario from the command line, no save needed (PR #125).

- `--system`, `--orbit`, `--altitude`, `--inclination`, `--launchpad`/`--launch-site`/`--lat`/`--lon`, `--loadout`.
- `--list-*` discovery flags. No save-schema change.

### v0.16.1

Small hygiene and correctness pass (PRs #122–#124).

- Two-sided guards on the Saturn V ascent test; F1 help gains the `[»Burn]` mouse row.
- `FlightPhase` vocabulary scaffolded. No save break.

### v0.16

`G` Auto-Warp, and each vessel now lives in its own star system (ADRs 0015/0016).

- Auto-Warp runs at the maximum permitted rate and drops to 1× exactly 30 sim-seconds before the next planted burn.
- Vessels bind to a System at spawn: the Kern Stack flies Lumen while a Sol craft orbits in parallel.
- Save v7→v8.

### v0.15

New Lumen system: a 17-body KSP-stock analog at roughly 1/10 scale (ADR 0014, PRs #104–#107).

- About 3.4 km/s to orbit; Scale Class hint on Systems and Loadouts.
- Kern Stack (4-stage Apollo-analog) is the scale-matched vehicle.
- Save break: delete your save file to start fresh.

### v0.14

A CSM+LM can spawn already docked in post-transposition shape (ADR 0011, PR #81).

- `NosePayloadPlan` splits a custom build at the Dock Seam and assembles the composite.
- `[d]` in the configurator cycles the seam; the `csm-lm` module drops the Apollo stack pre-seamed. No save schema bump.

### v0.13

The HUD shrinks to a pinned core chip plus corner chips over a full-width orbit map (ADR 0010).

- Settings screen toggles chip visibility; `F2` declutters.
- Orbit metrics and the active-burn readout are always on.

### v0.12

Plane-aware transfers, a two-stage Lander, and parachutes (ADRs 0005–0008).

- Numbered craft slots; dual-strategy Hohmann (combined Lambert vs. split coplanar raise).
- Analytic-Kepler predictor fidelity; Line-of-Nodes split rendezvous.
- Lander with surface staging; parachutes auto-deploy on dynamic pressure.

### v0.11

Launch chase-cam plus a proper landed/crashed lifecycle (ADR 0004).

- `ViewLaunch` auto-routes on pad launch; `CanSoftLand` decides Touchdown vs. Crashed; `[E]` removes wreckage.
- Lander silhouette with landing legs, hypergolic flame, per-stage launch sprites.

### v0.10

Planner and maneuver tooling round out.

- Rate-limited attitude slew; true plane-match and inclination burns.
- Multi-rev porkchop with short/long-branch picker; rendezvous advisory (`K`); perspective-tilt orbit view.

### v0.9

The fleet grows up: staging, ground launch, rendezvous tooling, navball.

- Unified Target slot; KSP-style staging chain (`space`) with Saturn V.
- Launchpad spawn, surface SAS, pitch trim, LAUNCH HUD.
- TCA/CA/DOCK READY readouts, NavMode cycle `;`, `C` circularize.
- Navball; solar lighting and eclipses.

### v0.8

Multi-craft polish: RCS, docking, drag, and a proper spawn form.

- RCS/monopropellant; multi-craft slate with per-craft burns; craft types and spawn form.
- Docking and undocking; atmospheric drag; sim-time planet rotation with view-aware projection.
- Adaptive warp clamps; iterate-for-target finite burns.

### v0.7

Modding, manual flight, and textured planets.

- External system/theme overlays; manual-flight stick (throttle + attitude keys).
- Inclination-change planner; textured Earth/Moon/Mars/Jupiter.

### v0.6

Planner UX and the first mission scaffold.

- Burn-at-next scheduler, projected-orbit HUD, finite-burn-aware iteration.
- Moon→parent escape planner; click-only mouse.

### v0.5

Moons and visuals.

- Body hierarchy with major moons (Luna, Phobos/Deimos, Galilean, Titan, Enceladus).
- Per-body color, vessel trail, HUD polish.

### v0.4

Save and load arrive.

- Versioned save/load envelope; mid-course refinement.

### v0.3

Interplanetary transfers.

- Lambert solver, porkchop plot, auto-plant Hohmann transfers.

### v0.2

Burns.

- Spacecraft, impulsive burns, finite-burn integrator.

### v0.1

Foundation.

- Heliocentric viewer, Verlet integrator, body catalog.
