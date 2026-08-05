package sim

import "math"

// ViewMode selects the canvas projection — which world axes map to
// canvas X+ and Y+. v0.6.4+. World-level state so the orbit screen
// and the maneuver-planner mini-canvas share the same camera angle
// without per-screen coordination.
//
// Eight values, but two different shapes (issue #348 §4 / ADR 0043).
// Six are pure projections, cycled by the `v` key
// (ProjectionViewModes): the v0.10.6 perspective-tilt view (the
// zero-value default), four hard-coded cardinal views (Top, Right,
// Bottom, Left), and the orbit-flat view that projects onto the
// active craft's orbit plane regardless of inclination — for these,
// per the Camera Contract (ADR 0021), a ViewMode never picks the
// camera's center or zoom, only which projection. The other two —
// ViewLaunch and ViewProximity — are scenes, not projections: each is
// reached by its own toggling jump key (V, o) rather than the `v`
// cycle, and each re-centres and re-scales the camera on entry as its
// own Framing Event. The v0.17.3 ViewTarget and v0.18.0 ViewSOIPass
// auto-framing views are retired (ADR 0021 D) — reading an encounter
// is "focus the pass Body", and the Local-to-Body arc draws the
// capture curve there.
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
	// ViewLaunch (v0.11.0+) is the chase-cam launch scene — a
	// human-scale side view with the rocket centred, the horizon
	// curving below in Body.SurfaceColor, and a body-fixed pad
	// marker + breadcrumb trail. Routed into automatically on
	// active-slot Landed-false→true transitions (a named Camera
	// Contract carve-out — it answers the player's launch command);
	// also entered and left manually by the toggling `V` jump key
	// (issue #348 §4, ToggleLaunchView) — press to enter with a fresh
	// altitude-derived Framing Event, press again to return to the
	// map exactly as it was left. NOT a `v`-cycle stop (see
	// ProjectionViewModes) — ADR 0021 D's retired apoapsis-floor
	// auto-release means no ambient sim state moves the camera either
	// way. Placed after the six projections so ViewTilted stays the
	// zero-value default. ADR-0002 captures the rationale for shipping
	// this as a distinct ViewMode instead of extending ViewTilted.
	ViewLaunch
	// ViewProximity (ADR 0043) is the target-relative close-range view:
	// the Target sits at the canvas centre in LVLH orientation (its
	// orbital prograde screen-right, the primary screen-down) and the
	// active vessel is drawn at its true relative position and scale.
	// The one ViewMode that is NOT a pure projection of the world frame —
	// it re-centres and re-orients on the Target — which is precisely why
	// the world-frame views cannot substitute for it: at docking range
	// both vessels sweep the canvas at orbital speed while the metres
	// between them stay sub-pixel.
	//
	// Target-gated: reachable only while the Target is a vessel or a
	// remote player's ghost (see World.ProximityAvailable). Entered and
	// left by the toggling `o` jump key only — NOT a `v`-cycle stop
	// (issue #348 §4; see ProjectionViewModes). Placed after ViewLaunch
	// so ViewTilted stays the zero-value default.
	ViewProximity
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
	case ViewLaunch:
		return "launch"
	case ViewProximity:
		return "proximity"
	}
	return "?"
}

// AllViewModes enumerates EVERY ViewMode value, Launch and Proximity
// included — used for completeness checks (String() distinctness and
// the like), never for the `v`-key cycle. That cycle is
// ProjectionViewModes below (issue #348 §4): Launch and Proximity are
// scenes reached by their own toggling jump keys, not cycle stops.
var AllViewModes = [...]ViewMode{
	ViewTilted,
	ViewTop,
	ViewRight,
	ViewBottom,
	ViewLeft,
	ViewOrbitFlat,
	ViewLaunch,
	ViewProximity,
}

// ProjectionViewModes enumerates the SIX map projections the `v` key
// cycles, in canonical order: Tilted → Top → Right → Bottom → Left →
// OrbitFlat, then wraps. The v0.10.6+ tilt opens the cycle as the
// zero-value default, the four cardinal cameras follow (each rotates
// 90° around the system), and orbit-flat lands as punctuation before
// the wrap.
//
// Issue #348 §4 (ADR 0043) removed ViewLaunch and ViewProximity from
// this cycle — each is now reached only by its own toggling jump key
// (`V`, `o`) — so unlike the old AllViewModes-based cycle, these six
// values are NOT the full ViewMode enumeration. They do stay
// contiguous 0..5 in this order, though, so CycleViewMode can still
// advance by modular arithmetic on the ordinal rather than an index
// lookup.
var ProjectionViewModes = [...]ViewMode{
	ViewTilted,
	ViewTop,
	ViewRight,
	ViewBottom,
	ViewLeft,
	ViewOrbitFlat,
}

// baseProjection resolves the map projection CycleViewMode should treat
// as m's position in the six-stop cycle. For a plain projection this is
// m itself. For ViewLaunch / ViewProximity — issue #348 §4 took both out
// of the cycle, so pressing `v` from inside one has to anchor on
// something — it's the return address the toggle key would restore
// (PrevViewMode / ProxReturnView), unwound across a stacked jump (e.g.
// jumping to Proximity FROM Launch, which ProxReturnView's own doc
// comment anticipates) until it lands on a real projection. Bounded to a
// few hops and defaults to ViewTilted if it somehow doesn't resolve —
// PrevViewMode/ProxReturnView are only ever seeded from a prior ViewMode,
// so a chain this long should never happen, but CycleViewMode must never
// spin on a corrupted chain.
func (w *World) baseProjection(m ViewMode) ViewMode {
	for i := 0; i < 4; i++ {
		switch m {
		case ViewLaunch:
			m = w.PrevViewMode
		case ViewProximity:
			m = w.ProxReturnView
		default:
			return m
		}
	}
	return ViewTilted
}

// CycleViewMode advances ViewMode to the next of the six map
// projections (ProjectionViewModes), wrapping around. Launch and
// Proximity are no longer cycle stops (issue #348 §4) — pressing `v`
// from inside either one ends that toggle session and advances from the
// projection it would have returned to (baseProjection), so `v` always
// lands on a projection regardless of where it was pressed from.
//
// A ViewMode change is a Framing Event (ADR 0021 A): the orbit screen
// refits the canvas once in response, then leaves the camera alone.
func (w *World) CycleViewMode() {
	if w.ViewMode == ViewLaunch {
		w.LaunchSessionActive = false
	}
	anchor := w.baseProjection(w.ViewMode)
	w.ViewMode = (anchor + 1) % ViewMode(len(ProjectionViewModes))
}

// ViewTilt holds the polar tilt θ and yaw φ (degrees) that
// ViewTilted applies to the projection basis. v0.10.6+. Per-session
// UI state — not persisted to save (same convention as ViewMode and
// InstantSAS). Theta is player-tunable via shift+up / shift+down at
// the orbit screen; Phi is player-tunable via { / } (ADR 0021 G,
// completing the "adjust angles" half of the KSP map-view intent).
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

// ViewTiltPhiStep is the per-press yaw nudge for the shift+←/→ keys.
// Unlike Theta there is no min/max — yaw is a full turn around the
// orbit, so the nudge wraps at 360° instead of clamping (ADR 0021 G).
const ViewTiltPhiStep = 5.0

// NudgeViewTiltPhi adds delta degrees to ViewTilt.Phi and wraps the
// result into [0°, 360°) — spinning past either end keeps rotating
// rather than pinning, unlike Theta's clamp. Returns the resulting
// Phi so the caller can stamp it into a status flash. ADR 0021 G.
func (w *World) NudgeViewTiltPhi(deltaDeg float64) float64 {
	w.ViewTilt.Phi = math.Mod(w.ViewTilt.Phi+deltaDeg, 360)
	if w.ViewTilt.Phi < 0 {
		w.ViewTilt.Phi += 360
	}
	return w.ViewTilt.Phi
}
