# Multiplayer — design spike

**Status:** v0.6.6 design-doc spike. No code, no commitments. The
goal is to surface the constraints that an actual MP implementation
will have to live inside, not to pick a stack. Numbered open questions
at the end.

## Why now

Single-player TSP is a deterministic Verlet/Kepler integrator over a
known body catalog. Two players watching the same Solar System
simultaneously is a natural extension — but the existing engine bakes
in two assumptions that don't survive the jump (a global time-warp
multiplier and a single in-process World), and it's cheaper to know
that *now*, while the v0.4 save schema is still pliable, than after a
network protocol has shipped.

## Transport

Three plausible choices, ordered by build cost:

- **WebSocket over TLS.** Cheapest to ship: the existing browser-
  reachable `tsp.buffalo-wahoo.ts.net` Caddy fronting could terminate
  TLS and proxy a Go `nhooyr.io/websocket` server with no new cert
  surface. Ordered, reliable, ~30–80 ms per hop on a residential
  link. Acceptable for state-snapshot replication; less ideal if any
  packet loss spikes block already-late deltas behind a HOL stall.
- **QUIC** (`quic-go`). Adds out-of-order stream delivery and 0-RTT
  resumption — useful when the time-warp authority bursts a snapshot
  every N seconds and a single dropped packet shouldn't block the next
  one. Cost: a new UDP/443 listener, NAT traversal closer to "WebRTC
  hard mode" than HTTP/TLS easy mode. Worth it once we see real loss
  patterns; over-engineered for a 2-player co-op MVP.
- **Custom UDP.** Full control over send rates, redundancy, and
  ack-window shape — and full ownership of NAT traversal, congestion,
  and security review. Reserved for a hypothetical "high-warp dogfight"
  mode that doesn't exist yet.

**Tentative pick:** WebSocket for the MVP. QUIC if the snapshot rate
ever has to scale past ~5 Hz under loss. Custom UDP only if a future
slice introduces sub-second-relevant action (it currently doesn't —
TSP is a planning game, not a twitch one).

## Authority model

This is the deal-breaker section. TSP's clock has a global warp
multiplier (1×, 100×, 10000× …) chosen by the player, and the
integrator's correctness depends on every body and the craft sharing
that multiplier. With two players the multiplier is no longer a
private knob:

- **Host-authoritative + warp-vote.** One peer owns the World; the
  other receives state snapshots and renders. Either peer can
  *propose* a warp change, but the host decides; the client rolls
  forward at whatever rate the snapshot stream implies. Easy to
  implement on top of the existing `World.Clock` (the host runs
  Tick; the client just paints). Cost: a player can't unilaterally
  warp ahead while the other is mid-burn. Probably the right
  trade-off — the existing single-player UX already pauses on burn
  start, and "warp blocked while peer is burning" is a small extension.
- **Lockstep / deterministic replay.** Each peer runs Tick locally;
  inputs (warp changes, node plants, system swaps) are exchanged and
  every peer applies them at the same sim-time. Cleanly solves
  warp-divergence — both peers are *always* at the same SimTime by
  construction — but requires bit-identical floating-point physics
  across machines, which Verlet + Kepler in Go is *probably* not
  (math intrinsics differ across architectures; we already see arm64
  vs amd64 numerical drift in CI). Failure mode: silent state
  divergence after several minutes of warp.
- **Per-player branches with rebase-on-rejoin.** Each peer has their
  own World; reconciliation runs only on key events (entering a
  shared SOI, intersecting orbits). Effectively single-player with
  optional encounters. Cheapest to ship, breaks the "we're playing
  the same Solar System" UX that motivates MP in the first place.

**Tentative pick:** Host-authoritative with a warp-arbitration rule:
warp can only increase when both peers' active-burn count is zero,
and decreases unilaterally to whichever peer requests it first.
Lockstep stays available as a fallback if snapshot bandwidth becomes
prohibitive.

## Persistence

The v0.4 save envelope (`File{Version, Generator, ClockT0,
BodyCatalogHash, Payload}`) was deliberately built with a future
multiplayer `session` block in mind — see `internal/save/save.go` and
the v0.4.0 entry in `docs/state-of-game.md` §3. The shape question is
*where* the new block sits:

- **Inside `Payload`.** Symmetric with `Craft`, `Nodes`,
  `ActiveBurn`, `Missions`. Loads as a single round-trip; trivial to
  v3 → v4 migrate (add an optional `Session *Session` field with
  `omitempty`). Implies "the session is part of the world state,"
  which is true for host-authoritative.
- **Sibling of `Payload`.** Promotes `session` to a peer-level
  envelope concern. Cleaner if a single save file ever has to
  represent multiple players' independent payloads; fits the
  per-player-branches model. Adds a level of nesting for the common
  single-player path that doesn't need it.

**Tentative pick:** inside `Payload`, schema bump v3 → v4 when the
real implementation lands. The host writes its World; the client
either (a) writes a thin "I joined session X" pointer save with the
host's snapshot URL, or (b) writes a host-authoritative snapshot
copy and on rejoin re-syncs from whichever timestamp is newer. Option
(a) is closer to how multiplayer TSP would *feel* (single shared
world, two participants), so probably the right shape.

Conflict resolution on rejoin is the genuinely hard sub-problem and
worth its own design doc — outline only here: timestamp-vector
clocks per player + last-writer-wins on the `Craft` block, with the
host's `Clock.SimTime` as tiebreaker. Missions and Nodes are
per-player (see Open Questions §3).

## Out of scope

- Implementation roadmap, target release, branch / tag plan.
- Cheating / tamper-evident snapshots.
- Voice / text chat (use the existing SideChat surface if needed).
- 3+ player sessions; the constraints above assume 2 peers.

## Open questions

1. Does multi-craft (the v0.7+ "active-craft selector" backlog item)
   land before or after MP? If after, MP-host's `World.Craft` is the
   only craft a client sees and the protocol is simpler. If before,
   the snapshot has to carry a craft list and the client UI gains a
   "watching peer's craft" mode.
2. Does the warp-arbitration rule (host-authoritative §) need a
   third player veto? With 2 peers, "either peer's active-burn
   blocks warp ≥ N" is unambiguous; with 3 peers it's a quorum
   problem.
3. Are missions per-player (each peer has independent objectives) or
   shared (both peers race / cooperate on the same objective list)?
   The v0.6.5 starter catalog is single-player by construction — the
   answer determines whether `Missions` lives inside the per-player
   `Payload` or alongside `Session`.
