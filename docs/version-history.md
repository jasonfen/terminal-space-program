# Version history

Newest first. One headline per release, then the concrete changes and the issues they closed.

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
- #426: Flight School grows to five rungs (Orientation, Plan a Burn, Fly It, Off the Pad, Meet & Dock), each step an objective in its own words; "aim for the Moon" credits only when TARGET reads Moon and "plant a transfer" only a Moon transfer within your Δv budget (event objectives gain `require_*` gates; `ManeuverNode.OverBudget` is the one over-budget predicate shared by the NODES chip, the planner and the evaluator); the ORBIT chip gains an `e:` row so the eccentricity-graded rungs have a number to check; the MISSION chip shows `relays online N/3` during Relay Constellation; the planner carries a one-line `MISSION ▸` reminder on tutorial rungs; the ladder screen splits into FLIGHT SCHOOL and CHALLENGES lists with their own counts, locked rungs wear `⚿`, an off program offers `[1]`/`[2]` to turn it on, and finishing a program shows a Sendoff; `Rendezvous & Dock` is a root rung and `Mars Flyby` now needs `Luna Capture`. Choosing or abandoning a rung is deferred (needs persisted state).
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
