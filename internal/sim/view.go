package sim

import "math"

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
	// ViewTarget (v0.17.3+) centers the canvas on the current body Target
	// and auto-frames the craft→target approach (refitting every frame as
	// the gap closes), so the player can watch the projected orbit pass the
	// target while hand-flying engine/RCS corrections. Reuses the orbit-flat
	// projection basis. Selected from the `v` cycle, but only reachable when
	// a body target is set (CycleViewMode skips it otherwise); falls back to
	// the ordinary focus center when the target is cleared mid-view.
	ViewTarget
	// ViewSOIPass (v0.18.0+, ADR 0019 F) frames the Body of the active SOI
	// Pass — the predicted transit of the live, unburned trajectory through a
	// sibling Body's SOI — and auto-fits to that Body's SOI so the encounter
	// arc + Perilune marker fill the canvas and the curvature near perilune
	// reads clearly. Crucially it is *independent of the Target slot*: the SOI
	// Pass renders whether or not the Body is targeted, and so does this view —
	// entering/leaving it never touches w.Target. Reuses the orbit-flat
	// projection basis and the TargetViewFraming widening geometry (against the
	// Pass Body instead of the Target). When an encounter resolves it centers on
	// the predicted *arrival*-position perilune, not the body's current position
	// (issue #144). Selected from the `v` cycle, but only reachable when an
	// upcoming SOI pass exists — the planted (node-modified) one while flying a
	// transfer, else the live one (CycleViewMode skips it otherwise); falls back
	// to the ordinary focus center when the pass disappears mid-view (craft
	// captured, or the orbit no longer reaches the SOI).
	ViewSOIPass
	// ViewLaunch (v0.11.0+) is the chase-cam launch scene — a
	// human-scale side view with the rocket centred, the horizon
	// curving below in Body.SurfaceColor, and a body-fixed pad
	// marker + breadcrumb trail. Routed into automatically on
	// active-slot Landed-false→true transitions; auto-released when
	// the ascent goes nearly orbital (apoapsis clear of the atmosphere
	// and within the circularisation-Δv cap). Appended to
	// the cycle (NOT prepended) so ViewTilted stays the zero-value
	// default. ADR-0002 captures the rationale for shipping this as
	// a distinct ViewMode instead of extending ViewTilted.
	ViewLaunch
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
	case ViewTarget:
		return "target"
	case ViewSOIPass:
		return "soi-pass"
	case ViewLaunch:
		return "launch"
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
	ViewTarget,
	ViewSOIPass,
	ViewLaunch,
}

// CycleViewMode advances ViewMode to the next mode in cycle order.
// Wraps around — modes are a small finite set. v0.11.0+: leaving
// ViewLaunch via manual cycle clears LaunchSessionActive — the
// player has taken over, no auto-release will fire even if apo
// crosses the floor later. ViewMode still advances by one
// (cycle semantics are *advance*, not *restore* PrevViewMode).
func (w *World) CycleViewMode() {
	if w.ViewMode == ViewLaunch {
		w.LaunchSessionActive = false
	}
	next := (w.ViewMode + 1) % ViewMode(len(AllViewModes))
	// Skip view modes that have nothing to frame from the current world
	// state, so a manual `v` cycle never lands on a dead view: ViewTarget
	// needs a body Target, ViewSOIPass needs an upcoming SOI Pass (ADR 0019 F —
	// reachable when bestSOIPass returns ok, planted or live, and entering it
	// never touches w.Target). At most one full lap before giving up, so a state
	// with every conditional mode unavailable can't spin forever.
	for range AllViewModes {
		if !w.viewModeSelectable(next) {
			next = (next + 1) % ViewMode(len(AllViewModes))
			continue
		}
		break
	}
	w.ViewMode = next
}

// viewModeSelectable reports whether the `v` cycle may land on mode m given
// the current world state. The conditional modes are skipped when they'd
// frame nothing; every other mode is always selectable.
func (w *World) viewModeSelectable(m ViewMode) bool {
	switch m {
	case ViewTarget:
		return w.Target.Kind == TargetBody
	case ViewSOIPass:
		// Any upcoming pass — the planted (node-modified) one while flying a
		// transfer whose pre-burn orbit can't yet reach the body, else the live
		// pass (issue #144). Gating on LiveSOIPass alone made the view
		// unreachable during exactly the planted transfer it's most useful for.
		_, ok := w.bestSOIPass()
		return ok
	}
	return true
}

// SetViewModeLaunch (v0.11.4+, ADR 0004) is the manual-jump path
// for the `V` (shift+v) keybinding: short-circuits the lowercase
// `v` cycle and drops the player into ViewLaunch focused on the
// active vessel. Stashes the prior ViewMode into PrevViewMode (so
// the existing apo-floor auto-release can restore on liftoff) and
// opens a session — same surface as routeToLaunchView, just
// player-initiated rather than auto-routed. No active vessel is
// not a precondition; the LaunchView.Render path covers the
// nil-active case (sub-scope 5).
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

// ViewTiltPhiStep is the per-press yaw nudge for the { / } keys.
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
