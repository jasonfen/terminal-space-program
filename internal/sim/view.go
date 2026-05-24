package sim

// ViewMode selects the canvas projection — which world axes map to
// canvas X+ and Y+. v0.6.4+. World-level state so the orbit screen
// and the maneuver-planner mini-canvas share the same camera angle
// without per-screen coordination.
//
// Six modes: the v0.10.6 perspective-tilt view (the new zero-value
// default), four hard-coded cardinal views (Top, Right, Bottom,
// Left), plus the orbit-flat view that projects onto the active
// craft's orbit plane regardless of inclination. Cycle order opens
// on the tilted projection, walks through the cardinals, and ends
// on orbit-flat as punctuation before wrapping.
type ViewMode int

const (
	// ViewTilted (v0.10.6+) renders the active craft's perifocal basis
	// with a polar tilt θ + yaw φ (sourced from World.ViewTilt). When
	// the active craft has no valid orbit (Landed / hyperbolic /
	// degenerate / no craft) the basis falls back to a tilted world-
	// axis basis so the depth cue stays alive on the pad. Prepended
	// to the cycle so it is the iota zero-value — a freshly spawned
	// World opens here, replacing pre-v0.10.6's ViewTop default. No
	// save migration: ViewMode is a per-session UI preference, not
	// persisted (world.go:60-70).
	ViewTilted ViewMode = iota
	// ViewTop is the pre-v0.6.4 default: drop world Z, project onto
	// world (X, Y). Equatorial orbits read as ellipses; inclined
	// orbits foreshorten.
	ViewTop
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
	case ViewTilted:
		return "tilted"
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
// Tilted → Top → Right → Bottom → Left → OrbitFlat → Tilted —
// the v0.10.6+ tilt opens the cycle as the new zero-value default,
// the four cardinal cameras follow (each rotates 90° around the
// system), and orbit-flat lands last as punctuation before wrapping.
var AllViewModes = [...]ViewMode{
	ViewTilted,
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

// ViewTilt holds the polar tilt θ and yaw φ (degrees) that
// ViewTilted applies to the projection basis. v0.10.6+. Per-session
// UI state — not persisted to save (same convention as ViewMode and
// InstantSAS). Theta is player-tunable via shift+up / shift+down at
// the orbit screen; Phi is reserved for v0.10.7's launch-anchor
// (player controls deferred to a post-ship playtest signal).
type ViewTilt struct {
	Theta float64
	Phi   float64
}

// DefaultViewTilt returns the starting (Theta, Phi) for a freshly
// constructed World. 25° polar tilt, 0° yaw — KSP defaults to ~30°
// but the terminal canvas's 2:4 braille aspect makes foreshortening
// read stronger than a graphical UI, so a touch less keeps inclined
// orbits from looking squashed. Tune in flight (shift+up / shift+down).
func DefaultViewTilt() ViewTilt {
	return ViewTilt{Theta: 25, Phi: 0}
}

// ViewTiltThetaMinDeg / ViewTiltThetaMaxDeg clamp the player's
// shift+up / shift+down nudges. 0° collapses the tilt back to the
// world-axis ViewTop projection (identity rotation); 60° pushes
// foreshortening to where the orbit ellipse starts to read as a
// horizontal cigar. Step is 5° per press.
const (
	ViewTiltThetaMinDeg = 0.0
	ViewTiltThetaMaxDeg = 60.0
	ViewTiltThetaStep   = 5.0
)

// NudgeViewTiltTheta adds delta degrees to ViewTilt.Theta and clamps
// to [ViewTiltThetaMinDeg, ViewTiltThetaMaxDeg]. v0.10.6+. Returns
// the resulting Theta so the caller can stamp it into a status flash.
func (w *World) NudgeViewTiltTheta(deltaDeg float64) float64 {
	w.ViewTilt.Theta += deltaDeg
	if w.ViewTilt.Theta < ViewTiltThetaMinDeg {
		w.ViewTilt.Theta = ViewTiltThetaMinDeg
	}
	if w.ViewTilt.Theta > ViewTiltThetaMaxDeg {
		w.ViewTilt.Theta = ViewTiltThetaMaxDeg
	}
	return w.ViewTilt.Theta
}
