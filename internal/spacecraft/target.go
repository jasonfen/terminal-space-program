package spacecraft

// TargetKind enumerates what a craft is aiming at. Type lives on
// `spacecraft` (not `sim`) so each Spacecraft can carry its own
// per-craft target as a struct field — moved from sim/target.go in
// v0.9.3 polish to support per-craft target binding (each vessel
// remembers its own target across active-craft switches).
//
// `sim` re-exports the type via an alias so existing readers
// (`w.Target.Kind == sim.TargetCraft`) continue to compile unchanged.
type TargetKind int

const (
	// TargetNone — no target set. Planners that consume Target fall
	// back to their kind-less default (equatorial inclination match,
	// "pick a body cursor first" status flash).
	TargetNone TargetKind = iota
	// TargetBody references a body by index in System().Bodies.
	TargetBody
	// TargetCraft references a non-active craft by its stable
	// Spacecraft.ID (v0.14.x / ADR 0012; was a World.Crafts index).
	TargetCraft
	// TargetSite is reserved; not populated until landing-site
	// targeting ships post-v0.9.
	TargetSite
	// TargetGhost references another player's craft by owner
	// fingerprint + craft ID (v0.27 S6, ADR 0034). Resolves against
	// the world's transient ghost slate — an unresolved ref (owner
	// gone, craft staged away, or not yet re-latched post-reconnect,
	// #294) has no relative state to compute with (nav/burn consumers
	// must check the resolve ok and degrade gracefully — see
	// World.HasRelativeTarget), but the lock itself is NOT the same as
	// TargetNone: Kind stays TargetGhost so a later resolve re-latches
	// it, and TargetName reports a pending state rather than "".
	TargetGhost
)

// Target identifies what a single craft is aiming at. The zero
// value (TargetNone) is the v0.9.0 default and round-trips through
// save as an absent JSON field. v0.9.3+ : every Spacecraft holds its
// own Target value so per-craft targeting persists across active-
// craft switches.
type Target struct {
	Kind    TargetKind
	BodyIdx int    // when Kind==TargetBody
	CraftID uint64 // when Kind==TargetCraft or TargetGhost — the target's stable Spacecraft.ID (ADR 0012)

	// GhostOwner names the remote player (key fingerprint) when
	// Kind==TargetGhost (v0.27 S6, ADR 0034). The fingerprint is
	// meaningful within the session it was set in: #294 preserves it
	// across save/load (CraftToWire/CraftFromWire) instead of
	// normalising the target away, so a guest's cross-player rendezvous
	// lock survives a reconnect. A ref that never resolves against the
	// live ghost slate (single-player, or a session whose target owner
	// never came back) just carries a lock that stays pending — see
	// World.HasRelativeTarget / World.TargetName.
	GhostOwner string `json:"ghost_owner,omitempty"`
}
