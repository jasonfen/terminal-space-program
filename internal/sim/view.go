package sim

// ViewMode selects the canvas projection — which world axes map to
// canvas X+ and Y+. v0.6.4+. World-level state so the orbit screen
// and the maneuver-planner mini-canvas share the same camera angle
// without per-screen coordination.
//
// Four hard-coded cardinal views: Top (the pre-v0.6.4 XY drop) plus
// three side views. Top → Right → Bottom → Left wraps around,
// matching the player's mental model of rotating the camera around
// the system.
type ViewMode int

const (
	// ViewTop is the pre-v0.6.4 default: drop world Z, project onto
	// world (X, Y). Equatorial orbits read as ellipses; inclined
	// orbits foreshorten. Zero-value preserves backwards behaviour.
	ViewTop ViewMode = iota
	// ViewRight looks at the system from world +X toward origin:
	// canvas X+ = world Y+, canvas Y+ = world Z+. An equatorial
	// orbit appears edge-on as a horizontal line passing through
	// the body's silhouette — useful for "watch the craft swing
	// around the back of the planet" geometry that Top hides.
	ViewRight
	// ViewBottom mirrors ViewTop vertically — looking up from -Z.
	// Same world-axes projection as Top with canvas Y inverted, so
	// the same orbit reads with N / S flipped. Useful when the
	// player wants the moon "below" the apsidal line for spatial
	// orientation.
	ViewBottom
	// ViewLeft mirrors ViewRight horizontally — looking from -X
	// toward origin. Canvas X+ = world Y-, canvas Y+ = world Z+.
	ViewLeft
)

// String returns a short human label for the view mode.
func (m ViewMode) String() string {
	switch m {
	case ViewTop:
		return "top"
	case ViewRight:
		return "right"
	case ViewBottom:
		return "bottom"
	case ViewLeft:
		return "left"
	}
	return "?"
}

// AllViewModes enumerates the modes in canonical cycle order.
// Top → Right → Bottom → Left → Top — each cycle rotates the camera
// 90° around the system in a consistent direction.
var AllViewModes = [...]ViewMode{
	ViewTop,
	ViewRight,
	ViewBottom,
	ViewLeft,
}

// CycleViewMode advances ViewMode to the next mode in cycle order.
// Wraps around — modes are a small finite set.
func (w *World) CycleViewMode() {
	w.ViewMode = (w.ViewMode + 1) % ViewMode(len(AllViewModes))
}
