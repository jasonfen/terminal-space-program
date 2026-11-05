package sim

import "github.com/jasonfen/terminal-space-program/internal/orbital"

// Ghost is one remote player's craft placed at this world's sim-time
// (v0.27 S5, ADR 0034): the last-reported orbit evaluated analytically
// at the viewer's clock. Honest staleness by design — it's where
// they'd be if they kept coasting; a burn on their side invalidates it
// until the next report corrects. Pure display data: the orbit screen
// draws it dim with the owner's handle; physics never sees it.
type Ghost struct {
	Owner     string       // ssh key fingerprint (roster identity)
	CraftID   uint64       // the remote craft's stable ID (target resolution)
	Handle    string       // display name, joined from the session roster
	Name      string       // craft name
	Glyph     string       // craft glyph (may be empty)
	PrimaryID string       // SOI primary the ghost orbits
	Pos       orbital.Vec3 // world-frame position at this world's sim-time
	RelPos    orbital.Vec3 // primary-relative position at this world's sim-time
	Vel       orbital.Vec3 // primary-relative velocity at this world's sim-time

	// The reported hull (ADR 0053 decision 9, slice E). Attitude is the unit
	// nose direction as of the report (a turn after it is not seen; Kepler
	// moves the position, not the pointing). A zero Attitude means no shape
	// is known: an older report, or a craft that never reported one. NoseM /
	// WidthM are the hull length and width, Ports the free ports (a list).
	Attitude orbital.Vec3
	NoseM    float64
	WidthM   float64
	Ports    []GhostPort

	// Landed ghosts (ADR 0066 decision 4): parked on the surface at
	// LatDeg/LonDeg, placed by body rotation at the viewer's clock, so Pos
	// and Vel (omega x R) are a surface point, not an orbit. The antenna
	// fields are the owner's report, kept for the relay work in S8.
	Landed        bool
	LatDeg        float64
	LonDeg        float64
	AntennaKind   string
	AntennaRangeM float64
	Controllable  bool
}

// GhostPort is one reported docking port: its offset from the ghost's
// position along its attitude, and the component it is on.
type GhostPort struct {
	OffsetM float64
	Name    string
}
