# Multiplayer (ssh)

Terminal Space Program hosts a shared session straight from your own game.
Guests join over ssh from any terminal; there is no separate server binary.
This page is the long version. The [README](../README.md#multiplayer-ssh)
has the three-command summary, and [controls.md](controls.md#global) lists
every key on the session roster.

## Hosting

```bash
terminal-space-program --serve                 # play AND accept guests (port 23234)
terminal-space-program serve invite dave       # mint a one-time invite code
ssh -p 23234 your-host                         # guests join from any terminal
```

You can also start hosting from inside a running game, with no restart and no
`--serve`: open the `O` session roster and press `h`. Press it again to stop,
which drops guests but keeps everyone's progress.

Running the session lives on that roster too. The host mints (`i`) and revokes
(`r`) invite codes and removes players (`x`), and can share the load: `p`
promotes a player to **admin**, who then handles invites and removals without
being able to promote anyone else, remove another admin, or remove the host.

`F4` restarts the server: everyone is warned, drained with their progress
saved, and reconnects a moment later. On a box set up to self-update, the same
key adopts a newer release when one is published. It is deliberately a function
key, not a letter: no irreversible admin action shares a key with a flight verb.

## Joining

Guests enroll once with the invite code (their ssh key becomes their identity)
and get their own persistent space program on the host's machine. Each player's
save lives on the host, so you can come back from any terminal.

## Independent time

Everyone warps time **independently**. Other players appear as dim "ghost"
vessels, with their orbits drawn on your map, evaluated at *your* clock, and
the `O` session roster shows who's ahead or behind.

From that roster:

- `t` targets one of their vessels. With more than one in your system it opens
  a picker so you aim at the right vessel.
- `s` **sync-warps** you forward to a player's time to fly formation. Sync is
  forward only; a player behind you syncs to you.
- `v` **spectates** a player, fitting the camera to their ghost orbit and
  following it so you can watch their burns play out. Warp clamps, planted
  burns, and SOI transitions are all honored en route.

## Rendezvous warp

Closing the distance takes orbits of coasting, so warp there *together*.
`w` on a player's roster row arms a **rendezvous warp** toward your predicted
closest approach with them. They get a persistent prompt on their main screen
(`y` joins), and from that moment your warps are rate-locked all the way to
the encounter, planted burns firing en route.

Either side cancels with `/`, and only with `/`: the manual warp keys and the
auto-warp toggle (`G` / `[»Burn]`) are inert while the coast runs (it owns the
rate), so a stray `.` or `G` can't tear the rendezvous down. If the encounter
drifts off the committed approach mid-coast, a warning chip says so. You arrive
at closest approach at 1×, already coupled, and slide straight into the final
approach.

### The terminal phase

The agreement doesn't end at arrival. Arriving hands the vessel back so you
can brake at closest approach, but the two of you stay time-locked through the
whole terminal phase (the burns, the waiting, the last few hundred metres) so
nobody has to sit at 1× while the other lines up.

In that phase the player who *proposed* the rendezvous flies the pair's clock.
Whoever joined rides copilot, and can brake the pair (`,`) or release back to
following (`.`) but never push it faster. Either side burning holds you both
at the burn cap. The `RENDEZVOUS` block on the map always says whose clock
you're on and what's holding it, so a warp key that does nothing always
explains itself.

It ends when you dock, or when either of you presses `/`. Flying 100 km wide
to set up a better approach won't drop it.

## Proximity coupling

Proximity does the rest for players with no agreement. Come within 35 km of a
player you're synced with, closing slower than 100 m/s, and your time-warp
**couples** to theirs, so neither can skip ahead during the approach. A
`TIME LOCK` line says who and at what rate. The session roster's `RANGE`
column shows how close you already are.

## Docking across players

Dock your vessel to theirs and you fly one shared stack.

- The guest can `U` **undock** their own component at any time.
- The pilot can hand the whole stack over with `J` **transfer control**. It
  is refused if the partner isn't in the session; someone has to be there to
  take the stick.
- Nobody ever gets stuck. The pilot's `U` releases a partner's vessel even if
  they've disconnected, and it's waiting for them, safed, when they come back.
  If the pilot is the one who's gone, the guest riding their stack can press
  `J` to take the stick themselves. An empty seat needs nobody's permission.
- After an undock, the pair is held apart even once you settle back inside
  docking range, so a stray touch doesn't silently re-fuse two vessels you
  meant to keep apart. The dock-gate ring in Proximity View (`o`) shows amber
  while it's held. Clearing 100 m, or waiting it out, releases the hold;
  pressing `c` re-arms right where you are.

## Chat

Coordinate it all without leaving the sim: `~` opens **chat** on the flight
view. Type and press `enter` to broadcast to the session; a leading `@handle`
sends a private line. `tab` completes the handle against who's online, and a
typo refuses to send rather than broadcasting by accident.

The sim keeps running while you type, `esc` bails, and messages ride the map
as transient chips for ~30 seconds. Chat is live coordination, not a message
board: nothing is stored, and it never depends on your vessel's CommNet link.
