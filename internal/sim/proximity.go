package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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
		if back == ViewLaunch && !w.LaunchSessionActive {
			// The chase-cam we jumped from has since been released (an
			// active-vessel switch onto a flying vessel, or a `v` cycle out
			// of the stack). Restoring ViewLaunch anyway would drop the
			// player into a scene with no session behind it — T+ frozen at
			// zero, no trail, no max-Q — which reads as a broken view
			// rather than a return. Fall through to the map underneath it.
			back = w.baseProjection(ViewLaunch)
		}
		w.ViewMode = back
		return false, ""
	}
	if reason := w.ProximityRefusal(); reason != "" {
		return false, reason
	}
	// The return address may legitimately be ViewLaunch — the two jump keys
	// are documented to stack. What keeps that from becoming the mutual
	// capture issue #348's review found is the mirror-image invariant on
	// the other side: routeToLaunchView never records a SCENE in
	// PrevViewMode, only the projection underneath it (see its doc
	// comment), so this chain is always at most one hop deep and always
	// bottoms out on the map.
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

// ProximityDockGateReady reports whether the pair sits inside BOTH
// docking gates right now — exactly checkDocking's own auto-fuse
// predicate (DockingDistM / DockingVMS), reused rather than
// re-thresholded so the dock-gate ring's green state and the game's
// actual "you are about to dock" moment can never drift apart (issue
// #348 §1: "DOCK READY becomes a place on screen").
func (w *World) ProximityDockGateReady(st ProximityState) bool {
	return st.RangeM < DockingDistM && st.VRelMS < DockingVMS
}

// ProximityDriftHorizonMin / ProximityDriftHorizonMax bound the no-burn
// drift forecast (issue #348 §1): "a few minutes to tens of minutes" in
// the issue's own words. The screen picks a horizon inside this band
// from the current zoom (see orbit_proximity.go's
// proximityDriftHorizonFor) and hands it to ProximityDriftPath — the sim
// owns the physics of walking the horizon forward, the screen owns how
// far ahead is worth looking at the player's current scale.
const (
	ProximityDriftHorizonMin = 2 * time.Minute
	ProximityDriftHorizonMax = 30 * time.Minute
)

// proximityDriftSamples is the target number of points on the drawn
// drift curve — plenty for a dashed polyline over a few-minutes-to-tens-
// of-minutes horizon at docking-relevant relative speeds (impactPathSamples
// uses 180 for a full 30-minute descent arc that can pass through
// atmosphere; this curve is always vacuum two-body, so it needs far less
// density to read smoothly).
const proximityDriftSamples = 60

// proximityDriftSubStepCap bounds the drift forecast's integrator
// sub-step, seconds. predictMaxSubStep alone can return up to
// predictMaxSubStepCap (120 s) for a long-period orbit, which is far
// coarser than the whole drift horizon can afford to lose to a single
// step's dt² truncation on the Verlet fallback path (reached only for a
// non-Kepler-eligible state — an atmosphere-skimming or hyperbolic
// craft). 5 s keeps a full 30-minute horizon under proximityDriftMaxSubSteps.
const proximityDriftSubStepCap = 5.0

// proximityDriftMaxSubSteps caps total integrator work per forecast so a
// degenerate state (tiny period → tiny sub-step) can't stall the render
// loop — same role as impactMaxSubSteps.
const proximityDriftMaxSubSteps = 1024

// ProximityDriftPoint is one sample of the no-burn relative-drift
// forecast: the active vessel's position relative to the target,
// expressed in the LVLH frame the TARGET'S OWN STATE defines AT THAT
// FUTURE INSTANT — not today's frame. orbital.LVLHBasis's doc comment
// explains why the basis has to rotate with the target; a caller that
// reused today's frame for every future sample would draw a curve that
// lies (a target-and-chaser pair sitting still relative to each other in
// the CO-ROTATING frame — e.g. two craft on the same circular orbit at a
// fixed phase offset — would instead sweep around like an inertial
// orbit, because the frozen frame keeps rotating relative to the actual
// physics while the physics itself doesn't).
//
// Local follows LVLHFrame.ToFrame's axis order: X = along-track,
// Y = radial-out, Z = cross-track, all metres.
type ProximityDriftPoint struct {
	T     time.Duration
	Local orbital.Vec3
}

// ProximityDriftPath forward-propagates BOTH the active vessel and the
// target — using predictStep, the repo's one propagator, exactly like
// forwardBallisticPath's established reuse pattern (descent.go) — and
// differences their states at each sample instant, expressed in the
// target's LVLH frame resolved fresh at that instant. No CW
// linearization: this is the exact two-body difference, sampled.
//
// The target's own drag coefficient isn't generally available (a ghost
// target has no Spacecraft at all; even a local craft target's bc is a
// simplifying stand-in for whatever the OTHER player is actually flying)
// so it propagates with bc=0 — a reasonable default since proximity ops
// are, by construction, well clear of any atmosphere dense enough for
// the choice to matter over a few-to-tens-of-minutes horizon.
//
// Both states propagate under the ACTIVE vessel's primary/mu — the same
// simplification TargetStateRelativeToActivePrimary already makes for
// every other relative-vector readout this view draws from (range,
// |v_rel|, closing): Proximity View exists for two craft sharing a
// neighbourhood, which in practice means sharing a primary.
//
// ok is false only for the same degenerate-input cases the rest of this
// file already refuses (no active craft, no resolvable vessel/ghost
// target, no defined LVLH frame at the current or a future instant) or a
// non-positive horizon.
func (w *World) ProximityDriftPath(horizon time.Duration) ([]ProximityDriftPoint, bool) {
	c := w.ActiveCraft()
	if c == nil {
		return nil, false
	}
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return nil, false
	}
	frame0, ok := orbital.LVLHBasis(rT, vT)
	if !ok {
		return nil, false
	}
	primary := c.Primary
	mu := primary.GravitationalParameter()
	total := horizon.Seconds()
	if mu <= 0 || total <= 0 {
		return nil, false
	}

	craftState := c.State
	targetState := physics.StateVector{R: rT, V: vT}
	craftBC := c.EffectiveBallisticCoefficient()
	const targetBC = 0.0

	dt := predictMaxSubStep(orbitalPeriod(craftState, mu))
	if td := predictMaxSubStep(orbitalPeriod(targetState, mu)); td < dt {
		dt = td
	}
	if dt > proximityDriftSubStepCap {
		dt = proximityDriftSubStepCap
	}
	steps := int(math.Ceil(total / dt))
	if steps < 1 {
		steps = 1
	}
	if steps > proximityDriftMaxSubSteps {
		steps = proximityDriftMaxSubSteps
		dt = total / float64(steps)
	}
	sampleEvery := steps / proximityDriftSamples
	if sampleEvery < 1 {
		sampleEvery = 1
	}

	points := make([]ProximityDriftPoint, 0, proximityDriftSamples+2)
	points = append(points, ProximityDriftPoint{Local: frame0.ToFrame(craftState.R.Sub(rT))})

	elapsed := 0.0
	for i := 0; i < steps; i++ {
		craftState = predictStep(craftState, mu, dt, primary, craftBC)
		targetState = predictStep(targetState, mu, dt, primary, targetBC)
		elapsed += dt
		if (i+1)%sampleEvery != 0 && i != steps-1 {
			continue
		}
		frame, ok := orbital.LVLHBasis(targetState.R, targetState.V)
		if !ok {
			// Degenerate future frame (radial fall / zero speed) — stop
			// extending the curve rather than draw a bogus point; every
			// sample taken so far is still valid.
			break
		}
		points = append(points, ProximityDriftPoint{
			T:     time.Duration(elapsed * float64(time.Second)),
			Local: frame.ToFrame(craftState.R.Sub(targetState.R)),
		})
	}
	if len(points) < 2 {
		return nil, false
	}
	return points, true
}
