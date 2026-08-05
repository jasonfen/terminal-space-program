package sim

import "math"

// ViewMode selects the canvas projection — which world axes map to
// canvas X+ and Y+. v0.6.4+. World-level state so the orbit screen
// and the maneuver-planner mini-canvas share the same camera angle
// without per-screen coordination.
//
// Projections only, per the Camera Contract (ADR 0021): a ViewMode
// never picks the camera's center or zoom — Focus picks *what* the
// camera centres on, ViewMode picks *which projection*. Seven modes:
// the v0.10.6 perspective-tilt view (the zero-value default), four
// hard-coded cardinal views (Top, Right, Bottom, Left), the
// orbit-flat view that projects onto the active craft's orbit plane
// regardless of inclination, and the launch chase-cam. The v0.17.3
// ViewTarget and v0.18.0 ViewSOIPass auto-framing views are retired
// (ADR 0021 D) — reading an encounter is "focus the pass Body", and
// the Local-to-Body arc draws the capture curve there.
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
	// left via a manual `v` cycle. ADR 0021 D retired the old
	// apoapsis-floor auto-release — no ambient sim state moves the
	// camera. Appended to the cycle (NOT prepended) so ViewTilted
	// stays the zero-value default. ADR-0002 captures the rationale
	// for shipping this as a distinct ViewMode instead of extending
	// ViewTilted.
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
	// left by the jump key; CycleViewMode skips it when unavailable
	// rather than parking the player in a dead view. Appended after
	// ViewLaunch so ViewTilted stays the zero-value default.
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

// AllViewModes enumerates the modes in canonical cycle order.
// Tilted → Top → Right → Bottom → Left → OrbitFlat → Launch →
// Proximity — the v0.10.6+ tilt opens the cycle as the new zero-value
// default, the four cardinal cameras follow (each rotates 90° around the
// system), orbit-flat lands as punctuation, and the two scene views
// (launch chase-cam, then ADR 0043's close-range Proximity View) close
// the lap before wrapping. ADR 0021 D removed the conditional ViewTarget
// / ViewSOIPass slots; ViewProximity is skipped-not-hidden (CycleViewMode
// steps over it without a vessel target) rather than reviving them.
//
// The values must stay contiguous 0..len-1 in this order: CycleViewMode
// advances by modular arithmetic on the ViewMode value, not by index
// lookup.
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

// CycleViewMode advances ViewMode to the next mode in cycle order.
// Wraps around — modes are a small finite set. v0.11.0+: leaving
// ViewLaunch via manual cycle clears LaunchSessionActive — and with
// ADR 0021 D this `v` cycle is THE way out of the chase cam (the
// apoapsis-floor auto-release is retired). ViewMode still advances
// by one (cycle semantics are *advance*, not *restore* PrevViewMode).
// A ViewMode change is a Framing Event (ADR 0021 A): the orbit screen
// refits the canvas once in response, then leaves the camera alone.
func (w *World) CycleViewMode() {
	if w.ViewMode == ViewLaunch {
		w.LaunchSessionActive = false
	}
	prev := w.ViewMode
	w.ViewMode = (w.ViewMode + 1) % ViewMode(len(AllViewModes))
	// Proximity View (ADR 0043) is target-gated: without a vessel or
	// ghost Target there is nothing to centre the frame on, so the cycle
	// steps over it rather than parking the player in a view that can
	// only apologise. Skipping (not hiding) keeps the cycle's arithmetic
	// unconditional — the slot exists whether or not it's reachable.
	if w.ViewMode == ViewProximity && !w.ProximityAvailable() {
		w.ViewMode = (w.ViewMode + 1) % ViewMode(len(AllViewModes))
	}
	if w.ViewMode == ViewProximity {
		// Arriving via the cycle still records where to come back to, so
		// a later jump-key press out of Proximity lands somewhere sane
		// instead of the zero-value default.
		w.ProxReturnView = prev
	}
}

// SetViewModeLaunch (v0.11.4+, ADR 0004) is the manual-jump path
// for the `V` (shift+v) keybinding: short-circuits the lowercase
// `v` cycle and drops the player into ViewLaunch focused on the
// active vessel. Stashes the prior ViewMode into PrevViewMode (so
// a switch-end release can restore it) and opens a session — same
// surface as routeToLaunchView, just player-initiated rather than
// auto-routed. Leaving is a manual `v` cycle (ADR 0021 D). No
// active vessel is not a precondition; the LaunchView.Render path
// covers the nil-active case (sub-scope 5).
func (w *World) SetViewModeLaunch() {
	w.routeToLaunchView()
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
