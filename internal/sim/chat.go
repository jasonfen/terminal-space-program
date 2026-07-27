package sim

import "time"

// ChatLine is one transient chat message (ADR 0035). Same contract as
// SessionEvent: serve-written, read by screens, never persisted, absent
// in single-player. Chat is a live co-op coordination tool — lines die
// with the server process and are stamped in wall clock, never sim time
// (players hold independent subspace times, so no sim stamp is
// displayable).
type ChatLine struct {
	Owner  string // sender fingerprint — filtering only, never rendered
	Handle string // sender handle, as rendered
	Text   string

	// To addresses a DM at one player (fingerprint); empty = broadcast.
	// ToHandle carries the target's handle so the sender's own echo can
	// render the visibly-distinct "→handle:" form.
	To       string
	ToHandle string

	At time.Time // wall clock: chat expires by real seconds regardless of warp
}
