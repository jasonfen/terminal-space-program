package sim

// ViewMode selects the canvas projection — which world axes map to
// canvas X+ and Y+. v0.6.4+. World-level state so the orbit screen
// and the maneuver-planner mini-canvas share the same camera angle
// without per-screen coordination.
//
// Five modes: four hard-coded cardinal views (Top, Right, Bottom,
// Left) plus the orbit-flat view that projects onto the active
// craft's orbit plane regardless of inclination. Cycle order rolls
// from Top through the cardinals and ends on orbit-flat before
// wrapping.
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
	// ViewOrbitFlat projects onto the active craft's orbit plane
	// via the perifocal (x̂, ŷ) basis. Inclined orbits render as
	// clean ellipses with no foreshortening — the geometry the
	// other views can't reveal because they're tied to world axes.
	// Falls back to ViewTop's basis when the orbit is degenerate
	// (no craft, e ≥ 1, a ≤ 0). Useful for reading the orbit's
	// actual shape as if i = 0.
	ViewOrbitFlat
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
	case ViewOrbitFlat:
		return "orbit-flat"
	}
	return "?"
}

// AllViewModes enumerates the modes in canonical cycle order.
// Top → Right → Bottom → Left → OrbitFlat → Top — cardinal cameras
// first (each rotates 90° around the system), then the orbit-plane
// projection as a punctuation mark.
var AllViewModes = [...]ViewMode{
	ViewTop,
	ViewRight,
	ViewBottom,
	ViewLeft,
	ViewOrbitFlat,
}

// CycleViewMode advances ViewMode to the next mode in cycle order.
// Wraps around — modes are a small finite set.
func (w *World) CycleViewMode() {
	w.ViewMode = (w.ViewMode + 1) % ViewMode(len(AllViewModes))
}
