package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// Proximity View (ADR 0043) — the sim half. This file owns three things:
// resolving the target-centred LVLH scene the screen draws, the
// enter/leave toggle (including its refusals), and the once-per-crossing
// hint that tells the player the view exists at the moment it starts
// being useful.
//
// It computes no relative-motion math of its own: the target's state
// comes from TargetStateRelativeToActivePrimary (the same resolver the
// TARGET chip, the rendezvous planner and the co-warp gate read), and the
// basis comes from orbital.LVLHBasis. Two sources of truth for "how far
// apart are we" is exactly how a readout and a picture drift apart.

// ProximityState is the resolved close-range scene: where to put the
// camera, which way is which, and the three numbers the pilot flies on.
// Everything is evaluated at the world's current sim-time, so a caller
// rebuilds it per frame and the LVLH frame rotates with the target's
// orbit for free.
type ProximityState struct {
	// Frame is the target-centred LVLH basis. Its axes are expressed in
	// world inertial axes (the primary-relative frame shares them), so
	// the screen can hand AlongTrack / RadialOut straight to the canvas.
	Frame orbital.LVLHFrame

	// TargetWorld / CraftWorld are the inertial (system-primary-frame)
	// positions of the target and the active vessel — the canvas centre
	// and the one other thing this slice draws.
	TargetWorld orbital.Vec3
	CraftWorld  orbital.Vec3

	// RelPos / RelVel are the ACTIVE VESSEL relative to the TARGET, in
	// world axes. Sign convention: positive along Frame.AlongTrack means
	// you are AHEAD of the target; positive along Frame.RadialOut means
	// you are ABOVE it.
	RelPos orbital.Vec3
	RelVel orbital.Vec3

	// RangeM / VRelMS / ClosingMS are the readout triple. ClosingMS is
	// positive while the gap is shrinking — the same sign convention the
	// TARGET chip's "closing:" row has always used.
	RangeM    float64
	VRelMS    float64
	ClosingMS float64

	// TargetName is the label to put on the centred vessel — a local
	// vessel's name or a remote player's ghost label.
	TargetName string
}

// ProximityState resolves the close-range scene against the current
// Target. ok=false whenever the view has nothing to show; use
// ProximityRefusal for the player-facing reason.
func (w *World) ProximityState() (ProximityState, bool) {
	c := w.ActiveCraft()
	if c == nil {
		return ProximityState{}, false
	}
	// Craft and ghost targets both resolve here (HasRelativeTarget's two
	// kinds); a body target returns ok=false, which is the availability
	// gate. Ghosts come back Kepler-propagated to this world's sim-time —
	// the same state the ghost's own drawing and the rendezvous-warp
	// coast read, so the picture can't disagree with them.
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return ProximityState{}, false
	}
	frame, ok := orbital.LVLHBasis(rT, vT)
	if !ok {
		return ProximityState{}, false
	}
	primaryPos := w.BodyPosition(c.Primary)
	rel := c.State.R.Sub(rT)
	relV := c.State.V.Sub(vT)
	rangeM := rel.Norm()
	var closing float64
	if rangeM > 0 {
		// -d|rel|/dt: positive while the gap shrinks.
		closing = -rel.Dot(relV) / rangeM
	}
	return ProximityState{
		Frame:       frame,
		TargetWorld: primaryPos.Add(rT),
		CraftWorld:  primaryPos.Add(c.State.R),
		RelPos:      rel,
		RelVel:      relV,
		RangeM:      rangeM,
		VRelMS:      relV.Norm(),
		ClosingMS:   closing,
		TargetName:  w.TargetName(),
	}, true
}

// ProximityAvailable reports whether Proximity View has a scene to draw.
func (w *World) ProximityAvailable() bool {
	_, ok := w.ProximityState()
	return ok
}

// ProximityRefusal returns the player-facing reason Proximity View can't
// open, or "" when it can. A silent no-op reads as a broken key, so every
// path that refuses hands back a sentence naming the fix.
func (w *World) ProximityRefusal() string {
	if w.ProximityAvailable() {
		return ""
	}
	if w.ActiveCraft() == nil {
		return "no vessel to fly"
	}
	switch w.Target.Kind {
	case TargetNone:
		return "no target — [t] cycles to a nearby vessel"
	case TargetBody, TargetSite:
		return "needs a VESSEL target, not a body — [t] cycles to nearby vessels"
	}
	// A craft/ghost target that didn't resolve: a stale ref (they left,
	// the vessel is gone) or a degenerate orbit with no defined local
	// horizontal. Both read the same from the seat — the aim slot points
	// at something the view can't find.
	return "target vessel isn't resolvable from here"
}

// ToggleProximityView is the jump key's whole behaviour: press to enter,
// press again to return to the map exactly as it was. entered reports
// which half fired; refusal is non-empty only when entry was refused
// (leaving never refuses — you can always get back to the map).
//
// The return trip restores the ViewMode the player jumped FROM, which is
// what makes this a toggle rather than another stop on the `v` cycle. The
// map's zoom and pan are restored screen-side (Proximity keeps its own
// zoom-memory slot); this half owns only the ViewMode.
func (w *World) ToggleProximityView() (entered bool, refusal string) {
	if w.ViewMode == ViewProximity {
		back := w.ProxReturnView
		if back == ViewProximity {
			// Defensive: never toggle out of Proximity into Proximity.
			back = ViewTilted
		}
		w.ViewMode = back
		return false, ""
	}
	if reason := w.ProximityRefusal(); reason != "" {
		return false, reason
	}
	w.ProxReturnView = w.ViewMode
	w.ViewMode = ViewProximity
	// Entering answers the hint, so it stops asking until the pair
	// separates and closes again.
	w.proxHint.dismissed = true
	return true, ""
}

// proximityHint is the once-per-crossing state behind the "you can jump
// to Proximity View now" chip. The chip is a standing block, not an
// event, so the flicker risk is a range oscillating on the threshold
// rather than a burst of toasts — hence a hysteresis band (arm at the
// co-warp couple range, re-arm only past the decouple range) instead of a
// cooldown. Session state; never persisted.
type proximityHint struct {
	// inside is true between crossing INTO the hint band and separating
	// back out past the release range.
	inside bool
	// target is the aim slot the current crossing belongs to. Retargeting
	// is a new question, so it restarts the crossing.
	target Target
	// dismissed records that the player already acted on this crossing by
	// entering Proximity View. Cleared when the crossing ends.
	dismissed bool
}

// proximityHintRangeM / proximityHintReleaseM are the hint's hysteresis
// band, deliberately the co-warp couple/decouple pair rather than numbers
// of their own: 35 km is already the range at which the game declares two
// players to be in the same neighbourhood (their warps lock), so it is
// exactly the moment "you are now flying relative to them" becomes true.
// Inventing a second proximity radius would give the player two different
// answers to the same question.
const (
	proximityHintRangeM   = coWarpCoupleRangeM
	proximityHintReleaseM = coWarpDecoupleRangeM
)

// updateProximityHint steps the crossing state machine. Called once per
// Tick, after integration, so it reads final positions.
func (w *World) updateProximityHint() {
	st, ok := w.ProximityState()
	if !ok {
		w.proxHint = proximityHint{}
		return
	}
	if w.proxHint.target != w.Target {
		w.proxHint = proximityHint{target: w.Target}
	}
	switch {
	case !w.proxHint.inside && st.RangeM <= proximityHintRangeM:
		w.proxHint.inside = true
	case w.proxHint.inside && st.RangeM >= proximityHintReleaseM:
		w.proxHint = proximityHint{target: w.Target}
	}
}

// ProximityHintActive reports whether the "jump key is available" chip
// should render this frame: inside the band, not already answered, and
// not already in the view it advertises.
func (w *World) ProximityHintActive() bool {
	if !w.proxHint.inside || w.proxHint.dismissed || w.ViewMode == ViewProximity {
		return false
	}
	return w.ProximityAvailable()
}
