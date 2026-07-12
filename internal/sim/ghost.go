package sim

import "github.com/jasonfen/terminal-space-program/internal/orbital"

// Ghost is one remote player's craft placed at this world's sim-time
// (v0.27 S5, ADR 0034): the last-reported orbit evaluated analytically
// at the viewer's clock. Honest staleness by design — it's where
// they'd be if they kept coasting; a burn on their side invalidates it
// until the next report corrects. Pure display data: the orbit screen
// draws it dim with the owner's handle; physics never sees it.
type Ghost struct {
	Owner     string // ssh key fingerprint (roster identity)
	Handle    string // display name, joined from the session roster
	Name      string // craft name
	Glyph     string // craft glyph (may be empty)
	PrimaryID string // SOI primary the ghost orbits
	Pos       orbital.Vec3
}
