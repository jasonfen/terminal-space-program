package sim

import "time"

// Multiplayer session display state (v0.27 S6, ADR 0034). Like
// World.Ghosts, everything here is written each tick by the serve
// layer and only read by screens — transient, never persisted, nil in
// single-player. The Session screen renders it; the orbit chip stack
// surfaces recent SessionEvents.

// SessionPlayer is one roster row as the viewer sees it.
type SessionPlayer struct {
	Fingerprint string
	Handle      string
	Role        string // sessiondir.RoleHost / RoleGuest (plain strings here to keep sim below the store)
	Online      bool

	// Last-known flight state, from the session store. Zero values
	// mean "no report yet" (offline since before this server run).
	System     string
	Primary    string
	CraftCount int

	// DeltaT is their subspace time minus the viewer's — positive
	// means they're ahead. Meaningless (and false) when HasReport is
	// false.
	HasReport bool
	DeltaT    time.Duration

	// DockedGuest marks a player riding someone's stack. Inert until
	// the v0.28 "touch" cycle ships cross-player docking.
	DockedGuest bool

	// Away marks a player whose session is still simulating but who has
	// gone silent (ADR 0036). Distinct from Online being false, which
	// means the session is gone: an Away player's craft keep flying, keep
	// holding the frontier, and keep whatever Commitment earned them a
	// Reprieve — there is simply nobody at the controls.
	Away bool

	// RangeM is the live range from the viewer's active craft to this
	// player's nearest craft in the same SOI (ADR 0037 §5) — the column
	// that makes the 35 km warp-lock neighbourhood learnable by watching a
	// number close, rather than by crossing an invisible line. HasRange is
	// false when there is nothing to measure (no anchor, no same-primary
	// craft, no report), and the roster then renders a blank instead of a
	// zero distance.
	HasRange bool
	RangeM   float64

	// WantsRendezvous / RendezvousOut are the roster-row Rendezvous Warp
	// markers (v0.29 S2): this player is armed toward the viewer awaiting
	// a response / the viewer is armed toward this player. Both render as
	// row tags on the Session screen.
	WantsRendezvous bool
	RendezvousOut   bool
}

// SessionInvite is one outstanding invite code (host's screen only).
type SessionInvite struct {
	Code   string
	Handle string
	Age    time.Duration
}

// SessionInfo is the Session screen's whole slate.
type SessionInfo struct {
	IsHost bool // viewer is the session's root host (promote/demote, stop-hosting)
	// CanAdminister is true for the host and any promoted admin (v0.30
	// S2): the invite pane and mint/revoke are gated on this, not on
	// IsHost. Authorization is still enforced in the serve handler — this
	// only drives what the screen offers.
	CanAdminister bool
	Self          string // viewer's fingerprint — the screen marks "you"
	Players       []SessionPlayer
	Invites       []SessionInvite // populated for the host and admins

	// Version surface (v0.30 S5). RunningVersion is always set on a
	// server; AvailableVersion is the newest published release when one is
	// newer than running (else ""); AdoptCapable is whether the supervisor
	// signalled adopt-capability — only then is the [u] restart-to-adopt
	// affordance offered (else the screen points at the manual update
	// path). The readout is universal; only the adopt action is gated.
	RunningVersion   string
	AvailableVersion string
	AdoptCapable     bool
}

// SessionEventKind enumerates the moments the chip stack surfaces.
type SessionEventKind int

const (
	SessionEventJoin SessionEventKind = iota
	SessionEventLeave
	SessionEventSync           // someone arrived at your subspace ("X synced to you")
	SessionEventSyncedTo       // you arrived at theirs ("synced to X") — local only, never broadcast
	SessionEventCoWarpCoupled  // co-warp coupled with a nearby player (v0.28 S1) — local only
	SessionEventCoWarpReleased // co-warp released on separation (v0.28 S1) — local only
	SessionEventDocked         // cross-player dock fused ("docked with X", v0.28 S5)
	SessionEventUndocked       // cross-player stack split ("undocked from X", v0.28 S5)
	SessionEventTransfer       // stack control handed over ("control → X", v0.28 S5)

	// SessionEventUndockRefused: my undock-as-guest was refused on the stack
	// owner's side — my components are no longer the top of the stack, so
	// peeling them would swap the two players' vehicles (#307). Addressed at
	// the guest, who pressed the key and is still docked.
	SessionEventUndockRefused
	// SessionEventDockLost: the cross-player stack this dock named no longer
	// exists (#309) — the craft riding in it went with it. Addressed at the
	// guest, whose docked-as-guest marker would otherwise just vanish.
	SessionEventDockLost

	// SessionEventTransferRefused: my [J] was refused before anything moved
	// (ADR 0040 §2) — most often because the partner has no live session, so
	// there is nobody there to take the stick. Detail carries the reason
	// verbatim; addressed at the player who pressed the key.
	SessionEventTransferRefused
	// SessionEventParcelReturned: a craft the owner released while I was away
	// arrived with me on connect (ADR 0040 §3). Distinct from
	// SessionEventUndocked because I did not ask for it and was not there —
	// the chip has to account for a ship appearing on my slate.
	SessionEventParcelReturned
	// SessionEventControlReclaimed: the stack I owned was taken back from my
	// empty seat by the guest riding in it (ADR 0040 §4). Addressed at the
	// returning owner, who would otherwise find their vehicle simply gone.
	SessionEventControlReclaimed

	// Rendezvous Warp moments (v0.29 S2) — all local-only: each side's
	// serve wrapper derives them from its own World transitions.
	SessionEventRendezvousArmed     // a partner armed toward you ("X wants to rendezvous")
	SessionEventRendezvousArrived   // a waypoint arrived inside couple range — the proximity handoff ("rendezvous with X — encounter reached")
	SessionEventRendezvousCancelled // the arm/coast was released by a cancel/retract ("rendezvous with X cancelled")
	SessionEventRendezvousDegraded  // the held encounter slipped past the committed approach
	SessionEventRendezvousWaypoint  // a waypoint passed outside couple range — the standing intent advanced (#252)

	// SessionEventServerRestart announces an admin-triggered graceful
	// restart to every connected player before the listener drains
	// (v0.30 S4) — a warning, not a silent drop; progress persists and a
	// reconnect resumes.
	SessionEventServerRestart

	// Reprieve moments (ADR 0036), addressed at the player holding a
	// Commitment with the one who went silent — the person whose own
	// flight now depends on an empty chair.
	SessionEventWentQuiet // their peer stopped answering; the Commitment holds the session up
	SessionEventBack      // they are answering again (woken, or reconnected and displaced)
	SessionEventTimedOut  // the session ended while away — nobody ever came back

	// SessionEventResumed opens the replay of everything that happened
	// while this player's own session ran unattended — local only, and
	// the only event carrying Elapsed.
	SessionEventResumed
)

// SessionEvent is a transient session moment (join / leave / sync —
// the v0.13 chip vocabulary). At is wall clock: chips expire by real
// seconds regardless of warp. Owner (fingerprint) is never rendered —
// the serve layer uses it to keep your own join out of your chips.
type SessionEvent struct {
	Kind   SessionEventKind
	Owner  string
	Handle string
	At     time.Time

	// To addresses an event at one player (fingerprint): a Sync event
	// is only meaningful to the player whose subspace was joined.
	// Empty means broadcast (join/leave).
	To string

	// Detail is extra display context for events that need it — ADR 0036
	// uses it for which Commitment is holding an away session up
	// ("rendezvous" / "dock"), so the partner learns what is at stake
	// rather than only that someone went quiet.
	Detail string

	// Elapsed is how much sim-time ran unattended, carried by
	// SessionEventResumed (ADR 0036 S6). A player who reconnects after
	// hours away lands in a world whose clock jumped; this is the number
	// that accounts for it. Zero on every other kind.
	Elapsed time.Duration
}
