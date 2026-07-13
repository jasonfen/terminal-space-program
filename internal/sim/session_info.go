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
}

// SessionInvite is one outstanding invite code (host's screen only).
type SessionInvite struct {
	Code   string
	Handle string
	Age    time.Duration
}

// SessionInfo is the Session screen's whole slate.
type SessionInfo struct {
	IsHost  bool
	Self    string // viewer's fingerprint — the screen marks "you"
	Players []SessionPlayer
	Invites []SessionInvite // populated only for the host
}

// SessionEventKind enumerates the moments the chip stack surfaces.
type SessionEventKind int

const (
	SessionEventJoin SessionEventKind = iota
	SessionEventLeave
	SessionEventSync     // someone arrived at your subspace ("X synced to you")
	SessionEventSyncedTo // you arrived at theirs ("synced to X") — local only, never broadcast
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
}
